package controller

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
