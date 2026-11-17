package codex

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	return gjson.ValidBytes(body) && IsResponsesLiteHeader(gjson.GetBytes(body, "client_metadata."+ResponsesLiteWSMetadataKey).String())
}

// ResponsesLiteDiagnostics contains no user content, schemas, or tool names.
// A compatibility fallback is not a request failure.
type ResponsesLiteDiagnostics struct {
	AdditionalToolItems int
	MigratedTools       int
	FallbackReason      string
}

func NormalizeResponsesLitePayload(body []byte) ([]byte, bool, error) {
	normalized, changed, _, err := NormalizeResponsesLitePayloadWithDiagnostics(body)
	return normalized, changed, err
}

// NormalizeResponsesLitePayloadWithDiagnostics preserves the ordered tool
// history. Additional tools become available at their position in input; they
// are not a globally unique registry. Only request-level tools are migrated.
// RawMessage avoids rounding schema numbers or losing unknown extension fields.
func NormalizeResponsesLitePayloadWithDiagnostics(body []byte) ([]byte, bool, ResponsesLiteDiagnostics, error) {
	var request map[string]json.RawMessage
	var diagnostics ResponsesLiteDiagnostics
	if err := common.Unmarshal(body, &request); err != nil || request == nil {
		return body, false, diagnostics, fmt.Errorf("Responses Lite request body must be a JSON object")
	}
	original, _ := common.Marshal(request)
	// Decode into fresh data and return only fully normalized requests. Errors
	// must never expose a partially migrated tools catalog to the caller.
	if err := normalizeResponsesLiteReasoning(request); err != nil {
		return body, false, diagnostics, err
	}
	diagnostics = normalizeResponsesLiteTools(request)
	if err := normalizeResponsesLiteInstructions(request); err != nil {
		return body, false, diagnostics, err
	}
	request["parallel_tool_calls"] = json.RawMessage("false")
	request["store"] = json.RawMessage("false")
	rebuilt, err := common.Marshal(request)
	if err != nil {
		return body, false, diagnostics, fmt.Errorf("encode Responses Lite request body: %w", err)
	}
	if bytes.Equal(original, rebuilt) {
		return body, false, diagnostics, nil
	}
	return rebuilt, true, diagnostics, nil
}

func normalizeResponsesLiteReasoning(request map[string]json.RawMessage) error {
	reasoning := make(map[string]json.RawMessage)
	if raw := request["reasoning"]; !responsesLiteNull(raw) {
		if common.Unmarshal(raw, &reasoning) != nil || reasoning == nil {
			return fmt.Errorf("Responses Lite requires reasoning to be an object")
		}
	}
	reasoning["context"] = json.RawMessage(`"all_turns"`)
	request["reasoning"], _ = common.Marshal(reasoning)
	var include []json.RawMessage
	if raw := request["include"]; !responsesLiteNull(raw) {
		if common.Unmarshal(raw, &include) != nil {
			return nil // Preserve the previous policy for unknown include shapes.
		}
	}
	for _, item := range include {
		var value string
		if common.Unmarshal(item, &value) == nil && value == "reasoning.encrypted_content" {
			return nil
		}
	}
	include = append(include, json.RawMessage(`"reasoning.encrypted_content"`))
	request["include"], _ = common.Marshal(include)
	return nil
}

