package model

import (
	"testing"

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
