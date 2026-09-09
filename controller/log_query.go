package controller

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

const adminLogWindowSeconds = int64(45 * 24 * 60 * 60)
const adminLogMaxOffset = 10000

var adminLogCountCache = newTTLCache[int64](time.Minute, 1024)
var adminLogCountFlight singleflight.Group
var adminLogStatFlight singleflight.Group
var adminLogListSlots = make(chan struct{}, 2)

// Historical windows remain available, including recharge and consumption
// records. This is a query bound, never a retention or deletion policy.
func parseAdminLogRange(c *gin.Context, now time.Time) (int64, int64, error) {
	parse := func(key string) (int64, error) {
		raw := c.Query(key)
		if raw == "" {
			return 0, nil
		}
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			return 0, errors.New("invalid " + key)
		}
		return v, nil
	}
	start, err := parse("start_timestamp")
	if err != nil {
		return 0, 0, err
	}
	end, err := parse("end_timestamp")
	if err != nil {
		return 0, 0, err
	}
	if end == 0 {
		end = now.Unix()
	}
	if start == 0 {
		start = end - adminLogWindowSeconds
	}
	if start < 0 || end < start || end-start > adminLogWindowSeconds {
		return 0, 0, errors.New("log time range must be ordered and cannot exceed 45 days")
	}
	return start, end, nil
}

func parseAdminLogPage(c *gin.Context, page *common.PageInfo) (int, error) {
	if page.Page < 1 || page.PageSize < 1 || page.PageSize > 100 {
		return 0, errors.New("invalid log pagination")
	}
	before := 0
	if raw := c.Query("before_id"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return 0, errors.New("invalid before_id")
		}
		before = v
	}
	// Divide before multiplying to also reject integer-overflow offsets.
	if before == 0 && page.Page-1 > adminLogMaxOffset/page.PageSize {
		return 0, errors.New("deep log pagination requires before_id; narrow the time range or use next page")
	}
	return before, nil
}

func cachedAdminLogCount(c *gin.Context, start, end int64) func(*gorm.DB) (int64, error) {
	values := make(url.Values)
	for _, key := range []string{"type", "username", "token_name", "model_name", "channel", "group", "request_id", "upstream_request_id", "exclude_filters", "account", "billing_mode", "billing_type", "stream"} {
		if value := c.Query(key); value != "" {
			values.Set(key, value)
		}
	}
	values.Set("start_timestamp", strconv.FormatInt(start, 10))
	values.Set("end_timestamp", strconv.FormatInt(end, 10))
	key := dashboardAggregateCacheKey("/api/log", values, "admin-log-count", strconv.Itoa(c.GetInt("id")))
	return func(query *gorm.DB) (int64, error) {
		if count, ok := adminLogCountCache.Get(key); ok {
			return count, nil
		}
		result := adminLogCountFlight.DoChan(key, func() (any, error) {
			if count, ok := adminLogCountCache.Get(key); ok {
				return count, nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), dashboardAggregateTimeout)
			defer cancel()
			release, err := acquireDashboardSlot(ctx, dashboardAggregateSlots, dashboardAggregateWaiters, dashboardQueueTimeout)
			if err != nil {
				return nil, err
			}
			defer release()
			var count int64
			if err := query.WithContext(ctx).Count(&count).Error; err != nil {
				return nil, err
			}
			adminLogCountCache.Set(key, count)
			return count, nil
		})
		select {
		case <-query.Statement.Context.Done():
			return 0, query.Statement.Context.Err()
		case value := <-result:
			if value.Err != nil {
				return 0, value.Err
			}
			return value.Val.(int64), nil
		}
	}
}

func queryAdminLogStat(key string, query func(context.Context) (model.Stat, error)) (model.Stat, error) {
	result, err, _ := adminLogStatFlight.Do(key, func() (any, error) {
		if stat, ok := logsStatCache.Get(key); ok {
			return stat, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), dashboardAggregateTimeout)
		defer cancel()
		release, err := acquireDashboardSlot(ctx, dashboardAggregateSlots, dashboardAggregateWaiters, dashboardQueueTimeout)
		if err != nil {
			return nil, err
		}
		defer release()
		stat, err := query(ctx)
		if err != nil {
			return nil, err
		}
		logsStatCache.Set(key, stat)
		return stat, nil
	})
	if err != nil {
		return model.Stat{}, err
	}
	return result.(model.Stat), nil
}
