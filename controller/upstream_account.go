package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type upstreamAccountPoolRequest struct {
	Name            *string               `json:"name"`
	Description     *string               `json:"description"`
	Platform        *string               `json:"platform"`
	CredentialType  *string               `json:"credential_type"`
	Status          *string               `json:"status"`
	DefaultProxyId  optionalNullable[int] `json:"default_proxy_id"`
	SchedulerConfig *string               `json:"scheduler_config"`
}

type upstreamAccountRequest struct {
	Name               *string                   `json:"name"`
	Notes              optionalNullable[string]  `json:"notes"`
	Platform           *string                   `json:"platform"`
	Type               *string                   `json:"type"`
	Credentials        *map[string]any           `json:"credentials"`
	CredentialPatch    *map[string]any           `json:"credential_patch"`
	Extra              *string                   `json:"extra"`
	ProxyId            optionalNullable[int]     `json:"proxy_id"`
	Concurrency        *int                      `json:"concurrency"`
	Priority           *int                      `json:"priority"`
	Weight             *int                      `json:"weight"`
	LoadFactor         optionalNullable[int]     `json:"load_factor"`
	RateMultiplier     optionalNullable[float64] `json:"rate_multiplier"`
	Status             *string                   `json:"status"`
	Schedulable        *bool                     `json:"schedulable"`
	ExpiresAt          optionalNullable[int64]   `json:"expires_at"`
	AutoPauseOnExpired *bool                     `json:"auto_pause_on_expired"`
	OAuthRefreshOwner  *string                   `json:"oauth_refresh_owner"`
	PoolIds            *[]int                    `json:"pool_ids"`
}

type upstreamProxyRequest struct {
	Name           *string                         `json:"name"`
	Protocol       *string                         `json:"protocol"`
	Host           *string                         `json:"host"`
	Port           *int                            `json:"port"`
	Auth           *service.UpstreamProxyAuthInput `json:"auth"`
	Status         *string                         `json:"status"`
	ExpiresAt      optionalNullable[int64]         `json:"expires_at"`
	FallbackMode   *string                         `json:"fallback_mode"`
	BackupProxyId  optionalNullable[int]           `json:"backup_proxy_id"`
	ExpiryWarnDays *int                            `json:"expiry_warn_days"`
}

type upstreamScheduledTestPlanRequest struct {
	Name            *string `json:"name"`
	Model           *string `json:"model"`
	IntervalMinutes *int    `json:"interval_minutes"`
	Enabled         *bool   `json:"enabled"`
	AutoRecover     *bool   `json:"auto_recover"`
}

type optionalNullable[T any] struct {
	Present bool
	Value   *T
}

func (value *optionalNullable[T]) UnmarshalJSON(data []byte) error {
	value.Present = true
	if strings.TrimSpace(string(data)) == "null" {
		value.Value = nil
		return nil
	}
	var decoded T
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	value.Value = &decoded
	return nil
}

type upstreamBatchFailure struct {
	Index   int    `json:"index,omitempty"`
	Id      int    `json:"id,omitempty"`
	Message string `json:"message"`
}

type upstreamBatchResult struct {
	SuccessIds []int                  `json:"success_ids"`
	Failures   []upstreamBatchFailure `json:"failures"`
}

func ListUpstreamAccountPools(c *gin.Context) {
	pools, err := service.ListUpstreamAccountPools()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, pools)
}

func GetUpstreamAccountPool(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	pool, err := service.GetUpstreamAccountPool(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, pool)
}

func GetUpstreamAccountPoolCapabilities(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	capabilities, err := service.GetUpstreamAccountPoolCapabilities(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, capabilities)
}

