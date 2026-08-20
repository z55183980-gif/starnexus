package doubao

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const zqbapiRealPersonErrorFragment = "may contain real person"

func (a *TaskAdaptor) ShouldRecoverFailedTask(task *model.Task, responseBody []byte) bool {
	switch a.ChannelType {
	case constant.ChannelTypeZQBAPI:
		return shouldRecoverZQBAPIFailedTask(task, responseBody)
	case constant.ChannelTypeDoubaoVideo2:
		return shouldRecoverDoubaoVideo2FailedTask(task, responseBody)
	default:
		return false
	}
}

// ShouldRecoverTaskSubmitFailure covers providers that reject the initial
// POST (rather than returning a terminal polling status). It is deliberately
// limited to requests that left a sanitized, public-media retry payload in
// the request context, so a failed inline upload is never persisted or retried.
func (a *TaskAdaptor) ShouldRecoverTaskSubmitFailure(c *gin.Context, info *relaycommon.RelayInfo, responseBody []byte) bool {
	if a == nil || a.ChannelType != constant.ChannelTypeZQBAPI || c == nil || info == nil {
		return false
	}
	if !isZQBAPINativeSeedanceRequest(c) && !isZQBAPIOpenAIVideoRequest(c) {
		return false
	}
	if !strings.Contains(strings.ToLower(string(responseBody)), zqbapiRealPersonErrorFragment) {
		return false
	}
	payload, ok := c.Get(string(constant.ContextKeyZQBAPIRetryPayload))
	return ok && len(contextPayloadBytes(payload)) > 0
}

// RecoverTaskSubmitFailure materializes image/video references into the
// configured approved group and rebuilds one native-compatible request body.
// The generic relay calls this at most once for the initial POST.
func (a *TaskAdaptor) RecoverTaskSubmitFailure(c *gin.Context, info *relaycommon.RelayInfo, responseBody []byte) (io.Reader, error) {
	if !a.ShouldRecoverTaskSubmitFailure(c, info, responseBody) {
		return nil, nil
	}
	payloadBytes := contextPayloadBytes(c.MustGet(string(constant.ContextKeyZQBAPIRetryPayload)))
	var requestBody requestPayload
	if err := common.Unmarshal(payloadBytes, &requestBody); err != nil {
		return nil, fmt.Errorf("decode ZQBAPI submit retry request: %w", err)
	}
	config, err := loadZQBAPIMaterialConfig(info.ChannelSetting)
	if err != nil {
		return nil, err
	}
	config.ChannelID = info.ChannelId
	preparedCount, err := materializeZQBAPIRequest(c.Request.Context(), c, config, &requestBody)
	if err != nil {
		return nil, err
	}
	if preparedCount == 0 {
		return nil, fmt.Errorf("ZQBAPI submit retry has no materializable image/video URL")
	}

	var data []byte
	if nativeRequest, nativeOK := getZQBAPINativeSeedanceRequest(c); nativeOK {
		data, err = mergeZQBAPINativeSeedancePayload(nativeRequest.Raw, &requestBody, false, true)
	} else {
		data, err = common.Marshal(&requestBody)
	}
	if err != nil {
		return nil, fmt.Errorf("marshal ZQBAPI submit retry request: %w", err)
	}
	// Do not allow the eventual task row to retain a retry payload after the
	// materialized request has been accepted; polling recovery is independent.
	c.Set(string(constant.ContextKeyZQBAPIRetryPayload), nil)
	return bytes.NewReader(data), nil
}

func contextPayloadBytes(value any) []byte {
	switch payload := value.(type) {
	case []byte:
		return payload
	case string:
		return []byte(payload)
	default:
		return nil
	}
}

func shouldRecoverZQBAPIFailedTask(task *model.Task, responseBody []byte) bool {
	if task == nil || task.PrivateData.ZQBAPIRetryCount > 0 || task.PrivateData.ZQBAPIRetryPayload == "" {
		return false
	}
	var failed responseTask
	if err := common.Unmarshal(responseBody, &failed); err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(failed.Error.Message), zqbapiRealPersonErrorFragment)
}

