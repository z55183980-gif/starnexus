package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexMissingStatusRetryConvertedRequest(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, ChannelMeta: &relaycommon.ChannelMeta{}}
	request := dto.OpenAIResponsesRequest{Input: []byte(`[
		{"type":"reasoning","id":"item_local_reasoning","status":"completed"},
		{"type":"message","role":"user","content":"hi"},
		{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}","status":"completed"},
		{"type":"function_call_output","call_id":"call_1","output":{"status":"nested"},"status":"completed"}
	]`)}
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, request)
	require.NoError(t, err)
	first, err := (&Adaptor{}).FinalizeOutboundJSONBody(ctx, info, mustMarshalStatusRetryRequest(t, converted))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(first, "input.1.status").Exists())
	require.False(t, gjson.GetBytes(first, "input.2.status").Exists())
	require.Equal(t, "nested", gjson.GetBytes(first, "input.2.output.status").String())
	require.False(t, gjson.GetBytes(first, "input.0.status").Exists())

	itemType, ok := ArmCodexMissingStatusRetry(ctx, 1)
	require.True(t, ok)
	require.Equal(t, "function_call", itemType)
	converted, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, request)
	require.NoError(t, err)
	retry, err := (&Adaptor{}).FinalizeOutboundJSONBody(ctx, info, mustMarshalStatusRetryRequest(t, converted))
	require.NoError(t, err)
	require.Equal(t, "completed", gjson.GetBytes(retry, "input.1.status").String())
	require.False(t, gjson.GetBytes(retry, "input.2.status").Exists())
	require.Equal(t, "nested", gjson.GetBytes(retry, "input.2.output.status").String())
	require.NotEqual(t, string(first), string(retry))
}

func TestCodexMissingStatusRetryAccountPassthrough(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}
	body := []byte(`{"model":"gpt-6-sol","input":[{"type":"message","role":"assistant","content":"hi","status":"completed"},{"type":"message","role":"user","content":"next"}]}`)
	first, err := RepairAccountPassthroughResponsesBody(ctx, body)
	require.NoError(t, err)
	first, err = (&Adaptor{}).FinalizeOutboundJSONBody(ctx, info, first)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(first, "input.0.status").Exists())
	_, ok := ArmCodexMissingStatusRetry(ctx, 0)
	require.True(t, ok)
	retry, err := RepairAccountPassthroughResponsesBody(ctx, body)
	require.NoError(t, err)
	retry, err = (&Adaptor{}).FinalizeOutboundJSONBody(ctx, info, retry)
	require.NoError(t, err)
	require.Equal(t, "completed", gjson.GetBytes(retry, "input.0.status").String())
}

func TestCodexMissingStatusRetryDoesNotInventOrMisplaceStatus(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}
	body := []byte(`{"input":[{"type":"message","role":"user","content":"hi"}]}`)
	_, err := RepairAccountPassthroughResponsesBody(ctx, body)
	require.NoError(t, err)
	_, ok := ArmCodexMissingStatusRetry(ctx, 0)
	require.False(t, ok)

	withStatus := []byte(`{"input":[{"type":"message","role":"assistant","content":"hi","status":"completed"}]}`)
	first, err := RepairAccountPassthroughResponsesBody(ctx, withStatus)
	require.NoError(t, err)
	_, err = (&Adaptor{}).FinalizeOutboundJSONBody(ctx, info, first)
	require.NoError(t, err)
	_, ok = ArmCodexMissingStatusRetry(ctx, 0)
	require.True(t, ok)
	changed := []byte(`{"input":[{"type":"message","role":"assistant","content":"different"}]}`)
	unchanged, err := (&Adaptor{}).FinalizeOutboundJSONBody(ctx, info, changed)
	require.NoError(t, err)
	require.Equal(t, string(changed), string(unchanged))
}

func mustMarshalStatusRetryRequest(t *testing.T, value any) []byte {
	t.Helper()
	data, err := common.Marshal(value)
	require.NoError(t, err)
	return data
}
