package codex

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexJSONModeInstruction = "Return the response as valid JSON."
const maxCodexStrictSchemaNormalizationDepth = 16

type codexStructuredOutputCompatInfo struct {
	JSONInstructionAdded                 bool `json:"json_instruction_added,omitempty"`
	RequiredFieldsAdded                  int  `json:"required_fields_added,omitempty"`
	AdditionalPropertiesAdded            int  `json:"additional_properties_added,omitempty"`
	UnsupportedFormatsRemoved            int  `json:"unsupported_formats_removed,omitempty"`
	ExplicitAdditionalPropertiesConflict bool `json:"explicit_additional_properties_conflict,omitempty"`
}

func (info codexStructuredOutputCompatInfo) relevant() bool {
	return info.JSONInstructionAdded || info.RequiredFieldsAdded > 0 ||
		info.AdditionalPropertiesAdded > 0 || info.UnsupportedFormatsRemoved > 0 ||
		info.ExplicitAdditionalPropertiesConflict
}

func (info codexStructuredOutputCompatInfo) auditMap() map[string]interface{} {
	audit := make(map[string]interface{})
	if info.JSONInstructionAdded {
		audit["json_instruction_added"] = true
	}
	if info.RequiredFieldsAdded > 0 {
		audit["required_fields_added"] = info.RequiredFieldsAdded
	}
	if info.AdditionalPropertiesAdded > 0 {
		audit["additional_properties_added"] = info.AdditionalPropertiesAdded
	}
	if info.UnsupportedFormatsRemoved > 0 {
		audit["unsupported_formats_removed"] = info.UnsupportedFormatsRemoved
	}
	if info.ExplicitAdditionalPropertiesConflict {
		audit["explicit_additional_properties_conflict"] = true
	}
	return audit
}

// applyCodexStructuredOutputCompatibility applies only the compatibility
// constraints required by the Codex Responses upstream. It intentionally
// leaves non-strict schemas and explicitly contradictory schema settings
// untouched so other OpenAI-compatible channels keep their existing wire
// semantics.
func applyCodexStructuredOutputCompatibility(c *gin.Context, request *dto.OpenAIResponsesRequest) {
	if request == nil || len(bytes.TrimSpace(request.Text)) == 0 {
		return
	}

	formatType := strings.TrimSpace(gjson.GetBytes(request.Text, "format.type").String())
	compatInfo := codexStructuredOutputCompatInfo{}
	switch formatType {
	case "json_object":
		if codexStructuredOutputContextContainsJSON(request) {
			return
		}
		if appendCodexJSONModeInstruction(request) {
			compatInfo.JSONInstructionAdded = true
		}
	case "json_schema":
		schema := gjson.GetBytes(request.Text, "format.schema")
		if !schema.Exists() || !schema.IsObject() {
			return
		}
		normalizedSchema, removedFormats := removeUnsupportedCodexSchemaFormats(json.RawMessage(schema.Raw))
		if removedFormats > 0 {
			updated, err := sjson.SetRawBytes(request.Text, "format.schema", normalizedSchema)
			if err != nil {
				return
			}
			request.Text = updated
			compatInfo.UnsupportedFormatsRemoved = removedFormats
		}
		if gjson.GetBytes(request.Text, "format.strict").Type != gjson.True {
			break
		}
		schema = gjson.GetBytes(request.Text, "format.schema")
		normalized, schemaInfo := normalizeCodexStrictSchema(json.RawMessage(schema.Raw))
		compatInfo.RequiredFieldsAdded = schemaInfo.RequiredFieldsAdded
		compatInfo.AdditionalPropertiesAdded = schemaInfo.AdditionalPropertiesAdded
		compatInfo.ExplicitAdditionalPropertiesConflict = schemaInfo.ExplicitAdditionalPropertiesConflict
		if schemaInfo.ExplicitAdditionalPropertiesConflict {
			compatInfo.RequiredFieldsAdded = 0
			compatInfo.AdditionalPropertiesAdded = 0
		} else if schemaInfo.RequiredFieldsAdded > 0 || schemaInfo.AdditionalPropertiesAdded > 0 {
			if updated, err := sjson.SetRawBytes(request.Text, "format.schema", normalized); err == nil {
				request.Text = updated
			} else {
				return
			}
		}
	default:
		return
	}

	if c != nil && compatInfo.relevant() {
		c.Set("codex_structured_output_compat", compatInfo.auditMap())
	}
}

