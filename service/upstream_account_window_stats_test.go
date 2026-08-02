package service

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAttachOpenAIWindowStatsCreatesMissingWindows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	originalLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() {
		model.LOG_DB = originalLogDB
		upstreamWindowStatsCache.Range(func(key, _ any) bool {
			upstreamWindowStatsCache.Delete(key)
			return true
		})
	})

	now := time.Unix(2_000_000_000, 0)
	account := &model.UpstreamAccount{Id: 991234}
	logs := []model.Log{
		{Type: model.LogTypeConsume, CreatedAt: now.Add(-4 * time.Hour).Unix(), UpstreamAccountId: account.Id, PromptTokens: 10, CompletionTokens: 5, AccountCost: 1, UserCost: 2},
		{Type: model.LogTypeConsume, CreatedAt: now.Add(-6 * 24 * time.Hour).Unix(), UpstreamAccountId: account.Id, PromptTokens: 20, CompletionTokens: 7, AccountCost: 3, UserCost: 4},
	}
	require.NoError(t, db.Create(&logs).Error)

	usage := &UpstreamAccountQuotaUsage{}
	attachOpenAIWindowStats(account, usage, now)

	require.NotNil(t, usage.RateLimit)
	require.NotNil(t, usage.RateLimit.PrimaryWindow)
	require.NotNil(t, usage.RateLimit.SecondaryWindow)
	require.Equal(t, int64((5 * time.Hour).Seconds()), usage.RateLimit.PrimaryWindow.LimitWindowSeconds)
	require.Equal(t, int64((7 * 24 * time.Hour).Seconds()), usage.RateLimit.SecondaryWindow.LimitWindowSeconds)
	require.Equal(t, int64(1), usage.RateLimit.PrimaryWindow.WindowStats.Requests)
	require.Equal(t, int64(15), usage.RateLimit.PrimaryWindow.WindowStats.Tokens)
	require.Equal(t, int64(2), usage.RateLimit.SecondaryWindow.WindowStats.Requests)
	require.Equal(t, int64(42), usage.RateLimit.SecondaryWindow.WindowStats.Tokens)
}

func TestAttachOpenAIWindowStatsPreservesExistingWeeklyWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))

	originalLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() {
		model.LOG_DB = originalLogDB
		upstreamWindowStatsCache.Range(func(key, _ any) bool {
			upstreamWindowStatsCache.Delete(key)
			return true
		})
	})

	now := time.Unix(2_000_000_000, 0)
	weekly := &UpstreamAccountRateLimitWindow{
		UsedPercent: 18, LimitWindowSeconds: int64((7 * 24 * time.Hour).Seconds()),
		ResetAfterSeconds: int64((5 * 24 * time.Hour).Seconds()), ResetAt: now.Add(5 * 24 * time.Hour).Unix(),
	}
	usage := &UpstreamAccountQuotaUsage{RateLimit: &UpstreamAccountRateLimit{PrimaryWindow: weekly}}
	attachOpenAIWindowStats(&model.UpstreamAccount{Id: 991235}, usage, now)

	require.NotNil(t, usage.RateLimit.PrimaryWindow)
	require.Equal(t, int64((5 * time.Hour).Seconds()), usage.RateLimit.PrimaryWindow.LimitWindowSeconds)
	require.Same(t, weekly, usage.RateLimit.SecondaryWindow)
	require.Equal(t, float64(18), usage.RateLimit.SecondaryWindow.UsedPercent)
}

func TestOpenAICodexWindowsFromHeaderNormalizesByDuration(t *testing.T) {
	now := time.Unix(2_000_000_000, 0)
	header := http.Header{}
	header.Set("x-codex-primary-used-percent", "18")
	header.Set("x-codex-primary-reset-after-seconds", "475200")
	header.Set("x-codex-primary-window-minutes", "10080")
	header.Set("x-codex-secondary-used-percent", "3")
	header.Set("x-codex-secondary-reset-after-seconds", "14400")
	header.Set("x-codex-secondary-window-minutes", "300")

	fiveHour, sevenDay := openAICodexWindowsFromHeader(header, now)
	require.NotNil(t, fiveHour)
	require.NotNil(t, sevenDay)
	require.Equal(t, float64(3), fiveHour.UsedPercent)
	require.Equal(t, int64((5 * time.Hour).Seconds()), fiveHour.LimitWindowSeconds)
	require.Equal(t, float64(18), sevenDay.UsedPercent)
	require.Equal(t, int64((7 * 24 * time.Hour).Seconds()), sevenDay.LimitWindowSeconds)
	require.Equal(t, now.Unix()+475200, sevenDay.ResetAt)
}

func TestOpenAICodexWindowsFromExtraKeepsRFC3339ResetTime(t *testing.T) {
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	resetAt := now.Add(5*24*time.Hour + 12*time.Hour)
	extra, err := common.Marshal(map[string]any{
		"codex_7d_used_percent":        18,
		"codex_7d_window_minutes":      10080,
		"codex_7d_reset_at":            resetAt.Format(time.RFC3339),
		"codex_usage_updated_at":       now.Format(time.RFC3339),
		"codex_7d_reset_after_seconds": int64(resetAt.Sub(now).Seconds()),
	})
	require.NoError(t, err)

	fiveHour, sevenDay := openAICodexWindowsFromExtra(string(extra), now)
	require.Nil(t, fiveHour)
	require.NotNil(t, sevenDay)
	require.Equal(t, float64(18), sevenDay.UsedPercent)
	require.Equal(t, resetAt.Unix(), sevenDay.ResetAt)
	require.False(t, openAICodexSnapshotStale(string(extra), now.Add(time.Minute)))
}

