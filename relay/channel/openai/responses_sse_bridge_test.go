package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const completedResponsesSSE = `data: {"type":"response.created","response":{"id":"resp_123"}}

data: {"type":"response.output_text.delta","delta":"hello"}

data: {"type":"response.completed","response":{"id":"resp_123","object":"response","created_at":123,"status":"completed","model":"gpt-5","output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"hello","annotations":[]}]}],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

data: [DONE]

`

func newResponsesSSEBridgeContext() (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}
	return ctx, recorder, info
}

func newResponsesSSEResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":   []string{"text/event-stream"},
			"Content-Length": []string{"999"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}

func TestResponsesSSEToNonStreamHandlerRebuildsEmptyCompletedOutput(t *testing.T) {
	t.Parallel()
	ctx, recorder, info := newResponsesSSEBridgeContext()

	// Codex-style stream: text only appears in deltas; completed has usage but empty output.
	body := `data: {"type":"response.created","response":{"id":"resp_empty_out"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}

data: {"type":"response.output_text.delta","item_id":"msg_1","output_index":0,"content_index":0,"delta":"PONG"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"","annotations":[]}]}}

data: {"type":"response.completed","response":{"id":"resp_empty_out","object":"response","created_at":123,"status":"completed","model":"gpt-5","output":[],"usage":{"input_tokens":4,"output_tokens":2,"total_tokens":6}}}

data: [DONE]

`
	usage, apiErr := OaiResponsesSSEToNonStreamHandler(ctx, info, newResponsesSSEResponse(body))
	require.Nil(t, apiErr)
	require.Equal(t, 6, usage.TotalTokens)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "resp_empty_out", gjson.Get(recorder.Body.String(), "id").String())
	require.Equal(t, "PONG", gjson.Get(recorder.Body.String(), "output.0.content.0.text").String())
	require.Equal(t, "message", gjson.Get(recorder.Body.String(), "output.0.type").String())
}

func TestResponsesSSEToChatHandlerRebuildsToolCallsFromDeltas(t *testing.T) {
	t.Parallel()
	ctx, recorder, info := newResponsesSSEBridgeContext()

	body := `data: {"type":"response.created","response":{"id":"resp_tool_rebuild"}}

data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"","status":"in_progress"}}

data: {"type":"response.function_call_arguments.delta","item_id":"fc_1","output_index":0,"delta":"{\"city\":\"London\"}"}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"","status":"completed"}}

data: {"type":"response.completed","response":{"id":"resp_tool_rebuild","object":"response","created_at":123,"status":"completed","model":"gpt-5","output":[],"usage":{"input_tokens":8,"output_tokens":5,"total_tokens":13}}}

data: [DONE]

`
	usage, apiErr := OaiResponsesSSEToChatHandler(ctx, info, newResponsesSSEResponse(body))
	require.Nil(t, apiErr)
	require.Equal(t, 13, usage.TotalTokens)
	require.Equal(t, "chat.completion", gjson.Get(recorder.Body.String(), "object").String())
	require.Equal(t, "get_weather", gjson.Get(recorder.Body.String(), "choices.0.message.tool_calls.0.function.name").String())
	require.Equal(t, `{"city":"London"}`, gjson.Get(recorder.Body.String(), "choices.0.message.tool_calls.0.function.arguments").String())
	require.Equal(t, "tool_calls", gjson.Get(recorder.Body.String(), "choices.0.finish_reason").String())
}

func TestResponsesSSEToNonStreamHandlerRebuildsFromTextDeltasWithoutItems(t *testing.T) {
	t.Parallel()
	ctx, recorder, info := newResponsesSSEBridgeContext()

	body := `data: {"type":"response.created","response":{"id":"resp_delta_only"}}

data: {"type":"response.output_text.delta","delta":"Hel"}

data: {"type":"response.output_text.delta","delta":"lo"}

data: {"type":"response.completed","response":{"id":"resp_delta_only","object":"response","status":"completed","model":"gpt-5","output":[],"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}}

data: [DONE]

`
	usage, apiErr := OaiResponsesSSEToNonStreamHandler(ctx, info, newResponsesSSEResponse(body))
	require.Nil(t, apiErr)
	require.Equal(t, 3, usage.TotalTokens)
	require.Equal(t, "Hello", gjson.Get(recorder.Body.String(), "output.0.content.0.text").String())
}

func TestResponsesSSEToNonStreamHandlerReturnsJSON(t *testing.T) {
	t.Parallel()
	ctx, recorder, info := newResponsesSSEBridgeContext()

	usage, apiErr := OaiResponsesSSEToNonStreamHandler(ctx, info, newResponsesSSEResponse(completedResponsesSSE))
	require.Nil(t, apiErr)
	require.Equal(t, 6, usage.TotalTokens)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.NotEqual(t, "999", recorder.Header().Get("Content-Length"))
	require.Equal(t, "resp_123", gjson.Get(recorder.Body.String(), "id").String())
	require.Equal(t, "hello", gjson.Get(recorder.Body.String(), "output.0.content.0.text").String())
	require.NotContains(t, recorder.Body.String(), "data:")
}

func TestResponsesSSEToChatHandlerReturnsJSON(t *testing.T) {
	t.Parallel()
	ctx, recorder, info := newResponsesSSEBridgeContext()

	usage, apiErr := OaiResponsesSSEToChatHandler(ctx, info, newResponsesSSEResponse(completedResponsesSSE))
	require.Nil(t, apiErr)
	require.Equal(t, 6, usage.TotalTokens)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "chat.completion", gjson.Get(recorder.Body.String(), "object").String())
	require.Equal(t, "hello", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
	require.NotContains(t, recorder.Body.String(), "data:")
}

func TestResponsesToChatHandlerBridgesSSEWithWrongContentType(t *testing.T) {
	t.Parallel()
	ctx, recorder, info := newResponsesSSEBridgeContext()

	// Reproduces prod log #1404056: SSE body starting with "event:" but
	// Content-Type not advertised as text/event-stream.
	sseBody := "event: response.created\n" + completedResponsesSSE
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(sseBody)),
	}

	usage, apiErr := OaiResponsesToChatHandler(ctx, info, resp)
	require.Nil(t, apiErr)
	require.Equal(t, 6, usage.TotalTokens)
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	require.Equal(t, "chat.completion", gjson.Get(recorder.Body.String(), "object").String())
	require.Equal(t, "hello", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
}

func TestResponsesToChatHandlerLogsNonJSONBody(t *testing.T) {
	t.Parallel()
	ctx, _, info := newResponsesSSEBridgeContext()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(strings.NewReader("error upstream unavailable")),
	}

	_, apiErr := OaiResponsesToChatHandler(ctx, info, resp)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "invalid character")
	require.Equal(t, "bad_response_body", string(apiErr.GetErrorCode()))
}

func TestResponsesSSEBridgeRejectsMissingTerminalResponse(t *testing.T) {
	t.Parallel()
	ctx, _, info := newResponsesSSEBridgeContext()
	resp := newResponsesSSEResponse("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: [DONE]\n\n")

	_, apiErr := OaiResponsesSSEToNonStreamHandler(ctx, info, resp)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "without a terminal response")
}

func TestResponsesSSEBridgePropagatesTerminalError(t *testing.T) {
	t.Parallel()
	ctx, _, info := newResponsesSSEBridgeContext()
	resp := newResponsesSSEResponse("data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"message\":\"upstream failed\"}}\n\n")

	_, apiErr := OaiResponsesSSEToNonStreamHandler(ctx, info, resp)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "upstream failed")
}
