package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"
)

const (
	upstreamAnthropicUsageURL      = "https://api.anthropic.com/api/oauth/usage"
	upstreamAnthropicUsageCacheTTL = 3 * time.Minute
	upstreamAnthropicUsageTimeout  = 30 * time.Second
	upstreamWindowStatsCacheTTL    = time.Minute
)

type UpstreamAccountWindowStats = model.UpstreamAccountWindowStats

type UpstreamAccountUsageWindow struct {
	UsedPercent        float64                     `json:"used_percent"`
	LimitWindowSeconds int64                       `json:"limit_window_seconds"`
	ResetAfterSeconds  int64                       `json:"reset_after_seconds"`
	ResetAt            int64                       `json:"reset_at"`
	WindowStats        *UpstreamAccountWindowStats `json:"window_stats,omitempty"`
}

type UpstreamAccountUsage struct {
	Source         string                      `json:"source"`
	FiveHour       *UpstreamAccountUsageWindow `json:"five_hour,omitempty"`
	SevenDay       *UpstreamAccountUsageWindow `json:"seven_day,omitempty"`
	SevenDaySonnet *UpstreamAccountUsageWindow `json:"seven_day_sonnet,omitempty"`
	SevenDayFable  *UpstreamAccountUsageWindow `json:"seven_day_fable,omitempty"`
	FetchedAt      int64                       `json:"fetched_at"`
}

type upstreamAnthropicUsageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

type upstreamAnthropicUsageResponse struct {
	FiveHour                upstreamAnthropicUsageWindow `json:"five_hour"`
	SevenDay                upstreamAnthropicUsageWindow `json:"seven_day"`
	SevenDaySonnet          upstreamAnthropicUsageWindow `json:"seven_day_sonnet"`
	SevenDayOverageIncluded upstreamAnthropicUsageWindow `json:"seven_day_overage_included"`
}

type upstreamAccountUsageCacheEntry struct {
	usage             UpstreamAccountUsage
	credentialVersion int64
	cachedAt          time.Time
}

type upstreamWindowStatsCacheEntry struct {
	stats    UpstreamAccountWindowStats
	cachedAt time.Time
}

var (
	upstreamAccountUsageCache  sync.Map
	upstreamAccountUsageFlight singleflight.Group
	upstreamWindowStatsCache   sync.Map
)

func QueryUpstreamAccountUsage(ctx context.Context, accountId int, force bool) (*UpstreamAccountUsage, error) {
	if accountId <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	var account model.UpstreamAccount
	if err := model.DB.First(&account, accountId).Error; err != nil {
		return nil, err
	}
	if account.Platform != constant.UpstreamPlatformAnthropic ||
		(account.Type != constant.UpstreamAccountTypeOAuth && account.Type != constant.UpstreamAccountTypeSetupToken) {
		return nil, errors.New("usage operations require an Anthropic OAuth or setup token account")
	}
	if account.Type == constant.UpstreamAccountTypeSetupToken {
		usage := buildAnthropicSetupTokenUsage(&account, time.Now())
		attachAnthropicWindowStats(&account, &usage, time.Now())
		return &usage, nil
	}

	if !force {
		if cached := cachedUpstreamAccountUsage(accountId, account.CredentialVersion); cached != nil {
			attachAnthropicWindowStats(&account, cached, time.Now())
			return cached, nil
		}
	}
	flightKey := fmt.Sprintf("%d:%d", accountId, account.CredentialVersion)
	result, err, _ := upstreamAccountUsageFlight.Do(flightKey, func() (any, error) {
		if !force {
			if cached := cachedUpstreamAccountUsage(accountId, account.CredentialVersion); cached != nil {
				return cached, nil
			}
		}
		usage, queryErr := queryAnthropicOAuthUsage(ctx, &account, true)
		if queryErr != nil {
			return nil, queryErr
		}
		upstreamAccountUsageCache.Store(accountId, upstreamAccountUsageCacheEntry{
			usage: *usage, credentialVersion: account.CredentialVersion, cachedAt: time.Now(),
		})
		return usage, nil
	})
	if err != nil {
		return nil, err
	}
	usage, _ := result.(*UpstreamAccountUsage)
	attachAnthropicWindowStats(&account, usage, time.Now())
	return usage, nil
}

func cachedUpstreamAccountUsage(accountId int, credentialVersion int64) *UpstreamAccountUsage {
	value, ok := upstreamAccountUsageCache.Load(accountId)
	if !ok {
		return nil
	}
	entry, ok := value.(upstreamAccountUsageCacheEntry)
	if !ok || entry.credentialVersion != credentialVersion || time.Since(entry.cachedAt) >= upstreamAnthropicUsageCacheTTL {
		upstreamAccountUsageCache.Delete(accountId)
		return nil
	}
	usage := entry.usage
	return &usage
}

