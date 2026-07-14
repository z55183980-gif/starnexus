package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

type updateUserNodeBindingRequest struct {
	Node string `json:"node"`
}

func getManagedUserForNodeBinding(c *gin.Context) (*model.User, bool) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return nil, false
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if !canManageTargetUser(c.GetInt("id"), c.GetInt("role"), user) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionSameLevel)
		return nil, false
	}
	return user, true
}

func GetUserNodeBinding(c *gin.Context) {
	user, ok := getManagedUserForNodeBinding(c)
	if !ok {
		return
	}
	binding, err := model.GetUserNodeBindingRecord(user.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"user_id":             user.Id,
		"node":                binding.Node,
		"revision":            binding.Revision,
		"tokens_synced":       binding.TokensSynced,
		"configured":          service.IsUserNodeRouterConfigured(),
		"propagation_seconds": 60,
	})
}

func UpdateUserNodeBinding(c *gin.Context) {
	user, ok := getManagedUserForNodeBinding(c)
	if !ok {
		return
	}
	var input updateUserNodeBindingRequest
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	binding, err := service.UpdateUserNodeRoute(c.Request.Context(), user.Id, input.Node)
	if err != nil {
		if errors.Is(err, service.ErrUserNodeRouterNotConfigured) {
			common.ApiErrorMsg(c, "用户节点路由未配置")
			return
		}
		common.ApiError(c, err)
		return
	}
	model.RecordLog(user.Id, model.LogTypeManage, fmt.Sprintf("admin updated user node binding to %s", binding.Node))
	common.ApiSuccess(c, gin.H{
		"user_id":             user.Id,
		"node":                binding.Node,
		"revision":            binding.Revision,
		"tokens_synced":       binding.TokensSynced,
		"configured":          true,
		"propagation_seconds": 60,
	})
}

func refreshUserNodeBindingAsync(userId int) {
	if userId <= 0 {
		return
	}
	gopool.Go(func() {
		if err := service.RefreshStoredUserNodeBinding(context.Background(), userId); err != nil {
			common.SysLog(fmt.Sprintf("failed to refresh user node binding for user %d: %s", userId, err.Error()))
		}
	})
}

func clearUserNodeBindingBestEffort(userId int) {
	if userId <= 0 {
		return
	}
	if service.IsUserNodeRouterConfigured() {
		if err := service.DeleteUserNodeRouting(context.Background(), userId); err != nil {
			common.SysLog(fmt.Sprintf("failed to clear user node binding for user %d: %s", userId, err.Error()))
		}
	}
	if err := model.DeleteUserNodeBinding(userId); err != nil {
		common.SysLog(fmt.Sprintf("failed to delete stored user node binding for user %d: %s", userId, err.Error()))
	}
}