func PublishUpstreamAccountPoolChannel(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request struct {
		Groups []string `json:"groups"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result, err := service.PublishUpstreamAccountPoolChannel(id, request.Groups)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("published upstream account pool %d as local channel %d", id, result.ChannelId))
	common.ApiSuccess(c, result)
}

func UnpublishUpstreamAccountPoolChannel(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	if err := service.UnpublishUpstreamAccountPoolChannel(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("unpublished upstream account pool %d local channel", id))
	common.ApiSuccess(c, nil)
}

func CreateUpstreamAccountPool(c *gin.Context) {
	var request upstreamAccountPoolRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	pool := model.UpstreamAccountPool{
		Name: stringValue(request.Name, ""), Description: stringValue(request.Description, ""),
		Platform:       stringValue(request.Platform, constant.UpstreamPlatformOpenAI),
		CredentialType: stringValue(request.CredentialType, "mixed"),
		Status:         stringValue(request.Status, constant.UpstreamStatusActive),
		DefaultProxyId: optionalPositiveId(request.DefaultProxyId), SchedulerConfig: stringValue(request.SchedulerConfig, "{}"),
	}
	if err := service.CreateUpstreamAccountPool(&pool); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("created upstream account pool %d", pool.Id))
	common.ApiSuccess(c, pool)
}

func UpdateUpstreamAccountPool(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request upstreamAccountPoolRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	current, err := service.GetUpstreamAccountPool(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pool := current.UpstreamAccountPool
	applyAccountPoolRequest(&pool, request)
	if err := service.UpdateUpstreamAccountPool(&pool); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("updated upstream account pool %d", id))
	common.ApiSuccess(c, nil)
}

func DeleteUpstreamAccountPool(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	if err := service.DeleteUpstreamAccountPool(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("deleted upstream account pool %d", id))
	common.ApiSuccess(c, nil)
}

func ListUpstreamAccountPoolMembers(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	members, err := service.ListUpstreamAccountPoolMembers(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, members)
}

func ReplaceUpstreamAccountPoolMembers(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request struct {
		Members []service.UpstreamAccountPoolMemberInput `json:"members"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if err := service.ReplaceUpstreamAccountPoolMembers(id, request.Members); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("replaced upstream account pool %d members", id))
	common.ApiSuccess(c, nil)
}

func ListUpstreamAccounts(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filter := service.UpstreamAccountListFilter{
		Platform: c.Query("platform"), Type: c.Query("type"), Status: c.Query("status"),
		Search: c.Query("search"), PoolId: queryPositiveInt(c, "pool_id"), ProxyId: queryPositiveInt(c, "proxy_id"),
		Page: pageInfo.GetPage(), PageSize: pageInfo.GetPageSize(),
	}
	if raw := strings.TrimSpace(c.Query("schedulable")); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
		filter.Schedulable = &value
	}
	accounts, total, err := service.ListUpstreamAccounts(filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": accounts, "total": total, "page": filter.Page, "page_size": filter.PageSize})
}

func ExportUpstreamAccounts(c *gin.Context) {
	var request struct {
		Ids []int `json:"ids"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	exported, err := service.ExportUpstreamAccounts(request.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("exported %d upstream account(s)", len(exported.Accounts)))
	common.ApiSuccess(c, exported)
}

func ImportUpstreamData(c *gin.Context) {
	var request struct {
		Data service.UpstreamAccountExport `json:"data"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result, err := service.ImportUpstreamData(request.Data)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf(
		"imported upstream data: %d account(s), %d proxy record(s), %d proxy record(s) reused",
		result.AccountCreated, result.ProxyCreated, result.ProxyReused,
	))
	common.ApiSuccess(c, result)
}

