package model

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// QuotaData 柱状图数据
type QuotaData struct {
	Id        int    `json:"id"`
	UserID    int    `json:"user_id" gorm:"index;index:idx_qdt_user_created,priority:1"`
	Username  string `json:"username" gorm:"index:idx_qdt_model_user_name,priority:2;index:idx_qdt_username_created,priority:1;size:64;default:''"`
	ModelName string `json:"model_name" gorm:"index:idx_qdt_model_user_name,priority:1;index:idx_qdt_created_model,priority:2;size:64;default:''"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index:idx_qdt_created_at;index:idx_qdt_user_created,priority:2;index:idx_qdt_created_model,priority:1;index:idx_qdt_username_created,priority:2"`
	TokenUsed int    `json:"token_used" gorm:"default:0"`
	Count     int    `json:"count" gorm:"default:0"`
	Quota     int    `json:"quota" gorm:"default:0"`
}

type QuotaDataStatus struct {
	Enabled                 bool  `json:"enabled"`
	FlushIntervalSeconds    int64 `json:"flush_interval_seconds"`
	LastSuccessfulFlushAt   int64 `json:"last_successful_flush_at"`
	PendingAggregationCount int   `json:"pending_aggregation_count"`
}

var quotaDataFlushLock sync.RWMutex
var quotaDataLastSuccessfulFlushAt atomic.Int64
var quotaDataFlushInProgress atomic.Bool

func UpdateQuotaData() {
	for {
		if common.DataExportEnabled {
			common.SysLog("正在更新数据看板数据...")
			SaveQuotaDataCache()
		}
		time.Sleep(time.Duration(common.DataExportInterval) * time.Minute)
	}
}

var CacheQuotaData = make(map[string]*QuotaData)
var CacheQuotaDataLock = sync.Mutex{}

func logQuotaDataCache(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
	key := fmt.Sprintf("%d-%s-%s-%d", userId, username, modelName, createdAt)
	quotaData, ok := CacheQuotaData[key]
	if ok {
		quotaData.Count += 1
		quotaData.Quota += quota
		quotaData.TokenUsed += tokenUsed
	} else {
		quotaData = &QuotaData{
			UserID:    userId,
			Username:  username,
			ModelName: modelName,
			CreatedAt: createdAt,
			Count:     1,
			Quota:     quota,
			TokenUsed: tokenUsed,
		}
	}
	CacheQuotaData[key] = quotaData
}

func LogQuotaData(userId int, username string, modelName string, quota int, createdAt int64, tokenUsed int) {
	// 只精确到小时
	createdAt = createdAt - (createdAt % 3600)

	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	logQuotaDataCache(userId, username, modelName, quota, createdAt, tokenUsed)
}

func SaveQuotaDataCache() {
	if err := flushQuotaDataCache(); err != nil {
		common.SysLog(fmt.Sprintf("保存数据看板数据失败: %s", err))
	}
}

