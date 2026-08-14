package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"

	"github.com/tidwall/sjson"
)

type codexInputRepairResult struct {
	DroppedReasoningItems int
	DroppedItemReferences int
	RemovedMessageIDs     int
	FirstDroppedIndex     int
	FirstRemovedIndex     int
	RemainingItems        int
	HasOrphanToolOutput   bool
}

type codexReasoningContentNormalizationResult struct {
	MovedSummaryBlocks int
	RemovedNullContent int
}

func (r codexReasoningContentNormalizationResult) Modified() bool {
	return r.MovedSummaryBlocks > 0 || r.RemovedNullContent > 0
}

func (r codexInputRepairResult) DroppedItems() int {
	return r.DroppedReasoningItems + r.DroppedItemReferences
}

func (r codexInputRepairResult) Modified() bool {
	return r.DroppedItems() > 0 || r.RemovedMessageIDs > 0
}

// repairCodexInvalidLocalItemIDs removes known invalid top-level local items
// and strips only invalid top-level message IDs. It does not rewrite IDs or
// inspect nested tool payloads.
func repairCodexInvalidLocalItemIDs(raw json.RawMessage) (json.RawMessage, codexInputRepairResult, error) {
	result := codexInputRepairResult{FirstDroppedIndex: -1, FirstRemovedIndex: -1}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, result, nil
	}

	var items []json.RawMessage
	inputIsArray := false
	switch trimmed[0] {
	case '[':
		inputIsArray = true
		if err := common.Unmarshal(trimmed, &items); err != nil {
			return nil, result, fmt.Errorf("repair Codex Responses input: %w", err)
		}
	case '{':
		items = []json.RawMessage{append(json.RawMessage(nil), trimmed...)}
	default:
		return raw, result, nil
	}

	filtered := make([]json.RawMessage, 0, len(items))
	for index, item := range items {
		itemType, itemID, err := codexResponsesItemTypeAndID(item)
		if err != nil {
			return nil, result, fmt.Errorf("repair Codex Responses input item %d: %w", index, err)
		}
		// Codex requires message IDs to use its provider-issued msg prefix. Drop
		// only the invalid ID before the first upstream request; keep the message
		// and its array position unchanged.
		if inputIsArray && itemType == "message" && itemID != "" && !strings.HasPrefix(itemID, "msg") {
			repairedItem, err := sjson.DeleteBytes(item, "id")
			if err != nil {
				return nil, result, fmt.Errorf("repair Codex Responses message item %d: %w", index, err)
			}
			filtered = append(filtered, repairedItem)
			result.RemovedMessageIDs++
			if result.FirstRemovedIndex < 0 {
				result.FirstRemovedIndex = index
			}
			continue
		}
		if !strings.HasPrefix(itemID, "item_") || (itemType != "reasoning" && itemType != "item_reference") {
			filtered = append(filtered, item)
			continue
		}
		if result.FirstDroppedIndex < 0 {
			result.FirstDroppedIndex = index
		}
		if itemType == "reasoning" {
			result.DroppedReasoningItems++
		} else {
			result.DroppedItemReferences++
		}
	}

	result.RemainingItems = len(filtered)
	if !result.Modified() {
		return raw, result, nil
	}
	if result.DroppedItemReferences > 0 {
		result.HasOrphanToolOutput = codexResponsesItemsHaveOrphanToolOutput(filtered)
	}
	repaired, err := common.Marshal(filtered)
	if err != nil {
		return nil, result, fmt.Errorf("repair Codex Responses input: %w", err)
	}
	return repaired, result, nil
}

func codexResponsesItemTypeAndID(item json.RawMessage) (string, string, error) {
	trimmed := bytes.TrimSpace(item)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", "", nil
	}
	var obj map[string]json.RawMessage
	if err := common.Unmarshal(trimmed, &obj); err != nil {
		return "", "", err
	}
	return codexJSONRawString(obj["type"]), codexJSONRawString(obj["id"]), nil
}

func codexResponsesItemsHaveOrphanToolOutput(items []json.RawMessage) bool {
	callIDs := make(map[string]struct{})
	outputCallIDs := make(map[string]struct{})
	for _, item := range items {
		var envelope struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
		}
		if common.Unmarshal(item, &envelope) != nil {
			continue
		}
		callID := strings.TrimSpace(envelope.CallID)
		switch {
		case isCodexToolCallItemType(envelope.Type):
			if callID != "" {
				callIDs[callID] = struct{}{}
			}
		case isCodexToolOutputItemType(envelope.Type):
			if callID == "" {
				return true
			}
			outputCallIDs[callID] = struct{}{}
		}
	}
	for callID := range outputCallIDs {
		if _, exists := callIDs[callID]; !exists {
			return true
		}
	}
	return false
}

func isCodexToolCallItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "tool_call", "function_call", "local_shell_call", "tool_search_call", "custom_tool_call", "mcp_tool_call", "computer_call":
		return true
	default:
		return false
	}
}

func isCodexToolOutputItemType(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "function_call_output", "local_shell_call_output", "tool_search_output", "custom_tool_call_output", "mcp_tool_call_output", "computer_call_output":
		return true
	default:
		return false
	}
}

func normalizeCodexResponsesRequest(request *dto.OpenAIResponsesRequest) error {
	if request == nil {
		return nil
	}
	if err := promoteCodexSystemMessages(request); err != nil {
		return err
	}
	normalizedInput, err := normalizeCodexResponsesInput(request.Input)
	if err != nil {
		return err
	}
	request.Input = normalizedInput
	normalizedTools, err := normalizeCodexResponsesTools(request.Tools)
	if err != nil {
		return err
	}
	request.Tools = normalizedTools

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

// promoteCodexSystemMessages adapts Responses clients that still send
// role:"system" items. ChatGPT's internal Codex endpoint rejects that role,
// while accepting the same guidance through top-level instructions and a
// developer message. Only top-level input items are inspected so role-shaped
// data inside tool outputs or arbitrary user JSON is never rewritten.
func promoteCodexSystemMessages(request *dto.OpenAIResponsesRequest) error {
	if request == nil {
		return nil
	}
	trimmed := bytes.TrimSpace(request.Input)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}

	var items []json.RawMessage
	preserveObjectShape := false
	switch trimmed[0] {
	case '[':
		if err := common.Unmarshal(trimmed, &items); err != nil {
			return fmt.Errorf("promote Codex Responses system messages: %w", err)
		}
	case '{':
		items = []json.RawMessage{append(json.RawMessage(nil), trimmed...)}
		preserveObjectShape = true
	default:
		return nil
	}

	modified := false
	systemTexts := make([]string, 0)
	for index, item := range items {
		normalized, text, changed, err := promoteCodexSystemMessageItem(item)
		if err != nil {
			return fmt.Errorf("promote Codex Responses system message %d: %w", index, err)
		}
		if !changed {
			continue
		}
		items[index] = normalized
		modified = true
		if text != "" {
			systemTexts = append(systemTexts, text)
		}
	}
	if !modified {
		return nil
	}

	var encoded json.RawMessage
	var err error
	if preserveObjectShape {
		encoded = items[0]
	} else {
		encoded, err = common.Marshal(items)
		if err != nil {
			return err
		}
	}
	request.Input = encoded

	if len(systemTexts) == 0 {
		return nil
	}
	extracted := strings.Join(systemTexts, "\n\n")
	var existing string
	if len(request.Instructions) > 0 && common.Unmarshal(request.Instructions, &existing) == nil {
		existing = strings.TrimSpace(existing)
		if existing != "" {
			extracted += "\n\n" + existing
		}
	}
	instructions, err := common.Marshal(extracted)
	if err != nil {
		return err
	}
	request.Instructions = instructions
	return nil
}

func promoteCodexSystemMessageItem(item json.RawMessage) (json.RawMessage, string, bool, error) {
	trimmed := bytes.TrimSpace(item)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return item, "", false, nil
	}
	var obj map[string]json.RawMessage
	if err := common.Unmarshal(trimmed, &obj); err != nil {
		return nil, "", false, err
	}
	if codexJSONRawString(obj["role"]) != "system" {
		return item, "", false, nil
	}
	developerRole, err := common.Marshal("developer")
	if err != nil {
		return nil, "", false, err
	}
	obj["role"] = developerRole
	normalized, err := common.Marshal(obj)
	if err != nil {
		return nil, "", false, err
	}
	return normalized, extractCodexSystemMessageText(obj["content"]), true, nil
}

func extractCodexSystemMessageText(content json.RawMessage) string {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return ""
	}
	if trimmed[0] == '"' {
		var text string
		if common.Unmarshal(trimmed, &text) == nil {
			return text
		}
		return ""
	}
	if trimmed[0] == '{' {
		return extractCodexSystemTextBlock(trimmed)
	}
	if trimmed[0] != '[' {
		return ""
	}
	var blocks []json.RawMessage
	if common.Unmarshal(trimmed, &blocks) != nil {
		return ""
	}
	var builder strings.Builder
	for _, block := range blocks {
		builder.WriteString(extractCodexSystemTextBlock(block))
	}
	return builder.String()
}

