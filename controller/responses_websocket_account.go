package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
)

func recordResponsesWSNativeSuccess(turn *responsesWebSocketTurn) {
	if turn == nil || turn.ctx == nil {
		return
	}
	service.RecordUpstreamAccountSuccessForModel(
		common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId),
		turn.info.UpstreamModelName,
	)
	recordUpstreamRequestEvent(turn.ctx, "request_success", "success", "")
}

func recordResponsesWSNativeFailure(turn *responsesWebSocketTurn, apiErr *types.NewAPIError) {
	if turn == nil || turn.ctx == nil || apiErr == nil {
		return
	}
	accountID := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId)
	proxyID := common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamProxyId)
	service.ApplyUpstreamAccountError(accountID, proxyID, apiErr)
	recordUpstreamRequestEvent(turn.ctx, "request_error", "error", service.UpstreamAccountErrorSummary(apiErr))
	recordRelayErrorLog(turn.ctx, apiErr)
}

func recordResponsesWSCapacityRetryDecision(turn *responsesWebSocketTurn, decision responsesWSCapacityRetryDecision) {
	if turn == nil || turn.ctx == nil {
		return
	}
	result := "skipped"
	if decision.Eligible {
		result = "allowed"
	}
	recordUpstreamRequestEventWithMetadata(
		turn.ctx,
		"capacity_retry_decision",
		result,
		decision.Reason,
		map[string]any{
			"reason":                  decision.Reason,
			"terminal_type":           decision.TerminalType,
			"retry_index":             turn.capacityRetryCount,
			"account_failovers":       turn.accountFailovers,
			"upstream_event_count":    turn.upstreamEventCount,
			"safe_prelude_count":      turn.capacitySafeEventCount,
			"semantic_output_started": turn.capacityRetryBlocked,
			"usage_reported":          turn.accumulator != nil && turn.accumulator.UsageReported(),
		},
	)
}

func responsesWSTerminalAPIError(turn *responsesWebSocketTurn) *types.NewAPIError {
	if turn == nil || turn.accumulator == nil {
		return types.NewErrorWithStatusCode(errors.New("upstream Responses WebSocket terminal failure"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	openAIError := turn.accumulator.FailureError()
	message := strings.TrimSpace(turn.accumulator.TerminalEventType())
	errorType := ""
	errorCode := ""
	if openAIError != nil {
		if strings.TrimSpace(openAIError.Message) != "" {
			message = strings.TrimSpace(openAIError.Message)
		}
		errorType = strings.ToLower(strings.TrimSpace(openAIError.Type))
		errorCode = strings.ToLower(strings.TrimSpace(fmt.Sprint(openAIError.Code)))
	}
	if message == "" {
		message = "upstream Responses WebSocket terminal failure"
	}
	combined := strings.ToLower(errorCode + " " + errorType + " " + message)
	status := http.StatusBadRequest
	switch {
	case isResponsesWSRateLimitError(errorCode, errorType):
		status = http.StatusTooManyRequests
	case strings.Contains(combined, "unauthorized") || strings.Contains(combined, "token_invalidated") || strings.Contains(combined, "token_revoked"):
		status = http.StatusUnauthorized
	case strings.Contains(combined, "overload") || strings.Contains(combined, "capacity") || strings.Contains(combined, "slow_down"):
		status = 529
	}
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseStatusCode, status, types.ErrOptionWithSkipRetry())
}

// isResponsesWSRateLimitError deliberately only accepts structured provider
// fields. Matching arbitrary message text (for example, "too many requests"
// in a diagnostic sentence) caused non-rate-limit terminal failures to be
// reported as upstream 429s.
func isResponsesWSRateLimitError(errorCode, errorType string) bool {
	switch strings.ToLower(strings.TrimSpace(errorCode)) {
	case "rate_limit", "rate_limit_exceeded", "rate_limited", "ratelimit":
		return true
	}
	switch strings.ToLower(strings.TrimSpace(errorType)) {
	case "rate_limit_error", "rate_limit":
		return true
	default:
		return false
	}
}
