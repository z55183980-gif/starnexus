package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// StreamBusinessMonitorLogs streams committed log events to an authenticated
// admin. PostgreSQL remains the source of truth; the frontend periodically
// refreshes its snapshot to compensate for missed stream events.
func StreamBusinessMonitorLogs(c *gin.Context) {
	if !common.RedisEnabled || common.RDB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"success": false,
			"message": "redis is required for live log streaming",
		})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache, no-transform")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	lastID := c.Query("last_id")
	if lastID == "" {
		lastID = c.GetHeader("Last-Event-ID")
	}
	lastID = strings.TrimSpace(lastID)

	for {
		if c.Request.Context().Err() != nil {
			return
		}

		messages, err := common.ReadBusinessMonitorLogStream(
			c.Request.Context(),
			lastID,
			100,
			15*time.Second,
		)
		if err != nil {
			if c.Request.Context().Err() != nil {
				return
			}
			c.Error(err)
			return
		}

		if len(messages) == 0 {
			if _, err = fmt.Fprint(c.Writer, ": heartbeat\n\n"); err != nil {
				return
			}
			if flusher, ok := c.Writer.(http.Flusher); ok {
				flusher.Flush()
			}
			continue
		}

		for _, message := range messages {
			payload, ok := message.Values["event"].(string)
			if !ok || payload == "" {
				lastID = message.ID
				continue
			}
			eventName := "business-monitor-log"
			if eventType, ok := message.Values["type"].(string); ok {
				switch eventType {
				case "alert":
					eventName = "business-monitor-alert"
				case "concurrency":
					eventName = "business-monitor-concurrency"
				case "node-alert":
					eventName = "business-monitor-node-alert"
				}
			}
			if _, err = fmt.Fprintf(c.Writer, "id: %s\nevent: %s\ndata: %s\n\n", message.ID, eventName, payload); err != nil {
				return
			}
			lastID = message.ID
		}
		if flusher, ok := c.Writer.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

func GetBusinessMonitorConcurrency(c *gin.Context) {
	count, err := common.BusinessMonitorConcurrencyCount(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"concurrency": count,
			"available":   common.RedisEnabled && common.RDB != nil,
		},
	})
}

func GetBusinessMonitorCacheHitRate(c *gin.Context) {
	endTimestamp := time.Now().Unix()
	startTimestamp := endTimestamp - int64((24*time.Hour)/time.Second)
	modelName := strings.TrimSpace(c.Query("model_name"))
	stats, err := model.GetBusinessMonitorCacheStats(startTimestamp, endTimestamp, modelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"input_tokens":              stats.InputTokens,
			"cache_read_tokens":         stats.CacheReadTokens,
			"billing_cache_read_tokens": stats.BillingCacheReadTokens,
			"cache_creation_tokens":     stats.CacheCreationTokens,
			"window_seconds":            int64((24 * time.Hour) / time.Second),
		},
	})
}

func GetBusinessMonitorAlerts(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	alerts, err := model.ListErrorAlerts(c.Query("status"), limit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    alerts,
	})
}

func GetBusinessMonitorAlertLog(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	log, err := model.GetErrorAlertLatestLog(uint(id))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, log)
}

func AcknowledgeBusinessMonitorAlert(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	input := struct {
		LastLogID int `json:"last_log_id"`
	}{}
	if c.Request.ContentLength != 0 {
		if err = common.DecodeJson(c.Request.Body, &input); err != nil {
			common.ApiError(c, err)
			return
		}
	}
	alert, err := model.AcknowledgeErrorAlert(uint(id), c.GetInt("id"), input.LastLogID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err = common.PublishBusinessMonitorEvent("alert", alert); err != nil {
		common.SysLog("failed to publish acknowledged business monitor alert: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": alert})
}

func AcknowledgeAllBusinessMonitorAlerts(c *gin.Context) {
	alerts, err := model.AcknowledgeAllErrorAlerts(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	for i := range alerts {
		if err = common.PublishBusinessMonitorEvent("alert", &alerts[i]); err != nil {
			common.SysLog("failed to publish acknowledged business monitor alert: " + err.Error())
		}
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": alerts})
}

func ResolveBusinessMonitorAlert(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	alert, err := model.ResolveErrorAlert(uint(id))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err = common.PublishBusinessMonitorEvent("alert", alert); err != nil {
		common.SysLog("failed to publish resolved business monitor alert: " + err.Error())
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": alert})
}
