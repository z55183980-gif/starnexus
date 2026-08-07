package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
)

func TestParseUpstreamAccountResetCredits(t *testing.T) {
	payload := map[string]any{
		"availableCount": float64(3),
		"rate_limit_reset_credits": []any{
			map[string]any{"resetType": "codex_rate_limits", "status": "available", "expiresAt": "2026-08-01T00:00:00Z"},
			map[string]any{"resetType": "other", "status": "available"},
		},
	}
	credits := parseUpstreamAccountResetCredits(payload)
	if credits == nil {
		t.Fatal("expected reset credit details")
	}
	if credits.AvailableCount != 3 {
		t.Fatalf("expected available count 3, got %d", credits.AvailableCount)
	}
	if len(credits.Credits) != 1 || credits.Credits[0].ExpiresAt != "2026-08-01T00:00:00Z" {
		t.Fatalf("unexpected credit details: %+v", credits.Credits)
	}
}

func TestParseUpstreamAccountResetCreditsArray(t *testing.T) {
	payload := []any{
		map[string]any{"reset_type": "codex_rate_limits", "status": "available"},
		map[string]any{"reset_type": "codex_rate_limits", "status": "redeemed"},
	}
	credits := parseUpstreamAccountResetCredits(payload)
	if credits == nil || credits.AvailableCount != 1 {
		t.Fatalf("unexpected reset credit count: %+v", credits)
	}
}

func TestCachedUpstreamAccountQuotaHonorsCreditRequirementAndTTL(t *testing.T) {
	const accountId = 987654
	const credentialVersion int64 = 3
	t.Cleanup(func() { upstreamAccountQuotaCache.Delete(accountId) })
	usage := UpstreamAccountQuotaUsage{AccountId: "acct-test", FetchedAt: time.Now().Unix()}
	upstreamAccountQuotaCache.Store(accountId, upstreamAccountQuotaCacheEntry{
		usage: usage, cachedAt: time.Now(), includeCredits: false, credentialVersion: credentialVersion,
	})
	if cached := cachedUpstreamAccountQuota(accountId, credentialVersion, false); cached == nil || cached.AccountId != usage.AccountId {
		t.Fatalf("expected cached usage, got %+v", cached)
	}
	if cached := cachedUpstreamAccountQuota(accountId, credentialVersion+1, false); cached != nil {
		t.Fatalf("expected cache miss after credential rotation, got %+v", cached)
	}
	upstreamAccountQuotaCache.Store(accountId, upstreamAccountQuotaCacheEntry{
		usage: usage, cachedAt: time.Now(), includeCredits: false, credentialVersion: credentialVersion,
	})
	if cached := cachedUpstreamAccountQuota(accountId, credentialVersion, true); cached != nil {
		t.Fatalf("expected cache miss when credit details are required, got %+v", cached)
	}

	upstreamAccountQuotaCache.Store(accountId, upstreamAccountQuotaCacheEntry{
		usage: usage, cachedAt: time.Now().Add(-upstreamAccountQuotaCacheTTL), includeCredits: true, credentialVersion: credentialVersion,
	})
	if cached := cachedUpstreamAccountQuota(accountId, credentialVersion, true); cached != nil {
		t.Fatalf("expected expired cache miss, got %+v", cached)
	}
}

func TestOpenAIQuotaUsageFromExtraBuildsCachedWindows(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	updatedAt := now.Add(-30 * time.Minute).UTC().Format(time.RFC3339)
	account := &model.UpstreamAccount{
		Extra: `{"codex_5h_used_percent":87.5,"codex_5h_reset_after_seconds":3600,"codex_5h_window_minutes":300,"codex_5h_reset_at":"` +
			now.Add(time.Hour).UTC().Format(time.RFC3339) +
			`","codex_7d_used_percent":42,"codex_7d_window_minutes":10080,"codex_usage_updated_at":"` + updatedAt + `"}`,
	}
	usage := openAIQuotaUsageFromExtra(account, now)
	if usage == nil || usage.Source != "cached" {
		t.Fatalf("expected cached usage, got %+v", usage)
	}
	if usage.RateLimit == nil || usage.RateLimit.PrimaryWindow == nil || usage.RateLimit.SecondaryWindow == nil {
		t.Fatalf("expected both windows, got %+v", usage.RateLimit)
	}
	if usage.RateLimit.PrimaryWindow.UsedPercent != 87.5 {
		t.Fatalf("unexpected 5h percent: %+v", usage.RateLimit.PrimaryWindow)
	}
	if usage.FetchedAt != now.Add(-30*time.Minute).Unix() {
		t.Fatalf("unexpected fetched_at: %d", usage.FetchedAt)
	}
}

func TestOpenAIQuotaUsageFallbackPrefersExtraOverStaleMemory(t *testing.T) {
	const accountId = 424242
	const credentialVersion int64 = 1
	t.Cleanup(func() { upstreamAccountQuotaCache.Delete(accountId) })
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	upstreamAccountQuotaCache.Store(accountId, upstreamAccountQuotaCacheEntry{
		usage: UpstreamAccountQuotaUsage{
			AccountId: "memory",
			Source:    "active",
			RateLimit: &UpstreamAccountRateLimit{
				PrimaryWindow: &UpstreamAccountRateLimitWindow{UsedPercent: 11},
			},
			FetchedAt: now.Unix(),
		},
		cachedAt:          now.Add(-time.Hour),
		includeCredits:    false,
		credentialVersion: credentialVersion,
	})
	account := &model.UpstreamAccount{
		Extra: `{"codex_5h_used_percent":55,"codex_5h_window_minutes":300,"codex_usage_updated_at":"` +
			now.Add(-10*time.Minute).UTC().Format(time.RFC3339) + `"}`,
	}
	usage := openAIQuotaUsageFallback(account, accountId, credentialVersion, false, now)
	if usage == nil || usage.Source != "cached" || usage.RateLimit == nil || usage.RateLimit.PrimaryWindow == nil {
		t.Fatalf("expected Extra fallback, got %+v", usage)
	}
	if usage.RateLimit.PrimaryWindow.UsedPercent != 55 {
		t.Fatalf("expected Extra percent 55, got %+v", usage.RateLimit.PrimaryWindow)
	}
}

func TestOpenAIQuotaUsageFallbackUsesStaleMemoryWithoutExtra(t *testing.T) {
	const accountId = 434343
	const credentialVersion int64 = 2
	t.Cleanup(func() { upstreamAccountQuotaCache.Delete(accountId) })
	now := time.Now()
	upstreamAccountQuotaCache.Store(accountId, upstreamAccountQuotaCacheEntry{
		usage: UpstreamAccountQuotaUsage{
			AccountId: "memory-only",
			Source:    "active",
			RateLimit: &UpstreamAccountRateLimit{
				PrimaryWindow: &UpstreamAccountRateLimitWindow{UsedPercent: 33},
			},
			FetchedAt: now.Unix(),
		},
		cachedAt:          now.Add(-time.Hour),
		includeCredits:    false,
		credentialVersion: credentialVersion,
	})
	usage := openAIQuotaUsageFallback(&model.UpstreamAccount{Extra: "{}"}, accountId, credentialVersion, false, now)
	if usage == nil || usage.Source != "cached" || usage.AccountId != "memory-only" {
		t.Fatalf("expected stale memory fallback, got %+v", usage)
	}
}
