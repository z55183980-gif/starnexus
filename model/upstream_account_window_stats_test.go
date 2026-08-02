package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGetUpstreamAccountWindowStats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Log{}))

	originalLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = originalLogDB })

	logs := []Log{
		{Type: LogTypeConsume, CreatedAt: 100, UpstreamAccountId: 7, PromptTokens: 10, CompletionTokens: 5, AccountCost: 1.25, UserCost: 2.5},
		{Type: LogTypeConsume, CreatedAt: 110, UpstreamAccountId: 7, PromptTokens: 20, CompletionTokens: 7, AccountCost: 3.5, UserCost: 4.75},
		{Type: LogTypeConsume, CreatedAt: 90, UpstreamAccountId: 7, PromptTokens: 100, CompletionTokens: 100, AccountCost: 10, UserCost: 10},
		{Type: LogTypeConsume, CreatedAt: 120, UpstreamAccountId: 8, PromptTokens: 100, CompletionTokens: 100, AccountCost: 10, UserCost: 10},
		{Type: LogTypeError, CreatedAt: 120, UpstreamAccountId: 7, PromptTokens: 100, CompletionTokens: 100, AccountCost: 10, UserCost: 10},
	}
	require.NoError(t, db.Create(&logs).Error)

	stats, err := GetUpstreamAccountWindowStats(7, 100)
	require.NoError(t, err)
	require.Equal(t, int64(2), stats.Requests)
	require.Equal(t, int64(42), stats.Tokens)
	require.InDelta(t, 4.75, stats.Cost, 0.000001)
	require.InDelta(t, 7.25, stats.UserCost, 0.000001)

	batch, err := GetUpstreamAccountWindowStatsBatch([]int{7, 8, 9}, 100)
	require.NoError(t, err)
	require.Equal(t, int64(2), batch[7].Requests)
	require.Equal(t, int64(42), batch[7].Tokens)
	require.InDelta(t, 4.75, batch[7].Cost, 0.000001)
	require.InDelta(t, 7.25, batch[7].UserCost, 0.000001)
	require.Equal(t, int64(1), batch[8].Requests)
	require.Equal(t, int64(200), batch[8].Tokens)
	require.InDelta(t, 10.0, batch[8].Cost, 0.000001)
	require.Equal(t, int64(0), batch[9].Requests)
	require.Equal(t, int64(0), batch[9].Tokens)
}
