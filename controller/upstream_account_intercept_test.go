package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDetectUpstreamClaudeIntercept(t *testing.T) {
	t.Parallel()
	one := uint(1)

	tests := []struct {
		name      string
		request   *dto.ClaudeRequest
		userAgent string
		want      upstreamClaudeInterceptType
	}{
		{name: "warmup", request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "user", Content: "Warmup"}}}, want: upstreamClaudeInterceptWarmup},
		{name: "suggestion", request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "user", Content: "[SUGGESTION MODE: predict]"}}}, want: upstreamClaudeInterceptSuggestion},
		{name: "haiku claude code probe", request: &dto.ClaudeRequest{Model: "claude-3-5-haiku", MaxTokens: &one}, userAgent: "claude-cli/2.1", want: upstreamClaudeInterceptHaikuProbe},
		{name: "haiku non claude client", request: &dto.ClaudeRequest{Model: "claude-3-5-haiku", MaxTokens: &one}, userAgent: "curl/8", want: upstreamClaudeInterceptNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			ctx.Request.Header.Set("User-Agent", test.userAgent)
			require.Equal(t, test.want, detectUpstreamClaudeIntercept(ctx, test.request))
		})
	}
}

func TestTryInterceptUpstreamWarmupStream(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamInterceptWarmup, true)
	stream := true
	request := &dto.ClaudeRequest{Model: "claude-sonnet-4", Stream: &stream, Messages: []dto.ClaudeMessage{{Role: "user", Content: "Warmup"}}}

	require.True(t, tryInterceptUpstreamWarmup(ctx, types.RelayFormatClaude, request))
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	body := recorder.Body.String()
	require.Contains(t, body, "event: message_start")
	require.Contains(t, body, "event: message_stop")
	require.Contains(t, body, "New Conversation")
	require.Equal(t, 1, strings.Count(body, "event: message_start"))
}

func TestTryInterceptUpstreamWarmupDisabled(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	request := &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{{Role: "user", Content: "Warmup"}}}
	require.False(t, tryInterceptUpstreamWarmup(ctx, types.RelayFormatClaude, request))
}
