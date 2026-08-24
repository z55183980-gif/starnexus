package model

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDoubaoVideo2UserMaterialTestDB(t *testing.T) {
	previous := DB
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/doubao-video2-user-material.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &DoubaoVideo2UserAssetGroup{}, &DoubaoVideo2UserAsset{}, &DoubaoVideo2UserMedia{}, &DoubaoVideo2AccessKey{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	DB = db
	t.Cleanup(func() {
		DB = previous
		_ = sqlDB.Close()
	})
}

func TestDoubaoVideo2MaterialStorageLimitAndDeletion(t *testing.T) {
	setupDoubaoVideo2UserMaterialTestDB(t)
	user := &User{Username: "material-user", Password: "not-a-real-password", DoubaoVideo2MaterialLimitBytes: 10}
	require.NoError(t, DB.Create(user).Error)
	group := &DoubaoVideo2UserAssetGroup{
		UserID: user.Id, ChannelID: 62, ProviderGroupID: "group-1", Name: "group", GroupType: "AIGC", Status: DoubaoVideo2UserMaterialStatusActive,
	}
	require.NoError(t, CreateDoubaoVideo2UserAssetGroup(group))

	require.NoError(t, ReserveDoubaoVideo2Storage(user.Id, 7))
	require.ErrorIs(t, ReserveDoubaoVideo2Storage(user.Id, 4), ErrDoubaoVideo2MaterialStorageLimit)
	usage, err := GetDoubaoVideo2StorageUsage(user.Id)
	require.NoError(t, err)
	require.Equal(t, DoubaoVideo2StorageUsage{UsedBytes: 7, LimitBytes: 10, RemainingBytes: 3}, *usage)

	media := &DoubaoVideo2UserMedia{
		UserID: user.Id, TokenHash: strings.Repeat("a", 64), ObjectKey: "doubao-video2-material-library/1/object.png", SizeBytes: 7, ContentType: "image/png",
	}
	require.NoError(t, CreateDoubaoVideo2UserMedia(media))
	asset := &DoubaoVideo2UserAsset{
		UserID: user.Id, AssetGroupID: group.ID, ChannelID: 62, ProviderAssetID: "asset-1", Name: "asset", AssetType: "Image", Status: DoubaoVideo2UserMaterialStatusActive,
	}
	require.NoError(t, CreateDoubaoVideo2UserAssetWithMedia(asset, media.ID))
	require.NoError(t, DeleteDoubaoVideo2UserAssetAndMedia(asset.ID, user.Id))

	usage, err = GetDoubaoVideo2StorageUsage(user.Id)
	require.NoError(t, err)
	require.Zero(t, usage.UsedBytes)
	updatedGroup, err := GetDoubaoVideo2UserAssetGroup(group.ID, user.Id)
	require.NoError(t, err)
	require.Zero(t, updatedGroup.AssetCount)
	_, err = GetDoubaoVideo2UserMediaByAssetID(asset.ID, user.Id)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
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
