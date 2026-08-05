package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type ModelRequest struct {
	Model string `json:"model"`
	Group string `json:"group,omitempty"`
}

func Distribute() func(c *gin.Context) {
	return func(c *gin.Context) {
		var channel *model.Channel
		channelId, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId)
		modelRequest, shouldSelectChannel, err := getModelRequest(c)
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		if compactModelEndpointMismatch(c.Request.URL.Path, modelRequest.Model) {
			abortWithOpenAiMessage(
				c,
				http.StatusBadRequest,
				i18n.T(c, i18n.MsgDistributorCompactModelEndpointMismatch, map[string]any{
					"Model": ratio_setting.BaseModelFromCompactVirtualModel(modelRequest.Model),
				}),
				types.ErrorCodeCompactModelEndpointMismatch,
			)
			return
		}
		if ok {
			id, err := strconv.Atoi(channelId.(string))
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			channel, err = model.GetChannelById(id, true)
			if err != nil {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidChannelId))
				return
			}
			if channel.Status != common.ChannelStatusEnabled {
				abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorChannelDisabled))
				return
			}
			if !service.ChannelSupportsRequestPath(channel, c.Request.URL.Path, modelRequest.Model) {
				abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorChannelEndpointUnsupported, map[string]any{"Path": c.Request.URL.Path}), types.ErrorCodeInvalidRequest)
				return
			}
		} else {
			// Select a channel for the user
			// check token model mapping
			modelLimitEnable := common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
			if modelLimitEnable {
				s, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
				if !ok {
					// token model limit is empty, all models are not allowed
					abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenNoModelAccess))
					return
				}
				var tokenModelLimit map[string]bool
				tokenModelLimit, ok = s.(map[string]bool)
				if !ok {
					tokenModelLimit = map[string]bool{}
				}
				matchName := ratio_setting.FormatMatchingModelName(modelRequest.Model) // match gpts & thinking-*
				if _, ok := tokenModelLimit[matchName]; !ok {
					abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorTokenModelForbidden, map[string]any{"Model": modelRequest.Model}))
					return
				}
			}

			if shouldSelectChannel {
				if modelRequest.Model == "" {
					abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorModelNameRequired))
					return
				}
				var selectGroup string
				usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
				// check path is /pg/chat/completions
				if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
					playgroundRequest := &dto.PlayGroundRequest{}
					err = common.UnmarshalBodyReusable(c, playgroundRequest)
					if err != nil {
						abortWithOpenAiMessage(c, http.StatusBadRequest, i18n.T(c, i18n.MsgDistributorInvalidPlayground, map[string]any{"Error": err.Error()}))
						return
					}
					if playgroundRequest.Group != "" {
						if !service.GroupInUserUsableGroups(usingGroup, playgroundRequest.Group) && playgroundRequest.Group != usingGroup {
							abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorGroupAccessDenied))
							return
						}
						usingGroup = playgroundRequest.Group
						common.SetContextKey(c, constant.ContextKeyUsingGroup, usingGroup)
					}
				}

				if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, modelRequest.Model, usingGroup); found {
					preferred, err := model.CacheGetChannel(preferredChannelID)
					if err == nil && service.ChannelSupportsRequestPath(preferred, c.Request.URL.Path, modelRequest.Model) {
						if preferred.Status != common.ChannelStatusEnabled {
							if service.ShouldSkipRetryAfterChannelAffinityFailure(c) {
								abortWithOpenAiMessage(c, http.StatusForbidden, i18n.T(c, i18n.MsgDistributorAffinityChannelDisabled))
								return
							}
						} else if usingGroup == "auto" {
							userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
							autoGroups := service.GetUserAutoGroup(userGroup)
							for _, g := range autoGroups {
								if model.IsChannelEnabledForGroupModel(g, modelRequest.Model, preferred.Id) {
									selectGroup = g
									common.SetContextKey(c, constant.ContextKeyAutoGroup, g)
									channel = preferred
									service.MarkChannelAffinityUsed(c, g, preferred.Id)
									break
								}
							}
						} else if model.IsChannelEnabledForGroupModel(usingGroup, modelRequest.Model, preferred.Id) {
							channel = preferred
							selectGroup = usingGroup
							service.MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
						}
					}
				}

				if channel == nil {
					channel, selectGroup, err = service.CacheGetRandomSatisfiedChannelFiltered(&service.RetryParam{
						Ctx:        c,
						ModelName:  modelRequest.Model,
						TokenGroup: usingGroup,
						Retry:      common.GetPointer(0),
					}, service.ChannelFilterForRequestPath(c.Request.URL.Path, modelRequest.Model))
					if err != nil {
						showGroup := usingGroup
						if usingGroup == "auto" {
							showGroup = fmt.Sprintf("auto(%s)", selectGroup)
						}
						message := i18n.T(c, i18n.MsgDistributorGetChannelFailed, map[string]any{"Group": showGroup, "Model": modelRequest.Model, "Error": err.Error()})
						// 如果错误，但是渠道不为空，说明是数据库一致性问题
						//if channel != nil {
						//	common.SysError(fmt.Sprintf("渠道不存在：%d", channel.Id))
						//	message = "数据库一致性已被破坏，请联系管理员"
						//}
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable, message, types.ErrorCodeModelNotFound)
						return
					}
					if channel == nil {
						abortWithOpenAiMessage(c, http.StatusServiceUnavailable, i18n.T(c, i18n.MsgDistributorNoAvailableChannel, map[string]any{"Group": usingGroup, "Model": modelRequest.Model}), types.ErrorCodeModelNotFound)
						return
					}
				}
			}
		}
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		// Task/video fetch (and similar) intentionally skip channel selection;
		// handlers resolve the origin channel from the stored task.
		if channel != nil {
			var setupErr *types.NewAPIError
			if channel.CredentialSource == constant.ChannelCredentialSourceAccountPool {
				setupErr = SetupContextForSelectedChannelWithoutAccount(c, channel, modelRequest.Model)
			} else {
				setupErr = SetupContextForSelectedChannel(c, channel, modelRequest.Model)
			}
			if setupErr != nil {
				abortWithOpenAiMessage(c, http.StatusServiceUnavailable, setupErr.Error())
				return
			}
			defer ReleaseUpstreamAccountSelection(c)
		}
		c.Next()
		if channel != nil && c.Writer != nil && c.Writer.Status() < http.StatusBadRequest {
			service.RecordChannelAffinity(c, channel.Id)
		}
	}
}

