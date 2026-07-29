package openai

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type ResponsesEventAccumulator struct {
	usage      dto.Usage
	outputText strings.Builder
	terminal   bool
	successful bool
	failed     bool
	responseID string
}

func NewResponsesEventAccumulator() *ResponsesEventAccumulator {
	return &ResponsesEventAccumulator{}
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
	case "response.completed", "response.failed", "response.incomplete", "error":
		a.terminal = true
		a.successful = event.Type == "response.completed" || event.Type == "response.incomplete"
		a.failed = event.Type == "response.failed" || event.Type == "error"
		if event.Response != nil {
			a.applyResponseUsage(event.Response)
			if event.Response.HasImageGenerationCall() {
				c.Set("image_generation_call", true)
				c.Set("image_generation_call_quality", event.Response.GetQuality())
				c.Set("image_generation_call_size", event.Response.GetSize())
			}
		}
	case "response.output_text.delta":
		a.outputText.WriteString(event.Delta)
	case dto.ResponsesOutputTypeItemDone:
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
	if usage.CompletionTokens == 0 && a.outputText.Len() > 0 && info != nil {
		usage.CompletionTokens = service.CountTextToken(a.outputText.String(), info.UpstreamModelName)
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens != 0 && info != nil {
		usage.PromptTokens = info.GetEstimatePromptTokens()
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	return &usage
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