func queryAnthropicOAuthUsage(ctx context.Context, account *model.UpstreamAccount, allowRefresh bool) (*UpstreamAccountUsage, error) {
	credentials, err := DecryptUpstreamAccountCredentials(account)
	if err != nil {
		return nil, errors.New("account credential cannot be decrypted")
	}
	accessToken := upstreamCredentialMapString(credentials, "access_token")
	if accessToken == "" {
		return nil, errors.New("Anthropic OAuth credential is missing access_token")
	}
	_, proxyURL, err := resolveUpstreamProxy(ctx, account, nil, "")
	if err != nil {
		return nil, err
	}
	client, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, upstreamAnthropicUsageTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, upstreamAnthropicUsageURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("anthropic-beta", "oauth-2025-04-20")
	request.Header.Set("User-Agent", "claude-code/2.1.220")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		if allowRefresh && (response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden) &&
			account.OAuthRefreshOwner == constant.UpstreamOAuthRefreshOwnerStarNexus {
			if _, refreshErr := RefreshUpstreamOAuthAccount(ctx, account.Id); refreshErr != nil {
				return nil, refreshErr
			}
			if err := model.DB.First(account, account.Id).Error; err != nil {
				return nil, err
			}
			return queryAnthropicOAuthUsage(ctx, account, false)
		}
		return nil, fmt.Errorf("Anthropic usage API returned status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload upstreamAnthropicUsageResponse
	if err := common.DecodeJson(response.Body, &payload); err != nil {
		return nil, fmt.Errorf("decode Anthropic usage response: %w", err)
	}
	now := time.Now()
	return &UpstreamAccountUsage{
		Source:         "active",
		FiveHour:       buildAnthropicUsageWindow(payload.FiveHour, 5*time.Hour, now),
		SevenDay:       buildAnthropicUsageWindow(payload.SevenDay, 7*24*time.Hour, now),
		SevenDaySonnet: buildAnthropicUsageWindow(payload.SevenDaySonnet, 7*24*time.Hour, now),
		SevenDayFable:  buildAnthropicUsageWindow(payload.SevenDayOverageIncluded, 7*24*time.Hour, now),
		FetchedAt:      now.Unix(),
	}, nil
}

func buildAnthropicUsageWindow(window upstreamAnthropicUsageWindow, duration time.Duration, now time.Time) *UpstreamAccountUsageWindow {
	if strings.TrimSpace(window.ResetsAt) == "" && window.Utilization == 0 {
		return nil
	}
	result := &UpstreamAccountUsageWindow{
		UsedPercent:        window.Utilization,
		LimitWindowSeconds: int64(duration.Seconds()),
	}
	if resetAt, err := time.Parse(time.RFC3339, strings.TrimSpace(window.ResetsAt)); err == nil {
		result.ResetAt = resetAt.Unix()
		result.ResetAfterSeconds = max(0, int64(resetAt.Sub(now).Seconds()))
	}
	return result
}

func buildAnthropicSetupTokenUsage(account *model.UpstreamAccount, now time.Time) UpstreamAccountUsage {
	result := UpstreamAccountUsage{Source: "estimated", FetchedAt: now.Unix()}
	window := &UpstreamAccountUsageWindow{LimitWindowSeconds: int64((5 * time.Hour).Seconds())}
	if account != nil {
		utilizationFound := false
		var extra map[string]any
		if common.UnmarshalJsonStr(account.Extra, &extra) == nil {
			if utilization, ok := quotaNumber(extra["session_window_utilization"]); ok {
				window.UsedPercent = utilization * 100
				utilizationFound = true
			}
		}
		if !utilizationFound {
			switch account.SessionWindowStatus {
			case "rejected":
				window.UsedPercent = 100
			case "allowed_warning":
				window.UsedPercent = 80
			}
		}
		if account.SessionWindowStart != nil && account.SessionWindowEnd != nil && *account.SessionWindowEnd > *account.SessionWindowStart {
			window.LimitWindowSeconds = *account.SessionWindowEnd - *account.SessionWindowStart
		}
		if account.SessionWindowEnd != nil && *account.SessionWindowEnd > now.Unix() {
			window.ResetAt = *account.SessionWindowEnd
			window.ResetAfterSeconds = *account.SessionWindowEnd - now.Unix()
		} else {
			window.UsedPercent = 0
		}
	}
	result.FiveHour = window
	return result
}

func attachAnthropicWindowStats(account *model.UpstreamAccount, usage *UpstreamAccountUsage, now time.Time) {
	if account == nil || usage == nil || usage.FiveHour == nil {
		return
	}
	stats, err := queryUpstreamWindowStats(account.Id, upstreamWindowStatsStart(usage.FiveHour.ResetAt, 5*time.Hour, now))
	if err == nil {
		usage.FiveHour.WindowStats = stats
	}
}

func upstreamWindowStatsStart(resetAt int64, duration time.Duration, now time.Time) time.Time {
	if resetAt > now.Unix() {
		return time.Unix(resetAt, 0).Add(-duration)
	}
	return now.Add(-duration)
}

func queryUpstreamWindowStats(accountId int, start time.Time) (*UpstreamAccountWindowStats, error) {
	key := fmt.Sprintf("%d:%d", accountId, start.Unix())
	if value, ok := upstreamWindowStatsCache.Load(key); ok {
		if entry, ok := value.(upstreamWindowStatsCacheEntry); ok && time.Since(entry.cachedAt) < upstreamWindowStatsCacheTTL {
			stats := entry.stats
			return &stats, nil
		}
		upstreamWindowStatsCache.Delete(key)
	}
	stats, err := model.GetUpstreamAccountWindowStats(accountId, start.Unix())
	if err != nil {
		return nil, err
	}
	upstreamWindowStatsCache.Store(key, upstreamWindowStatsCacheEntry{stats: *stats, cachedAt: time.Now()})
	return stats, nil
}
