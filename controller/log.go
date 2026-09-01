package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"golang.org/x/sync/singleflight"

	"github.com/gin-gonic/gin"
)

var (
	logsStatCache             = newTTLCache[model.Stat](15*time.Second, 2048)
	usageDetailsSummaryCache  = newTTLCache[model.UsageDetailsSummary](2*time.Minute, 1024)
	usageDetailsSummaryFlight singleflight.Group
)

const dashboardAggregateTimeout = 30 * time.Second

var dashboardAggregateSlots = make(chan struct{}, 2)

func GetAllLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	accountName := c.Query("account")
	billingMode := c.Query("billing_mode")
	var billingType *int
	if rawBillingType := c.Query("billing_type"); rawBillingType != "" {
		if parsed, parseErr := strconv.Atoi(rawBillingType); parseErr == nil && (parsed == 0 || parsed == 1) {
			billingType = &parsed
		}
	}
	var stream *bool
	if rawStream := c.Query("stream"); rawStream == "true" || rawStream == "false" {
		parsed := rawStream == "true"
		stream = &parsed
	}
	excludeFilters, err := model.ParseLogExcludeFilters(c.Query("exclude_filters"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logs, total, err := model.GetAllLogs(logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), channel, group, requestId, upstreamRequestId, excludeFilters, model.LogQueryOptions{
		AccountName: accountName,
		BillingMode: billingMode,
		BillingType: billingType,
		Stream:      stream,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

func GetAgentUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	agentId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	excludeFilters, err := model.ParseLogExcludeFilters(c.Query("exclude_filters"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logs, total, err := model.GetAgentUserLogs(agentId, logType, startTimestamp, endTimestamp, modelName, username, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId, upstreamRequestId, excludeFilters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func GetUserLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	userId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	requestId := c.Query("request_id")
	upstreamRequestId := c.Query("upstream_request_id")
	excludeFilters, err := model.ParseLogExcludeFilters(c.Query("exclude_filters"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	logs, total, err := model.GetUserLogs(userId, logType, startTimestamp, endTimestamp, modelName, tokenName, pageInfo.GetStartIdx(), pageInfo.GetPageSize(), group, requestId, upstreamRequestId, excludeFilters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
	return
}

// Deprecated: SearchAllLogs 已废弃，前端未使用该接口。
func SearchAllLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

// Deprecated: SearchUserLogs 已废弃，前端未使用该接口。
func SearchUserLogs(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": false,
		"message": "该接口已废弃",
	})
}

func GetLogByKey(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	if tokenId == 0 {
		c.JSON(200, gin.H{
			"success": false,
			"message": "无效的令牌",
		})
		return
	}
	logs, err := model.GetLogByTokenId(tokenId)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data":    logs,
	})
}

func GetLogsStat(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	excludeFilters, err := model.ParseLogExcludeFilters(c.Query("exclude_filters"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cacheKey := dashboardCacheKey(c.Request.URL.Path, c.Request.URL.Query(), "admin-log-stat", strconv.Itoa(c.GetInt("id")), 15*time.Second)
	if cached, found := logsStatCache.Get(cacheKey); found {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"quota": cached.Quota,
				"rpm":   cached.Rpm,
				"tpm":   cached.Tpm,
			},
		})
		return
	}
	stat, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, excludeFilters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, "")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": stat.Quota,
			"rpm":   stat.Rpm,
			"tpm":   stat.Tpm,
		},
	})
	logsStatCache.Set(cacheKey, stat)
	return
}

