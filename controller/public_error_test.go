package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
)

func TestSanitizePublicErrorMessage(t *testing.T) {
	message := `field "content" is not supported by the ZQBAPI OpenAI Videos compatibility endpoint (zqbapi)`
	got := sanitizePublicErrorMessage(message)
	if got == message {
		t.Fatal("provider name was not sanitized")
	}
	if want := `field "content" is not supported by the video provider OpenAI Videos compatibility endpoint (video provider)`; got != want {
		t.Fatalf("sanitized message = %q, want %q", got, want)
	}
}

func TestRespondTaskErrorUsesOpenAIVideoEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set(string(constant.ContextKeyZQBAPIOpenAIVideoRequest), true)

	respondTaskError(c, &dto.TaskError{
		Code:       "unsupported_input",
		Message:    "ZQBAPI rejected https://provider.example/internal",
		StatusCode: http.StatusBadRequest,
	})

	var response map[string]any
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	errorObject, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("standard error envelope missing: %s", recorder.Body.String())
	}
	message, _ := errorObject["message"].(string)
	if message == "" || upstreamProviderNamePattern.MatchString(message) || publicErrorURLPattern.MatchString(message) {
		t.Fatalf("public error was not sanitized: %q", message)
	}
	if _, exists := response["data"]; exists {
		t.Fatalf("legacy task error fields leaked: %s", recorder.Body.String())
	}
}
