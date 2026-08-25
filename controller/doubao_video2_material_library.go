package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	doubaorelay "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const doubaoVideo2UserMaterialUploadLimit = service.DoubaoVideo2MaximumMediaSize

type createDoubaoVideo2UserAssetGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type createDoubaoVideo2UserAssetFromURLRequest struct {
	GroupID     string `json:"GroupId"`
	URL         string `json:"URL"`
	Name        string `json:"Name"`
	AssetType   string `json:"AssetType"`
	ProjectName string `json:"ProjectName,omitempty"`
}

type doubaoVideo2UserAssetResponse struct {
	*model.DoubaoVideo2UserAsset
	PreviewURL  string `json:"preview_url,omitempty"`
	ContentType string `json:"content_type,omitempty"`
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

func UpdateDoubaoVideo2UserAssetGroup(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid material group ID"))
		return
	}
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
	userID := c.GetInt("id")
	if err := model.UpdateDoubaoVideo2UserAssetGroup(id, userID, map[string]any{
		"name": request.Name, "description": request.Description,
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	group, err := model.GetDoubaoVideo2UserAssetGroup(id, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, group)
}

func DeleteDoubaoVideo2UserAssetGroup(c *gin.Context) {
	groupID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || groupID <= 0 {
		common.ApiError(c, errors.New("invalid material group ID"))
		return
	}
	deletedAssets, err := deleteDoubaoVideo2UserAssetGroup(c.Request.Context(), c.GetInt("id"), groupID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true, "deleted_assets": deletedAssets})
}

func deleteDoubaoVideo2UserAssetGroup(ctx context.Context, userID int, groupID int64) (int, error) {
	if _, err := model.GetDoubaoVideo2UserAssetGroup(groupID, userID); err != nil {
		return 0, err
	}
	assets, err := model.ListDoubaoVideo2UserAssetsByGroup(userID, groupID)
	if err != nil {
		return 0, err
	}
	deletedAssets := 0
	for _, asset := range assets {
		if err := deleteDoubaoVideo2UserAsset(ctx, userID, asset); err != nil {
			return deletedAssets, fmt.Errorf("delete material %q from group after deleting %d material(s): %w", asset.Name, deletedAssets, err)
		}
		deletedAssets++
	}
	if err := model.DeleteDoubaoVideo2UserAssetGroup(groupID, userID); err != nil {
		return deletedAssets, err
	}
	return deletedAssets, nil
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
	assetIDs := make([]int64, 0, len(assets))
	for _, asset := range assets {
		assetIDs = append(assetIDs, asset.ID)
	}
	media, err := model.ListDoubaoVideo2UserMediaByAssetIDs(assetIDs, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	mediaByAssetID := make(map[int64]*model.DoubaoVideo2UserMedia, len(media))
	for _, item := range media {
		mediaByAssetID[item.AssetID] = item
	}
	items := make([]*doubaoVideo2UserAssetResponse, 0, len(assets))
	for _, asset := range assets {
		item := &doubaoVideo2UserAssetResponse{DoubaoVideo2UserAsset: asset}
		if source := mediaByAssetID[asset.ID]; source != nil {
			item.PreviewURL = fmt.Sprintf("/api/doubao-video/materials/%d/content", asset.ID)
			item.ContentType = source.ContentType
		} else if strings.TrimSpace(asset.SourceURL) != "" {
			item.PreviewURL = fmt.Sprintf("/api/doubao-video/materials/%d/content", asset.ID)
		}
		items = append(items, item)
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetDoubaoVideo2MaterialStorageUsage(c *gin.Context) {
	usage, err := model.GetDoubaoVideo2StorageUsage(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, usage)
}

// ServeDoubaoVideo2MaterialContent is intentionally unauthenticated: the
// upstream API-key account and its preview UI must be able to fetch it. The
// random URL token is the bearer credential and only its hash is stored. The
// response streams the object instead of redirecting because the provider's
// material importer does not reliably follow HTTP redirects.
func ServeDoubaoVideo2MaterialContent(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if extensionIndex := strings.LastIndexByte(token, '.'); extensionIndex > 0 {
		token = token[:extensionIndex]
	}
	if len(token) < 40 || len(token) > 128 {
		c.Status(http.StatusNotFound)
		return
	}
	media, err := model.GetDoubaoVideo2UserMediaByTokenHash(doubaoVideo2MaterialTokenHash(token))
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	mediaURL, err := service.PresignDoubaoVideo2PersistentMaterial(c.Request.Context(), media.ObjectKey)
	if err != nil {
		common.SysError(fmt.Sprintf("presign persistent DoubaoVideo2.0 material %d: %v", media.ID, err))
		c.Status(http.StatusServiceUnavailable)
		return
	}
	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, mediaURL, nil)
	if err != nil {
		c.Status(http.StatusServiceUnavailable)
		return
	}
	if rangeHeader := strings.TrimSpace(c.GetHeader("Range")); rangeHeader != "" {
		request.Header.Set("Range", rangeHeader)
	}
	response, err := (&http.Client{Timeout: 2 * time.Minute}).Do(request)
	if err != nil {
		common.SysError(fmt.Sprintf("read persistent DoubaoVideo2.0 material %d: %v", media.ID, err))
		c.Status(http.StatusServiceUnavailable)
		return
	}
	defer response.Body.Close()
	for _, headerName := range []string{"Accept-Ranges", "Content-Length", "Content-Range", "Content-Type", "ETag", "Last-Modified"} {
		if value := response.Header.Get(headerName); value != "" {
			c.Header(headerName, value)
		}
	}
	c.Header("Cache-Control", "private, no-store")
	c.Status(response.StatusCode)
	_, _ = io.Copy(c.Writer, io.LimitReader(response.Body, service.DoubaoVideo2MaximumMediaSize+1))
}

// ServeDoubaoVideo2UserAssetContent exposes a short-lived preview only after
// the signed-in user has been verified as the owner of the material.
func ServeDoubaoVideo2UserAssetContent(c *gin.Context) {
	assetID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || assetID <= 0 {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	userID := c.GetInt("id")
	media, mediaErr := model.GetDoubaoVideo2UserMediaByAssetID(assetID, userID)
	mediaURL := ""
	if mediaErr == nil {
		mediaURL, err = service.PresignDoubaoVideo2PersistentMaterial(c.Request.Context(), media.ObjectKey)
		if err != nil {
			common.SysError(fmt.Sprintf("presign DoubaoVideo2.0 user material %d: %v", assetID, err))
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
	} else if errors.Is(mediaErr, gorm.ErrRecordNotFound) {
		asset, assetErr := model.GetDoubaoVideo2UserAsset(assetID, userID)
		if assetErr != nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		mediaURL = strings.TrimSpace(asset.SourceURL)
		parsed, parseErr := url.Parse(mediaURL)
		if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
	} else {
		common.SysError(fmt.Sprintf("load DoubaoVideo2.0 user material %d: %v", assetID, mediaErr))
		c.AbortWithStatus(http.StatusServiceUnavailable)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Redirect(http.StatusTemporaryRedirect, mediaURL)
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
	if err := model.ReserveDoubaoVideo2Storage(userID, int64(len(data))); err != nil {
		common.ApiError(c, err)
		return
	}
	reserved := true
	releaseReservation := func() {
		if reserved {
			_ = model.ReleaseDoubaoVideo2Storage(userID, int64(len(data)))
			reserved = false
		}
	}
	objectKey, err := service.StoreDoubaoVideo2PersistentMaterial(c.Request.Context(), userID, data, contentType)
	if err != nil {
		releaseReservation()
		common.ApiError(c, err)
		return
	}
	accessToken, err := common.GenerateRandomCharsKey(48)
	if err != nil {
		_ = service.DeleteDoubaoVideo2PersistentMaterial(c.Request.Context(), objectKey)
		releaseReservation()
		common.ApiError(c, err)
		return
	}
	media := &model.DoubaoVideo2UserMedia{
		UserID: userID, TokenHash: doubaoVideo2MaterialTokenHash(accessToken), ObjectKey: objectKey,
		SizeBytes: int64(len(data)), ContentType: strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])),
	}
	if err := model.CreateDoubaoVideo2UserMedia(media); err != nil {
		_ = service.DeleteDoubaoVideo2PersistentMaterial(c.Request.Context(), objectKey)
		releaseReservation()
		common.ApiError(c, err)
		return
	}
	reserved = false
	upstreamURL, err := service.PresignDoubaoVideo2PersistentMaterial(c.Request.Context(), objectKey)
	if err != nil {
		_ = service.DeleteDoubaoVideo2PersistentMaterial(c.Request.Context(), objectKey)
		_ = model.DeleteDoubaoVideo2UnboundUserMedia(media.ID, userID)
		common.ApiError(c, err)
		return
	}
	asset, err := createDoubaoVideo2UserAssetFromURLWithMedia(c.Request.Context(), userID, group, upstreamURL, name, assetType, media.ID)
	if err != nil {
		if doubaorelay.IsDoubaoVideo2MaterialCreateAmbiguous(err) {
			common.SysError(fmt.Sprintf("retain ambiguous DoubaoVideo2.0 material source media_id=%d user_id=%d: %v", media.ID, userID, err))
			common.ApiError(c, err)
			return
		}
		_ = service.DeleteDoubaoVideo2PersistentMaterial(c.Request.Context(), objectKey)
		_ = model.DeleteDoubaoVideo2UnboundUserMedia(media.ID, userID)
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, asset)
}

func DeleteDoubaoVideo2UserAsset(c *gin.Context) {
	assetID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || assetID <= 0 {
		common.ApiError(c, errors.New("invalid material ID"))
		return
	}
	userID := c.GetInt("id")
	asset, err := model.GetDoubaoVideo2UserAsset(assetID, userID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := deleteDoubaoVideo2UserAsset(c.Request.Context(), userID, asset); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}

func deleteDoubaoVideo2UserAsset(ctx context.Context, userID int, asset *model.DoubaoVideo2UserAsset) error {
	if asset == nil || asset.UserID != userID {
		return gorm.ErrRecordNotFound
	}
	channel, err := model.GetChannelById(asset.ChannelID, true)
	if err != nil {
		return err
	}
	client, err := doubaorelay.NewDoubaoVideo2UserMaterialClient(channel)
	if err != nil {
		return err
	}
	if err := client.DeleteAsset(ctx, asset.ProviderAssetID); err != nil && !doubaorelay.IsDoubaoVideo2MaterialNotFound(err) {
		return err
	}
	media, mediaErr := model.GetDoubaoVideo2UserMediaByAssetID(asset.ID, userID)
	if mediaErr == nil {
		if err := service.DeleteDoubaoVideo2PersistentMaterial(ctx, media.ObjectKey); err != nil {
			return fmt.Errorf("delete material source from R2: %w", err)
		}
	} else if !errors.Is(mediaErr, gorm.ErrRecordNotFound) {
		return mediaErr
	}
	if err := model.DeleteDoubaoVideo2UserAssetAndMedia(asset.ID, userID); err != nil {
		return err
	}
	return nil
}

func SyncDoubaoVideo2UserAssets(c *gin.Context) {
	if err := syncDoubaoVideo2UserAssets(c.Request.Context(), c.GetInt("id"), 100, true); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"synced": true})
}

func createDoubaoVideo2UserAssetFromURL(ctx context.Context, userID int, group *model.DoubaoVideo2UserAssetGroup, sourceURL, name, assetType string) (*model.DoubaoVideo2UserAsset, error) {
	return createDoubaoVideo2UserAssetFromURLWithMedia(ctx, userID, group, sourceURL, name, assetType, 0)
}

func createDoubaoVideo2UserAssetFromURLWithMedia(ctx context.Context, userID int, group *model.DoubaoVideo2UserAssetGroup, sourceURL, name, assetType string, mediaID int64) (*model.DoubaoVideo2UserAsset, error) {
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
	if mediaID == 0 {
		asset.SourceURL = strings.TrimSpace(sourceURL)
	}
	if err := model.CreateDoubaoVideo2UserAssetWithMedia(asset, mediaID); err != nil {
		return nil, err
	}
	return asset, nil
}

func doubaoVideo2MaterialTokenHash(token string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(digest[:])
}

func doubaoVideo2MaterialPublicURL(token, contentType string) (string, error) {
	baseURL := strings.TrimSpace(os.Getenv("DOUBAO_VIDEO2_MATERIAL_PUBLIC_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(system_setting.ServerAddress)
	}
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
		return "", errors.New("DoubaoVideo2.0 material public base URL must be configured as a public HTTPS address")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	extension, ok := doubaoVideo2MaterialFileExtension(contentType)
	if !ok {
		return "", errors.New("DoubaoVideo2.0 material content type is not supported")
	}
	return strings.TrimRight(parsed.String(), "/") + "/api/doubao-video/material-content/" + url.PathEscape(token) + "." + extension, nil
}

func doubaoVideo2MaterialFileExtension(contentType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0])) {
	case "image/jpeg":
		return "jpg", true
	case "image/png":
		return "png", true
	case "image/webp":
		return "webp", true
	case "image/gif":
		return "gif", true
	case "video/mp4":
		return "mp4", true
	case "video/webm":
		return "webm", true
	case "audio/mpeg":
		return "mp3", true
	case "audio/wav", "audio/x-wav":
		return "wav", true
	case "audio/mp4", "audio/x-m4a":
		return "m4a", true
	default:
		return "", false
	}
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
	return syncDoubaoVideo2UserAssets(ctx, userID, limit, false)
}

