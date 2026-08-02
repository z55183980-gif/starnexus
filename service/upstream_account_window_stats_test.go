package service

import (
	"testing"
	"time"

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
