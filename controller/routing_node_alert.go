package controller

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetRoutingNodeAlertRules(c *gin.Context) {
	rules, err := model.ListRoutingNodeAlertRules(false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rules)
}

func GetRoutingNodeAlerts(c *gin.Context) {
	nodeId, _ := strconv.Atoi(c.Query("node_id"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	alerts, err := model.ListRoutingNodeAlerts(c.Query("status"), c.Query("severity"), nodeId, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, alerts)
}

func GetRoutingNodeAlertUnreadSummary(c *gin.Context) {
	summary, err := model.GetRoutingNodeAlertUnreadSummary(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

func MarkRoutingNodeAlertsRead(c *gin.Context) {
	summary, err := model.MarkRoutingNodeAlertsRead(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, summary)
}

func GetRoutingNodeAlertEvents(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	events, err := model.ListRoutingNodeAlertEvents(id, limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, events)
}

func AcknowledgeRoutingNodeAlert(c *gin.Context) {
	mutateRoutingNodeAlert(c, model.RoutingNodeAlertEventAcknowledged, 0)
}

func SilenceRoutingNodeAlert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	input := struct {
		Seconds int64 `json:"seconds"`
	}{}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || input.Seconds < 0 || input.Seconds > 30*24*60*60 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	eventType := model.RoutingNodeAlertEventSilenced
	silencedUntil := common.GetTimestamp() + input.Seconds
	if input.Seconds == 0 {
		eventType = model.RoutingNodeAlertEventUnsilenced
		silencedUntil = 0
	}
	alert, err := model.MutateRoutingNodeAlert(id, c.GetInt("id"), eventType, silencedUntil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	publishRoutingNodeAlert(alert)
	common.ApiSuccess(c, alert)
}

func ResolveRoutingNodeAlert(c *gin.Context) {
	mutateRoutingNodeAlert(c, model.RoutingNodeAlertEventResolved, 0)
}

func mutateRoutingNodeAlert(c *gin.Context, eventType string, silencedUntil int64) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	alert, err := model.MutateRoutingNodeAlert(id, c.GetInt("id"), eventType, silencedUntil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	publishRoutingNodeAlert(alert)
	common.ApiSuccess(c, alert)
}

func publishRoutingNodeAlert(alert *model.RoutingNodeAlertState) {
	if err := common.PublishBusinessMonitorEvent("node-alert", alert); err != nil {
		common.SysLog("failed to publish routing node alert mutation: " + err.Error())
	}
}
