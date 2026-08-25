package doubao

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"golang.org/x/sync/singleflight"
)

const (
	doubaoVideo2MaterialVersion      = "2024-01-01"
	doubaoVideo2MaterialCallTimeout  = 30 * time.Second
	doubaoVideo2MaterialWaitTimeout  = 2 * time.Minute
	doubaoVideo2AssetLeaseTTL        = 5 * time.Minute
	doubaoVideo2AssetRegistryWait    = 4 * time.Minute
	doubaoVideo2AssetRegistryTTL     = 365 * 24 * time.Hour
	doubaoVideo2AssetCacheTTL        = time.Hour
	doubaoVideo2AssetNegativeTTL     = 24 * time.Hour
	doubaoVideo2AssetTransientTTL    = time.Minute
	doubaoVideo2AssetAuthFailureTTL  = 5 * time.Minute
	doubaoVideo2AssetRecheckInterval = time.Hour
)

type doubaoVideo2ErrorKind string

const (
	doubaoVideo2ErrorInvalidImage      doubaoVideo2ErrorKind = "doubao_video2_invalid_image"
	doubaoVideo2ErrorMaterialRejected  doubaoVideo2ErrorKind = "doubao_video2_material_rejected"
	doubaoVideo2ErrorMaterialAuth      doubaoVideo2ErrorKind = "doubao_video2_material_auth_failed"
	doubaoVideo2ErrorMaterialRateLimit doubaoVideo2ErrorKind = "doubao_video2_material_rate_limited"
	doubaoVideo2ErrorMaterialTransient doubaoVideo2ErrorKind = "doubao_video2_material_transient"
	doubaoVideo2ErrorMaterialAmbiguous doubaoVideo2ErrorKind = "doubao_video2_material_create_ambiguous"
	doubaoVideo2ErrorMaterialConfig    doubaoVideo2ErrorKind = "doubao_video2_material_not_configured"
	doubaoVideo2ErrorRequestTooLarge   doubaoVideo2ErrorKind = "doubao_video2_request_body_too_large"
	doubaoVideo2ErrorTemporaryMedia    doubaoVideo2ErrorKind = "doubao_video2_temporary_media_failed"
)

type doubaoVideo2BuildError struct {
	Kind      doubaoVideo2ErrorKind
	Stage     string
	RequestID string
	AssetID   string
	Err       error
}

func (e *doubaoVideo2BuildError) Error() string {
	if e == nil {
		return "DoubaoVideo2.0 material error"
	}
	message := "DoubaoVideo2.0 " + e.Stage
	if e.RequestID != "" {
		message += " (request_id=" + e.RequestID + ")"
	}
	if e.AssetID != "" {
		message += " (asset_id=" + e.AssetID + ")"
	}
	if e.Err != nil {
		message += ": " + e.Err.Error()
	}
	return strings.TrimSpace(message)
}
func (e *doubaoVideo2BuildError) Unwrap() error         { return e.Err }
func (e *doubaoVideo2BuildError) TaskErrorCode() string { return string(e.Kind) }
func (e *doubaoVideo2BuildError) TaskLocalError() bool  { return true }
func (e *doubaoVideo2BuildError) Temporary() bool {
	return e != nil && (e.Kind == doubaoVideo2ErrorMaterialTransient || e.Kind == doubaoVideo2ErrorMaterialRateLimit)
}
func (e *doubaoVideo2BuildError) TaskHTTPStatus() int {
	if e == nil {
		return http.StatusBadGateway
	}
	switch e.Kind {
	case doubaoVideo2ErrorRequestTooLarge:
		return http.StatusRequestEntityTooLarge
	case doubaoVideo2ErrorTemporaryMedia:
		return http.StatusBadGateway
	case doubaoVideo2ErrorInvalidImage, doubaoVideo2ErrorMaterialRejected:
		return http.StatusUnprocessableEntity
	case doubaoVideo2ErrorMaterialAmbiguous:
		return http.StatusBadGateway
	case doubaoVideo2ErrorMaterialRateLimit:
		return http.StatusTooManyRequests
	case doubaoVideo2ErrorMaterialConfig:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadGateway
	}
}

type doubaoVideo2MaterialCallError struct {
	Action     string
	StatusCode int
	RequestID  string
	Code       string
	Message    string
	Err        error
}

