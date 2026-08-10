package doubao

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/service"

	"golang.org/x/sync/singleflight"
)

const (
	zqbapiMaterialVersion       = "2024-01-01"
	zqbapiMaterialService       = "ark"
	zqbapiMaterialDefaultRegion = "cn-beijing"
	zqbapiMaterialWaitTimeout   = 60 * time.Second
	zqbapiAssetCacheTTL         = 24 * time.Hour
)

type zqbapiMaterialConfig struct {
	AccessKeyID   string
	SecretKey     string
	GroupID       string
	ProjectName   string
	Region        string
	ProviderURL   string
	OutboundProxy string
}

type zqbapiMaterialResponseMetadata struct {
	RequestID string `json:"RequestId"`
	Error     *struct {
		Code    any    `json:"Code"`
		Message string `json:"Message"`
	} `json:"Error,omitempty"`
}

type zqbapiCreateAssetResponse struct {
	ResponseMetadata zqbapiMaterialResponseMetadata `json:"ResponseMetadata"`
	Result           struct {
		ID string `json:"Id"`
	} `json:"Result"`
}

type zqbapiGetAssetResponse struct {
	ResponseMetadata zqbapiMaterialResponseMetadata `json:"ResponseMetadata"`
	Result           struct {
		ID     string `json:"Id"`
		Status string `json:"Status"`
		Error  *struct {
			Code    any    `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
	} `json:"Result"`
}

type zqbapiUploadAssetFileResponse struct {
	ResponseMetadata zqbapiMaterialResponseMetadata `json:"ResponseMetadata"`
	Result           struct {
		URL      string `json:"Url"`
		FileName string `json:"FileName"`
	} `json:"Result"`
}

type zqbapiAssetCacheEntry struct {
	AssetID  string
	ExpireAt time.Time
}

var (
	zqbapiAssetCache sync.Map
	zqbapiAssetGroup singleflight.Group
)

func loadZQBAPIMaterialConfig(proxy string) (zqbapiMaterialConfig, error) {
	providerURL := strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_BASE_URL"))
	if providerURL == "" {
		providerURL = constant.ChannelBaseURLs[constant.ChannelTypeZQBAPI]
	}
	config := zqbapiMaterialConfig{
		AccessKeyID:   strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_ACCESS_KEY_ID")),
		SecretKey:     strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_SECRET_ACCESS_KEY")),
		GroupID:       strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_GROUP_ID")),
		ProjectName:   strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_PROJECT_NAME")),
		Region:        strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_REGION")),
		ProviderURL:   strings.TrimRight(providerURL, "/"),
		OutboundProxy: proxy,
	}
	if config.ProjectName == "" {
		config.ProjectName = "default"
	}
	if config.Region == "" {
		config.Region = zqbapiMaterialDefaultRegion
	}
	if config.AccessKeyID == "" || config.SecretKey == "" || config.GroupID == "" {
		return zqbapiMaterialConfig{}, fmt.Errorf("ZQBAPI material SDK is not configured: set ZQBAPI_MATERIAL_ACCESS_KEY_ID, ZQBAPI_MATERIAL_SECRET_ACCESS_KEY and ZQBAPI_MATERIAL_GROUP_ID")
	}
	parsed, err := url.Parse(config.ProviderURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return zqbapiMaterialConfig{}, fmt.Errorf("invalid ZQBAPI provider URL: %q", config.ProviderURL)
	}
	config.ProviderURL = parsed.Scheme + "://" + parsed.Host
	return config, nil
}

func prepareZQBAPIAsset(ctx context.Context, config zqbapiMaterialConfig, source string, inspection *zqbapiImageInspection) (string, error) {
	if inspection == nil || len(inspection.Data) == 0 {
		return "", fmt.Errorf("image data is empty")
	}
	digest := sha256.Sum256(inspection.Data)
	cacheKey := config.GroupID + ":" + hex.EncodeToString(digest[:])
	if cached, ok := zqbapiAssetCache.Load(cacheKey); ok {
		entry := cached.(zqbapiAssetCacheEntry)
		if time.Now().Before(entry.ExpireAt) && entry.AssetID != "" {
			return "asset://" + entry.AssetID, nil
		}
		zqbapiAssetCache.Delete(cacheKey)
	}

	value, err, _ := zqbapiAssetGroup.Do(cacheKey, func() (any, error) {
		if cached, ok := zqbapiAssetCache.Load(cacheKey); ok {
			entry := cached.(zqbapiAssetCacheEntry)
			if time.Now().Before(entry.ExpireAt) && entry.AssetID != "" {
				return entry.AssetID, nil
			}
		}

		materialURL := strings.TrimSpace(source)
		if !strings.HasPrefix(materialURL, "http://") && !strings.HasPrefix(materialURL, "https://") {
			uploadedURL, uploadErr := uploadZQBAPIAssetFile(ctx, config, inspection.Data, inspection.MIMEType, cacheKey)
			if uploadErr != nil {
				return nil, uploadErr
			}
			materialURL = uploadedURL
		}

		assetID, createErr := createZQBAPIAsset(ctx, config, materialURL, "starnexus-"+cacheKey[len(config.GroupID)+1:len(config.GroupID)+13])
		if createErr != nil {
			return nil, createErr
		}
		if waitErr := waitForZQBAPIAsset(ctx, config, assetID); waitErr != nil {
			return nil, waitErr
		}
		zqbapiAssetCache.Store(cacheKey, zqbapiAssetCacheEntry{AssetID: assetID, ExpireAt: time.Now().Add(zqbapiAssetCacheTTL)})
		return assetID, nil
	})
	if err != nil {
		return "", err
	}
	return "asset://" + value.(string), nil
}

func createZQBAPIAsset(ctx context.Context, config zqbapiMaterialConfig, materialURL, name string) (string, error) {
	body, err := common.Marshal(map[string]any{
		"GroupId":     config.GroupID,
		"URL":         materialURL,
		"Name":        name,
		"AssetType":   "Image",
		"ProjectName": config.ProjectName,
	})
	if err != nil {
		return "", err
	}
	responseBody, err := callZQBAPIMaterial(ctx, config, "CreateAsset", "application/json", body)
	if err != nil {
		return "", err
	}
	var response zqbapiCreateAssetResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode CreateAsset response: %w", err)
	}
	if err := zqbapiMetadataError(response.ResponseMetadata); err != nil {
		return "", err
	}
	if response.Result.ID == "" {
		return "", fmt.Errorf("CreateAsset returned an empty asset ID")
	}
	return response.Result.ID, nil
}

func uploadZQBAPIAssetFile(ctx context.Context, config zqbapiMaterialConfig, data []byte, mimeType, cacheKey string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	extension := zqbapiImageExtension(mimeType)
	part, err := writer.CreateFormFile("file", "starnexus-"+cacheKey[len(config.GroupID)+1:len(config.GroupID)+13]+extension)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("AssetType", "Image"); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	responseBody, err := callZQBAPIMaterial(ctx, config, "UploadAssetFile", writer.FormDataContentType(), body.Bytes())
	if err != nil {
		return "", err
	}
	var response zqbapiUploadAssetFileResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode UploadAssetFile response: %w", err)
	}
	if err := zqbapiMetadataError(response.ResponseMetadata); err != nil {
		return "", err
	}
	if response.Result.URL == "" {
		return "", fmt.Errorf("UploadAssetFile returned an empty URL")
	}
	return response.Result.URL, nil
}

func waitForZQBAPIAsset(ctx context.Context, config zqbapiMaterialConfig, assetID string) error {
	waitCtx, cancel := context.WithTimeout(ctx, zqbapiMaterialWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		status, reason, err := getZQBAPIAssetStatus(waitCtx, config, assetID)
		if err != nil {
			return err
		}
		switch strings.ToLower(status) {
		case "active":
			return nil
		case "failed", "error", "rejected":
			return fmt.Errorf("asset %s processing failed: %s", assetID, reason)
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for asset %s to become active: %w", assetID, waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func getZQBAPIAssetStatus(ctx context.Context, config zqbapiMaterialConfig, assetID string) (string, string, error) {
	body, err := common.Marshal(map[string]any{"Id": assetID, "ProjectName": config.ProjectName})
	if err != nil {
		return "", "", err
	}
	responseBody, err := callZQBAPIMaterial(ctx, config, "GetAsset", "application/json", body)
	if err != nil {
		return "", "", err
	}
	var response zqbapiGetAssetResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return "", "", fmt.Errorf("decode GetAsset response: %w", err)
	}
	if err := zqbapiMetadataError(response.ResponseMetadata); err != nil {
		return "", "", err
	}
	reason := ""
	if response.Result.Error != nil {
		reason = response.Result.Error.Message
	}
	return response.Result.Status, reason, nil
}

func callZQBAPIMaterial(ctx context.Context, config zqbapiMaterialConfig, action, contentType string, body []byte) ([]byte, error) {
	parsed, err := url.Parse(config.ProviderURL)
	if err != nil {
		return nil, err
	}
	query := "Action=" + url.QueryEscape(action) + "&Version=" + url.QueryEscape(zqbapiMaterialVersion)
	now := time.Now().UTC()
	xDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	payloadHash := zqbapiSHA256Hex(body)
	signedHeaders := "content-type;host;x-content-sha256;x-date"
	canonicalHeaders := "content-type:" + contentType + "\n" +
		"host:" + parsed.Host + "\n" +
		"x-content-sha256:" + payloadHash + "\n" +
		"x-date:" + xDate + "\n"
	canonicalRequest := "POST\n/\n" + query + "\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash
	credentialScope := shortDate + "/" + config.Region + "/" + zqbapiMaterialService + "/request"
	stringToSign := "HMAC-SHA256\n" + xDate + "\n" + credentialScope + "\n" + zqbapiSHA256Hex([]byte(canonicalRequest))
	dateKey := zqbapiHMAC([]byte(config.SecretKey), shortDate)
	regionKey := zqbapiHMAC(dateKey, config.Region)
	serviceKey := zqbapiHMAC(regionKey, zqbapiMaterialService)
	signingKey := zqbapiHMAC(serviceKey, "request")
	signature := hex.EncodeToString(zqbapiHMAC(signingKey, stringToSign))
	authorization := "HMAC-SHA256 Credential=" + config.AccessKeyID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.ProviderURL+"/?"+query, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Date", xDate)
	request.Header.Set("X-Content-Sha256", payloadHash)
	client, err := service.GetHttpClientWithProxy(config.OutboundProxy)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call ZQBAPI material action %s: %w", action, err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("ZQBAPI material action %s returned HTTP %d: %s", action, response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return responseBody, nil
}

func zqbapiMetadataError(metadata zqbapiMaterialResponseMetadata) error {
	if metadata.Error == nil {
		return nil
	}
	return fmt.Errorf("ZQBAPI material error %v: %s", metadata.Error.Code, metadata.Error.Message)
}

func zqbapiSHA256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func zqbapiHMAC(key []byte, data string) []byte {
	hash := hmac.New(sha256.New, key)
	_, _ = hash.Write([]byte(data))
	return hash.Sum(nil)
}

func zqbapiImageExtension(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0])) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}
