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
	"gorm.io/gorm"
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
			if fallback := buildAnthropicPassiveUsage(&account, time.Now()); fallback != nil {
				return fallback, nil
			}
			if stale := staleUpstreamAccountUsage(accountId, account.CredentialVersion); stale != nil {
				return stale, nil
			}
			return nil, queryErr
		}
		syncAnthropicActiveUsageToPassive(&account, usage)
		if usage.SevenDayFable == nil {
			if fable := buildPassiveAnthropicUsageWindow(account.Extra, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset", time.Now()); fable != nil {
				usage.SevenDayFable = fable
			}
		}
		upstreamAccountUsageCache.Store(accountId, upstreamAccountUsageCacheEntry{
			usage: *usage, credentialVersion: account.CredentialVersion, cachedAt: time.Now(),
		})
		return usage, nil
	})
	if err != nil {
		if fallback := buildAnthropicPassiveUsage(&account, time.Now()); fallback != nil {
			attachAnthropicWindowStats(&account, fallback, time.Now())
			return fallback, nil
		}
		if stale := staleUpstreamAccountUsage(accountId, account.CredentialVersion); stale != nil {
			attachAnthropicWindowStats(&account, stale, time.Now())
			return stale, nil
		}
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

func staleUpstreamAccountUsage(accountId int, credentialVersion int64) *UpstreamAccountUsage {
	value, ok := upstreamAccountUsageCache.Load(accountId)
	if !ok {
		return nil
	}
	entry, ok := value.(upstreamAccountUsageCacheEntry)
	if !ok || entry.credentialVersion != credentialVersion {
		return nil
	}
	usage := entry.usage
	if usage.Source == "" || usage.Source == "active" {
		usage.Source = "cached"
	}
	return &usage
}

// buildAnthropicPassiveUsage reconstructs usage windows from Extra / session_window
// fields written by prior successful probes (sub2api GetPassiveUsage equivalent).
func buildAnthropicPassiveUsage(account *model.UpstreamAccount, now time.Time) *UpstreamAccountUsage {
	if account == nil {
		return nil
	}
	usage := buildAnthropicSetupTokenUsage(account, now)
	usage.Source = "passive"
	if sampledAt := anthropicPassiveSampledAt(account.Extra); sampledAt > 0 {
		usage.FetchedAt = sampledAt
	}
	usage.SevenDay = buildPassiveAnthropicUsageWindow(account.Extra, "passive_usage_7d_utilization", "passive_usage_7d_reset", now)
	usage.SevenDaySonnet = buildPassiveAnthropicUsageWindow(account.Extra, "passive_usage_7d_sonnet_utilization", "passive_usage_7d_sonnet_reset", now)
	usage.SevenDayFable = buildPassiveAnthropicUsageWindow(account.Extra, "passive_usage_7d_oi_utilization", "passive_usage_7d_oi_reset", now)
	if usage.FiveHour == nil && usage.SevenDay == nil && usage.SevenDaySonnet == nil && usage.SevenDayFable == nil {
		return nil
	}
	// Setup-token style 5h is always present; only keep it when it carries a real
	// utilization, reset, or session window. Otherwise require another window.
	if usage.SevenDay == nil && usage.SevenDaySonnet == nil && usage.SevenDayFable == nil {
		if usage.FiveHour == nil {
			return nil
		}
		hasSignal := usage.FiveHour.UsedPercent > 0 || usage.FiveHour.ResetAt > 0 ||
			anthropicHasSessionWindow(account) || anthropicHasSessionUtilization(account.Extra)
		if !hasSignal {
			return nil
		}
	}
	return &usage
}

func anthropicHasSessionWindow(account *model.UpstreamAccount) bool {
	return account != nil && account.SessionWindowEnd != nil && *account.SessionWindowEnd > 0
}

func anthropicHasSessionUtilization(rawExtra string) bool {
	extra := map[string]any{}
	if strings.TrimSpace(rawExtra) == "" || common.UnmarshalJsonStr(rawExtra, &extra) != nil {
		return false
	}
	_, ok := quotaNumber(extra["session_window_utilization"])
	return ok
}

func anthropicPassiveSampledAt(rawExtra string) int64 {
	extra := map[string]any{}
	if strings.TrimSpace(rawExtra) == "" || common.UnmarshalJsonStr(rawExtra, &extra) != nil {
		return 0
	}
	return codexExtraResetAt(extra["passive_usage_sampled_at"])
}

func buildPassiveAnthropicUsageWindow(rawExtra string, utilKey string, resetKey string, now time.Time) *UpstreamAccountUsageWindow {
	extra := map[string]any{}
	if strings.TrimSpace(rawExtra) == "" || common.UnmarshalJsonStr(rawExtra, &extra) != nil {
		return nil
	}
	util, hasUtil := quotaNumber(extra[utilKey])
	resetUnix, hasReset := quotaNumber(extra[resetKey])
	if !hasUtil && (!hasReset || resetUnix <= 0) {
		return nil
	}
	window := &UpstreamAccountUsageWindow{
		UsedPercent:        util * 100,
		LimitWindowSeconds: int64((7 * 24 * time.Hour).Seconds()),
	}
	if hasReset && resetUnix > 0 {
		window.ResetAt = int64(resetUnix)
		window.ResetAfterSeconds = max(0, int64(resetUnix)-now.Unix())
		if window.ResetAt <= now.Unix() {
			window.UsedPercent = 0
		}
	}
	return window
}

// syncAnthropicActiveUsageToPassive writes successful OAuth usage into Extra so
// expired / temporarily unreachable accounts can still render last-known bars.
func syncAnthropicActiveUsageToPassive(account *model.UpstreamAccount, usage *UpstreamAccountUsage) {
	if account == nil || usage == nil || account.Id <= 0 {
		return
	}
	updates := map[string]any{}
	if usage.FiveHour != nil {
		updates["session_window_utilization"] = usage.FiveHour.UsedPercent / 100
	}
	if usage.SevenDay != nil {
		updates["passive_usage_7d_utilization"] = usage.SevenDay.UsedPercent / 100
		if usage.SevenDay.ResetAt > 0 {
			updates["passive_usage_7d_reset"] = usage.SevenDay.ResetAt
		}
	}
	if usage.SevenDaySonnet != nil {
		updates["passive_usage_7d_sonnet_utilization"] = usage.SevenDaySonnet.UsedPercent / 100
		if usage.SevenDaySonnet.ResetAt > 0 {
			updates["passive_usage_7d_sonnet_reset"] = usage.SevenDaySonnet.ResetAt
		}
	}
	if usage.SevenDayFable != nil {
		updates["passive_usage_7d_oi_utilization"] = usage.SevenDayFable.UsedPercent / 100
		if usage.SevenDayFable.ResetAt > 0 {
			updates["passive_usage_7d_oi_reset"] = usage.SevenDayFable.ResetAt
		}
	}
	if len(updates) == 0 {
		return
	}
	updates["passive_usage_sampled_at"] = time.Now().UTC().Format(time.RFC3339)
	persistAnthropicPassiveUsage(account.Id, updates)
	if usage.FiveHour != nil && usage.FiveHour.ResetAt > 0 {
		now := time.Now().Unix()
		end := usage.FiveHour.ResetAt
		start := end - usage.FiveHour.LimitWindowSeconds
		if usage.FiveHour.LimitWindowSeconds <= 0 {
			start = end - int64((5 * time.Hour).Seconds())
		}
		_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).Updates(map[string]any{
			"session_window_start":  start,
			"session_window_end":    end,
			"session_window_status": "active",
			"updated_at":            now,
		}).Error
		account.SessionWindowStart = &start
		account.SessionWindowEnd = &end
		account.SessionWindowStatus = "active"
	}
}

func persistAnthropicPassiveUsage(accountId int, updates map[string]any) {
	if accountId <= 0 || len(updates) == 0 {
		return
	}
	_ = model.DB.Transaction(func(tx *gorm.DB) error {
		var account model.UpstreamAccount
		if err := tx.Select("id", "extra").First(&account, accountId).Error; err != nil {
			return err
		}
		extra := map[string]any{}
		if strings.TrimSpace(account.Extra) != "" {
			_ = common.UnmarshalJsonStr(account.Extra, &extra)
		}
		for key, value := range updates {
			extra[key] = value
		}
		encoded, err := common.Marshal(extra)
		if err != nil {
			return err
		}
		return tx.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Update("extra", string(encoded)).Error
	})
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