func compactModelEndpointMismatch(requestPath string, modelName string) bool {
	return ratio_setting.IsCompactVirtualModel(modelName) &&
		requestPath != "/v1/responses/compact"
}

// getModelFromRequest 从请求中读取模型信息
// 根据 Content-Type 自动处理：
// - application/json
// - application/x-www-form-urlencoded
// - multipart/form-data
func getModelFromRequest(c *gin.Context) (*ModelRequest, error) {
	if strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
		modelRequest, err := getModelFromJSONBody(c)
		if err != nil {
			return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
		}
		return modelRequest, nil
	}

	var modelRequest ModelRequest
	err := common.UnmarshalBodyReusable(c, &modelRequest)
	if err != nil {
		return nil, errors.New(i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
	}
	return &modelRequest, nil
}

func getModelFromJSONBody(c *gin.Context) (*ModelRequest, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	requestBody, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if !gjson.ValidBytes(requestBody) {
		return nil, errors.New("invalid JSON request body")
	}

	values := gjson.GetManyBytes(requestBody, "model", "group")
	model, err := getJSONStringValue(values[0], "model")
	if err != nil {
		return nil, err
	}
	group, err := getJSONStringValue(values[1], "group")
	if err != nil {
		return nil, err
	}

	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr != nil {
		return nil, seekErr
	}
	c.Request.Body = io.NopCloser(storage)

	return &ModelRequest{
		Model: model,
		Group: group,
	}, nil
}

func getJSONStringValue(result gjson.Result, field string) (string, error) {
	if !result.Exists() || result.Type == gjson.Null {
		return "", nil
	}
	if result.Type != gjson.String {
		return "", fmt.Errorf("field %s must be a string", field)
	}
	return result.String(), nil
}