// removeUnsupportedCodexSchemaFormats removes schema keywords that the Codex
// Responses backend rejects while preserving the surrounding property shape.
// The cleanup is deliberately limited to JSON Schema child locations and does
// not rewrite arbitrary values nested in defaults, examples, or tool payloads.
func removeUnsupportedCodexSchemaFormats(raw json.RawMessage) (json.RawMessage, int) {
	return removeUnsupportedCodexSchemaFormatsNode(raw, 0)
}

func removeUnsupportedCodexSchemaFormatsNode(raw json.RawMessage, depth int) (json.RawMessage, int) {
	if depth > maxCodexStrictSchemaNormalizationDepth {
		return raw, 0
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() {
		return raw, 0
	}

	out := append(json.RawMessage(nil), raw...)
	removed := 0
	if root.Get("format").Type == gjson.String && root.Get("format").String() == "uri" {
		if updated, err := sjson.DeleteBytes(out, "format"); err == nil {
			out = updated
			removed++
		}
	}

	for _, field := range []string{"properties", "$defs", "definitions", "patternProperties"} {
		container := gjson.GetBytes(out, escapeGJSONPath(field))
		if !container.Exists() || !container.IsObject() {
			continue
		}
		containerRaw := []byte(container.Raw)
		changed := false
		container.ForEach(func(key, value gjson.Result) bool {
			if !value.IsObject() {
				return true
			}
			normalized, count := removeUnsupportedCodexSchemaFormatsNode(json.RawMessage(value.Raw), depth+1)
			if count == 0 {
				return true
			}
			if updated, err := sjson.SetRawBytes(containerRaw, escapeSJSONPath(key.String()), normalized); err == nil {
				containerRaw = updated
				changed = true
				removed += count
			}
			return true
		})
		if changed {
			if updated, err := sjson.SetRawBytes(out, escapeSJSONPath(field), containerRaw); err == nil {
				out = updated
			}
		}
	}

	for _, field := range []string{"items", "contains", "propertyNames", "additionalProperties", "not", "if", "then", "else"} {
		child := gjson.GetBytes(out, escapeGJSONPath(field))
		if !child.Exists() {
			continue
		}
		if child.IsObject() {
			normalized, count := removeUnsupportedCodexSchemaFormatsNode(json.RawMessage(child.Raw), depth+1)
			if count > 0 {
				if updated, err := sjson.SetRawBytes(out, escapeSJSONPath(field), normalized); err == nil {
					out = updated
					removed += count
				}
			}
			continue
		}
		if !child.IsArray() {
			continue
		}
		arrayRaw := []byte(child.Raw)
		changed := false
		child.ForEach(func(key, value gjson.Result) bool {
			if !value.IsObject() {
				return true
			}
			normalized, count := removeUnsupportedCodexSchemaFormatsNode(json.RawMessage(value.Raw), depth+1)
			if count == 0 {
				return true
			}
			if updated, err := sjson.SetRawBytes(arrayRaw, strconv.Itoa(int(key.Int())), normalized); err == nil {
				arrayRaw = updated
				changed = true
				removed += count
			}
			return true
		})
		if changed {
			if updated, err := sjson.SetRawBytes(out, escapeSJSONPath(field), arrayRaw); err == nil {
				out = updated
			}
		}
	}

	for _, field := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		array := gjson.GetBytes(out, escapeGJSONPath(field))
		if !array.Exists() || !array.IsArray() {
			continue
		}
		arrayRaw := []byte(array.Raw)
		changed := false
		array.ForEach(func(key, value gjson.Result) bool {
			if !value.IsObject() {
				return true
			}
			normalized, count := removeUnsupportedCodexSchemaFormatsNode(json.RawMessage(value.Raw), depth+1)
			if count == 0 {
				return true
			}
			if updated, err := sjson.SetRawBytes(arrayRaw, strconv.Itoa(int(key.Int())), normalized); err == nil {
				arrayRaw = updated
				changed = true
				removed += count
			}
			return true
		})
		if changed {
			if updated, err := sjson.SetRawBytes(out, escapeSJSONPath(field), arrayRaw); err == nil {
				out = updated
			}
		}
	}

	return out, removed
}