// GetLogsSummary returns aggregate usage metrics for the administrator
// usage-details view. It accepts the same filters as the paginated log list.
func GetLogsSummary(c *gin.Context) {
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	username := c.Query("username")
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	group := c.Query("group")
	accountName := c.Query("account")
	billingMode := c.Query("billing_mode")
	var billingType *int
	if rawBillingType := c.Query("billing_type"); rawBillingType != "" {
		if parsed, parseErr := strconv.Atoi(rawBillingType); parseErr == nil && (parsed == 0 || parsed == 1) {
			billingType = &parsed
		}
	}
	var stream *bool
	if rawStream := c.Query("stream"); rawStream == "true" || rawStream == "false" {
		parsed := rawStream == "true"
		stream = &parsed
	}
	cacheKey := dashboardAggregateCacheKey(c.Request.URL.Path, c.Request.URL.Query(), "admin-usage-summary", strconv.Itoa(c.GetInt("id")))
	if cached, found := usageDetailsSummaryCache.Get(cacheKey); found {
		common.ApiSuccess(c, cached)
		return
	}
	stale, hasStale := usageDetailsSummaryCache.GetStale(cacheKey)

	result, err, _ := usageDetailsSummaryFlight.Do(cacheKey, func() (any, error) {
		if cached, found := usageDetailsSummaryCache.Get(cacheKey); found {
			return cached, nil
		}
		// The shared flight must outlive the initiating browser request; a
		// disconnected tab should not cancel work awaited by other callers.
		queryCtx, cancel := context.WithTimeout(context.Background(), dashboardAggregateTimeout)
		defer cancel()
		select {
		case dashboardAggregateSlots <- struct{}{}:
			defer func() { <-dashboardAggregateSlots }()
		case <-queryCtx.Done():
			return nil, queryCtx.Err()
		default:
			return nil, errors.New("dashboard aggregate is busy")
		}
		summary, queryErr := model.GetUsageDetailsSummaryContext(queryCtx, logType, startTimestamp, endTimestamp, modelName, username, tokenName, group, model.LogQueryOptions{
			AccountName: accountName,
			BillingMode: billingMode,
			BillingType: billingType,
			Stream:      stream,
		})
		if queryErr != nil {
			return nil, queryErr
		}
		usageDetailsSummaryCache.Set(cacheKey, summary)
		return summary, nil
	})
	if err != nil {
		if hasStale {
			common.ApiSuccess(c, stale)
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result.(model.UsageDetailsSummary))
}

func GetAgentLogsStat(c *gin.Context) {
	agentId := c.GetInt("id")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	username := c.Query("username")
	modelName := c.Query("model_name")
	group := c.Query("group")
	excludeFilters, err := model.ParseLogExcludeFilters(c.Query("exclude_filters"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cacheKey := dashboardCacheKey(c.Request.URL.Path, c.Request.URL.Query(), "agent-log-stat", strconv.Itoa(agentId), 15*time.Second)
	if cached, found := logsStatCache.Get(cacheKey); found {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"quota": cached.Quota,
				"rpm":   cached.Rpm,
				"tpm":   cached.Tpm,
			},
		})
		return
	}
	stat, err := model.SumAgentUsedQuota(agentId, logType, startTimestamp, endTimestamp, modelName, username, tokenName, group, excludeFilters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": stat.Quota,
			"rpm":   stat.Rpm,
			"tpm":   stat.Tpm,
		},
	})
	logsStatCache.Set(cacheKey, stat)
}

func GetLogsSelfStat(c *gin.Context) {
	username := c.GetString("username")
	logType, _ := strconv.Atoi(c.Query("type"))
	startTimestamp, _ := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	endTimestamp, _ := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	tokenName := c.Query("token_name")
	modelName := c.Query("model_name")
	channel, _ := strconv.Atoi(c.Query("channel"))
	group := c.Query("group")
	excludeFilters, err := model.ParseLogExcludeFilters(c.Query("exclude_filters"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	cacheKey := dashboardCacheKey(c.Request.URL.Path, c.Request.URL.Query(), "self-log-stat", strconv.Itoa(c.GetInt("id"))+":"+username, 15*time.Second)
	if cached, found := logsStatCache.Get(cacheKey); found {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data": gin.H{
				"quota": cached.Quota,
				"rpm":   cached.Rpm,
				"tpm":   cached.Tpm,
			},
		})
		return
	}
	quotaNum, err := model.SumUsedQuota(logType, startTimestamp, endTimestamp, modelName, username, tokenName, channel, group, excludeFilters)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	//tokenNum := model.SumUsedToken(logType, startTimestamp, endTimestamp, modelName, username, tokenName)
	c.JSON(200, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"quota": quotaNum.Quota,
			"rpm":   quotaNum.Rpm,
			"tpm":   quotaNum.Tpm,
			//"token": tokenNum,
		},
	})
	logsStatCache.Set(cacheKey, quotaNum)
	return
}

func DeleteHistoryLogs(c *gin.Context) {
	targetTimestamp, _ := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if targetTimestamp == 0 {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}
	count, err := model.DeleteOldLog(c.Request.Context(), targetTimestamp, 100)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
	return
}