func extractCodexSystemTextBlock(block json.RawMessage) string {
	var obj map[string]json.RawMessage
	if common.Unmarshal(bytes.TrimSpace(block), &obj) != nil {
		return ""
	}
	switch codexJSONRawString(obj["type"]) {
	case "text", "input_text", "output_text":
		var text string
		if common.Unmarshal(obj["text"], &text) == nil {
			return text
		}
	}
	return ""
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
	if err := normalizeCodexResponsesToolItems(items); err != nil {
		return nil, err
	}
	return common.Marshal(items)
}

const (
	codexCallIDMaxLength = 64
	codexCallIDPrefix    = "fc_"
)

func compactCodexCallID(callID string) string {
	digest := sha256.Sum256([]byte("starnexus:codex-call-id:v1:" + callID))
	encoded := hex.EncodeToString(digest[:])
	return codexCallIDPrefix + encoded[:codexCallIDMaxLength-len(codexCallIDPrefix)]
}

func normalizeCodexResponsesToolItems(items []json.RawMessage) error {
	for index, item := range items {
		trimmed := bytes.TrimSpace(item)
		if len(trimmed) == 0 || trimmed[0] != '{' {
			continue
		}
		var envelope struct {
			Type string `json:"type"`
		}
		if err := common.Unmarshal(item, &envelope); err != nil {
			return fmt.Errorf("normalize Codex Responses call_id at input item %d: %w", index, err)
		}
		if !isCodexToolCallItemType(envelope.Type) && !isCodexToolOutputItemType(envelope.Type) {
			continue
		}

		var obj map[string]json.RawMessage
		if err := common.Unmarshal(item, &obj); err != nil {
			return fmt.Errorf("normalize Codex Responses call_id at input item %d: %w", index, err)
		}
		changed := false
		if codexToolCallItemRequiresName(envelope.Type) {
			name := strings.TrimSpace(codexJSONRawString(obj["name"]))
			if name == "" {
				name = strings.TrimSpace(codexJSONRawString(obj["tool_name"]))
			}
			if name == "" {
				for _, nestedKey := range []string{"function", "custom"} {
					var nested map[string]json.RawMessage
					if common.Unmarshal(obj[nestedKey], &nested) == nil {
						name = strings.TrimSpace(codexJSONRawString(nested["name"]))
					}
					if name != "" {
						break
					}
				}
			}
			if name == "" {
				return fmt.Errorf("input[%d] %s item is missing name", index, envelope.Type)
			}
			if strings.TrimSpace(codexJSONRawString(obj["name"])) == "" {
				encodedName, err := common.Marshal(name)
				if err != nil {
					return fmt.Errorf("normalize Codex Responses tool item name at input item %d: %w", index, err)
				}
				obj["name"] = encodedName
				changed = true
			}
		}

		callID := codexJSONRawString(obj["call_id"])
		if len(callID) > codexCallIDMaxLength {
			encoded, err := common.Marshal(compactCodexCallID(callID))
			if err != nil {
				return fmt.Errorf("normalize Codex Responses call_id at input item %d: %w", index, err)
			}
			obj["call_id"] = encoded
			changed = true
		}
		if !changed {
			continue
		}
		normalized, err := common.Marshal(obj)
		if err != nil {
			return fmt.Errorf("normalize Codex Responses call_id at input item %d: %w", index, err)
		}
		items[index] = normalized
	}
	return nil
}

func codexToolCallItemRequiresName(itemType string) bool {
	switch strings.TrimSpace(itemType) {
	case "function_call", "custom_tool_call", "mcp_tool_call":
		return true
	default:
		return false
	}
}

func normalizeCodexResponsesTools(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '[' {
		return raw, nil
	}

	var tools []json.RawMessage
	if err := common.Unmarshal(trimmed, &tools); err != nil {
		return nil, fmt.Errorf("normalize Codex Responses tools: %w", err)
	}
	modified := false
	for index, tool := range tools {
		normalized, changed, err := normalizeCodexResponsesTool(tool, index)
		if err != nil {
			return nil, err
		}
		if changed {
			tools[index] = normalized
			modified = true
		}
	}
	if !modified {
		return raw, nil
	}
	return common.Marshal(tools)
}

