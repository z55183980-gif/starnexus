package controller

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

const maxBatchUserCount = 1000

type BatchUpdateUserGroupRequest struct {
	Ids   []int  `json:"ids"`
	Group string `json:"group"`
}

func normalizeBatchUpdateUserGroupRequest(req *BatchUpdateUserGroupRequest) bool {
	if req == nil {
		return false
	}

	req.Group = strings.TrimSpace(req.Group)
	seen := make(map[int]struct{}, len(req.Ids))
	ids := make([]int, 0, len(req.Ids))
	for _, id := range req.Ids {
		if id <= 0 {
			return false
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	req.Ids = ids

	return len(req.Ids) > 0 &&
		len(req.Ids) <= maxBatchUserCount &&
		req.Group != "" &&
		utf8.RuneCountInString(req.Group) <= 64
}

// BatchUpdateUserGroup performs an all-or-nothing permission check before
// updating the selected users' ownership group.
func BatchUpdateUserGroup(c *gin.Context) {
	var req BatchUpdateUserGroupRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || !normalizeBatchUpdateUserGroupRequest(&req) {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}

	users, err := model.GetUsersByIdsUnscoped(req.Ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(users) != len(req.Ids) {
		common.ApiErrorI18n(c, i18n.MsgUserNotExists)
		return
	}

	myId := c.GetInt("id")
	myRole := c.GetInt("role")
	for _, user := range users {
		if !canManageTargetUser(myId, myRole, user) {
			common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
			return
		}
		if user.DeletedAt.Valid {
			common.ApiErrorI18n(c, i18n.MsgInvalidParams)
			return
		}
	}

	if err := model.BatchUpdateUserGroup(req.Ids, req.Group); err != nil {
		common.ApiError(c, err)
		return
	}

	adminInfo := map[string]interface{}{
		"admin_id":       myId,
		"admin_username": c.GetString("username"),
	}
	for _, user := range users {
		model.RecordLogWithAdminInfo(
			user.Id,
			model.LogTypeManage,
			fmt.Sprintf("管理员批量调整用户分组从 %s 为 %s", user.Group, req.Group),
			adminInfo,
		)
		if err := model.InvalidateUserCache(user.Id); err != nil {
			common.SysLog(fmt.Sprintf("failed to invalidate user cache for user %d: %s", user.Id, err.Error()))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"updated_count": len(users),
		},
	})
}
