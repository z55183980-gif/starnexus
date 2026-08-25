package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

const (
	DoubaoVideo2DefaultUserStorageLimit int64 = 3 << 30

	DoubaoVideo2UserMaterialStatusProcessing = "Processing"
	DoubaoVideo2UserMaterialStatusActive     = "Active"
	DoubaoVideo2UserMaterialStatusRejected   = "Rejected"
	DoubaoVideo2UserMaterialStatusFailed     = "Failed"

	DoubaoVideo2AccessKeyStatusDisabled = 0
	DoubaoVideo2AccessKeyStatusActive   = 1
)

var (
	ErrDoubaoVideo2MaterialStorageLimit  = errors.New("素材库达到限额请删除已有素材或联系客服调整")
	ErrDoubaoVideo2MaterialGroupNotEmpty = errors.New("material group still contains materials")
)

// DoubaoVideo2UserAssetGroup maps one user-owned material group to the exact
// channel/account that created it upstream. Assets must remain pinned to that
// channel because provider asset IDs are account scoped.
type DoubaoVideo2UserAssetGroup struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	UserID          int    `json:"user_id" gorm:"not null;index;uniqueIndex:idx_doubao2_user_provider_group,priority:1"`
	ChannelID       int    `json:"channel_id" gorm:"not null;index;uniqueIndex:idx_doubao2_user_provider_group,priority:2"`
	ProviderGroupID string `json:"provider_group_id" gorm:"type:varchar(160);not null;index;uniqueIndex:idx_doubao2_user_provider_group,priority:3"`
	Name            string `json:"name" gorm:"type:varchar(64);not null;index"`
	Description     string `json:"description" gorm:"type:varchar(300)"`
	GroupType       string `json:"group_type" gorm:"type:varchar(32);not null;default:'AIGC'"`
	Status          string `json:"status" gorm:"type:varchar(32);not null;index"`
	AssetCount      int64  `json:"asset_count" gorm:"not null;default:0"`
	CreatedAt       int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt       int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

