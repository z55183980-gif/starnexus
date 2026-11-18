package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const (
	refusalTestChannel = 92

	// Recorded S4 → S1 stream for a refusal that happened after the Wrapper
	// had opened the stream (2026-11-17 08:37Z, usage trimmed): the Wrapper's
	// structured error event, then the gateway's appended generic error.
	s4StreamRefusalEvents = "event: error\n" +
		`data: {"error":{"details":{"model":"claude-opus-5-5","source":"cli_stream","stop_details":{"category":"cyber","type":"refusal"},"stop_reason":"refusal","usage":{"input_tokens":2,"output_tokens":0}},"message":"Upstream model declined the request (stop_reason=refusal)","retry_safe":false,"type":"upstream_refusal_error"},"type":"error"}` + "\n\n" +
		`data: {"error":{"message":"Upstream request failed","type":"upstream_error"},"type":"error"}` + "\n\n"
	s4MessageStart         = `data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],"usage":{"input_tokens":2,"output_tokens":0}}}` + "\n\n"
	s4NonStreamRefusalBody = `{"error":{"message":"Upstream model declined the request (stop_reason=refusal)","type":"upstream_error"},"type":"error"}`
)

func optInRefusalChannel(t *testing.T) {
	original := setting.UpstreamRefusalNoRetryConfig2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUpstreamRefusalNoRetryConfigByJsonString(original))
	})
	require.NoError(t, setting.UpdateUpstreamRefusalNoRetryConfigByJsonString(`{"channel_ids":[92]}`))
}

func newClaudeRelayContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c, recorder
}

// replayStream feeds a recorded upstream SSE body through the production Claude
// stream handler and returns the error it hands to the relay loop.
func replayStream(t *testing.T, c *gin.Context, body string) *types.NewAPIError {
	t.Helper()
	info := relaycommon.GenRelayInfoOpenAI(c, nil)
	info.ChannelMeta = &relaycommon.ChannelMeta{}
	info.RelayFormat = types.RelayFormatClaude
	info.DisablePing = true
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	_, apiErr := claude.ClaudeStreamHandler(c, resp, info)
	require.NotNil(t, apiErr)
	return apiErr
}

func nonStreamRefusal(t *testing.T) *types.NewAPIError {
	t.Helper()
	return service.RelayErrorHandler(context.Background(), &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(s4NonStreamRefusalBody)),
	}, false)
}

func TestUpstreamRefusalBeforeOptInKeepsLegacyRetry(t *testing.T) {
	c, _ := newClaudeRelayContext()
	apiErr := nonStreamRefusal(t)
	terminal, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
	require.False(t, refused, "channel not opted in")
	require.Same(t, apiErr, terminal)
	require.True(t, shouldRetry(c, apiErr, 3), "legacy behavior: 422 stays retryable until the channel opts in")
}

func TestNonStreamUpstreamRefusalEndsRequestWith422(t *testing.T) {
	optInRefusalChannel(t)
	c, recorder := newClaudeRelayContext()
	terminal, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, nonStreamRefusal(t))
	require.True(t, refused)
	require.False(t, shouldRetry(c, terminal, 3), "no same-request or channel retry")

	// Same as the Relay defer: the client-facing message carries the request id.
	terminal.SetMessage(common.MessageWithRequestId(terminal.Error(), "req-refusal-test"))
	writeClaudeRelayError(c, terminal)
	require.Contains(t, recorder.Body.String(), "req-refusal-test")
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.True(t, strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json"))
	require.Contains(t, recorder.Body.String(), `"type":"upstream_refusal_error"`)
	require.Contains(t, recorder.Body.String(), "stop_reason=refusal")
}

// The stream was already open (message_start forwarded) when the refusal
// arrived. The client must get one well-formed SSE error event, not a retry,
// not the gateway's trailing generic error and not a JSON body appended to SSE.
func TestStreamedUpstreamRefusalAfterMessageStartTerminatesSSE(t *testing.T) {
	optInRefusalChannel(t)
	c, recorder := newClaudeRelayContext()
	apiErr := replayStream(t, c, s4MessageStart+s4StreamRefusalEvents)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode, "stream handler status before normalization")
	require.True(t, c.Writer.Written(), "message_start was already forwarded")

	terminal, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
	require.True(t, refused, "structured refusal must be recognized despite status 500")
	require.False(t, shouldRetry(c, terminal, 3))

	terminal.SetMessage(common.MessageWithRequestId(terminal.Error(), "req-refusal-test"))
	writeClaudeRelayError(c, terminal)
	output := recorder.Body.String()
	require.Contains(t, output, "req-refusal-test")
	require.Equal(t, http.StatusOK, recorder.Code, "status cannot change after streaming starts")
	require.Equal(t, 1, strings.Count(output, `"type":"message_start"`))
	require.Equal(t, 1, strings.Count(output, "event: error"))
	require.Contains(t, output, `"type":"upstream_refusal_error"`)
	require.NotContains(t, output, "Upstream request failed")
	require.NotContains(t, output, "message_stop")
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		require.True(t, strings.HasPrefix(line, "event: ") || strings.HasPrefix(line, "data: "), "non-SSE line in stream: %q", line)
	}
}