func getModelRequest(c *gin.Context) (*ModelRequest, bool, error) {
	var modelRequest ModelRequest
	shouldSelectChannel := true
	var err error
	if strings.Contains(c.Request.URL.Path, "/mj/") {
		relayMode := relayconstant.Path2RelayModeMidjourney(c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeMidjourneyTaskFetch ||
			relayMode == relayconstant.RelayModeMidjourneyTaskFetchByCondition ||
			relayMode == relayconstant.RelayModeMidjourneyNotify ||
			relayMode == relayconstant.RelayModeMidjourneyTaskImageSeed {
			shouldSelectChannel = false
		} else {
			midjourneyRequest := dto.MidjourneyRequest{}
			err = common.UnmarshalBodyReusable(c, &midjourneyRequest)
			if err != nil {
				return nil, false, errors.New(i18n.T(c, i18n.MsgDistributorInvalidMidjourney, map[string]any{"Error": err.Error()}))
			}
			midjourneyModel, mjErr, success := service.GetMjRequestModel(relayMode, &midjourneyRequest)
			if mjErr != nil {
				return nil, false, fmt.Errorf("%s", mjErr.Description)
			}
			if midjourneyModel == "" {
				if !success {
					return nil, false, fmt.Errorf("%s", i18n.T(c, i18n.MsgDistributorInvalidParseModel))
				} else {
					// task fetch, task fetch by condition, notify
					shouldSelectChannel = false
				}
			}
			modelRequest.Model = midjourneyModel
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/suno/") {
		relayMode := relayconstant.Path2RelaySuno(c.Request.Method, c.Request.URL.Path)
		if relayMode == relayconstant.RelayModeSunoFetch ||
			relayMode == relayconstant.RelayModeSunoFetchByID {
			shouldSelectChannel = false
		} else {
			modelName := service.CoverTaskActionToModelName(constant.TaskPlatformSuno, c.Param("action"))
			modelRequest.Model = modelName
		}
		c.Set("platform", string(constant.TaskPlatformSuno))
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos/") && strings.HasSuffix(c.Request.URL.Path, "/remix") {
		relayMode := relayconstant.RelayModeVideoSubmit
		c.Set("relay_mode", relayMode)
		shouldSelectChannel = false
	} else if strings.Contains(c.Request.URL.Path, "/v1/videos") {
		//curl https://api.openai.com/v1/videos \
		//  -H "Authorization: Bearer $OPENAI_API_KEY" \
		//  -F "model=sora-2" \
		//  -F "prompt=A calico cat playing a piano on stage"
		//	-F input_reference="@image.jpg"
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			relayMode = relayconstant.RelayModeVideoSubmit
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			if req != nil {
				modelRequest.Model = req.Model
			}
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
		}
		c.Set("relay_mode", relayMode)
	} else if strings.Contains(c.Request.URL.Path, "/v1/video/generations") {
		relayMode := relayconstant.RelayModeUnknown
		if c.Request.Method == http.MethodPost {
			req, err := getModelFromRequest(c)
			if err != nil {
				return nil, false, err
			}
			modelRequest.Model = req.Model
			relayMode = relayconstant.RelayModeVideoSubmit
		} else if c.Request.Method == http.MethodGet {
			relayMode = relayconstant.RelayModeVideoFetchByID
			shouldSelectChannel = false
		}
		if _, ok := c.Get("relay_mode"); !ok {
			c.Set("relay_mode", relayMode)
		}
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1beta/models/") || strings.HasPrefix(c.Request.URL.Path, "/v1/models/") {
		// Gemini API 路径处理: /v1beta/models/gemini-2.0-flash:generateContent
		relayMode := relayconstant.RelayModeGemini
		modelName := extractModelNameFromGeminiPath(c.Request.URL.Path)
		if modelName != "" {
			modelRequest.Model = modelName
		}
		c.Set("relay_mode", relayMode)
	} else if !strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") && !strings.Contains(c.Request.Header.Get("Content-Type"), "multipart/form-data") {
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/realtime") {
		//wss://api.openai.com/v1/realtime?model=gpt-4o-realtime-preview-2024-10-01
		modelRequest.Model = c.Query("model")
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/moderations") {
		if modelRequest.Model == "" {
			modelRequest.Model = "text-moderation-stable"
		}
	}
	if strings.HasSuffix(c.Request.URL.Path, "embeddings") {
		if modelRequest.Model == "" {
			modelRequest.Model = c.Param("model")
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/images/generations") {
		modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "dall-e")
	} else if strings.HasPrefix(c.Request.URL.Path, "/v1/images/edits") {
		//modelRequest.Model = common.GetStringIfEmpty(c.PostForm("model"), "gpt-image-1")
		contentType := c.ContentType()
		if slices.Contains([]string{gin.MIMEPOSTForm, gin.MIMEMultipartPOSTForm}, contentType) {
			req, err := getModelFromRequest(c)
			if err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
		}
	}
	if strings.HasPrefix(c.Request.URL.Path, "/v1/audio") {
		relayMode := relayconstant.RelayModeAudioSpeech
		if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/speech") {

			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "tts-1")
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/translations") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranslation
		} else if strings.HasPrefix(c.Request.URL.Path, "/v1/audio/transcriptions") {
			// 先尝试从请求读取
			if req, err := getModelFromRequest(c); err == nil && req.Model != "" {
				modelRequest.Model = req.Model
			}
			modelRequest.Model = common.GetStringIfEmpty(modelRequest.Model, "whisper-1")
			relayMode = relayconstant.RelayModeAudioTranscription
		}
		c.Set("relay_mode", relayMode)
	}
	if strings.HasPrefix(c.Request.URL.Path, "/pg/chat/completions") {
		// playground chat completions
		req, err := getModelFromRequest(c)
		if err != nil {
			return nil, false, err
		}
		modelRequest.Model = req.Model
		modelRequest.Group = req.Group
		common.SetContextKey(c, constant.ContextKeyTokenGroup, modelRequest.Group)
	}

	if strings.HasPrefix(c.Request.URL.Path, "/v1/responses/compact") && modelRequest.Model != "" {
		modelRequest.Model = ratio_setting.WithCompactModelSuffix(modelRequest.Model)
	}
	return &modelRequest, shouldSelectChannel, nil
}

func SetupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	return setupContextForSelectedChannel(c, channel, modelName, true)
}

func SetupContextForSelectedChannelWithoutAccount(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	return setupContextForSelectedChannel(c, channel, modelName, false)
}

