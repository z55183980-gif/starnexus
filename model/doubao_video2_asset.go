package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	DoubaoVideo2AssetStatusPreparing  = "PREPARING"
	DoubaoVideo2AssetStatusProcessing = "PROCESSING"
	DoubaoVideo2AssetStatusActive     = "ACTIVE"
	DoubaoVideo2AssetStatusFailed     = "FAILED"
)

// DoubaoVideo2Asset is the channel-type-62 cross-node idempotency registry.
// Source URLs and bytes are deliberately not persisted.
type DoubaoVideo2Asset struct {
	ID                   int64  `json:"id" gorm:"primaryKey"`
	CreatedAt            int64  `json:"created_at" gorm:"autoCreateTime;index"`
	UpdatedAt            int64  `json:"updated_at" gorm:"autoUpdateTime"`
	CacheKey             string `json:"cache_key" gorm:"type:char(64);uniqueIndex"`
	ChannelID            int    `json:"channel_id" gorm:"index"`
	ProviderHash         string `json:"provider_hash" gorm:"type:char(64)"`
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

func ClaimDoubaoVideo2Asset(record *DoubaoVideo2Asset, leaseOwner string, leaseTTL time.Duration) (*DoubaoVideo2Asset, bool, error) {
	if DB == nil || record == nil || record.CacheKey == "" {
		return record, true, nil
	}
	now := time.Now().Unix()
	record.Status, record.LeaseOwner = DoubaoVideo2AssetStatusPreparing, leaseOwner
	record.LeaseUntil = now + int64(leaseTTL/time.Second)
	result := DB.Clauses(clause.OnConflict{DoNothing: true}).Create(record)
	if result.Error != nil || result.RowsAffected > 0 {
		return record, result.RowsAffected > 0, result.Error
	}
	existing := &DoubaoVideo2Asset{}
	if err := DB.Where("cache_key = ?", record.CacheKey).First(existing).Error; err != nil {
		return nil, false, err
	}
	if existing.Status == DoubaoVideo2AssetStatusActive && existing.AssetID != "" && (existing.ExpiresAt == 0 || existing.ExpiresAt > now) {
		return existing, false, nil
	}
	// A known processing asset must be polled, never replaced by another
	// CreateAsset call merely because the node lease expired.
	if existing.Status == DoubaoVideo2AssetStatusProcessing && existing.AssetID != "" {
		return existing, false, nil
	}
	if existing.Status == DoubaoVideo2AssetStatusFailed && existing.ExpiresAt > now {
		return existing, false, nil
	}
	if existing.LeaseUntil > now {
		return existing, false, nil
	}
	result = DB.Model(&DoubaoVideo2Asset{}).Where("id = ? AND lease_until <= ?", existing.ID, now).Updates(map[string]any{
		"status": DoubaoVideo2AssetStatusPreparing, "asset_id": "", "error_code": "", "error_message": "", "request_id": "",
		"lease_owner": leaseOwner, "lease_until": record.LeaseUntil, "expires_at": 0,
	})
	if result.Error != nil {
		return nil, false, result.Error
	}
	if err := DB.First(existing, existing.ID).Error; err != nil {
		return nil, false, err
	}
	return existing, result.RowsAffected > 0, nil
}

func GetDoubaoVideo2Asset(cacheKey string) (*DoubaoVideo2Asset, bool, error) {
	if DB == nil || cacheKey == "" {
		return nil, false, nil
	}
	record := &DoubaoVideo2Asset{}
	err := DB.Where("cache_key = ?", cacheKey).First(record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return record, err == nil, err
}

func UpdateDoubaoVideo2AssetOwned(id int64, owner string, updates map[string]any) (bool, error) {
	if DB == nil || id == 0 {
		return true, nil
	}
	result := DB.Model(&DoubaoVideo2Asset{}).Where("id = ? AND lease_owner = ?", id, owner).Updates(updates)
	return result.RowsAffected > 0, result.Error
}

func UpdateDoubaoVideo2AssetState(id int64, updates map[string]any) error {
	if DB == nil || id == 0 {
		return nil
	}
	return DB.Model(&DoubaoVideo2Asset{}).Where("id = ?", id).Updates(updates).Error
}

func DeleteExpiredDoubaoVideo2AssetRecords(now int64, limit int) (int64, error) {
	if DB == nil || limit <= 0 {
		return 0, nil
	}
	var ids []int64
	if err := DB.Model(&DoubaoVideo2Asset{}).Where("expires_at > 0 AND expires_at < ?", now).Order("expires_at").Limit(limit).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return 0, err
	}
	result := DB.Where("id IN ?", ids).Delete(&DoubaoVideo2Asset{})
	return result.RowsAffected, result.Error
}
