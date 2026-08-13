package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
