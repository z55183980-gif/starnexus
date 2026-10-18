package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestContentTimingIgnoresControlAndThinking(t *testing.T) {
	info := &relaycommon.RelayInfo{StartTime: time.Now().Add(-time.Second), ContentTimingTracked: true}
	for _, frame := range []string{
		`{"type":"ping"}`,
		`{"type":"message_start"}`,
		`{"type":"content_block_start","content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"internal"}}`,
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":""}}`,
		`{"type":"message_delta","usage":{"output_tokens":100}}`,
		`{"type":"message_stop"}`,
	} {
		observeClaudeContentTiming(info, frame)
		require.Nil(t, info.FirstContentMs, frame)
	}
	for _, frame := range []string{
		`{"type":"content_block_delta","delta":{"type":"text_delta","text":"OK"}}`,
		`{"type":"content_block_start","content_block":{"type":"text","text":"OK"}}`,
		`{"type":"content_block_start","content_block":{"type":"tool_use","name":"read_file"}}`,
		`{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{}"}}`,
	} {
		info.FirstContentMs = nil
		observeClaudeContentTiming(info, frame)
		require.NotNil(t, info.FirstContentMs, frame)
		require.InDelta(t, 1000, *info.FirstContentMs, 200)
		first := *info.FirstContentMs
		observeClaudeContentTiming(info, frame)
		require.Equal(t, first, *info.FirstContentMs)
	}
}

func TestHeartbeatDoesNotBecomeContentOrChangeUsage(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.StartTime = time.Now().Add(-10 * time.Millisecond)
	info.StartFirstTokenAttempt(info.StartTime)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatClaude
	info.DisablePing = true
	r, writer := io.Pipe()
	defer writer.Close()
	go func() {
		io.WriteString(writer, "data: {\"type\":\"ping\"}\n\n")
		time.Sleep(80 * time.Millisecond)
		io.WriteString(writer, "data: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-opus-5-5\",\"usage\":{\"input_tokens\":2,\"cache_read_input_tokens\":2259,\"cache_creation_input_tokens\":100,\"cache_creation\":{\"ephemeral_1h_input_tokens\":100}}}}\n\n")
		io.WriteString(writer, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n")
		io.WriteString(writer, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n")
		io.WriteString(writer, "data: {\"type\":\"message_stop\"}\n\n")
	}()
	usage, err := ClaudeStreamHandler(c, &http.Response{Body: r}, info)
	require.Nil(t, err)
	require.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	require.NotNil(t, info.FirstContentMs)
	require.GreaterOrEqual(t, *info.FirstContentMs, int64(70))
	require.Less(t, info.FirstResponseTime.Sub(info.StartTime).Milliseconds(), *info.FirstContentMs)
	require.True(t, info.HasSendResponse(), "transport/retry state remains unchanged")
	require.Equal(t, 2, usage.PromptTokens)
	require.Equal(t, 3, usage.CompletionTokens)
	require.Equal(t, 2259, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, 100, usage.PromptTokensDetails.CachedCreationTokens)
	require.Equal(t, 100, usage.ClaudeCacheCreation1hTokens)
	require.Contains(t, w.Body.String(), `"type":"ping"`, "keepalive must still reach the client")
	info.StartFirstTokenAttempt(time.Now())
	require.Nil(t, info.FirstContentMs)
	require.False(t, info.ContentTimingTracked)
}

func TestFailedContentWriteDoesNotRecordFirstContent(t *testing.T) {
	c, _ := gin.CreateTestContext(&failedClaudeWriter{httptest.NewRecorder()})
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), ContentTimingTracked: true, RelayFormat: types.RelayFormatClaude}
	err := HandleStreamResponseData(c, info, &ClaudeResponseInfo{Usage: &dto.Usage{}, ResponseText: strings.Builder{}}, `{"type":"content_block_delta","delta":{"type":"text_delta","text":"OK"}}`)
	require.NotNil(t, err)
	require.Nil(t, info.FirstContentMs)
}
