package controller

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	taskdoubao "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func relayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	switch info.RelayMode {
	case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
		err = relay.ImageHelper(c, info)
	case relayconstant.RelayModeAudioSpeech:
		fallthrough
	case relayconstant.RelayModeAudioTranslation:
		fallthrough
	case relayconstant.RelayModeAudioTranscription:
		err = relay.AudioHelper(c, info)
	case relayconstant.RelayModeRerank:
		err = relay.RerankHelper(c, info)
	case relayconstant.RelayModeEmbeddings:
		err = relay.EmbeddingHelper(c, info)
	case relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact:
		err = relay.ResponsesHelper(c, info)
	case relayconstant.RelayModeAlphaSearch:
		err = relay.AlphaSearchHelper(c, info)
	default:
		err = relay.TextHelper(c, info)
	}
	return err
}

func geminiRelayHandler(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	var err *types.NewAPIError
	if strings.Contains(c.Request.URL.Path, "embed") {
		err = relay.GeminiEmbeddingHandler(c, info)
	} else {
		err = relay.GeminiHelper(c, info)
	}
	return err
}

func Relay(c *gin.Context, relayFormat types.RelayFormat) {

	requestId := c.GetString(common.RequestIdKey)
	//group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	//originalModel := common.GetContextKeyString(c, constant.ContextKeyOriginalModel)

	var (
		newAPIError *types.NewAPIError
		ws          *websocket.Conn
	)

	if relayFormat == types.RelayFormatOpenAIRealtime {
		var err error
		ws, err = upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			helper.WssError(c, ws, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry()).ToOpenAIError())
			return
		}
		defer ws.Close()
	}

	defer func() {
		if newAPIError != nil {
			logger.LogError(c, fmt.Sprintf("relay error: %s", newAPIError.Error()))
			newAPIError.SetMessage(common.MessageWithRequestId(newAPIError.Error(), requestId))
			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				helper.WssError(c, ws, newAPIError.ToOpenAIError())
			case types.RelayFormatClaude:
				c.JSON(newAPIError.StatusCode, gin.H{
					"type":  "error",
					"error": newAPIError.ToClaudeError(),
				})
			default:
				c.JSON(newAPIError.StatusCode, gin.H{
					"error": newAPIError.ToOpenAIError(),
				})
			}
		}
	}()

	request, err := helper.GetAndValidateRequest(c, relayFormat)
	if err != nil {
		// Map "request body too large" to 413 so clients can handle it correctly
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			newAPIError = types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
		} else {
			newAPIError = types.NewError(err, types.ErrorCodeInvalidRequest)
		}
		return
	}
	if tryInterceptUpstreamWarmup(c, relayFormat, request) {
		return
	}

	// Restore a lossless, gateway-owned Responses continuation before token
	// estimation. Keep the client request unchanged so upstreams that support
	// previous_response_id can continue to consume it natively; Codex HTTP opts
	// into the expanded copy in its adaptor.
	var responsesContinuationMeta *types.TokenCountMeta
	if relayFormat == types.RelayFormatOpenAIResponses {
		if responsesRequest, ok := request.(*dto.OpenAIResponsesRequest); ok {
			service.PrepareResponsesHTTPContinuation(c, responsesRequest)
			if preferredAccountID := service.ResponsesHTTPContinuationPreferredAccountID(c); preferredAccountID > 0 {
				common.SetContextKey(c, constant.ContextKeyUpstreamAccountPreferredId, preferredAccountID)
				common.SetContextKey(c, constant.ContextKeyUpstreamAccountPreferredRequired, false)
			}
			if constant.CountToken {
				if expandedMeta, expanded := service.ResponsesHTTPContinuationTokenCountMeta(c, responsesRequest); expanded {
					responsesContinuationMeta = expandedMeta
				}
			}
		}
	}

	relayInfo, err := relaycommon.GenRelayInfo(c, relayFormat, request, ws)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeGenRelayInfoFailed)
		return
	}

	needSensitiveCheck := setting.ShouldCheckPromptSensitive()
	needCountToken := constant.CountToken
	// Avoid building huge CombineText (strings.Join) when token counting and sensitive check are both disabled.
	var meta *types.TokenCountMeta
	if needSensitiveCheck || needCountToken {
		meta = request.GetTokenCountMeta()
	} else {
		meta = fastTokenCountMetaForPricing(request)
	}

	if needSensitiveCheck {
		words, sensitiveError := checkGlobalPromptSensitive(meta)
		if sensitiveError != nil {
			logger.LogWarn(c, fmt.Sprintf("user sensitive words detected: %s", strings.Join(words, ", ")))
			newAPIError = sensitiveError
			return
		}
	}

	pricingMeta := meta
	if responsesContinuationMeta != nil {
		pricingMeta = responsesContinuationMeta
	}
	tokens, err := service.EstimateRequestToken(c, pricingMeta, relayInfo)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeCountTokenFailed)
		return
	}
	// Context auto compaction is parked for now. Keep the implementation here so
	// it can be re-enabled by uncommenting this block when needed.
	/*
		if relayFormat == types.RelayFormatClaude {
			compactedMeta, compactedTokens, compactResult, compactErr := relay.MaybeCompactClaudeRequest(c, relayInfo, tokens)
			if compactErr != nil {
				newAPIError = compactErr
				return
			}
			if compactResult != nil && compactResult.Applied {
				meta = compactedMeta
				tokens = compactedTokens
				c.Set("context_compaction_result", compactResult)
				c.Set("context_compaction_admin_info", compactResult.AdminInfo())
			}
		}
	*/

	relayInfo.SetEstimatePromptTokens(tokens)

	priceData, err := helper.ModelPriceHelper(c, relayInfo, tokens, pricingMeta)
	if err != nil {
		newAPIError = types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest))
		return
	}
	if relayFormat == types.RelayFormatOpenAIAlphaSearch {
		priceData.QuotaToPreConsume = service.AddKnownToolCallSurchargeToPreConsumeQuota(c, relayInfo, priceData.QuotaToPreConsume)
	}

	// common.SetContextKey(c, constant.ContextKeyTokenCountMeta, meta)

	if priceData.FreeModel && priceData.QuotaToPreConsume == 0 {
		logger.LogInfo(c, fmt.Sprintf("模型 %s 免费，跳过预扣费", relayInfo.OriginModelName))
	} else {
		newAPIError = service.PreConsumeBilling(c, priceData.QuotaToPreConsume, relayInfo)
		if newAPIError != nil {
			return
		}
	}

	defer func() {
		// Only return quota if downstream failed and quota was actually pre-consumed
		if newAPIError != nil {
			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			if relayInfo.Billing != nil {
				relayInfo.Billing.Refund(c)
			}
			service.ChargeViolationFeeIfNeeded(c, relayInfo, newAPIError)
		}
	}()

	// Paid requests must pass the definitive wallet/subscription reservation
	// before invoking either audit path. Requests that billing will reject must
	// not consume moderation capacity or create security-audit records. Free
	// requests still reach the audits because they intentionally skip billing.
	if moderationError := service.ApplyContentModeration(c, request, relayFormat, relayInfo.OriginModelName); moderationError != nil {
		newAPIError = moderationError
		return
	}

	if promptAuditError := service.ApplyPromptAudit(c, request, relayFormat, relayInfo.OriginModelName); promptAuditError != nil {
		newAPIError = promptAuditError
		return
	}

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}
	relayInfo.RetryIndex = 0
	relayInfo.LastError = nil

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		relayInfo.RetryIndex = retryParam.GetRetry()
		channel, channelErr := getChannel(c, relayInfo, retryParam)
		if channelErr != nil {
			logger.LogError(c, channelErr.Error())
			newAPIError = channelErr
			if shouldRetry(c, channelErr, common.RetryTimes-retryParam.GetRetry()) {
				// Mark the initial channel attempt as consumed so the next iteration
				// enters the normal channel selector instead of retrying the same
				// unavailable local account pool from middleware context.
				service.ClearResponsesHTTPContinuationPersistTarget(c)
				relayInfo.InitChannelMeta(c)
				continue
			}
			break
		}

		addUsedChannel(c, channel.Id)
		accountFailoverStartedAt := time.Now()
		accountFailovers := 0
		capacityFailoverExhausted := false
		for {
			bodyStorage, bodyErr := common.GetBodyStorage(c)
			if bodyErr != nil {
				// Ensure consistent 413 for oversized bodies even when error occurs later (e.g., retry path)
				if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusRequestEntityTooLarge, types.ErrOptionWithSkipRetry())
				} else {
					newAPIError = types.NewErrorWithStatusCode(bodyErr, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
				}
				break
			}
			c.Request.Body = io.NopCloser(bodyStorage)

			switch relayFormat {
			case types.RelayFormatOpenAIRealtime:
				newAPIError = relay.WssHelper(c, relayInfo)
			case types.RelayFormatClaude:
				newAPIError = relay.ClaudeHelper(c, relayInfo)
			case types.RelayFormatGemini:
				newAPIError = geminiRelayHandler(c, relayInfo)
			default:
				newAPIError = relayHandler(c, relayInfo)
			}

			if newAPIError == nil {
				relayInfo.LastError = nil
				service.RecordUpstreamAccountSuccessForModel(
					common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId),
					relayInfo.UpstreamModelName,
				)
				recordUpstreamRequestEvent(c, "request_success", "success", "")
				return
			}

			newAPIError = service.NormalizeViolationFeeError(newAPIError)
			relayInfo.LastError = newAPIError
			if relay.TryRepairCodexInvalidMessageIDForRetry(c, relayInfo, newAPIError) {
				logger.LogWarn(c, "retrying Codex Responses once after removing an upstream-rejected message id")
				recordUpstreamRequestEvent(c, "request_retry", "retry", "removed upstream-rejected Codex message id")
				service.ClearResponsesHTTPContinuationPersistTarget(c)
				relayInfo.LastError = nil
				continue
			}
			if relay.TryArmCodexReasoningContentRetry(c, relayInfo, newAPIError) {
				logger.LogWarn(c, "retrying Codex Responses once after an upstream-rejected reasoning content array")
				recordUpstreamRequestEvent(c, "request_retry", "retry", "removed upstream-rejected Codex reasoning content")
				service.ClearResponsesHTTPContinuationPersistTarget(c)
				relayInfo.LastError = nil
				continue
			}
			accountId := common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId)
			proxyId := common.GetContextKeyInt(c, constant.ContextKeyUpstreamProxyId)
			disposition := service.ApplyUpstreamAccountError(accountId, proxyId, newAPIError)
			if disposition.Handled() {
				recordUpstreamRequestEvent(c, "request_error", "error", service.UpstreamAccountErrorSummary(newAPIError))
				if service.IsUpstreamCapacityError(newAPIError) {
					// 529/overload is account/model scoped. Record a model-scoped
					// transient failure for the failed account, then fail over immediately
					// to another account serving the same model. Do not replay the
					// overloaded account.
					service.RecordUpstreamAccountModelTransientFailure(accountId, relayInfo.UpstreamModelName)
					capacityFailoverExhausted = true
				}
				if disposition.RetryWithinPool() && relayInfo.SendResponseCount == 0 &&
					service.ShouldRetryUpstreamAccount(accountFailovers, accountFailoverStartedAt) {
					capacityError := service.IsUpstreamCapacityError(newAPIError)
					excludedIds, _ := common.GetContextKeyType[map[int]struct{}](c, constant.ContextKeyUpstreamAccountExcluded)
					if excludedIds == nil {
						excludedIds = make(map[int]struct{})
					}
					excludedIds[accountId] = struct{}{}
					common.SetContextKey(c, constant.ContextKeyUpstreamAccountExcluded, excludedIds)
					if capacityError {
						recordUpstreamRequestEvent(c, "request_retry", "retry", fmt.Sprintf("model capacity failover from account #%d", accountId))
					}
					if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr == nil {
						accountFailovers++
						capacityFailoverExhausted = false
						service.ClearResponsesHTTPContinuationPersistTarget(c)
						relayInfo.InitChannelMeta(c)
						continue
					} else {
						if !capacityError {
							newAPIError = setupErr
							relayInfo.LastError = setupErr
						}
					}
				}
				break
			}

			processChannelError(c, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)
			break
		}

		if capacityFailoverExhausted {
			// The account-pool failover has been exhausted. Do not let the outer
			// channel retry remap this request to another model or emit a false
			// "no available channel" error.
			break
		}
		if !shouldRetry(c, newAPIError, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
		service.ClearResponsesHTTPContinuationPersistTarget(c)
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}
	if newAPIError != nil {
		gopool.Go(func() {
			perfmetrics.RecordRelaySample(relayInfo, false, 0)
		})
	}
}