func normalizeCodexResponsesTool(tool json.RawMessage, index int) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(tool)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return tool, false, nil
	}

	var obj map[string]json.RawMessage
	if err := common.Unmarshal(trimmed, &obj); err != nil {
		return nil, false, fmt.Errorf("normalize Codex Responses tool %d: %w", index, err)
	}
	toolType := strings.TrimSpace(codexJSONRawString(obj["type"]))
	if toolType != "function" && toolType != dto.CustomType {
		return tool, false, nil
	}

	name := strings.TrimSpace(codexJSONRawString(obj["name"]))
	nestedKey := toolType
	var nested map[string]json.RawMessage
	if nestedRaw, ok := obj[nestedKey]; ok && len(bytes.TrimSpace(nestedRaw)) > 0 {
		_ = common.Unmarshal(nestedRaw, &nested)
	}
	if name == "" && nested != nil {
		name = strings.TrimSpace(codexJSONRawString(nested["name"]))
	}
	if name == "" {
		return nil, false, fmt.Errorf("tools[%d] %s tool is missing name", index, toolType)
	}

	changed := false
	if strings.TrimSpace(codexJSONRawString(obj["name"])) == "" {
		encodedName, err := common.Marshal(name)
		if err != nil {
			return nil, false, fmt.Errorf("normalize Codex Responses tool %d name: %w", index, err)
		}
		obj["name"] = encodedName
		changed = true
	}
	if nested != nil {
		keys := []string{"description", "parameters", "strict", "format"}
		if toolType == dto.CustomType {
			keys = make([]string, 0, len(nested))
			for key := range nested {
				if key != "name" && key != "type" {
					keys = append(keys, key)
				}
			}
		}
		for _, key := range keys {
			if _, exists := obj[key]; exists {
				continue
			}
			if value, exists := nested[key]; exists {
				obj[key] = value
				changed = true
			}
		}
		delete(obj, nestedKey)
		changed = true
	}
	if !changed {
		return tool, false, nil
	}
	normalized, err := common.Marshal(obj)
	if err != nil {
		return nil, false, fmt.Errorf("normalize Codex Responses tool %d: %w", index, err)
	}
	return normalized, true, nil
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

// normalizeCodexReasoningSummaryContent repairs the known invalid shape
// produced by older Responses bridges: summary_text belongs in a reasoning
// item's summary array, not its content array. It only inspects top-level
// reasoning items and leaves reasoning_text or unknown content blocks intact.
func normalizeCodexReasoningSummaryContent(raw json.RawMessage) (json.RawMessage, codexReasoningContentNormalizationResult, error) {
	result := codexReasoningContentNormalizationResult{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return raw, result, nil
	}

	var items []json.RawMessage
	if err := common.Unmarshal(trimmed, &items); err != nil {
		return nil, result, fmt.Errorf("normalize Codex reasoning content: %w", err)
	}
	modified := false
	for index, item := range items {
		itemTrimmed := bytes.TrimSpace(item)
		if len(itemTrimmed) == 0 || itemTrimmed[0] != '{' {
			continue
		}
		var obj map[string]json.RawMessage
		if err := common.Unmarshal(itemTrimmed, &obj); err != nil {
			return nil, result, fmt.Errorf("normalize Codex reasoning content item %d: %w", index, err)
		}
		if codexJSONRawString(obj["type"]) != "reasoning" {
			continue
		}

		content, exists := obj["content"]
		if !exists {
			continue
		}
		contentTrimmed := bytes.TrimSpace(content)
		if bytes.Equal(contentTrimmed, []byte("null")) {
			delete(obj, "content")
			encoded, err := common.Marshal(obj)
			if err != nil {
				return nil, result, fmt.Errorf("normalize Codex reasoning content item %d: %w", index, err)
			}
			items[index] = encoded
			result.RemovedNullContent++
			modified = true
			continue
		}
		if len(contentTrimmed) == 0 || contentTrimmed[0] != '[' {
			continue
		}

		var contentBlocks []json.RawMessage
		if err := common.Unmarshal(contentTrimmed, &contentBlocks); err != nil {
			return nil, result, fmt.Errorf("normalize Codex reasoning content item %d blocks: %w", index, err)
		}
		var summaryBlocks []json.RawMessage
		if summary, hasSummary := obj["summary"]; hasSummary && !bytes.Equal(bytes.TrimSpace(summary), []byte("null")) {
			summaryTrimmed := bytes.TrimSpace(summary)
			if len(summaryTrimmed) == 0 || summaryTrimmed[0] != '[' {
				continue
			}
			if err := common.Unmarshal(summaryTrimmed, &summaryBlocks); err != nil {
				return nil, result, fmt.Errorf("normalize Codex reasoning summary item %d: %w", index, err)
			}
		}

		seenSummary := make(map[string]struct{}, len(summaryBlocks))
		for _, block := range summaryBlocks {
			if key, ok := codexReasoningTextBlockKey(block, "summary_text"); ok {
				seenSummary[key] = struct{}{}
			}
		}
		remaining := make([]json.RawMessage, 0, len(contentBlocks))
		moved := 0
		for _, block := range contentBlocks {
			key, isSummary := codexReasoningTextBlockKey(block, "summary_text")
			if !isSummary {
				remaining = append(remaining, block)
				continue
			}
			if _, duplicate := seenSummary[key]; !duplicate {
				summaryBlocks = append(summaryBlocks, block)
				seenSummary[key] = struct{}{}
			}
			moved++
		}
		if moved == 0 {
			continue
		}

		encodedSummary, err := common.Marshal(summaryBlocks)
		if err != nil {
			return nil, result, fmt.Errorf("normalize Codex reasoning summary item %d: %w", index, err)
		}
		obj["summary"] = encodedSummary
		if len(remaining) == 0 {
			delete(obj, "content")
		} else {
			encodedContent, err := common.Marshal(remaining)
			if err != nil {
				return nil, result, fmt.Errorf("normalize Codex reasoning content item %d: %w", index, err)
			}
			obj["content"] = encodedContent
		}
		encoded, err := common.Marshal(obj)
		if err != nil {
			return nil, result, fmt.Errorf("normalize Codex reasoning content item %d: %w", index, err)
		}
		items[index] = encoded
		result.MovedSummaryBlocks += moved
		modified = true
	}
	if !modified {
		return raw, result, nil
	}
	encoded, err := common.Marshal(items)
	return encoded, result, err
}