func (e *doubaoVideo2MaterialCallError) Error() string {
	return fmt.Sprintf("action=%s status=%d code=%s request_id=%s: %s", e.Action, e.StatusCode, e.Code, e.RequestID, strings.TrimSpace(e.Message))
}
func (e *doubaoVideo2MaterialCallError) Unwrap() error { return e.Err }
func (e *doubaoVideo2MaterialCallError) retryable() bool {
	text := strings.ToLower(e.Code + " " + e.Message)
	return (e.Err != nil && e.StatusCode == 0) || e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500 || strings.Contains(text, "throttl") || strings.Contains(text, "timeout")
}

type doubaoVideo2MaterialConfig struct {
	ChannelID     int
	APIKey        string
	GroupID       string
	ProviderURL   string
	OutboundProxy string
	MaterialMode  string
}

type doubaoVideo2MaterialMetadata struct {
	RequestID string `json:"RequestId"`
	Error     *struct {
		Code    any    `json:"Code"`
		Message string `json:"Message"`
	} `json:"Error,omitempty"`
}
type doubaoVideo2CreateAssetResponse struct {
	ResponseMetadata doubaoVideo2MaterialMetadata `json:"ResponseMetadata"`
	Result           struct {
		ID string `json:"Id"`
	} `json:"Result"`
}
type doubaoVideo2GetAssetResponse struct {
	ResponseMetadata doubaoVideo2MaterialMetadata `json:"ResponseMetadata"`
	Result           struct {
		ID     string `json:"Id"`
		Status string `json:"Status"`
		URL    string `json:"URL"`
		Error  *struct {
			Code    any    `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error,omitempty"`
	} `json:"Result"`
}
type doubaoVideo2AssetCacheEntry struct {
	AssetID  string
	ExpireAt time.Time
}

var doubaoVideo2AssetCache sync.Map
var doubaoVideo2AssetGroup singleflight.Group

func loadDoubaoVideo2MaterialConfig(channelID int, apiKey, baseURL string, setting dto.ChannelSettings) (doubaoVideo2MaterialConfig, error) {
	if setting.DoubaoVideo2 == nil {
		return doubaoVideo2MaterialConfig{}, &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialConfig, Stage: "config", Err: errors.New("material mode is not configured")}
	}
	setting.DoubaoVideo2.Normalize()
	if err := setting.DoubaoVideo2.Validate(); err != nil {
		return doubaoVideo2MaterialConfig{}, &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialConfig, Stage: "config", Err: err}
	}
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return doubaoVideo2MaterialConfig{}, &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialConfig, Stage: "config", Err: errors.New("invalid channel base URL")}
	}
	if strings.TrimSpace(apiKey) == "" {
		return doubaoVideo2MaterialConfig{}, &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialConfig, Stage: "config", Err: errors.New("channel ApiKey is empty")}
	}
	return doubaoVideo2MaterialConfig{
		ChannelID: channelID, APIKey: strings.TrimSpace(apiKey), GroupID: setting.DoubaoVideo2.GroupID,
		ProviderURL: strings.TrimRight(parsed.String(), "/"), OutboundProxy: setting.Proxy,
		MaterialMode: setting.DoubaoVideo2.MaterialMode,
	}, nil
}