func checkGlobalPromptSensitive(meta *types.TokenCountMeta) ([]string, *types.NewAPIError) {
	if meta == nil {
		return nil, nil
	}
	contains, words := service.CheckSensitiveText(meta.CombineText)
	if !contains {
		return nil, nil
	}
	return words, types.NewErrorWithStatusCode(
		fmt.Errorf("sensitive words detected: %s", strings.Join(words, ", ")),
		types.ErrorCodeSensitiveWordsDetected,
		http.StatusBadRequest,
		types.ErrOptionWithSkipRetry(),
	)
}

var upgrader = websocket.Upgrader{
	Subprotocols: []string{"realtime"}, // WS 握手支持的协议，如果有使用 Sec-WebSocket-Protocol，则必须在此声明对应的 Protocol TODO add other protocol
	CheckOrigin: func(r *http.Request) bool {
		return true // 允许跨域
	},
}

func addUsedChannel(c *gin.Context, channelId int) {
	useChannel := c.GetStringSlice("use_channel")
	useChannel = append(useChannel, fmt.Sprintf("%d", channelId))
	c.Set("use_channel", useChannel)
}

func fastTokenCountMetaForPricing(request dto.Request) *types.TokenCountMeta {
	if request == nil {
		return &types.TokenCountMeta{}
	}
	meta := &types.TokenCountMeta{
		TokenType: types.TokenTypeTokenizer,
	}
	switch r := request.(type) {
	case *dto.GeneralOpenAIRequest:
		maxCompletionTokens := lo.FromPtrOr(r.MaxCompletionTokens, uint(0))
		maxTokens := lo.FromPtrOr(r.MaxTokens, uint(0))
		if maxCompletionTokens > maxTokens {
			meta.MaxTokens = int(maxCompletionTokens)
		} else {
			meta.MaxTokens = int(maxTokens)
		}
	case *dto.OpenAIResponsesRequest:
		meta.MaxTokens = int(lo.FromPtrOr(r.MaxOutputTokens, uint(0)))
	case *dto.ClaudeRequest:
		meta.MaxTokens = int(lo.FromPtr(r.MaxTokens))
	case *dto.ImageRequest:
		// Pricing for image requests depends on ImagePriceRatio; safe to compute even when CountToken is disabled.
		return r.GetTokenCountMeta()
	default:
		// Best-effort: leave CombineText empty to avoid large allocations.
	}
	return meta
}