func codexReasoningTextBlockKey(block json.RawMessage, expectedType string) (string, bool) {
	var obj map[string]json.RawMessage
	if common.Unmarshal(bytes.TrimSpace(block), &obj) != nil || codexJSONRawString(obj["type"]) != expectedType {
		return "", false
	}
	return expectedType + "\x00" + codexJSONRawString(obj["text"]), true
}

// applyCodexReasoningContentRetry removes reasoning_text only after the
// upstream has explicitly rejected the exact content array. When the client
// did not preserve encrypted_content there is no lossless replay path, so the
// caller receives a degradation flag for audit logging.
func applyCodexReasoningContentRetry(raw json.RawMessage, index, expectedLength int) (json.RawMessage, bool, error) {
	var items []json.RawMessage
	if err := common.Unmarshal(bytes.TrimSpace(raw), &items); err != nil {
		return nil, false, fmt.Errorf("repair rejected Codex reasoning content: %w", err)
	}
	if index < 0 || index >= len(items) {
		return nil, false, fmt.Errorf("cannot safely repair rejected Codex reasoning content at input[%d]: index no longer exists", index)
	}

	var obj map[string]json.RawMessage
	if err := common.Unmarshal(bytes.TrimSpace(items[index]), &obj); err != nil {
		return nil, false, fmt.Errorf("repair rejected Codex reasoning content at input[%d]: %w", index, err)
	}
	if codexJSONRawString(obj["type"]) != "reasoning" {
		return nil, false, fmt.Errorf("cannot safely repair rejected Codex reasoning content at input[%d]: item is not reasoning", index)
	}
	var contentBlocks []json.RawMessage
	content := bytes.TrimSpace(obj["content"])
	if len(content) == 0 || content[0] != '[' || common.Unmarshal(content, &contentBlocks) != nil {
		return nil, false, fmt.Errorf("cannot safely repair rejected Codex reasoning content at input[%d]: content is not an array", index)
	}
	if len(contentBlocks) != expectedLength {
		return nil, false, fmt.Errorf("cannot safely repair rejected Codex reasoning content at input[%d]: expected length %d, got %d", index, expectedLength, len(contentBlocks))
	}
	for _, block := range contentBlocks {
		if _, ok := codexReasoningTextBlockKey(block, "reasoning_text"); !ok {
			return nil, false, fmt.Errorf("cannot safely repair rejected Codex reasoning content at input[%d]: unsupported content block", index)
		}
	}
	removedWithoutEncryptedContent := codexJSONRawString(obj["encrypted_content"]) == ""

	delete(obj, "content")
	if summary, exists := obj["summary"]; !exists || bytes.Equal(bytes.TrimSpace(summary), []byte("null")) {
		emptySummary, err := common.Marshal([]json.RawMessage{})
		if err != nil {
			return nil, false, err
		}
		obj["summary"] = emptySummary
	}
	encodedItem, err := common.Marshal(obj)
	if err != nil {
		return nil, false, fmt.Errorf("repair rejected Codex reasoning content at input[%d]: %w", index, err)
	}
	items[index] = encodedItem
	encoded, err := common.Marshal(items)
	if err != nil {
		return nil, false, fmt.Errorf("repair rejected Codex reasoning content: %w", err)
	}
	return encoded, removedWithoutEncryptedContent, nil
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
