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