func getChannel(c *gin.Context, info *relaycommon.RelayInfo, retryParam *service.RetryParam) (*model.Channel, *types.NewAPIError) {
	if info.ChannelMeta == nil {
		if common.GetContextKeyString(c, constant.ContextKeyChannelCredentialSource) == constant.ChannelCredentialSourceAccountPool {
			channelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
			channel, err := model.CacheGetChannel(channelId)
			if err != nil || channel == nil {
				channel, err = model.GetChannelById(channelId, true)
			}
			if err != nil || channel == nil {
				return nil, types.NewError(errors.New("local account pool channel is unavailable"), types.ErrorCodeGetChannelFailed)
			}
			if apiErr := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName); apiErr != nil {
				return nil, apiErr
			}
			return channel, nil
		}
		autoBan := c.GetBool("auto_ban")
		autoBanInt := 1
		if !autoBan {
			autoBanInt = 0
		}
		return &model.Channel{
			Id:      c.GetInt("channel_id"),
			Type:    c.GetInt("channel_type"),
			Name:    c.GetString("channel_name"),
			AutoBan: &autoBanInt,
		}, nil
	}
	channel, selectGroup, err := service.CacheGetRandomSatisfiedChannelFiltered(retryParam, service.ChannelFilterForRequestPath(c.Request.URL.Path, info.OriginModelName))

	info.PriceData.GroupRatioInfo = helper.HandleGroupRatio(c, info)

	if err != nil {
		return nil, types.NewError(fmt.Errorf("获取分组 %s 下模型 %s 的可用渠道失败（retry）: %s", selectGroup, info.OriginModelName, err.Error()), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if channel == nil {
		return nil, types.NewError(fmt.Errorf("分组 %s 下模型 %s 的可用渠道不存在（retry）", selectGroup, info.OriginModelName), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, info.OriginModelName)
	if newAPIError != nil {
		return nil, newAPIError
	}
	return channel, nil
}

func shouldRetry(c *gin.Context, openaiErr *types.NewAPIError, retryTimes int) bool {
	if openaiErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if types.IsChannelError(openaiErr) {
		return true
	}
	if types.IsSkipRetryError(openaiErr) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	// Capacity errors are already handled by the account-pool failover loop.
	// Retrying them in the outer channel loop can remap the request to a
	// different model and produce a misleading "no available channel" error.
	if service.IsUpstreamCapacityError(openaiErr) {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	code := openaiErr.StatusCode
	if code >= 200 && code < 300 {
		return false
	}
	if code < 100 || code > 599 {
		return true
	}
	if operation_setting.IsAlwaysSkipRetryCode(openaiErr.GetErrorCode()) {
		return false
	}
	return operation_setting.ShouldRetryByStatusCode(code)
}

func processChannelError(c *gin.Context, channelError types.ChannelError, err *types.NewAPIError) {
	accountId := common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId)
	proxyId := common.GetContextKeyInt(c, constant.ContextKeyUpstreamProxyId)
	if service.ApplyUpstreamAccountError(accountId, proxyId, err).Handled() {
		recordUpstreamRequestEvent(c, "request_error", "error", service.UpstreamAccountErrorSummary(err))
		logger.LogError(c, fmt.Sprintf("upstream account error (account #%d, channel #%d, status code: %d): %s", accountId, channelError.ChannelId, err.StatusCode, service.UpstreamAccountErrorSummary(err)))
		return
	}
	logger.LogError(c, fmt.Sprintf("channel error (channel #%d, status code: %d): %s", channelError.ChannelId, err.StatusCode, err.Error()))
	// 不要使用context获取渠道信息，异步处理时可能会出现渠道信息不一致的情况
	// do not use context to get channel info, there may be inconsistent channel info when processing asynchronously
	if service.ShouldDisableChannel(err) && channelError.AutoBan {
		gopool.Go(func() {
			service.DisableChannel(channelError, err.ErrorWithStatusCode())
		})
	}

	recordRelayErrorLog(c, err)
}

func recordRelayErrorLog(c *gin.Context, err *types.NewAPIError) {
	if c == nil || err == nil {
		return
	}
	if constant.ErrorLogEnabled && types.IsRecordErrorLog(err) {
		// 保存错误日志到mysql中
		userId := c.GetInt("id")
		tokenName := c.GetString("token_name")
		modelName := c.GetString("original_model")
		tokenId := c.GetInt("token_id")
		userGroup := c.GetString("group")
		channelId := c.GetInt("channel_id")
		other := make(map[string]interface{})
		if c.Request != nil && c.Request.URL != nil {
			other["request_path"] = c.Request.URL.Path
		}
		other["error_type"] = err.GetErrorType()
		other["error_code"] = err.GetErrorCode()
		other["status_code"] = err.StatusCode
		other["channel_id"] = channelId
		other["channel_name"] = c.GetString("channel_name")
		other["channel_type"] = c.GetInt("channel_type")
		adminInfo := make(map[string]interface{})
		adminInfo["use_channel"] = c.GetStringSlice("use_channel")
		isMultiKey := common.GetContextKeyBool(c, constant.ContextKeyChannelIsMultiKey)
		if isMultiKey {
			adminInfo["is_multi_key"] = true
			adminInfo["multi_key_index"] = common.GetContextKeyInt(c, constant.ContextKeyChannelMultiKeyIndex)
		}
		service.AppendChannelAffinityAdminInfo(c, adminInfo)
		service.AppendCodexInputRepairAdminInfo(c, adminInfo)
		service.AppendCodexStructuredOutputCompatAdminInfo(c, adminInfo)
		other["admin_info"] = adminInfo
		startTime := common.GetContextKeyTime(c, constant.ContextKeyRequestStartTime)
		if startTime.IsZero() {
			startTime = time.Now()
		}
		elapsed := time.Since(startTime)
		if elapsed < 0 {
			elapsed = 0
		}
		useTimeSeconds := int(elapsed / time.Second)
		model.RecordErrorLog(c, userId, channelId, modelName, tokenName, err.MaskSensitiveErrorWithStatusCode(), tokenId, useTimeSeconds, elapsed.Milliseconds(), common.GetContextKeyBool(c, constant.ContextKeyIsStream), userGroup, other)
	}
}

func recordUpstreamRequestEvent(c *gin.Context, eventType string, result string, message string) {
	recordUpstreamRequestEventWithMetadata(c, eventType, result, message, nil)
}

func recordUpstreamRequestEventWithMetadata(c *gin.Context, eventType string, result string, message string, metadata map[string]any) {
	if c == nil {
		return
	}
	accountId := common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId)
	if accountId <= 0 {
		return
	}
	service.RecordUpstreamAccountEvent(service.UpstreamAccountEventInput{
		AccountId: accountId,
		PoolId:    common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountPoolId),
		ProxyId:   common.GetContextKeyInt(c, constant.ContextKeyUpstreamProxyId),
		ChannelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		RequestId: c.GetString(common.RequestIdKey),
		LeaseId:   common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountLeaseId),
		EventType: eventType,
		Result:    result,
		Message:   message,
		Metadata:  metadata,
	})
}

