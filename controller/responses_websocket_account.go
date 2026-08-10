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
	service.RecordUpstreamAccountSuccess(common.GetContextKeyInt(turn.ctx, appconstant.ContextKeyUpstreamAccountId))
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
	case strings.Contains(combined, "rate_limit") || strings.Contains(combined, "too many requests"):
		status = http.StatusTooManyRequests
	case strings.Contains(combined, "unauthorized") || strings.Contains(combined, "token_invalidated") || strings.Contains(combined, "token_revoked"):
		status = http.StatusUnauthorized
	case strings.Contains(combined, "overload") || strings.Contains(combined, "capacity"):
		status = 529
	}
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCodeBadResponseStatusCode, status, types.ErrOptionWithSkipRetry())
}
