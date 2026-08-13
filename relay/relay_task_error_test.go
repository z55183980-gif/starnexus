package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
)

func TestZQBAPIOpenAIVideoSubmitErrorKeepsUpstreamBodyInternal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body: io.NopCloser(strings.NewReader(
			`{"message":"ZQBAPI rejected content[1] at https://provider.example/private"}`,
		)),
	}

	taskErr := openAIVideoSubmitError(c, resp)
	if taskErr.StatusCode != http.StatusBadRequest || taskErr.Code != "video_request_rejected" {
		t.Fatalf("status=%d code=%q", taskErr.StatusCode, taskErr.Code)
	}
	if strings.Contains(strings.ToLower(taskErr.Message), "zqbapi") || strings.Contains(taskErr.Message, "provider.example") || strings.Contains(taskErr.Message, "content[1]") {
		t.Fatalf("public message leaked upstream details: %q", taskErr.Message)
	}
	if taskErr.Error == nil || !strings.Contains(taskErr.Error.Error(), "provider.example") {
		t.Fatalf("internal diagnostics were not retained: %v", taskErr.Error)
	}
}

func TestDoubaoVideo2SubmitErrorClassifiesRequestBodyOverflowAsLocal413(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeDoubaoVideo2)
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body: io.NopCloser(strings.NewReader(
			`{"code":"InternalError","message":"Error 1406 (22001): Data too long for column 'request_body' at row 1"}`,
		)),
	}

	taskErr := openAIVideoSubmitError(c, resp)
	if taskErr.StatusCode != http.StatusRequestEntityTooLarge || taskErr.Code != "doubao_video2_request_body_too_large" {
		t.Fatalf("status=%d code=%q", taskErr.StatusCode, taskErr.Code)
	}
	if !taskErr.LocalError {
		t.Fatal("deterministic upstream request_body overflow must not be retried")
	}
	if strings.Contains(strings.ToLower(taskErr.Message), "mysql") || strings.Contains(taskErr.Message, "1406") {
		t.Fatalf("public message leaked storage details: %q", taskErr.Message)
	}
}