func RelayMidjourney(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatMjProxy, nil, nil)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"description": fmt.Sprintf("failed to generate relay info: %s", err.Error()),
			"type":        "upstream_error",
			"code":        4,
		})
		return
	}

	var mjErr *dto.MidjourneyResponse
	switch relayInfo.RelayMode {
	case relayconstant.RelayModeMidjourneyNotify:
		mjErr = relay.RelayMidjourneyNotify(c)
	case relayconstant.RelayModeMidjourneyTaskFetch, relayconstant.RelayModeMidjourneyTaskFetchByCondition:
		mjErr = relay.RelayMidjourneyTask(c, relayInfo.RelayMode)
	case relayconstant.RelayModeMidjourneyTaskImageSeed:
		mjErr = relay.RelayMidjourneyTaskImageSeed(c)
	case relayconstant.RelayModeSwapFace:
		mjErr = relay.RelaySwapFace(c, relayInfo)
	default:
		mjErr = relay.RelayMidjourneySubmit(c, relayInfo)
	}
	//err = relayMidjourneySubmit(c, relayMode)
	log.Println(mjErr)
	if mjErr != nil {
		statusCode := http.StatusBadRequest
		if mjErr.Code == 30 {
			mjErr.Result = "当前分组负载已饱和，请稍后再试，或升级账户以提升服务质量。"
			statusCode = http.StatusTooManyRequests
		}
		c.JSON(statusCode, gin.H{
			"description": fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result),
			"type":        "upstream_error",
			"code":        mjErr.Code,
		})
		channelId := c.GetInt("channel_id")
		logger.LogError(c, fmt.Sprintf("relay error (channel #%d, status code %d): %s", channelId, statusCode, fmt.Sprintf("%s %s", mjErr.Description, mjErr.Result)))
	}
}

