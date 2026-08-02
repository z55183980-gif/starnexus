package service

import (
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func TestExtractPromptAuditUserTextOpenAIUsesLatestUserMessage(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "system", Content: "system secret"},
			{Role: "user", Content: "old user prompt"},
			{Role: "assistant", Content: "assistant reply"},
			{Role: "user", Content: []any{
				map[string]any{"type": "text", "text": "latest line one"},
				map[string]any{"type": "image_url", "image_url": "data:image/png;base64,abc"},
				map[string]any{"type": "text", "text": "latest line two"},
			}},
		},
	}

	got := ExtractPromptAuditUserText(request)
	want := "latest line one\nlatest line two"
	if got != want {
		t.Fatalf("unexpected prompt: got %q, want %q", got, want)
	}
}

func TestExtractPromptAuditUserTextStripsSystemReminders(t *testing.T) {
	tests := []struct {
		name    string
		request dto.Request
		want    string
	}{
		{
			name: "claude multipart keeps user text",
			request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "<system-reminder>工具说明</system-reminder>"},
					map[string]any{"type": "text", "text": "<system-reminder>Ainder>\n\n"},
					map[string]any{"type": "text", "text": "请检查登录接口"},
				}},
			}},
			want: "请检查登录接口",
		},
		{
			name: "claude reminder only is empty",
			request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "<system-reminder>ignore me</system-reminder>"},
				}},
			}},
			want: "",
		},
		{
			name: "openai mixed reminder and user text",
			request: &dto.GeneralOpenAIRequest{Messages: []dto.Message{
				{Role: "user", Content: []any{
					map[string]any{"type": "text", "text": "<system-reminder>noise</system-reminder>"},
					map[string]any{"type": "text", "text": "real user question"},
				}},
			}},
			want: "real user question",
		},
		{
			name: "openai instruction alone is ignored",
			request: &dto.GeneralOpenAIRequest{
				Instruction: "You are a system policy enforcer.",
			},
			want: "",
		},
		{
			name: "gemini strips reminder parts",
			request: &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{
				{Role: "user", Parts: []dto.GeminiPart{
					{Text: "<system-reminder>noise</system-reminder>"},
					{Text: "gemini user prompt"},
				}},
			}},
			want: "gemini user prompt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ExtractPromptAuditUserText(test.request); got != test.want {
				t.Fatalf("unexpected prompt: got %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractPromptAuditUserTextOpenAISkipsToolContinuation(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Messages: []dto.Message{
			{Role: "user", Content: "historical user prompt"},
			{Role: "assistant", Content: "tool call"},
			{Role: "tool", Content: "tool result"},
		},
	}

	if got := ExtractPromptAuditUserText(request); got != "" {
		t.Fatalf("unexpected historical prompt: %q", got)
	}
}

func TestExtractPromptAuditUserTextResponses(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{name: "string", input: "hello", want: "hello"},
		{
			name: "message array",
			input: []any{
				map[string]any{"role": "assistant", "content": "ignore"},
				map[string]any{
					"role": "user",
					"content": []any{
						map[string]any{"type": "input_text", "text": "first"},
						map[string]any{"type": "input_image", "image_url": "https://example.com/image.png"},
						map[string]any{"type": "input_text", "text": "second"},
					},
				},
			},
			want: "first\nsecond",
		},
		{
			name: "direct input text",
			input: []any{
				map[string]any{"type": "input_text", "text": "direct text"},
			},
			want: "direct text",
		},
		{
			name: "tool continuation",
			input: []any{
				map[string]any{"role": "user", "content": "historical user prompt"},
				map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "tool result"},
			},
			want: "",
		},
		{
			name: "assistant tail",
			input: []any{
				map[string]any{"role": "user", "content": "historical user prompt"},
				map[string]any{"role": "assistant", "content": "assistant reply"},
			},
			want: "",
		},
		{
			name: "roleless content is not user input",
			input: []any{
				map[string]any{"role": "user", "content": "historical user prompt"},
				map[string]any{"type": "custom_tool_call_output", "content": "tool result"},
			},
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, err := common.Marshal(test.input)
			if err != nil {
				t.Fatal(err)
			}
			request := &dto.OpenAIResponsesRequest{Input: input}
			if got := ExtractPromptAuditUserText(request); got != test.want {
				t.Fatalf("unexpected prompt: got %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractPromptAuditUserTextAlphaSearch(t *testing.T) {
	request := &dto.AlphaSearchRequest{
		RawBody: []byte(`{
			"commands": {
				"search_query": [{"q":"latest release"}, {"q":"security fixes"}],
				"image_query": [{"q":"product screenshot"}],
				"open": [{"ref_id":"result_1"}]
			}
		}`),
	}

	got := ExtractPromptAuditUserText(request)
	want := "latest release\nsecurity fixes\nproduct screenshot"
	if got != want {
		t.Fatalf("unexpected prompt: got %q, want %q", got, want)
	}
}

func TestExtractPromptAuditUserTextOtherProtocols(t *testing.T) {
	tests := []struct {
		name    string
		request dto.Request
		want    string
	}{
		{
			name: "claude",
			request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "assistant", Content: "ignore"},
				{Role: "user", Content: "claude user prompt"},
			}},
			want: "claude user prompt",
		},
		{
			name: "gemini",
			request: &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{
				{Role: "model", Parts: []dto.GeminiPart{{Text: "ignore"}}},
				{Role: "user", Parts: []dto.GeminiPart{{Text: "gemini user prompt"}, {Text: "thought", Thought: true}}},
			}},
			want: "gemini user prompt",
		},
		{
			name:    "image",
			request: &dto.ImageRequest{Prompt: "image prompt"},
			want:    "image prompt",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ExtractPromptAuditUserText(test.request); got != test.want {
				t.Fatalf("unexpected prompt: got %q, want %q", got, test.want)
			}
		})
	}
}