func setupContextForSelectedChannel(c *gin.Context, channel *model.Channel, modelName string, acquireAccount bool) *types.NewAPIError {
	previousChannelId := common.GetContextKeyInt(c, constant.ContextKeyChannelId)
	ReleaseUpstreamAccountSelection(c)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPoolId, 0)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountId, 0)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountName, "")
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlatform, "")
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountType, "")
	common.SetContextKey(c, constant.ContextKeyUpstreamProxyId, 0)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountLeaseId, "")
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountMappedModel, "")
	// Do not let an account-less channel or the next failover attempt inherit
	// the previous account's billing multiplier. setupLocalUpstreamAccount
	// overwrites this with the selected account's value when one is configured.
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountRateMultiplier, 1.0)
	common.SetContextKey(c, constant.ContextKeyUpstreamInterceptWarmup, false)
	c.Set("original_model", modelName) // for retry
	if channel == nil {
		return types.NewError(errors.New("channel is nil"), types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
	if previousChannelId != channel.Id {
		common.SetContextKey(c, constant.ContextKeyUpstreamAccountExcluded, map[int]struct{}{})
	}
	common.SetContextKey(c, constant.ContextKeyChannelName, channel.Name)
	common.SetContextKey(c, constant.ContextKeyChannelType, channel.Type)
	common.SetContextKey(c, constant.ContextKeyChannelCreateTime, channel.CreatedTime)
	common.SetContextKey(c, constant.ContextKeyChannelSetting, channel.GetSetting())
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, channel.GetOtherSettings())
	paramOverride := channel.GetParamOverride()
	headerOverride := channel.GetHeaderOverride()
	if mergedParam, applied := service.ApplyChannelAffinityOverrideTemplate(c, paramOverride); applied {
		paramOverride = mergedParam
	}
	common.SetContextKey(c, constant.ContextKeyChannelParamOverride, paramOverride)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, headerOverride)
	if nil != channel.OpenAIOrganization && *channel.OpenAIOrganization != "" {
		common.SetContextKey(c, constant.ContextKeyChannelOrganization, *channel.OpenAIOrganization)
	}
	common.SetContextKey(c, constant.ContextKeyChannelAutoBan, channel.GetAutoBan())
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, channel.GetModelMapping())
	common.SetContextKey(c, constant.ContextKeyChannelStatusCodeMapping, channel.GetStatusCodeMapping())
	common.SetContextKey(c, constant.ContextKeyChannelCredentialSource, channel.CredentialSource)

	key, index, newAPIError := channel.GetNextEnabledKey()
	if newAPIError != nil {
		return newAPIError
	}
	if channel.ChannelInfo.IsMultiKey {
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
		common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, index)
	} else {
		// 必须设置为 false，否则在重试到单个 key 的时候会导致日志显示错误
		common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, false)
	}
	// c.Request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", key))
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, channel.GetBaseURL())
	if acquireAccount && channel.CredentialSource == constant.ChannelCredentialSourceAccountPool {
		if apiErr := setupLocalUpstreamAccount(c, channel, modelName); apiErr != nil {
			return apiErr
		}
	}

	common.SetContextKey(c, constant.ContextKeySystemPromptOverride, false)

	// TODO: api_version统一
	switch channel.Type {
	case constant.ChannelTypeAzure:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeVertexAi:
		c.Set("region", channel.Other)
	case constant.ChannelTypeXunfei:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeGemini:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeAli:
		c.Set("plugin", channel.Other)
	case constant.ChannelCloudflare:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeMokaAI:
		c.Set("api_version", channel.Other)
	case constant.ChannelTypeCoze:
		c.Set("bot_id", channel.Other)
	}
	return nil
}

