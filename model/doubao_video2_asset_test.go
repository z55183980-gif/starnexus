package model

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDoubaoVideo2AssetTestDB(t *testing.T) {
	previous := DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-video2-assets.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&DoubaoVideo2Asset{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	t.Cleanup(func() {
		DB = previous
		_ = sqlDB.Close()
	})
}

func TestClaimDoubaoVideo2AssetIsIndependentAndExclusive(t *testing.T) {
	setupDoubaoVideo2AssetTestDB(t)
	first, owner, err := ClaimDoubaoVideo2Asset(&DoubaoVideo2Asset{CacheKey: "doubao2-key"}, "node-a", time.Minute)
	require.NoError(t, err)
	require.True(t, owner)
	require.NotZero(t, first.ID)

	second, owner, err := ClaimDoubaoVideo2Asset(&DoubaoVideo2Asset{CacheKey: "doubao2-key"}, "node-b", time.Minute)
	require.NoError(t, err)
	require.False(t, owner)
	require.Equal(t, first.ID, second.ID)
}

func TestClaimDoubaoVideo2AssetReusesActiveRecord(t *testing.T) {
	setupDoubaoVideo2AssetTestDB(t)
	record, owner, err := ClaimDoubaoVideo2Asset(&DoubaoVideo2Asset{CacheKey: "doubao2-active"}, "node-a", time.Minute)
	require.NoError(t, err)
	require.True(t, owner)
	won, err := UpdateDoubaoVideo2AssetOwned(record.ID, "node-a", map[string]any{
		"status": DoubaoVideo2AssetStatusActive, "asset_id": "asset-62", "lease_owner": "", "lease_until": 0,
	})
	require.NoError(t, err)
	require.True(t, won)

	reused, owner, err := ClaimDoubaoVideo2Asset(&DoubaoVideo2Asset{CacheKey: "doubao2-active"}, "node-b", time.Minute)
	require.NoError(t, err)
	require.False(t, owner)
	require.Equal(t, "asset-62", reused.AssetID)
}

func TestClaimDoubaoVideo2AssetNeverRecreatesKnownProcessingAsset(t *testing.T) {
	setupDoubaoVideo2AssetTestDB(t)
	record, owner, err := ClaimDoubaoVideo2Asset(&DoubaoVideo2Asset{CacheKey: "doubao2-processing"}, "node-a", time.Minute)
	require.NoError(t, err)
	require.True(t, owner)
	_, err = UpdateDoubaoVideo2AssetOwned(record.ID, "node-a", map[string]any{
		"status": DoubaoVideo2AssetStatusProcessing, "asset_id": "asset-processing", "lease_owner": "", "lease_until": 0,
	})
	require.NoError(t, err)

	reused, owner, err := ClaimDoubaoVideo2Asset(&DoubaoVideo2Asset{CacheKey: "doubao2-processing"}, "node-b", time.Minute)
	require.NoError(t, err)
	require.False(t, owner)
	require.Equal(t, DoubaoVideo2AssetStatusProcessing, reused.Status)
	require.Equal(t, "asset-processing", reused.AssetID)
}