func TestShouldProbeOpenAICodexUsageWindowsSkipsCompleteUpstreamUsage(t *testing.T) {
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	fiveHour := &UpstreamAccountRateLimitWindow{UsedPercent: 3, LimitWindowSeconds: int64((5 * time.Hour).Seconds())}
	sevenDay := &UpstreamAccountRateLimitWindow{UsedPercent: 18, LimitWindowSeconds: int64((7 * 24 * time.Hour).Seconds())}

	require.False(t, shouldProbeOpenAICodexUsageWindows(false, fiveHour, sevenDay, fiveHour, sevenDay, "{}", now))
	require.True(t, shouldProbeOpenAICodexUsageWindows(true, fiveHour, sevenDay, fiveHour, sevenDay, "{}", now))
}

func TestRefreshOpenAICodexUsageWindowsDoesNotGenerateWhenUpstreamUsageIsComplete(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	fiveHour := &UpstreamAccountRateLimitWindow{UsedPercent: 3, LimitWindowSeconds: int64((5 * time.Hour).Seconds())}
	sevenDay := &UpstreamAccountRateLimitWindow{UsedPercent: 18, LimitWindowSeconds: int64((7 * 24 * time.Hour).Seconds())}
	usage := &UpstreamAccountQuotaUsage{RateLimit: &UpstreamAccountRateLimit{PrimaryWindow: fiveHour, SecondaryWindow: sevenDay}}
	probeAccount := model.UpstreamAccount{Id: 991237, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth, Extra: "{}"}
	call := &upstreamAccountQuotaCall{
		client: server.Client(), account: probeAccount,
		credentials: map[string]any{"access_token": "token", "account_id": "chatgpt-account", "base_url": server.URL},
	}

	refreshOpenAICodexUsageWindows(t.Context(), &model.UpstreamAccount{Extra: "{}"}, call, usage, false, now)
	require.Zero(t, requests.Load())
}

func TestShouldProbeOpenAICodexUsageWindowsUsesStoredSnapshotFreshness(t *testing.T) {
	now := time.Date(2026, 8, 2, 15, 0, 0, 0, time.UTC)
	fiveHour := &UpstreamAccountRateLimitWindow{UsedPercent: 3, LimitWindowSeconds: int64((5 * time.Hour).Seconds())}
	sevenDay := &UpstreamAccountRateLimitWindow{UsedPercent: 18, LimitWindowSeconds: int64((7 * 24 * time.Hour).Seconds())}
	freshExtra, err := common.Marshal(map[string]any{"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339)})
	require.NoError(t, err)
	staleExtra, err := common.Marshal(map[string]any{"codex_usage_updated_at": now.Add(-10 * time.Minute).Format(time.RFC3339)})
	require.NoError(t, err)

	require.False(t, shouldProbeOpenAICodexUsageWindows(false, fiveHour, nil, fiveHour, sevenDay, string(freshExtra), now))
	require.True(t, shouldProbeOpenAICodexUsageWindows(false, fiveHour, nil, fiveHour, sevenDay, string(staleExtra), now))
	require.True(t, shouldProbeOpenAICodexUsageWindows(false, fiveHour, nil, fiveHour, nil, string(freshExtra), now))
}

func TestProbeOpenAICodexUsageWindowsReadsResponseHeaders(t *testing.T) {
	requestSeen := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestSeen <- request.Clone(request.Context())
		writer.Header().Set("x-codex-primary-used-percent", "22")
		writer.Header().Set("x-codex-primary-reset-after-seconds", "432000")
		writer.Header().Set("x-codex-primary-window-minutes", "10080")
		writer.Header().Set("x-codex-secondary-used-percent", "4")
		writer.Header().Set("x-codex-secondary-reset-after-seconds", "10800")
		writer.Header().Set("x-codex-secondary-window-minutes", "300")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	account := model.UpstreamAccount{Id: 991236, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth, Extra: "{}"}
	credentials := map[string]any{"access_token": "token", "account_id": "chatgpt-account", "base_url": server.URL}
	call := &upstreamAccountQuotaCall{client: server.Client(), account: account, credentials: credentials}
	now := time.Unix(2_000_000_000, 0)
	fiveHour, sevenDay, err := probeOpenAICodexUsageWindows(t.Context(), call, now)
	require.NoError(t, err)
	require.Equal(t, float64(4), fiveHour.UsedPercent)
	require.Equal(t, float64(22), sevenDay.UsedPercent)

	request := <-requestSeen
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/backend-api/codex/responses", request.URL.Path)
	require.Equal(t, "Bearer token", request.Header.Get("Authorization"))
	require.Equal(t, "chatgpt-account", request.Header.Get("chatgpt-account-id"))
}
