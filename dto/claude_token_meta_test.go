package dto

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestClaudeToolResultTokenMeta(t *testing.T) {
	image := map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": strings.Repeat("aB12+/", 200000)}}
	urlImage := map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.invalid/image.png?private=value"}}
	text := map[string]any{"type": "text", "text": "result\n\"quoted\""}
	for _, tc := range []struct {
		name     string
		content  any
		wantText string
		images   int
		url      bool
	}{
		{"string", "plain\nresult", "plain\nresult", 0, false},
		{"text_blocks", []any{text}, "result\n\"quoted\"", 0, false},
		{"base64_image", []any{image}, "", 1, false},
		{"url_image", []any{urlImage}, "", 1, true},
		{"mixed", []any{text, image, text}, "result\n\"quoted\"", 1, false},
		{"nested", []any{map[string]any{"type": "tool_result", "content": []any{text, image}}}, "result\n\"quoted\"", 1, false},
		{"typed_blocks", []ClaudeMediaMessage{{Type: "image", Source: &ClaudeMessageSource{Type: "base64", MediaType: "image/png", Data: "aGVsbG8="}}}, "", 1, false},
		{"unknown_block", []any{map[string]any{"type": "custom_result", "value": "keep-this-value"}}, "keep-this-value", 0, false},
		{"object_fallback", map[string]any{"value": "keep-object-value"}, "keep-object-value", 0, false},
		{"empty", nil, "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := ClaudeRequest{Model: "claude-opus-5-5", Messages: []ClaudeMessage{{Role: "user", Content: []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": tc.content}}}}}
			before, err := common.Marshal(req)
			require.NoError(t, err)
			meta := req.GetTokenCountMeta()
			require.Len(t, meta.Files, tc.images)
			require.Equal(t, 1, meta.MessagesCount)
			require.Contains(t, meta.CombineText, tc.wantText)
			require.NotContains(t, meta.CombineText, "aB12+/")
			require.NotContains(t, meta.CombineText, "private=value")
			for _, f := range meta.Files {
				require.Equal(t, types.FileTypeImage, f.FileType)
				require.Equal(t, tc.url, f.IsURL())
				require.NotEmpty(t, f.GetRawData())
			}
			after, err := common.Marshal(req)
			require.NoError(t, err)
			require.Equal(t, string(before), string(after), "estimation must not modify the forwarded request")
		})
	}
}

func TestClaudeToolResultLongImageHistory(t *testing.T) {
	image := ClaudeMediaMessage{Type: "image", Source: &ClaudeMessageSource{Type: "base64", MediaType: "image/jpeg", Data: strings.Repeat("Ab12+Z/7", 32768)}}
	var top, nested ClaudeRequest
	for i := 0; i < 44; i++ {
		top.Messages = append(top.Messages, ClaudeMessage{Role: "user", Content: []ClaudeMediaMessage{image}})
		nested.Messages = append(nested.Messages, ClaudeMessage{Role: "user", Content: []ClaudeMediaMessage{{Type: "tool_result", Content: []ClaudeMediaMessage{image}}}})
	}
	a, b := top.GetTokenCountMeta(), nested.GetTokenCountMeta()
	require.Len(t, b.Files, 44)
	require.Equal(t, a.CombineText, b.CombineText)
	for i := range a.Files {
		require.Equal(t, a.Files[i].GetRawData(), b.Files[i].GetRawData())
	}
}
