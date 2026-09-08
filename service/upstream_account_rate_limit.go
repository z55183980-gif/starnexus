package service

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
)

type codexRateLimitWindow struct {
	usedPercent   float64
	hasUsed       bool
	resetSeconds  int64
	windowMinutes int64
}

const codexSevenDayWindowMinutes int64 = 7 * 24 * 60

func upstreamRateLimitState(apiErr *types.NewAPIError, now int64) (int64, *int64, *int64) {
	resetAt, windowStart, windowEnd := upstreamExplicitRateLimitState(apiErr, now)
	if resetAt > now {
		return resetAt, windowStart, windowEnd
	}
	return now + int64(time.Minute.Seconds()), nil, nil
}

// upstreamExplicitRateLimitState returns only a reset supplied by the
// provider. The caller may choose a narrower operator rule when no provider
// reset is present; the generic rate-limit path uses a one-minute fallback in
// upstreamRateLimitState instead.
func upstreamExplicitRateLimitState(apiErr *types.NewAPIError, now int64) (int64, *int64, *int64) {
	if apiErr == nil {
		return 0, nil, nil
	}
	header, body := apiErr.UpstreamResponse()
	if resetAt, windowStart, windowEnd := codexRateLimitState(header, now); resetAt > now {
		return resetAt, windowStart, windowEnd
	}
	if resetAt := openAIRateLimitResetFromBody(body, now); resetAt > now {
		return resetAt, nil, nil
	}
	if resetAt := genericRateLimitResetFromHeader(header, now); resetAt > now {
		return resetAt, nil, nil
	}
	return 0, nil, nil
}

func codexRateLimitState(header http.Header, now int64) (int64, *int64, *int64) {
	if len(header) == 0 {
		return 0, nil, nil
	}
	windows := []codexRateLimitWindow{
		parseCodexRateLimitWindow(header, "x-codex-primary"),
		parseCodexRateLimitWindow(header, "x-codex-secondary"),
	}
	available := make([]codexRateLimitWindow, 0, len(windows))
	exhausted := make([]codexRateLimitWindow, 0, len(windows))
	for _, window := range windows {
		if window.resetSeconds <= 0 {
			continue
		}
		available = append(available, window)
		if window.hasUsed && window.usedPercent >= 100 {
			exhausted = append(exhausted, window)
		}
	}
	selected := available
	if len(exhausted) > 0 {
		selected = exhausted
	}
	var resetWindow codexRateLimitWindow
	for _, window := range selected {
		// The seven-day usage window is informational for account usage. It is
		// not an account cooldown trigger: a 429 must not make an otherwise
		// usable OAuth account unavailable for the remainder of that window.
		if window.windowMinutes >= codexSevenDayWindowMinutes {
			continue
		}
		if resetWindow.resetSeconds == 0 || window.windowMinutes > resetWindow.windowMinutes ||
			(window.windowMinutes == resetWindow.windowMinutes && window.resetSeconds > resetWindow.resetSeconds) {
			resetWindow = window
		}
	}
	if resetWindow.resetSeconds <= 0 {
		return 0, nil, nil
	}
	resetAt := now + resetWindow.resetSeconds
	var sessionWindow *codexRateLimitWindow
	for i := range available {
		window := &available[i]
		if window.windowMinutes <= 0 || window.windowMinutes >= codexSevenDayWindowMinutes {
			continue
		}
		if sessionWindow == nil || window.windowMinutes < sessionWindow.windowMinutes {
			sessionWindow = window
		}
	}
	if sessionWindow == nil {
		return resetAt, nil, nil
	}
	windowEnd := now + sessionWindow.resetSeconds
	windowStart := windowEnd - sessionWindow.windowMinutes*60
	return resetAt, &windowStart, &windowEnd
}

func isCodexSevenDayWindow(start, end int64) bool {
	return start > 0 && end > start && end-start >= codexSevenDayWindowMinutes*60
}

