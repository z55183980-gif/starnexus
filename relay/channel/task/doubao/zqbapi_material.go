package doubao

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"golang.org/x/sync/singleflight"
)

const (
	zqbapiAssetTypeImage        = "Image"
	zqbapiAssetTypeVideo        = "Video"
	zqbapiMaterialVersion       = "2024-01-01"
	zqbapiMaterialService       = "ark"
	zqbapiMaterialDefaultRegion = "cn-beijing"
	zqbapiMaterialWaitTimeout   = 60 * time.Second
	zqbapiAssetCacheTTL         = time.Hour
	zqbapiMaterialCallTimeout   = 30 * time.Second
	zqbapiMaterialSafeRetries   = 2
	zqbapiAssetLeaseTTL         = 5 * time.Minute
	zqbapiAssetRegistryWait     = 4 * time.Minute
	zqbapiAssetRegistryTTL      = 365 * 24 * time.Hour
	zqbapiAssetNegativeTTL      = 24 * time.Hour
	zqbapiAssetTransientTTL     = time.Minute
	zqbapiAssetAuthFailureTTL   = 5 * time.Minute
	zqbapiAssetRecheckInterval  = time.Hour
)

type zqbapiMaterialConfig struct {
	ChannelID      int
	AccessKeyID    string
	SecretKey      string
	GroupID        string
	ProjectName    string
	Region         string
	ProviderURL    string
	OutboundProxy  string
	MaterialMode   string
	AssetGroupType string
	AutoNormalize  bool
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

func loadZQBAPIMaterialConfig(setting dto.ChannelSettings) (zqbapiMaterialConfig, error) {
	providerURL := strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_BASE_URL"))
	if providerURL == "" {
		providerURL = constant.ChannelBaseURLs[constant.ChannelTypeZQBAPI]
	}
	config := zqbapiMaterialConfig{
		AccessKeyID:    strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_ACCESS_KEY_ID")),
		SecretKey:      strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_SECRET_ACCESS_KEY")),
		GroupID:        strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_GROUP_ID")),
		ProjectName:    strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_PROJECT_NAME")),
		Region:         strings.TrimSpace(os.Getenv("ZQBAPI_MATERIAL_REGION")),
		ProviderURL:    strings.TrimRight(providerURL, "/"),
		OutboundProxy:  setting.Proxy,
		MaterialMode:   dto.ZQBAPIMaterialModeFacePreflight,
		AssetGroupType: "virtual",
		AutoNormalize:  true,
	}
	if setting.ZQBAPI != nil {
		setting.ZQBAPI.Normalize()
		if setting.ZQBAPI.GroupID != "" {
			config.GroupID = setting.ZQBAPI.GroupID
		}
		if setting.ZQBAPI.ProjectName != "" {
			config.ProjectName = setting.ZQBAPI.ProjectName
		}
		config.MaterialMode = setting.ZQBAPI.MaterialMode
		config.AssetGroupType = setting.ZQBAPI.AssetGroupType
		if setting.ZQBAPI.AutoNormalize != nil {
			config.AutoNormalize = *setting.ZQBAPI.AutoNormalize
		}
	}
	if config.ProjectName == "" {
		config.ProjectName = "default"
	}
	if config.Region == "" {
		config.Region = zqbapiMaterialDefaultRegion
	}
	if config.AccessKeyID == "" || config.SecretKey == "" || config.GroupID == "" {
		return zqbapiMaterialConfig{}, newZQBAPIBuildError(zqbapiErrorMaterialConfig, "config", fmt.Errorf("ZQBAPI material SDK is not configured: set ZQBAPI_MATERIAL_ACCESS_KEY_ID, ZQBAPI_MATERIAL_SECRET_ACCESS_KEY and ZQBAPI_MATERIAL_GROUP_ID"))
	}
	parsed, err := url.Parse(config.ProviderURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return zqbapiMaterialConfig{}, fmt.Errorf("invalid ZQBAPI provider URL: %q", config.ProviderURL)
	}
	config.ProviderURL = parsed.Scheme + "://" + parsed.Host
	return config, nil
}

func ValidateZQBAPIMaterialConfiguration(setting dto.ChannelSettings) error {
	if setting.ZQBAPI != nil {
		setting.ZQBAPI.Normalize()
		if setting.ZQBAPI.MaterialMode == dto.ZQBAPIMaterialModeOff {
			return nil
		}
		if err := setting.ZQBAPI.Validate(); err != nil {
			return err
		}
	}
	_, err := loadZQBAPIMaterialConfig(setting)
	return err
}

func prepareZQBAPIAsset(ctx context.Context, config zqbapiMaterialConfig, source string, inspection *zqbapiImageInspection) (string, error) {
	if inspection == nil || len(inspection.Data) == 0 {
		return "", fmt.Errorf("image data is empty")
	}
	return prepareZQBAPIMediaAsset(ctx, config, source, inspection.Data, inspection.MIMEType, inspection.NormalizationVersion, zqbapiAssetTypeImage)
}

func prepareZQBAPIVideoAsset(ctx context.Context, config zqbapiMaterialConfig, source string, inspection *zqbapiVideoInspection) (string, error) {
	if inspection == nil || len(inspection.Data) == 0 {
		return "", fmt.Errorf("video data is empty")
	}
	return prepareZQBAPIMediaAsset(ctx, config, source, inspection.Data, inspection.MIMEType, inspection.NormalizationVersion, zqbapiAssetTypeVideo)
}

func prepareZQBAPIMediaAsset(ctx context.Context, config zqbapiMaterialConfig, _ string, data []byte, mimeType, normalizationVersion, assetType string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("%s data is empty", strings.ToLower(assetType))
	}
	digest := sha256.Sum256(data)
	mediaDigest := hex.EncodeToString(digest[:])
	cacheKey := zqbapiAssetRegistryKeyForType(config, mediaDigest, normalizationVersion, assetType)
	if cached, ok := zqbapiAssetCache.Load(cacheKey); ok {
		entry := cached.(zqbapiAssetCacheEntry)
		if time.Now().Before(entry.ExpireAt) && entry.AssetID != "" {
			logger.LogDebug(ctx, "ZQBAPI material memory cache hit channel=%d key=%s", config.ChannelID, cacheKey[:12])
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

		leaseOwner, ownerErr := common.GenerateRandomCharsKey(16)
		if ownerErr != nil {
			leaseOwner = fmt.Sprintf("local-%d", time.Now().UnixNano())
		}
		record := &model.ZQBAPIAsset{
			CacheKey:             cacheKey,
			ChannelID:            config.ChannelID,
			ProviderHash:         zqbapiProviderHash(config),
			ProjectName:          config.ProjectName,
			GroupID:              config.GroupID,
			ImageSHA256:          mediaDigest,
			MediaType:            assetType,
			NormalizationVersion: normalizationVersion,
		}
		record, owner, claimErr := model.ClaimZQBAPIAsset(record, leaseOwner, zqbapiAssetLeaseTTL)
		if claimErr != nil {
			return nil, newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_claim", claimErr)
		}
		if !owner {
			assetID, waitErr := waitForZQBAPIRegistryAsset(ctx, config, cacheKey, record)
			if waitErr == nil && assetID != "" {
				logger.LogDebug(ctx, "ZQBAPI material registry reuse channel=%d asset=%s key=%s", config.ChannelID, assetID, cacheKey[:12])
				zqbapiAssetCache.Store(cacheKey, zqbapiAssetCacheEntry{AssetID: assetID, ExpireAt: time.Now().Add(zqbapiAssetCacheTTL)})
				return assetID, nil
			}
			if waitErr != nil {
				return nil, waitErr
			}
			return nil, newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_wait", fmt.Errorf("asset registry lease ended without a result"))
		}

		failOwned := func(err error) error {
			kind, requestID := zqbapiBuildErrorDetails(err)
			ttl := zqbapiAssetNegativeTTL
			if kind == zqbapiErrorMaterialTransient || kind == zqbapiErrorMaterialRateLimit {
				ttl = zqbapiAssetTransientTTL
			} else if kind == zqbapiErrorMaterialAuth {
				ttl = zqbapiAssetAuthFailureTTL
			}
			message := err.Error()
			if len(message) > 1000 {
				message = message[:1000]
			}
			_, updateErr := model.UpdateZQBAPIAssetOwned(record.ID, leaseOwner, map[string]any{
				"status":          model.ZQBAPIAssetStatusFailed,
				"error_code":      string(kind),
				"error_message":   message,
				"request_id":      requestID,
				"lease_owner":     "",
				"lease_until":     0,
				"last_checked_at": time.Now().Unix(),
				"expires_at":      time.Now().Add(ttl).Unix(),
			})
			if updateErr != nil {
				return newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_fail_update", updateErr)
			}
			logger.LogWarn(ctx, fmt.Sprintf("ZQBAPI material preparation failed channel=%d kind=%s asset=%s request_id=%s: %v", config.ChannelID, kind, record.AssetID, requestID, err))
			return err
		}

		// Always upload the validated/normalized bytes. Passing the caller's URL
		// directly would bypass normalization and can fail when the provider cannot
		// reach an expiring or access-controlled URL.
		materialURL, uploadErr := uploadZQBAPIMediaFile(ctx, config, data, mimeType, cacheKey, assetType)
		if uploadErr != nil {
			return nil, failOwned(classifyZQBAPIMaterialError("upload", uploadErr))
		}
		if won, renewErr := model.UpdateZQBAPIAssetOwned(record.ID, leaseOwner, map[string]any{
			"lease_until": time.Now().Add(zqbapiAssetLeaseTTL).Unix(),
		}); renewErr != nil || !won {
			if renewErr == nil {
				renewErr = fmt.Errorf("asset registry lease was lost after upload")
			}
			return nil, newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_lease_renew", renewErr)
		}

		assetID, createErr := createZQBAPIAsset(ctx, config, materialURL, "starnexus-"+mediaDigest[:12], assetType)
		if createErr != nil {
			return nil, failOwned(classifyZQBAPIMaterialError("create", createErr))
		}
		if _, updateErr := model.UpdateZQBAPIAssetOwned(record.ID, leaseOwner, map[string]any{
			"status":          model.ZQBAPIAssetStatusProcessing,
			"asset_id":        assetID,
			"last_checked_at": time.Now().Unix(),
			"lease_until":     time.Now().Add(zqbapiAssetLeaseTTL).Unix(),
		}); updateErr != nil {
			return nil, failOwned(newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_processing_update", updateErr))
		}
		logger.LogInfo(ctx, fmt.Sprintf("ZQBAPI material created channel=%d asset=%s key=%s; waiting for activation", config.ChannelID, assetID, cacheKey[:12]))
		if waitErr := waitForZQBAPIAsset(ctx, config, assetID); waitErr != nil {
			if buildErr, ok := waitErr.(*zqbapiBuildError); ok {
				buildErr.AssetID = assetID
				return nil, failOwned(buildErr)
			}
			return nil, failOwned(&zqbapiBuildError{Kind: zqbapiErrorMaterialRejected, Stage: "wait", AssetID: assetID, Err: waitErr})
		}
		if won, updateErr := model.UpdateZQBAPIAssetOwned(record.ID, leaseOwner, map[string]any{
			"status":          model.ZQBAPIAssetStatusActive,
			"asset_id":        assetID,
			"error_code":      "",
			"error_message":   "",
			"lease_owner":     "",
			"lease_until":     0,
			"last_checked_at": time.Now().Unix(),
			"expires_at":      time.Now().Add(zqbapiAssetRegistryTTL).Unix(),
		}); updateErr != nil || !won {
			if updateErr == nil {
				updateErr = fmt.Errorf("asset registry lease was lost")
			}
			return nil, newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_active_update", updateErr)
		}
		zqbapiAssetCache.Store(cacheKey, zqbapiAssetCacheEntry{AssetID: assetID, ExpireAt: time.Now().Add(zqbapiAssetCacheTTL)})
		logger.LogInfo(ctx, fmt.Sprintf("ZQBAPI material active channel=%d asset=%s key=%s", config.ChannelID, assetID, cacheKey[:12]))
		return assetID, nil
	})
	if err != nil {
		return "", err
	}
	return "asset://" + value.(string), nil
}

func zqbapiProviderHash(config zqbapiMaterialConfig) string {
	return zqbapiSHA256Hex([]byte(fmt.Sprintf("%d|%s|%s|%s|%s|%s", config.ChannelID, config.ProviderURL, config.AccessKeyID, config.ProjectName, config.MaterialMode, config.AssetGroupType)))
}

func zqbapiAssetRegistryKey(config zqbapiMaterialConfig, imageDigest, normalizationVersion string) string {
	return zqbapiSHA256Hex([]byte(strings.Join([]string{
		zqbapiProviderHash(config), config.GroupID, imageDigest, normalizationVersion,
	}, "|")))
}

func zqbapiAssetRegistryKeyForType(config zqbapiMaterialConfig, mediaDigest, normalizationVersion, assetType string) string {
	if assetType == zqbapiAssetTypeImage {
		// Preserve the existing image cache namespace so already-active image
		// assets remain reusable after video support is introduced.
		return zqbapiAssetRegistryKey(config, mediaDigest, normalizationVersion)
	}
	return zqbapiSHA256Hex([]byte(strings.Join([]string{
		zqbapiProviderHash(config), config.GroupID, assetType, mediaDigest, normalizationVersion,
	}, "|")))
}

func zqbapiBuildErrorDetails(err error) (zqbapiErrorKind, string) {
	var buildErr *zqbapiBuildError
	if errors.As(err, &buildErr) {
		return buildErr.Kind, buildErr.RequestID
	}
	return zqbapiErrorMaterialTransient, ""
}

func waitForZQBAPIRegistryAsset(ctx context.Context, config zqbapiMaterialConfig, cacheKey string, initial *model.ZQBAPIAsset) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, zqbapiAssetRegistryWait)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	record := initial
	for {
		if record != nil {
			switch record.Status {
			case model.ZQBAPIAssetStatusActive:
				if record.AssetID == "" {
					return "", newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_wait", fmt.Errorf("active registry record has no asset ID"))
				}
				if record.LastCheckedAt == 0 || time.Now().Unix()-record.LastCheckedAt >= int64(zqbapiAssetRecheckInterval/time.Second) {
					status, reason, err := getZQBAPIAssetStatus(waitCtx, config, record.AssetID)
					if err != nil {
						return "", classifyZQBAPIMaterialError("registry_recheck", err)
					}
					if strings.EqualFold(status, "active") {
						_ = model.UpdateZQBAPIAssetState(record.ID, map[string]any{"last_checked_at": time.Now().Unix()})
						return record.AssetID, nil
					}
					_ = model.UpdateZQBAPIAssetState(record.ID, map[string]any{
						"status": model.ZQBAPIAssetStatusFailed, "error_code": string(zqbapiErrorMaterialRejected),
						"error_message": reason, "lease_until": 0, "expires_at": time.Now().Unix(),
					})
					return "", newZQBAPIBuildError(zqbapiErrorMaterialRejected, "registry_recheck", fmt.Errorf("cached asset is no longer active: %s (%s)", status, reason))
				}
				return record.AssetID, nil
			case model.ZQBAPIAssetStatusFailed:
				kind := zqbapiErrorKind(record.ErrorCode)
				if kind == "" {
					kind = zqbapiErrorMaterialRejected
				}
				return "", &zqbapiBuildError{Kind: kind, Stage: "registry_cached_failure", RequestID: record.RequestID, AssetID: record.AssetID, Err: fmt.Errorf("%s", record.ErrorMessage)}
			}
		}
		select {
		case <-waitCtx.Done():
			return "", newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_wait", waitCtx.Err())
		case <-ticker.C:
			var found bool
			var err error
			record, found, err = model.GetZQBAPIAsset(cacheKey)
			if err != nil {
				return "", newZQBAPIBuildError(zqbapiErrorMaterialTransient, "registry_wait", err)
			}
			if !found {
				record = nil
			}
		}
	}
}