func normalizeResponsesLiteTools(request map[string]json.RawMessage) ResponsesLiteDiagnostics {
	var diagnostics ResponsesLiteDiagnostics
	items, ok := responsesLiteInputItems(request["input"])
	if !ok {
		diagnostics.FallbackReason = "unsupported_input_shape"
		return diagnostics
	}
	for _, item := range items {
		if gjson.GetBytes(item, "type").String() == "additional_tools" {
			diagnostics.AdditionalToolItems++
		}
	}
	if common.GetEnvOrDefaultBool("RESPONSES_LITE_TOOLS_PASSTHROUGH", false) {
		diagnostics.FallbackReason = "tools_passthrough_enabled"
		return diagnostics
	}
	var tools []json.RawMessage
	if raw := request["tools"]; !responsesLiteNull(raw) {
		if common.Unmarshal(raw, &tools) != nil {
			diagnostics.FallbackReason = "unsupported_tools_shape"
			return diagnostics
		}
		for _, tool := range tools {
			if !responsesLiteMigratableTool(tool) {
				diagnostics.FallbackReason = "unsupported_tool_definition"
				return diagnostics
			}
		}
	}
	// Never deduplicate, validate conflicts, or mutate existing declarations.
	// A request-level catalog belongs before history, in a separate item.
	if len(tools) > 0 || diagnostics.AdditionalToolItems == 0 {
		if tools == nil {
			tools = []json.RawMessage{}
		}
		prefix, err := common.Marshal(struct {
			Type  string            `json:"type"`
			Role  string            `json:"role"`
			Tools []json.RawMessage `json:"tools"`
		}{"additional_tools", "developer", tools})
		if err != nil {
			diagnostics.FallbackReason = "tool_prefix_encoding_failed"
			return diagnostics
		}
		updated, err := common.Marshal(append([]json.RawMessage{prefix}, items...))
		if err != nil {
			diagnostics.FallbackReason = "tool_input_encoding_failed"
			return diagnostics
		}
		request["input"] = updated
		diagnostics.MigratedTools = len(tools)
	}
	// Remove tools only after the complete migration succeeded. Unknown tool
	// shapes above retain both original tools and input for the upstream.
	delete(request, "tools")
	return diagnostics
}

func responsesLiteMigratableTool(raw json.RawMessage) bool {
	var shorthand string
	if common.Unmarshal(raw, &shorthand) == nil && strings.TrimSpace(shorthand) != "" {
		return true
	}
	var tool struct {
		Type string `json:"type"`
	}
	if common.Unmarshal(raw, &tool) != nil {
		return false
	}
	switch strings.TrimSpace(tool.Type) {
	case "function", "custom", "tool_search", "namespace":
		return true
	default:
		return false
	}
}

func normalizeResponsesLiteInstructions(request map[string]json.RawMessage) error {
	var instructions string
	if raw := request["instructions"]; !responsesLiteNull(raw) && common.Unmarshal(raw, &instructions) != nil {
		return fmt.Errorf("Responses Lite instructions must be a string")
	}
	if strings.TrimSpace(instructions) != "" {
		items, ok := responsesLiteInputItems(request["input"])
		if !ok {
			return fmt.Errorf("Responses Lite instructions require input to be a string or array")
		}
		message, err := common.Marshal(map[string]any{
			"type": "message", "role": "developer",
			"content": []map[string]string{{"type": "input_text", "text": instructions}},
		})
		if err != nil {
			return err
		}
		insertAt := 0
		for insertAt < len(items) && gjson.GetBytes(items[insertAt], "type").String() == "additional_tools" {
			insertAt++
		}
		updated := make([]json.RawMessage, 0, len(items)+1)
		updated = append(updated, items[:insertAt]...)
		updated = append(updated, message)
		updated = append(updated, items[insertAt:]...)
		encoded, err := common.Marshal(updated)
		if err != nil {
			return err
		}
		request["input"] = encoded
	}
	request["instructions"] = json.RawMessage(`""`)
	return nil
}

func responsesLiteInputItems(raw json.RawMessage) ([]json.RawMessage, bool) {
	if responsesLiteNull(raw) {
		return nil, true
	}
	var items []json.RawMessage
	if common.Unmarshal(raw, &items) == nil {
		return items, true
	}
	var text string
	if common.Unmarshal(raw, &text) == nil {
		message, err := common.Marshal(map[string]string{"type": "message", "role": "user", "content": text})
		return []json.RawMessage{message}, err == nil
	}
	return nil, false
}

func responsesLiteNull(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
