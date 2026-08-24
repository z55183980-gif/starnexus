package controller

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	doubaorelay "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	doubaoVideo2OpenAPIVersion = "2024-01-01"
	doubaoVideo2OpenAPIRegion  = "cn-beijing"
	doubaoVideo2OpenAPIService = "ark"
	doubaoVideo2OpenAPIMaxBody = 2 << 20
)

type createDoubaoVideo2AccessKeyRequest struct {
	Name string `json:"name"`
}

type updateDoubaoVideo2AccessKeyRequest struct {
	Status int `json:"status"`
}

func ListDoubaoVideo2AccessKeys(c *gin.Context) {
	keys, err := model.ListDoubaoVideo2AccessKeys(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, keys)
}

func CreateDoubaoVideo2AccessKey(c *gin.Context) {
	request := createDoubaoVideo2AccessKeyRequest{}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	keys, err := model.ListDoubaoVideo2AccessKeys(c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(keys) >= 2 {
		common.ApiError(c, errors.New("each user can create at most two Doubao video access keys"))
		return
	}
	created, err := service.CreateDoubaoVideo2AccessKey(c.GetInt("id"), request.Name)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, created)
}

func UpdateDoubaoVideo2AccessKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid access key ID"))
		return
	}
	request := updateDoubaoVideo2AccessKeyRequest{}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Status != model.DoubaoVideo2AccessKeyStatusActive && request.Status != model.DoubaoVideo2AccessKeyStatusDisabled {
		common.ApiError(c, errors.New("invalid access key status"))
		return
	}
	if err := model.UpdateDoubaoVideo2AccessKey(id, c.GetInt("id"), map[string]any{"status": request.Status}); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"updated": true})
}

func DeleteDoubaoVideo2AccessKey(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid access key ID"))
		return
	}
	if err := model.DeleteDoubaoVideo2AccessKey(id, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"deleted": true})
}

// DoubaoVideo2MaterialOpenAPI exposes the Volcengine Action/Version and
// HMAC-SHA256 request shape while keeping provider credentials server-side.
func DoubaoVideo2MaterialOpenAPI(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, doubaoVideo2OpenAPIMaxBody+1))
	if err != nil || len(body) > doubaoVideo2OpenAPIMaxBody {
		writeDoubaoVideo2OpenAPIError(c, http.StatusBadRequest, "InvalidRequest", "request body is invalid or too large")
		return
	}
	key, err := authenticateDoubaoVideo2OpenAPI(c.Request, body)
	if err != nil {
		writeDoubaoVideo2OpenAPIError(c, http.StatusUnauthorized, "InvalidAccessKey", err.Error())
		return
	}
	action := strings.TrimSpace(c.Query("Action"))
	if c.Query("Version") != doubaoVideo2OpenAPIVersion {
		writeDoubaoVideo2OpenAPIError(c, http.StatusBadRequest, "InvalidVersion", "Version must be 2024-01-01")
		return
	}
	result, err := dispatchDoubaoVideo2MaterialOpenAPI(c.Request.Context(), key.UserID, action, body)
	if err != nil {
		writeDoubaoVideo2OpenAPIError(c, http.StatusBadRequest, "OperationFailed", err.Error())
		return
	}
	_ = model.TouchDoubaoVideo2AccessKey(key.ID, time.Now().Unix())
	c.JSON(http.StatusOK, gin.H{
		"ResponseMetadata": gin.H{
			"RequestId": common.GetUUID(), "Action": action, "Version": doubaoVideo2OpenAPIVersion,
			"Service": doubaoVideo2OpenAPIService, "Region": doubaoVideo2OpenAPIRegion,
		},
		"Result": result,
	})
}

