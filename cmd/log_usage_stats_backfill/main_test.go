package main

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBackfillUpdatesHistoricalRowsInBatches(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	originalLogDB := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = originalLogDB })

	logs := []model.Log{
		{Type: model.LogTypeConsume, Other: `{"cache_tokens":1500,"group_ratio":0.5,"billing_mode":"tiered_expr","billing_source":"subscription"}`},
		{Type: model.LogTypeConsume, Other: `{"usage_semantic":"anthropic","cache_creation_tokens_5m":100,"cache_creation_tokens_1h":200}`},
		{Type: model.LogTypeConsume, Other: `{not-json`},
		{Type: model.LogTypeError, Other: `{"cache_tokens":999}`},
	}
	// These represent rows written before the normalized columns existed.
	require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&logs).Error)

	require.NoError(t, backfill(options{Mode: "backfill", BatchSize: 2}))

	var got []model.Log
	require.NoError(t, db.Order("id ASC").Find(&got).Error)
	require.Len(t, got, 4)
	require.Equal(t, int8(1), got[0].UsageStatsReady)
	require.Equal(t, int64(1500), got[0].UsageCacheReadTokens)
	require.InDelta(t, 0.5, got[0].UsageGroupRatio, 0.000001)
	require.Equal(t, "tiered_expr", got[0].UsageBillingMode)
	require.Equal(t, "subscription", got[0].UsageBillingSource)
	require.Equal(t, int8(1), got[1].UsageIsAnthropic)
	require.Equal(t, int64(300), got[1].UsageCacheCreationTokens)
	require.Equal(t, int8(1), got[2].UsageStatsReady)
	require.Zero(t, got[2].UsageCacheReadTokens)
	// All log types are marked ready so an unfiltered summary cannot silently
	// mix normalized and historical rows.
	require.Equal(t, int8(1), got[3].UsageStatsReady)
	require.Equal(t, int64(999), got[3].UsageCacheReadTokens)

	var pending int64
	require.NoError(t, pendingQuery(options{}).Count(&pending).Error)
	require.Zero(t, pending)
	// A resumed run is idempotent.
	require.NoError(t, backfill(options{Mode: "backfill", BatchSize: 2}))
}

func TestRunRejectsBatchLargerThanPortableParameterLimit(t *testing.T) {
	err := run(options{Mode: "plan", BatchSize: 2001})
	require.EqualError(t, err, "batch-size must be between 1 and 2000")
}
