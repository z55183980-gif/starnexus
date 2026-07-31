package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetrySkipsUpstreamContextLengthExceeded(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Your input exceeds the context window of this model.",
		Type:    "invalid_request_error",
		Code:    string(types.ErrorCodeContextLengthExceeded),
	}, http.StatusBadGateway)

	require.Equal(t, types.ErrorCodeContextLengthExceeded, apiErr.GetErrorCode())
	require.False(t, shouldRetry(c, apiErr, 3))
}

func TestShouldRetryKeepsOrdinaryBadGatewayRetryable(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "temporary upstream failure",
		Type:    "upstream_error",
		Code:    "temporary_upstream_error",
	}, http.StatusBadGateway)

	require.True(t, shouldRetry(c, apiErr, 3))
}
