package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const openAICodexUsageSnapshotTTL = 5 * time.Minute

func refreshOpenAICodexUsageWindows(ctx context.Context, account *model.UpstreamAccount, call *upstreamAccountQuotaCall, usage *UpstreamAccountQuotaUsage, force bool, now time.Time) {
	if account == nil || call == nil || usage == nil {
		return
	}
	upstreamFiveHour, upstreamSevenDay := quotaUsageWindows(usage)
	fiveHour, sevenDay := upstreamFiveHour, upstreamSevenDay
	storedFiveHour, storedSevenDay := openAICodexWindowsFromExtra(account.Extra, now)
	if fiveHour == nil {
		fiveHour = storedFiveHour
	}
	if sevenDay == nil {
		sevenDay = storedSevenDay
	}
	setCanonicalQuotaUsageWindows(usage, fiveHour, sevenDay)

	if !shouldProbeOpenAICodexUsageWindows(force, upstreamFiveHour, upstreamSevenDay, fiveHour, sevenDay, account.Extra, now) {
		if upstreamFiveHour != nil && upstreamSevenDay != nil {
			persistOpenAICodexSnapshotAsync(account.Id, openAICodexSnapshotUpdates(upstreamFiveHour, upstreamSevenDay, now))
		}
		return
	}
	probedFiveHour, probedSevenDay, err := probeOpenAICodexUsageWindows(ctx, call, now)
	if err != nil {
		return
	}
	if probedFiveHour != nil {
		fiveHour = probedFiveHour
	}
	if probedSevenDay != nil {
		sevenDay = probedSevenDay
	}
	setCanonicalQuotaUsageWindows(usage, fiveHour, sevenDay)

	updates := openAICodexSnapshotUpdates(fiveHour, sevenDay, now)
	if len(updates) == 0 {
		return
	}
	persistOpenAICodexSnapshotAsync(account.Id, updates)
}

func shouldProbeOpenAICodexUsageWindows(force bool, upstreamFiveHour *UpstreamAccountRateLimitWindow, upstreamSevenDay *UpstreamAccountRateLimitWindow, mergedFiveHour *UpstreamAccountRateLimitWindow, mergedSevenDay *UpstreamAccountRateLimitWindow, rawExtra string, now time.Time) bool {
	if force {
		return true
	}
	if upstreamFiveHour != nil && upstreamSevenDay != nil {
		return false
	}
	if mergedFiveHour == nil || mergedSevenDay == nil {
		return true
	}
	return openAICodexSnapshotStale(rawExtra, now)
}

func probeOpenAICodexUsageWindows(ctx context.Context, call *upstreamAccountQuotaCall, now time.Time) (*UpstreamAccountRateLimitWindow, *UpstreamAccountRateLimitWindow, error) {
	if call == nil || call.client == nil || call.account.Id <= 0 {
		return nil, nil, errors.New("OpenAI Codex probe is unavailable")
	}
	modelName := normalizeUpstreamAccountProbeModel(&call.account, upstreamAccountProbeModel(&call.account, call.credentials))
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, _, err := buildUpstreamGenerationProbeRequest(probeCtx, &call.account, call.credentials, modelName, UpstreamAccountTestModeDefault)
	if err != nil {
		return nil, nil, err
	}
	response, err := call.client.Do(request)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()

	fiveHour, sevenDay := openAICodexWindowsFromHeader(response.Header, now)
	if fiveHour != nil || sevenDay != nil {
		return fiveHour, sevenDay, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, nil, errors.New("OpenAI Codex probe did not return usage windows")
	}
	return nil, nil, nil
}

func openAICodexWindowsFromHeader(header http.Header, now time.Time) (*UpstreamAccountRateLimitWindow, *UpstreamAccountRateLimitWindow) {
	primary := codexQuotaWindowFromHeader(header, "x-codex-primary", now)
	secondary := codexQuotaWindowFromHeader(header, "x-codex-secondary", now)
	return canonicalCodexWindows(primary, secondary)
}

func codexQuotaWindowFromHeader(header http.Header, prefix string, now time.Time) *UpstreamAccountRateLimitWindow {
	parsed := parseCodexRateLimitWindow(header, prefix)
	if !parsed.hasUsed && parsed.resetSeconds <= 0 && parsed.windowMinutes <= 0 {
		return nil
	}
	window := &UpstreamAccountRateLimitWindow{
		UsedPercent: parsed.usedPercent, ResetAfterSeconds: parsed.resetSeconds,
		LimitWindowSeconds: parsed.windowMinutes * 60,
	}
	if parsed.resetSeconds > 0 {
		window.ResetAt = now.Unix() + parsed.resetSeconds
	}
	return window
}

