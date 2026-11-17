package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func runClaudeStream(t *testing.T, body string) (*gin.Context, *dto.Usage) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatClaude
	info.DisablePing = true
	info.SetEstimatePromptTokens(957866)
	usage, err := ClaudeStreamHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, info)
	require.Nil(t, err)
	return c, usage
}

// A gateway replay of a completed response (x-wrapper-replayed) streams the
// stored content with every usage field zeroed. That zero is the upstream's
// final account and must not be replaced by a local estimate of the prompt.
func TestClaudeStreamZeroReportedUsageIsNotEstimated(t *testing.T) {
	zero := `{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`
	body := "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-opus-5-5\",\"usage\":" + zero + "}}\n\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"replayed answer text\"}}\n\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":" + zero + "}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	c, usage := runClaudeStream(t, body)
	require.Zero(t, usage.PromptTokens)
	require.Zero(t, usage.CompletionTokens)
	require.Zero(t, usage.PromptTokensDetails.CachedTokens)
	require.Zero(t, usage.PromptTokensDetails.CachedCreationTokens)
	require.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}

// Without a stated output_tokens the upstream never gave a final account, so
// the existing local estimate still applies.
func TestClaudeStreamMissingFinalUsageStillEstimates(t *testing.T) {
	body := "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-opus-5-5\"}}\n\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello there\"}}\n\n" +
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{}}\n\n" +
		"data: {\"type\":\"message_stop\"}\n\n"
	c, usage := runClaudeStream(t, body)
	require.Equal(t, 957866, usage.PromptTokens)
	require.Positive(t, usage.CompletionTokens)
	require.True(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
}
