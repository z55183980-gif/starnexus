package openai

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesEventAccumulatorCompletedUsage(t *testing.T) {
	t.Parallel()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
		ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{
			BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
				dto.BuildInToolWebSearchPreview: &relaycommon.BuildInToolInfo{ToolName: dto.BuildInToolWebSearchPreview},
			},
		},
	}
	accumulator := NewResponsesEventAccumulator()

	_, err := accumulator.Consume(ctx, info, []byte(`{"type":"response.output_item.done","item":{"type":"web_search_call"}}`))
	require.NoError(t, err)
	_, err = accumulator.Consume(ctx, info, []byte(`{"type":"response.completed","response":{"id":"resp_123","usage":{"input_tokens":12,"output_tokens":7,"total_tokens":19,"input_tokens_details":{"cached_tokens":5,"cache_write_tokens":2}}}}`))
	require.NoError(t, err)

	usage := accumulator.Usage(info)
	require.True(t, accumulator.Terminal())
	require.True(t, accumulator.Successful())
	require.Equal(t, "resp_123", accumulator.ResponseID())
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 7, usage.CompletionTokens)
	require.Equal(t, 19, usage.TotalTokens)
	require.Equal(t, 5, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 2, usage.PromptTokensDetails.CacheWriteTokens)
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
}

func TestResponsesEventAccumulatorErrorIsTerminal(t *testing.T) {
	t.Parallel()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	accumulator := NewResponsesEventAccumulator()
	_, err := accumulator.Consume(ctx, &relaycommon.RelayInfo{}, []byte(`{"type":"error","error":{"message":"failed"}}`))
	require.NoError(t, err)
	require.True(t, accumulator.Terminal())
	require.True(t, accumulator.Failed())
	require.False(t, accumulator.Successful())
}

func TestIsResponsesFirstOutputEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event *dto.ResponsesStreamResponse
		want  bool
	}{
		{name: "nil event", event: nil, want: false},
		{name: "empty type", event: &dto.ResponsesStreamResponse{}, want: false},
		{name: "websocket timing", event: &dto.ResponsesStreamResponse{Type: "responsesapi.websocket_timing"}, want: false},
		{name: "response created", event: &dto.ResponsesStreamResponse{Type: "response.created"}, want: false},
		{name: "response in progress", event: &dto.ResponsesStreamResponse{Type: "response.in_progress"}, want: false},
		{name: "output item added", event: &dto.ResponsesStreamResponse{Type: dto.ResponsesOutputTypeItemAdded}, want: false},
		{name: "output item done", event: &dto.ResponsesStreamResponse{Type: dto.ResponsesOutputTypeItemDone}, want: false},
		{name: "empty text delta", event: &dto.ResponsesStreamResponse{Type: "response.output_text.delta"}, want: true},
		{name: "text delta", event: &dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "hello"}, want: true},
		{name: "reasoning summary delta", event: &dto.ResponsesStreamResponse{Type: "response.reasoning_summary_text.delta", Delta: "thinking"}, want: true},
		{name: "function arguments delta", event: &dto.ResponsesStreamResponse{Type: "response.function_call_arguments.delta", Delta: "{"}, want: true},
		{name: "output text done", event: &dto.ResponsesStreamResponse{Type: "response.output_text.done"}, want: true},
		{name: "generic response output", event: &dto.ResponsesStreamResponse{Type: "response.output"}, want: true},
		{name: "response completed", event: &dto.ResponsesStreamResponse{Type: "response.completed"}, want: false},
		{name: "error", event: &dto.ResponsesStreamResponse{Type: "error"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsResponsesFirstOutputEvent(tt.event))
		})
	}
}
