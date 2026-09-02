package model

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

var tokenPricingContentPatterns = []*regexp.Regexp{
	regexp.MustCompile(`,\s*rawTokens=\d+`),
	regexp.MustCompile(`,\s*rawPrompt=\d+`),
	regexp.MustCompile(`,\s*rawCompletion=\d+`),
	regexp.MustCompile(`,\s*inputRatio=[0-9.]+`),
	regexp.MustCompile(`,\s*outputRatio=[0-9.]+`),
}

func sanitizeUserLogContent(content string) string {
	for _, pattern := range tokenPricingContentPatterns {
		content = pattern.ReplaceAllString(content, "")
	}
	return content
}

type Log struct {
	Id                  int     `json:"id" gorm:"index:idx_created_at_id,priority:1;index:idx_user_id_id,priority:2"`
	UserId              int     `json:"user_id" gorm:"index;index:idx_user_id_id,priority:1"`
	CreatedAt           int64   `json:"created_at" gorm:"bigint;index:idx_created_at_id,priority:2;index:idx_created_at_type;index:idx_logs_type_created_at,priority:2;index:idx_logs_account_window,priority:3"`
	Type                int     `json:"type" gorm:"index:idx_created_at_type;index:idx_logs_type_created_at,priority:1;index:idx_logs_account_window,priority:1"`
	Content             string  `json:"content"`
	Username            string  `json:"username" gorm:"index;index:index_username_model_name,priority:2;default:''"`
	TokenName           string  `json:"token_name" gorm:"index;default:''"`
	ModelName           string  `json:"model_name" gorm:"index;index:index_username_model_name,priority:1;default:''"`
	Quota               int     `json:"quota" gorm:"default:0"`
	PromptTokens        int     `json:"prompt_tokens" gorm:"default:0"`
	CompletionTokens    int     `json:"completion_tokens" gorm:"default:0"`
	UseTime             int     `json:"use_time" gorm:"default:0"`
	UseTimeMs           *int64  `json:"use_time_ms,omitempty" gorm:"bigint"`
	IsStream            bool    `json:"is_stream"`
	ChannelId           int     `json:"channel" gorm:"index"`
	ChannelName         string  `json:"channel_name" gorm:"->"`
	TokenId             int     `json:"token_id" gorm:"default:0;index"`
	Group               string  `json:"group" gorm:"index"`
	Ip                  string  `json:"ip" gorm:"index;default:''"`
	RequestId           string  `json:"request_id,omitempty" gorm:"type:varchar(64);index:idx_logs_request_id;default:''"`
	UpstreamRequestId   string  `json:"upstream_request_id,omitempty" gorm:"type:varchar(128);index:idx_logs_upstream_request_id;default:''"`
	UpstreamAccountId   int     `json:"upstream_account_id,omitempty" gorm:"index;index:idx_logs_account_window,priority:2;default:0"`
	UpstreamAccountName string  `json:"upstream_account_name,omitempty" gorm:"->"`
	AccountCost         float64 `json:"account_cost,omitempty" gorm:"type:decimal(20,8);size:20;not null;default:0.000000"`
	UserCost            float64 `json:"user_cost,omitempty" gorm:"type:decimal(20,8);size:20;not null;default:0.000000"`
	Other               string  `json:"other"`

	// Usage statistics are normalized once when a log is written. Keeping these
	// values in ordinary columns lets usage dashboards aggregate without parsing
	// the large `other` JSON payload for every matching row.
	UsageStatsReady          int8    `json:"-" gorm:"column:usage_stats_ready;not null;default:0"`
	UsageCacheReadTokens     int64   `json:"-" gorm:"column:usage_cache_read_tokens;not null;default:0"`
	UsageBillingCacheRead    int64   `json:"-" gorm:"column:usage_billing_cache_read_tokens;not null;default:0"`
	UsageCacheCreationTokens int64   `json:"-" gorm:"column:usage_cache_creation_tokens;not null;default:0"`
	UsageIsAnthropic         int8    `json:"-" gorm:"column:usage_is_anthropic;not null;default:0"`
	UsageGroupRatio          float64 `json:"-" gorm:"column:usage_group_ratio;not null;default:0"`
	UsageBillingMode         string  `json:"-" gorm:"column:usage_billing_mode;type:varchar(32)"`
	UsageBillingSource       string  `json:"-" gorm:"column:usage_billing_source;type:varchar(32)"`
}

// NormalizeLogUsageStats extracts the small set of values used by the usage
// dashboards. It is intentionally exported so the offline backfill command can
// apply the exact same normalization as live log writes. The return value says
// whether a non-empty payload contained valid JSON.
func NormalizeLogUsageStats(log *Log) bool {
	if log == nil {
		return false
	}
	var other businessMonitorCacheLogOther
	if strings.TrimSpace(log.Other) != "" {
		// A malformed historical payload must not make a new log insert fail. The
		// raw payload remains available in Other for diagnostics, while the
		// normalized counters safely remain zero.
		if err := common.UnmarshalJsonStr(log.Other, &other); err != nil {
			log.UsageStatsReady = 1
			log.UsageCacheReadTokens = 0
			log.UsageBillingCacheRead = 0
			log.UsageCacheCreationTokens = 0
			log.UsageIsAnthropic = 0
			log.UsageGroupRatio = 0
			log.UsageBillingMode = ""
			log.UsageBillingSource = ""
			return false
		}
	}

	cacheReadTokens := max(other.CacheTokens, 0)
	billingCacheReadTokens := cacheReadTokens
	if cacheBilling := other.AdminInfo.CacheBilling; cacheBilling != nil &&
		cacheBilling.RawCacheReadTokens == cacheReadTokens &&
		cacheBilling.BillingCacheReadTokens >= 0 &&
		cacheBilling.BillingCacheReadTokens <= cacheReadTokens {
		billingCacheReadTokens = cacheBilling.BillingCacheReadTokens
	}
	cacheCreationTokens := max(other.cacheCreationTotal(), 0)
	isAnthropic := other.UsageSemantic == "anthropic" || other.Claude

	log.UsageStatsReady = 1
	log.UsageCacheReadTokens = cacheReadTokens
	log.UsageBillingCacheRead = billingCacheReadTokens
	log.UsageCacheCreationTokens = cacheCreationTokens
	if isAnthropic {
		log.UsageIsAnthropic = 1
	} else {
		log.UsageIsAnthropic = 0
	}
	if other.GroupRatio > 0 {
		log.UsageGroupRatio = other.GroupRatio
	} else {
		log.UsageGroupRatio = 0
	}
	log.UsageBillingMode = other.BillingMode
	log.UsageBillingSource = other.BillingSource
	return true
}

