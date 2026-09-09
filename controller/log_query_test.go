package controller

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func logQueryContext(query string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/log/?"+query, nil)
	return c
}

func TestAdminLogRange(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		query      string
		start, end int64
		invalid    bool
	}{
		{"", now.Unix() - adminLogWindowSeconds, now.Unix(), false},
		{"start_timestamp=0&end_timestamp=0", now.Unix() - adminLogWindowSeconds, now.Unix(), false},
		{"end_timestamp=1700000000", 1700000000 - adminLogWindowSeconds, 1700000000, false},
		{"start_timestamp=1799999990", 1799999990, now.Unix(), false},
		{"start_timestamp=100&end_timestamp=200", 100, 200, false},
		{"start_timestamp=100", 0, 0, true},
		{"start_timestamp=-1", 0, 0, true},
		{"start_timestamp=abc", 0, 0, true},
		{"end_timestamp=9223372036854775808", 0, 0, true},
		{"start_timestamp=200&end_timestamp=100", 0, 0, true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			start, end, err := parseAdminLogRange(logQueryContext(tc.query), now)
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.start, start)
			require.Equal(t, tc.end, end)
		})
	}
}

func TestAdminLogPageBounds(t *testing.T) {
	for _, tc := range []struct {
		query      string
		page, size int
		invalid    bool
	}{
		{"", 1, 20, false}, {"", 501, 20, false}, {"", 502, 20, true},
		{"before_id=42", 502, 20, false}, {"before_id=-1", 1, 20, true},
		{"before_id=bad", 1, 20, true}, {"", -1, 20, true}, {"", 1, -1, true},
		{"", int(^uint(0) >> 1), 100, true},
	} {
		_, err := parseAdminLogPage(logQueryContext(tc.query), &common.PageInfo{Page: tc.page, PageSize: tc.size})
		require.Equal(t, tc.invalid, err != nil, "%+v", tc)
	}
}

func TestAdminLogCountCacheFiltersAndNoWrites(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/logs.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	for _, typ := range []int{model.LogTypeTopup, model.LogTypeConsume, model.LogTypeRefund, model.LogTypeManage, model.LogTypeSystem, model.LogTypeError} {
		require.NoError(t, db.Create(&model.Log{Type: typ, CreatedAt: 100}).Error)
	}
	adminLogCountCache = newTTLCache[int64](time.Minute, 1024)
	defer func() { adminLogCountCache = newTTLCache[int64](time.Minute, 1024) }()
	var calls atomic.Int32
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:count", func(*gorm.DB) { calls.Add(1) }))
	base := db.Model(&model.Log{}).Where("created_at >= ? AND created_at <= ?", 1, 200)
	count, err := cachedAdminLogCount(logQueryContext("p=1"), 1, 200)(base)
	require.NoError(t, err)
	require.EqualValues(t, 6, count)
	count, err = cachedAdminLogCount(logQueryContext("p=2&before_id=3&page_size=2"), 1, 200)(base)
	require.NoError(t, err)
	require.EqualValues(t, 6, count)
	require.EqualValues(t, 1, calls.Load())
	count, err = cachedAdminLogCount(logQueryContext("type=1"), 1, 200)(base.Where("type = ?", model.LogTypeTopup))
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.EqualValues(t, 2, calls.Load())
	var remaining int64
	require.NoError(t, db.Model(&model.Log{}).Count(&remaining).Error)
	require.EqualValues(t, 6, remaining)
}