func setupLocalUpstreamAccount(c *gin.Context, channel *model.Channel, modelName string) *types.NewAPIError {
	if channel.UpstreamAccountPoolId == nil || *channel.UpstreamAccountPoolId <= 0 {
		return types.NewErrorWithStatusCode(errors.New("local account pool is not configured"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	router, err := service.GetConfiguredUpstreamAccountRouter()
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	setting := channel.GetSetting()
	// Local accounts own their connection protocol. Ignore legacy channel-level
	// transport settings so every request follows the selected account.
	setting.Proxy = ""
	setting.PassThroughBodyEnabled = false
	selectionModel := strings.TrimSpace(modelName)
	compactRequest := strings.HasSuffix(selectionModel, ratio_setting.CompactModelSuffix)
	if compactRequest {
		selectionModel = strings.TrimSuffix(selectionModel, ratio_setting.CompactModelSuffix)
	}
	codexClient, codexAppServer := detectCodexClient(c)
	excludedIds, _ := common.GetContextKeyType[map[int]struct{}](c, constant.ContextKeyUpstreamAccountExcluded)
	accountAffinity, hasAccountAffinity := service.GetUpstreamAccountAffinityContext(c)
	accountAffinitySeed := ""
	accountAffinityKeyFP := ""
	accountAffinityTTL := 0
	if hasAccountAffinity {
		accountAffinitySeed = accountAffinity.KeySeed
		accountAffinityKeyFP = accountAffinity.KeyFingerprint
		accountAffinityTTL = accountAffinity.TTLSeconds
	}
	selection, err := router.Select(c.Request.Context(), service.UpstreamAccountSelectionRequest{
		PoolId: *channel.UpstreamAccountPoolId, ChannelType: channel.Type,
		AllowedAccountTypes: localUpstreamAllowedAccountTypes(
			channel.Type,
			c.Request.URL.Path,
			!model_setting.GetGlobalSettings().PassThroughRequestEnabled && !setting.PassThroughBodyEnabled,
		), Model: selectionModel, RequestPath: c.Request.URL.Path, CompactRequest: compactRequest,
		CodexClient: codexClient, CodexAppServer: codexAppServer,
		ChannelProxy: "", RequestId: c.GetString(common.RequestIdKey), ExcludedIds: excludedIds,
		ResponsesWebSocket:    common.GetContextKeyBool(c, constant.ContextKeyResponsesWebSocketIngress),
		PreferredAccountId:    common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountPreferredId),
		RequirePreferred:      common.GetContextKeyBool(c, constant.ContextKeyUpstreamAccountPreferredRequired),
		RequiredWebSocketMode: common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountRequiredWSMode),
		AccountAffinitySeed:   accountAffinitySeed,
		AccountAffinityKeyFP:  accountAffinityKeyFP,
		AccountAffinityTTL:    accountAffinityTTL,
	})
	if err != nil {
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	service.MarkUpstreamAccountAffinitySelection(c, selection.Affinity)
	effectiveChannelType, err := localUpstreamChannelType(selection.Account.Platform, selection.Account.Type)
	if err != nil {
		_ = selection.Release(context.Background())
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	options, err := model.ParseUpstreamAccountOptionsWithCredentials(selection.Account.Extra, selection.Credentials)
	if err != nil {
		_ = selection.Release(context.Background())
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	key, err := localUpstreamChannelKey(selection.Account.Platform, selection.Account.Type, selection.Credentials)
	if err != nil {
		_ = selection.Release(context.Background())
		return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	setting.Proxy = selection.ProxyURL
	if options.OpenAIPassthrough || options.AnthropicPassthrough {
		setting.AccountPassThroughBodyEnabled = true
	}
	common.SetContextKey(c, constant.ContextKeyChannelSetting, setting)
	otherSettings, _ := common.GetContextKeyType[dto.ChannelOtherSettings](c, constant.ContextKeyChannelOtherSetting)
	switch effectiveChannelType {
	case constant.ChannelTypeAws:
		if isUpstreamBedrockAPIKeyMode(selection.Credentials) {
			otherSettings.AwsKeyType = dto.AwsKeyTypeApiKey
		} else {
			otherSettings.AwsKeyType = dto.AwsKeyTypeAKSK
		}
	case constant.ChannelTypeVertexAi:
		otherSettings.VertexKeyType = dto.VertexKeyTypeJSON
		region := upstreamCredentialString(selection.Credentials, "location")
		if region == "" {
			region = "global"
		}
		c.Set("region", region)
	case constant.ChannelTypeOpenAI, constant.ChannelTypeCodex:
		wsMode := options.OpenAIWSMode(selection.Account.Type)
		otherSettings.ResponsesWebSocketV2Mode = wsMode
		otherSettings.ResponsesWebSocketV2Enabled = wsMode == model.UpstreamOpenAIWSModeContextPool || wsMode == model.UpstreamOpenAIWSModePassthrough
		if strings.HasPrefix(strings.TrimSpace(c.Request.URL.Path), "/v1/alpha/search") {
			otherSettings.AlphaSearchEnabled = true
		}
	}
	common.SetContextKey(c, constant.ContextKeyChannelOtherSetting, otherSettings)
	accountHeaderOverrides := service.MergeUpstreamAccountHeaderOverrides(
		nil,
		&selection.Account,
		selection.Credentials,
	)
	common.SetContextKey(c, constant.ContextKeyChannelHeaderOverride, accountHeaderOverrides)
	common.SetContextKey(c, constant.ContextKeyChannelModelMapping, "")
	common.SetContextKey(c, constant.ContextKeyChannelOrganization, "")
	common.SetContextKey(c, constant.ContextKeyChannelType, effectiveChannelType)
	common.SetContextKey(c, constant.ContextKeyChannelKey, key)
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, localUpstreamBaseURL(effectiveChannelType, selection.Credentials, ""))
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPoolId, selection.Pool.Id)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountId, selection.Account.Id)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountName, selection.Account.Name)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlatform, selection.Account.Platform)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountType, selection.Account.Type)
	common.SetContextKey(c, constant.ContextKeyUpstreamAnthropicAuthScheme, options.AnthropicAPIKeyAuthScheme)
	common.SetContextKey(c, constant.ContextKeyUpstreamOpenAIResponsesMode, options.EffectiveOpenAIResponsesMode())
	common.SetContextKey(c, constant.ContextKeyUpstreamOpenAILongContextBilling, options.OpenAILongContextBillingEnabled)
	common.SetContextKey(c, constant.ContextKeyUpstreamInterceptWarmup, options.InterceptWarmupRequests)
	if selection.Account.RateMultiplier != nil {
		common.SetContextKey(c, constant.ContextKeyUpstreamAccountRateMultiplier, *selection.Account.RateMultiplier)
	}
	if selection.ModelMapped {
		common.SetContextKey(c, constant.ContextKeyUpstreamAccountMappedModel, selection.MappedModel)
	}
	proxyId := 0
	if selection.Proxy != nil {
		proxyId = selection.Proxy.Id
		common.SetContextKey(c, constant.ContextKeyUpstreamProxyId, proxyId)
	}
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountLeaseId, selection.Lease.Id)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountSelection, selection)
	requestId := c.GetString(common.RequestIdKey)
	service.RecordUpstreamAccountEventAsync(service.UpstreamAccountEventInput{
		AccountId: selection.Account.Id, PoolId: selection.Pool.Id, ProxyId: proxyId,
		ChannelId: channel.Id, RequestId: requestId, LeaseId: selection.Lease.Id,
		EventType: "request_selected", Result: "selected",
	})
	leaseRequestContext, cancelLeaseRequest := context.WithCancelCause(c.Request.Context())
	c.Request = c.Request.WithContext(leaseRequestContext)
	selection.StartLeaseRefresh(leaseRequestContext, func(leaseErr error) {
		service.RecordUpstreamAccountEvent(service.UpstreamAccountEventInput{
			AccountId: selection.Account.Id, PoolId: selection.Pool.Id, ProxyId: proxyId,
			ChannelId: channel.Id, RequestId: requestId, LeaseId: selection.Lease.Id,
			EventType: "lease_lost", Result: "error", Message: "upstream account lease lost",
		})
		cancelLeaseRequest(leaseErr)
	})
	return nil
}

