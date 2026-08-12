package openaicompat

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatCompletionsRequestToResponsesNormalizesAllToolShapes(t *testing.T) {
	t.Parallel()
	var req dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"model":"gpt-test",
		"messages":[{"role":"user","content":"use tools"}],
		"tools":[
			{"type":"function","function":{"name":"nested","description":"nested desc","parameters":{"type":"object"},"strict":false}},
			{"type":"function","name":"flat","description":"flat desc","parameters":{"type":"object"},"strict":true},
			{"type":"custom","name":"exec","description":"run code","format":{"type":"grammar","syntax":"lark","definition":"start: WORD"}}
		]
	}`, &req))

	converted, err := ChatCompletionsRequestToResponsesRequest(&req)
	require.NoError(t, err)
	require.Equal(t, int64(3), gjson.GetBytes(converted.Tools, "#").Int())
	require.Equal(t, "nested", gjson.GetBytes(converted.Tools, "0.name").String())
	require.False(t, gjson.GetBytes(converted.Tools, "0.strict").Bool())
	require.Equal(t, "flat", gjson.GetBytes(converted.Tools, "1.name").String())
	require.True(t, gjson.GetBytes(converted.Tools, "1.strict").Bool())
	require.Equal(t, "custom", gjson.GetBytes(converted.Tools, "2.type").String())
	require.Equal(t, "exec", gjson.GetBytes(converted.Tools, "2.name").String())
	require.Equal(t, "grammar", gjson.GetBytes(converted.Tools, "2.format.type").String())
}

func TestChatCompletionsRequestToResponsesRejectsUnnamedTools(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"parameters":{"type":"object"}}}]}`,
		`{"model":"gpt-test","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"custom","format":{"type":"text"}}]}`,
	} {
		var req dto.GeneralOpenAIRequest
		require.NoError(t, common.UnmarshalJsonStr(body, &req))
		_, err := ChatCompletionsRequestToResponsesRequest(&req)
		require.Error(t, err)
		require.Contains(t, err.Error(), "tools[0]")
		require.Contains(t, err.Error(), "missing name")
	}
}

func TestResponsesRequestToChatCompletionsRequest(t *testing.T) {
	stream := true
	maxOutputTokens := uint(64)
	req := &dto.OpenAIResponsesRequest{
		Model:           "gpt-test",
		Input:           []byte(`[{"role":"user","content":[{"type":"input_text","text":"hello"},{"type":"input_image","image_url":"https://example.com/a.png"}]},{"type":"function_call","call_id":"call_1","name":"lookup","arguments":{"q":"hello"}},{"type":"function_call_output","call_id":"call_1","output":{"ok":true}}]`),
		Instructions:    []byte(`"be brief"`),
		Stream:          &stream,
		MaxOutputTokens: &maxOutputTokens,
		Tools:           []byte(`[{"type":"function","name":"lookup","description":"Lookup","parameters":{"type":"object"}}]`),
		ToolChoice:      []byte(`{"type":"function","name":"lookup"}`),
		Text:            []byte(`{"format":{"type":"json_schema","name":"answer","schema":{"type":"object"}}}`),
	}

	got, err := ResponsesRequestToChatCompletionsRequest(req)
	require.NoError(t, err)
	require.Equal(t, "gpt-test", got.Model)
	require.True(t, *got.Stream)
	require.Equal(t, maxOutputTokens, *got.MaxCompletionTokens)
	require.Len(t, got.Messages, 4)
	require.Equal(t, "system", got.Messages[0].Role)
	require.Equal(t, "be brief", got.Messages[0].Content)
	require.Equal(t, "user", got.Messages[1].Role)
	require.Len(t, got.Tools, 1)
	require.Equal(t, "lookup", got.Tools[0].Function.Name)
	require.Equal(t, "json_schema", got.ResponseFormat.Type)

	toolChoice, ok := got.ToolChoice.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "function", toolChoice["type"])
	require.Equal(t, "lookup", toolChoice["function"].(map[string]any)["name"])
	require.Equal(t, "tool", got.Messages[3].Role)
	require.Equal(t, "call_1", got.Messages[3].ToolCallId)
}

func TestResponsesRequestToChatCompletionsRejectsStatefulFields(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{
		Model:              "gpt-test",
		Input:              []byte(`"hello"`),
		PreviousResponseID: "resp_old",
	}

	_, err := ResponsesRequestToChatCompletionsRequest(req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "previous_response_id")
}

