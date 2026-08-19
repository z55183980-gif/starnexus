package openai

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type ResponsesEventAccumulator struct {
	usage        dto.Usage
	outputText   strings.Builder
	terminal     bool
	successful   bool
	failed       bool
	responseID   string
	failure      *types.OpenAIError
	terminalType string

	// Streamed output reconstruction for non-stream bridges. Codex often emits
	// text/tool payloads only in delta/item events while response.completed
	// carries usage with an empty output array.
	itemsByIndex     map[int]*dto.ResponsesOutput
	itemIndexByID    map[string]int
	functionArgsByID map[string]*strings.Builder
	reasoningSummary strings.Builder
	maxOutputIndex   int
}

func NewResponsesEventAccumulator() *ResponsesEventAccumulator {
	return &ResponsesEventAccumulator{
		itemsByIndex:     make(map[int]*dto.ResponsesOutput),
		itemIndexByID:    make(map[string]int),
		functionArgsByID: make(map[string]*strings.Builder),
		maxOutputIndex:   -1,
	}
}

// IsResponsesFirstOutputEvent reports whether an event starts client output
// and should be used for time-to-first-token measurement. Transport prelude,
// response lifecycle, item lifecycle, and terminal events are excluded.
func IsResponsesFirstOutputEvent(event *dto.ResponsesStreamResponse) bool {
	if event == nil {
		return false
	}
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		return false
	}
	switch eventType {
	case "response.created", "response.in_progress", dto.ResponsesOutputTypeItemAdded, dto.ResponsesOutputTypeItemDone:
		return false
	}
	if strings.Contains(eventType, ".delta") {
		return true
	}
	return strings.HasPrefix(eventType, "response.output_text") || strings.HasPrefix(eventType, "response.output")
}

// ResponsesStreamDataStartsClientOutput reports whether a Responses event
// commits the current upstream attempt. Lifecycle and empty structural events
// are kept private so a request-scoped capacity failure can still be retried
// without exposing an abandoned response ID to the client.
func ResponsesStreamDataStartsClientOutput(data string, event *dto.ResponsesStreamResponse) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || event == nil {
		return false
	}
	eventType := strings.TrimSpace(event.Type)
	switch eventType {
	case "response.created", "response.in_progress", "response.failed":
		return false
	case "error":
		failure := responsesStreamFailureFromPayload(common.StringToByteSlice(trimmed))
		if failure == nil {
			return true
		}
		return !service.IsUpstreamCapacityError(types.WithOpenAIError(*failure, http.StatusServiceUnavailable))
	case dto.ResponsesOutputTypeItemAdded, "response.content_part.added", "response.reasoning_summary_part.added":
		return responsesStreamAddedEventStartsClientOutput(common.StringToByteSlice(trimmed), eventType)
	default:
		return true
	}
}

func responsesStreamAddedEventStartsClientOutput(payload []byte, eventType string) bool {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return true
	}

	switch eventType {
	case dto.ResponsesOutputTypeItemAdded:
		item := gjson.GetBytes(payload, "item")
		if !item.Exists() || !item.IsObject() {
			return true
		}
		switch strings.TrimSpace(item.Get("type").String()) {
		case "reasoning":
			if item.Get("encrypted_content").String() != "" {
				return true
			}
			summary := item.Get("summary")
			if !summary.IsArray() {
				return false
			}
			for _, part := range summary.Array() {
				if strings.TrimSpace(part.Get("type").String()) != "summary_text" || part.Get("text").String() != "" {
					return true
				}
			}
			return false
		case "message":
			content := item.Get("content")
			if !content.IsArray() {
				return false
			}
			for _, part := range content.Array() {
				switch strings.TrimSpace(part.Get("type").String()) {
				case "output_text":
					if part.Get("text").String() != "" {
						return true
					}
				case "refusal":
					if part.Get("refusal").String() != "" {
						return true
					}
				default:
					return true
				}
			}
			return false
		case "function_call":
			return item.Get("arguments").String() != ""
		case "custom_tool_call":
			return item.Get("input").String() != ""
		case "compaction":
			return item.Get("encrypted_content").String() != ""
		default:
			return true
		}
	case "response.content_part.added":
		part := gjson.GetBytes(payload, "part")
		if !part.Exists() || !part.IsObject() {
			return true
		}
		switch strings.TrimSpace(part.Get("type").String()) {
		case "output_text":
			return part.Get("text").String() != ""
		case "refusal":
			return part.Get("refusal").String() != ""
		default:
			return true
		}
	case "response.reasoning_summary_part.added":
		part := gjson.GetBytes(payload, "part")
		if !part.Exists() || !part.IsObject() || strings.TrimSpace(part.Get("type").String()) != "summary_text" {
			return true
		}
		return part.Get("text").String() != ""
	default:
		return true
	}
}