func prepareDoubaoVideo2Asset(ctx context.Context, config doubaoVideo2MaterialConfig, sourceURL string, inspection *zqbapiImageInspection) (string, error) {
	if inspection == nil || len(inspection.Data) == 0 {
		return "", &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorInvalidImage, Stage: "inspect", Err: errors.New("image data is empty")}
	}
	digest := sha256.Sum256(inspection.Data)
	imageDigest := hex.EncodeToString(digest[:])
	providerHash := doubaoVideo2SHA256(fmt.Sprintf("%d|%s|%s", config.ChannelID, config.ProviderURL, config.APIKey))
	cacheKey := doubaoVideo2SHA256(strings.Join([]string{providerHash, config.GroupID, imageDigest, inspection.NormalizationVersion}, "|"))
	if cached, ok := doubaoVideo2AssetCache.Load(cacheKey); ok {
		entry := cached.(doubaoVideo2AssetCacheEntry)
		if time.Now().Before(entry.ExpireAt) && entry.AssetID != "" {
			return "asset://" + entry.AssetID, nil
		}
		doubaoVideo2AssetCache.Delete(cacheKey)
	}
	value, err, _ := doubaoVideo2AssetGroup.Do(cacheKey, func() (any, error) {
		owner, randomErr := common.GenerateRandomCharsKey(16)
		if randomErr != nil {
			owner = fmt.Sprintf("local-%d", time.Now().UnixNano())
		}
		record, claimed, claimErr := model.ClaimDoubaoVideo2Asset(&model.DoubaoVideo2Asset{
			CacheKey: cacheKey, ChannelID: config.ChannelID, ProviderHash: providerHash, GroupID: config.GroupID,
			ImageSHA256: imageDigest, NormalizationVersion: inspection.NormalizationVersion,
		}, owner, doubaoVideo2AssetLeaseTTL)
		if claimErr != nil {
			return nil, doubaoVideo2WrapError("registry_claim", claimErr)
		}
		if !claimed {
			return waitForDoubaoVideo2RegistryAsset(ctx, config, cacheKey, record)
		}
		fail := func(failure error) (any, error) {
			buildErr := doubaoVideo2WrapError("prepare", failure)
			// Once CreateAsset returned an ID, a transient GetAsset failure must
			// keep that asset in PROCESSING. Another node/request can safely poll
			// the same ID instead of creating a duplicate asset.
			if record.AssetID != "" && buildErr.Temporary() {
				_, _ = model.UpdateDoubaoVideo2AssetOwned(record.ID, owner, map[string]any{
					"status": model.DoubaoVideo2AssetStatusProcessing, "asset_id": record.AssetID,
					"error_code": string(buildErr.Kind), "error_message": buildErr.Error(),
					"request_id": buildErr.RequestID, "lease_owner": "", "lease_until": 0, "last_checked_at": time.Now().Unix(),
				})
				return nil, buildErr
			}
			ttl := doubaoVideo2AssetNegativeTTL
			if buildErr.Temporary() {
				ttl = doubaoVideo2AssetTransientTTL
			}
			if buildErr.Kind == doubaoVideo2ErrorMaterialAuth {
				ttl = doubaoVideo2AssetAuthFailureTTL
			}
			message := buildErr.Error()
			if len(message) > 1000 {
				message = message[:1000]
			}
			_, _ = model.UpdateDoubaoVideo2AssetOwned(record.ID, owner, map[string]any{
				"status": model.DoubaoVideo2AssetStatusFailed, "error_code": string(buildErr.Kind), "error_message": message,
				"request_id": buildErr.RequestID, "lease_owner": "", "lease_until": 0, "last_checked_at": time.Now().Unix(), "expires_at": time.Now().Add(ttl).Unix(),
			})
			return nil, buildErr
		}
		assetID, createErr := createDoubaoVideo2Asset(ctx, config, sourceURL, "starnexus-"+imageDigest[:12])
		if createErr != nil {
			return fail(classifyDoubaoVideo2CreateError(createErr))
		}
		record.AssetID = assetID
		record.Status = model.DoubaoVideo2AssetStatusProcessing
		if won, updateErr := model.UpdateDoubaoVideo2AssetOwned(record.ID, owner, map[string]any{
			"status": model.DoubaoVideo2AssetStatusProcessing, "asset_id": assetID, "last_checked_at": time.Now().Unix(), "lease_until": time.Now().Add(doubaoVideo2AssetLeaseTTL).Unix(),
		}); updateErr != nil || !won {
			return nil, &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialAmbiguous, Stage: "registry_processing_update", AssetID: assetID, Err: errors.New("asset was created but its registry state could not be persisted; CreateAsset will not be replayed")}
		}
		logger.LogInfo(ctx, fmt.Sprintf("DoubaoVideo2.0 material created channel=%d asset=%s key=%s", config.ChannelID, assetID, cacheKey[:12]))
		if waitErr := waitForDoubaoVideo2Asset(ctx, config, assetID); waitErr != nil {
			return fail(waitErr)
		}
		if won, updateErr := model.UpdateDoubaoVideo2AssetOwned(record.ID, owner, map[string]any{
			"status": model.DoubaoVideo2AssetStatusActive, "asset_id": assetID, "error_code": "", "error_message": "",
			"lease_owner": "", "lease_until": 0, "last_checked_at": time.Now().Unix(), "expires_at": time.Now().Add(doubaoVideo2AssetRegistryTTL).Unix(),
		}); updateErr != nil || !won {
			return nil, doubaoVideo2WrapError("registry_active_update", updateErr)
		}
		doubaoVideo2AssetCache.Store(cacheKey, doubaoVideo2AssetCacheEntry{AssetID: assetID, ExpireAt: time.Now().Add(doubaoVideo2AssetCacheTTL)})
		return assetID, nil
	})
	if err != nil {
		return "", err
	}
	return "asset://" + value.(string), nil
}

