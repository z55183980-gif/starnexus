package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
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
			{Role: "tool", Content: "tool result"},
		},
	}

	got := ExtractPromptAuditUserText(request)
	want := "latest line one\nlatest line two"
	if got != want {
		t.Fatalf("unexpected prompt: got %q, want %q", got, want)
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
				{Role: "user", Content: "claude user prompt"},
				{Role: "assistant", Content: "ignore"},
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
