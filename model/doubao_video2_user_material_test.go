package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDoubaoVideo2UserMaterialTestDB(t *testing.T) {
	previous := DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-video2-user-material.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&DoubaoVideo2UserAssetGroup{}, &DoubaoVideo2UserAsset{}, &DoubaoVideo2AccessKey{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	t.Cleanup(func() {
		DB = previous
		_ = sqlDB.Close()
	})
}

func TestResolveDoubaoVideo2UserAssetChannelEnforcesOwnershipStatusAndChannel(t *testing.T) {
	setupDoubaoVideo2UserMaterialTestDB(t)
	assets := []*DoubaoVideo2UserAsset{
		{UserID: 7, AssetGroupID: 1, ChannelID: 62, ProviderAssetID: "owned-a", Name: "a", AssetType: "Image", Status: DoubaoVideo2UserMaterialStatusActive},
		{UserID: 7, AssetGroupID: 1, ChannelID: 62, ProviderAssetID: "owned-b", Name: "b", AssetType: "Video", Status: DoubaoVideo2UserMaterialStatusActive},
		{UserID: 8, AssetGroupID: 2, ChannelID: 62, ProviderAssetID: "foreign", Name: "foreign", AssetType: "Image", Status: DoubaoVideo2UserMaterialStatusActive},
		{UserID: 7, AssetGroupID: 1, ChannelID: 62, ProviderAssetID: "pending", Name: "pending", AssetType: "Image", Status: DoubaoVideo2UserMaterialStatusProcessing},
		{UserID: 7, AssetGroupID: 3, ChannelID: 63, ProviderAssetID: "other-channel", Name: "other", AssetType: "Image", Status: DoubaoVideo2UserMaterialStatusActive},
	}
	for _, asset := range assets {
		require.NoError(t, DB.Create(asset).Error)
	}

	channelID, err := ResolveDoubaoVideo2UserAssetChannel(7, []string{"owned-a", "owned-b"})
	require.NoError(t, err)
	require.Equal(t, 62, channelID)

	channelID, err = ResolveDoubaoVideo2UserAssetChannel(7, []string{"unknown"})
	require.NoError(t, err)
	require.Zero(t, channelID)

	_, err = ResolveDoubaoVideo2UserAssetChannel(7, []string{"foreign"})
	require.ErrorContains(t, err, "current user")
	_, err = ResolveDoubaoVideo2UserAssetChannel(7, []string{"pending"})
	require.ErrorContains(t, err, "not approved")
	_, err = ResolveDoubaoVideo2UserAssetChannel(7, []string{"owned-a", "other-channel"})
	require.ErrorContains(t, err, "different upstream channels")
}
