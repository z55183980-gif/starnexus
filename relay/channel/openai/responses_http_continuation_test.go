package openai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func newResponsesHTTPIntegrationContext(userID, tokenID int) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, userID)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, tokenID)
	return ctx, recorder
}

func enableResponsesHTTPPersist(ctx *gin.Context) {
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 42)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPoolId, 3)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 9)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	service.MarkResponsesHTTPContinuationPersistTarget(ctx)
}

func TestResponsesHTTPHandlerCommitsFunctionCallContinuationAfterWrite(t *testing.T) {
	firstCtx, _ := newResponsesHTTPIntegrationContext(8101, 9101)
	enableResponsesHTTPPersist(firstCtx)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"lookup"`)})
	responseBody := `{
		"id":"resp_http_handler_call",
		"object":"response",
		"output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}],
		"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}
	}`
	usage, apiErr := OaiResponsesHandler(firstCtx, &relaycommon.RelayInfo{}, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	})
	require.Nil(t, apiErr)
	require.Equal(t, 7, usage.TotalTokens)

	secondCtx, _ := newResponsesHTTPIntegrationContext(8101, 9101)
	second := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_http_handler_call",
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	service.PrepareResponsesHTTPContinuation(secondCtx, second)
	require.Nil(t, service.ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
	require.Equal(t, int64(3), gjson.GetBytes(second.Input, "#").Int())
}

func TestResponsesSSEBridgeStoresPortableReasoningForContinuation(t *testing.T) {
	firstCtx, _ := newResponsesHTTPIntegrationContext(8102, 9102)
	enableResponsesHTTPPersist(firstCtx)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})
	body := `data: {"type":"response.created","response":{"id":"resp_http_bridge_raw"}}

data: {"type":"response.completed","response":{"id":"resp_http_bridge_raw","object":"response","model":"gpt-5","output":[{"type":"reasoning","id":"rs_1","encrypted_content":"ciphertext"},{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

data: [DONE]

`
	_, apiErr := OaiResponsesSSEToNonStreamHandler(firstCtx, &relaycommon.RelayInfo{}, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	require.Nil(t, apiErr)

	secondCtx, _ := newResponsesHTTPIntegrationContext(8102, 9102)
	second := &dto.OpenAIResponsesRequest{Model: "gpt-5", PreviousResponseID: "resp_http_bridge_raw", Input: json.RawMessage(`"second"`)}
	service.PrepareResponsesHTTPContinuation(secondCtx, second)
	require.Nil(t, service.ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
	require.False(t, gjson.GetBytes(second.Input, `#(type=="reasoning")`).Exists())
	require.True(t, gjson.GetBytes(second.Input, `#(type=="message")`).Exists())
}

func TestResponsesSSERebuildStageOmitsEmptyQualityInContinuation(t *testing.T) {
	firstCtx, _ := newResponsesHTTPIntegrationContext(8104, 9104)
	enableResponsesHTTPPersist(firstCtx)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})

	// Codex-style stream: completed output is empty, so the bridge rebuilds from
	// item/delta events and stages the rebuilt DTO output for continuation.
	body := `data: {"type":"response.created","response":{"id":"resp_rebuild_quality"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}

data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"done"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"","annotations":[]}]}}

data: {"type":"response.completed","response":{"id":"resp_rebuild_quality","object":"response","model":"gpt-5","output":[],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

data: [DONE]

`
	_, apiErr := OaiResponsesSSEToNonStreamHandler(firstCtx, &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	})
	require.Nil(t, apiErr)

	secondCtx, _ := newResponsesHTTPIntegrationContext(8104, 9104)
	second := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_rebuild_quality",
		Input:              json.RawMessage(`"second"`),
	}
	service.PrepareResponsesHTTPContinuation(secondCtx, second)
	require.Nil(t, service.ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
	require.NotEmpty(t, string(second.Input), "expanded continuation input should not be empty")

	assistant := gjson.GetBytes(second.Input, `#(role=="assistant")`)
	require.True(t, assistant.Exists(), "continuation input=%s", string(second.Input))
	require.Equal(t, "message", assistant.Get("type").String())
	require.Equal(t, "done", assistant.Get("content.0.text").String())
	require.False(t, assistant.Get("quality").Exists())
	require.False(t, assistant.Get("size").Exists())
}

func TestResponsesStreamHandlerCommitsDeliveredFunctionCall(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	firstCtx, _ := newResponsesHTTPIntegrationContext(8103, 9103)
	enableResponsesHTTPPersist(firstCtx)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"lookup"`)})
	body := `data: {"type":"response.created","response":{"id":"resp_http_stream_call"}}

data: {"type":"response.output_item.done","item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}}

data: {"type":"response.completed","response":{"id":"resp_http_stream_call","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}],"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}

data: [DONE]

`
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	_, apiErr := OaiResponsesStreamHandler(firstCtx, info, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))})
	require.Nil(t, apiErr)

	secondCtx, _ := newResponsesHTTPIntegrationContext(8103, 9103)
	second := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_http_stream_call",
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	service.PrepareResponsesHTTPContinuation(secondCtx, second)
	require.Nil(t, service.ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
}