func responsesStreamFailureFromPayload(payload []byte) *types.OpenAIError {
	if len(payload) == 0 || !gjson.ValidBytes(payload) {
		return nil
	}
	code := gjson.GetBytes(payload, "response.error.code").String()
	message := gjson.GetBytes(payload, "response.error.message").String()
	errorType := gjson.GetBytes(payload, "response.error.type").String()
	if !gjson.GetBytes(payload, "response.error").Exists() {
		code = gjson.GetBytes(payload, "error.code").String()
		message = gjson.GetBytes(payload, "error.message").String()
		errorType = gjson.GetBytes(payload, "error.type").String()
	}
	if code == "" && message == "" && errorType == "" {
		return nil
	}
	return &types.OpenAIError{Code: code, Message: message, Type: errorType}
}

// SanitizeResponsesCapacityErrorForClient converts fatal Codex capacity codes
// to the retryable server_error code after semantic output has made server-side
// replay unsafe. Messages and non-capacity error codes are preserved.
func SanitizeResponsesCapacityErrorForClient(data string) (string, bool) {
	trimmed := strings.TrimSpace(data)
	payload := common.StringToByteSlice(trimmed)
	failure := responsesStreamFailureFromPayload(payload)
	if failure == nil || !service.IsUpstreamCapacityError(types.WithOpenAIError(*failure, http.StatusServiceUnavailable)) {
		return data, false
	}

	updated := payload
	changed := false
	for _, path := range []string{"response.error.code", "error.code"} {
		parent := strings.TrimSuffix(path, ".code")
		if !gjson.GetBytes(updated, parent).Exists() {
			continue
		}
		code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(updated, path).String()))
		if code != "" && code != "server_is_overloaded" && code != "slow_down" {
			continue
		}
		next, err := sjson.SetBytes(updated, path, "server_error")
		if err != nil {
			return data, false
		}
		updated = next
		changed = true
	}
	if !changed {
		return data, false
	}
	return string(updated), true
}

// IsResponsesFirstFrameEvent reports whether an event is the first frame of a
// Responses API lifecycle. It intentionally excludes transport-only WebSocket
// events such as responsesapi.websocket_timing while allowing response.created
// to start the lower, S4-compatible first-response latency measurement.
func IsResponsesFirstFrameEvent(event *dto.ResponsesStreamResponse) bool {
	if event == nil {
		return false
	}
	eventType := strings.TrimSpace(event.Type)
	return strings.HasPrefix(eventType, "response.")
}