func waitForDoubaoVideo2RegistryAsset(ctx context.Context, config doubaoVideo2MaterialConfig, cacheKey string, record *model.DoubaoVideo2Asset) (string, error) {
	waitCtx, cancel := context.WithTimeout(ctx, doubaoVideo2AssetRegistryWait)
	defer cancel()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if record != nil {
			switch record.Status {
			case model.DoubaoVideo2AssetStatusActive:
				if record.AssetID == "" {
					return "", doubaoVideo2WrapError("registry_wait", errors.New("active record has no asset ID"))
				}
				if record.LastCheckedAt == 0 || time.Now().Unix()-record.LastCheckedAt >= int64(doubaoVideo2AssetRecheckInterval/time.Second) {
					status, reason, err := getDoubaoVideo2AssetStatus(waitCtx, config, record.AssetID)
					if err != nil {
						return "", err
					}
					if !strings.EqualFold(status, "active") {
						_ = model.UpdateDoubaoVideo2AssetState(record.ID, map[string]any{
							"status": model.DoubaoVideo2AssetStatusFailed, "error_code": string(doubaoVideo2ErrorMaterialRejected),
							"error_message": reason, "lease_until": 0, "expires_at": time.Now().Unix(),
						})
						return "", &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialRejected, Stage: "registry_recheck", AssetID: record.AssetID, Err: fmt.Errorf("status=%s: %s", status, reason)}
					}
					_ = model.UpdateDoubaoVideo2AssetState(record.ID, map[string]any{"last_checked_at": time.Now().Unix()})
				}
				return record.AssetID, nil
			case model.DoubaoVideo2AssetStatusFailed:
				kind := doubaoVideo2ErrorKind(record.ErrorCode)
				if kind == "" {
					kind = doubaoVideo2ErrorMaterialRejected
				}
				return "", &doubaoVideo2BuildError{Kind: kind, Stage: "registry_cached_failure", RequestID: record.RequestID, AssetID: record.AssetID, Err: errors.New(record.ErrorMessage)}
			case model.DoubaoVideo2AssetStatusProcessing:
				if record.AssetID == "" {
					break
				}
				status, reason, err := getDoubaoVideo2AssetStatus(waitCtx, config, record.AssetID)
				if err != nil {
					return "", err
				}
				switch strings.ToLower(status) {
				case "active":
					_ = model.UpdateDoubaoVideo2AssetState(record.ID, map[string]any{
						"status": model.DoubaoVideo2AssetStatusActive, "error_code": "", "error_message": "",
						"lease_owner": "", "lease_until": 0, "last_checked_at": time.Now().Unix(), "expires_at": time.Now().Add(doubaoVideo2AssetRegistryTTL).Unix(),
					})
					return record.AssetID, nil
				case "failed", "error", "rejected":
					_ = model.UpdateDoubaoVideo2AssetState(record.ID, map[string]any{
						"status": model.DoubaoVideo2AssetStatusFailed, "error_code": string(doubaoVideo2ErrorMaterialRejected),
						"error_message": reason, "lease_owner": "", "lease_until": 0, "last_checked_at": time.Now().Unix(), "expires_at": time.Now().Add(doubaoVideo2AssetNegativeTTL).Unix(),
					})
					return "", &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialRejected, Stage: "registry_processing", AssetID: record.AssetID, Err: errors.New(reason)}
				}
			}
		}
		select {
		case <-waitCtx.Done():
			return "", doubaoVideo2WrapError("registry_wait", waitCtx.Err())
		case <-ticker.C:
			var found bool
			var err error
			record, found, err = model.GetDoubaoVideo2Asset(cacheKey)
			if err != nil {
				return "", doubaoVideo2WrapError("registry_wait", err)
			}
			if !found {
				record = nil
			}
		}
	}
}