func dispatchDoubaoVideo2MaterialOpenAPI(ctx context.Context, userID int, action string, body []byte) (any, error) {
	switch action {
	case "CreateAssetGroup":
		request := createDoubaoVideo2UserAssetGroupRequest{}
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, err
		}
		channel, err := model.GetEnabledChannelByType(constant.ChannelTypeDoubaoVideo2)
		if err != nil {
			return nil, errors.New("no enabled DoubaoVideo2.0 channel is available")
		}
		client, err := doubaorelay.NewDoubaoVideo2UserMaterialClient(channel)
		if err != nil {
			return nil, err
		}
		created, err := client.CreateAssetGroup(ctx, request.Name, request.Description)
		if err != nil {
			return nil, err
		}
		group := &model.DoubaoVideo2UserAssetGroup{
			UserID: userID, ChannelID: channel.Id, ProviderGroupID: created.ID,
			Name: strings.TrimSpace(request.Name), Description: strings.TrimSpace(request.Description),
			GroupType: "AIGC", Status: created.Status,
		}
		if err := model.CreateDoubaoVideo2UserAssetGroup(group); err != nil {
			return nil, err
		}
		return gin.H{"Id": group.ProviderGroupID, "Status": group.Status}, nil
	case "ListAssetGroups":
		groups, err := model.ListDoubaoVideo2UserAssetGroups(userID, "")
		if err != nil {
			return nil, err
		}
		items := make([]gin.H, 0, len(groups))
		for _, group := range groups {
			items = append(items, gin.H{
				"Id": group.ProviderGroupID, "Name": group.Name, "Description": group.Description,
				"GroupType": group.GroupType, "Status": group.Status, "AssetCount": group.AssetCount,
			})
		}
		return gin.H{"Items": items, "TotalCount": len(items)}, nil
	case "CreateAsset":
		request := createDoubaoVideo2UserAssetFromURLRequest{}
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, err
		}
		parsed, err := url.Parse(strings.TrimSpace(request.URL))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return nil, errors.New("URL must be a public HTTP(S) address")
		}
		group, err := model.GetDoubaoVideo2UserAssetGroupByProviderID(request.GroupID, userID)
		if err != nil {
			return nil, err
		}
		asset, err := createDoubaoVideo2UserAssetFromURL(ctx, userID, group, request.URL, request.Name, request.AssetType)
		if err != nil {
			return nil, err
		}
		return gin.H{"Id": asset.ProviderAssetID, "Status": asset.Status}, nil
	case "GetAsset":
		var request struct {
			ID string `json:"Id"`
		}
		if err := common.Unmarshal(body, &request); err != nil {
			return nil, err
		}
		asset, err := getDoubaoVideo2UserAssetForOpenAPI(userID, request.ID)
		if err != nil {
			return nil, err
		}
		_ = syncOneDoubaoVideo2UserAsset(ctx, asset)
		asset, _ = getDoubaoVideo2UserAssetForOpenAPI(userID, request.ID)
		group, groupErr := model.GetDoubaoVideo2UserAssetGroup(asset.AssetGroupID, userID)
		if groupErr != nil {
			return nil, groupErr
		}
		return gin.H{
			"Id": asset.ProviderAssetID, "GroupId": group.ProviderGroupID, "Name": asset.Name,
			"AssetType": asset.AssetType, "Status": asset.Status, "ErrorMessage": asset.ErrorMessage,
		}, nil
	case "ListAssets":
		assets, total, err := model.ListDoubaoVideo2UserAssets(userID, 0, "", 0, 100)
		if err != nil {
			return nil, err
		}
		items := make([]gin.H, 0, len(assets))
		for _, asset := range assets {
			items = append(items, gin.H{
				"Id": asset.ProviderAssetID, "Name": asset.Name, "AssetType": asset.AssetType,
				"Status": asset.Status, "CreatedAt": asset.CreatedAt,
			})
		}
		return gin.H{"Items": items, "TotalCount": total}, nil
	default:
		return nil, fmt.Errorf("unsupported Action %q", action)
	}
}

