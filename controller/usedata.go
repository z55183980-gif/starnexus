package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

const quotaDataMaxRangeSeconds = int64(31 * 24 * time.Hour / time.Second)

type quotaDataQueryResult struct {
	Data      []*model.QuotaData
	QueriedAt int64
}

var (
	quotaDataCache  = newTTLCache[quotaDataQueryResult](20*time.Second, 2048)
	quotaDataFlight singleflight.Group
	quotaDataSlots  = make(chan struct{}, 2)
)

func parseQuotaDataRange(c *gin.Context) (int64, int64, error) {
	startTimestamp, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || startTimestamp <= 0 {
		return 0, 0, errors.New("invalid start_timestamp")
	}
	endTimestamp, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || endTimestamp <= 0 {
		return 0, 0, errors.New("invalid end_timestamp")
	}
	if endTimestamp < startTimestamp {
		return 0, 0, errors.New("end_timestamp must be greater than start_timestamp")
	}
	if endTimestamp-startTimestamp > quotaDataMaxRangeSeconds {
		return 0, 0, errors.New("time range cannot exceed 31 days")
	}
	return startTimestamp, endTimestamp, nil
}

func normalizeQuotaDataCacheQuery(queryValues url.Values) url.Values {
	normalized := make(url.Values, len(queryValues))
	for key, values := range queryValues {
		if key == "default_time" {
			continue
		}
		normalized[key] = append([]string(nil), values...)
	}
	return normalized
}

func queryQuotaData(c *gin.Context, scope string, identity string, query func(context.Context) ([]*model.QuotaData, error)) (quotaDataQueryResult, error) {
	cacheKey := dashboardAggregateCacheKey(c.Request.URL.Path, normalizeQuotaDataCacheQuery(c.Request.URL.Query()), scope, identity)
	if cached, found := quotaDataCache.Get(cacheKey); found {
		return cached, nil
	}
	stale, hasStale := quotaDataCache.GetStale(cacheKey)
	result, err, _ := quotaDataFlight.Do(cacheKey, func() (any, error) {
		if cached, found := quotaDataCache.Get(cacheKey); found {
			return cached, nil
		}
		queryContext, cancel := context.WithTimeout(context.Background(), dashboardAggregateTimeout)
		defer cancel()
		select {
		case quotaDataSlots <- struct{}{}:
			defer func() { <-quotaDataSlots }()
		case <-queryContext.Done():
			return nil, queryContext.Err()
		default:
			return nil, errors.New("dashboard aggregate is busy")
		}
		data, queryErr := query(queryContext)
		if queryErr != nil {
			return nil, queryErr
		}
		value := quotaDataQueryResult{Data: data, QueriedAt: time.Now().Unix()}
		quotaDataCache.Set(cacheKey, value)
		return value, nil
	})
	if err != nil {
		if hasStale {
			return stale, nil
		}
		return quotaDataQueryResult{}, err
	}
	return result.(quotaDataQueryResult), nil
}

func respondQuotaData(c *gin.Context, result quotaDataQueryResult) {
	status := model.GetQuotaDataStatus()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    result.Data,
		"meta": gin.H{
			"queried_at":                result.QueriedAt,
			"aggregation_enabled":       status.Enabled,
			"flush_interval_seconds":    status.FlushIntervalSeconds,
			"last_successful_flush_at":  status.LastSuccessfulFlushAt,
			"pending_aggregation_count": status.PendingAggregationCount,
		},
	})
}

func GetAllQuotaDates(c *gin.Context) {
	startTimestamp, endTimestamp, err := parseQuotaDataRange(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	username := strings.TrimSpace(c.Query("username"))
	result, err := queryQuotaData(c, "admin-quota-data", "shared", func(ctx context.Context) ([]*model.QuotaData, error) {
		return model.GetAllQuotaDatesContext(ctx, startTimestamp, endTimestamp, username)
	})
	if err != nil {
		common.ApiError(c, fmt.Errorf("query dashboard data: %w", err))
		return
	}
	respondQuotaData(c, result)
}

func GetQuotaDatesByUser(c *gin.Context) {
	startTimestamp, endTimestamp, err := parseQuotaDataRange(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := queryQuotaData(c, "admin-user-quota-data", "shared", func(ctx context.Context) ([]*model.QuotaData, error) {
		return model.GetQuotaDataGroupByUserContext(ctx, startTimestamp, endTimestamp)
	})
	if err != nil {
		common.ApiError(c, fmt.Errorf("query dashboard user data: %w", err))
		return
	}
	respondQuotaData(c, result)
}

func GetUserQuotaDates(c *gin.Context) {
	userId := c.GetInt("id")
	startTimestamp, endTimestamp, err := parseQuotaDataRange(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := queryQuotaData(c, "self-quota-data", strconv.Itoa(userId), func(ctx context.Context) ([]*model.QuotaData, error) {
		return model.GetQuotaDataByUserIdContext(ctx, userId, startTimestamp, endTimestamp)
	})
	if err != nil {
		common.ApiError(c, fmt.Errorf("query dashboard data: %w", err))
		return
	}
	respondQuotaData(c, result)
}
