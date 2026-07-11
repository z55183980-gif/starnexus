package openaicompat

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

const (
	responsesEventCreated               = "response.created"
	responsesEventCompleted             = "response.completed"
	responsesEventIncomplete            = "response.incomplete"
	responsesEventOutputTextDelta       = "response.output_text.delta"
	responsesEventOutputItemAdded       = "response.output_item.added"
	responsesEventOutputItemDone        = "response.output_item.done"
	responsesEventFunctionArgsDelta     = "response.function_call_arguments.delta"
	responsesEventFunctionArgsDone      = "response.function_call_arguments.done"
	responsesEventReasoningSummaryDelta = "response.reasoning_summary_text.delta"
	responsesEventReasoningSummaryDone  = "response.reasoning_summary_text.done"

	responsesInputTypeFunctionCall       = "function_call"
	responsesInputTypeFunctionCallOutput = "function_call_output"
	responsesInputTypeCustomToolCall     = "custom_tool_call"

	responsesOutputTypeFunctionCall        = "function_call"
	responsesOutputTypeCustomToolCall      = "custom_tool_call"
	responsesOutputTypeMessage             = "message"
	responsesOutputTypeReasoning           = "reasoning"
	responsesIncompleteReasonContentFilter = "content_filter"
	responsesIncompleteReasonMaxTokens     = "max_output_tokens"
)

func rawJSONPresent(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	return common.GetJsonType(raw) != "null"
}

func responseStatusString(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Status) == 0 {
		return ""
	}
	if common.GetJsonType(resp.Status) == "string" {
		var status string
		if err := common.Unmarshal(resp.Status, &status); err == nil {
			return strings.TrimSpace(status)
		}
	}
	return strings.Trim(strings.TrimSpace(string(resp.Status)), `"`)
}

func responseIncompleteReason(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || resp.IncompleteDetails == nil {
		return ""
	}
	if resp.IncompleteDetails.Reason != "" {
		return strings.TrimSpace(resp.IncompleteDetails.Reason)
	}
	return strings.TrimSpace(resp.IncompleteDetails.Reasoning)
}

func responsesStatusRaw(status string) json.RawMessage {
	raw, err := common.Marshal(status)
	if err != nil {
		return json.RawMessage(`"completed"`)
	}
	return raw
}

func intPtr(v int) *int {
	return &v
}
