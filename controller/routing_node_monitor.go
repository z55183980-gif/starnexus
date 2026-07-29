package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const routingNodeMonitorMaxBodyBytes = 64 * 1024

func getRoutingNodeMonitorBearerToken(c *gin.Context) string {
	token := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(token) > 7 && strings.EqualFold(token[:7], "Bearer ") {
		return strings.TrimSpace(token[7:])
	}
	return ""
}

func ReportRoutingNodeMonitor(c *gin.Context) {
	token := getRoutingNodeMonitorBearerToken(c)

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, routingNodeMonitorMaxBodyBytes)
	var report service.RoutingNodeMonitorReport
	if err := common.DecodeJson(c.Request.Body, &report); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid monitor report"})
		return
	}
	if err := service.SubmitRoutingNodeMonitorReport(token, &report); err != nil {
		if errors.Is(err, service.ErrRoutingNodeMonitorUnauthorized) {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "unauthorized"})
			return
		}
		if errors.Is(err, service.ErrInvalidRoutingNodeMonitorReport) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

func EnrollRoutingNodeMonitor(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, routingNodeMonitorMaxBodyBytes)
	var request service.RoutingNodeMonitorEnrollmentRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid enrollment request"})
		return
	}
	result, err := service.EnrollRoutingNodeMonitor(getRoutingNodeMonitorBearerToken(c), request.NodeKey)
	if err != nil {
		if errors.Is(err, service.ErrRoutingNodeMonitorUnauthorized) {
			c.JSON(http.StatusUnauthorized, gin.H{"success": false, "message": "unauthorized"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func RotateRoutingNodeMonitorEnrollmentToken(c *gin.Context) {
	token, err := service.RotateRoutingNodeMonitorEnrollmentToken()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, "rotated routing node monitor enrollment token")
	common.ApiSuccess(c, gin.H{
		"token":       token,
		"enroll_path": "/api/node-monitor/enroll",
		"report_path": "/api/node-monitor/report",
	})
}

func GetRoutingNodeMonitorEnrollmentToken(c *gin.Context) {
	token, err := service.GetRoutingNodeMonitorEnrollmentToken()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"token":       token,
		"enroll_path": "/api/node-monitor/enroll",
		"report_path": "/api/node-monitor/report",
	})
}

func RotateRoutingNodeMonitorToken(c *gin.Context) {
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
	token, err := service.RotateRoutingNodeMonitorToken(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordLog(c.GetInt("id"), model.LogTypeManage, "rotated routing node monitor token "+node.Key)
	common.ApiSuccess(c, gin.H{
		"token":       token,
		"node_key":    node.Key,
		"node_name":   node.Name,
		"report_path": "/api/node-monitor/report",
	})
}