func PreviewUpstreamAccountsFromCRS(c *gin.Context) {
	var request service.CRSSyncInput
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result, err := service.PreviewUpstreamAccountsFromCRS(c.Request.Context(), request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func SyncUpstreamAccountsFromCRS(c *gin.Context) {
	var request service.CRSSyncInput
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result, err := service.SyncUpstreamAccountsFromCRS(c.Request.Context(), request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("synced upstream accounts from CRS: %d created, %d updated, %d failed", result.Created, result.Updated, result.Failed))
	common.ApiSuccess(c, result)
}

func GetUpstreamAccount(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	account, err := service.GetUpstreamAccount(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, account)
}

func GetUpstreamAccountQuota(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	usage, err := service.QueryUpstreamAccountQuota(c.Request.Context(), id, service.UpstreamAccountQuotaQueryOptions{
		Force:          strings.EqualFold(strings.TrimSpace(c.Query("force")), "true"),
		IncludeCredits: !strings.EqualFold(strings.TrimSpace(c.Query("include_credits")), "false"),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, usage)
}

func ResetUpstreamAccountQuota(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	result, err := service.ResetUpstreamAccountQuota(c.Request.Context(), id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("reset upstream account %d quota", id))
	common.ApiSuccess(c, result)
}

func GetUpstreamAccountStats(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	stats, err := service.GetUpstreamAccountStats(id, queryPositiveInt(c, "days"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, stats)
}

func ListUpstreamAccountScheduledTestPlans(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	plans, err := service.ListUpstreamAccountScheduledTestPlans(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, plans)
}

func CreateUpstreamAccountScheduledTestPlan(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request upstreamScheduledTestPlanRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	plan := model.UpstreamAccountScheduledTestPlan{
		AccountId: id, Name: stringValue(request.Name, "Scheduled test"), Model: stringValue(request.Model, ""),
		IntervalMinutes: 60, Enabled: true, AutoRecover: true,
	}
	applyScheduledTestPlanRequest(&plan, request)
	if err := service.CreateUpstreamAccountScheduledTestPlan(&plan); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("created upstream account %d scheduled test plan %d", id, plan.Id))
	common.ApiSuccess(c, plan)
}

func UpdateUpstreamAccountScheduledTestPlan(c *gin.Context) {
	accountId, ok := positivePathId(c)
	if !ok {
		return
	}
	planId, ok := positiveNamedPathId(c, "planId")
	if !ok {
		return
	}
	var request upstreamScheduledTestPlanRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	plan, err := service.GetUpstreamAccountScheduledTestPlan(accountId, planId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	applyScheduledTestPlanRequest(plan, request)
	if err := service.UpdateUpstreamAccountScheduledTestPlan(plan); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("updated upstream account %d scheduled test plan %d", accountId, planId))
	common.ApiSuccess(c, plan)
}

func DeleteUpstreamAccountScheduledTestPlan(c *gin.Context) {
	accountId, ok := positivePathId(c)
	if !ok {
		return
	}
	planId, ok := positiveNamedPathId(c, "planId")
	if !ok {
		return
	}
	if err := service.DeleteUpstreamAccountScheduledTestPlan(accountId, planId); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("deleted upstream account %d scheduled test plan %d", accountId, planId))
	common.ApiSuccess(c, nil)
}

func ListUpstreamAccountScheduledTestResults(c *gin.Context) {
	accountId, ok := positivePathId(c)
	if !ok {
		return
	}
	planId, ok := positiveNamedPathId(c, "planId")
	if !ok {
		return
	}
	results, err := service.ListUpstreamAccountScheduledTestResults(accountId, planId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, results)
}

func CreateUpstreamAccount(c *gin.Context) {
	var request upstreamAccountRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.Credentials == nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	account := newAccountFromRequest(request)
	poolIds := []int{}
	if request.PoolIds != nil {
		poolIds = *request.PoolIds
	}
	input := &service.UpstreamAccountCreateInput{Account: account, Credentials: *request.Credentials, PoolIds: poolIds}
	if err := service.CreateUpstreamAccount(input); err != nil {
		common.ApiError(c, err)
		return
	}
	account = input.Account
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("created upstream account %d", account.Id))
	view, err := service.GetUpstreamAccount(account.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func UpdateUpstreamAccount(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request upstreamAccountRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	current, err := service.GetUpstreamAccount(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	account := current.UpstreamAccount
	applyAccountRequest(&account, request)
	if err := service.UpdateUpstreamAccount(&service.UpstreamAccountUpdateInput{Account: account, Credentials: request.Credentials, CredentialPatch: request.CredentialPatch, PoolIds: request.PoolIds}); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("updated upstream account %d", id))
	view, err := service.GetUpstreamAccount(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func DeleteUpstreamAccount(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	if err := service.DeleteUpstreamAccount(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("deleted upstream account %d", id))
	common.ApiSuccess(c, nil)
}

func RecoverUpstreamAccount(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request struct {
		Scope service.UpstreamAccountRecoveryScope `json:"scope"`
	}
	if c.Request.ContentLength != 0 {
		if err := common.DecodeJson(c.Request.Body, &request); err != nil {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
	}
	if err := service.RecoverUpstreamAccountRuntimeState(id, request.Scope); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("recovered upstream account %d runtime state (%s)", id, request.Scope))
	account, err := service.GetUpstreamAccount(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, account)
}

func RecoverUpstreamAccountsBatch(c *gin.Context) {
	var request struct {
		Ids   []int                                `json:"ids"`
		Scope service.UpstreamAccountRecoveryScope `json:"scope"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Ids) == 0 || len(request.Ids) > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result := upstreamBatchResult{SuccessIds: []int{}, Failures: []upstreamBatchFailure{}}
	for _, id := range request.Ids {
		if err := service.RecoverUpstreamAccountRuntimeState(id, request.Scope); err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Id: id, Message: err.Error()})
			continue
		}
		result.SuccessIds = append(result.SuccessIds, id)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("recovered %d upstream account runtime states", len(result.SuccessIds)))
	common.ApiSuccess(c, result)
}

func CreateUpstreamAccountsBatch(c *gin.Context) {
	var request struct {
		Items []upstreamAccountRequest `json:"items"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Items) == 0 || len(request.Items) > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result := upstreamBatchResult{SuccessIds: []int{}, Failures: []upstreamBatchFailure{}}
	for index, item := range request.Items {
		if item.Credentials == nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Index: index, Message: "credentials are required"})
			continue
		}
		account := newAccountFromRequest(item)
		poolIds := []int{}
		if item.PoolIds != nil {
			poolIds = *item.PoolIds
		}
		input := &service.UpstreamAccountCreateInput{Account: account, Credentials: *item.Credentials, PoolIds: poolIds}
		err := service.CreateUpstreamAccount(input)
		if err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Index: index, Message: err.Error()})
			continue
		}
		result.SuccessIds = append(result.SuccessIds, input.Account.Id)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("batch created %d upstream account(s)", len(result.SuccessIds)))
	common.ApiSuccess(c, result)
}

