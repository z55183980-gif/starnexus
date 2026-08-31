package controller

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

var upstreamProviderNamePattern = regexp.MustCompile(`(?i)zqbapi`)
var publicErrorURLPattern = regexp.MustCompile(`https?://[^\s"']+`)

// sanitizePublicErrorMessage prevents provider implementation names from
// leaking through client-facing API errors while preserving the original
// error for internal logging and diagnostics.
func sanitizePublicErrorMessage(message string) string {
	message = upstreamProviderNamePattern.ReplaceAllString(message, "video provider")
	return publicErrorURLPattern.ReplaceAllString(message, "[upstream URL hidden]")
}

func isOpenAIVideoPublicRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	path := c.Request.URL.Path
	if path != "/v1/videos" && !strings.HasPrefix(path, "/v1/videos/") {
		return false
	}
	if c.GetBool(string(constant.ContextKeyOpenAIVideoRequest)) {
		return true
	}
	if c.GetBool(string(constant.ContextKeyZQBAPIOpenAIVideoRequest)) {
		return true
	}
	channelType := common.GetContextKeyInt(c, constant.ContextKeyChannelType)
	return channelType == constant.ChannelTypeZQBAPI || channelType == constant.ChannelTypeDoubaoVideo2
}

func isNativeSeedancePublicRequest(c *gin.Context) bool {
	if c == nil || c.Request == nil {
		return false
	}
	path := strings.TrimSuffix(c.Request.URL.Path, "/")
	return path == "/api/v3/contents/generations/tasks" ||
		strings.HasPrefix(path, "/api/v3/contents/generations/tasks/")
}

func nativeSeedancePublicErrorCode(taskErr *dto.TaskError) string {
	if taskErr == nil {
		return "InternalServiceError"
	}
	// A local compatibility/concurrency error may also use HTTP 429. Preserve
	// its specific code instead of exposing the provider-facing QuotaExceeded
	// code, which would falsely attribute the rejection to upstream quota.
	if taskErr.LocalError && strings.TrimSpace(taskErr.Code) != "" {
		return taskErr.Code
	}
	switch taskErr.StatusCode {
	case http.StatusUnauthorized:
		return "AuthenticationError"
	case http.StatusForbidden:
		return "PermissionDenied"
	case http.StatusNotFound:
		return "NotFound"
	case http.StatusTooManyRequests:
		return "QuotaExceeded"
	}
	if taskErr.StatusCode >= http.StatusInternalServerError {
		return "InternalServiceError"
	}
	switch taskErr.Code {
	case "invalid_duration", "unsupported_duration":
		return "InvalidParameter.Duration"
	case "invalid_content":
		return "InvalidParameter.Content"
	case "unsupported_model", "model_mapping_failed":
		return "InvalidParameter.Model"
	case "task_not_exist", "video_not_found":
		return "NotFound"
	case "read_request_body_failed":
		if taskErr.StatusCode == http.StatusRequestEntityTooLarge {
			return "RequestTooLarge"
		}
		return "InvalidRequest"
	case "invalid_request", "unsupported_channel":
		return "InvalidParameter"
	default:
		if strings.TrimSpace(taskErr.Code) != "" {
			return taskErr.Code
		}
		return "InvalidRequest"
	}
}

func writeNativeSeedanceError(c *gin.Context, taskErr *dto.TaskError) {
	message := sanitizePublicErrorMessage(taskErr.Message)
	if taskErr.StatusCode >= http.StatusInternalServerError {
		message = "The video request could not be completed because of an internal error."
	}
	writeOpenAIVideoError(c, taskErr.StatusCode, nativeSeedancePublicErrorCode(taskErr), "", message)
}

func openAIVideoErrorType(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate_limit_error"
	case status >= http.StatusInternalServerError:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

func writeOpenAIVideoError(c *gin.Context, status int, code, param, message string) {
	var publicParam any
	if strings.TrimSpace(param) != "" {
		publicParam = param
	}
	c.JSON(status, gin.H{"error": gin.H{
		"message": sanitizePublicErrorMessage(message),
		"type":    openAIVideoErrorType(status),
		"param":   publicParam,
		"code":    code,
	}})
}