type DoubaoVideo2UserAsset struct {
	ID              int64  `json:"id" gorm:"primaryKey"`
	UserID          int    `json:"user_id" gorm:"not null;index;uniqueIndex:idx_doubao2_user_provider_asset,priority:1"`
	AssetGroupID    int64  `json:"asset_group_id" gorm:"not null;index"`
	ChannelID       int    `json:"channel_id" gorm:"not null;index;uniqueIndex:idx_doubao2_user_provider_asset,priority:2"`
	ProviderAssetID string `json:"provider_asset_id" gorm:"type:varchar(160);not null;index;uniqueIndex:idx_doubao2_user_provider_asset,priority:3"`
	Name            string `json:"name" gorm:"type:varchar(128);not null;index"`
	AssetType       string `json:"asset_type" gorm:"type:varchar(16);not null;index"`
	SourceURL       string `json:"-" gorm:"type:text"`
	Status          string `json:"status" gorm:"type:varchar(32);not null;index"`
	ErrorCode       string `json:"error_code" gorm:"type:varchar(128)"`
	ErrorMessage    string `json:"error_message" gorm:"type:text"`
	RequestID       string `json:"request_id" gorm:"type:varchar(160)"`
	LastSyncedAt    int64  `json:"last_synced_at" gorm:"index"`
	CreatedAt       int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt       int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

// DoubaoVideo2UserMedia keeps the original object available for exactly as
// long as the matching upstream API-key asset exists. Public access uses a
// random bearer token; only its SHA-256 digest is persisted.
type DoubaoVideo2UserMedia struct {
	ID          int64  `json:"id" gorm:"primaryKey"`
	UserID      int    `json:"user_id" gorm:"not null;index"`
	AssetID     int64  `json:"-" gorm:"not null;default:0;index"`
	TokenHash   string `json:"-" gorm:"type:char(64);not null;uniqueIndex"`
	ObjectKey   string `json:"-" gorm:"type:varchar(255);not null;uniqueIndex"`
	SizeBytes   int64  `json:"size_bytes" gorm:"not null"`
	ContentType string `json:"content_type" gorm:"type:varchar(128);not null"`
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

type DoubaoVideo2StorageUsage struct {
	UsedBytes      int64 `json:"used_bytes"`
	LimitBytes     int64 `json:"limit_bytes"`
	RemainingBytes int64 `json:"remaining_bytes"`
}

// DoubaoVideo2AccessKey uses the same public AK shape as Volcengine. The SK is
// encrypted at rest and is returned only in the create response.
type DoubaoVideo2AccessKey struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	UserID            int    `json:"user_id" gorm:"not null;index"`
	Name              string `json:"name" gorm:"type:varchar(64);not null"`
	AccessKeyID       string `json:"access_key_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	SecretCiphertext  string `json:"-" gorm:"type:text;not null"`
	SecretNonce       string `json:"-" gorm:"type:varchar(128)"`
	SecretKeyVersion  int    `json:"-" gorm:"not null;default:0"`
	CredentialVersion int64  `json:"-" gorm:"not null;default:1"`
	Status            int    `json:"status" gorm:"not null;default:1;index"`
	LastUsedAt        int64  `json:"last_used_at" gorm:"index"`
	CreatedAt         int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt         int64  `json:"updated_at" gorm:"autoUpdateTime"`
}

func ListDoubaoVideo2UserAssetGroups(userID int, keyword string) ([]*DoubaoVideo2UserAssetGroup, error) {
	groups := make([]*DoubaoVideo2UserAssetGroup, 0)
	query := DB.Where("user_id = ?", userID)
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("name LIKE ?", "%"+keyword+"%")
	}
	err := query.Order("created_at desc").Find(&groups).Error
	return groups, err
}

func GetDoubaoVideo2UserAssetGroup(id int64, userID int) (*DoubaoVideo2UserAssetGroup, error) {
	group := &DoubaoVideo2UserAssetGroup{}
	err := DB.Where("id = ? AND user_id = ?", id, userID).First(group).Error
	return group, err
}

func GetDoubaoVideo2UserAssetGroupByProviderID(providerGroupID string, userID int) (*DoubaoVideo2UserAssetGroup, error) {
	group := &DoubaoVideo2UserAssetGroup{}
	err := DB.Where("provider_group_id = ? AND user_id = ?", strings.TrimSpace(providerGroupID), userID).First(group).Error
	return group, err
}

func CreateDoubaoVideo2UserAssetGroup(group *DoubaoVideo2UserAssetGroup) error {
	return DB.Create(group).Error
}

func UpdateDoubaoVideo2UserAssetGroup(id int64, userID int, updates map[string]any) error {
	result := DB.Model(&DoubaoVideo2UserAssetGroup{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func ListDoubaoVideo2UserAssetsByGroup(userID int, groupID int64) ([]*DoubaoVideo2UserAsset, error) {
	assets := make([]*DoubaoVideo2UserAsset, 0)
	err := DB.Where("user_id = ? AND asset_group_id = ?", userID, groupID).
		Order("id asc").Find(&assets).Error
	return assets, err
}

func DeleteDoubaoVideo2UserAssetGroup(id int64, userID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var assetCount int64
		if err := tx.Model(&DoubaoVideo2UserAsset{}).
			Where("user_id = ? AND asset_group_id = ?", userID, id).
			Count(&assetCount).Error; err != nil {
			return err
		}
		if assetCount > 0 {
			return ErrDoubaoVideo2MaterialGroupNotEmpty
		}
		result := tx.Where("id = ? AND user_id = ?", id, userID).
			Delete(&DoubaoVideo2UserAssetGroup{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func ListDoubaoVideo2UserAssets(userID int, groupID int64, keyword string, offset, limit int) ([]*DoubaoVideo2UserAsset, int64, error) {
	query := DB.Model(&DoubaoVideo2UserAsset{}).Where("user_id = ?", userID)
	if groupID > 0 {
		query = query.Where("asset_group_id = ?", groupID)
	}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		query = query.Where("name LIKE ? OR provider_asset_id LIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	assets := make([]*DoubaoVideo2UserAsset, 0)
	if limit <= 0 {
		limit = 20
	}
	err := query.Order("created_at desc").Offset(offset).Limit(limit).Find(&assets).Error
	return assets, total, err
}

func GetDoubaoVideo2UserAsset(id int64, userID int) (*DoubaoVideo2UserAsset, error) {
	asset := &DoubaoVideo2UserAsset{}
	err := DB.Where("id = ? AND user_id = ?", id, userID).First(asset).Error
	return asset, err
}

func GetDoubaoVideo2UserAssetByProviderID(providerAssetID string, userID int) (*DoubaoVideo2UserAsset, error) {
	asset := &DoubaoVideo2UserAsset{}
	err := DB.Where("provider_asset_id = ? AND user_id = ?", strings.TrimSpace(providerAssetID), userID).First(asset).Error
	return asset, err
}

// ResolveDoubaoVideo2UserAssetChannel verifies known provider asset IDs belong
// to the caller and returns their shared channel. Unknown IDs are left alone so
// callers can still use assets created directly in the same upstream account.
func ResolveDoubaoVideo2UserAssetChannel(userID int, providerAssetIDs []string) (int, error) {
	if userID <= 0 || len(providerAssetIDs) == 0 {
		return 0, nil
	}
	assets := make([]*DoubaoVideo2UserAsset, 0)
	if err := DB.Where("provider_asset_id IN ?", providerAssetIDs).Find(&assets).Error; err != nil {
		return 0, err
	}
	channelID := 0
	for _, asset := range assets {
		if asset.UserID != userID {
			return 0, errors.New("material asset does not belong to the current user")
		}
		if asset.Status != DoubaoVideo2UserMaterialStatusActive {
			return 0, errors.New("material asset is not approved and active")
		}
		if channelID == 0 {
			channelID = asset.ChannelID
		} else if channelID != asset.ChannelID {
			return 0, errors.New("material assets belong to different upstream channels")
		}
	}
	return channelID, nil
}

func CreateDoubaoVideo2UserAsset(asset *DoubaoVideo2UserAsset) error {
	return CreateDoubaoVideo2UserAssetWithMedia(asset, 0)
}

func CreateDoubaoVideo2UserAssetWithMedia(asset *DoubaoVideo2UserAsset, mediaID int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(asset).Error; err != nil {
			return err
		}
		if mediaID > 0 {
			result := tx.Model(&DoubaoVideo2UserMedia{}).
				Where("id = ? AND user_id = ? AND asset_id = ?", mediaID, asset.UserID, 0).
				UpdateColumn("asset_id", asset.ID)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return gorm.ErrRecordNotFound
			}
		}
		return tx.Model(&DoubaoVideo2UserAssetGroup{}).
			Where("id = ? AND user_id = ?", asset.AssetGroupID, asset.UserID).
			UpdateColumn("asset_count", gorm.Expr("asset_count + ?", 1)).Error
	})
}

func GetDoubaoVideo2StorageUsage(userID int) (*DoubaoVideo2StorageUsage, error) {
	var user struct {
		UsedBytes  int64 `gorm:"column:doubao_video2_material_bytes"`
		LimitBytes int64 `gorm:"column:doubao_video2_material_limit_bytes"`
	}
	if err := DB.Model(&User{}).Select("doubao_video2_material_bytes", "doubao_video2_material_limit_bytes").
		Where("id = ?", userID).Take(&user).Error; err != nil {
		return nil, err
	}
	limit := user.LimitBytes
	if limit <= 0 {
		limit = DoubaoVideo2DefaultUserStorageLimit
	}
	remaining := limit - user.UsedBytes
	if remaining < 0 {
		remaining = 0
	}
	return &DoubaoVideo2StorageUsage{UsedBytes: user.UsedBytes, LimitBytes: limit, RemainingBytes: remaining}, nil
}

// ReserveDoubaoVideo2Storage performs the limit check and increment in one
// UPDATE so concurrent uploads cannot jointly exceed the user's allowance.
func ReserveDoubaoVideo2Storage(userID int, sizeBytes int64) error {
	if userID <= 0 || sizeBytes <= 0 {
		return errors.New("material storage reservation is invalid")
	}
	result := DB.Model(&User{}).
		Where("id = ? AND doubao_video2_material_bytes <= (CASE WHEN doubao_video2_material_limit_bytes > 0 THEN doubao_video2_material_limit_bytes ELSE ? END) - ?",
			userID, DoubaoVideo2DefaultUserStorageLimit, sizeBytes).
		UpdateColumn("doubao_video2_material_bytes", gorm.Expr("doubao_video2_material_bytes + ?", sizeBytes))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrDoubaoVideo2MaterialStorageLimit
	}
	return nil
}

func ReleaseDoubaoVideo2Storage(userID int, sizeBytes int64) error {
	if userID <= 0 || sizeBytes <= 0 {
		return nil
	}
	return DB.Model(&User{}).Where("id = ?", userID).UpdateColumn(
		"doubao_video2_material_bytes",
		gorm.Expr("CASE WHEN doubao_video2_material_bytes >= ? THEN doubao_video2_material_bytes - ? ELSE 0 END", sizeBytes, sizeBytes),
	).Error
}

func CreateDoubaoVideo2UserMedia(media *DoubaoVideo2UserMedia) error {
	return DB.Create(media).Error
}

func GetDoubaoVideo2UserMediaByTokenHash(tokenHash string) (*DoubaoVideo2UserMedia, error) {
	media := &DoubaoVideo2UserMedia{}
	err := DB.Where("token_hash = ?", strings.TrimSpace(tokenHash)).First(media).Error
	return media, err
}

func GetDoubaoVideo2UserMediaByAssetID(assetID int64, userID int) (*DoubaoVideo2UserMedia, error) {
	media := &DoubaoVideo2UserMedia{}
	err := DB.Where("asset_id = ? AND user_id = ?", assetID, userID).First(media).Error
	return media, err
}

func ListDoubaoVideo2UserMediaByAssetIDs(assetIDs []int64, userID int) ([]*DoubaoVideo2UserMedia, error) {
	media := make([]*DoubaoVideo2UserMedia, 0)
	if userID <= 0 || len(assetIDs) == 0 {
		return media, nil
	}
	err := DB.Where("asset_id IN ? AND user_id = ?", assetIDs, userID).Find(&media).Error
	return media, err
}

func DeleteDoubaoVideo2UnboundUserMedia(mediaID int64, userID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		media := &DoubaoVideo2UserMedia{}
		if err := tx.Where("id = ? AND user_id = ? AND asset_id = ?", mediaID, userID, 0).First(media).Error; err != nil {
			return err
		}
		if err := tx.Delete(media).Error; err != nil {
			return err
		}
		return tx.Model(&User{}).Where("id = ?", userID).UpdateColumn(
			"doubao_video2_material_bytes",
			gorm.Expr("CASE WHEN doubao_video2_material_bytes >= ? THEN doubao_video2_material_bytes - ? ELSE 0 END", media.SizeBytes, media.SizeBytes),
		).Error
	})
}

func DeleteDoubaoVideo2UserAssetAndMedia(assetID int64, userID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		asset := &DoubaoVideo2UserAsset{}
		if err := tx.Where("id = ? AND user_id = ?", assetID, userID).First(asset).Error; err != nil {
			return err
		}
		media := &DoubaoVideo2UserMedia{}
		mediaErr := tx.Where("asset_id = ? AND user_id = ?", asset.ID, userID).First(media).Error
		if mediaErr != nil && !errors.Is(mediaErr, gorm.ErrRecordNotFound) {
			return mediaErr
		}
		if err := tx.Delete(asset).Error; err != nil {
			return err
		}
		if err := tx.Model(&DoubaoVideo2UserAssetGroup{}).
			Where("id = ? AND user_id = ?", asset.AssetGroupID, userID).
			UpdateColumn("asset_count", gorm.Expr("CASE WHEN asset_count > 0 THEN asset_count - 1 ELSE 0 END")).Error; err != nil {
			return err
		}
		if mediaErr == nil {
			if err := tx.Delete(media).Error; err != nil {
				return err
			}
			if err := tx.Model(&User{}).Where("id = ?", userID).UpdateColumn(
				"doubao_video2_material_bytes",
				gorm.Expr("CASE WHEN doubao_video2_material_bytes >= ? THEN doubao_video2_material_bytes - ? ELSE 0 END", media.SizeBytes, media.SizeBytes),
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func UpdateDoubaoVideo2UserAssetStatus(id int64, userID int, updates map[string]any) error {
	return DB.Model(&DoubaoVideo2UserAsset{}).Where("id = ? AND user_id = ?", id, userID).Updates(updates).Error
}

func ListPendingDoubaoVideo2UserAssets(userID int, limit int) ([]*DoubaoVideo2UserAsset, error) {
	if limit <= 0 {
		limit = 20
	}
	assets := make([]*DoubaoVideo2UserAsset, 0)
	err := DB.Where("user_id = ? AND status = ?", userID, DoubaoVideo2UserMaterialStatusProcessing).
		Order("last_synced_at asc").Limit(limit).Find(&assets).Error
	return assets, err
}

func ListDoubaoVideo2UserAssetsForSync(userID int, limit int) ([]*DoubaoVideo2UserAsset, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	assets := make([]*DoubaoVideo2UserAsset, 0)
	err := DB.Where("user_id = ?", userID).Order("last_synced_at asc, id asc").Limit(limit).Find(&assets).Error
	return assets, err
}

func ListDoubaoVideo2AccessKeys(userID int) ([]*DoubaoVideo2AccessKey, error) {
	keys := make([]*DoubaoVideo2AccessKey, 0)
	err := DB.Where("user_id = ?", userID).Order("created_at desc").Find(&keys).Error
	return keys, err
}

func GetDoubaoVideo2AccessKey(id int64, userID int) (*DoubaoVideo2AccessKey, error) {
	key := &DoubaoVideo2AccessKey{}
	err := DB.Where("id = ? AND user_id = ?", id, userID).First(key).Error
	return key, err
}

func GetDoubaoVideo2AccessKeyByAK(accessKeyID string) (*DoubaoVideo2AccessKey, error) {
	key := &DoubaoVideo2AccessKey{}
	err := DB.Where("access_key_id = ?", strings.TrimSpace(accessKeyID)).First(key).Error
	return key, err
}

func CreateDoubaoVideo2AccessKey(key *DoubaoVideo2AccessKey) error {
	return DB.Create(key).Error
}

func UpdateDoubaoVideo2AccessKey(id int64, userID int, updates map[string]any) error {
	result := DB.Model(&DoubaoVideo2AccessKey{}).Where("id = ? AND user_id = ?", id, userID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func DeleteDoubaoVideo2AccessKey(id int64, userID int) error {
	result := DB.Where("id = ? AND user_id = ?", id, userID).Delete(&DoubaoVideo2AccessKey{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func TouchDoubaoVideo2AccessKey(id int64, timestamp int64) error {
	if id <= 0 {
		return errors.New("access key ID is required")
	}
	return DB.Model(&DoubaoVideo2AccessKey{}).Where("id = ?", id).UpdateColumn("last_used_at", timestamp).Error
}