func codexStructuredOutputContextContainsJSON(request *dto.OpenAIResponsesRequest) bool {
	if request == nil {
		return false
	}
	// The Codex Responses backend validates JSON mode against input messages
	// only. Text in the top-level instructions field does not satisfy it.
	return containsASCIIJSON(request.Input)
}

func containsASCIIJSON(raw []byte) bool {
	for index := 0; index+4 <= len(raw); index++ {
		if (raw[index] == 'j' || raw[index] == 'J') &&
			(raw[index+1] == 's' || raw[index+1] == 'S') &&
			(raw[index+2] == 'o' || raw[index+2] == 'O') &&
			(raw[index+3] == 'n' || raw[index+3] == 'N') {
			return true
		}
	}
	return false
}

func appendCodexJSONModeInstruction(request *dto.OpenAIResponsesRequest) bool {
	if request == nil {
		return false
	}
	trimmed := bytes.TrimSpace(request.Input)
	if len(trimmed) == 0 {
		return false
	}

	if trimmed[0] == '"' {
		var input string
		if common.Unmarshal(trimmed, &input) != nil {
			return false
		}
		input = strings.TrimSpace(input)
		if input != "" {
			input += "\n\n"
		}
		encoded, err := common.Marshal(input + codexJSONModeInstruction)
		if err != nil {
			return false
		}
		request.Input = encoded
		return true
	}

	if trimmed[0] != '[' {
		return false
	}
	var items []json.RawMessage
	if common.Unmarshal(trimmed, &items) != nil {
		return false
	}
	marker := json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"Return the response as valid JSON."}]}`)
	items = append(items, marker)
	encoded, err := common.Marshal(items)
	if err != nil {
		return false
	}
	request.Input = encoded
	return true
}

func normalizeCodexStrictSchema(raw json.RawMessage) (json.RawMessage, codexStructuredOutputCompatInfo) {
	info := codexStructuredOutputCompatInfo{}
	normalized := normalizeCodexStrictSchemaNode(raw, &info, true, 0)
	return normalized, info
}

func normalizeCodexStrictSchemaNode(raw json.RawMessage, info *codexStructuredOutputCompatInfo, isRoot bool, depth int) json.RawMessage {
	if depth > maxCodexStrictSchemaNormalizationDepth {
		return raw
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() {
		return raw
	}
	// OpenAI Structured Outputs requires an object at the root. Do not try to
	// repair a root union because choosing or wrapping a branch would change the
	// client's declared response shape.
	if isRoot && root.Get("anyOf").Exists() {
		return raw
	}
	out := append(json.RawMessage(nil), raw...)

	for _, field := range []string{"properties", "$defs", "definitions"} {
		container := gjson.GetBytes(out, escapeGJSONPath(field))
		if !container.Exists() || !container.IsObject() {
			continue
		}
		containerRaw := []byte(container.Raw)
		changed := false
		container.ForEach(func(key, value gjson.Result) bool {
			if !value.IsObject() {
				return true
			}
			normalizedChild := normalizeCodexStrictSchemaNode(json.RawMessage(value.Raw), info, false, depth+1)
			if bytes.Equal(normalizedChild, []byte(value.Raw)) {
				return true
			}
			updated, err := sjson.SetRawBytes(containerRaw, escapeSJSONPath(key.String()), normalizedChild)
			if err == nil {
				containerRaw = updated
				changed = true
			}
			return true
		})
		if changed {
			if updated, err := sjson.SetRawBytes(out, escapeSJSONPath(field), containerRaw); err == nil {
				out = updated
			}
		}
	}

	for _, field := range []string{"items"} {
		child := gjson.GetBytes(out, field)
		if !child.Exists() || !child.IsObject() {
			continue
		}
		normalizedChild := normalizeCodexStrictSchemaNode(json.RawMessage(child.Raw), info, false, depth+1)
		if !bytes.Equal(normalizedChild, []byte(child.Raw)) {
			if updated, err := sjson.SetRawBytes(out, field, normalizedChild); err == nil {
				out = updated
			}
		}
	}

	for _, field := range []string{"anyOf"} {
		variants := gjson.GetBytes(out, field)
		if !variants.Exists() || !variants.IsArray() {
			continue
		}
		variantsRaw := []byte(variants.Raw)
		changed := false
		variants.ForEach(func(key, value gjson.Result) bool {
			if !value.IsObject() {
				return true
			}
			normalizedChild := normalizeCodexStrictSchemaNode(json.RawMessage(value.Raw), info, false, depth+1)
			if bytes.Equal(normalizedChild, []byte(value.Raw)) {
				return true
			}
			updated, err := sjson.SetRawBytes(variantsRaw, strconv.Itoa(int(key.Int())), normalizedChild)
			if err == nil {
				variantsRaw = updated
				changed = true
			}
			return true
		})
		if changed {
			if updated, err := sjson.SetRawBytes(out, field, variantsRaw); err == nil {
				out = updated
			}
		}
	}

	root = gjson.ParseBytes(out)
	properties := root.Get("properties")
	isObject := root.Get("type").String() == "object" || properties.IsObject()
	if !isObject {
		return out
	}

	if properties.IsObject() {
		propertyNames := make([]string, 0)
		properties.ForEach(func(key, _ gjson.Result) bool {
			propertyNames = append(propertyNames, key.String())
			return true
		})
		if len(propertyNames) > 0 {
			required := root.Get("required")
			if !required.Exists() || required.IsArray() {
				requiredNames := make([]string, 0)
				requiredSet := make(map[string]struct{})
				validRequired := true
				if required.Exists() {
					required.ForEach(func(_, value gjson.Result) bool {
						if value.Type != gjson.String {
							validRequired = false
							return false
						}
						requiredNames = append(requiredNames, value.String())
						requiredSet[value.String()] = struct{}{}
						return true
					})
				}
				if validRequired {
					added := 0
					for _, propertyName := range propertyNames {
						if _, exists := requiredSet[propertyName]; exists {
							continue
						}
						requiredNames = append(requiredNames, propertyName)
						requiredSet[propertyName] = struct{}{}
						added++
					}
					if added > 0 {
						if encoded, err := common.Marshal(requiredNames); err == nil {
							if updated, err := sjson.SetRawBytes(out, "required", encoded); err == nil {
								out = updated
								info.RequiredFieldsAdded += added
							}
						}
					}
				}
			}
		}
	}

	additionalProperties := gjson.GetBytes(out, "additionalProperties")
	if !additionalProperties.Exists() {
		if updated, err := sjson.SetBytes(out, "additionalProperties", false); err == nil {
			out = updated
			info.AdditionalPropertiesAdded++
		}
	} else if additionalProperties.Type != gjson.False {
		info.ExplicitAdditionalPropertiesConflict = true
	}
	return out
}

func escapeSJSONPath(key string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		".", "\\.",
		"*", "\\*",
		"?", "\\?",
		"#", "\\#",
	)
	return replacer.Replace(key)
}

func escapeGJSONPath(key string) string {
	return strings.ReplaceAll(key, ".", "\\.")
}
