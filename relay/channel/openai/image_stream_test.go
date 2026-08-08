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
	"github.com/QuantumNous/new-api/setting/ratio_setting"
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

func TestUpdateOpenAIImageTierPriceUsesReturnedSize(t *testing.T) {
	require.NoError(t, ratio_setting.UpdateGPTImagePriceByJSONString(`{"gpt-image-2":{"1k":0.04,"2k":0.08,"4k":0.16}}`))
	t.Cleanup(func() { _ = ratio_setting.UpdateGPTImagePriceByJSONString("{}") })

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-image-2",
		PriceData: types.PriceData{
			UsePrice:   true,
			ModelPrice: 0.04,
		},
	}
	updateOpenAIImageTierPrice(info, []byte(`{"size":"3840x2160"}`))
	require.Equal(t, 0.16, info.PriceData.ModelPrice)
	require.Equal(t, ratio_setting.GPTImageTier4K, info.PriceData.ImageSizeTier)

	updateOpenAIImageTierPrice(info, []byte(`{"data":[{"size":"1024x1024"},{"size":"2048x2048"}]}`))
	require.Equal(t, 0.16, info.PriceData.ModelPrice, "a later smaller result must not downgrade the settled tier")
	require.Equal(t, ratio_setting.GPTImageTier4K, info.PriceData.ImageSizeTier)
}
