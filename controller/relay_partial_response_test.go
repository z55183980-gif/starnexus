package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TestManagedPartialResponseNeverRetriesAndTerminatesSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Header("Content-Type", "text/event-stream")
	c.Writer.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"real text\"}}\n\n")
	c.Writer.Flush()
	failure := types.WithClaudeError(types.ClaudeError{Type: "managed_partial_response", Message: "reset required"}, 500)
	if shouldRetry(c, failure, 3) {
		t.Fatal("partial output must not retry on another channel")
	}
	writeClaudeRelayError(c, failure)
	output := recorder.Body.String()
	if strings.Count(output, "event: error") != 1 || strings.Contains(output, "message_stop") {
		t.Fatalf("invalid terminal stream: %s", output)
	}
	var decoded struct {
		Type  string
		Error types.ClaudeError
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "managed_partial_response") {
			if err := common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &decoded); err != nil {
				t.Fatal(err)
			}
		}
	}
	if decoded.Type != "error" || decoded.Error.Type != "managed_partial_response" {
		t.Fatalf("missing SSE error: %s", output)
	}
	if recorder.Code != 200 {
		t.Fatal("cannot change status after streaming starts")
	}
}

func TestClaudeRelayErrorBeforeStreamPreservesHTTPError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"managed_partial_response", "rate_limit_error"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
		writeClaudeRelayError(c, types.WithClaudeError(types.ClaudeError{Type: kind, Message: "failed"}, 502))
		if c.Writer.Status() != 502 || !strings.HasPrefix(c.Writer.Header().Get("Content-Type"), "application/json") {
			t.Fatal("HTTP error behavior changed")
		}
	}
}
