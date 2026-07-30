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
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] == '[' {
		return raw, nil
	}
	var item any
	if err := common.Unmarshal(trimmed, &item); err != nil {
		return nil, fmt.Errorf("normalize Codex Responses input: %w", err)
	}
	if text, ok := item.(string); ok {
		if strings.TrimSpace(text) == "" {
			return common.Marshal([]any{})
		}
		item = map[string]any{"type": "message", "role": "user", "content": text}
	}
	return common.Marshal([]any{item})
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
