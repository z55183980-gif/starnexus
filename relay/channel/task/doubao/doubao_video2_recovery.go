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

func shouldRecoverDoubaoVideo2FailedTask(task *model.Task, responseBody []byte) bool {
	if task == nil || task.PrivateData.DoubaoVideo2RetryCount > 0 || task.PrivateData.DoubaoVideo2RetryPayload == "" {
		return false
	}
	var failed responseTask
	if err := common.Unmarshal(responseBody, &failed); err != nil {
		return false
	}
	return isDoubaoVideo2RealPersonRejection(failed.Error.Message)
}

func isDoubaoVideo2RealPersonRejection(message string) bool {
	message = strings.ToLower(strings.TrimSpace(message))
	if strings.Contains(message, "may contain real person") {
		return true
	}
	return (strings.Contains(message, "real person") && (strings.Contains(message, "material") || strings.Contains(message, "asset") || strings.Contains(message, "reference"))) ||
		(strings.Contains(message, "真人") && (strings.Contains(message, "素材") || strings.Contains(message, "人脸") || strings.Contains(message, "授权")))
}

func recoverDoubaoVideo2FailedTask(ctx context.Context, channelModel *model.Channel, task *model.Task, responseBody []byte) (*service.TaskFailureRecovery, error) {
	if channelModel == nil || channelModel.Type != constant.ChannelTypeDoubaoVideo2 || task == nil || task.PrivateData.DoubaoVideo2RetryPayload == "" {
		return nil, nil
	}
	var failed responseTask
	if err := common.Unmarshal(responseBody, &failed); err != nil || !isDoubaoVideo2RealPersonRejection(failed.Error.Message) {
		return nil, nil
	}
	var requestBody requestPayload
	if err := common.UnmarshalJsonStr(task.PrivateData.DoubaoVideo2RetryPayload, &requestBody); err != nil {
		return nil, fmt.Errorf("decode DoubaoVideo2.0 retry request: %w", err)
	}
	setting := channelModel.GetSetting()
	baseURL := channelModel.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[constant.ChannelTypeDoubaoVideo2]
	}
	apiKey := channelModel.Key
	if task.PrivateData.Key != "" {
		apiKey = task.PrivateData.Key
	}
	config, err := loadDoubaoVideo2MaterialConfig(channelModel.Id, apiKey, baseURL, setting)
	if err != nil {
		return nil, err
	}
	prepared := 0
	for index := range requestBody.Content {
		item := &requestBody.Content[index]
		if item.Type != "image_url" || item.ImageURL == nil {
			continue
		}
		source := strings.TrimSpace(item.ImageURL.URL)
		if source == "" || strings.HasPrefix(source, "asset://") {
			continue
		}
		if !isPublicDoubaoVideo2MaterialURL(source) {
			return nil, fmt.Errorf("DoubaoVideo2.0 recovery content[%d] is not a public HTTP(S) image URL", index)
		}
		inspection, inspectErr := inspectDoubaoVideo2Image(nil, source)
		if inspectErr != nil {
			return nil, fmt.Errorf("inspect DoubaoVideo2.0 recovery content[%d]: %w", index, inspectErr)
		}
		assetURL, prepareErr := prepareDoubaoVideo2Asset(ctx, config, source, inspection)
		if prepareErr != nil {
			return nil, fmt.Errorf("prepare DoubaoVideo2.0 recovery content[%d]: %w", index, prepareErr)
		}
		item.ImageURL.URL = assetURL
		prepared++
	}
	if prepared == 0 {
		return nil, fmt.Errorf("DoubaoVideo2.0 real-person recovery has no materializable image URL")
	}
	data, err := common.Marshal(&requestBody)
	if err != nil {
		return nil, err
	}
	upstreamTaskID, responseData, err := submitDoubaoVideo2Recovery(ctx, baseURL, apiKey, setting.Proxy, data)
	if err != nil {
		return nil, err
	}
	return &service.TaskFailureRecovery{UpstreamTaskID: upstreamTaskID, TaskData: responseData}, nil
}

func submitDoubaoVideo2Recovery(ctx context.Context, baseURL, apiKey, proxy string, data []byte) (string, []byte, error) {
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
		return "", nil, fmt.Errorf("DoubaoVideo2.0 recovery returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result responsePayload
	if err := common.Unmarshal(responseBody, &result); err != nil {
		return "", nil, fmt.Errorf("decode DoubaoVideo2.0 recovery response: %w", err)
	}
	if result.ID == "" {
		return "", nil, fmt.Errorf("DoubaoVideo2.0 recovery returned an empty task ID")
	}
	return result.ID, responseBody, nil
}
