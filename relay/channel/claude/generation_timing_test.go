package claude

import (
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

func TestGenerationTimingPreservesProtocolAndResponseClock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.RelayFormat = types.RelayFormatClaude
	info.StartTime = time.Now().Add(-12 * time.Second)
	info.StartFirstTokenAttempt(time.Now().Add(-10 * time.Second))
	info.SetFirstResponseTime()
	responseClock := info.FirstResponseTime
	usage := &dto.Usage{PromptTokens: 12, CompletionTokens: 30}
	ci := &ClaudeResponseInfo{Usage: usage, ResponseText: strings.Builder{}}
	err := HandleStreamResponseData(c, info, ci, `{"type":"message_stop","gateway_timing":{"version":1,"first_content_age_ms":9000}}`)
	require.Nil(t, err)
	require.NotNil(t, info.GenerationFirstTokenMs)
	require.InDelta(t, 1000, *info.GenerationFirstTokenMs, 100)
	require.Equal(t, responseClock, info.FirstResponseTime)
	require.True(t, info.HasSendResponse())
	require.Equal(t, 12, usage.PromptTokens)
	require.Equal(t, 30, usage.CompletionTokens)
	require.Contains(t, w.Body.String(), `data: {"type":"message_stop"}`)
	require.NotContains(t, w.Body.String(), "gateway_timing")
	info.StartFirstTokenAttempt(time.Now())
	require.Nil(t, info.GenerationFirstTokenMs, "retry must not inherit previous attempt telemetry")
}

func TestGenerationTimingMissingInvalidAndNonterminalReceipts(t *testing.T) {
	for _, metadata := range []string{`null`, `{"version":2,"first_content_age_ms":9000}`, `{"version":1,"first_content_age_ms":-1}`, `{"version":1,"first_content_age_ms":100000}`, `{"version":1,"first_content_age_ms":1.5}`, `{"version":1,"first_content_age_ms":"9000"}`, `{"version":true,"first_content_age_ms":9000}`} {
		info := &relaycommon.RelayInfo{StartTime: time.Now().Add(-10 * time.Second)}
		clean := consumeGatewayGenerationTiming(info, `{"type":"message_stop","gateway_timing":`+metadata+`}`)
		require.Nil(t, info.GenerationFirstTokenMs, metadata)
		require.NotContains(t, clean, "gateway_timing")
	}
	info := &relaycommon.RelayInfo{StartTime: time.Now().Add(-10 * time.Second)}
	for _, typ := range []string{"message_start", "error", "content_block_delta"} {
		clean := consumeGatewayGenerationTiming(info, `{"type":"`+typ+`","gateway_timing":{"version":1,"first_content_age_ms":9000}}`)
		require.NotContains(t, clean, "gateway_timing")
		require.Nil(t, info.GenerationFirstTokenMs)
	}
	original := `{"type":"message_stop"}`
	require.Equal(t, original, consumeGatewayGenerationTiming(info, original))
	require.Nil(t, info.GenerationFirstTokenMs)
	consumeGatewayGenerationTiming(info, `{"type":"message_stop","gateway_timing":{"version":1,"first_content_age_ms":0}}`)
	require.NotNil(t, info.GenerationFirstTokenMs)
	require.InDelta(t, 10000, *info.GenerationFirstTokenMs, 100)
}
