package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type panicFlushResponseWriter struct {
	gin.ResponseWriter
}

func (w *panicFlushResponseWriter) Flush() {
	panic("flush failed")
}

func TestWriteOpenaiImageStreamChunkReturnsWriteError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)

	err := writeOpenaiImageStreamChunk(c, []byte(`{"type":"image_generation.completed","b64_json":"abc"}`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "request context done")
}

func TestOpenaiImageStreamHandlerReturnsWriteError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &panicFlushResponseWriter{ResponseWriter: c.Writer}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"image_generation.completed\",\"b64_json\":\"abc\"}\n")),
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	usage, apiErr := OpenaiImageStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "flush panic recovered")
	require.True(t, types.IsSkipRetryError(apiErr))
	require.NotNil(t, info.StreamStatus)
	require.True(t, info.StreamStatus.HasErrors())
}

func TestOpenaiImageJSONAsStreamHandlerReturnsWriteError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &panicFlushResponseWriter{ResponseWriter: c.Writer}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"created": 123,
			"data": [{"b64_json": "abc"}],
			"usage": {"input_tokens": 1, "output_tokens": 2, "total_tokens": 3}
		}`)),
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	usage, apiErr := openaiImageJSONAsStreamHandler(c, info, resp)

	require.NotNil(t, usage)
	require.NotNil(t, apiErr)
	require.Contains(t, apiErr.Error(), "flush panic recovered")
	require.True(t, types.IsSkipRetryError(apiErr))
	require.NotNil(t, info.StreamStatus)
	require.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
}

func TestOpenaiImageJSONAsStreamHandlerUsesEditEventType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(`{
			"created": 123,
			"data": [{"b64_json": "abc"}],
			"usage": {"input_tokens": 1, "output_tokens": 2, "total_tokens": 3}
		}`)),
	}
	info := &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeImagesEdits,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	usage, apiErr := openaiImageJSONAsStreamHandler(c, info, resp)

	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	body := recorder.Body.String()
	require.Contains(t, body, "event: image_edit.completed")
	require.Contains(t, body, `"type":"image_edit.completed"`)
	require.NotContains(t, body, "event: image_generation.completed")
}
