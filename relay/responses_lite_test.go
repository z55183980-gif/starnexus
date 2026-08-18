package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newResponsesLiteRelayContext(t *testing.T, mode string) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://chatgpt.com")
	common.SetContextKey(c, constant.ContextKeyChannelKey, `{"access_token":"token","account_id":"account"}`)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{ResponsesWebSocketV2Mode: mode})
	return c
}

func TestPrepareResponsesWebSocketRequestNormalizesLiteEveryTurn(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newResponsesLiteRelayContext(t, model.UpstreamOpenAIWSModeContextPool)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, OriginModelName: "gpt-5.6-sol"}

	for turn, reasoning := range []*dto.Reasoning{
		{Effort: "high", Context: stringPointer("current_turn")},
		nil,
	} {
		request := &dto.OpenAIResponsesRequest{
			Model:          "gpt-5.6-sol",
			Input:          []byte(`[{"type":"message","role":"user","content":"hello"}]`),
			Reasoning:      reasoning,
			ClientMetadata: []byte(`{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}`),
			Tools: []byte(`[
				{"type":"function","name":"shell","parameters":{"type":"object"}},
				{"type":"namespace","name":"collaboration","tools":[{"type":"function","name":"spawn_agent"}]}
			]`),
		}

		prepared, _, apiErr := PrepareResponsesWebSocketRequest(c, info, request, false)
		require.Nilf(t, apiErr, "turn %d", turn+1)
		require.Equal(t, "all_turns", gjson.GetBytes(prepared, "reasoning.context").String())
		require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(prepared, "include.0").String())
		require.False(t, gjson.GetBytes(prepared, "store").Bool())
		require.Equal(t, "true", gjson.GetBytes(prepared, "client_metadata."+codex.ResponsesLiteWSMetadataKey).String())
		require.False(t, gjson.GetBytes(prepared, "tools").Exists())
		require.Equal(t, "shell", gjson.GetBytes(prepared, `input.#(type=="additional_tools").tools.0.name`).String())
		require.Equal(t, "collaboration", gjson.GetBytes(prepared, `input.#(type=="additional_tools").tools.1.name`).String())
	}
}

func TestPrepareResponsesWebSocketRequestRestoresLiteHeaderForHTTPBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newResponsesLiteRelayContext(t, model.UpstreamOpenAIWSModeHTTPBridge)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, OriginModelName: "gpt-5.6-sol"}
	request := &dto.OpenAIResponsesRequest{
		Model:          "gpt-5.6-sol",
		Input:          []byte(`[{"type":"message","role":"user","content":"hello"}]`),
		ClientMetadata: []byte(`{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}`),
	}

	prepared, _, apiErr := PrepareResponsesWebSocketRequest(c, info, request, true)
	require.Nil(t, apiErr)
	require.Equal(t, "all_turns", gjson.GetBytes(prepared, "reasoning.context").String())
	require.False(t, gjson.GetBytes(prepared, "store").Bool())
	require.Equal(t, "true", c.GetHeader(codex.ResponsesLiteHeader))
}

func TestPrepareResponsesWebSocketRequestRestoresLiteHeaderForSub2APIBridge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeSub2API)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://sub2api.example")
	common.SetContextKey(c, constant.ContextKeyChannelKey, "sub2api-token")
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, dto.ChannelOtherSettings{ResponsesWebSocketV2Mode: model.UpstreamOpenAIWSModeHTTPBridge})
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses, OriginModelName: "gpt-5.6-sol"}
	request := &dto.OpenAIResponsesRequest{
		Model:          "gpt-5.6-sol",
		Input:          []byte(`[{"type":"message","role":"user","content":"hello"}]`),
		ClientMetadata: []byte(`{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}`),
	}

	prepared, _, apiErr := PrepareResponsesWebSocketRequest(c, info, request, true)
	require.Nil(t, apiErr)
	require.False(t, gjson.GetBytes(prepared, "reasoning.context").Exists(), "downstream Sub2API owns Lite normalization")
	require.Equal(t, "true", c.GetHeader(codex.ResponsesLiteHeader))
}

func TestNormalizeSelectedResponsesLitePayloadOnlyAppliesToOpenAIOAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := newResponsesLiteRelayContext(t, model.UpstreamOpenAIWSModeContextPool)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeAPIKey)
	body := []byte(`{
		"reasoning":{"context":"current_turn"},
		"client_metadata":{"ws_request_header_x_openai_internal_codex_responses_lite":"true"}
	}`)

	normalized, isLite, err := normalizeSelectedResponsesLitePayload(c, &relaycommon.RelayInfo{}, body, true)
	require.NoError(t, err)
	require.False(t, isLite)
	require.JSONEq(t, string(body), string(normalized))
}

func stringPointer(value string) *string {
	return &value
}