func detectCodexClient(c *gin.Context) (bool, bool) {
	if c == nil || c.Request == nil {
		return false, false
	}
	identity := strings.ToLower(strings.TrimSpace(c.GetHeader("User-Agent") + " " + c.GetHeader("originator")))
	return strings.Contains(identity, "codex"), strings.Contains(identity, "app-server") || strings.Contains(identity, "app_server")
}

func ReleaseUpstreamAccountSelection(c *gin.Context) {
	if c == nil {
		return
	}
	value, ok := common.GetContextKey(c, constant.ContextKeyUpstreamAccountSelection)
	if !ok {
		return
	}
	selection, ok := value.(*service.UpstreamAccountSelection)
	if ok && selection != nil {
		_ = selection.Release(context.Background())
		service.RecordUpstreamAccountEvent(service.UpstreamAccountEventInput{
			AccountId: common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountId),
			PoolId:    common.GetContextKeyInt(c, constant.ContextKeyUpstreamAccountPoolId),
			ProxyId:   common.GetContextKeyInt(c, constant.ContextKeyUpstreamProxyId),
			ChannelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
			RequestId: c.GetString(common.RequestIdKey),
			LeaseId:   common.GetContextKeyString(c, constant.ContextKeyUpstreamAccountLeaseId),
			EventType: "lease_released", Result: "released",
		})
	}
	c.Set(string(constant.ContextKeyUpstreamAccountSelection), nil)
}

func localUpstreamChannelKey(platform string, accountType string, credentials map[string]any) (string, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountType = strings.ToLower(strings.TrimSpace(accountType))
	switch {
	case platform == constant.UpstreamPlatformOpenAI && accountType == constant.UpstreamAccountTypeOAuth:
		encoded, err := common.Marshal(credentials)
		if err != nil {
			return "", errors.New("failed to encode local OAuth credential")
		}
		return string(encoded), nil
	case accountType == constant.UpstreamAccountTypeAPIKey:
		key := upstreamCredentialString(credentials, "api_key")
		if key == "" {
			return "", errors.New("local API key credential is missing api_key")
		}
		return key, nil
	case platform == constant.UpstreamPlatformAnthropic && (accountType == constant.UpstreamAccountTypeOAuth || accountType == constant.UpstreamAccountTypeSetupToken):
		key := upstreamCredentialString(credentials, "access_token")
		if key == "" {
			return "", errors.New("local Claude credential is missing access_token")
		}
		return key, nil
	case platform == constant.UpstreamPlatformAnthropic && accountType == constant.UpstreamAccountTypeBedrock:
		region := upstreamCredentialString(credentials, "aws_region")
		if isUpstreamBedrockAPIKeyMode(credentials) {
			key := upstreamCredentialString(credentials, "api_key")
			if key == "" || region == "" {
				return "", errors.New("local Bedrock API key credential is incomplete")
			}
			return key + "|" + region, nil
		}
		accessKey := upstreamCredentialString(credentials, "aws_access_key_id")
		secretKey := upstreamCredentialString(credentials, "aws_secret_access_key")
		sessionToken := upstreamCredentialString(credentials, "aws_session_token")
		if accessKey == "" || secretKey == "" || region == "" {
			return "", errors.New("local Bedrock SigV4 credential is incomplete")
		}
		if sessionToken != "" {
			return strings.Join([]string{accessKey, secretKey, sessionToken, region}, "|"), nil
		}
		return strings.Join([]string{accessKey, secretKey, region}, "|"), nil
	case platform == constant.UpstreamPlatformAnthropic && accountType == constant.UpstreamAccountTypeServiceAccount:
		key := upstreamCredentialString(credentials, "service_account_json")
		if key == "" {
			return "", errors.New("local Vertex credential is missing service_account_json")
		}
		return key, nil
	}
	return "", errors.New("channel type does not support local account credentials")
}