func createZQBAPIAsset(ctx context.Context, config zqbapiMaterialConfig, materialURL, name string, assetType ...string) (string, error) {
	typeName := zqbapiAssetTypeImage
	if len(assetType) > 0 && strings.TrimSpace(assetType[0]) != "" {
		typeName = strings.TrimSpace(assetType[0])
	}
	body, err := common.Marshal(map[string]any{
		"GroupId":     config.GroupID,
		"URL":         materialURL,
		"Name":        name,
		"AssetType":   typeName,
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
	return uploadZQBAPIMediaFile(ctx, config, data, mimeType, cacheKey, zqbapiAssetTypeImage)
}

func uploadZQBAPIMediaFile(ctx context.Context, config zqbapiMaterialConfig, data []byte, mimeType, cacheKey, assetType string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	extension := zqbapiMediaExtension(mimeType)
	fileSuffix := cacheKey
	if len(fileSuffix) > 12 {
		fileSuffix = fileSuffix[:12]
	}
	part, err := writer.CreateFormFile("file", "starnexus-"+fileSuffix+extension)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(data); err != nil {
		return "", err
	}
	if err := writer.WriteField("AssetType", assetType); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	responseBody, err := callZQBAPIMaterialWithRetry(ctx, config, "UploadAssetFile", writer.FormDataContentType(), body.Bytes())
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
	lastStatus := ""
	for {
		status, reason, err := getZQBAPIAssetStatus(waitCtx, config, assetID)
		if err != nil {
			return classifyZQBAPIMaterialError("get_asset", err)
		}
		lastStatus = status
		switch strings.ToLower(status) {
		case "active":
			return nil
		case "failed", "error", "rejected":
			return newZQBAPIBuildError(zqbapiErrorMaterialRejected, "get_asset", fmt.Errorf("asset %s processing failed: %s", assetID, reason))
		}
		select {
		case <-waitCtx.Done():
			return newZQBAPIBuildError(zqbapiErrorMaterialTransient, "get_asset", fmt.Errorf("wait for asset %s to become active (last_status=%s): %w", assetID, lastStatus, waitCtx.Err()))
		case <-ticker.C:
		}
	}
}

func getZQBAPIAssetStatus(ctx context.Context, config zqbapiMaterialConfig, assetID string) (string, string, error) {
	body, err := common.Marshal(map[string]any{"Id": assetID, "ProjectName": config.ProjectName})
	if err != nil {
		return "", "", err
	}
	responseBody, err := callZQBAPIMaterialWithRetry(ctx, config, "GetAsset", "application/json", body)
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

	callCtx, cancel := context.WithTimeout(ctx, zqbapiMaterialCallTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, config.ProviderURL+"/?"+query, bytes.NewReader(body))
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
		return nil, &zqbapiMaterialCallError{Action: action, Err: err}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > 2<<20 {
		return nil, &zqbapiMaterialCallError{Action: action, StatusCode: response.StatusCode, Message: "response exceeds 2MB"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		metadata := zqbapiMaterialResponseMetadata{}
		_ = common.Unmarshal(responseBody, &struct {
			ResponseMetadata *zqbapiMaterialResponseMetadata `json:"ResponseMetadata"`
		}{ResponseMetadata: &metadata})
		callErr := &zqbapiMaterialCallError{Action: action, StatusCode: response.StatusCode, RequestID: metadata.RequestID, Message: strings.TrimSpace(string(responseBody))}
		if metadata.Error != nil {
			callErr.Code = fmt.Sprint(metadata.Error.Code)
			callErr.Message = metadata.Error.Message
		}
		return nil, callErr
	}
	var envelope struct {
		ResponseMetadata zqbapiMaterialResponseMetadata `json:"ResponseMetadata"`
	}
	if err := common.Unmarshal(responseBody, &envelope); err == nil && envelope.ResponseMetadata.Error != nil {
		return nil, &zqbapiMaterialCallError{
			Action: action, StatusCode: response.StatusCode, RequestID: envelope.ResponseMetadata.RequestID,
			Code: fmt.Sprint(envelope.ResponseMetadata.Error.Code), Message: envelope.ResponseMetadata.Error.Message,
		}
	}
	return responseBody, nil
}

func callZQBAPIMaterialWithRetry(ctx context.Context, config zqbapiMaterialConfig, action, contentType string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= zqbapiMaterialSafeRetries; attempt++ {
		responseBody, err := callZQBAPIMaterial(ctx, config, action, contentType, body)
		if err == nil {
			return responseBody, nil
		}
		lastErr = err
		var callErr *zqbapiMaterialCallError
		if !errors.As(err, &callErr) || !callErr.Retryable() || attempt == zqbapiMaterialSafeRetries {
			break
		}
		delay := time.Duration(250*(1<<attempt)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

func zqbapiMetadataError(metadata zqbapiMaterialResponseMetadata) error {
	if metadata.Error == nil {
		return nil
	}
	return &zqbapiMaterialCallError{
		StatusCode: http.StatusOK,
		RequestID:  metadata.RequestID,
		Code:       fmt.Sprint(metadata.Error.Code),
		Message:    metadata.Error.Message,
	}
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
	return zqbapiMediaExtension(mimeType)
}

func zqbapiMediaExtension(mimeType string) string {
	typeName := strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	switch typeName {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "video/mpeg":
		return ".mpeg"
	case "video/x-matroska":
		return ".mkv"
	case "video/mp4":
		return ".mp4"
	default:
		if strings.HasPrefix(typeName, "video/") {
			return ".mp4"
		}
		return ".jpg"
	}
}
