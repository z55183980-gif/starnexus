package codex

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/tidwall/gjson"
)

const (
	ResponsesLiteHeader        = "X-OpenAI-Internal-Codex-Responses-Lite"
	ResponsesLiteWSMetadataKey = "ws_request_header_x_openai_internal_codex_responses_lite"
)

func IsResponsesLiteHeader(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

func IsResponsesLiteWebSocketPayload(body []byte) bool {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return false
	}
	return IsResponsesLiteHeader(gjson.GetBytes(body, "client_metadata."+ResponsesLiteWSMetadataKey).String())
}

// NormalizeResponsesLitePayload applies the wire contract used by the Codex
// Responses Lite endpoint. Lite requires reasoning over all turns, keeps
// instructions and tools inside input, and uses store=false with serialized
// tool declarations in input.additional_tools.
func NormalizeResponsesLitePayload(body []byte) ([]byte, bool, error) {
	var requestBody map[string]any
	if err := common.Unmarshal(body, &requestBody); err != nil {
		return body, false, fmt.Errorf("decode Responses Lite request body: %w", err)
	}

	changed, err := normalizeResponsesLiteTools(requestBody)
	if err != nil {
		return body, false, err
	}
	instructionsChanged, err := normalizeResponsesLiteInstructions(requestBody)
	if err != nil {
		return body, false, err
	}
	if instructionsChanged {
		changed = true
	}
	if ensureResponsesLiteParallelToolCalls(requestBody) {
		changed = true
	}
	if ensureResponsesLiteStore(requestBody) {
		changed = true
	}
	if ensureResponsesLiteReasoningInclude(requestBody) {
		changed = true
	}
	if !changed {
		return body, false, nil
	}

	rebuilt, err := common.Marshal(requestBody)
	if err != nil {
		return body, false, fmt.Errorf("encode Responses Lite request body: %w", err)
	}
	return rebuilt, true, nil
}

func ensureResponsesLiteStore(requestBody map[string]any) bool {
	if requestBody == nil {
		return false
	}
	if value, exists := requestBody["store"]; exists {
		if store, ok := value.(bool); ok && !store {
			return false
		}
	}
	requestBody["store"] = false
	return true
}

func normalizeResponsesLiteInstructions(requestBody map[string]any) (bool, error) {
	if requestBody == nil {
		return false, nil
	}

	rawInstructions, exists := requestBody["instructions"]
	if !exists || rawInstructions == nil {
		requestBody["instructions"] = ""
		return true, nil
	}
	instructions, ok := rawInstructions.(string)
	if !ok {
		return false, fmt.Errorf("Responses Lite instructions must be a string")
	}
	if strings.TrimSpace(instructions) == "" {
		if instructions == "" {
			return false, nil
		}
		requestBody["instructions"] = ""
		return true, nil
	}

	input, err := prependResponsesLiteDeveloperMessage(requestBody["input"], instructions)
	if err != nil {
		return false, err
	}
	requestBody["input"] = input
	requestBody["instructions"] = ""
	return true, nil
}

func ensureResponsesLiteParallelToolCalls(requestBody map[string]any) bool {
	if requestBody == nil {
		return false
	}
	if value, exists := requestBody["parallel_tool_calls"]; exists {
		if parallel, ok := value.(bool); ok && !parallel {
			return false
		}
	}
	requestBody["parallel_tool_calls"] = false
	return true
}