func localUpstreamAllowedAccountTypes(channelType int, requestPath string, allowCompatibilityConversion bool) []string {
	if channelType == constant.ChannelTypeAnthropic {
		return []string{
			constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeSetupToken,
			constant.UpstreamAccountTypeAPIKey, constant.UpstreamAccountTypeBedrock,
			constant.UpstreamAccountTypeServiceAccount,
		}
	}
	requestPath = strings.TrimSpace(requestPath)
	if strings.HasPrefix(requestPath, "/v1/alpha/search") {
		return []string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeAPIKey}
	}
	if strings.HasPrefix(requestPath, "/v1/responses") {
		return []string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeAPIKey}
	}
	if allowCompatibilityConversion && (strings.HasPrefix(requestPath, "/v1/chat/completions") || strings.HasPrefix(requestPath, "/pg/chat/completions")) {
		return []string{constant.UpstreamAccountTypeOAuth, constant.UpstreamAccountTypeAPIKey}
	}
	return []string{constant.UpstreamAccountTypeAPIKey}
}

func localUpstreamChannelType(platform string, accountType string) (int, error) {
	platform = strings.ToLower(strings.TrimSpace(platform))
	accountType = strings.ToLower(strings.TrimSpace(accountType))
	switch {
	case platform == constant.UpstreamPlatformOpenAI && accountType == constant.UpstreamAccountTypeOAuth:
		return constant.ChannelTypeCodex, nil
	case platform == constant.UpstreamPlatformOpenAI && accountType == constant.UpstreamAccountTypeAPIKey:
		return constant.ChannelTypeOpenAI, nil
	case platform == constant.UpstreamPlatformAnthropic && (accountType == constant.UpstreamAccountTypeOAuth || accountType == constant.UpstreamAccountTypeSetupToken || accountType == constant.UpstreamAccountTypeAPIKey):
		return constant.ChannelTypeAnthropic, nil
	case platform == constant.UpstreamPlatformAnthropic && accountType == constant.UpstreamAccountTypeBedrock:
		return constant.ChannelTypeAws, nil
	case platform == constant.UpstreamPlatformAnthropic && accountType == constant.UpstreamAccountTypeServiceAccount:
		return constant.ChannelTypeVertexAi, nil
	default:
		return constant.ChannelTypeUnknown, fmt.Errorf("unsupported local upstream account %q/%q", platform, accountType)
	}
}

func localUpstreamBaseURL(channelType int, credentials map[string]any, channelBaseURL string) string {
	if channelType == constant.ChannelTypeCodex {
		return constant.ChannelBaseURLs[constant.ChannelTypeCodex]
	}
	if baseURL := upstreamCredentialString(credentials, "base_url"); baseURL != "" {
		return baseURL
	}
	if (channelType == constant.ChannelTypeOpenAI || channelType == constant.ChannelTypeAnthropic) && strings.TrimSpace(channelBaseURL) != "" {
		return channelBaseURL
	}
	if channelType >= 0 && channelType < len(constant.ChannelBaseURLs) {
		return constant.ChannelBaseURLs[channelType]
	}
	return ""
}

