package model

import (
	"errors"
	"strings"
)

// ContentModerationKeyUsage stores one successful audit-provider call. It is
// separate from PromptAuditLog because allowed prompts must not appear in the
// security audit trail, while their provider usage still needs accounting.
type ContentModerationKeyUsage struct {
	Id                  int     `json:"id"`
	KeyHash             string  `json:"-" gorm:"type:varchar(64);index:idx_content_moderation_key_created,priority:1;not null"`
	KeyMask             string  `json:"key_mask" gorm:"type:varchar(64);not null"`
	Provider            string  `json:"provider" gorm:"type:varchar(32);not null;default:''"`
	ModelName           string  `json:"model_name" gorm:"type:varchar(255);not null;default:''"`
	PromptTokens        int64   `json:"prompt_tokens" gorm:"not null;default:0"`
	CompletionTokens    int64   `json:"completion_tokens" gorm:"not null;default:0"`
	CacheHitTokens      int64   `json:"cache_hit_tokens" gorm:"not null;default:0"`
	CacheMissTokens     int64   `json:"cache_miss_tokens" gorm:"not null;default:0"`
	TotalTokens         int64   `json:"total_tokens" gorm:"not null;default:0"`
	TokenUsageAvailable bool    `json:"token_usage_available" gorm:"not null;default:false"`
	BillingUSD          float64 `json:"billing_usd" gorm:"type:decimal(20,10);not null;default:0"`
	BillingAvailable    bool    `json:"billing_available" gorm:"not null;default:false"`
	CreatedAt           int64   `json:"created_at" gorm:"bigint;index:idx_content_moderation_key_created,priority:2;not null"`
}

type ContentModerationKeyUsageAggregate struct {
	KeyHash                  string  `json:"-"`
	RequestCount             int64   `json:"request_count"`
	PromptTokens             int64   `json:"prompt_tokens"`
	CompletionTokens         int64   `json:"completion_tokens"`
	CacheHitTokens           int64   `json:"cache_hit_tokens"`
	CacheMissTokens          int64   `json:"cache_miss_tokens"`
	TotalTokens              int64   `json:"total_tokens"`
	BillingUSD               float64 `json:"billing_usd"`
	TokenUsageAvailableCount int64   `json:"-"`
	BillingAvailableCount    int64   `json:"-"`
}

func CreateContentModerationKeyUsage(usage *ContentModerationKeyUsage) error {
	if usage == nil || strings.TrimSpace(usage.KeyHash) == "" || usage.CreatedAt <= 0 {
		return errors.New("invalid content moderation key usage")
	}
	return LOG_DB.Create(usage).Error
}

func AggregateContentModerationKeyUsage(keyHashes []string, startTime, endTime int64) (map[string]ContentModerationKeyUsageAggregate, error) {
	result := make(map[string]ContentModerationKeyUsageAggregate, len(keyHashes))
	if len(keyHashes) == 0 {
		return result, nil
	}

	query := LOG_DB.Model(&ContentModerationKeyUsage{}).Where("key_hash IN ?", keyHashes)
	if startTime > 0 {
		query = query.Where("created_at >= ?", startTime)
	}
	if endTime > 0 {
		query = query.Where("created_at <= ?", endTime)
	}

	var aggregates []ContentModerationKeyUsageAggregate
	err := query.Select(
		"key_hash, COUNT(*) AS request_count, " +
			"COALESCE(SUM(prompt_tokens), 0) AS prompt_tokens, " +
			"COALESCE(SUM(completion_tokens), 0) AS completion_tokens, " +
			"COALESCE(SUM(cache_hit_tokens), 0) AS cache_hit_tokens, " +
			"COALESCE(SUM(cache_miss_tokens), 0) AS cache_miss_tokens, " +
			"COALESCE(SUM(total_tokens), 0) AS total_tokens, " +
			"COALESCE(SUM(billing_usd), 0) AS billing_usd",
	).Group("key_hash").Scan(&aggregates).Error
	if err != nil {
		return nil, err
	}
	for _, aggregate := range aggregates {
		result[aggregate.KeyHash] = aggregate
	}

	type availabilityAggregate struct {
		KeyHash string
		Count   int64
	}
	countAvailable := func(column string) error {
		availableQuery := query.Where(column+" = ?", true)
		var rows []availabilityAggregate
		if err := availableQuery.Select("key_hash, COUNT(*) AS count").Group("key_hash").Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			aggregate := result[row.KeyHash]
			if column == "token_usage_available" {
				aggregate.TokenUsageAvailableCount = row.Count
			} else {
				aggregate.BillingAvailableCount = row.Count
			}
			result[row.KeyHash] = aggregate
		}
		return nil
	}
	if err := countAvailable("token_usage_available"); err != nil {
		return nil, err
	}
	if err := countAvailable("billing_available"); err != nil {
		return nil, err
	}
	return result, nil
}
