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
	"github.com/QuantumNous/new-api/service"
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

	preparedCount := 0
	for index := range requestBody.Content {
		item := &requestBody.Content[index]
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		source := strings.TrimSpace(item.ImageURL.URL)
		if source == "" || strings.HasPrefix(source, "asset://") {
			continue
		}
		if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
			return nil, fmt.Errorf("ZQBAPI retry supports only public image URLs")
		}
		inspection, err := inspectZQBAPIImage(nil, source)
		if err != nil {
			return nil, fmt.Errorf("load ZQBAPI retry image content[%d]: %w", index, err)
		}
		assetURL, err := prepareZQBAPIAsset(ctx, config, source, inspection)
		if err != nil {
			return nil, fmt.Errorf("prepare ZQBAPI retry material content[%d]: %w", index, err)
		}
		item.ImageURL.URL = assetURL
		preparedCount++
	}
	if preparedCount == 0 {
		return nil, fmt.Errorf("ZQBAPI real-person retry has no materializable image URL")
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