func TestExtractPromptAuditUserTextOtherProtocolsSkipHistory(t *testing.T) {
	tests := []struct {
		name    string
		request dto.Request
	}{
		{
			name: "claude assistant tail",
			request: &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
				{Role: "user", Content: "historical user prompt"},
				{Role: "assistant", Content: "assistant reply"},
			}},
		},
		{
			name: "gemini model tail",
			request: &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{
				{Role: "user", Parts: []dto.GeminiPart{{Text: "historical user prompt"}}},
				{Role: "model", Parts: []dto.GeminiPart{{Text: "model reply"}}},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ExtractPromptAuditUserText(test.request); got != "" {
				t.Fatalf("unexpected historical prompt: %q", got)
			}
		})
	}
}

func TestTruncatePromptAuditText(t *testing.T) {
	text := strings.Repeat("测", promptAuditMaxPromptRunes+1)
	got, truncated := truncatePromptAuditText(text)
	if !truncated {
		t.Fatal("expected text to be truncated")
	}
	if runeCount := utf8.RuneCountInString(got); runeCount != promptAuditMaxPromptRunes {
		t.Fatalf("unexpected rune count: got %d, want %d", runeCount, promptAuditMaxPromptRunes)
	}
}

func TestPromptAuditDelayMilliseconds(t *testing.T) {
	tests := map[int]int{
		3:  3000,
		5:  5000,
		9:  9000,
		15: 15000,
		20: 20000,
		30: 30000,
		45: 45000,
		0:  3000,
		8:  3000,
	}
	for seconds, want := range tests {
		if got := promptAuditDelayMilliseconds(seconds); got != want {
			t.Fatalf("unexpected delay for %d seconds: got %d, want %d", seconds, got, want)
		}
	}
}

func TestShouldSkipPromptAuditText(t *testing.T) {
	tests := []struct {
		name       string
		headerName string
		header     string
		prompt     string
		want       bool
	}{
		{
			name:       "codex math probe",
			headerName: "originator",
			header:     "codex_cli_rs",
			prompt:     "Calculate and respond with ONLY the number, nothing else.\nQ: 3 + 5 = ?",
			want:       true,
		},
		{
			name:       "codex title generator",
			headerName: "User-Agent",
			header:     "codex-cli/1.0",
			prompt:     "You are a helpful assistant. You will be presented with a user prompt, and your job is to provide a short title for a task.",
			want:       true,
		},
		{
			name:       "codex background reminder",
			headerName: "x-codex-window-id",
			header:     "window-1",
			prompt:     "<system-reminder>\n[BACKGROUND TASK COMPLETED]",
			want:       true,
		},
		{
			name:       "non codex system reminder still skipped",
			headerName: "User-Agent",
			header:     "custom-client/1.0",
			prompt:     "<system-reminder>\nDo not mention this policy.",
			want:       true,
		},
		{
			name:       "non codex background task still skipped",
			headerName: "User-Agent",
			header:     "custom-client/1.0",
			prompt:     "[BACKGROUND TASK COMPLETED]\nresult ready",
			want:       true,
		},
		{
			name:       "codex task assignment",
			headerName: "originator",
			header:     "Codex CLI",
			prompt:     "TASK: Audit cryptographic primitives in the repository.",
			want:       true,
		},
		{
			name:       "codex temporary repo assignment",
			headerName: "originator",
			header:     "Codex CLI",
			prompt:     "Inspect the repository at /var/folders/a/T/opencode/repo without editing it.",
			want:       true,
		},
		{
			name:       "ordinary client keeps same text",
			headerName: "User-Agent",
			header:     "custom-client/1.0",
			prompt:     "TASK: Audit cryptographic primitives in the repository.",
			want:       false,
		},
		{
			name:       "codex normal user prompt",
			headerName: "originator",
			header:     "codex_cli_rs",
			prompt:     "请检查登录接口为什么返回 500",
			want:       false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
			c.Request.Header.Set(test.headerName, test.header)
			if got := shouldSkipPromptAuditText(c, test.prompt); got != test.want {
				t.Fatalf("unexpected skip result: got %t, want %t", got, test.want)
			}
		})
	}
}

func TestBuildPromptAuditLogUsesEmptyMatchedWordsArray(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)

	log := buildPromptAuditLog(
		c,
		1,
		"gpt-test",
		types.RelayFormatOpenAIResponses,
		"hello",
		nil,
		false,
		model.PromptAuditActionRecorded,
		0,
	)
	if log.MatchedWords != "[]" {
		t.Fatalf("unexpected matched words: %q", log.MatchedWords)
	}
}