// flushQuotaDataCache swaps the hot in-memory map before touching the
// database. Relay logging therefore only waits for a small map swap, not for
// the database transaction. Queries use quotaDataFlushLock to see either the
// pre-flush in-memory values or the committed values, never a gap between the
// two.
func flushQuotaDataCache() error {
	quotaDataFlushLock.Lock()
	defer quotaDataFlushLock.Unlock()

	CacheQuotaDataLock.Lock()
	pending := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()

	if len(pending) == 0 {
		return nil
	}

	quotaDataFlushInProgress.Store(true)
	defer quotaDataFlushInProgress.Store(false)

	// Load existing rows in one query, then batch-insert only new dimensions.
	// Existing rows still require individual arithmetic updates because GORM's
	// cross-database expression support for a multi-row upsert is not portable.
	pendingRows := make([]*QuotaData, 0, len(pending))
	userIDs := make([]int, 0, len(pending))
	seenUserIDs := make(map[int]struct{}, len(pending))
	var minCreatedAt int64
	var maxCreatedAt int64
	for _, item := range pending {
		pendingRows = append(pendingRows, item)
		if _, seen := seenUserIDs[item.UserID]; !seen {
			seenUserIDs[item.UserID] = struct{}{}
			userIDs = append(userIDs, item.UserID)
		}
		if minCreatedAt == 0 || item.CreatedAt < minCreatedAt {
			minCreatedAt = item.CreatedAt
		}
		if item.CreatedAt > maxCreatedAt {
			maxCreatedAt = item.CreatedAt
		}
	}

	err := DB.Transaction(func(tx *gorm.DB) error {
		var existingRows []*QuotaData
		if err := tx.Where("user_id IN ? and created_at >= ? and created_at <= ?", userIDs, minCreatedAt, maxCreatedAt).
			Find(&existingRows).Error; err != nil {
			return err
		}
		existingByKey := make(map[string]*QuotaData, len(existingRows))
		for _, item := range existingRows {
			existingByKey[quotaDataIdentityKey(item)] = item
		}

		newRows := make([]*QuotaData, 0, len(pendingRows))
		for _, quotaData := range pendingRows {
			existing, found := existingByKey[quotaDataIdentityKey(quotaData)]
			if !found {
				newRows = append(newRows, quotaData)
				continue
			}
			result := tx.Model(&QuotaData{}).
				Where("id = ?", existing.Id).
				Updates(map[string]interface{}{
					"count":      gorm.Expr("count + ?", quotaData.Count),
					"quota":      gorm.Expr("quota + ?", quotaData.Quota),
					"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
				})
			if result.Error != nil {
				return result.Error
			}
		}
		if len(newRows) > 0 {
			return tx.CreateInBatches(newRows, 100).Error
		}
		return nil
	})
	if err != nil {
		mergePendingQuotaData(pending)
		return err
	}

	quotaDataLastSuccessfulFlushAt.Store(time.Now().Unix())
	common.SysLog(fmt.Sprintf("保存数据看板数据成功，共保存%d条数据", len(pending)))
	return nil
}

func mergePendingQuotaData(pending map[string]*QuotaData) {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()
	for key, item := range pending {
		// GORM can populate primary keys before a transaction is rolled back.
		// Retried cache rows must let the database assign a fresh key.
		item.Id = 0
		if current, ok := CacheQuotaData[key]; ok {
			current.Count += item.Count
			current.Quota += item.Quota
			current.TokenUsed += item.TokenUsed
			continue
		}
		CacheQuotaData[key] = item
	}
}

func GetQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	return GetQuotaDataByUsernameContext(context.Background(), username, startTime, endTime)
}

func GetQuotaDataByUsernameContext(ctx context.Context, username string, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	quotaDataFlushLock.RLock()
	defer quotaDataFlushLock.RUnlock()

	var quotaDatas []*QuotaData
	err = DB.WithContext(ctx).Table("quota_data").Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).Order("created_at ASC").Find(&quotaDatas).Error
	if err != nil {
		return nil, err
	}
	return mergeQuotaDataRows(quotaDatas, snapshotPendingQuotaData(func(item *QuotaData) bool {
		return item.Username == username && item.CreatedAt >= startTime && item.CreatedAt <= endTime
	}), quotaDataIdentityKey), nil
}

func GetQuotaDataByUserId(userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	return GetQuotaDataByUserIdContext(context.Background(), userId, startTime, endTime)
}

func GetQuotaDataByUserIdContext(ctx context.Context, userId int, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	quotaDataFlushLock.RLock()
	defer quotaDataFlushLock.RUnlock()

	var quotaDatas []*QuotaData
	err = DB.WithContext(ctx).Table("quota_data").Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime).Order("created_at ASC").Find(&quotaDatas).Error
	if err != nil {
		return nil, err
	}
	return mergeQuotaDataRows(quotaDatas, snapshotPendingQuotaData(func(item *QuotaData) bool {
		return item.UserID == userId && item.CreatedAt >= startTime && item.CreatedAt <= endTime
	}), quotaDataIdentityKey), nil
}

func GetQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	return GetQuotaDataGroupByUserContext(context.Background(), startTime, endTime)
}

