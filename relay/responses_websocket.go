package relay

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func PrepareResponsesWebSocketRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.OpenAIResponsesRequest, forceStream bool) ([]byte, channel.Adaptor, *types.NewAPIError) {
	if info == nil || request == nil {
		return nil, nil, types.NewErrorWithStatusCode(fmt.Errorf("responses websocket request is nil"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	info.InitChannelMeta(c)

	convertedRequest, err := common.DeepCopy(request)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if forceStream {
		stream := true
		convertedRequest.Stream = &stream
	}
	if err := helper.ModelMappedHelper(c, info, convertedRequest); err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return nil, nil, types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	var upstreamRequest any = convertedRequest
	if !model_setting.GetGlobalSettings().PassThroughRequestEnabled && !info.ChannelSetting.ShouldPassThroughBody() {
		upstreamRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *convertedRequest)
		if err != nil {
			return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		relaycommon.AppendRequestConversionFromRequest(info, upstreamRequest)
	}

	jsonData, err := common.Marshal(upstreamRequest)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, nil, newAPIErrorFromParamOverride(err)
		}
	}
	if forceStream {
		jsonData, err = forceResponsesRequestStream(jsonData)
		if err != nil {
			return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
	}
	logger.LogDebug(c, "responses websocket client request prepared: %d bytes", len(jsonData))
	return jsonData, adaptor, nil
}

func forceResponsesRequestStream(jsonData []byte) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(jsonData, &payload); err != nil {
		return nil, err
	}
	stream, err := common.Marshal(true)
	if err != nil {
		return nil, err
	}
	payload["stream"] = stream
	return common.Marshal(payload)
}