// The refusal arrived before anything was forwarded: answer with the same
// HTTP 422 a non-streamed refusal gets, not the stream handler's 500.
func TestStreamedUpstreamRefusalBeforeAnyOutputReturns422(t *testing.T) {
	optInRefusalChannel(t)
	c, recorder := newClaudeRelayContext()
	apiErr := replayStream(t, c, s4StreamRefusalEvents)
	require.False(t, c.Writer.Written())

	require.Equal(t, "text/event-stream", c.Writer.Header().Get("Content-Type"), "stream handler staged SSE headers")

	terminal, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
	require.True(t, refused)
	writeClaudeRelayError(c, terminal)
	require.Equal(t, http.StatusUnprocessableEntity, recorder.Code)
	require.True(t, strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json"), recorder.Header().Get("Content-Type"))
	for _, header := range []string{"Transfer-Encoding", "Cache-Control", "X-Accel-Buffering", "Connection"} {
		require.Empty(t, recorder.Header().Get(header), header)
	}
	require.Contains(t, recorder.Body.String(), `"type":"upstream_refusal_error"`)
	require.NotContains(t, recorder.Body.String(), "Upstream request failed")
}

// With the channel not opted in, every writer must behave exactly as before
// the change, even though a raw streamed refusal carries the same error code.
func TestSwitchOffKeepsLegacyRefusalOutput(t *testing.T) {
	original := setting.UpstreamRefusalNoRetryConfig2JsonString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUpstreamRefusalNoRetryConfigByJsonString(original))
	})
	require.NoError(t, setting.UpdateUpstreamRefusalNoRetryConfigByJsonString(`{"channel_ids":[]}`))

	t.Run("after message_start", func(t *testing.T) {
		c, recorder := newClaudeRelayContext()
		apiErr := replayStream(t, c, s4MessageStart+s4StreamRefusalEvents)
		require.Equal(t, types.ErrorCodeUpstreamRefusal, apiErr.GetErrorCode(), "raw stream code equals the new constant")
		_, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
		require.False(t, refused)
		require.False(t, isUpstreamRefusalTerminal(c))
		require.True(t, shouldRetry(c, apiErr, 3))

		writeClaudeRelayError(c, apiErr)
		output := recorder.Body.String()
		// Legacy: the JSON error is appended to the open stream, no SSE event.
		require.Zero(t, strings.Count(output, "event: error"))
		require.Contains(t, output, `{"error":`)
		require.NotContains(t, output, `data: {"error":{"type":"upstream_refusal_error"`)
	})
	t.Run("before first frame", func(t *testing.T) {
		c, recorder := newClaudeRelayContext()
		apiErr := replayStream(t, c, s4StreamRefusalEvents)
		_, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
		require.False(t, refused)
		writeClaudeRelayError(c, apiErr)
		// Legacy: stream handler status and staged SSE headers are untouched.
		require.Equal(t, http.StatusInternalServerError, recorder.Code)
		require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	})
}

// Header cleanup is scoped to refusals: other errors before the first frame
// keep their existing (legacy) response headers.
func TestNonRefusalErrorKeepsStagedHeaders(t *testing.T) {
	optInRefusalChannel(t)
	c, recorder := newClaudeRelayContext()
	apiErr := replayStream(t, c, `data: {"error":{"message":"Upstream request failed","type":"upstream_error"},"type":"error"}`+"\n\n")
	_, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
	require.False(t, refused)
	writeClaudeRelayError(c, apiErr)
	require.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

func TestGenericStreamFailureKeepsRetryPolicy(t *testing.T) {
	optInRefusalChannel(t)
	c, _ := newClaudeRelayContext()
	apiErr := replayStream(t, c, `data: {"error":{"message":"Upstream request failed","type":"upstream_error"},"type":"error"}`+"\n\n")
	terminal, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
	require.False(t, refused)
	require.Same(t, apiErr, terminal)
	require.True(t, shouldRetry(c, apiErr, 3), "ordinary stream failures keep the existing retry policy")
}

func TestOrdinary422KeepsRetryPolicy(t *testing.T) {
	optInRefusalChannel(t)
	c, _ := newClaudeRelayContext()
	apiErr := service.RelayErrorHandler(context.Background(), &http.Response{
		StatusCode: http.StatusUnprocessableEntity,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"unprocessable input","type":"invalid_request_error"},"type":"error"}`)),
	}, false)
	_, refused := terminateOnUpstreamRefusal(c, refusalTestChannel, 0, apiErr)
	require.False(t, refused)
	require.True(t, shouldRetry(c, apiErr, 3))
}