func normalizeResponsesLiteTools(requestBody map[string]any) (bool, error) {
	if requestBody == nil {
		return false, nil
	}
	if rawReasoning, exists := requestBody["reasoning"]; exists && rawReasoning != nil {
		if _, ok := rawReasoning.(map[string]any); !ok {
			return false, fmt.Errorf("Responses Lite requires reasoning to be an object")
		}
	}

	rawTools, exists := requestBody["tools"]
	if !exists || rawTools == nil {
		if exists {
			delete(requestBody, "tools")
		}
		contextChanged, err := ensureResponsesLiteReasoningContext(requestBody)
		if err != nil {
			return false, err
		}
		input, inputChanged, inputErr := appendResponsesLiteAdditionalTools(requestBody["input"], nil)
		if inputErr != nil {
			return false, inputErr
		}
		requestBody["input"] = input
		return contextChanged || inputChanged || exists, nil
	}
	tools, ok := rawTools.([]any)
	if !ok {
		return false, fmt.Errorf("Responses Lite requires tools to be an array")
	}

	liteTools := make([]any, 0, len(tools))
	for index, rawTool := range tools {
		if customTool, ok := rawTool.(string); ok {
			if strings.TrimSpace(customTool) == "" {
				return false, fmt.Errorf("Responses Lite custom tool at index %d must not be empty", index)
			}
			liteTools = append(liteTools, rawTool)
			continue
		}

		tool, ok := rawTool.(map[string]any)
		if !ok {
			return false, fmt.Errorf("Responses Lite tool at index %d must be an object", index)
		}
		toolType := strings.TrimSpace(responsesLiteString(tool["type"]))
		switch toolType {
		case "function", "custom", "tool_search", "namespace":
			liteTools = append(liteTools, rawTool)
		case "":
			return false, fmt.Errorf("Responses Lite tool at index %d is missing type", index)
		default:
			return false, fmt.Errorf("Responses Lite does not support top-level tool type %q at index %d", toolType, index)
		}
	}

	contextChanged, err := ensureResponsesLiteReasoningContext(requestBody)
	if err != nil {
		return false, err
	}
	if len(liteTools) == 0 {
		if _, exists := requestBody["tools"]; exists {
			delete(requestBody, "tools")
			input, _, inputErr := appendResponsesLiteAdditionalTools(requestBody["input"], nil)
			if inputErr != nil {
				return false, inputErr
			}
			requestBody["input"] = input
			return true, nil
		}
		return contextChanged, nil
	}

	input, _, err := appendResponsesLiteAdditionalTools(requestBody["input"], liteTools)
	if err != nil {
		return false, err
	}
	requestBody["input"] = input
	delete(requestBody, "tools")
	return true, nil
}

func ensureResponsesLiteReasoningContext(requestBody map[string]any) (bool, error) {
	rawReasoning, exists := requestBody["reasoning"]
	if !exists || rawReasoning == nil {
		requestBody["reasoning"] = map[string]any{"context": "all_turns"}
		return true, nil
	}
	reasoning, ok := rawReasoning.(map[string]any)
	if !ok {
		return false, fmt.Errorf("Responses Lite requires reasoning to be an object")
	}
	if context, ok := reasoning["context"].(string); ok && context == "all_turns" {
		return false, nil
	}
	reasoning["context"] = "all_turns"
	return true, nil
}

func ensureResponsesLiteReasoningInclude(requestBody map[string]any) bool {
	reasoning, ok := requestBody["reasoning"].(map[string]any)
	if !ok || len(reasoning) == 0 {
		return false
	}
	const encryptedContent = "reasoning.encrypted_content"

	existing, exists := requestBody["include"]
	if !exists || existing == nil {
		requestBody["include"] = []any{encryptedContent}
		return true
	}
	include, ok := existing.([]any)
	if !ok {
		return false
	}
	for _, item := range include {
		if value, ok := item.(string); ok && value == encryptedContent {
			return false
		}
	}
	requestBody["include"] = append(include, encryptedContent)
	return true
}