func (a *ResponsesEventAccumulator) Consume(c *gin.Context, info *relaycommon.RelayInfo, data []byte) (*dto.ResponsesStreamResponse, error) {
	var event dto.ResponsesStreamResponse
	if err := common.Unmarshal(data, &event); err != nil {
		return nil, err
	}
	if event.Response != nil && event.Response.ID != "" {
		a.responseID = event.Response.ID
	}

	switch event.Type {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "error":
		a.terminal = true
		a.terminalType = event.Type
		a.successful = event.Type == "response.completed" || event.Type == "response.done" || event.Type == "response.incomplete"
		a.failed = !a.successful
		if event.Response != nil {
			a.failure = event.Response.GetOpenAIError()
			a.applyResponseUsage(event.Response)
			if event.Response.HasImageGenerationCall() {
				c.Set("image_generation_call", true)
				c.Set("image_generation_call_quality", event.Response.GetQuality())
				c.Set("image_generation_call_size", event.Response.GetSize())
			}
		}
		if event.Type == "error" && a.failure == nil {
			var errorEnvelope struct {
				Error any `json:"error"`
			}
			if common.Unmarshal(data, &errorEnvelope) == nil {
				a.failure = dto.GetOpenAIError(errorEnvelope.Error)
			}
		}
	case "response.output_text.delta":
		a.outputText.WriteString(event.Delta)
		a.appendMessageTextDelta(event)
	case "response.reasoning_summary_text.delta":
		a.reasoningSummary.WriteString(event.Delta)
	case "response.function_call_arguments.delta":
		a.appendFunctionArgumentsDelta(event)
	case dto.ResponsesOutputTypeItemAdded, dto.ResponsesOutputTypeItemDone:
		a.upsertOutputItem(event)
		if event.Item != nil && event.Item.Type == dto.BuildInCallWebSearchCall && info != nil && info.ResponsesUsageInfo != nil {
			if tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; tool != nil {
				tool.CallCount++
			}
		}
	}

	return &event, nil
}

func (a *ResponsesEventAccumulator) applyResponseUsage(response *dto.OpenAIResponsesResponse) {
	if response == nil || response.Usage == nil {
		return
	}
	a.usage.PromptTokens = response.Usage.InputTokens
	a.usage.CompletionTokens = response.Usage.OutputTokens
	a.usage.TotalTokens = response.Usage.TotalTokens
	if response.Usage.InputTokensDetails != nil {
		a.usage.PromptTokensDetails.CachedTokens = response.Usage.InputTokensDetails.CachedTokens
		a.usage.PromptTokensDetails.CacheWriteTokens = response.Usage.InputTokensDetails.CacheWriteTokens
	}
}

