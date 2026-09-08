package service

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

// IsUpstreamCyberPolicyError deliberately matches only the provider's
// structured error code. Human-readable messages are not stable enough to be
// used as an account suspension signal.
func IsUpstreamCyberPolicyError(apiErr *types.NewAPIError) bool {
	if apiErr == nil {
		return false
	}
	return strings.EqualFold(
		strings.TrimSpace(string(apiErr.GetErrorCode())),
		string(types.ErrorCodeCyberPolicy),
	)
}

func NormalizeUpstreamCyberPolicyError(apiErr *types.NewAPIError) *types.NewAPIError {
	if !IsUpstreamCyberPolicyError(apiErr) {
		return apiErr
	}
	openAIError := apiErr.ToOpenAIError()
	openAIError.Code = string(types.ErrorCodeCyberPolicy)
	if strings.TrimSpace(openAIError.Type) == "" {
		openAIError.Type = "policy_error"
	}
	normalized := types.WithOpenAIError(
		openAIError,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
	header, body := apiErr.UpstreamResponse()
	normalized.SetUpstreamResponse(header, body)
	return normalized
}

func SuspendUserAPIForUpstreamCyberPolicy(c *gin.Context, apiErr *types.NewAPIError, modelName string) bool {
	if c == nil || !IsUpstreamCyberPolicyError(apiErr) {
		return false
	}
	userId := common.GetContextKeyInt(c, constant.ContextKeyUserId)
	if userId <= 0 {
		return false
	}
	openAIError := apiErr.ToOpenAIError()
	endpoint := ""
	if c.Request != nil && c.Request.URL != nil {
		endpoint = c.Request.URL.Path
	}
	evidenceBytes, _ := common.Marshal(map[string]any{
		"error_code":  strings.TrimSpace(fmt.Sprint(openAIError.Code)),
		"error_type":  strings.TrimSpace(openAIError.Type),
		"status_code": apiErr.StatusCode,
		"endpoint":    endpoint,
	})
	changed, err := model.SuspendUserAPIForCyberPolicy(model.SuspendUserAPIInput{
		UserId:            userId,
		Reason:            string(types.ErrorCodeCyberPolicy),
		RequestId:         c.GetString(common.RequestIdKey),
		TokenId:           common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		ModelName:         strings.TrimSpace(modelName),
		ChannelId:         common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		UpstreamAccountId: common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId),
		NodeName:          common.NodeName,
		Evidence:          string(evidenceBytes),
	})
	if err != nil {
		logger.LogError(c, fmt.Sprintf("failed to suspend API access for cyber_policy user %d: %v", userId, err))
		return false
	}
	InvalidatePromptAuditPolicyCache()
	if !changed {
		return false
	}
	if err := model.InvalidateUserCache(userId); err != nil {
		logger.LogWarn(c, fmt.Sprintf("failed to invalidate suspended user %d cache: %v", userId, err))
	}
	if err := model.InvalidateUserTokensCache(userId); err != nil {
		logger.LogWarn(c, fmt.Sprintf("failed to invalidate suspended user %d token cache: %v", userId, err))
	}
	logger.LogWarn(c, fmt.Sprintf("API access suspended for user %d after structured cyber_policy response", userId))
	return true
}
