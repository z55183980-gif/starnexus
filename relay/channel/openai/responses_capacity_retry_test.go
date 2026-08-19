package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamStagesCapacityPreludeForAccountRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelCredentialSource, constant.ChannelCredentialSourceAccountPool)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), StreamStatus: relaycommon.NewStreamStatus()}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_capacity\"}}\n\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"Selected model is at capacity.\"}}}\n\n",
		)),
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, response)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, 529, apiErr.StatusCode)
	require.Empty(t, recorder.Body.String())
	require.Zero(t, info.SendResponseCount)
}

func TestResponsesStreamFlushesStagedPreludeBeforeOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelCredentialSource, constant.ChannelCredentialSourceAccountPool)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), StreamStatus: relaycommon.NewStreamStatus()}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_ok\"}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n" +
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n",
		)),
	}

	_, apiErr := OaiResponsesStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), "event: response.created")
	require.Contains(t, recorder.Body.String(), "event: response.output_text.delta")
	require.GreaterOrEqual(t, info.SendResponseCount, 2)
}

func TestResponsesStreamHidesMetadataPreludeAndCapacityErrorFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelCredentialSource, constant.ChannelCredentialSourceAccountPool)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), StreamStatus: relaycommon.NewStreamStatus()}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_capacity\"}}\n\n" +
				"data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"resp_capacity\"}}\n\n" +
				"data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"reasoning\",\"summary\":[]}}\n\n" +
				"data: {\"type\":\"response.reasoning_summary_part.added\",\"part\":{\"type\":\"summary_text\",\"text\":\"\"}}\n\n" +
				"data: {\"type\":\"error\",\"error\":{\"type\":\"service_unavailable_error\",\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}\n\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}}\n\n",
		)),
	}

	usage, apiErr := OaiResponsesStreamHandler(c, info, response)
	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.Equal(t, 529, apiErr.StatusCode)
	require.Empty(t, recorder.Body.String())
	require.Zero(t, info.SendResponseCount)
}

func TestResponsesStreamDoesNotHideNonCapacityFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelCredentialSource, constant.ChannelCredentialSourceAccountPool)
	info := &relaycommon.RelayInfo{StartTime: time.Now(), StreamStatus: relaycommon.NewStreamStatus()}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_invalid\"}}\n\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request\",\"message\":\"invalid input\"}}}\n\n",
		)),
	}

	_, apiErr := OaiResponsesStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), "response.created")
	require.Contains(t, recorder.Body.String(), "invalid_request")
	require.NotZero(t, info.SendResponseCount)
}

func TestResponsesStreamCapacityAfterOutputRewritesFatalCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyChannelCredentialSource, constant.ChannelCredentialSourceAccountPool)
	info := &relaycommon.RelayInfo{
		StartTime:    time.Now(),
		StreamStatus: relaycommon.NewStreamStatus(),
		ChannelMeta:  &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5"},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_partial\"}}\n\n" +
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n" +
				"data: {\"type\":\"error\",\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"}}\n\n" +
				"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"server_is_overloaded\",\"message\":\"Our servers are currently overloaded. Please try again later.\"},\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n",
		)),
	}

	_, apiErr := OaiResponsesStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), "partial")
	require.Contains(t, recorder.Body.String(), `"code":"server_error"`)
	require.NotContains(t, recorder.Body.String(), "server_is_overloaded")
}

func TestResponsesStreamDataStartsClientOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "created", data: `{"type":"response.created","response":{"id":"resp_1"}}`, want: false},
		{name: "in progress", data: `{"type":"response.in_progress","response":{"id":"resp_1"}}`, want: false},
		{name: "empty reasoning item", data: `{"type":"response.output_item.added","item":{"type":"reasoning","summary":[]}}`, want: false},
		{name: "encrypted reasoning item", data: `{"type":"response.output_item.added","item":{"type":"reasoning","encrypted_content":"cipher"}}`, want: true},
		{name: "empty reasoning part", data: `{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":""}}`, want: false},
		{name: "reasoning part", data: `{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text","text":"thinking"}}`, want: true},
		{name: "empty content part", data: `{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`, want: false},
		{name: "text delta", data: `{"type":"response.output_text.delta","delta":"hello"}`, want: true},
		{name: "capacity error", data: `{"type":"error","error":{"code":"server_is_overloaded","message":"overloaded"}}`, want: false},
		{name: "slow down error", data: `{"type":"error","error":{"code":"slow_down","message":"slow down"}}`, want: false},
		{name: "invalid request error", data: `{"type":"error","error":{"code":"invalid_request","message":"invalid"}}`, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var event dto.ResponsesStreamResponse
			require.NoError(t, common.UnmarshalJsonStr(tt.data, &event))
			require.Equal(t, tt.want, ResponsesStreamDataStartsClientOutput(tt.data, &event))
		})
	}
}
