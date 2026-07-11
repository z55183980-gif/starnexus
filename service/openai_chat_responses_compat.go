package service

import (
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/service/openaicompat"
)

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	return openaicompat.ChatCompletionsRequestToResponsesRequest(req)
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	return openaicompat.ResponsesRequestToChatCompletionsRequest(req)
}

func ResponsesResponseToChatCompletionsResponse(resp *dto.OpenAIResponsesResponse, id string) (*dto.OpenAITextResponse, *dto.Usage, error) {
	return openaicompat.ResponsesResponseToChatCompletionsResponse(resp, id)
}

func ChatCompletionsResponseToResponsesResponse(resp *dto.OpenAITextResponse, id string) (*dto.OpenAIResponsesResponse, *dto.Usage, error) {
	return openaicompat.ChatCompletionsResponseToResponsesResponse(resp, id)
}

type ChatToResponsesStreamEvent = openaicompat.ChatToResponsesStreamEvent

type ChatToResponsesStreamState = openaicompat.ChatToResponsesStreamState

func NewChatToResponsesStreamState(id string, model string) *ChatToResponsesStreamState {
	return openaicompat.NewChatToResponsesStreamState(id, model)
}

func ChatCompletionsStreamChunkToResponsesEvents(chunk *dto.ChatCompletionsStreamResponse, state *ChatToResponsesStreamState) ([]ChatToResponsesStreamEvent, error) {
	return openaicompat.ChatCompletionsStreamChunkToResponsesEvents(chunk, state)
}

func FinalizeChatCompletionsStreamToResponses(state *ChatToResponsesStreamState) []ChatToResponsesStreamEvent {
	return openaicompat.FinalizeChatCompletionsStreamToResponses(state)
}

func UsageFromChatUsage(src *dto.Usage) *dto.Usage {
	return openaicompat.UsageFromChatUsage(src)
}

func ExtractOutputTextFromResponses(resp *dto.OpenAIResponsesResponse) string {
	return openaicompat.ExtractOutputTextFromResponses(resp)
}