func (a *ResponsesEventAccumulator) Usage(info *relaycommon.RelayInfo) *dto.Usage {
	usage := a.usage
	if usage.CompletionTokens == 0 && info != nil {
		if completionText := a.fallbackCompletionText(); completionText != "" {
			usage.CompletionTokens = service.CountTextToken(completionText, info.UpstreamModelName)
		}
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 && info != nil {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return &usage
}

// fallbackCompletionText collects every locally observable output category
// used by Responses clients. This keeps interrupted/tool-only streams billable
// when the upstream terminal usage frame is unavailable.
func (a *ResponsesEventAccumulator) fallbackCompletionText() string {
	var text strings.Builder
	appendPart := func(part string) {
		if part = strings.TrimSpace(part); part == "" {
			return
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(part)
	}

	appendPart(a.outputText.String())
	appendPart(a.reasoningSummary.String())

	indexes := make([]int, 0, len(a.itemsByIndex))
	for index := range a.itemsByIndex {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		item := cloneResponsesOutput(a.itemsByIndex[index])
		a.applyBufferedFunctionArgs(item)
		if item.Type != "function_call" {
			continue
		}
		appendPart(item.Name)
		appendPart(item.ArgumentsString())
	}

	return text.String()
}

func (a *ResponsesEventAccumulator) Terminal() bool {
	return a.terminal
}

func (a *ResponsesEventAccumulator) Successful() bool {
	return a.successful
}

func (a *ResponsesEventAccumulator) Failed() bool {
	return a.failed
}

func (a *ResponsesEventAccumulator) ResponseID() string {
	return a.responseID
}

func (a *ResponsesEventAccumulator) FailureError() *types.OpenAIError {
	return a.failure
}

func (a *ResponsesEventAccumulator) TerminalEventType() string {
	return a.terminalType
}

// FinalizeResponse returns the terminal Responses object, rebuilding output from
// streamed item/delta events when the completed payload has empty output.
func (a *ResponsesEventAccumulator) FinalizeResponse(response *dto.OpenAIResponsesResponse) *dto.OpenAIResponsesResponse {
	if response == nil {
		return nil
	}
	if responsesOutputHasClientContent(response.Output) {
		return response
	}
	rebuilt := a.rebuiltOutputs()
	if len(rebuilt) == 0 {
		return response
	}
	cloned := *response
	cloned.Output = rebuilt
	return &cloned
}

func (a *ResponsesEventAccumulator) upsertOutputItem(event dto.ResponsesStreamResponse) {
	if event.Item == nil {
		return
	}
	index := a.resolveOutputIndex(event)
	item := cloneResponsesOutput(event.Item)
	if existing := a.itemsByIndex[index]; existing != nil {
		item = mergeResponsesOutput(existing, item)
	}
	a.itemsByIndex[index] = item
	if item.ID != "" {
		a.itemIndexByID[item.ID] = index
	}
	if item.CallId != "" {
		a.itemIndexByID[item.CallId] = index
	}
	if args := item.ArgumentsString(); args != "" {
		a.ensureFunctionArgsBuilder(item.ID, item.CallId).Reset()
		a.ensureFunctionArgsBuilder(item.ID, item.CallId).WriteString(args)
	}
}

func (a *ResponsesEventAccumulator) appendMessageTextDelta(event dto.ResponsesStreamResponse) {
	if event.Delta == "" {
		return
	}
	index := a.resolveOutputIndex(event)
	item := a.itemsByIndex[index]
	if item == nil {
		item = &dto.ResponsesOutput{
			Type:   "message",
			ID:     strings.TrimSpace(event.ItemID),
			Status: "completed",
			Role:   "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type:        "output_text",
				Annotations: []any{},
			}},
		}
		a.itemsByIndex[index] = item
		if item.ID != "" {
			a.itemIndexByID[item.ID] = index
		}
	}
	if item.Type == "" {
		item.Type = "message"
	}
	if item.Role == "" {
		item.Role = "assistant"
	}
	if item.Status == "" {
		item.Status = "completed"
	}
	contentIndex := 0
	if event.ContentIndex != nil && *event.ContentIndex >= 0 {
		contentIndex = *event.ContentIndex
	}
	for len(item.Content) <= contentIndex {
		item.Content = append(item.Content, dto.ResponsesOutputContent{
			Type:        "output_text",
			Annotations: []any{},
		})
	}
	if item.Content[contentIndex].Type == "" {
		item.Content[contentIndex].Type = "output_text"
	}
	if item.Content[contentIndex].Annotations == nil {
		item.Content[contentIndex].Annotations = []any{}
	}
	item.Content[contentIndex].Text += event.Delta
}

func (a *ResponsesEventAccumulator) appendFunctionArgumentsDelta(event dto.ResponsesStreamResponse) {
	if event.Delta == "" {
		return
	}
	itemID := strings.TrimSpace(event.ItemID)
	builder := a.ensureFunctionArgsBuilder(itemID, "")
	builder.WriteString(event.Delta)

	index, ok := a.itemIndexByID[itemID]
	if !ok {
		index = a.resolveOutputIndex(event)
	}
	item := a.itemsByIndex[index]
	if item == nil {
		item = &dto.ResponsesOutput{
			Type:   "function_call",
			ID:     itemID,
			Status: "completed",
		}
		a.itemsByIndex[index] = item
		if itemID != "" {
			a.itemIndexByID[itemID] = index
		}
	}
	if item.Type == "" {
		item.Type = "function_call"
	}
	item.Arguments = responsesArgumentsRawMessage(builder.String())
}

func (a *ResponsesEventAccumulator) ensureFunctionArgsBuilder(itemID, callID string) *strings.Builder {
	key := strings.TrimSpace(itemID)
	if key == "" {
		key = strings.TrimSpace(callID)
	}
	if key == "" {
		key = "_default"
	}
	if a.functionArgsByID[key] == nil {
		a.functionArgsByID[key] = &strings.Builder{}
	}
	if callID = strings.TrimSpace(callID); callID != "" && callID != key {
		a.functionArgsByID[callID] = a.functionArgsByID[key]
	}
	return a.functionArgsByID[key]
}

func (a *ResponsesEventAccumulator) resolveOutputIndex(event dto.ResponsesStreamResponse) int {
	if event.OutputIndex != nil && *event.OutputIndex >= 0 {
		if *event.OutputIndex > a.maxOutputIndex {
			a.maxOutputIndex = *event.OutputIndex
		}
		return *event.OutputIndex
	}
	itemID := strings.TrimSpace(event.ItemID)
	if itemID == "" && event.Item != nil {
		itemID = strings.TrimSpace(event.Item.ID)
		if itemID == "" {
			itemID = strings.TrimSpace(event.Item.CallId)
		}
	}
	if itemID != "" {
		if index, ok := a.itemIndexByID[itemID]; ok {
			return index
		}
	}
	// Anonymous text deltas without item metadata should keep appending to the
	// current open slot instead of allocating a new output index each event.
	if event.Type == "response.output_text.delta" && a.maxOutputIndex >= 0 {
		return a.maxOutputIndex
	}
	a.maxOutputIndex++
	index := a.maxOutputIndex
	if itemID != "" {
		a.itemIndexByID[itemID] = index
	}
	return index
}

func (a *ResponsesEventAccumulator) rebuiltOutputs() []dto.ResponsesOutput {
	outputs := make([]dto.ResponsesOutput, 0, len(a.itemsByIndex)+1)
	if len(a.itemsByIndex) > 0 {
		indexes := make([]int, 0, len(a.itemsByIndex))
		for index := range a.itemsByIndex {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			item := cloneResponsesOutput(a.itemsByIndex[index])
			a.applyBufferedFunctionArgs(item)
			outputs = append(outputs, *item)
		}
	}

	if !responsesOutputHasClientContent(outputs) && a.outputText.Len() > 0 {
		outputs = append(outputs, dto.ResponsesOutput{
			Type:   "message",
			ID:     "msg_sse_rebuild",
			Status: "completed",
			Role:   "assistant",
			Content: []dto.ResponsesOutputContent{{
				Type:        "output_text",
				Text:        a.outputText.String(),
				Annotations: []any{},
			}},
		})
	}

	if a.reasoningSummary.Len() > 0 && !responsesOutputHasReasoningText(outputs) {
		summary := []dto.ResponsesReasoningSummaryPart{{
			Type: "summary_text",
			Text: a.reasoningSummary.String(),
		}}
		attached := false
		for index := range outputs {
			if outputs[index].Type == "reasoning" {
				outputs[index].Summary = summary
				attached = true
				break
			}
		}
		if !attached {
			outputs = append([]dto.ResponsesOutput{{
				Type:    "reasoning",
				ID:      "rs_sse_rebuild",
				Status:  "completed",
				Summary: summary,
			}}, outputs...)
		}
	}

	return outputs
}

func (a *ResponsesEventAccumulator) applyBufferedFunctionArgs(item *dto.ResponsesOutput) {
	if item == nil || (item.Type != "function_call" && item.Type != "custom_tool_call") {
		return
	}
	for _, key := range []string{item.ID, item.CallId} {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if builder := a.functionArgsByID[key]; builder != nil && builder.Len() > 0 {
			item.Arguments = responsesArgumentsRawMessage(builder.String())
			return
		}
	}
}

func responsesOutputHasClientContent(outputs []dto.ResponsesOutput) bool {
	for _, out := range outputs {
		switch strings.TrimSpace(out.Type) {
		case "message":
			for _, content := range out.Content {
				if strings.TrimSpace(content.Text) != "" {
					return true
				}
			}
		case "function_call", "custom_tool_call":
			if strings.TrimSpace(out.Name) != "" || strings.TrimSpace(out.ArgumentsString()) != "" {
				return true
			}
		case "reasoning":
			for _, summary := range out.Summary {
				if strings.TrimSpace(summary.Text) != "" {
					return true
				}
			}
			for _, content := range out.Content {
				if strings.TrimSpace(content.Text) != "" {
					return true
				}
			}
		case dto.ResponsesOutputTypeImageGenerationCall:
			return true
		}
	}
	return false
}

func responsesOutputHasReasoningText(outputs []dto.ResponsesOutput) bool {
	for _, out := range outputs {
		if out.Type != "reasoning" {
			continue
		}
		for _, summary := range out.Summary {
			if strings.TrimSpace(summary.Text) != "" {
				return true
			}
		}
		for _, content := range out.Content {
			if strings.TrimSpace(content.Text) != "" {
				return true
			}
		}
	}
	return false
}

func cloneResponsesOutput(item *dto.ResponsesOutput) *dto.ResponsesOutput {
	if item == nil {
		return &dto.ResponsesOutput{}
	}
	cloned := *item
	if item.Content != nil {
		cloned.Content = append([]dto.ResponsesOutputContent(nil), item.Content...)
	}
	if item.Summary != nil {
		cloned.Summary = append([]dto.ResponsesReasoningSummaryPart(nil), item.Summary...)
	}
	if item.Arguments != nil {
		cloned.Arguments = append(json.RawMessage(nil), item.Arguments...)
	}
	return &cloned
}

func mergeResponsesOutput(existing, incoming *dto.ResponsesOutput) *dto.ResponsesOutput {
	if existing == nil {
		return cloneResponsesOutput(incoming)
	}
	if incoming == nil {
		return cloneResponsesOutput(existing)
	}
	merged := cloneResponsesOutput(existing)
	if incoming.Type != "" {
		merged.Type = incoming.Type
	}
	if incoming.ID != "" {
		merged.ID = incoming.ID
	}
	if incoming.Status != "" {
		merged.Status = incoming.Status
	}
	if incoming.Role != "" {
		merged.Role = incoming.Role
	}
	if incoming.CallId != "" {
		merged.CallId = incoming.CallId
	}
	if incoming.Name != "" {
		merged.Name = incoming.Name
	}
	if incoming.Quality != "" {
		merged.Quality = incoming.Quality
	}
	if incoming.Size != "" {
		merged.Size = incoming.Size
	}
	if incoming.EncryptedContent != "" {
		merged.EncryptedContent = incoming.EncryptedContent
	}
	if len(incoming.Content) > 0 {
		if responsesOutputContentHasText(incoming.Content) || !responsesOutputContentHasText(merged.Content) {
			merged.Content = append([]dto.ResponsesOutputContent(nil), incoming.Content...)
		}
	}
	if len(incoming.Summary) > 0 {
		if responsesReasoningSummaryHasText(incoming.Summary) || !responsesReasoningSummaryHasText(merged.Summary) {
			merged.Summary = append([]dto.ResponsesReasoningSummaryPart(nil), incoming.Summary...)
		}
	}
	if args := incoming.ArgumentsString(); args != "" {
		merged.Arguments = append(json.RawMessage(nil), incoming.Arguments...)
	}
	return merged
}

func responsesOutputContentHasText(content []dto.ResponsesOutputContent) bool {
	for _, item := range content {
		if strings.TrimSpace(item.Text) != "" {
			return true
		}
	}
	return false
}

func responsesReasoningSummaryHasText(summary []dto.ResponsesReasoningSummaryPart) bool {
	for _, item := range summary {
		if strings.TrimSpace(item.Text) != "" {
			return true
		}
	}
	return false
}

func responsesArgumentsRawMessage(args string) json.RawMessage {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	trimmed := strings.TrimSpace(args)
	if json.Valid([]byte(trimmed)) && (trimmed[0] == '{' || trimmed[0] == '[') {
		return json.RawMessage(trimmed)
	}
	encoded, err := common.Marshal(args)
	if err != nil {
		return json.RawMessage(trimmed)
	}
	return encoded
}
