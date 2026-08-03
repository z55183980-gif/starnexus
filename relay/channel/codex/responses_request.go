package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

func normalizeCodexResponsesRequest(request *dto.OpenAIResponsesRequest) error {
	if request == nil {
		return nil
	}
	normalizedInput, err := normalizeCodexResponsesInput(request.Input)
	if err != nil {
		return err
	}
	request.Input = normalizedInput

	// ChatGPT's internal Codex endpoint accepts a narrower Responses schema.
	// Keep this list aligned with the proven SUB2API OAuth normalization path.
	request.Metadata = nil
	request.PromptCacheRetention = nil
	request.PromptCacheOptions = nil
	request.SafetyIdentifier = nil
	request.StreamOptions = nil
	request.TopP = nil
	request.User = nil

	if request.Reasoning != nil {
		include, includeErr := ensureCodexReasoningEncryptedContent(request.Include)
		if includeErr != nil {
			return includeErr
		}
		request.Include = include
	}
	return nil
}

func normalizeCodexResponsesInput(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, nil
	}

	items, err := normalizeCodexResponsesInputItems(trimmed)
	if err != nil {
		return nil, err
	}
	if err := scrubCodexResponsesInputItems(items); err != nil {
		return nil, err
	}
	return common.Marshal(items)
}

func normalizeCodexResponsesInputItems(raw json.RawMessage) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	switch trimmed[0] {
	case '[':
		var items []json.RawMessage
		if err := common.Unmarshal(trimmed, &items); err != nil {
			return nil, fmt.Errorf("normalize Codex Responses input: %w", err)
		}
		return items, nil
	case '{':
		var item map[string]json.RawMessage
		if err := common.Unmarshal(trimmed, &item); err != nil {
			return nil, fmt.Errorf("normalize Codex Responses input: %w", err)
		}
		return []json.RawMessage{append(json.RawMessage(nil), trimmed...)}, nil
	case '"':
		var text string
		if err := common.Unmarshal(trimmed, &text); err != nil {
			return nil, fmt.Errorf("normalize Codex Responses input: %w", err)
		}
		if strings.TrimSpace(text) == "" {
			return []json.RawMessage{}, nil
		}
		item, err := common.Marshal(map[string]any{"type": "message", "role": "user", "content": text})
		if err != nil {
			return nil, fmt.Errorf("normalize Codex Responses input: %w", err)
		}
		return []json.RawMessage{item}, nil
	default:
		var item json.RawMessage
		if err := common.Unmarshal(trimmed, &item); err != nil {
			return nil, fmt.Errorf("normalize Codex Responses input: %w", err)
		}
		return []json.RawMessage{append(json.RawMessage(nil), trimmed...)}, nil
	}
}

// scrubCodexResponsesInputItems removes Codex-rejected fields only at known
// Responses schema locations. It intentionally does not recursively delete
// same-named keys inside tool outputs or arbitrary user JSON payloads. Raw JSON
// values are retained so large integers never round-trip through float64.
func scrubCodexResponsesInputItems(items []json.RawMessage) error {
	for index, item := range items {
		normalized, changed, err := scrubCodexResponsesInputItem(item)
		if err != nil {
			return fmt.Errorf("normalize Codex Responses input item %d: %w", index, err)
		}
		if changed {
			items[index] = normalized
		}
	}
	return nil
}

func scrubCodexResponsesInputItem(item json.RawMessage) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(item)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return item, false, nil
	}
	var obj map[string]json.RawMessage
	if err := common.Unmarshal(trimmed, &obj); err != nil {
		return nil, false, err
	}
	itemType := codexJSONRawString(obj["type"])
	changed := false
	if itemType != "image_generation_call" {
		if _, exists := obj["quality"]; exists {
			delete(obj, "quality")
			changed = true
		}
		if _, exists := obj["size"]; exists {
			delete(obj, "size")
			changed = true
		}
	}
	switch itemType {
	case "input_text", "input_image", "input_file":
		if _, exists := obj["prompt_cache_breakpoint"]; exists {
			delete(obj, "prompt_cache_breakpoint")
			changed = true
		}
	}
	if content, exists := obj["content"]; exists {
		normalized, contentChanged, err := scrubCodexResponsesMessageContent(content)
		if err != nil {
			return nil, false, err
		}
		if contentChanged {
			obj["content"] = normalized
			changed = true
		}
	}
	if !changed {
		return item, false, nil
	}
	normalized, err := common.Marshal(obj)
	if err != nil {
		return nil, false, err
	}
	return normalized, true, nil
}

func scrubCodexResponsesMessageContent(content json.RawMessage) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 {
		return content, false, nil
	}
	switch trimmed[0] {
	case '[':
		var blocks []json.RawMessage
		if err := common.Unmarshal(trimmed, &blocks); err != nil {
			return nil, false, err
		}
		changed := false
		for index, block := range blocks {
			normalized, blockChanged, err := scrubCodexResponsesContentBlock(block)
			if err != nil {
				return nil, false, err
			}
			if blockChanged {
				blocks[index] = normalized
				changed = true
			}
		}
		if !changed {
			return content, false, nil
		}
		normalized, err := common.Marshal(blocks)
		return normalized, true, err
	case '{':
		return scrubCodexResponsesContentBlock(content)
	default:
		return content, false, nil
	}
}

func scrubCodexResponsesContentBlock(block json.RawMessage) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(block)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return block, false, nil
	}
	var obj map[string]json.RawMessage
	if err := common.Unmarshal(trimmed, &obj); err != nil {
		return nil, false, err
	}
	switch codexJSONRawString(obj["type"]) {
	case "input_text", "input_image", "input_file", "output_text", "text":
		if _, exists := obj["prompt_cache_breakpoint"]; !exists {
			return block, false, nil
		}
		delete(obj, "prompt_cache_breakpoint")
		normalized, err := common.Marshal(obj)
		return normalized, true, err
	default:
		return block, false, nil
	}
}

func codexJSONRawString(value json.RawMessage) string {
	if len(bytes.TrimSpace(value)) == 0 {
		return ""
	}
	var text string
	if err := common.Unmarshal(value, &text); err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

func ensureCodexReasoningEncryptedContent(raw json.RawMessage) (json.RawMessage, error) {
	const encryptedContent = "reasoning.encrypted_content"
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return common.Marshal([]string{encryptedContent})
	}
	var include []string
	if err := common.Unmarshal(trimmed, &include); err != nil {
		return nil, fmt.Errorf("normalize Codex Responses include: %w", err)
	}
	for _, value := range include {
		if value == encryptedContent {
			return raw, nil
		}
	}
	include = append(include, encryptedContent)
	return common.Marshal(include)
}