func GetQuotaDataGroupByUserContext(ctx context.Context, startTime int64, endTime int64) (quotaData []*QuotaData, err error) {
	quotaDataFlushLock.RLock()
	defer quotaDataFlushLock.RUnlock()

	var quotaDatas []*QuotaData
	err = DB.WithContext(ctx).Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Order("created_at ASC").
		Find(&quotaDatas).Error
	if err != nil {
		return nil, err
	}
	return mergeQuotaDataRows(quotaDatas, snapshotPendingQuotaData(func(item *QuotaData) bool {
		return item.CreatedAt >= startTime && item.CreatedAt <= endTime
	}), func(item *QuotaData) string {
		return fmt.Sprintf("%s-%d", item.Username, item.CreatedAt)
	}), nil
}

func GetAllQuotaDates(startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	return GetAllQuotaDatesContext(context.Background(), startTime, endTime, username)
}

func GetAllQuotaDatesContext(ctx context.Context, startTime int64, endTime int64, username string) (quotaData []*QuotaData, err error) {
	if username != "" {
		return GetQuotaDataByUsernameContext(ctx, username, startTime, endTime)
	}

	quotaDataFlushLock.RLock()
	defer quotaDataFlushLock.RUnlock()

	var quotaDatas []*QuotaData
	err = DB.WithContext(ctx).Table("quota_data").
		Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("model_name, created_at").
		Order("created_at ASC").
		Find(&quotaDatas).Error
	if err != nil {
		return nil, err
	}
	return mergeQuotaDataRows(quotaDatas, snapshotPendingQuotaData(func(item *QuotaData) bool {
		return item.CreatedAt >= startTime && item.CreatedAt <= endTime
	}), func(item *QuotaData) string {
		return fmt.Sprintf("%s-%d", item.ModelName, item.CreatedAt)
	}), nil
}

func snapshotPendingQuotaData(include func(*QuotaData) bool) []*QuotaData {
	CacheQuotaDataLock.Lock()
	defer CacheQuotaDataLock.Unlock()

	items := make([]*QuotaData, 0, len(CacheQuotaData))
	for _, item := range CacheQuotaData {
		if !include(item) {
			continue
		}
		copyOfItem := *item
		copyOfItem.Id = 0
		items = append(items, &copyOfItem)
	}
	return items
}

func mergeQuotaDataRows(databaseRows []*QuotaData, pendingRows []*QuotaData, key func(*QuotaData) string) []*QuotaData {
	merged := make(map[string]*QuotaData, len(databaseRows)+len(pendingRows))
	for _, rows := range [][]*QuotaData{databaseRows, pendingRows} {
		for _, item := range rows {
			itemKey := key(item)
			if current, ok := merged[itemKey]; ok {
				current.Count += item.Count
				current.Quota += item.Quota
				current.TokenUsed += item.TokenUsed
				continue
			}
			copyOfItem := *item
			merged[itemKey] = &copyOfItem
		}
	}

	result := make([]*QuotaData, 0, len(merged))
	for _, item := range merged {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt == result[j].CreatedAt {
			return key(result[i]) < key(result[j])
		}
		return result[i].CreatedAt < result[j].CreatedAt
	})
	return result
}

func quotaDataIdentityKey(item *QuotaData) string {
	return fmt.Sprintf("%d-%s-%s-%d", item.UserID, item.Username, item.ModelName, item.CreatedAt)
}

func GetQuotaDataStatus() QuotaDataStatus {
	CacheQuotaDataLock.Lock()
	pendingCount := len(CacheQuotaData)
	CacheQuotaDataLock.Unlock()
	if quotaDataFlushInProgress.Load() {
		pendingCount++
	}
	return QuotaDataStatus{
		Enabled:                 common.DataExportEnabled,
		FlushIntervalSeconds:    int64(common.DataExportInterval) * int64(time.Minute/time.Second),
		LastSuccessfulFlushAt:   quotaDataLastSuccessfulFlushAt.Load(),
		PendingAggregationCount: pendingCount,
	}
}
