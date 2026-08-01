package relay

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func shouldUsePassthroughRequestBody(info *relaycommon.RelayInfo) bool {
	return model_setting.GetGlobalSettings().PassThroughRequestEnabled ||
		(info != nil && info.ChannelSetting.ShouldPassThroughBody())
}

// preparePassthroughRequestBody preserves the selected account's wire format
// while still enforcing channel policy for account-owned passthrough. Explicit
// global or channel passthrough keeps its historical full-bypass semantics.
func preparePassthroughRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, io.Closer, *types.NewAPIError) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if info == nil {
		return common.ReaderOnly(storage), nil, nil
	}

	accountPassthrough := info.ChannelSetting.AccountPassThroughBodyEnabled
	accountMappedModel := strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountMappedModel))
	needsAccountModelMapping := accountMappedModel != "" &&
		(accountPassthrough || info.RelayMode == relayconstant.RelayModeResponsesCompact)
	needsResponsesLiteNormalization := info.RelayMode == relayconstant.RelayModeResponses && selectedResponsesLiteHTTP(c, info)
	if !accountPassthrough && !needsAccountModelMapping && !needsResponsesLiteNormalization {
		return common.ReaderOnly(storage), nil, nil
	}

	rawBody, err := storage.Bytes()
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	if !gjson.ValidBytes(rawBody) {
		return common.ReaderOnly(storage), nil, nil
	}

	jsonData := rawBody
	if needsAccountModelMapping {
		if modelField := gjson.GetBytes(jsonData, "model"); modelField.Exists() && modelField.String() != accountMappedModel {
			jsonData, err = sjson.SetBytes(jsonData, "model", accountMappedModel)
			if err != nil {
				return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}
		}
	}
	if accountPassthrough {
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, false)
		if err != nil {
			return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return nil, nil, newAPIErrorFromParamOverride(err)
			}
		}
	}
	if needsResponsesLiteNormalization {
		jsonData, _, err = normalizeSelectedResponsesLitePayload(c, info, jsonData, false)
		if err != nil {
			return nil, nil, types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	if bytes.Equal(jsonData, rawBody) {
		return common.ReaderOnly(storage), nil, nil
	}

	body, size, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
	if err != nil {
		return nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	info.UpstreamRequestBodySize = size
	return body, closer, nil
}