func parseCodexRateLimitWindow(header http.Header, prefix string) codexRateLimitWindow {
	window := codexRateLimitWindow{}
	if value, err := strconv.ParseFloat(strings.TrimSpace(header.Get(prefix+"-used-percent")), 64); err == nil {
		window.usedPercent = value
		window.hasUsed = true
	}
	window.resetSeconds, _ = strconv.ParseInt(strings.TrimSpace(header.Get(prefix+"-reset-after-seconds")), 10, 64)
	window.windowMinutes, _ = strconv.ParseInt(strings.TrimSpace(header.Get(prefix+"-window-minutes")), 10, 64)
	return window
}

func openAIRateLimitResetFromBody(body []byte, now int64) int64 {
	if len(body) == 0 {
		return 0
	}
	var envelope struct {
		Error struct {
			ResetsAt        any `json:"resets_at"`
			ResetsInSeconds any `json:"resets_in_seconds"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &envelope); err != nil {
		return 0
	}
	if resetAt := numericTimestamp(envelope.Error.ResetsAt); resetAt > now {
		return resetAt
	}
	if seconds := numericTimestamp(envelope.Error.ResetsInSeconds); seconds > 0 {
		return now + seconds
	}
	return 0
}

func numericTimestamp(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		return 0
	}
}

func genericRateLimitResetFromHeader(header http.Header, now int64) int64 {
	if len(header) == 0 {
		return 0
	}
	if retryAfter := strings.TrimSpace(header.Get("Retry-After")); retryAfter != "" {
		if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds > 0 {
			return now + seconds
		}
		if parsed, err := http.ParseTime(retryAfter); err == nil && parsed.Unix() > now {
			return parsed.Unix()
		}
	}
	var longest time.Duration
	for _, name := range []string{"x-ratelimit-reset-requests", "x-ratelimit-reset-tokens"} {
		value := strings.TrimSpace(header.Get(name))
		if value == "" {
			continue
		}
		duration, err := time.ParseDuration(value)
		if err != nil {
			if seconds, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil {
				duration = time.Duration(seconds) * time.Second
			}
		}
		if duration > longest {
			longest = duration
		}
	}
	if longest > 0 {
		return now + int64(longest.Seconds())
	}
	return 0
}

func RecordUpstreamAccountRateLimitHeadersAsync(accountId int, header http.Header) {
	if accountId <= 0 || !hasCodexRateLimitHeaders(header) {
		return
	}
	cloned := header.Clone()
	gopool.Go(func() {
		recordUpstreamAccountRateLimitHeaders(accountId, cloned)
	})
}

func recordUpstreamAccountRateLimitHeaders(accountId int, header http.Header) {
	now := common.GetTimestamp()
	resetAt, windowStart, windowEnd := codexRateLimitState(header, now)
	updates := map[string]any{"updated_at": now}
	exhausted := codexRateLimitExhausted(header)
	if windowStart != nil && windowEnd != nil {
		updates["session_window_start"] = *windowStart
		updates["session_window_end"] = *windowEnd
		updates["session_window_status"] = "active"
		if exhausted {
			updates["session_window_status"] = "rejected"
		}
	} else {
		updates["session_window_start"] = nil
		updates["session_window_end"] = nil
		updates["session_window_status"] = ""
	}
	if exhausted && resetAt > now {
		updates["rate_limited_at"] = now
		updates["rate_limit_reset_at"] = resetAt
	} else {
		updates["rate_limited_at"] = nil
		updates["rate_limit_reset_at"] = nil
	}
	_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(updates).Error
}

func hasCodexRateLimitHeaders(header http.Header) bool {
	return len(header) > 0 && (header.Get("x-codex-primary-reset-after-seconds") != "" || header.Get("x-codex-secondary-reset-after-seconds") != "")
}

func codexRateLimitExhausted(header http.Header) bool {
	for _, prefix := range []string{"x-codex-primary", "x-codex-secondary"} {
		window := parseCodexRateLimitWindow(header, prefix)
		if window.windowMinutes >= codexSevenDayWindowMinutes {
			continue
		}
		if window.hasUsed && window.usedPercent >= 100 {
			return true
		}
	}
	return false
}
