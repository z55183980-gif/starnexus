package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAggregateContentModerationKeyUsage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ContentModerationKeyUsage{}))
	originalLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = originalLogDB })

	rows := []ContentModerationKeyUsage{
		{KeyHash: "key-a", KeyMask: "****aaaa", PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, TokenUsageAvailable: true, BillingUSD: 0.01, BillingAvailable: true, CreatedAt: 100},
		{KeyHash: "key-a", KeyMask: "****aaaa", PromptTokens: 20, CompletionTokens: 3, TotalTokens: 23, TokenUsageAvailable: true, BillingUSD: 0.02, BillingAvailable: true, CreatedAt: 200},
		{KeyHash: "key-b", KeyMask: "****bbbb", CreatedAt: 200},
	}
	require.NoError(t, db.Create(&rows).Error)

	aggregates, err := AggregateContentModerationKeyUsage([]string{"key-a", "key-b"}, 150, 250)
	require.NoError(t, err)
	require.Equal(t, int64(1), aggregates["key-a"].RequestCount)
	require.Equal(t, int64(23), aggregates["key-a"].TotalTokens)
	require.InDelta(t, 0.02, aggregates["key-a"].BillingUSD, 0.0000001)
	require.Equal(t, int64(1), aggregates["key-a"].TokenUsageAvailableCount)
	require.Equal(t, int64(1), aggregates["key-a"].BillingAvailableCount)
	require.Equal(t, int64(1), aggregates["key-b"].RequestCount)
	require.Zero(t, aggregates["key-b"].TokenUsageAvailableCount)
}
