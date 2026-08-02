package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// collectResponsesSSE consumes an upstream Responses SSE body without writing
// any streaming frames to the downstream client. Codex always streams ordinary
// Responses requests, including when the downstream client requested JSON.
func collectResponsesSSE(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.OpenAIResponsesResponse, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid responses stream"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	accumulator := NewResponsesEventAccumulator()
	var finalResponse *dto.OpenAIResponsesResponse
	idleTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	if idleTimeout <= 0 {
		idleTimeout = 60 * time.Second
	}
	_, err := helper.ScanSSEDataWithIdleTimeout(c.Request.Context(), resp.Body, idleTimeout, func(data []byte) (bool, error) {
		event, consumeErr := accumulator.Consume(c, nil, data)
		if consumeErr != nil {
			return false, consumeErr
		}
		if event.Response != nil && accumulator.Terminal() {
			finalResponse = event.Response
			var rawEvent map[string]json.RawMessage
			if common.Unmarshal(data, &rawEvent) == nil {
				responseID, output := service.ResponsesHTTPResponseEnvelope(rawEvent["response"])
				service.StageResponsesHTTPResponseOutput(c, responseID, output)
			}
		}
		return accumulator.Terminal(), nil
	})
	if err != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("read responses stream: %w", err), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	if accumulator.Failed() {
		if upstreamErr := accumulator.FailureError(); upstreamErr != nil {
			return nil, types.WithOpenAIError(*upstreamErr, http.StatusInternalServerError)
		}
		return nil, types.NewOpenAIError(
			fmt.Errorf("responses stream ended with %s", accumulator.TerminalEventType()),
			types.ErrorCodeBadResponse,
			http.StatusInternalServerError,
		)
	}
	if !accumulator.Terminal() || finalResponse == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("responses stream ended without a terminal response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	return finalResponse, nil
}

func bufferedResponsesHTTPResponse(resp *http.Response, response *dto.OpenAIResponsesResponse) (*http.Response, error) {
	body, err := common.Marshal(response)
	if err != nil {
		return nil, err
	}
	header := make(http.Header)
	statusCode := http.StatusOK
	if resp != nil {
		header = resp.Header.Clone()
		statusCode = resp.StatusCode
	}
	header.Set("Content-Type", "application/json")
	header.Del("Content-Length")
	header.Del("Transfer-Encoding")
	return &http.Response{
		StatusCode: statusCode,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}, nil
}

// OaiResponsesSSEToNonStreamHandler converts a forced upstream Responses SSE
// response back into the JSON response requested by a non-stream client.
func OaiResponsesSSEToNonStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	response, apiErr := collectResponsesSSE(c, info, resp)
	if apiErr != nil {
		return nil, apiErr
	}
	bufferedResp, err := bufferedResponsesHTTPResponse(resp, response)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	return OaiResponsesHandler(c, info, bufferedResp)
}

// OaiResponsesSSEToChatHandler converts a forced upstream Responses SSE
// response into one non-stream Chat Completions JSON response.
func OaiResponsesSSEToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	response, apiErr := collectResponsesSSE(c, info, resp)
	if apiErr != nil {
		return nil, apiErr
	}
	bufferedResp, err := bufferedResponsesHTTPResponse(resp, response)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	return OaiResponsesToChatHandler(c, info, bufferedResp)
}
