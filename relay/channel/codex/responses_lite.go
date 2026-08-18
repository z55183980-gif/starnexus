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
// Responses Lite endpoint. Besides requiring reasoning over all turns, Lite
// carries private namespace declarations through input.additional_tools and
// only accepts a narrow set of top-level tool kinds.
func NormalizeResponsesLitePayload(body []byte) ([]byte, bool, error) {
	var requestBody map[string]any
	if err := common.Unmarshal(body, &requestBody); err != nil {
		return body, false, fmt.Errorf("decode Responses Lite request body: %w", err)
	}

	changed, err := normalizeResponsesLiteTools(requestBody)
	if err != nil {
		return body, false, err
	}
	if ensureResponsesLiteParallelToolCalls(requestBody) {
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
		return ensureResponsesLiteReasoningContext(requestBody)
	}
	tools, ok := rawTools.([]any)
	if !ok {
		return false, fmt.Errorf("Responses Lite requires tools to be an array")
	}

	topLevelTools := make([]any, 0, len(tools))
	namespaceTools := make([]any, 0, len(tools))
	for index, rawTool := range tools {
		if customTool, ok := rawTool.(string); ok {
			if strings.TrimSpace(customTool) == "" {
				return false, fmt.Errorf("Responses Lite custom tool at index %d must not be empty", index)
			}
			topLevelTools = append(topLevelTools, rawTool)
			continue
		}

		tool, ok := rawTool.(map[string]any)
		if !ok {
			return false, fmt.Errorf("Responses Lite tool at index %d must be an object", index)
		}
		toolType := strings.TrimSpace(responsesLiteString(tool["type"]))
		switch toolType {
		case "function", "custom", "tool_search":
			topLevelTools = append(topLevelTools, rawTool)
		case "namespace":
			namespaceTools = append(namespaceTools, rawTool)
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
	if len(namespaceTools) == 0 {
		return contextChanged, nil
	}

	input, err := appendResponsesLiteAdditionalTools(requestBody["input"], namespaceTools)
	if err != nil {
		return false, err
	}
	requestBody["input"] = input
	if len(topLevelTools) == 0 {
		delete(requestBody, "tools")
	} else {
		requestBody["tools"] = topLevelTools
	}
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

func appendResponsesLiteAdditionalTools(input any, namespaceTools []any) ([]any, error) {
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
		return nil, fmt.Errorf("Responses Lite namespace tools require input to be a string or array")
	}

	var target map[string]any
	var targetTools []any
	var allAdditionalTools []any
	for _, rawItem := range items {
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
			return nil, fmt.Errorf("Responses Lite input.additional_tools tools must be an array")
		}
		if target == nil {
			target = item
			targetTools = additionalTools
		}
		allAdditionalTools = append(allAdditionalTools, additionalTools...)
	}

	merged, err := mergeResponsesLiteAdditionalTools(allAdditionalTools, namespaceTools)
	if err != nil {
		return nil, err
	}
	newTools := merged[len(allAdditionalTools):]
	if target != nil {
		if len(newTools) > 0 {
			target["tools"] = append(append([]any(nil), targetTools...), newTools...)
		}
		return items, nil
	}

	items = append(items, map[string]any{
		"type":  "additional_tools",
		"role":  "developer",
		"tools": newTools,
	})
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