func UpdateUpstreamAccountsBatch(c *gin.Context) {
	var request struct {
		Ids   []int                  `json:"ids"`
		Patch upstreamAccountRequest `json:"patch"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || !validBatchIds(request.Ids) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result := upstreamBatchResult{SuccessIds: []int{}, Failures: []upstreamBatchFailure{}}
	for _, id := range uniquePositiveIds(request.Ids) {
		current, err := service.GetUpstreamAccount(id)
		if err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Id: id, Message: err.Error()})
			continue
		}
		account := current.UpstreamAccount
		applyAccountRequest(&account, request.Patch)
		err = service.UpdateUpstreamAccount(&service.UpstreamAccountUpdateInput{Account: account, Credentials: request.Patch.Credentials, CredentialPatch: request.Patch.CredentialPatch, PoolIds: request.Patch.PoolIds})
		if err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Id: id, Message: err.Error()})
			continue
		}
		result.SuccessIds = append(result.SuccessIds, id)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("batch updated %d upstream account(s)", len(result.SuccessIds)))
	common.ApiSuccess(c, result)
}

func DeleteUpstreamAccountsBatch(c *gin.Context) {
	ids, ok := decodeUpstreamBatchIds(c)
	if !ok {
		return
	}
	result := upstreamBatchResult{SuccessIds: []int{}, Failures: []upstreamBatchFailure{}}
	for _, id := range uniquePositiveIds(ids) {
		if err := service.DeleteUpstreamAccount(id); err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Id: id, Message: err.Error()})
			continue
		}
		result.SuccessIds = append(result.SuccessIds, id)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("batch deleted %d upstream account(s)", len(result.SuccessIds)))
	common.ApiSuccess(c, result)
}

func StartUpstreamAccountOAuth(c *gin.Context) {
	var request struct {
		AccountId      *int                  `json:"account_id"`
		ProxyId        optionalNullable[int] `json:"proxy_id"`
		Platform       string                `json:"platform"`
		CredentialType string                `json:"credential_type"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result, err := service.StartUpstreamAccountOAuth(service.UpstreamOAuthStartInput{
		AccountId: request.AccountId, ProxyId: optionalPositiveId(request.ProxyId), ProxyIdPresent: request.ProxyId.Present,
		Platform: request.Platform, CredentialType: request.CredentialType,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func CompleteUpstreamAccountOAuth(c *gin.Context) {
	var request struct {
		Input   string `json:"input"`
		Name    string `json:"name"`
		PoolIds []int  `json:"pool_ids"`
		ProxyId *int   `json:"proxy_id"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	code, state, err := parseCodexAuthorizationInput(request.Input)
	if err != nil || strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	account, err := service.CompleteUpstreamAccountOAuth(ctx, service.UpstreamOAuthCompleteInput{
		State: state, Code: code, Name: request.Name, PoolIds: request.PoolIds, ProxyId: request.ProxyId,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("completed upstream OAuth account %d", account.Id))
	common.ApiSuccess(c, account)
}

func RefreshUpstreamAccountOAuth(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	account, err := service.RefreshUpstreamOAuthAccount(ctx, id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("refreshed upstream OAuth account %d", id))
	common.ApiSuccess(c, account)
}

func TestUpstreamAccount(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request struct {
		Model   string `json:"model"`
		ModelId string `json:"model_id"`
		Mode    string `json:"mode"`
	}
	if c.Request.ContentLength != 0 {
		if err := common.DecodeJson(c.Request.Body, &request); err != nil {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	testModel := strings.TrimSpace(request.ModelId)
	if testModel == "" {
		testModel = request.Model
	}
	result, err := service.TestUpstreamAccount(ctx, id, service.UpstreamAccountTestOptions{Model: testModel, Mode: request.Mode})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func TestUpstreamAccountsBatch(c *gin.Context) {
	var request struct {
		Ids     []int  `json:"ids"`
		Model   string `json:"model"`
		ModelId string `json:"model_id"`
		Mode    string `json:"mode"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Ids) == 0 || len(request.Ids) > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	results := make([]*service.UpstreamAccountTestResult, 0, len(request.Ids))
	testModel := strings.TrimSpace(request.ModelId)
	if testModel == "" {
		testModel = request.Model
	}
	for _, id := range request.Ids {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		result, err := service.TestUpstreamAccount(ctx, id, service.UpstreamAccountTestOptions{Model: testModel, Mode: request.Mode})
		cancel()
		if err != nil {
			results = append(results, &service.UpstreamAccountTestResult{AccountId: id, Result: "test_failed"})
			continue
		}
		results = append(results, result)
	}
	common.ApiSuccess(c, results)
}

func ListUpstreamProxies(c *gin.Context) {
	proxies, err := service.ListUpstreamProxies()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, proxies)
}

func GetUpstreamProxy(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	proxy, err := service.GetUpstreamProxy(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, proxy)
}

func CreateUpstreamProxy(c *gin.Context) {
	var request upstreamProxyRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	auth := service.UpstreamProxyAuthInput{}
	if request.Auth != nil {
		auth = *request.Auth
	}
	proxy := newProxyFromRequest(request)
	input := &service.UpstreamProxyCreateInput{Proxy: proxy, Auth: auth}
	if err := service.CreateUpstreamProxy(input); err != nil {
		common.ApiError(c, err)
		return
	}
	proxy = input.Proxy
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("created upstream proxy %d", proxy.Id))
	view, err := service.GetUpstreamProxy(proxy.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func UpdateUpstreamProxy(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	var request upstreamProxyRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	current, err := service.GetUpstreamProxy(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	proxy := current.UpstreamProxy
	applyProxyRequest(&proxy, request)
	if err := service.UpdateUpstreamProxy(&service.UpstreamProxyUpdateInput{Proxy: proxy, Auth: request.Auth}); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("updated upstream proxy %d", id))
	view, err := service.GetUpstreamProxy(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

func DeleteUpstreamProxy(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	if err := service.DeleteUpstreamProxy(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("deleted upstream proxy %d", id))
	common.ApiSuccess(c, nil)
}

func CreateUpstreamProxiesBatch(c *gin.Context) {
	var request struct {
		Items []upstreamProxyRequest `json:"items"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Items) == 0 || len(request.Items) > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result := upstreamBatchResult{SuccessIds: []int{}, Failures: []upstreamBatchFailure{}}
	for index, item := range request.Items {
		auth := service.UpstreamProxyAuthInput{}
		if item.Auth != nil {
			auth = *item.Auth
		}
		proxy := newProxyFromRequest(item)
		input := &service.UpstreamProxyCreateInput{Proxy: proxy, Auth: auth}
		if err := service.CreateUpstreamProxy(input); err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Index: index, Message: err.Error()})
			continue
		}
		result.SuccessIds = append(result.SuccessIds, input.Proxy.Id)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("batch created %d upstream proxy record(s)", len(result.SuccessIds)))
	common.ApiSuccess(c, result)
}

func UpdateUpstreamProxiesBatch(c *gin.Context) {
	var request struct {
		Ids   []int                `json:"ids"`
		Patch upstreamProxyRequest `json:"patch"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || !validBatchIds(request.Ids) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	result := upstreamBatchResult{SuccessIds: []int{}, Failures: []upstreamBatchFailure{}}
	for _, id := range uniquePositiveIds(request.Ids) {
		current, err := service.GetUpstreamProxy(id)
		if err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Id: id, Message: err.Error()})
			continue
		}
		proxy := current.UpstreamProxy
		applyProxyRequest(&proxy, request.Patch)
		if err := service.UpdateUpstreamProxy(&service.UpstreamProxyUpdateInput{Proxy: proxy, Auth: request.Patch.Auth}); err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Id: id, Message: err.Error()})
			continue
		}
		result.SuccessIds = append(result.SuccessIds, id)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("batch updated %d upstream proxy record(s)", len(result.SuccessIds)))
	common.ApiSuccess(c, result)
}

func DeleteUpstreamProxiesBatch(c *gin.Context) {
	ids, ok := decodeUpstreamBatchIds(c)
	if !ok {
		return
	}
	result := upstreamBatchResult{SuccessIds: []int{}, Failures: []upstreamBatchFailure{}}
	for _, id := range uniquePositiveIds(ids) {
		if err := service.DeleteUpstreamProxy(id); err != nil {
			result.Failures = append(result.Failures, upstreamBatchFailure{Id: id, Message: err.Error()})
			continue
		}
		result.SuccessIds = append(result.SuccessIds, id)
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("batch deleted %d upstream proxy record(s)", len(result.SuccessIds)))
	common.ApiSuccess(c, result)
}

func TestUpstreamProxy(c *gin.Context) {
	id, ok := positivePathId(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	result, err := service.TestUpstreamProxy(ctx, id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func TestUpstreamProxiesBatch(c *gin.Context) {
	var request struct {
		Ids []int `json:"ids"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || len(request.Ids) == 0 || len(request.Ids) > 100 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	results := make([]*service.UpstreamProxyTestResult, 0, len(request.Ids))
	for _, id := range request.Ids {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
		result, err := service.TestUpstreamProxy(ctx, id)
		cancel()
		if err != nil {
			results = append(results, &service.UpstreamProxyTestResult{ProxyId: id, Result: "test_failed"})
			continue
		}
		results = append(results, result)
	}
	common.ApiSuccess(c, results)
}

func newAccountFromRequest(request upstreamAccountRequest) model.UpstreamAccount {
	account := model.UpstreamAccount{
		Platform: constant.UpstreamPlatformOpenAI, Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
		Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		OAuthRefreshOwner: constant.UpstreamOAuthRefreshOwnerStarNexus,
	}
	applyAccountRequest(&account, request)
	if account.Type != constant.UpstreamAccountTypeOAuth && account.Type != constant.UpstreamAccountTypeSetupToken {
		account.OAuthRefreshOwner = constant.UpstreamOAuthRefreshOwnerExternal
	}
	return account
}

func applyAccountRequest(account *model.UpstreamAccount, request upstreamAccountRequest) {
	if request.Name != nil {
		account.Name = *request.Name
	}
	if request.Notes.Present {
		account.Notes = request.Notes.Value
	}
	if request.Platform != nil {
		account.Platform = *request.Platform
	}
	if request.Type != nil {
		account.Type = *request.Type
	}
	if request.Extra != nil {
		account.Extra = *request.Extra
	}
	if request.ProxyId.Present {
		account.ProxyId = optionalPositiveId(request.ProxyId)
		account.ProxyFallbackOriginId = nil
	}
	if request.Concurrency != nil {
		account.Concurrency = *request.Concurrency
	}
	if request.Priority != nil {
		account.Priority = *request.Priority
	}
	if request.Weight != nil {
		account.Weight = *request.Weight
	}
	if request.LoadFactor.Present {
		account.LoadFactor = request.LoadFactor.Value
	}
	if request.RateMultiplier.Present {
		account.RateMultiplier = request.RateMultiplier.Value
	}
	if request.Status != nil {
		account.Status = *request.Status
	}
	if request.Schedulable != nil {
		account.Schedulable = *request.Schedulable
	}
	if request.ExpiresAt.Present {
		account.ExpiresAt = request.ExpiresAt.Value
	}
	if request.AutoPauseOnExpired != nil {
		account.AutoPauseOnExpired = *request.AutoPauseOnExpired
	}
	if request.OAuthRefreshOwner != nil {
		account.OAuthRefreshOwner = *request.OAuthRefreshOwner
	}
}

func applyAccountPoolRequest(pool *model.UpstreamAccountPool, request upstreamAccountPoolRequest) {
	if request.Name != nil {
		pool.Name = *request.Name
	}
	if request.Description != nil {
		pool.Description = *request.Description
	}
	if request.Platform != nil {
		pool.Platform = *request.Platform
	}
	if request.CredentialType != nil {
		pool.CredentialType = *request.CredentialType
	}
	if request.Status != nil {
		pool.Status = *request.Status
	}
	if request.DefaultProxyId.Present {
		pool.DefaultProxyId = optionalPositiveId(request.DefaultProxyId)
	}
	if request.SchedulerConfig != nil {
		pool.SchedulerConfig = *request.SchedulerConfig
	}
}

func newProxyFromRequest(request upstreamProxyRequest) model.UpstreamProxy {
	proxy := model.UpstreamProxy{
		Status: constant.UpstreamStatusActive, FallbackMode: constant.UpstreamProxyFallbackNone,
	}
	applyProxyRequest(&proxy, request)
	return proxy
}

func applyProxyRequest(proxy *model.UpstreamProxy, request upstreamProxyRequest) {
	if request.Name != nil {
		proxy.Name = *request.Name
	}
	if request.Protocol != nil {
		proxy.Protocol = *request.Protocol
	}
	if request.Host != nil {
		proxy.Host = *request.Host
	}
	if request.Port != nil {
		proxy.Port = *request.Port
	}
	if request.Status != nil {
		proxy.Status = *request.Status
	}
	if request.ExpiresAt.Present {
		proxy.ExpiresAt = request.ExpiresAt.Value
	}
	if request.FallbackMode != nil {
		proxy.FallbackMode = *request.FallbackMode
	}
	if request.BackupProxyId.Present {
		proxy.BackupProxyId = optionalPositiveId(request.BackupProxyId)
	}
	if request.ExpiryWarnDays != nil {
		proxy.ExpiryWarnDays = *request.ExpiryWarnDays
	}
}

func applyScheduledTestPlanRequest(plan *model.UpstreamAccountScheduledTestPlan, request upstreamScheduledTestPlanRequest) {
	if request.Name != nil {
		plan.Name = *request.Name
	}
	if request.Model != nil {
		plan.Model = *request.Model
	}
	if request.IntervalMinutes != nil {
		plan.IntervalMinutes = *request.IntervalMinutes
	}
	if request.Enabled != nil {
		plan.Enabled = *request.Enabled
	}
	if request.AutoRecover != nil {
		plan.AutoRecover = *request.AutoRecover
	}
}

func stringValue(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}

func positiveOptionalId(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	return value
}

func optionalPositiveId(value optionalNullable[int]) *int {
	if !value.Present || value.Value == nil || *value.Value <= 0 {
		return nil
	}
	return value.Value
}

func positivePathId(c *gin.Context) (int, bool) {
	return positiveNamedPathId(c, "id")
}

func positiveNamedPathId(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return 0, false
	}
	return id, true
}

func queryPositiveInt(c *gin.Context, key string) int {
	id, _ := strconv.Atoi(c.Query(key))
	if id < 0 {
		return 0
	}
	return id
}

func decodeUpstreamBatchIds(c *gin.Context) ([]int, bool) {
	var request struct {
		Ids []int `json:"ids"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || !validBatchIds(request.Ids) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return nil, false
	}
	return request.Ids, true
}

func validBatchIds(ids []int) bool {
	if len(ids) == 0 || len(ids) > 100 {
		return false
	}
	for _, id := range ids {
		if id <= 0 {
			return false
		}
	}
	return true
}

func uniquePositiveIds(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