func RelayNotImplemented(c *gin.Context) {
	err := types.OpenAIError{
		Message: "API not implemented",
		Type:    "new_api_error",
		Param:   "",
		Code:    "api_not_implemented",
	}
	c.JSON(http.StatusNotImplemented, gin.H{
		"error": err,
	})
}

func RelayNotFound(c *gin.Context) {
	err := types.OpenAIError{
		Message: fmt.Sprintf("Invalid URL (%s %s)", c.Request.Method, c.Request.URL.Path),
		Type:    "invalid_request_error",
		Param:   "",
		Code:    "",
	}
	c.JSON(http.StatusNotFound, gin.H{
		"error": err,
	})
}

func RelayTaskFetch(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskError(c, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}
	if taskErr := relay.RelayTaskFetch(c, relayInfo.RelayMode); taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

func RelayTask(c *gin.Context) {
	relayInfo, err := relaycommon.GenRelayInfo(c, types.RelayFormatTask, nil, nil)
	if err != nil {
		respondTaskError(c, &dto.TaskError{
			Code:       "gen_relay_info_failed",
			Message:    err.Error(),
			StatusCode: http.StatusInternalServerError,
		})
		return
	}

	if taskErr := relay.ResolveOriginTask(c, relayInfo); taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}

	var result *relay.TaskSubmitResult
	var taskErr *dto.TaskError
	defer func() {
		if taskErr != nil && relayInfo.Billing != nil {
			relayInfo.Billing.Refund(c)
		}
	}()

	retryParam := &service.RetryParam{
		Ctx:        c,
		TokenGroup: relayInfo.TokenGroup,
		ModelName:  relayInfo.OriginModelName,
		Retry:      common.GetPointer(0),
	}

	for ; retryParam.GetRetry() <= common.RetryTimes; retryParam.IncreaseRetry() {
		var channel *model.Channel

		if lockedCh, ok := relayInfo.LockedChannel.(*model.Channel); ok && lockedCh != nil {
			channel = lockedCh
			if retryParam.GetRetry() > 0 {
				if setupErr := middleware.SetupContextForSelectedChannel(c, channel, relayInfo.OriginModelName); setupErr != nil {
					taskErr = service.TaskErrorWrapperLocal(setupErr.Err, "setup_locked_channel_failed", http.StatusInternalServerError)
					break
				}
			}
		} else {
			var channelErr *types.NewAPIError
			channel, channelErr = getChannel(c, relayInfo, retryParam)
			if channelErr != nil {
				logger.LogError(c, channelErr.Error())
				taskErr = service.TaskErrorWrapperLocal(channelErr.Err, "get_channel_failed", http.StatusInternalServerError)
				break
			}
		}

		addUsedChannel(c, channel.Id)
		bodyStorage, bodyErr := common.GetBodyStorage(c)
		if bodyErr != nil {
			if common.IsRequestBodyTooLargeError(bodyErr) || errors.Is(bodyErr, common.ErrRequestBodyTooLarge) {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusRequestEntityTooLarge)
			} else {
				taskErr = service.TaskErrorWrapperLocal(bodyErr, "read_request_body_failed", http.StatusBadRequest)
			}
			break
		}
		c.Request.Body = io.NopCloser(bodyStorage)

		result, taskErr = relay.RelayTaskSubmit(c, relayInfo)
		if taskErr == nil {
			break
		}

		if !taskErr.LocalError {
			processChannelError(c,
				*types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey,
					common.GetContextKeyString(c, constant.ContextKeyChannelKey), channel.GetAutoBan()),
				types.NewOpenAIError(taskErr.Error, types.ErrorCodeBadResponseStatusCode, taskErr.StatusCode))
		}

		if !shouldRetryTaskRelay(c, channel.Id, taskErr, common.RetryTimes-retryParam.GetRetry()) {
			break
		}
	}

	useChannel := c.GetStringSlice("use_channel")
	if len(useChannel) > 1 {
		retryLogStr := fmt.Sprintf("重试：%s", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(useChannel)), "->"), "[]"))
		logger.LogInfo(c, retryLogStr)
	}

	// ── 成功：将预扣转为任务预授权冻结，终态再结算并记录一条日志 ──
	if taskErr == nil {
		isSeedanceVideoModel := ratio_setting.IsSeedanceVideoModel(relayInfo.OriginModelName)
		silentVideoPreAuth := relayInfo.ChannelType == constant.ChannelTypeSora && isSeedanceVideoModel
		// Native DoubaoVideo Seedance tasks expose actual video tokens only when
		// the asynchronous task completes. Keep their estimated reservation
		// silent as well, then write one final log from the native usage payload.
		if (relayInfo.ChannelType == constant.ChannelTypeDoubaoVideo || relayInfo.ChannelType == constant.ChannelTypeDoubaoVideo2 || relayInfo.ChannelType == constant.ChannelTypeZQBAPI) &&
			isSeedanceVideoModel && !relayInfo.PriceData.UsePrice && relayInfo.PriceData.ModelRatio > 0 {
			silentVideoPreAuth = true
		}
		var settleErr error
		consumeLogID := 0
		durableTaskBilling := service.HasDurableBilling(relayInfo)
		if silentVideoPreAuth || durableTaskBilling {
			settleErr = service.SettleTaskPreAuthorization(relayInfo, result.Quota)
		} else {
			settleErr = service.SettleBilling(c, relayInfo, result.Quota)
			consumeLogID = service.LogTaskConsumption(c, relayInfo)
		}
		if settleErr != nil {
			common.SysError("settle task billing error: " + settleErr.Error())
		}

		task := model.InitTask(result.Platform, relayInfo)
		task.PrivateData.UpstreamTaskID = result.UpstreamTaskID
		task.PrivateData.ConsumeLogID = consumeLogID
		task.PrivateData.BillingSource = relayInfo.BillingSource
		task.PrivateData.SubscriptionId = relayInfo.SubscriptionId
		task.PrivateData.TokenId = relayInfo.TokenId
		if relayInfo.ChannelType == constant.ChannelTypeZQBAPI {
			taskdoubao.ApplyZQBAPIOpenAIVideoTaskProperties(c, task)
			if task.Properties.OpenAIVideo {
				task.PrivateData.Key = relayInfo.ApiKey
			}
			if payload, exists := c.Get(string(constant.ContextKeyZQBAPIRetryPayload)); exists {
				if data, ok := payload.([]byte); ok && len(data) > 0 {
					task.PrivateData.ZQBAPIRetryPayload = string(data)
				}
			}
		}
		if relayInfo.ChannelType == constant.ChannelTypeDoubaoVideo2 {
			taskdoubao.ApplyDoubaoVideo2OpenAIVideoTaskProperties(c, task)
			if payload, exists := c.Get(string(constant.ContextKeyDoubaoVideo2RetryPayload)); exists {
				if data, ok := payload.([]byte); ok && len(data) > 0 {
					task.PrivateData.DoubaoVideo2RetryPayload = string(data)
					task.PrivateData.Key = relayInfo.ApiKey
				}
			}
		}
		task.PrivateData.BillingContext = &model.TaskBillingContext{
			ModelPrice:      relayInfo.PriceData.ModelPrice,
			GroupRatio:      relayInfo.PriceData.GroupRatioInfo.GroupRatio,
			ModelRatio:      relayInfo.PriceData.ModelRatio,
			OtherRatios:     relayInfo.PriceData.OtherRatios,
			OriginModelName: relayInfo.OriginModelName,
			PerCallBilling:  common.StringsContains(constant.TaskPricePatches, relayInfo.OriginModelName) || relayInfo.PriceData.UsePrice,
		}
		if silentVideoPreAuth || durableTaskBilling {
			bc := task.PrivateData.BillingContext
			bc.PreAuthorization = true
			bc.PreAuthorizedQuota = result.Quota
			bc.RequestPath = c.Request.URL.Path
			bc.QuotaPerUnit = common.QuotaPerUnit
			bc.VideoTokenBilling = silentVideoPreAuth
			if silentVideoPreAuth {
				if estimatedTextTokens, ok := c.Get("seedance_estimated_text_tokens"); ok {
					bc.EstimatedTextTokens, _ = estimatedTextTokens.(int)
				}
				if estimatedVideoTokens, ok := c.Get("seedance_estimated_video_tokens"); ok {
					bc.EstimatedVideoTokens, _ = estimatedVideoTokens.(int)
				}
				if taskReq, err := relaycommon.GetTaskRequest(c); err == nil {
					bc.VideoResolution = taskReq.Size
					if resolution, ok := taskReq.Metadata["resolution"].(string); ok && resolution != "" {
						bc.VideoResolution = resolution
					}
				}
			}
		}
		if v, ok := c.Get("doubao_seedance_has_video"); ok {
			if hasVideo, ok := v.(bool); ok {
				task.PrivateData.BillingContext.VideoHasInput = &hasVideo
			}
		}
		task.Quota = result.Quota
		task.Data = result.TaskData
		task.Action = relayInfo.Action
		if insertErr := service.InsertTaskWithBilling(task, relayInfo); insertErr != nil {
			common.SysError("insert task error: " + insertErr.Error())
			if refundErr := service.RefundUnboundDurableBilling(relayInfo); refundErr != nil {
				common.SysError("refund unbound durable task billing error: " + refundErr.Error())
			}
			if len(result.ResponseBody) > 0 {
				if !service.HasDurableBilling(relayInfo) {
					service.RefundTaskQuota(c, task, "任务创建失败")
				}
				respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("video task could not be persisted"), "task_persistence_failed", http.StatusInternalServerError))
				return
			}
			if silentVideoPreAuth && !service.HasDurableBilling(relayInfo) {
				service.RefundTaskQuota(c, task, "任务创建失败")
			}
		} else if len(result.ResponseBody) > 0 {
			status := result.ResponseStatus
			if status == 0 {
				status = http.StatusOK
			}
			c.Data(status, "application/json; charset=utf-8", result.ResponseBody)
		}
	}

	if taskErr != nil {
		respondTaskError(c, taskErr)
	}
}

