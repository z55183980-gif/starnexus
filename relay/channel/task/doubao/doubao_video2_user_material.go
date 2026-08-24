package doubao

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type DoubaoVideo2UserMaterialClient struct {
	config doubaoVideo2MaterialConfig
}

type DoubaoVideo2UserMaterialResult struct {
	ID        string
	Status    string
	Reason    string
	RequestID string
}

type doubaoVideo2FlexibleID string

func (id *doubaoVideo2FlexibleID) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "" || raw == "null" {
		*id = ""
		return nil
	}
	if strings.HasPrefix(raw, "\"") {
		var value string
		if err := common.Unmarshal(data, &value); err != nil {
			return err
		}
		*id = doubaoVideo2FlexibleID(strings.TrimSpace(value))
		return nil
	}
	*id = doubaoVideo2FlexibleID(raw)
	return nil
}

func NewDoubaoVideo2UserMaterialClient(channel *model.Channel) (*DoubaoVideo2UserMaterialClient, error) {
	if channel == nil || channel.Type != constant.ChannelTypeDoubaoVideo2 {
		return nil, errors.New("an enabled DoubaoVideo2.0 channel is required")
	}
	if channel.Status != common.ChannelStatusEnabled {
		return nil, errors.New("the DoubaoVideo2.0 channel is disabled")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(constant.ChannelBaseURLs[constant.ChannelTypeDoubaoVideo2], "/")
	}
	keys := channel.GetKeys()
	if len(keys) == 0 || strings.TrimSpace(keys[0]) == "" {
		return nil, errors.New("the DoubaoVideo2.0 channel API key is empty")
	}
	return &DoubaoVideo2UserMaterialClient{config: doubaoVideo2MaterialConfig{
		ChannelID: channel.Id, APIKey: strings.TrimSpace(keys[0]), ProviderURL: baseURL,
		OutboundProxy: channel.GetSetting().Proxy,
	}}, nil
}

func (client *DoubaoVideo2UserMaterialClient) CreateAssetGroup(ctx context.Context, name, description string) (*DoubaoVideo2UserMaterialResult, error) {
	if client == nil {
		return nil, errors.New("DoubaoVideo2.0 material client is required")
	}
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || len(name) > 64 || len(description) > 300 {
		return nil, errors.New("material group name or description is invalid")
	}
	body, err := common.Marshal(map[string]any{
		"Name": name, "Description": description, "GroupType": "AIGC",
	})
	if err != nil {
		return nil, err
	}
	responseBody, err := callDoubaoVideo2Material(ctx, client.config, "CreateAssetGroup", body)
	if err != nil {
		return nil, err
	}
	var response struct {
		ResponseMetadata doubaoVideo2MaterialMetadata `json:"ResponseMetadata"`
		Result           struct {
			ID     doubaoVideo2FlexibleID `json:"Id"`
			Status string                 `json:"Status"`
		} `json:"Result"`
	}
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode CreateAssetGroup response: %w", err)
	}
	if err := doubaoVideo2MetadataError(response.ResponseMetadata); err != nil {
		return nil, err
	}
	groupID := strings.TrimSpace(string(response.Result.ID))
	if groupID == "" {
		return nil, errors.New("CreateAssetGroup returned an empty group ID")
	}
	status := strings.TrimSpace(response.Result.Status)
	if status == "" {
		status = model.DoubaoVideo2UserMaterialStatusActive
	}
	return &DoubaoVideo2UserMaterialResult{ID: groupID, Status: status, RequestID: response.ResponseMetadata.RequestID}, nil
}

func (client *DoubaoVideo2UserMaterialClient) CreateAsset(ctx context.Context, groupID, sourceURL, name, assetType string) (*DoubaoVideo2UserMaterialResult, error) {
	if client == nil {
		return nil, errors.New("DoubaoVideo2.0 material client is required")
	}
	groupID = strings.TrimSpace(groupID)
	sourceURL = strings.TrimSpace(sourceURL)
	name = strings.TrimSpace(name)
	assetType = normalizeDoubaoVideo2UserAssetType(assetType)
	if groupID == "" || sourceURL == "" || name == "" || assetType == "" {
		return nil, errors.New("GroupId, URL, Name and AssetType are required")
	}
	body, err := common.Marshal(map[string]any{
		"GroupId": groupID, "URL": sourceURL, "Name": name, "AssetType": assetType,
	})
	if err != nil {
		return nil, err
	}
	responseBody, err := callDoubaoVideo2Material(ctx, client.config, "CreateAsset", body)
	if err != nil {
		return nil, classifyDoubaoVideo2CreateError(err)
	}
	var response struct {
		ResponseMetadata doubaoVideo2MaterialMetadata `json:"ResponseMetadata"`
		Result           struct {
			ID doubaoVideo2FlexibleID `json:"Id"`
		} `json:"Result"`
	}
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode CreateAsset response: %w", err)
	}
	if err := doubaoVideo2MetadataError(response.ResponseMetadata); err != nil {
		return nil, err
	}
	assetID := strings.TrimSpace(string(response.Result.ID))
	if assetID == "" {
		return nil, errors.New("CreateAsset returned an empty asset ID")
	}
	return &DoubaoVideo2UserMaterialResult{
		ID: assetID, Status: model.DoubaoVideo2UserMaterialStatusProcessing,
		RequestID: response.ResponseMetadata.RequestID,
	}, nil
}

func (client *DoubaoVideo2UserMaterialClient) GetAsset(ctx context.Context, assetID string) (*DoubaoVideo2UserMaterialResult, error) {
	if client == nil {
		return nil, errors.New("DoubaoVideo2.0 material client is required")
	}
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		return nil, errors.New("asset ID is required")
	}
	body, err := common.Marshal(map[string]any{"Id": assetID})
	if err != nil {
		return nil, err
	}
	responseBody, err := callDoubaoVideo2MaterialSafe(ctx, client.config, "GetAsset", body)
	if err != nil {
		return nil, err
	}
	var response doubaoVideo2GetAssetResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("decode GetAsset response: %w", err)
	}
	if err := doubaoVideo2MetadataError(response.ResponseMetadata); err != nil {
		return nil, err
	}
	reason := ""
	if response.Result.Error != nil {
		reason = strings.TrimSpace(response.Result.Error.Message)
	}
	return &DoubaoVideo2UserMaterialResult{
		ID: assetID, Status: normalizeDoubaoVideo2UserMaterialStatus(response.Result.Status),
		Reason: reason, RequestID: response.ResponseMetadata.RequestID,
	}, nil
}

func normalizeDoubaoVideo2UserAssetType(assetType string) string {
	switch strings.ToLower(strings.TrimSpace(assetType)) {
	case "image":
		return "Image"
	case "video":
		return "Video"
	case "audio":
		return "Audio"
	default:
		return ""
	}
}

func normalizeDoubaoVideo2UserMaterialStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active", "approved", "success", "succeeded":
		return model.DoubaoVideo2UserMaterialStatusActive
	case "failed", "error":
		return model.DoubaoVideo2UserMaterialStatusFailed
	case "rejected", "denied":
		return model.DoubaoVideo2UserMaterialStatusRejected
	default:
		return model.DoubaoVideo2UserMaterialStatusProcessing
	}
}