// BeforeCreate keeps live writes and historical backfills on the same
// normalization path. The hook only touches internal usage-stat columns.
func (log *Log) BeforeCreate(tx *gorm.DB) error {
	if log.UsageStatsReady == 1 {
		return nil
	}
	NormalizeLogUsageStats(log)
	return nil
}

func publishBusinessMonitorLog(log *Log) {
	if !common.RedisEnabled || common.RDB == nil {
		return
	}
	logCopy := *log
	gopool.Go(func() {
		if err := populateLogUpstreamAccountName(&logCopy); err != nil {
			common.SysLog("failed to populate business monitor upstream account name: " + err.Error())
		}
		if logCopy.ChannelName == "" && logCopy.ChannelId > 0 {
			channel, err := CacheGetChannel(logCopy.ChannelId)
			if err == nil && channel != nil {
				logCopy.ChannelName = channel.Name
			}
		}
		if err := common.PublishBusinessMonitorLog(&logCopy); err != nil {
			common.SysLog("failed to publish business monitor log: " + err.Error())
		}
	})
}

func populateLogUpstreamAccountName(log *Log) error {
	if log == nil || log.UpstreamAccountId <= 0 || log.UpstreamAccountName != "" {
		return nil
	}
	var account UpstreamAccount
	err := DB.Select("name").First(&account, log.UpstreamAccountId).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	log.UpstreamAccountName = account.Name
	return nil
}

func publishBusinessMonitorAlert(log *Log) {
	logCopy := *log
	gopool.Go(func() {
		alert, err := UpsertErrorAlert(&logCopy)
		if err != nil {
			common.SysLog("failed to upsert business monitor alert: " + err.Error())
			return
		}
		if !common.RedisEnabled || common.RDB == nil {
			return
		}
		if err = common.PublishBusinessMonitorEvent("alert", alert); err != nil {
			common.SysLog("failed to publish business monitor alert: " + err.Error())
		}
	})
}

// don't use iota, avoid change log type value
const (
	LogTypeUnknown = 0
	LogTypeTopup   = 1
	LogTypeConsume = 2
	LogTypeManage  = 3
	LogTypeSystem  = 4
	LogTypeError   = 5
	LogTypeRefund  = 6
)

func formatUserLogs(logs []*Log, startIdx int) {
	for i := range logs {
		logs[i].ChannelName = ""
		logs[i].UpstreamAccountId = 0
		logs[i].UpstreamAccountName = ""
		logs[i].AccountCost = 0
		logs[i].Content = sanitizeUserLogContent(logs[i].Content)
		var otherMap map[string]interface{}
		otherMap, _ = common.StrToMap(logs[i].Other)
		if otherMap != nil {
			projectCacheBillingDisplay(otherMap)
			// Remove admin-only debug fields.
			delete(otherMap, "admin_info")
			// delete(otherMap, "reject_reason")
			delete(otherMap, "stream_status")
			delete(otherMap, "token_pricing_enabled")
			delete(otherMap, "token_pricing_input_ratio")
			delete(otherMap, "token_pricing_output_ratio")
			delete(otherMap, "token_pricing_rules")
			delete(otherMap, "raw_prompt_tokens")
			delete(otherMap, "raw_completion_tokens")
			delete(otherMap, "raw_total_tokens")
			delete(otherMap, "billing_prompt_tokens")
			delete(otherMap, "billing_completion_tokens")
			delete(otherMap, "billing_total_tokens")
		}
		logs[i].Other = common.MapToJsonStr(otherMap)
		logs[i].Id = startIdx + i + 1
	}
}

// projectCacheBillingDisplay replaces the usage-log cache-read count with the
// settled billing count while keeping the internal adjustment policy under
// admin_info. The raw count must match the public log value before the
// projection is trusted, so incomplete or stale audit data fails closed.
func projectCacheBillingDisplay(otherMap map[string]interface{}) {
	visibleCacheTokens, ok := otherMap["cache_tokens"].(float64)
	if !ok || visibleCacheTokens < 0 {
		return
	}
	adminInfo, ok := otherMap["admin_info"].(map[string]interface{})
	if !ok {
		return
	}
	cacheBilling, ok := adminInfo["cache_billing"].(map[string]interface{})
	if !ok {
		return
	}
	rawCacheTokens, rawOk := cacheBilling["raw_cache_read_tokens"].(float64)
	billingCacheTokens, billingOk := cacheBilling["billing_cache_read_tokens"].(float64)
	if !rawOk || !billingOk || rawCacheTokens < 0 ||
		visibleCacheTokens != rawCacheTokens || billingCacheTokens < 0 || billingCacheTokens > rawCacheTokens {
		return
	}
	otherMap["cache_tokens"] = billingCacheTokens
}

// projectAdminLogCacheBillingDisplay projects only the cache count used by the
// admin usage-log list. Unlike formatUserLogs, it deliberately preserves
// administrator-only account and audit fields.
func projectAdminLogCacheBillingDisplay(logs []*Log) {
	for i := range logs {
		otherMap, err := common.StrToMap(logs[i].Other)
		if err != nil || otherMap == nil {
			continue
		}
		projectCacheBillingDisplay(otherMap)
		logs[i].Other = common.MapToJsonStr(otherMap)
	}
}

func GetLogByTokenId(tokenId int) (logs []*Log, err error) {
	err = LOG_DB.Model(&Log{}).Where("token_id = ?", tokenId).Order("id desc").Limit(common.MaxRecentItems).Find(&logs).Error
	formatUserLogs(logs, 0)
	return logs, err
}

func RecordLog(userId int, logType int, content string) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

// RecordLogWithAdminInfo 记录操作日志，并将管理员相关信息存入 Other.admin_info，
func RecordLogWithAdminInfo(userId int, logType int, content string, adminInfo map[string]interface{}) {
	if logType == LogTypeConsume && !common.LogConsumeEnabled {
		return
	}
	username, _ := GetUsernameById(userId, false)
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      logType,
		Content:   content,
	}
	if len(adminInfo) > 0 {
		other := map[string]interface{}{
			"admin_info": adminInfo,
		}
		log.Other = common.MapToJsonStr(other)
	}
	if err := LOG_DB.Create(log).Error; err != nil {
		common.SysLog("failed to record log: " + err.Error())
	}
}