func authenticateDoubaoVideo2OpenAPI(request *http.Request, body []byte) (*model.DoubaoVideo2AccessKey, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("request URL is required")
	}
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "HMAC-SHA256 ") {
		return nil, errors.New("Authorization must use HMAC-SHA256")
	}
	attributes := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(authorization, "HMAC-SHA256 "), ",") {
		pair := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(pair) == 2 {
			attributes[pair[0]] = pair[1]
		}
	}
	credentialParts := strings.Split(attributes["Credential"], "/")
	if len(credentialParts) != 5 || credentialParts[2] != doubaoVideo2OpenAPIRegion || credentialParts[3] != doubaoVideo2OpenAPIService || credentialParts[4] != "request" {
		return nil, errors.New("Credential scope is invalid")
	}
	key, err := model.GetDoubaoVideo2AccessKeyByAK(credentialParts[0])
	if err != nil || key.Status != model.DoubaoVideo2AccessKeyStatusActive {
		return nil, errors.New("Access Key ID is invalid or disabled")
	}
	xDate := strings.TrimSpace(request.Header.Get("X-Date"))
	requestTime, err := time.Parse("20060102T150405Z", xDate)
	if err != nil || time.Since(requestTime) > 5*time.Minute || time.Until(requestTime) > 5*time.Minute || credentialParts[1] != requestTime.UTC().Format("20060102") {
		return nil, errors.New("X-Date is invalid or expired")
	}
	payloadHash := sha256.Sum256(body)
	payloadHashHex := hex.EncodeToString(payloadHash[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(request.Header.Get("X-Content-Sha256"))), []byte(payloadHashHex)) != 1 {
		return nil, errors.New("X-Content-Sha256 does not match the request body")
	}
	signedHeaders := strings.Split(strings.ToLower(strings.TrimSpace(attributes["SignedHeaders"])), ";")
	if len(signedHeaders) == 0 {
		return nil, errors.New("SignedHeaders is required")
	}
	sortedHeaders := append([]string(nil), signedHeaders...)
	sort.Strings(sortedHeaders)
	if strings.Join(sortedHeaders, ";") != strings.Join(signedHeaders, ";") {
		return nil, errors.New("SignedHeaders must be sorted")
	}
	requiredSignedHeaders := map[string]bool{
		"content-type": false, "host": false, "x-content-sha256": false, "x-date": false,
	}
	for _, headerName := range signedHeaders {
		if _, required := requiredSignedHeaders[strings.TrimSpace(headerName)]; required {
			requiredSignedHeaders[strings.TrimSpace(headerName)] = true
		}
	}
	for headerName, present := range requiredSignedHeaders {
		if !present {
			return nil, fmt.Errorf("SignedHeaders must include %s", headerName)
		}
	}
	canonicalHeaders := strings.Builder{}
	for _, headerName := range signedHeaders {
		headerName = strings.ToLower(strings.TrimSpace(headerName))
		var value string
		if headerName == "host" {
			value = request.Host
		} else {
			value = request.Header.Get(headerName)
		}
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			return nil, fmt.Errorf("signed header %s is missing", headerName)
		}
		canonicalHeaders.WriteString(headerName + ":" + value + "\n")
	}
	canonicalURI := request.URL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalRequest := request.Method + "\n" + canonicalURI + "\n" + request.URL.Query().Encode() + "\n" +
		canonicalHeaders.String() + "\n" + strings.Join(signedHeaders, ";") + "\n" + payloadHashHex
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	credentialScope := strings.Join(credentialParts[1:], "/")
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + credentialScope + "\n" + hex.EncodeToString(canonicalHash[:])
	secret, err := service.DecryptDoubaoVideo2AccessKeySecret(key)
	if err != nil {
		return nil, err
	}
	dateKey := doubaoVideo2OpenAPIHMAC([]byte(secret), credentialParts[1])
	regionKey := doubaoVideo2OpenAPIHMAC(dateKey, credentialParts[2])
	serviceKey := doubaoVideo2OpenAPIHMAC(regionKey, credentialParts[3])
	signingKey := doubaoVideo2OpenAPIHMAC(serviceKey, "request")
	expectedSignature := hex.EncodeToString(doubaoVideo2OpenAPIHMAC(signingKey, stringToSign))
	providedSignature := strings.ToLower(strings.TrimSpace(attributes["Signature"]))
	if subtle.ConstantTimeCompare([]byte(providedSignature), []byte(expectedSignature)) != 1 {
		return nil, errors.New("request signature is invalid")
	}
	return key, nil
}

func doubaoVideo2OpenAPIHMAC(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func syncOneDoubaoVideo2UserAsset(ctx context.Context, asset *model.DoubaoVideo2UserAsset) error {
	if asset == nil {
		return errors.New("asset is required")
	}
	channel, err := model.GetChannelById(asset.ChannelID, true)
	if err != nil {
		return err
	}
	client, err := doubaorelay.NewDoubaoVideo2UserMaterialClient(channel)
	if err != nil {
		return err
	}
	result, err := client.GetAsset(ctx, asset.ProviderAssetID)
	updates := map[string]any{"last_synced_at": time.Now().Unix()}
	if err != nil {
		updates["error_message"] = err.Error()
	} else {
		updates["status"] = result.Status
		updates["request_id"] = result.RequestID
		updates["error_message"] = result.Reason
	}
	if updateErr := model.UpdateDoubaoVideo2UserAssetStatus(asset.ID, asset.UserID, updates); updateErr != nil {
		return updateErr
	}
	return err
}

func writeDoubaoVideo2OpenAPIError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{
		"ResponseMetadata": gin.H{
			"RequestId": common.GetUUID(), "Action": c.Query("Action"), "Version": c.Query("Version"),
			"Service": doubaoVideo2OpenAPIService, "Region": doubaoVideo2OpenAPIRegion,
			"Error": gin.H{"Code": code, "Message": message},
		},
	})
}
