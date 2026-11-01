package types

import "testing"

func TestToClaudeErrorKeepsUpstreamTypeWithoutCode(t *testing.T) {
	err := WithOpenAIError(OpenAIError{Type: "request_too_large", Message: "Request exceeds the maximum size"}, 413)
	got := err.ToClaudeError()
	if got.Type != "request_too_large" {
		t.Fatalf("type = %q, want request_too_large", got.Type)
	}
	if got.Message != "Request exceeds the maximum size" {
		t.Fatalf("message = %q", got.Message)
	}
	if err.StatusCode != 413 {
		t.Fatalf("status = %d, want 413", err.StatusCode)
	}
}

func TestToClaudeErrorKeepsCodeWhenPresent(t *testing.T) {
	err := WithOpenAIError(OpenAIError{Type: "invalid_request_error", Code: "context_length_exceeded", Message: "too long"}, 400)
	if got := err.ToClaudeError().Type; got != "context_length_exceeded" {
		t.Fatalf("type = %q, want the code", got)
	}
}