func RecordTopupLog(userId int, content string, callerIp string, paymentMethod string, callbackPaymentMethod string) {
	username, _ := GetUsernameById(userId, false)
	adminInfo := map[string]interface{}{
		"server_ip":               common.GetIp(),
		"node_name":               common.NodeName,
		"caller_ip":               callerIp,
		"payment_method":          paymentMethod,
		"callback_payment_method": callbackPaymentMethod,
		"version":                 common.Version,
	}
	other := map[string]interface{}{
		"admin_info": adminInfo,
	}
	log := &Log{
		UserId:    userId,
		Username:  username,
		CreatedAt: common.GetTimestamp(),
		Type:      LogTypeTopup,
		Content:   content,
		Ip:        callerIp,
		Other:     common.MapToJsonStr(other),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record topup log: " + err.Error())
	}
}

func RecordErrorLog(c *gin.Context, userId int, channelId int, modelName string, tokenName string, content string, tokenId int, useTimeSeconds int, useTimeMilliseconds int64,
	isStream bool, group string, other map[string]interface{}) {
	logger.LogInfo(c, fmt.Sprintf("record error log: userId=%d, channelId=%d, modelName=%s, tokenName=%s, content=%s", userId, channelId, modelName, tokenName, content))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	upstreamAccountId := common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId)
	upstreamAccountName := common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountName)
	otherStr := common.MapToJsonStr(attachNodeNameToLogOther(other))
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeError,
		Content:          content,
		PromptTokens:     0,
		CompletionTokens: 0,
		TokenName:        tokenName,
		ModelName:        modelName,
		Quota:            0,
		ChannelId:        channelId,
		TokenId:          tokenId,
		UseTime:          useTimeSeconds,
		UseTimeMs:        resolveUseTimeMilliseconds(useTimeSeconds, &useTimeMilliseconds),
		IsStream:         isStream,
		Group:            group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:           requestId,
		UpstreamRequestId:   upstreamRequestId,
		UpstreamAccountId:   upstreamAccountId,
		UpstreamAccountName: upstreamAccountName,
		Other:               otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
	} else {
		publishBusinessMonitorLog(log)
		publishBusinessMonitorAlert(log)
	}
}

type RecordConsumeLogParams struct {
	ChannelId           int                    `json:"channel_id"`
	PromptTokens        int                    `json:"prompt_tokens"`
	CompletionTokens    int                    `json:"completion_tokens"`
	ModelName           string                 `json:"model_name"`
	TokenName           string                 `json:"token_name"`
	Quota               int                    `json:"quota"`
	Content             string                 `json:"content"`
	TokenId             int                    `json:"token_id"`
	UseTimeSeconds      int                    `json:"use_time_seconds"`
	UseTimeMilliseconds *int64                 `json:"use_time_milliseconds,omitempty"`
	IsStream            bool                   `json:"is_stream"`
	Group               string                 `json:"group"`
	Other               map[string]interface{} `json:"other"`
}

func attachNodeNameToLogOther(other map[string]interface{}) map[string]interface{} {
	if common.NodeName == "" {
		return other
	}
	if other == nil {
		other = make(map[string]interface{})
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["node_name"] = common.NodeName
	return other
}

func resolveUseTimeMilliseconds(useTimeSeconds int, useTimeMilliseconds *int64) *int64 {
	if useTimeMilliseconds != nil {
		value := *useTimeMilliseconds
		if value < 0 {
			value = 0
		}
		return &value
	}
	if useTimeSeconds <= 0 {
		return nil
	}
	value := int64(useTimeSeconds) * 1000
	return &value
}

func RecordConsumeLog(c *gin.Context, userId int, params RecordConsumeLogParams) {
	_ = RecordConsumeLogWithID(c, userId, params)
}

// RecordConsumeLogWithID records a consumption log and returns its primary key.
// Async tasks persist this ID so completion can update the original log timing.
func RecordConsumeLogWithID(c *gin.Context, userId int, params RecordConsumeLogParams) int {
	if !common.LogConsumeEnabled {
		return 0
	}
	logger.LogInfo(c, fmt.Sprintf("record consume log: userId=%d, params=%s", userId, common.GetJsonString(params)))
	username := c.GetString("username")
	requestId := c.GetString(common.RequestIdKey)
	upstreamRequestId := c.GetString(common.UpstreamRequestIdKey)
	upstreamAccountId, accountCost, userCost := consumeLogAccountCosts(c, params)
	upstreamAccountName := common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountName)
	otherStr := common.MapToJsonStr(attachNodeNameToLogOther(params.Other))
	// 判断是否需要记录 IP
	needRecordIp := false
	if settingMap, err := GetUserSetting(userId, false); err == nil {
		if settingMap.RecordIpLog {
			needRecordIp = true
		}
	}
	log := &Log{
		UserId:           userId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             LogTypeConsume,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        params.TokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		UseTimeMs:        resolveUseTimeMilliseconds(params.UseTimeSeconds, params.UseTimeMilliseconds),
		IsStream:         params.IsStream,
		Group:            params.Group,
		Ip: func() string {
			if needRecordIp {
				return c.ClientIP()
			}
			return ""
		}(),
		RequestId:           requestId,
		UpstreamRequestId:   upstreamRequestId,
		UpstreamAccountId:   upstreamAccountId,
		UpstreamAccountName: upstreamAccountName,
		AccountCost:         accountCost,
		UserCost:            userCost,
		Other:               otherStr,
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		logger.LogError(c, "failed to record log: "+err.Error())
		return 0
	} else {
		publishBusinessMonitorLog(log)
	}
	if common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(userId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
		})
	}
	return log.Id
}

func UpdateLogUseTime(logID int, useTimeMilliseconds int64) error {
	if logID <= 0 {
		return nil
	}
	if useTimeMilliseconds < 0 {
		useTimeMilliseconds = 0
	}
	return LOG_DB.Model(&Log{}).Where("id = ?", logID).Updates(map[string]any{
		"use_time":    int(useTimeMilliseconds / 1000),
		"use_time_ms": useTimeMilliseconds,
	}).Error
}