func createDoubaoVideo2Asset(ctx context.Context, config doubaoVideo2MaterialConfig, sourceURL, name string) (string, error) {
	body, err := common.Marshal(map[string]any{"GroupId": config.GroupID, "URL": sourceURL, "Name": name, "AssetType": "Image"})
	if err != nil {
		return "", err
	}
	responseBody, err := callDoubaoVideo2Material(ctx, config, "CreateAsset", body)
	if err != nil {
		return "", err
	}
	var response doubaoVideo2CreateAssetResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("decode CreateAsset response: %w", err)
	}
	if err := doubaoVideo2MetadataError(response.ResponseMetadata); err != nil {
		return "", err
	}
	if strings.TrimSpace(response.Result.ID) == "" {
		return "", errors.New("CreateAsset returned an empty asset ID")
	}
	return response.Result.ID, nil
}

func waitForDoubaoVideo2Asset(ctx context.Context, config doubaoVideo2MaterialConfig, assetID string) error {
	waitCtx, cancel := context.WithTimeout(ctx, doubaoVideo2MaterialWaitTimeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		status, reason, err := getDoubaoVideo2AssetStatus(waitCtx, config, assetID)
		if err != nil {
			return err
		}
		switch strings.ToLower(status) {
		case "active":
			return nil
		case "failed", "error", "rejected":
			return &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialRejected, Stage: "get_asset", AssetID: assetID, Err: errors.New(reason)}
		}
		select {
		case <-waitCtx.Done():
			return doubaoVideo2WrapError("get_asset", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

func getDoubaoVideo2AssetStatus(ctx context.Context, config doubaoVideo2MaterialConfig, assetID string) (string, string, error) {
	body, err := common.Marshal(map[string]any{"Id": assetID})
	if err != nil {
		return "", "", err
	}
	responseBody, err := callDoubaoVideo2MaterialSafe(ctx, config, "GetAsset", body)
	if err != nil {
		return "", "", err
	}
	var response doubaoVideo2GetAssetResponse
	if err := common.Unmarshal(responseBody, &response); err != nil {
		return "", "", fmt.Errorf("decode GetAsset response: %w", err)
	}
	if err := doubaoVideo2MetadataError(response.ResponseMetadata); err != nil {
		return "", "", err
	}
	reason := ""
	if response.Result.Error != nil {
		reason = response.Result.Error.Message
	}
	return response.Result.Status, reason, nil
}

func callDoubaoVideo2Material(ctx context.Context, config doubaoVideo2MaterialConfig, action string, body []byte) ([]byte, error) {
	endpoint, err := url.Parse(strings.TrimRight(config.ProviderURL, "/") + "/api/support/v1/asset")
	if err != nil {
		return nil, err
	}
	query := endpoint.Query()
	query.Set("Action", action)
	query.Set("Version", doubaoVideo2MaterialVersion)
	endpoint.RawQuery = query.Encode()
	callCtx, cancel := context.WithTimeout(ctx, doubaoVideo2MaterialCallTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("ApiKey", config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(config.OutboundProxy)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, &doubaoVideo2MaterialCallError{Action: action, Err: err, Message: err.Error()}
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > 2<<20 {
		return nil, &doubaoVideo2MaterialCallError{Action: action, StatusCode: response.StatusCode, Message: "response exceeds 2MB"}
	}
	var envelope struct {
		ResponseMetadata doubaoVideo2MaterialMetadata `json:"ResponseMetadata"`
	}
	_ = common.Unmarshal(responseBody, &envelope)
	if response.StatusCode < 200 || response.StatusCode >= 300 || envelope.ResponseMetadata.Error != nil {
		callErr := &doubaoVideo2MaterialCallError{Action: action, StatusCode: response.StatusCode, RequestID: envelope.ResponseMetadata.RequestID, Message: strings.TrimSpace(string(responseBody))}
		if envelope.ResponseMetadata.Error != nil {
			callErr.Code = fmt.Sprint(envelope.ResponseMetadata.Error.Code)
			callErr.Message = envelope.ResponseMetadata.Error.Message
		}
		return nil, callErr
	}
	return responseBody, nil
}

func callDoubaoVideo2MaterialSafe(ctx context.Context, config doubaoVideo2MaterialConfig, action string, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		response, err := callDoubaoVideo2Material(ctx, config, action, body)
		if err == nil {
			return response, nil
		}
		lastErr = err
		var callErr *doubaoVideo2MaterialCallError
		if !errors.As(err, &callErr) || !callErr.retryable() || attempt == 2 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(250*(1<<attempt)) * time.Millisecond):
		}
	}
	return nil, lastErr
}

func doubaoVideo2MetadataError(metadata doubaoVideo2MaterialMetadata) error {
	if metadata.Error == nil {
		return nil
	}
	return &doubaoVideo2MaterialCallError{StatusCode: http.StatusOK, RequestID: metadata.RequestID, Code: fmt.Sprint(metadata.Error.Code), Message: metadata.Error.Message}
}

func doubaoVideo2WrapError(stage string, err error) *doubaoVideo2BuildError {
	var existing *doubaoVideo2BuildError
	if errors.As(err, &existing) {
		return existing
	}
	result := &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialTransient, Stage: stage, Err: err}
	var callErr *doubaoVideo2MaterialCallError
	if errors.As(err, &callErr) {
		result.RequestID = callErr.RequestID
		text := strings.ToLower(callErr.Code + " " + callErr.Message)
		switch {
		case callErr.StatusCode == http.StatusUnauthorized || callErr.StatusCode == http.StatusForbidden || strings.Contains(text, "auth") || strings.Contains(text, "permission"):
			result.Kind = doubaoVideo2ErrorMaterialAuth
		case callErr.StatusCode == http.StatusTooManyRequests || strings.Contains(text, "throttl") || strings.Contains(text, "ratelimit"):
			result.Kind = doubaoVideo2ErrorMaterialRateLimit
		case !callErr.retryable():
			result.Kind = doubaoVideo2ErrorMaterialRejected
		}
	}
	return result
}

// A timeout, connection loss, or 5xx after CreateAsset was sent is ambiguous:
// the provider may already have created the asset. Never let the automatic
// recovery loop replay such a non-idempotent request. Explicit 429 responses
// are safe to retry because the provider rejected the request before creation.
func classifyDoubaoVideo2CreateError(err error) error {
	var callErr *doubaoVideo2MaterialCallError
	if !errors.As(err, &callErr) {
		return err
	}
	if callErr.StatusCode == http.StatusTooManyRequests {
		return doubaoVideo2WrapError("create", err)
	}
	if callErr.StatusCode == 0 || callErr.StatusCode >= 500 {
		return &doubaoVideo2BuildError{Kind: doubaoVideo2ErrorMaterialAmbiguous, Stage: "create", RequestID: callErr.RequestID, Err: errors.New("provider response was inconclusive; CreateAsset was not replayed to avoid a duplicate asset")}
	}
	return doubaoVideo2WrapError("create", err)
}

// IsDoubaoVideo2MaterialCreateAmbiguous reports whether CreateAsset may have
// succeeded even though the provider response was lost. Callers must retain
// the submitted source URL in this case so a created upstream asset does not
// lose its preview source.
func IsDoubaoVideo2MaterialCreateAmbiguous(err error) bool {
	var buildErr *doubaoVideo2BuildError
	return errors.As(err, &buildErr) && buildErr.Kind == doubaoVideo2ErrorMaterialAmbiguous
}

// IsDoubaoVideo2MaterialNotFound makes delete and lifecycle synchronization
// idempotent across provider HTTP and metadata error shapes.
func IsDoubaoVideo2MaterialNotFound(err error) bool {
	var callErr *doubaoVideo2MaterialCallError
	if !errors.As(err, &callErr) {
		return false
	}
	if callErr.StatusCode == http.StatusNotFound {
		return true
	}
	text := strings.ToLower(callErr.Code + " " + callErr.Message)
	return strings.Contains(text, "notfound") || strings.Contains(text, "not found") ||
		strings.Contains(text, "does not exist") || strings.Contains(text, "不存在")
}

func doubaoVideo2SHA256(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