func syncDoubaoVideo2UserAssets(ctx context.Context, userID, limit int, includeActive bool) error {
	var (
		assets []*model.DoubaoVideo2UserAsset
		err    error
	)
	if includeActive {
		assets, err = model.ListDoubaoVideo2UserAssetsForSync(userID, limit)
	} else {
		assets, err = model.ListPendingDoubaoVideo2UserAssets(userID, limit)
	}
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
		if syncErr != nil && doubaorelay.IsDoubaoVideo2MaterialNotFound(syncErr) {
			media, mediaErr := model.GetDoubaoVideo2UserMediaByAssetID(asset.ID, userID)
			if mediaErr == nil {
				if deleteErr := service.DeleteDoubaoVideo2PersistentMaterial(ctx, media.ObjectKey); deleteErr != nil {
					_ = model.UpdateDoubaoVideo2UserAssetStatus(asset.ID, userID, map[string]any{
						"last_synced_at": time.Now().Unix(), "error_message": deleteErr.Error(),
					})
					continue
				}
			} else if !errors.Is(mediaErr, gorm.ErrRecordNotFound) {
				continue
			}
			_ = model.DeleteDoubaoVideo2UserAssetAndMedia(asset.ID, userID)
			continue
		}
		updates := map[string]any{"last_synced_at": time.Now().Unix()}
		if syncErr != nil {
			updates["error_message"] = syncErr.Error()
		} else {
			updates["status"] = result.Status
			updates["request_id"] = result.RequestID
			updates["error_message"] = result.Reason
			if parsed, parseErr := url.Parse(strings.TrimSpace(result.URL)); parseErr == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") {
				updates["source_url"] = strings.TrimSpace(result.URL)
			}
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