func consumeLogAccountCosts(c *gin.Context, params RecordConsumeLogParams) (int, float64, float64) {
	accountId := common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId)
	if accountId <= 0 || common.QuotaPerUnit <= 0 {
		return 0, 0, 0
	}
	userCost := float64(params.Quota) / common.QuotaPerUnit
	groupRatio := 1.0
	if value, ok := params.Other["group_ratio"].(float64); ok && value > 0 {
		groupRatio = value
	}
	accountRateMultiplier := 1.0
	if value, ok := common.GetContextKeyType[float64](c, constant.ContextKeyUpstreamAccountRateMultiplier); ok && value >= 0 {
		accountRateMultiplier = value
	}
	return accountId, userCost / groupRatio * accountRateMultiplier, userCost
}

type RecordTaskBillingLogParams struct {
	UserId              int
	LogType             int
	Content             string
	ChannelId           int
	ModelName           string
	Quota               int
	TokenId             int
	Group               string
	PromptTokens        int
	CompletionTokens    int
	UseTimeSeconds      int
	UseTimeMilliseconds *int64
	Other               map[string]interface{}
}

func RecordTaskBillingLog(params RecordTaskBillingLogParams) int {
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return 0
	}
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	log := &Log{
		UserId:           params.UserId,
		Username:         username,
		CreatedAt:        common.GetTimestamp(),
		Type:             params.LogType,
		Content:          params.Content,
		PromptTokens:     params.PromptTokens,
		CompletionTokens: params.CompletionTokens,
		TokenName:        tokenName,
		ModelName:        params.ModelName,
		Quota:            params.Quota,
		ChannelId:        params.ChannelId,
		TokenId:          params.TokenId,
		UseTime:          params.UseTimeSeconds,
		UseTimeMs:        resolveUseTimeMilliseconds(params.UseTimeSeconds, params.UseTimeMilliseconds),
		Group:            params.Group,
		Other:            common.MapToJsonStr(attachNodeNameToLogOther(params.Other)),
	}
	err := LOG_DB.Create(log).Error
	if err != nil {
		common.SysLog("failed to record task billing log: " + err.Error())
		return 0
	} else {
		publishBusinessMonitorLog(log)
	}
	if params.LogType == LogTypeConsume && common.DataExportEnabled {
		gopool.Go(func() {
			LogQuotaData(params.UserId, username, params.ModelName, params.Quota, common.GetTimestamp(), params.PromptTokens+params.CompletionTokens)
		})
	}
	return log.Id
}

