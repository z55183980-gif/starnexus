package relay

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	appconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	openaichannel "github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func responsesViaChatCompletions(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, request *dto.OpenAIResponsesRequest) (*dto.Usage, *types.NewAPIError) {
	responsesJSON, err := common.Marshal(request)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	responsesJSON, err = relaycommon.RemoveDisabledFields(responsesJSON, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	if len(info.ParamOverride) > 0 {
		responsesJSON, err = relaycommon.ApplyParamOverrideWithRelayInfo(responsesJSON, info)
		if err != nil {
			return nil, newAPIErrorFromParamOverride(err)
		}
	}

	var overriddenResponsesReq dto.OpenAIResponsesRequest
	if err := common.Unmarshal(responsesJSON, &overriddenResponsesReq); err != nil {
		return nil, types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
	}

	chatReq, err := service.ResponsesRequestToChatCompletionsRequest(&overriddenResponsesReq)
	if err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	includeUsage := true
	if chatReq.StreamOptions != nil {
		includeUsage = chatReq.StreamOptions.IncludeUsage
	}
	if !info.SupportStreamOptions || !lo.FromPtrOr(chatReq.Stream, false) {
		chatReq.StreamOptions = nil
	} else if appconstant.ForceStreamOption {
		chatReq.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
	}
	info.ShouldIncludeUsage = includeUsage
	info.AppendRequestConversion(types.RelayFormatOpenAI)

	savedRelayMode := info.RelayMode
	savedRequestURLPath := info.RequestURLPath
	defer func() {
		info.RelayMode = savedRelayMode
		info.RequestURLPath = savedRequestURLPath
	}()

	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.RequestURLPath = "/v1/chat/completions"

	convertedRequest, err := adaptor.ConvertOpenAIRequest(c, info, chatReq)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	format, ok := relaycommon.GuessRelayFormatFromRequest(convertedRequest)
	if !ok || format != types.RelayFormatOpenAI {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("responses to chat compatibility requires an OpenAI chat upstream request, got %T", convertedRequest),
			types.ErrorCodeConvertRequestFailed,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()
	jsonData = nil
	info.UpstreamRequestBodySize = size
	var requestBody io.Reader = body

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	if resp == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")
	httpResp := resp.(*http.Response)
	info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
	if httpResp.StatusCode != http.StatusOK {
		newAPIError := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return nil, newAPIError
	}

	if info.IsStream {
		usage, newAPIError := openaichannel.OaiChatToResponsesStreamHandler(c, info, httpResp)
		if newAPIError != nil {
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return nil, newAPIError
		}
		return usage, nil
	}

	usage, newAPIError := openaichannel.OaiChatToResponsesHandler(c, info, httpResp)
	if newAPIError != nil {
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return nil, newAPIError
	}
	return usage, nil
}
