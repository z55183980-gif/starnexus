package openai

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	// read response body
	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	contentType := ""
	if resp.Header != nil {
		contentType = resp.Header.Get("Content-Type")
	}
	if common.LooksLikeSSEBody(responseBody) {
		logger.LogWarn(c, fmt.Sprintf(
			"responses handler got SSE body with content_type=%q; bridging to SSE collector",
			contentType,
		))
		bridged := *resp
		bridged.Body = io.NopCloser(bytes.NewReader(responseBody))
		if bridged.Header == nil {
			bridged.Header = make(http.Header)
		} else {
			bridged.Header = resp.Header.Clone()
		}
		if !common.IsEventStreamContentType(contentType) {
			bridged.Header.Set("Content-Type", "text/event-stream")
		}
		return OaiResponsesSSEToNonStreamHandler(c, info, &bridged)
	}
	err = common.Unmarshal(responseBody, &responsesResponse)
	if err != nil {
		logger.LogError(c, fmt.Sprintf(
			"bad responses JSON body: content_type=%q body_prefix=%q err=%v",
			contentType,
			common.PreviewUpstreamBody(responseBody, 256),
			err,
		))
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	if responsesResponse.HasImageGenerationCall() {
		c.Set("image_generation_call", true)
		c.Set("image_generation_call_quality", responsesResponse.GetQuality())
		c.Set("image_generation_call_size", responsesResponse.GetSize())
	}

	// Commit continuation state only after the response body was successfully
	// written downstream. This prevents hidden tool calls from poisoning the
	// next turn when the client has already disconnected.
	writeErr := service.IOCopyBytesGracefully(c, resp, responseBody)
	if writeErr == nil {
		responseID, output := service.ResponsesHTTPResponseEnvelope(responseBody)
		output = service.PreferStagedResponsesHTTPOutput(c, responseID, output)
		service.CommitResponsesHTTPContinuation(c, responseID, output)
	}

	// compute usage
	usage := dto.Usage{}
	if responsesResponse.Usage != nil {
		usage.PromptTokens = responsesResponse.Usage.InputTokens
		usage.CompletionTokens = responsesResponse.Usage.OutputTokens
		usage.TotalTokens = responsesResponse.Usage.TotalTokens
		if responsesResponse.Usage.InputTokensDetails != nil {
			usage.PromptTokensDetails.CachedTokens = responsesResponse.Usage.InputTokensDetails.CachedTokens
			usage.PromptTokensDetails.CacheWriteTokens = responsesResponse.Usage.InputTokensDetails.CacheWriteTokens
		}
	}
	service.ReconcileCodexResponsesUsage(c, info, &usage)
	if info == nil || info.ResponsesUsageInfo == nil || info.ResponsesUsageInfo.BuiltInTools == nil {
		return &usage, nil
	}
	// 解析 Tools 用量
	for _, tool := range responsesResponse.Tools {
		buildToolinfo, ok := info.ResponsesUsageInfo.BuiltInTools[common.Interface2String(tool["type"])]
		if !ok || buildToolinfo == nil {
			logger.LogError(c, fmt.Sprintf("BuiltInTools not found for tool type: %v", tool["type"]))
			continue
		}
		buildToolinfo.CallCount++
	}
	return &usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}

	defer service.CloseResponseBodyGracefully(resp)

	accumulator := NewResponsesEventAccumulator()
	stageCapacityPrelude := common.GetContextKeyString(c, appconstant.ContextKeyChannelCredentialSource) == appconstant.ChannelCredentialSourceAccountPool
	type stagedResponsesEvent struct {
		response dto.ResponsesStreamResponse
		data     string
	}
	prelude := make([]stagedResponsesEvent, 0, 8)
	clientOutputStarted := false
	var capacityErr *types.NewAPIError
	deliver := func(streamResponse dto.ResponsesStreamResponse, data string) error {
		if err := sendResponsesStreamData(c, streamResponse, data); err != nil {
			return err
		}
		info.SendResponseCount++
		info.SetFirstResponseTime()
		service.RecordDeliveredResponsesHTTPEvent(c, []byte(data))
		return nil
	}
	flushPrelude := func() error {
		for _, staged := range prelude {
			if err := deliver(staged.response, staged.data); err != nil {
				return err
			}
		}
		prelude = prelude[:0]
		return nil
	}

	// HTTP/SSE keeps the S4 reporting convention: lifecycle events such as
	// response.created starts FRT for HTTP/SSE and WebSocket paths.
	helper.StreamScannerHandlerWithOptions(c, resp, info, helper.StreamScannerOptions{DisableAutoFirstResponseTime: true}, func(data string, sr *helper.StreamResult) {

		streamResponse, err := accumulator.Consume(c, info, common.StringToByteSlice(data))
		if err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		if stageCapacityPrelude && !clientOutputStarted && !accumulator.UsageReported() &&
			accumulator.Terminal() && !accumulator.Successful() {
			if failure := accumulator.FailureError(); failure != nil {
				candidate := types.WithOpenAIError(*failure, http.StatusServiceUnavailable)
				if service.IsUpstreamCapacityError(candidate) {
					candidate.StatusCode = 529
					capacityErr = candidate
					prelude = prelude[:0]
					sr.Stop(candidate)
					return
				}
			}
		}
		if stageCapacityPrelude && !clientOutputStarted && !ResponsesStreamDataStartsClientOutput(data, streamResponse) {
			prelude = append(prelude, stagedResponsesEvent{response: *streamResponse, data: data})
			return
		}
		if stageCapacityPrelude && clientOutputStarted && accumulator.Terminal() && !accumulator.Successful() {
			if sanitized, changed := SanitizeResponsesCapacityErrorForClient(data); changed {
				data = sanitized
			}
		}
		if writeErr := flushPrelude(); writeErr != nil {
			sr.Stop(writeErr)
			return
		}
		if writeErr := deliver(*streamResponse, data); writeErr != nil {
			sr.Stop(writeErr)
			return
		}
		clientOutputStarted = true
	})
	if capacityErr != nil {
		return nil, capacityErr
	}
	if writeErr := flushPrelude(); writeErr != nil {
		return nil, types.NewErrorWithStatusCode(writeErr, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	service.FinalizeResponsesHTTPContinuationStream(c)

	usage := accumulator.Usage(info)
	service.ReconcileCodexResponsesUsage(c, info, usage)

	return usage, nil
}