// respondTaskError 统一输出 Task 错误响应（含 429 限流提示改写）
func respondTaskError(c *gin.Context, taskErr *dto.TaskError) {
	if taskErr == nil {
		return
	}
	isOpenAIVideoRequest := isOpenAIVideoPublicRequest(c)
	if taskErr.StatusCode == http.StatusTooManyRequests {
		if isOpenAIVideoRequest {
			taskErr.Message = "The video service is temporarily busy. Please retry later."
		} else {
			taskErr.Message = "当前分组上游负载已饱和，请稍后再试"
		}
	} else if isOpenAIVideoRequest {
		taskErr.Message = sanitizePublicErrorMessage(taskErr.Message)
	}
	if isOpenAIVideoRequest {
		message := taskErr.Message
		if taskErr.StatusCode >= http.StatusInternalServerError {
			switch taskErr.Code {
			case "video_service_error", "video_service_authentication_failed", "task_persistence_failed":
			default:
				message = "The video request could not be completed because of an internal error."
			}
		}
		writeOpenAIVideoError(c, taskErr.StatusCode, taskErr.Code, "", message)
		return
	}
	c.JSON(taskErr.StatusCode, taskErr)
}

func shouldRetryTaskRelay(c *gin.Context, channelId int, taskErr *dto.TaskError, retryTimes int) bool {
	if taskErr == nil {
		return false
	}
	if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
		return false
	}
	if retryTimes <= 0 {
		return false
	}
	if _, ok := c.Get("specific_channel_id"); ok {
		return false
	}
	if taskErr.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if taskErr.StatusCode == 307 {
		return true
	}
	if taskErr.StatusCode/100 == 5 {
		// 超时不重试
		if operation_setting.IsAlwaysSkipRetryStatusCode(taskErr.StatusCode) {
			return false
		}
		return true
	}
	if taskErr.StatusCode == http.StatusBadRequest {
		return false
	}
	if taskErr.StatusCode == 408 {
		// azure处理超时不重试
		return false
	}
	if taskErr.LocalError {
		return false
	}
	if taskErr.StatusCode/100 == 2 {
		return false
	}
	return true
}