func canonicalCodexWindows(primary *UpstreamAccountRateLimitWindow, secondary *UpstreamAccountRateLimitWindow) (*UpstreamAccountRateLimitWindow, *UpstreamAccountRateLimitWindow) {
	if primary == nil && secondary == nil {
		return nil, nil
	}
	if primary != nil && secondary != nil && primary.LimitWindowSeconds > 0 && secondary.LimitWindowSeconds > 0 {
		if primary.LimitWindowSeconds < secondary.LimitWindowSeconds {
			return primary, secondary
		}
		return secondary, primary
	}
	if primary != nil && primary.LimitWindowSeconds > 0 {
		if primary.LimitWindowSeconds < int64((24 * time.Hour).Seconds()) {
			return primary, secondary
		}
		return secondary, primary
	}
	if secondary != nil && secondary.LimitWindowSeconds > 0 {
		if secondary.LimitWindowSeconds < int64((24 * time.Hour).Seconds()) {
			return secondary, primary
		}
		return primary, secondary
	}
	// Codex legacy headers use primary for 7d and secondary for 5h.
	return secondary, primary
}

func openAICodexWindowsFromExtra(rawExtra string, now time.Time) (*UpstreamAccountRateLimitWindow, *UpstreamAccountRateLimitWindow) {
	extra := map[string]any{}
	if strings.TrimSpace(rawExtra) == "" || common.UnmarshalJsonStr(rawExtra, &extra) != nil {
		return nil, nil
	}
	return openAICodexWindowFromExtra(extra, "5h", 5*time.Hour, now), openAICodexWindowFromExtra(extra, "7d", 7*24*time.Hour, now)
}

func openAICodexWindowFromExtra(extra map[string]any, label string, fallback time.Duration, now time.Time) *UpstreamAccountRateLimitWindow {
	used, ok := quotaNumber(extra["codex_"+label+"_used_percent"])
	if !ok {
		return nil
	}
	window := &UpstreamAccountRateLimitWindow{UsedPercent: used, LimitWindowSeconds: int64(fallback.Seconds())}
	if minutes, ok := quotaNumber(extra["codex_"+label+"_window_minutes"]); ok && minutes > 0 {
		window.LimitWindowSeconds = int64(minutes * 60)
	}
	if seconds, ok := quotaNumber(extra["codex_"+label+"_reset_after_seconds"]); ok && seconds > 0 {
		window.ResetAfterSeconds = int64(seconds)
	}
	if resetAt := codexExtraResetAt(extra["codex_"+label+"_reset_at"]); resetAt > 0 {
		window.ResetAt = resetAt
		window.ResetAfterSeconds = max(int64(0), resetAt-now.Unix())
		if resetAt <= now.Unix() {
			window.UsedPercent = 0
		}
	} else if updatedAt := codexExtraResetAt(extra["codex_usage_updated_at"]); updatedAt > 0 && window.ResetAfterSeconds > 0 {
		window.ResetAt = updatedAt + window.ResetAfterSeconds
		window.ResetAfterSeconds = max(int64(0), window.ResetAt-now.Unix())
		if window.ResetAt <= now.Unix() {
			window.UsedPercent = 0
		}
	}
	return window
}

func codexExtraResetAt(value any) int64 {
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return 0
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			return parsed.Unix()
		}
	}
	if number, ok := quotaNumber(value); ok && number > 0 {
		return int64(number)
	}
	return 0
}

func openAICodexSnapshotStale(rawExtra string, now time.Time) bool {
	extra := map[string]any{}
	if strings.TrimSpace(rawExtra) == "" || common.UnmarshalJsonStr(rawExtra, &extra) != nil {
		return true
	}
	updatedAt := codexExtraResetAt(extra["codex_usage_updated_at"])
	return updatedAt <= 0 || now.Unix()-updatedAt >= int64(openAICodexUsageSnapshotTTL.Seconds())
}

func openAICodexSnapshotUpdates(fiveHour *UpstreamAccountRateLimitWindow, sevenDay *UpstreamAccountRateLimitWindow, now time.Time) map[string]any {
	updates := map[string]any{}
	appendCodexWindowSnapshot(updates, "5h", fiveHour)
	appendCodexWindowSnapshot(updates, "7d", sevenDay)
	if len(updates) > 0 {
		updates["codex_usage_updated_at"] = now.UTC().Format(time.RFC3339)
	}
	return updates
}

func appendCodexWindowSnapshot(updates map[string]any, label string, window *UpstreamAccountRateLimitWindow) {
	if window == nil {
		return
	}
	updates["codex_"+label+"_used_percent"] = window.UsedPercent
	updates["codex_"+label+"_reset_after_seconds"] = window.ResetAfterSeconds
	updates["codex_"+label+"_window_minutes"] = window.LimitWindowSeconds / 60
	if window.ResetAt > 0 {
		updates["codex_"+label+"_reset_at"] = time.Unix(window.ResetAt, 0).UTC().Format(time.RFC3339)
	}
}

func persistOpenAICodexSnapshot(accountId int, updates map[string]any) {
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

func persistOpenAICodexSnapshotAsync(accountId int, updates map[string]any) {
	if accountId <= 0 || len(updates) == 0 {
		return
	}
	gopool.Go(func() {
		persistOpenAICodexSnapshot(accountId, updates)
	})
}
