package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type createPromptAuditPolicyRequest struct {
	UserId int `json:"user_id" binding:"required"`
}

type updatePromptAuditPolicyRequest struct {
	MonitorEnabled *bool `json:"monitor_enabled"`
	DelayOnHit     *bool `json:"delay_on_hit"`
	DelaySeconds   *int  `json:"delay_seconds"`
	BlockOnHit     *bool `json:"block_on_hit"`
}

type promptAuditLogCursorPage struct {
	Items      []model.PromptAuditLog `json:"items"`
	HasMore    bool                   `json:"has_more"`
	NextCursor int                    `json:"next_cursor"`
}

func ListPromptAuditPolicies(c *gin.Context) {
	policies, err := model.ListPromptAuditPolicies()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, policies)
}

func CreatePromptAuditPolicy(c *gin.Context) {
	var request createPromptAuditPolicyRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	policy, err := model.CreatePromptAuditPolicy(request.UserId, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidatePromptAuditPolicyCache()
	common.ApiSuccess(c, policy)
}

func UpdatePromptAuditPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid security audit record id"))
		return
	}
	policy, err := model.GetPromptAuditPolicyById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	var request updatePromptAuditPolicyRequest
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.MonitorEnabled != nil {
		policy.MonitorEnabled = *request.MonitorEnabled
	}
	if request.DelayOnHit != nil {
		policy.DelayOnHit = *request.DelayOnHit
		if *request.DelayOnHit {
			policy.BlockOnHit = false
		}
	}
	if request.DelaySeconds != nil {
		policy.DelaySeconds = *request.DelaySeconds
	}
	if request.BlockOnHit != nil {
		policy.BlockOnHit = *request.BlockOnHit
		if *request.BlockOnHit {
			policy.DelayOnHit = false
		}
	}
	if policy.DelayOnHit && policy.BlockOnHit {
		common.ApiError(c, errors.New("delay and block actions cannot be enabled together"))
		return
	}
	if err = model.UpdatePromptAuditPolicy(policy); err != nil {
		common.ApiError(c, err)
		return
	}
	service.InvalidatePromptAuditPolicyCache()
	common.ApiSuccess(c, policy)
}

func DeletePromptAuditPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid security audit record id"))
		return
	}
	if err = model.DeletePromptAuditPolicyAndLogs(id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiError(c, errors.New("security audit record not found"))
			return
		}
		common.ApiError(c, err)
		return
	}
	service.InvalidatePromptAuditPolicyCache()
	common.ApiSuccess(c, nil)
}

func ListPromptAuditLogs(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		common.ApiError(c, errors.New("invalid user id"))
		return
	}
	if _, err = model.GetPromptAuditPolicyByUserId(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	if beforeId, exists, cursorErr := getPromptAuditLogCursor(c, "before_id"); exists {
		if cursorErr != nil {
			common.ApiError(c, cursorErr)
			return
		}
		listPromptAuditLogsBeforeId(c, userId, beforeId)
		return
	}
	if afterId, exists, cursorErr := getPromptAuditLogCursor(c, "after_id"); exists {
		if cursorErr != nil {
			common.ApiError(c, cursorErr)
			return
		}
		listPromptAuditLogsAfterId(c, userId, afterId)
		return
	}
	pageInfo := common.GetPageQuery(c)
	logs, total, err := model.ListPromptAuditLogsByUserId(userId, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}

func getPromptAuditLogCursor(c *gin.Context, name string) (int, bool, error) {
	raw, exists := c.GetQuery(name)
	if !exists {
		return 0, false, nil
	}
	cursor, err := strconv.Atoi(raw)
	if err != nil || cursor < 0 {
		return 0, true, errors.New("invalid prompt audit log cursor")
	}
	return cursor, true, nil
}

func listPromptAuditLogsBeforeId(c *gin.Context, userId int, beforeId int) {
	pageInfo := common.GetPageQuery(c)
	logs, hasMore, err := model.ListPromptAuditLogsBeforeId(userId, beforeId, pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	nextCursor := beforeId
	if len(logs) > 0 {
		nextCursor = logs[len(logs)-1].Id
	}
	common.ApiSuccess(c, promptAuditLogCursorPage{
		Items:      logs,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func listPromptAuditLogsAfterId(c *gin.Context, userId int, afterId int) {
	pageInfo := common.GetPageQuery(c)
	logs, hasMore, err := model.ListPromptAuditLogsAfterId(userId, afterId, pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	nextCursor := afterId
	if len(logs) > 0 {
		nextCursor = logs[len(logs)-1].Id
	}
	common.ApiSuccess(c, promptAuditLogCursorPage{
		Items:      logs,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	})
}

func DeletePromptAuditLogs(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		common.ApiError(c, errors.New("invalid user id"))
		return
	}
	if _, err = model.GetPromptAuditPolicyByUserId(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	deleted, err := model.DeletePromptAuditLogsByUserId(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": deleted})
}

type testContentModerationAPIKeyRequest struct {
	APIKey    string `json:"api_key"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	TimeoutMS int    `json:"timeout_ms"`
}

// TestContentModerationAPIKey probes Moderations with a draft or stored key
// (same approach as sub2api risk-control api-keys/test: POST /v1/moderations).
func TestContentModerationAPIKey(c *gin.Context) {
	var req testContentModerationAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.TestContentModerationAPIKey(
		c.Request.Context(),
		req.BaseURL,
		req.Model,
		req.APIKey,
		req.TimeoutMS,
	)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func ListContentModerationLogs(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	filter := model.ContentModerationLogFilter{
		Action:   c.Query("action"),
		Category: c.Query("category"),
		Keyword:  c.Query("keyword"),
	}
	if startRaw := c.Query("start_timestamp"); startRaw != "" {
		start, err := strconv.ParseInt(startRaw, 10, 64)
		if err != nil || start < 0 {
			common.ApiError(c, errors.New("invalid start_timestamp"))
			return
		}
		filter.StartTime = start
	}
	if endRaw := c.Query("end_timestamp"); endRaw != "" {
		end, err := strconv.ParseInt(endRaw, 10, 64)
		if err != nil || end < 0 {
			common.ApiError(c, errors.New("invalid end_timestamp"))
			return
		}
		filter.EndTime = end
	}
	logs, total, err := model.ListContentModerationLogs(filter, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(logs)
	common.ApiSuccess(c, pageInfo)
}