func TestChatCompletionsResponseToResponsesResponse(t *testing.T) {
	resp := &dto.OpenAITextResponse{
		Id:      "chatcmpl_1",
		Model:   "gpt-test",
		Created: float64(123),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message: dto.Message{
					Role:    "assistant",
					Content: "hello",
				},
				FinishReason: "length",
			},
		},
		Usage: dto.Usage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
		},
	}

	got, usage, err := ChatCompletionsResponseToResponsesResponse(resp, "resp_1")
	require.NoError(t, err)
	require.Equal(t, "resp_1", got.ID)
	require.Equal(t, "incomplete", responseStatusString(got))
	require.Equal(t, responsesIncompleteReasonMaxTokens, got.IncompleteDetails.Reason)
	require.Len(t, got.Output, 1)
	require.Equal(t, responsesOutputTypeMessage, got.Output[0].Type)
	require.Equal(t, "hello", got.Output[0].Content[0].Text)
	require.Equal(t, 10, usage.InputTokens)
	require.Equal(t, 5, usage.OutputTokens)
}

func TestResponsesResponseToChatCompletionsPreservesTextToolCallsAndReasoning(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		ID:        "resp_1",
		Model:     "gpt-test",
		CreatedAt: 123,
		Status:    []byte(`"completed"`),
		Output: []dto.ResponsesOutput{
			{
				Type: responsesOutputTypeMessage,
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "I will call a tool."},
				},
			},
			{
				Type:   responsesOutputTypeReasoning,
				Status: "completed",
				Content: []dto.ResponsesOutputContent{
					{Type: "summary_text", Text: "short reasoning"},
				},
			},
			{
				Type:      responsesOutputTypeFunctionCall,
				ID:        "fc_1",
				CallId:    "call_1",
				Name:      "lookup",
				Arguments: []byte(`{"q":"x"}`),
			},
		},
		Usage: &dto.Usage{InputTokens: 7, OutputTokens: 4, TotalTokens: 11},
	}

	chat, usage, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_1")

	require.NoError(t, err)
	require.Equal(t, "tool_calls", chat.Choices[0].FinishReason)
	require.Equal(t, "I will call a tool.", chat.Choices[0].Message.StringContent())
	require.Equal(t, "short reasoning", chat.Choices[0].Message.GetReasoningContent())
	toolCalls := chat.Choices[0].Message.ParseToolCalls()
	require.Len(t, toolCalls, 1)
	require.Equal(t, "call_1", toolCalls[0].ID)
	require.Equal(t, "lookup", toolCalls[0].Function.Name)
	require.Equal(t, `{"q":"x"}`, toolCalls[0].Function.Arguments)
	require.Equal(t, 11, usage.TotalTokens)
}

func TestResponsesResponseToChatCompletionsMapsIncompleteFinishReason(t *testing.T) {
	resp := &dto.OpenAIResponsesResponse{
		Model:             "gpt-test",
		CreatedAt:         123,
		Status:            []byte(`"incomplete"`),
		IncompleteDetails: &dto.IncompleteDetails{Reason: responsesIncompleteReasonContentFilter},
		Output: []dto.ResponsesOutput{
			{
				Type: responsesOutputTypeMessage,
				Role: "assistant",
				Content: []dto.ResponsesOutputContent{
					{Type: "output_text", Text: "blocked"},
				},
			},
		},
	}

	chat, _, err := ResponsesResponseToChatCompletionsResponse(resp, "chatcmpl_1")

	require.NoError(t, err)
	require.Equal(t, "content_filter", chat.Choices[0].FinishReason)
	require.Equal(t, "blocked", chat.Choices[0].Message.StringContent())
}

func TestChatCompletionsStreamChunkToResponsesEvents(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_stream", "gpt-test")
	delta := "hello"
	events, err := ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{
		Id:      "chatcmpl_stream",
		Created: 123,
		Model:   "gpt-test",
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index: 0,
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Content: &delta,
				},
			},
		},
	}, state)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, responsesEventCreated, events[0].Type)
	require.Equal(t, responsesEventOutputItemAdded, events[1].Type)
	require.Equal(t, responsesEventOutputTextDelta, events[2].Type)

	finish := "stop"
	events, err = ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index:        0,
				FinishReason: &finish,
			},
		},
		Usage: &dto.Usage{PromptTokens: 3, CompletionTokens: 2, TotalTokens: 5},
	}, state)
	require.NoError(t, err)
	require.Len(t, events, 2)

	finalEvents := FinalizeChatCompletionsStreamToResponses(state)
	require.Len(t, finalEvents, 1)
	require.Equal(t, responsesEventCompleted, finalEvents[0].Type)
	require.Equal(t, 5, finalEvents[0].Payload.Response.Usage.TotalTokens)

	body, err := common.Marshal(finalEvents[0].Payload)
	require.NoError(t, err)
	require.Contains(t, string(body), `"response.completed"`)
}
