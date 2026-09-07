package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaDataQueryIncludesPendingAndFlushesWithoutDoubleCounting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&QuotaData{}))
	require.True(t, db.Migrator().HasIndex(&QuotaData{}, "idx_qdt_user_created"))
	require.True(t, db.Migrator().HasIndex(&QuotaData{}, "idx_qdt_created_model"))
	require.True(t, db.Migrator().HasIndex(&QuotaData{}, "idx_qdt_username_created"))

	originalDB := DB
	CacheQuotaDataLock.Lock()
	originalCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = originalCache
		CacheQuotaDataLock.Unlock()
	})

	const bucket = int64(1_699_999_200)
	stored := QuotaData{
		UserID: 7, Username: "alice", ModelName: "gpt-test", CreatedAt: bucket,
		Count: 2, Quota: 20, TokenUsed: 200,
	}
	require.NoError(t, db.Create(&stored).Error)

	LogQuotaData(7, "alice", "gpt-test", 5, bucket+10, 50)
	rows, err := GetQuotaDataByUserId(7, bucket, bucket)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 3, rows[0].Count)
	require.Equal(t, 25, rows[0].Quota)
	require.Equal(t, 250, rows[0].TokenUsed)

	require.NoError(t, flushQuotaDataCache())
	rows, err = GetQuotaDataByUserId(7, bucket, bucket)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 3, rows[0].Count)
	require.Equal(t, 25, rows[0].Quota)
	require.Equal(t, 250, rows[0].TokenUsed)
}

func TestHybridQuotaDataRebuildsCurrentBucketWithoutDoubleCounting(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&QuotaData{}, &Log{}))

	originalDB := DB
	originalLogDB := LOG_DB
	originalExportEnabled := common.DataExportEnabled
	DB = db
	LOG_DB = db
	common.DataExportEnabled = true
	t.Cleanup(func() {
		DB = originalDB
		LOG_DB = originalLogDB
		common.DataExportEnabled = originalExportEnabled
	})

	now := time.Now().Unix()
	currentBucket := now - now%quotaDataBucketSeconds
	require.NoError(t, db.Create(&QuotaData{
		UserID: 7, Username: "alice", ModelName: "gpt-test", CreatedAt: currentBucket - quotaDataBucketSeconds,
		Count: 1, Quota: 10, TokenUsed: 100,
	}).Error)
	// This row represents an already-flushed partial current bucket. The hybrid
	// query must replace it with the complete raw-log aggregation below.
	require.NoError(t, db.Create(&QuotaData{
		UserID: 7, Username: "alice", ModelName: "gpt-test", CreatedAt: currentBucket,
		Count: 1, Quota: 20, TokenUsed: 200,
	}).Error)
	require.NoError(t, db.Create(&Log{
		UserId: 7, Username: "alice", ModelName: "gpt-test", Type: LogTypeConsume,
		CreatedAt: currentBucket + 10, Quota: 30, PromptTokens: 120, CompletionTokens: 80,
	}).Error)
	require.NoError(t, db.Create(&Log{
		UserId: 7, Username: "alice", ModelName: "gpt-test", Type: LogTypeConsume,
		CreatedAt: currentBucket + 20, Quota: 40, PromptTokens: 150, CompletionTokens: 50,
	}).Error)

	rows, err := GetAllQuotaDatesHybridContext(context.Background(), currentBucket-quotaDataBucketSeconds, now, "")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, 10, rows[0].Quota)
	require.Equal(t, 70, rows[1].Quota)
	require.Equal(t, 400, rows[1].TokenUsed)
	require.Equal(t, 2, rows[1].Count)
}

func TestQuotaDataFlushRestoresPendingDataOnFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	originalDB := DB
	CacheQuotaDataLock.Lock()
	originalCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = originalCache
		CacheQuotaDataLock.Unlock()
	})

	LogQuotaData(8, "bob", "gpt-test", 9, 1_700_000_410, 90)
	require.Error(t, flushQuotaDataCache())

	CacheQuotaDataLock.Lock()
	require.Len(t, CacheQuotaData, 1)
	CacheQuotaDataLock.Unlock()
}

func TestMergePendingQuotaDataClearsRolledBackPrimaryKey(t *testing.T) {
	originalCache := CacheQuotaData
	CacheQuotaDataLock.Lock()
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		CacheQuotaDataLock.Lock()
		CacheQuotaData = originalCache
		CacheQuotaDataLock.Unlock()
	})

	pending := map[string]*QuotaData{
		"pending": {Id: 42, UserID: 9, Count: 1},
	}
	mergePendingQuotaData(pending)

	CacheQuotaDataLock.Lock()
	restored := CacheQuotaData["pending"]
	CacheQuotaDataLock.Unlock()
	require.NotNil(t, restored)
	require.Zero(t, restored.Id)
}
