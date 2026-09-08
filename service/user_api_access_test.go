package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestNormalizeUpstreamCyberPolicyErrorRequiresStructuredCode(t *testing.T) {
	structured := types.WithOpenAIError(types.OpenAIError{
		Message: "This content was flagged for possible cybersecurity risk",
		Type:    "invalid_request_error",
		Code:    "cyber_policy",
	}, http.StatusInternalServerError)

	normalized := NormalizeUpstreamCyberPolicyError(structured)
	require.Equal(t, http.StatusBadRequest, normalized.StatusCode)
	require.Equal(t, types.ErrorCodeCyberPolicy, normalized.GetErrorCode())
	require.True(t, types.IsSkipRetryError(normalized))

	messageOnly := types.WithOpenAIError(types.OpenAIError{
		Message: "cyber_policy appeared only in message text",
		Code:    "server_error",
	}, http.StatusInternalServerError)
	require.False(t, IsUpstreamCyberPolicyError(messageOnly))
	require.Same(t, messageOnly, NormalizeUpstreamCyberPolicyError(messageOnly))
}
