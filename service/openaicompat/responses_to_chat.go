package openaicompat

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	if resp == nil {
		return nil, nil, errors.New("response is nil")
	}

	text := ExtractOutputTextFromResponses(resp)
	reasoning := ExtractReasoningTextFromResponses(resp)

	usage := UsageFromResponsesUsage(resp.Usage)

	created := resp.CreatedAt

	var toolCalls []dto.ToolCallResponse
	if len(resp.Output) > 0 {
		for _, out := range resp.Output {
			if !isResponsesToolOutputType(out.Type) {
				continue
			}
			name := strings.TrimSpace(out.Name)
			if name == "" {
				continue
			}
			callId := strings.TrimSpace(out.CallId)
			if callId == "" {
				callId = strings.TrimSpace(out.ID)
			}
			toolCalls = append(toolCalls, dto.ToolCallResponse{
				ID:   callId,
				Type: "function",
				Function: dto.FunctionResponse{
					Name:      name,
					Arguments: out.ArgumentsString(),
				},
			})
		}
	}

	finishReason := "stop"
	if mappedReason, ok := ResponsesFinishReasonFromStatus(resp); ok {
		finishReason = mappedReason
	} else if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	msg := dto.Message{
		Role:    "assistant",
		Content: text,
	}
	if reasoning != "" {
		msg.ReasoningContent = &reasoning
	}
	if len(toolCalls) > 0 {
		msg.SetToolCalls(toolCalls)
	}

	out := &dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   resp.Model,
		Choices: []dto.OpenAITextResponseChoice{
			{
				Index:        0,
				Message:      msg,
				FinishReason: finishReason,
			},
		},
		Usage: *usage,
	}

	return out, usage, nil
}

func UsageFromResponsesUsage(src *dto.Usage) *dto.Usage {
	usage := &dto.Usage{}
	if src == nil {
		return usage
	}
	if src.InputTokens != 0 {
		usage.PromptTokens = src.InputTokens
		usage.InputTokens = src.InputTokens
	}
	if src.OutputTokens != 0 {
		usage.CompletionTokens = src.OutputTokens
		usage.OutputTokens = src.OutputTokens
	}
	if src.TotalTokens != 0 {
		usage.TotalTokens = src.TotalTokens
	} else {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if src.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = src.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.ImageTokens = src.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.AudioTokens = src.InputTokensDetails.AudioTokens
	}
	if src.CompletionTokenDetails.ReasoningTokens != 0 {
		usage.CompletionTokenDetails.ReasoningTokens = src.CompletionTokenDetails.ReasoningTokens
	}
	return usage
}

func ResponsesFinishReasonFromStatus(resp *dto.OpenAIResponsesResponse) (string, bool) {
	if resp == nil || responseStatusString(resp) != "incomplete" {
		return "", false
	}
	if responseIncompleteReason(resp) == responsesIncompleteReasonContentFilter {
		return "content_filter", true
	}
	return "length", true
}

func isResponsesToolOutputType(outputType string) bool {
	switch strings.TrimSpace(outputType) {
	case responsesOutputTypeFunctionCall, responsesOutputTypeCustomToolCall:
		return true
	default:
		return false
	}
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder

	// Prefer assistant message outputs.
	for _, out := range resp.Output {
		if out.Type != "message" {
			continue
		}
		if out.Role != "" && out.Role != "assistant" {
			continue
		}
		for _, c := range out.Content {
			if c.Type == "output_text" && c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	if sb.Len() > 0 {
		return sb.String()
	}
	for _, out := range resp.Output {
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}

func ExtractReasoningTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	if resp == nil || len(resp.Output) == 0 {
		return ""
	}

	var sb strings.Builder
	for _, out := range resp.Output {
		if out.Type != responsesOutputTypeReasoning {
			continue
		}
		for _, c := range out.Content {
			if c.Text != "" {
				sb.WriteString(c.Text)
			}
		}
	}
	return sb.String()
}