// RecordTaskBillingLogIdempotent creates one task billing log per durable
// outbox event. Receipt and log share the LOG_DB transaction, so retries after
// crashes or a temporary log-database outage cannot duplicate user records.
func RecordTaskBillingLogIdempotent(eventID string, params RecordTaskBillingLogParams) (int, error) {
	if strings.TrimSpace(eventID) == "" {
		return 0, errors.New("billing log event id is empty")
	}
	if params.LogType == LogTypeConsume && !common.LogConsumeEnabled {
		return 0, nil
	}
	// Resolve display-only metadata before opening the LOG_DB transaction. When
	// DB and LOG_DB share a single SQLite connection, querying DB from inside the
	// transaction would wait on itself indefinitely.
	username, _ := GetUsernameById(params.UserId, false)
	tokenName := ""
	if params.TokenId > 0 {
		if token, err := GetTokenById(params.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	logID := 0
	err := LOG_DB.Transaction(func(tx *gorm.DB) error {
		var receipt BillingLogReceipt
		found := tx.Where("event_id = ?", eventID).Limit(1).Find(&receipt)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected > 0 {
			logID = receipt.LogID
			return nil
		}
		log := &Log{
			UserId: params.UserId, Username: username, CreatedAt: common.GetTimestamp(), Type: params.LogType,
			Content: params.Content, PromptTokens: params.PromptTokens, CompletionTokens: params.CompletionTokens,
			TokenName: tokenName, ModelName: params.ModelName, Quota: params.Quota, ChannelId: params.ChannelId,
			TokenId: params.TokenId, UseTime: params.UseTimeSeconds,
			UseTimeMs: resolveUseTimeMilliseconds(params.UseTimeSeconds, params.UseTimeMilliseconds),
			Group:     params.Group, Other: common.MapToJsonStr(attachNodeNameToLogOther(params.Other)),
		}
		if err := tx.Create(log).Error; err != nil {
			return err
		}
		logID = log.Id
		return tx.Create(&BillingLogReceipt{EventID: eventID, LogID: logID}).Error
	})
	return logID, err
}

type LogExcludeFilter struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

const maxLogExcludeFilters = 10

var allowedLogExcludeFields = map[string]bool{
	"model_name": true,
	"token_name": true,
	"group":      true,
	"username":   true,
}

func ParseLogExcludeFilters(raw string) ([]LogExcludeFilter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	var filters []LogExcludeFilter
	if err := common.UnmarshalJsonStr(raw, &filters); err != nil {
		return nil, errors.New("无效的排除条件")
	}
	if len(filters) > maxLogExcludeFilters {
		return nil, errors.New("排除条件数量过多")
	}

	valid := make([]LogExcludeFilter, 0, len(filters))
	for _, filter := range filters {
		field := strings.TrimSpace(filter.Field)
		value := strings.TrimSpace(filter.Value)
		if field == "" || value == "" {
			continue
		}
		if !allowedLogExcludeFields[field] {
			continue
		}
		valid = append(valid, LogExcludeFilter{Field: field, Value: value})
	}
	return valid, nil
}

func qualifyLogColumn(tablePrefix, column string) string {
	if tablePrefix == "" {
		return column
	}
	return tablePrefix + column
}

func applyLogExcludeFilters(tx *gorm.DB, filters []LogExcludeFilter, tablePrefix string) (*gorm.DB, error) {
	for _, filter := range filters {
		value := strings.TrimSpace(filter.Value)
		if value == "" {
			continue
		}

		switch filter.Field {
		case "model_name", "token_name":
			pattern, err := buildFuzzyLikePattern(value)
			if err != nil {
				return nil, err
			}
			column := qualifyLogColumn(tablePrefix, filter.Field)
			tx = tx.Where(column+" NOT LIKE ? ESCAPE '!'", pattern)
		case "group":
			column := qualifyLogColumn(tablePrefix, logGroupCol)
			tx = tx.Where(column+" != ?", value)
		case "username":
			column := qualifyLogColumn(tablePrefix, "username")
			tx = tx.Where("LOWER("+column+") != LOWER(?)", value)
		}
	}
	return tx, nil
}

// LogQueryOptions contains optional filters for admin usage-detail views. It is
// variadic on GetAllLogs so existing callers (and older integrations) keep the
// original signature while newer callers can filter by upstream account and
// billing metadata.
type LogQueryOptions struct {
	AccountName string
	BillingMode string
	BillingType *int
	Stream      *bool
}

func GetAllLogs(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, channel int, group string, requestId string, upstreamRequestId string, excludeFilters []LogExcludeFilter, options ...LogQueryOptions) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB
	} else {
		tx = LOG_DB.Where("logs.type = ?", logType)
	}

	if modelName != "" {
		modelNamePattern, patternErr := buildFuzzyLikePattern(modelName)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("logs.model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if username != "" {
		usernamePattern, patternErr := buildFuzzyLikePattern(username)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("LOWER(logs.username) LIKE LOWER(?) ESCAPE '!'", usernamePattern)
	}
	if tokenName != "" {
		tokenNamePattern, patternErr := buildFuzzyLikePattern(tokenName)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("logs.token_name LIKE ? ESCAPE '!'", tokenNamePattern)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	if len(options) > 0 {
		option := options[0]
		if option.AccountName != "" {
			accountPattern, patternErr := buildFuzzyLikePattern(option.AccountName)
			if patternErr != nil {
				return nil, 0, patternErr
			}
			// LOG_DB may be configured as a separate database from DB. Resolve
			// matching account IDs first, then pass the values to the log query so
			// the filter remains valid on every supported database topology.
			var accountIDs []int
			if err = DB.Model(&UpstreamAccount{}).
				Where("LOWER(name) LIKE LOWER(?) ESCAPE '!'", accountPattern).
				Pluck("id", &accountIDs).Error; err != nil {
				return nil, 0, err
			}
			if len(accountIDs) == 0 {
				return []*Log{}, 0, nil
			}
			tx = tx.Where("logs.upstream_account_id IN ?", accountIDs)
		}
		if option.BillingMode != "" {
			validBillingMode := false
			switch option.BillingMode {
			case "token", "per_request", "image", "video", "tiered_expr":
				validBillingMode = true
			default:
				// Ignore unknown billing modes instead of interpolating arbitrary
				// input into a LIKE pattern.
			}
			if validBillingMode {
				// Billing metadata is normalized at write/backfill time, so this
				// predicate can use an ordinary column instead of scanning
				// and parsing the large `other` JSON payload.
				tx = tx.Where("logs.usage_billing_mode = ?", option.BillingMode)
			}
		}
		if option.BillingType != nil {
			if *option.BillingType == 1 {
				tx = tx.Where("logs.usage_billing_source = ?", "subscription")
			} else if *option.BillingType == 0 {
				tx = tx.Where("(logs.usage_billing_source IS NULL OR logs.usage_billing_source <> ?)", "subscription")
			}
		}
		if option.Stream != nil {
			tx = tx.Where("logs.is_stream = ?", *option.Stream)
		}
	}
	tx, err = applyLogExcludeFilters(tx, excludeFilters, "logs.")
	if err != nil {
		return nil, 0, err
	}
	err = tx.Model(&Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}

	channelIds := types.NewSet[int]()
	accountIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
		if log.UpstreamAccountId != 0 {
			accountIds.Add(log.UpstreamAccountId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	if accountIds.Len() > 0 {
		var accounts []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if err = DB.Model(&UpstreamAccount{}).
			Select("id, name").
			Where("id IN ?", accountIds.Items()).
			Find(&accounts).Error; err != nil {
			return logs, total, err
		}
		accountMap := make(map[int]string, len(accounts))
		for _, account := range accounts {
			accountMap[account.Id] = account.Name
		}
		for i := range logs {
			logs[i].UpstreamAccountName = accountMap[logs[i].UpstreamAccountId]
		}
	}

	projectAdminLogCacheBillingDisplay(logs)

	return logs, total, err
}

const logSearchCountLimit = 10000

func getInviteeUserIDsByAgent(agentID int) ([]int, error) {
	if agentID <= 0 {
		return []int{}, nil
	}
	userIDs := []int{agentID}
	var inviteeIDs []int
	err := DB.Model(&User{}).
		Where("inviter_id = ? AND role = ?", agentID, common.RoleCommonUser).
		Pluck("id", &inviteeIDs).Error
	if err != nil {
		return nil, err
	}
	userIDs = append(userIDs, inviteeIDs...)
	return userIDs, nil
}

func GetAgentUserLogs(agentID int, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, excludeFilters []LogExcludeFilter) (logs []*Log, total int64, err error) {
	userIDs, err := getInviteeUserIDsByAgent(agentID)
	if err != nil {
		return nil, 0, err
	}
	if len(userIDs) == 0 {
		return []*Log{}, 0, nil
	}

	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id IN ?", userIDs)
	} else {
		tx = LOG_DB.Where("logs.user_id IN ? AND logs.type = ?", userIDs, logType)
	}

	if modelName != "" {
		modelNamePattern, patternErr := buildFuzzyLikePattern(modelName)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("logs.model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if username != "" {
		usernamePattern, patternErr := buildFuzzyLikePattern(username)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("LOWER(logs.username) LIKE LOWER(?) ESCAPE '!'", usernamePattern)
	}
	if tokenName != "" {
		tokenNamePattern, patternErr := buildFuzzyLikePattern(tokenName)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("logs.token_name LIKE ? ESCAPE '!'", tokenNamePattern)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	tx, err = applyLogExcludeFilters(tx, excludeFilters, "logs.")
	if err != nil {
		return nil, 0, err
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count agent logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search agent logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

func GetUserLogs(userId int, logType int, startTimestamp int64, endTimestamp int64, modelName string, tokenName string, startIdx int, num int, group string, requestId string, upstreamRequestId string, excludeFilters []LogExcludeFilter) (logs []*Log, total int64, err error) {
	var tx *gorm.DB
	if logType == LogTypeUnknown {
		tx = LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if modelName != "" {
		modelNamePattern, patternErr := buildFuzzyLikePattern(modelName)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("logs.model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if tokenName != "" {
		tokenNamePattern, patternErr := buildFuzzyLikePattern(tokenName)
		if patternErr != nil {
			return nil, 0, patternErr
		}
		tx = tx.Where("logs.token_name LIKE ? ESCAPE '!'", tokenNamePattern)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", group)
	}
	tx, err = applyLogExcludeFilters(tx, excludeFilters, "logs.")
	if err != nil {
		return nil, 0, err
	}
	err = tx.Model(&Log{}).Limit(logSearchCountLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}
	err = tx.Order("logs.id desc").Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, errors.New("查询日志失败")
	}

	formatUserLogs(logs, startIdx)
	return logs, total, err
}

type Stat struct {
	Quota int `json:"quota"`
	Rpm   int `json:"rpm"`
	Tpm   int `json:"tpm"`
}

type BusinessMonitorCacheStats struct {
	InputTokens            int64 `json:"input_tokens"`
	CacheReadTokens        int64 `json:"cache_read_tokens"`
	BillingCacheReadTokens int64 `json:"billing_cache_read_tokens"`
	CacheCreationTokens    int64 `json:"cache_creation_tokens"`
}

// UsageDetailsSummary contains the aggregate metrics displayed above the
// administrator usage-details table. Token buckets follow the same
// normalization used by GetBusinessMonitorCacheStats, while costs are USD.
type UsageDetailsSummary struct {
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheTokens     int64   `json:"cache_tokens"`
	TotalTokens     int64   `json:"total_tokens"`
	ActualCostUSD   float64 `json:"actual_cost_usd"`
	AccountCostUSD  float64 `json:"account_cost_usd"`
	StandardCostUSD float64 `json:"standard_cost_usd"`
	PendingRows     int64   `json:"-" gorm:"column:pending_rows"`
}

var ErrLogUsageStatsBackfillRequired = errors.New("用量统计历史数据尚未回填完成")

type businessMonitorCacheBillingInfo struct {
	RawCacheReadTokens     int64 `json:"raw_cache_read_tokens"`
	BillingCacheReadTokens int64 `json:"billing_cache_read_tokens"`
}

type businessMonitorCacheLogOther struct {
	UsageSemantic         string  `json:"usage_semantic"`
	Claude                bool    `json:"claude"`
	GroupRatio            float64 `json:"group_ratio"`
	BillingMode           string  `json:"billing_mode"`
	BillingSource         string  `json:"billing_source"`
	CacheTokens           int64   `json:"cache_tokens"`
	CacheWriteTokens      int64   `json:"cache_write_tokens"`
	CacheCreationTokens   int64   `json:"cache_creation_tokens"`
	CacheCreationTokens5m int64   `json:"cache_creation_tokens_5m"`
	CacheCreationTokens1h int64   `json:"cache_creation_tokens_1h"`
	AdminInfo             struct {
		CacheBilling *businessMonitorCacheBillingInfo `json:"cache_billing"`
	} `json:"admin_info"`
}

func (other businessMonitorCacheLogOther) cacheCreationTotal() int64 {
	if other.CacheWriteTokens > 0 {
		return other.CacheWriteTokens
	}
	splitTotal := other.CacheCreationTokens5m + other.CacheCreationTokens1h
	if splitTotal > 0 {
		if other.CacheCreationTokens > splitTotal {
			return other.CacheCreationTokens
		}
		return splitTotal
	}
	return other.CacheCreationTokens
}

// GetBusinessMonitorCacheStats normalizes logs into the same token buckets as
// sub2api: uncached input, cache read, and cache creation. OpenAI-style prompt
// totals already include cache tokens, while Anthropic reports these buckets
// separately, so the former is de-duplicated before aggregation.
func GetBusinessMonitorCacheStats(startTimestamp int64, endTimestamp int64, modelName string) (stats BusinessMonitorCacheStats, err error) {
	return GetBusinessMonitorCacheStatsContext(context.Background(), startTimestamp, endTimestamp, modelName)
}

func GetBusinessMonitorCacheStatsContext(ctx context.Context, startTimestamp int64, endTimestamp int64, modelName string) (stats BusinessMonitorCacheStats, err error) {
	queryDB := LOG_DB
	if ctx != nil {
		queryDB = LOG_DB.WithContext(ctx)
	}
	query := queryDB.Table("logs").
		Where("type = ?", LogTypeConsume)
	if modelName != "" {
		query = query.Where("model_name = ?", modelName)
	}
	if startTimestamp != 0 {
		query = query.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		query = query.Where("created_at <= ?", endTimestamp)
	}

	// These columns are populated on new writes and by the offline backfill
	// command. Never fall back to scanning `other` here: a malformed or old row
	// must not turn one dashboard refresh into a full application-side scan.
	cacheRead := "CASE WHEN COALESCE(usage_cache_read_tokens, 0) > 0 THEN usage_cache_read_tokens ELSE 0 END"
	cacheCreation := "CASE WHEN COALESCE(usage_cache_creation_tokens, 0) > 0 THEN usage_cache_creation_tokens ELSE 0 END"
	inputTokens := "CASE WHEN usage_is_anthropic = 1 THEN CASE WHEN COALESCE(prompt_tokens, 0) > 0 THEN prompt_tokens ELSE 0 END ELSE CASE WHEN COALESCE(prompt_tokens, 0) - " + cacheRead + " - " + cacheCreation + " > 0 THEN COALESCE(prompt_tokens, 0) - " + cacheRead + " - " + cacheCreation + " ELSE 0 END END"
	selectSQL := "COALESCE(SUM(" + inputTokens + "), 0) AS input_tokens, " +
		"COALESCE(SUM(" + cacheRead + "), 0) AS cache_read_tokens, " +
		"COALESCE(SUM(CASE WHEN COALESCE(usage_billing_cache_read_tokens, 0) > 0 THEN usage_billing_cache_read_tokens ELSE 0 END), 0) AS billing_cache_read_tokens, " +
		"COALESCE(SUM(" + cacheCreation + "), 0) AS cache_creation_tokens, " +
		"COALESCE(SUM(CASE WHEN COALESCE(usage_stats_ready, 0) = 1 THEN 0 ELSE 1 END), 0) AS pending_rows"
	var row businessMonitorCacheStatsRow
	if queryErr := query.Select(selectSQL).Scan(&row).Error; queryErr != nil {
		common.SysError("failed to query business monitor cache stats: " + queryErr.Error())
		return stats, errors.New("查询缓存命中统计失败")
	}
	if row.PendingRows > 0 {
		return stats, ErrLogUsageStatsBackfillRequired
	}
	return BusinessMonitorCacheStats{
		InputTokens:            row.InputTokens,
		CacheReadTokens:        row.CacheReadTokens,
		BillingCacheReadTokens: row.BillingCacheReadTokens,
		CacheCreationTokens:    row.CacheCreationTokens,
	}, nil
}

type businessMonitorCacheStatsRow struct {
	InputTokens            int64 `gorm:"column:input_tokens"`
	CacheReadTokens        int64 `gorm:"column:cache_read_tokens"`
	BillingCacheReadTokens int64 `gorm:"column:billing_cache_read_tokens"`
	CacheCreationTokens    int64 `gorm:"column:cache_creation_tokens"`
	PendingRows            int64 `gorm:"column:pending_rows"`
}

// GetUsageDetailsSummary aggregates all matching usage-detail records. The
// list endpoint is paginated, but these metrics represent the complete
// filtered result set using normalized columns rather than the `other` JSON.
func GetUsageDetailsSummary(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, group string, options ...LogQueryOptions) (summary UsageDetailsSummary, err error) {
	return GetUsageDetailsSummaryContext(context.Background(), logType, startTimestamp, endTimestamp, modelName, username, tokenName, group, options...)
}

func GetUsageDetailsSummaryContext(ctx context.Context, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, group string, options ...LogQueryOptions) (summary UsageDetailsSummary, err error) {
	return getUsageDetailsSummaryAggregate(ctx, logType, startTimestamp, endTimestamp, modelName, username, tokenName, group, options...)
}

func getUsageDetailsSummaryAggregate(ctx context.Context, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, group string, options ...LogQueryOptions) (UsageDetailsSummary, error) {
	queryDB := LOG_DB
	if ctx != nil {
		queryDB = LOG_DB.WithContext(ctx)
	}
	query := queryDB.Table("logs")
	if logType != LogTypeUnknown {
		query = query.Where("logs.type = ?", logType)
	}
	if modelName != "" {
		pattern, patternErr := buildFuzzyLikePattern(modelName)
		if patternErr != nil {
			return UsageDetailsSummary{}, patternErr
		}
		query = query.Where("logs.model_name LIKE ? ESCAPE '!'", pattern)
	}
	if username != "" {
		pattern, patternErr := buildFuzzyLikePattern(username)
		if patternErr != nil {
			return UsageDetailsSummary{}, patternErr
		}
		query = query.Where("LOWER(logs.username) LIKE LOWER(?) ESCAPE '!'", pattern)
	}
	if tokenName != "" {
		pattern, patternErr := buildFuzzyLikePattern(tokenName)
		if patternErr != nil {
			return UsageDetailsSummary{}, patternErr
		}
		query = query.Where("logs.token_name LIKE ? ESCAPE '!'", pattern)
	}
	if startTimestamp != 0 {
		query = query.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		query = query.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		query = query.Where("logs."+logGroupCol+" = ?", group)
	}
	if len(options) > 0 {
		option := options[0]
		if option.AccountName != "" {
			pattern, patternErr := buildFuzzyLikePattern(option.AccountName)
			if patternErr != nil {
				return UsageDetailsSummary{}, patternErr
			}
			var accountIDs []int
			accountDB := DB
			if ctx != nil {
				accountDB = DB.WithContext(ctx)
			}
			if err := accountDB.Model(&UpstreamAccount{}).Where("LOWER(name) LIKE LOWER(?) ESCAPE '!'", pattern).Pluck("id", &accountIDs).Error; err != nil {
				return UsageDetailsSummary{}, err
			}
			if len(accountIDs) == 0 {
				return UsageDetailsSummary{}, nil
			}
			query = query.Where("logs.upstream_account_id IN ?", accountIDs)
		}
		if option.BillingMode != "" {
			switch option.BillingMode {
			case "token", "per_request", "image", "video", "tiered_expr":
				// Keep not-yet-backfilled rows in the aggregate so PendingRows
				// rejects the result instead of silently omitting possible matches.
				query = query.Where("(COALESCE(logs.usage_stats_ready, 0) = 0 OR logs.usage_billing_mode = ?)", option.BillingMode)
			}
		}
		if option.BillingType != nil {
			if *option.BillingType == 1 {
				query = query.Where("(COALESCE(logs.usage_stats_ready, 0) = 0 OR logs.usage_billing_source = ?)", "subscription")
			} else if *option.BillingType == 0 {
				query = query.Where("(COALESCE(logs.usage_stats_ready, 0) = 0 OR logs.usage_billing_source IS NULL OR logs.usage_billing_source <> ?)", "subscription")
			}
		}
		if option.Stream != nil {
			query = query.Where("logs.is_stream = ?", *option.Stream)
		}
	}

	cacheRead := "CASE WHEN COALESCE(usage_cache_read_tokens, 0) > 0 THEN usage_cache_read_tokens ELSE 0 END"
	cacheCreation := "CASE WHEN COALESCE(usage_cache_creation_tokens, 0) > 0 THEN usage_cache_creation_tokens ELSE 0 END"
	inputTokens := "CASE WHEN usage_is_anthropic = 1 THEN CASE WHEN COALESCE(prompt_tokens, 0) > 0 THEN prompt_tokens ELSE 0 END ELSE CASE WHEN COALESCE(prompt_tokens, 0) - " + cacheRead + " - " + cacheCreation + " > 0 THEN COALESCE(prompt_tokens, 0) - " + cacheRead + " - " + cacheCreation + " ELSE 0 END END"
	outputTokens := "CASE WHEN completion_tokens > 0 THEN completion_tokens ELSE 0 END"
	actualCost := "CASE WHEN user_cost > 0 THEN user_cost WHEN ? > 0 AND quota > 0 THEN CAST(quota AS DECIMAL(20,8)) / CAST(? AS DECIMAL(20,8)) ELSE 0 END"
	groupRatio := "COALESCE(CAST(usage_group_ratio AS DECIMAL(20,8)), 0)"
	standardCost := "CASE WHEN " + groupRatio + " > 0 AND (" + actualCost + ") > 0 THEN (" + actualCost + ") / " + groupRatio + " WHEN ? > 0 AND quota > 0 THEN CAST(quota AS DECIMAL(20,8)) / CAST(? AS DECIMAL(20,8)) ELSE 0 END"

	selectSQL := "COALESCE(SUM(" + inputTokens + "), 0) AS input_tokens, " +
		"COALESCE(SUM(" + outputTokens + "), 0) AS output_tokens, " +
		"COALESCE(SUM(" + cacheRead + " + " + cacheCreation + "), 0) AS cache_tokens, " +
		"COALESCE(SUM(" + inputTokens + " + " + outputTokens + " + " + cacheRead + " + " + cacheCreation + "), 0) AS total_tokens, " +
		"COALESCE(SUM(" + actualCost + "), 0) AS actual_cost_usd, " +
		"COALESCE(SUM(CASE WHEN account_cost > 0 THEN account_cost ELSE 0 END), 0) AS account_cost_usd, " +
		"COALESCE(SUM(" + standardCost + "), 0) AS standard_cost_usd, " +
		"COALESCE(SUM(CASE WHEN COALESCE(usage_stats_ready, 0) = 1 THEN 0 ELSE 1 END), 0) AS pending_rows"
	quotaPerUnit := float64(common.QuotaPerUnit)
	var summary UsageDetailsSummary
	// actualCost appears once in the outer aggregate and twice in the
	// standard-cost expression. Each occurrence has a guarded quota divisor.
	args := []any{quotaPerUnit, quotaPerUnit, quotaPerUnit, quotaPerUnit, quotaPerUnit, quotaPerUnit, quotaPerUnit, quotaPerUnit}
	err := query.Select(selectSQL, args...).Scan(&summary).Error
	if err == nil && summary.PendingRows > 0 {
		return UsageDetailsSummary{}, ErrLogUsageStatsBackfillRequired
	}
	return summary, err
}

func SumUsedQuota(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, channel int, group string, excludeFilters []LogExcludeFilter) (stat Stat, err error) {
	tx := LOG_DB.Table("logs").Select("sum(quota) quota")

	// 为rpm和tpm创建单独的查询
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm")

	if username != "" {
		usernamePattern, patternErr := buildFuzzyLikePattern(username)
		if patternErr != nil {
			return stat, patternErr
		}
		tx = tx.Where("LOWER(username) LIKE LOWER(?) ESCAPE '!'", usernamePattern)
		rpmTpmQuery = rpmTpmQuery.Where("LOWER(username) LIKE LOWER(?) ESCAPE '!'", usernamePattern)
	}
	if tokenName != "" {
		tokenNamePattern, patternErr := buildFuzzyLikePattern(tokenName)
		if patternErr != nil {
			return stat, patternErr
		}
		tx = tx.Where("token_name LIKE ? ESCAPE '!'", tokenNamePattern)
		rpmTpmQuery = rpmTpmQuery.Where("token_name LIKE ? ESCAPE '!'", tokenNamePattern)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		modelNamePattern, patternErr := buildFuzzyLikePattern(modelName)
		if patternErr != nil {
			return stat, patternErr
		}
		tx = tx.Where("model_name LIKE ? ESCAPE '!'", modelNamePattern)
		rpmTpmQuery = rpmTpmQuery.Where("model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx, err = applyLogExcludeFilters(tx, excludeFilters, "")
	if err != nil {
		return stat, err
	}
	rpmTpmQuery, err = applyLogExcludeFilters(rpmTpmQuery, excludeFilters, "")
	if err != nil {
		return stat, err
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)

	// 只统计最近60秒的rpm和tpm
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	// 执行查询
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}

	return stat, nil
}

func SumAgentUsedQuota(agentID int, logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string, group string, excludeFilters []LogExcludeFilter) (stat Stat, err error) {
	userIDs, err := getInviteeUserIDsByAgent(agentID)
	if err != nil || len(userIDs) == 0 {
		return stat, err
	}

	tx := LOG_DB.Table("logs").Select("sum(quota) quota").Where("user_id IN ?", userIDs)
	rpmTpmQuery := LOG_DB.Table("logs").Select("count(*) rpm, sum(prompt_tokens) + sum(completion_tokens) tpm").Where("user_id IN ?", userIDs)

	if username != "" {
		usernamePattern, patternErr := buildFuzzyLikePattern(username)
		if patternErr != nil {
			return stat, patternErr
		}
		tx = tx.Where("LOWER(username) LIKE LOWER(?) ESCAPE '!'", usernamePattern)
		rpmTpmQuery = rpmTpmQuery.Where("LOWER(username) LIKE LOWER(?) ESCAPE '!'", usernamePattern)
	}
	if tokenName != "" {
		tokenNamePattern, patternErr := buildFuzzyLikePattern(tokenName)
		if patternErr != nil {
			return stat, patternErr
		}
		tx = tx.Where("token_name LIKE ? ESCAPE '!'", tokenNamePattern)
		rpmTpmQuery = rpmTpmQuery.Where("token_name LIKE ? ESCAPE '!'", tokenNamePattern)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
		rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
		rpmTpmQuery = rpmTpmQuery.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		modelNamePattern, patternErr := buildFuzzyLikePattern(modelName)
		if patternErr != nil {
			return stat, patternErr
		}
		tx = tx.Where("model_name LIKE ? ESCAPE '!'", modelNamePattern)
		rpmTpmQuery = rpmTpmQuery.Where("model_name LIKE ? ESCAPE '!'", modelNamePattern)
	}
	if group != "" {
		tx = tx.Where(logGroupCol+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupCol+" = ?", group)
	}

	tx, err = applyLogExcludeFilters(tx, excludeFilters, "")
	if err != nil {
		return stat, err
	}
	rpmTpmQuery, err = applyLogExcludeFilters(rpmTpmQuery, excludeFilters, "")
	if err != nil {
		return stat, err
	}

	tx = tx.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", LogTypeConsume)
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", time.Now().Add(-60*time.Second).Unix())

	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query agent log stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	if err := rpmTpmQuery.Scan(&stat).Error; err != nil {
		common.SysError("failed to query agent rpm/tpm stat: " + err.Error())
		return stat, errors.New("查询统计数据失败")
	}
	return stat, nil
}

func SumUsedToken(logType int, startTimestamp int64, endTimestamp int64, modelName string, username string, tokenName string) (token int) {
	tx := LOG_DB.Table("logs").Select("ifnull(sum(prompt_tokens),0) + ifnull(sum(completion_tokens),0)")
	if username != "" {
		tx = tx.Where("username = ?", username)
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if modelName != "" {
		tx = tx.Where("model_name = ?", modelName)
	}
	tx.Where("type = ?", LogTypeConsume).Scan(&token)
	return token
}

func DeleteOldLog(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	var total int64 = 0

	for {
		if nil != ctx.Err() {
			return total, ctx.Err()
		}

		result := LOG_DB.Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		if nil != result.Error {
			return total, result.Error
		}

		total += result.RowsAffected

		if result.RowsAffected < int64(limit) {
			break
		}
	}

	return total, nil
}