func appendResponsesLiteAdditionalTools(input any, liteTools []any) ([]any, bool, error) {
	var items []any
	switch typed := input.(type) {
	case nil:
		items = make([]any, 0, 1)
	case string:
		items = []any{map[string]any{
			"type":    "message",
			"role":    "user",
			"content": typed,
		}}
	case []any:
		items = typed
	default:
		return nil, false, fmt.Errorf("Responses Lite tools require input to be a string or array")
	}

	var target map[string]any
	var targetTools []any
	targetIndex := -1
	var allAdditionalTools []any
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok || strings.TrimSpace(responsesLiteString(item["type"])) != "additional_tools" {
			continue
		}
		rawAdditionalTools, exists := item["tools"]
		additionalTools := []any(nil)
		toolsOK := true
		if exists && rawAdditionalTools != nil {
			additionalTools, toolsOK = rawAdditionalTools.([]any)
		}
		if !toolsOK {
			return nil, false, fmt.Errorf("Responses Lite input.additional_tools tools must be an array")
		}
		if target == nil {
			target = item
			targetTools = additionalTools
			targetIndex = index
		}
		allAdditionalTools = append(allAdditionalTools, additionalTools...)
	}

	merged, err := mergeResponsesLiteAdditionalTools(allAdditionalTools, liteTools)
	if err != nil {
		return nil, false, err
	}
	newTools := merged[len(allAdditionalTools):]
	changed := false
	if target != nil {
		if strings.TrimSpace(responsesLiteString(target["role"])) != "developer" {
			target["role"] = "developer"
			changed = true
		}
		if len(newTools) > 0 {
			target["tools"] = append(append([]any(nil), targetTools...), newTools...)
			changed = true
		} else if targetTools == nil {
			target["tools"] = []any{}
			changed = true
		}
		if targetIndex > 0 {
			copy(items[targetIndex:], items[targetIndex+1:])
			items = items[:len(items)-1]
			items = append([]any{target}, items...)
			changed = true
		}
		return items, changed, nil
	}

	if newTools == nil {
		newTools = []any{}
	}
	items = append([]any{map[string]any{
		"type":  "additional_tools",
		"role":  "developer",
		"tools": newTools,
	}}, items...)
	return items, true, nil
}

func prependResponsesLiteDeveloperMessage(input any, instructions string) ([]any, error) {
	var items []any
	switch typed := input.(type) {
	case nil:
		items = make([]any, 0, 1)
	case string:
		items = []any{map[string]any{
			"type":    "message",
			"role":    "user",
			"content": typed,
		}}
	case []any:
		items = typed
	default:
		return nil, fmt.Errorf("Responses Lite instructions require input to be a string or array")
	}

	developerMessage := map[string]any{
		"type": "message",
		"role": "developer",
		"content": []any{
			map[string]any{
				"type": "input_text",
				"text": instructions,
			},
		},
	}
	insertAt := 0
	for insertAt < len(items) {
		item, ok := items[insertAt].(map[string]any)
		if !ok || strings.TrimSpace(responsesLiteString(item["type"])) != "additional_tools" {
			break
		}
		insertAt++
	}
	items = append(items, nil)
	copy(items[insertAt+1:], items[insertAt:])
	items[insertAt] = developerMessage
	return items, nil
}

func mergeResponsesLiteAdditionalTools(existing []any, moved []any) ([]any, error) {
	merged := append([]any(nil), existing...)
	seen := make(map[string]any, len(existing)+len(moved))
	for _, rawTool := range existing {
		if identity := responsesLiteToolIdentity(rawTool); identity != "" {
			if previous, exists := seen[identity]; exists && !reflect.DeepEqual(previous, rawTool) {
				return nil, fmt.Errorf("Responses Lite additional_tools contains conflicting definitions for %s", responsesLiteToolIdentityForError(rawTool))
			}
			seen[identity] = rawTool
		}
	}
	for _, rawTool := range moved {
		identity := responsesLiteToolIdentity(rawTool)
		if identity != "" {
			if previous, exists := seen[identity]; exists {
				if reflect.DeepEqual(previous, rawTool) {
					continue
				}
				return nil, fmt.Errorf("Responses Lite additional_tools conflicts with migrated %s", responsesLiteToolIdentityForError(rawTool))
			}
			seen[identity] = rawTool
		}
		merged = append(merged, rawTool)
	}
	return merged, nil
}

func responsesLiteToolIdentity(rawTool any) string {
	tool, ok := rawTool.(map[string]any)
	if !ok {
		return ""
	}
	toolType := strings.TrimSpace(responsesLiteString(tool["type"]))
	name := strings.TrimSpace(responsesLiteString(tool["name"]))
	if toolType == "" || name == "" {
		return ""
	}
	return toolType + "\x00" + name
}

func responsesLiteToolIdentityForError(rawTool any) string {
	tool, _ := rawTool.(map[string]any)
	return fmt.Sprintf(
		"tool type %q name %q",
		strings.TrimSpace(responsesLiteString(tool["type"])),
		strings.TrimSpace(responsesLiteString(tool["name"])),
	)
}

func responsesLiteString(value any) string {
	text, _ := value.(string)
	return text
}
