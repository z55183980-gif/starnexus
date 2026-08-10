package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ZQBAPIAssetStatusPreparing  = "PREPARING"
	ZQBAPIAssetStatusProcessing = "PROCESSING"
	ZQBAPIAssetStatusActive     = "ACTIVE"
	ZQBAPIAssetStatusFailed     = "FAILED"
)

// ZQBAPIAsset is the cross-node idempotency registry for provider material.
// It stores identifiers and state only; source image bytes and URLs are never
// persisted here.
type ZQBAPIAsset struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	CreatedAt            int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"autoUpdateTime"`
	CacheKey             string `json:"cache_key" gorm:"type:char(64);uniqueIndex"`
	ChannelID            int    `json:"channel_id" gorm:"index"`
	ProviderHash         string `json:"provider_hash" gorm:"type:char(64)"`
	ProjectName          string `json:"project_name" gorm:"type:varchar(100)"`
	GroupID              string `json:"group_id" gorm:"type:varchar(160);index"`
	ImageSHA256          string `json:"image_sha256" gorm:"type:char(64);index"`
	NormalizationVersion string `json:"normalization_version" gorm:"type:varchar(32)"`
	AssetID              string `json:"asset_id" gorm:"type:varchar(160);index"`
	Status               string `json:"status" gorm:"type:varchar(20);index"`
	ErrorCode            string `json:"error_code" gorm:"type:varchar(64)"`
	ErrorMessage         string `json:"error_message" gorm:"type:text"`
	RequestID            string `json:"request_id" gorm:"type:varchar(160)"`
	LeaseOwner           string `json:"lease_owner" gorm:"type:varchar(64)"`
	LeaseUntil           int64  `json:"lease_until" gorm:"index"`
	LastCheckedAt        int64  `json:"last_checked_at" gorm:"index"`
	ExpiresAt            int64  `json:"expires_at" gorm:"index"`
}

func ClaimZQBAPIAsset(record *ZQBAPIAsset, leaseOwner string, leaseTTL time.Duration) (*ZQBAPIAsset, bool, error) {
	if DB == nil || record == nil || record.CacheKey == "" {
		return record, true, nil
	}
	now := time.Now().Unix()
	record.Status = ZQBAPIAssetStatusPreparing
	record.LeaseOwner = leaseOwner
	record.LeaseUntil = now + int64(leaseTTL/time.Second)
	createResult := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(record)
	if createResult.Error != nil {
		return nil, false, createResult.Error
	}
	if createResult.RowsAffected > 0 {
		return record, true, nil
	}

	existing := &ZQBAPIAsset{}
	if err := DB.Where("cache_key = ?", record.CacheKey).First(existing).Error; err != nil {
		return nil, false, err
	}
	if existing.Status == ZQBAPIAssetStatusActive && existing.AssetID != "" && (existing.ExpiresAt == 0 || existing.ExpiresAt > now) {
		return existing, false, nil
	}
	claimable := existing.LeaseUntil <= now || (existing.Status == ZQBAPIAssetStatusFailed && existing.ExpiresAt <= now)
	if !claimable {
		return existing, false, nil
	}
	result := DB.Model(&ZQBAPIAsset{}).
		Where("id = ? AND lease_until <= ?", existing.ID, now).
		Updates(map[string]any{
			"status":        ZQBAPIAssetStatusPreparing,
			"asset_id":      "",
			"error_code":    "",
			"error_message": "",
			"request_id":    "",
			"lease_owner":   leaseOwner,
			"lease_until":   now + int64(leaseTTL/time.Second),
			"expires_at":    0,
		})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		if err := DB.First(existing, existing.ID).Error; err != nil {
			return nil, false, err
		}
		return existing, false, nil
	}
	if err := DB.First(existing, existing.ID).Error; err != nil {
		return nil, false, err
	}
	return existing, true, nil
}

func GetZQBAPIAsset(cacheKey string) (*ZQBAPIAsset, bool, error) {
	if DB == nil || cacheKey == "" {
		return nil, false, nil
	}
	record := &ZQBAPIAsset{}
	err := DB.Where("cache_key = ?", cacheKey).First(record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return record, err == nil, err
}

func UpdateZQBAPIAssetOwned(id int64, leaseOwner string, updates map[string]any) (bool, error) {
	if DB == nil || id == 0 {
		return true, nil
	}
	result := DB.Model(&ZQBAPIAsset{}).Where("id = ? AND lease_owner = ?", id, leaseOwner).Updates(updates)
	return result.RowsAffected > 0, result.Error
}

func UpdateZQBAPIAssetState(id int64, updates map[string]any) error {
	if DB == nil || id == 0 {
		return nil
	}
	return DB.Model(&ZQBAPIAsset{}).Where("id = ?", id).Updates(updates).Error
}

func DeleteExpiredZQBAPIAssetRecords(now int64, limit int) (int64, error) {
	if DB == nil || limit <= 0 {
		return 0, nil
	}
	var ids []int64
	if err := DB.Model(&ZQBAPIAsset{}).
		Where("expires_at > 0 AND expires_at < ?", now).
		Order("expires_at").Limit(limit).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return 0, err
	}
	result := DB.Where("id IN ?", ids).Delete(&ZQBAPIAsset{})
	return result.RowsAffected, result.Error
}
