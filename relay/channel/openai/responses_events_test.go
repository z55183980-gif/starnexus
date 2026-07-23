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