func upstreamCredentialString(credentials map[string]any, key string) string {
	value, ok := credentials[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func isUpstreamBedrockAPIKeyMode(credentials map[string]any) bool {
	mode := strings.ToLower(upstreamCredentialString(credentials, "auth_mode"))
	return mode == "apikey" || mode == "api_key"
}

// SelectChannelForModel selects and initializes a channel without reading an
// HTTP request body. It is used by protocols whose model arrives after the
// initial HTTP handshake, such as Responses WebSocket v2.
func SelectChannelForModel(c *gin.Context, modelName string) (*model.Channel, *types.NewAPIError) {
	return SelectChannelForModelFiltered(c, modelName, nil)
}

func SelectChannelForModelFiltered(c *gin.Context, modelName string, filter func(*model.Channel) bool) (*model.Channel, *types.NewAPIError) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		return nil, types.NewErrorWithStatusCode(
			errors.New("model is required"),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		value, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
		if !ok {
			return nil, types.NewErrorWithStatusCode(errors.New("token has no model access"), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
		limits, ok := value.(map[string]bool)
		if !ok {
			limits = map[string]bool{}
		}
		matchName := ratio_setting.FormatMatchingModelName(modelName)
		if !limits[matchName] {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("token is not allowed to use model %s", modelName), types.ErrorCodeAccessDenied, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
	}

	var channel *model.Channel
	if channelID, ok := common.GetContextKey(c, constant.ContextKeyTokenSpecificChannelId); ok {
		id, err := strconv.Atoi(fmt.Sprint(channelID))
		if err != nil {
			return nil, types.NewErrorWithStatusCode(errors.New("invalid specific channel id"), types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		channel, err = model.GetChannelById(id, true)
		if err != nil || channel == nil {
			return nil, types.NewErrorWithStatusCode(errors.New("specific channel not found"), types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if channel.Status != common.ChannelStatusEnabled {
			return nil, types.NewErrorWithStatusCode(errors.New("specific channel is disabled"), types.ErrorCodeGetChannelFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry())
		}
		if filter != nil && !filter(channel) {
			return nil, types.NewErrorWithStatusCode(errors.New("specific channel does not support the requested protocol"), types.ErrorCodeGetChannelFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	} else {
		usingGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		selectGroup := usingGroup
		continuationChannelID := common.GetContextKeyInt(c, constant.ContextKeyResponsesWebSocketPreferredChannelId)
		if continuationChannelID > 0 {
			preferred, err := model.CacheGetChannel(continuationChannelID)
			if err != nil || preferred == nil || preferred.Status != common.ChannelStatusEnabled || (filter != nil && !filter(preferred)) {
				return nil, types.NewErrorWithStatusCode(errors.New("Responses WebSocket continuation channel is unavailable"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
			}
			if usingGroup == "auto" {
				userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
				for _, group := range service.GetUserAutoGroup(userGroup) {
					if model.IsChannelEnabledForGroupModel(group, modelName, preferred.Id) {
						selectGroup = group
						common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
						channel = preferred
						break
					}
				}
			} else if model.IsChannelEnabledForGroupModel(usingGroup, modelName, preferred.Id) {
				channel = preferred
			}
			if channel == nil {
				return nil, types.NewErrorWithStatusCode(errors.New("Responses WebSocket continuation channel no longer serves this model and group"), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
			}
		}
		if channel == nil {
			if preferredChannelID, found := service.GetPreferredChannelByAffinity(c, modelName, usingGroup); found {
				preferred, err := model.CacheGetChannel(preferredChannelID)
				if err == nil && preferred != nil && preferred.Status == common.ChannelStatusEnabled && (filter == nil || filter(preferred)) {
					if usingGroup == "auto" {
						userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
						for _, group := range service.GetUserAutoGroup(userGroup) {
							if model.IsChannelEnabledForGroupModel(group, modelName, preferred.Id) {
								selectGroup = group
								common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
								channel = preferred
								service.MarkChannelAffinityUsed(c, group, preferred.Id)
								break
							}
						}
					} else if model.IsChannelEnabledForGroupModel(usingGroup, modelName, preferred.Id) {
						channel = preferred
						service.MarkChannelAffinityUsed(c, usingGroup, preferred.Id)
					}
				}
			}
		}

		if channel == nil {
			var err error
			channel, selectGroup, err = service.CacheGetRandomSatisfiedChannelFiltered(&service.RetryParam{
				Ctx:        c,
				ModelName:  modelName,
				TokenGroup: usingGroup,
				Retry:      common.GetPointer(0),
			}, filter)
			if err != nil {
				return nil, types.NewErrorWithStatusCode(fmt.Errorf("failed to select channel for group %s and model %s: %w", selectGroup, modelName, err), types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
			}
			if channel == nil {
				return nil, types.NewErrorWithStatusCode(fmt.Errorf("no available channel for group %s and model %s", selectGroup, modelName), types.ErrorCodeModelNotFound, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
			}
		}
	}

	common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
	if apiErr := SetupContextForSelectedChannelWithoutAccount(c, channel, modelName); apiErr != nil {
		return nil, apiErr
	}
	return channel, nil
}

// extractModelNameFromGeminiPath 从 Gemini API URL 路径中提取模型名
// 输入格式: /v1beta/models/gemini-2.0-flash:generateContent
// 输出: gemini-2.0-flash
func extractModelNameFromGeminiPath(path string) string {
	// 查找 "/models/" 的位置
	modelsPrefix := "/models/"
	modelsIndex := strings.Index(path, modelsPrefix)
	if modelsIndex == -1 {
		return ""
	}

	// 从 "/models/" 之后开始提取
	startIndex := modelsIndex + len(modelsPrefix)
	if startIndex >= len(path) {
		return ""
	}

	// 查找 ":" 的位置，模型名在 ":" 之前
	colonIndex := strings.Index(path[startIndex:], ":")
	if colonIndex == -1 {
		// 如果没有找到 ":"，返回从 "/models/" 到路径结尾的部分
		return path[startIndex:]
	}

	// 返回模型名部分
	return path[startIndex : startIndex+colonIndex]
}
