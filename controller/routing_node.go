package controller

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type routingNodeRequest struct {
	Key            string `json:"key"`
	Name           string `json:"name"`
	Origin         string `json:"origin"`
	Enabled        bool   `json:"enabled"`
	Sort           int    `json:"sort"`
	MonitorEnabled bool   `json:"monitor_enabled"`
}

func GetRoutingNodes(c *gin.Context) {
	includeDisabled := c.GetInt("role") == common.RoleRootUser && c.Query("all") == "true"
	nodes, err := model.ListRoutingNodes(includeDisabled)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nodes)
}

func GetRoutingNodeBoundUsers(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	node, err := model.GetRoutingNodeById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo := common.GetPageQuery(c)
	users, total, err := model.ListRoutingNodeBoundUsers(node.Key, pageInfo)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"items":     users,
		"total":     total,
		"page":      pageInfo.GetPage(),
		"page_size": pageInfo.GetPageSize(),
	})
}

func CreateRoutingNode(c *gin.Context) {
	var input routingNodeRequest
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	key, err := model.NormalizeRoutingNodeKey(input.Key)
	if err != nil || strings.TrimSpace(input.Name) == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	origin, err := service.NormalizeRoutingOrigin(input.Origin)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	node := &model.RoutingNode{
		Key:            key,
		Name:           input.Name,
		Origin:         origin,
		Enabled:        input.Enabled,
		Sort:           input.Sort,
		MonitorEnabled: input.MonitorEnabled,
	}
	if err := model.CreateRoutingNode(node); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("created routing node %s", node.Key))
	common.ApiSuccess(c, node)
}

func UpdateRoutingNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	current, err := model.GetRoutingNodeById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var input routingNodeRequest
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || strings.TrimSpace(input.Name) == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	origin, err := service.NormalizeRoutingOrigin(input.Origin)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !input.Enabled {
		count, countErr := model.CountUserNodeBindingsByNode(current.Key)
		if countErr != nil {
			common.ApiError(c, countErr)
			return
		}
		if count > 0 {
			common.ApiErrorMsg(c, "routing node still has bound users")
			return
		}
	}
	originChanged := current.Origin != origin
	current.Name = input.Name
	current.Origin = origin
	current.Enabled = input.Enabled
	current.Sort = input.Sort
	current.MonitorEnabled = input.MonitorEnabled
	if err := model.UpdateRoutingNode(current); err != nil {
		common.ApiError(c, err)
		return
	}
	if originChanged {
		service.TriggerUserNodeRoutingReconcile()
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("updated routing node %s", current.Key))
	common.ApiSuccess(c, current)
}

func DeleteRoutingNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	node, err := model.GetRoutingNodeById(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DeleteRoutingNode(id); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, fmt.Sprintf("deleted routing node %s", node.Key))
	common.ApiSuccess(c, nil)
}

func ReconcileRoutingNodes(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"started": service.TriggerUserNodeRoutingReconcile()})
}
