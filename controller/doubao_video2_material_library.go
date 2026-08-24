package controller

import (
	"context"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	doubaorelay "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const doubaoVideo2UserMaterialUploadLimit = service.DoubaoVideo2MaximumMediaSize

type createDoubaoVideo2UserAssetGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type createDoubaoVideo2UserAssetFromURLRequest struct {
	GroupID   string `json:"GroupId"`
	URL       string `json:"URL"`
	Name      string `json:"Name"`
	AssetType string `json:"AssetType"`
}

func ListDoubaoVideo2UserAssetGroups(c *gin.Context) {
	groups, err := model.ListDoubaoVideo2UserAssetGroups(c.GetInt("id"), c.Query("keyword"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, groups)
}

func CreateDoubaoVideo2UserAssetGroup(c *gin.Context) {
	request := createDoubaoVideo2UserAssetGroupRequest{}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	if request.Name == "" || len(request.Name) > 64 || len(request.Description) > 300 {
		common.ApiError(c, errors.New("material group name must be 1-64 characters and description must not exceed 300 characters"))
		return
	}
	channel, err := model.GetEnabledChannelByType(constant.ChannelTypeDoubaoVideo2)
	if err != nil {
		common.ApiError(c, errors.New("no enabled DoubaoVideo2.0 channel is available for material synchronization"))
		return
	}
	client, err := doubaorelay.NewDoubaoVideo2UserMaterialClient(channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := client.CreateAssetGroup(c.Request.Context(), request.Name, request.Description)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	group := &model.DoubaoVideo2UserAssetGroup{
		UserID: c.GetInt("id"), ChannelID: channel.Id, ProviderGroupID: result.ID,
		Name: request.Name, Description: request.Description, GroupType: "AIGC", Status: result.Status,
	}
	if err := model.CreateDoubaoVideo2UserAssetGroup(group); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, group)
}

func ListDoubaoVideo2UserAssets(c *gin.Context) {
	userID := c.GetInt("id")
	_ = syncPendingDoubaoVideo2UserAssets(c.Request.Context(), userID, 20)
	pageInfo := common.GetPageQuery(c)
	groupID, _ := strconv.ParseInt(strings.TrimSpace(c.Query("group_id")), 10, 64)
	assets, total, err := model.ListDoubaoVideo2UserAssets(userID, groupID, c.Query("keyword"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(assets)
	common.ApiSuccess(c, pageInfo)
}

func UploadDoubaoVideo2UserAsset(c *gin.Context) {
	userID := c.GetInt("id")
	groupID, err := strconv.ParseInt(strings.TrimSpace(c.PostForm("group_id")), 10, 64)
	if err != nil || groupID <= 0 {
		common.ApiError(c, errors.New("a valid material group is required"))
		return
	}
	group, err := model.GetDoubaoVideo2UserAssetGroup(groupID, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader == nil {
		common.ApiError(c, errors.New("a multipart file field named file is required"))
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > doubaoVideo2UserMaterialUploadLimit {
		common.ApiError(c, errors.New("the material file is empty or exceeds the upload limit"))
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, doubaoVideo2UserMaterialUploadLimit+1))
	if err != nil || int64(len(data)) > doubaoVideo2UserMaterialUploadLimit {
		common.ApiError(c, errors.New("the material file exceeds the upload limit"))
		return
	}
	contentType := strings.TrimSpace(fileHeader.Header.Get("Content-Type"))
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		contentType = mime.TypeByExtension(filepath.Ext(fileHeader.Filename))
	}
	assetType := doubaoVideo2AssetTypeFromContentType(contentType)
	if assetType == "" {
		common.ApiError(c, errors.New("the material file type is not supported"))
		return
	}
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = strings.TrimSpace(fileHeader.Filename)
	}
	if name == "" || len(name) > 128 {
		common.ApiError(c, errors.New("material name must be between 1 and 128 characters"))
		return
	}
	mediaURL, err := service.StoreDoubaoVideo2Media(c.Request.Context(), data, contentType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	asset, err := createDoubaoVideo2UserAssetFromURL(c.Request.Context(), userID, group, mediaURL, name, assetType)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, asset)
}

func SyncDoubaoVideo2UserAssets(c *gin.Context) {
	if err := syncPendingDoubaoVideo2UserAssets(c.Request.Context(), c.GetInt("id"), 100); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"synced": true})
}

func createDoubaoVideo2UserAssetFromURL(ctx context.Context, userID int, group *model.DoubaoVideo2UserAssetGroup, sourceURL, name, assetType string) (*model.DoubaoVideo2UserAsset, error) {
	if group == nil || group.UserID != userID {
		return nil, gorm.ErrRecordNotFound
	}
	channel, err := model.GetChannelById(group.ChannelID, true)
	if err != nil {
		return nil, err
	}
	client, err := doubaorelay.NewDoubaoVideo2UserMaterialClient(channel)
	if err != nil {
		return nil, err
	}
	assetType = normalizeDoubaoVideo2UserAssetType(assetType)
	if assetType == "" {
		return nil, errors.New("AssetType must be Image, Video, or Audio")
	}
	result, err := client.CreateAsset(ctx, group.ProviderGroupID, sourceURL, name, assetType)
	if err != nil {
		return nil, err
	}
	asset := &model.DoubaoVideo2UserAsset{
		UserID: userID, AssetGroupID: group.ID, ChannelID: group.ChannelID,
		ProviderAssetID: result.ID, Name: strings.TrimSpace(name), AssetType: assetType,
		Status: result.Status, RequestID: result.RequestID, LastSyncedAt: time.Now().Unix(),
	}
	if err := model.CreateDoubaoVideo2UserAsset(asset); err != nil {
		return nil, err
	}
	return asset, nil
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

func syncPendingDoubaoVideo2UserAssets(ctx context.Context, userID, limit int) error {
	assets, err := model.ListPendingDoubaoVideo2UserAssets(userID, limit)
	if err != nil {
		return err
	}
	clients := make(map[int]*doubaorelay.DoubaoVideo2UserMaterialClient)
	for _, asset := range assets {
		client := clients[asset.ChannelID]
		if client == nil {
			channel, channelErr := model.GetChannelById(asset.ChannelID, true)
			if channelErr != nil {
				continue
			}
			client, channelErr = doubaorelay.NewDoubaoVideo2UserMaterialClient(channel)
			if channelErr != nil {
				continue
			}
			clients[asset.ChannelID] = client
		}
		result, syncErr := client.GetAsset(ctx, asset.ProviderAssetID)
		updates := map[string]any{"last_synced_at": time.Now().Unix()}
		if syncErr != nil {
			updates["error_message"] = syncErr.Error()
		} else {
			updates["status"] = result.Status
			updates["request_id"] = result.RequestID
			updates["error_message"] = result.Reason
			if result.Status == model.DoubaoVideo2UserMaterialStatusRejected || result.Status == model.DoubaoVideo2UserMaterialStatusFailed {
				updates["error_code"] = strings.ToLower(result.Status)
			} else {
				updates["error_code"] = ""
			}
		}
		_ = model.UpdateDoubaoVideo2UserAssetStatus(asset.ID, userID, updates)
	}
	return nil
}

func doubaoVideo2AssetTypeFromContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "Image"
	case strings.HasPrefix(contentType, "video/"):
		return "Video"
	case strings.HasPrefix(contentType, "audio/"):
		return "Audio"
	default:
		return ""
	}
}

func getDoubaoVideo2UserAssetForOpenAPI(userID int, providerAssetID string) (*model.DoubaoVideo2UserAsset, error) {
	return model.GetDoubaoVideo2UserAssetByProviderID(providerAssetID, userID)
}
