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