func TestAdminLogStatCoalescingAndCapacity(t *testing.T) {
	key := t.Name()
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	query := func(ctx context.Context) (model.Stat, error) {
		require.NotNil(t, ctx.Done())
		_, ok := ctx.Deadline()
		require.True(t, ok)
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return model.Stat{Quota: 123}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := queryAdminLogStat(key, query)
			require.NoError(t, err)
			require.EqualValues(t, 123, result.Quota)
		}()
	}
	<-started
	close(release)
	wg.Wait()
	require.EqualValues(t, 1, calls.Load())
	dashboardAggregateSlots <- struct{}{}
	dashboardAggregateSlots <- struct{}{}
	_, err := queryAdminLogStat(key+"busy", query)
	<-dashboardAggregateSlots
	<-dashboardAggregateSlots
	require.ErrorContains(t, err, "busy")
	require.EqualValues(t, 1, calls.Load())
	_, err = queryAdminLogStat(key+"error", func(context.Context) (model.Stat, error) { return model.Stat{}, errors.New("timeout") })
	require.Error(t, err)
	_, found := logsStatCache.Get(key + "error")
	require.False(t, found)
}

func TestAdminLogEndpointCursorAndHistoricalBilling(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/endpoint.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	original := model.LOG_DB
	model.LOG_DB = db
	defer func() { model.LOG_DB = original }()
	adminLogCountCache = newTTLCache[int64](time.Minute, 1024)
	defer func() { adminLogCountCache = newTTLCache[int64](time.Minute, 1024) }()
	for i, typ := range []int{model.LogTypeTopup, model.LogTypeConsume, model.LogTypeRefund} {
		require.NoError(t, db.Create(&model.Log{Id: i + 1, Type: typ, CreatedAt: 100}).Error)
	}
	type response struct {
		Success bool `json:"success"`
		Data    struct {
			Items      []model.Log `json:"items"`
			Total      *int64      `json:"total"`
			HasMore    bool        `json:"has_more"`
			NextCursor int         `json:"next_cursor"`
		} `json:"data"`
	}
	request := func(query string) response {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest("GET", "/api/log/?"+query, nil)
		GetAllLogs(c)
		var result response
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
		return result
	}
	first := request("start_timestamp=90&end_timestamp=200&page_size=2")
	require.True(t, first.Success)
	require.NotNil(t, first.Data.Total)
	require.EqualValues(t, 3, *first.Data.Total)
	require.Equal(t, 2, first.Data.NextCursor)
	second := request("start_timestamp=90&end_timestamp=200&page_size=2&p=2&before_id=2")
	require.True(t, second.Success)
	require.Len(t, second.Data.Items, 1)
	require.Equal(t, model.LogTypeTopup, second.Data.Items[0].Type)
	require.EqualValues(t, 3, *second.Data.Total)
	// The list must still succeed while every aggregate slot is occupied.
	dashboardAggregateSlots <- struct{}{}
	dashboardAggregateSlots <- struct{}{}
	withoutCount := request("start_timestamp=90&end_timestamp=200&page_size=2&include_total=false")
	<-dashboardAggregateSlots
	<-dashboardAggregateSlots
	require.True(t, withoutCount.Success)
	require.Nil(t, withoutCount.Data.Total)
	require.Len(t, withoutCount.Data.Items, 2)
	require.True(t, withoutCount.Data.HasMore)
	require.Equal(t, 2, withoutCount.Data.NextCursor)
	last := request("start_timestamp=90&end_timestamp=200&page_size=2&p=2&before_id=2&include_total=false")
	require.True(t, last.Success)
	require.False(t, last.Data.HasMore)
	require.Len(t, last.Data.Items, 1)
	// Exactly a full last page must not advertise an empty next page.
	exact := request("start_timestamp=90&end_timestamp=200&page_size=3&include_total=false")
	require.True(t, exact.Success)
	require.False(t, exact.Data.HasMore)
	defaultWindow := request("")
	require.True(t, defaultWindow.Success)
	require.Empty(t, defaultWindow.Data.Items)
	for _, query := range []string{"start_timestamp=100", "page_size=-1", "p=999999"} {
		require.False(t, request(query).Success)
	}
	var remaining int64
	require.NoError(t, db.Model(&model.Log{}).Count(&remaining).Error)
	require.EqualValues(t, 3, remaining)
}
