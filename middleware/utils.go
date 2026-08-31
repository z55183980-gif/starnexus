package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func abortWithOpenAiMessage(c *gin.Context, statusCode int, message string, code ...types.ErrorCode) {
	if c != nil && c.Request != nil {
		path := strings.TrimSuffix(c.Request.URL.Path, "/")
		if path == "/api/v3/contents/generations/tasks" || strings.HasPrefix(path, "/api/v3/contents/generations/tasks/") {
			publicCode := "InvalidRequest"
			publicType := "invalid_request_error"
			switch statusCode {
			case http.StatusUnauthorized:
				publicCode = "AuthenticationError"
				publicType = "Unauthorized"
			case http.StatusForbidden:
				publicCode = "PermissionDenied"
				publicType = "Forbidden"
			case http.StatusTooManyRequests:
				publicCode = "QuotaExceeded"
				publicType = "rate_limit_error"
				if len(code) > 0 && (code[0] == types.ErrorCodeGatewayRateLimit || code[0] == types.ErrorCodeUserConcurrencyLimit) {
					publicCode = string(code[0])
					publicType = "new_api_error"
				}
			}
			c.JSON(statusCode, gin.H{"error": gin.H{
				"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
				"type":    publicType, "param": nil, "code": publicCode,
			}})
			c.Abort()
			logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", c.GetInt("id"), message))
			return
		}
	}
	codeStr := ""
	if len(code) > 0 {
		codeStr = string(code[0])
	}
	userId := c.GetInt("id")
	c.JSON(statusCode, gin.H{
		"error": gin.H{
			"message": common.MessageWithRequestId(message, c.GetString(common.RequestIdKey)),
			"type":    "new_api_error",
			"code":    codeStr,
		},
	})
	c.Abort()
	logger.LogError(c.Request.Context(), fmt.Sprintf("user %d | %s", userId, message))
}

func abortWithMidjourneyMessage(c *gin.Context, statusCode int, code int, description string) {
	c.JSON(statusCode, gin.H{
		"description": description,
		"type":        "new_api_error",
		"code":        code,
	})
	c.Abort()
	logger.LogError(c.Request.Context(), description)
}