func (a *TaskAdaptor) RecoverFailedTask(ctx context.Context, channelModel *model.Channel, task *model.Task, responseBody []byte) (*service.TaskFailureRecovery, error) {
	if channelModel != nil && channelModel.Type == constant.ChannelTypeDoubaoVideo2 {
		return recoverDoubaoVideo2FailedTask(ctx, channelModel, task, responseBody)
	}
	if channelModel == nil || channelModel.Type != constant.ChannelTypeZQBAPI || task == nil {
		return nil, nil
	}
	if task.PrivateData.ZQBAPIRetryPayload == "" {
		return nil, nil
	}
	var failed responseTask
	if err := common.Unmarshal(responseBody, &failed); err != nil {
		return nil, nil
	}
	if !strings.Contains(strings.ToLower(failed.Error.Message), zqbapiRealPersonErrorFragment) {
		return nil, nil
	}

	var requestBody requestPayload
	if err := common.UnmarshalJsonStr(task.PrivateData.ZQBAPIRetryPayload, &requestBody); err != nil {
		return nil, fmt.Errorf("decode ZQBAPI retry request: %w", err)
	}
	baseURL := channelModel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeZQBAPI]
	}
	config, err := loadZQBAPIMaterialConfig(channelModel.GetSetting())
	if err != nil {
		return nil, err
	}
	config.ChannelID = channelModel.Id

	preparedCount, err := materializeZQBAPIRequest(ctx, nil, config, &requestBody)
	if err != nil {
		return nil, err
	}
	if preparedCount == 0 {
		return nil, fmt.Errorf("ZQBAPI real-person retry has no materializable image/video URL")
	}

	data, err := common.Marshal(&requestBody)
	if err != nil {
		return nil, err
	}
	modelAPIKey := channelModel.Key
	if task.PrivateData.Key != "" {
		modelAPIKey = task.PrivateData.Key
	}
	upstreamTaskID, responseData, err := submitZQBAPIRetry(ctx, baseURL, modelAPIKey, channelModel.GetSetting().Proxy, data)
	if err != nil {
		return nil, err
	}
	return &service.TaskFailureRecovery{UpstreamTaskID: upstreamTaskID, TaskData: responseData}, nil
}

func materializeZQBAPIRequest(ctx context.Context, ginContext *gin.Context, config zqbapiMaterialConfig, body *requestPayload) (int, error) {
	if body == nil {
		return 0, nil
	}
	preparedCount := 0
	for index := range body.Content {
		item := &body.Content[index]
		switch item.Type {
		case "image_url":
			if item.ImageURL == nil {
				continue
			}
			source := strings.TrimSpace(item.ImageURL.URL)
			if source == "" || strings.HasPrefix(source, "asset://") {
				continue
			}
			if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
				return 0, fmt.Errorf("ZQBAPI materialization supports only public image/video URLs")
			}
			inspection, err := inspectZQBAPIImage(ginContext, source)
			if err != nil {
				return 0, fmt.Errorf("load ZQBAPI image content[%d]: %w", index, err)
			}
			assetURL, err := prepareZQBAPIAsset(ctx, config, source, inspection)
			if err != nil {
				return 0, fmt.Errorf("prepare ZQBAPI image material content[%d]: %w", index, err)
			}
			item.ImageURL.URL = assetURL
			preparedCount++
		case "video_url":
			if item.VideoURL == nil {
				continue
			}
			source := strings.TrimSpace(item.VideoURL.URL)
			if source == "" || strings.HasPrefix(source, "asset://") {
				continue
			}
			if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
				return 0, fmt.Errorf("ZQBAPI materialization supports only public image/video URLs")
			}
			inspection, err := inspectZQBAPIVideo(ginContext, source)
			if err != nil {
				return 0, fmt.Errorf("load ZQBAPI video content[%d]: %w", index, err)
			}
			assetURL, err := prepareZQBAPIVideoAsset(ctx, config, source, inspection)
			if err != nil {
				return 0, fmt.Errorf("prepare ZQBAPI video material content[%d]: %w", index, err)
			}
			item.VideoURL.URL = assetURL
			preparedCount++
		}
	}
	return preparedCount, nil
}

func submitZQBAPIRetry(ctx context.Context, baseURL, apiKey, proxy string, data []byte) (string, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/v3/contents/generations/tasks", bytes.NewReader(data))
	if err != nil {
		return "", nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return "", nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", nil, fmt.Errorf("ZQBAPI retry returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result responsePayload
	if err := common.Unmarshal(responseBody, &result); err != nil {
		return "", nil, fmt.Errorf("decode ZQBAPI retry response: %w", err)
	}
	if result.ID == "" {
		return "", nil, fmt.Errorf("ZQBAPI retry returned an empty task ID")
	}
	return result.ID, responseBody, nil
}
