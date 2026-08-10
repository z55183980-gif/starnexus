package doubao

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type zqbapiErrorKind string

const (
	zqbapiErrorInvalidImage      zqbapiErrorKind = "invalid_image"
	zqbapiErrorMaterialRejected  zqbapiErrorKind = "material_rejected"
	zqbapiErrorMaterialAuth      zqbapiErrorKind = "material_auth_failed"
	zqbapiErrorMaterialRateLimit zqbapiErrorKind = "material_rate_limited"
	zqbapiErrorMaterialTransient zqbapiErrorKind = "material_transient"
	zqbapiErrorMaterialConfig    zqbapiErrorKind = "material_not_configured"
)

type zqbapiBuildError struct {
	Kind      zqbapiErrorKind
	Stage     string
	RequestID string
	AssetID   string
	Err       error
}

func (e *zqbapiBuildError) Error() string {
	if e == nil {
		return "ZQBAPI material error"
	}
	prefix := "ZQBAPI material"
	if e.Stage != "" {
		prefix += " " + e.Stage
	}
	if e.RequestID != "" {
		prefix += " (request_id=" + e.RequestID + ")"
	}
	if e.AssetID != "" {
		prefix += " (asset_id=" + e.AssetID + ")"
	}
	if e.Err == nil {
		return prefix + " failed"
	}
	return fmt.Sprintf("%s: %v", prefix, e.Err)
}

func (e *zqbapiBuildError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *zqbapiBuildError) TaskErrorCode() string {
	if e == nil || e.Kind == "" {
		return string(zqbapiErrorMaterialTransient)
	}
	return string(e.Kind)
}

func (e *zqbapiBuildError) TaskHTTPStatus() int {
	if e == nil {
		return http.StatusBadGateway
	}
	switch e.Kind {
	case zqbapiErrorInvalidImage, zqbapiErrorMaterialRejected:
		return http.StatusUnprocessableEntity
	case zqbapiErrorMaterialRateLimit:
		return http.StatusTooManyRequests
	case zqbapiErrorMaterialConfig:
		return http.StatusServiceUnavailable
	case zqbapiErrorMaterialAuth:
		return http.StatusBadGateway
	default:
		return http.StatusBadGateway
	}
}

// Material preparation owns its retry policy. Returning a local task error
// prevents the generic task relay from repeating CreateAsset and producing
// duplicate failed assets.
func (e *zqbapiBuildError) TaskLocalError() bool { return true }

func (e *zqbapiBuildError) Temporary() bool {
	return e != nil && (e.Kind == zqbapiErrorMaterialTransient || e.Kind == zqbapiErrorMaterialRateLimit)
}

func newZQBAPIBuildError(kind zqbapiErrorKind, stage string, err error) error {
	if existing, ok := err.(*zqbapiBuildError); ok {
		if existing.Stage == "" {
			existing.Stage = stage
		}
		return existing
	}
	return &zqbapiBuildError{Kind: kind, Stage: stage, Err: err}
}

type zqbapiMaterialCallError struct {
	Action     string
	StatusCode int
	RequestID  string
	Code       string
	Message    string
	Err        error
}

func (e *zqbapiMaterialCallError) Error() string {
	if e == nil {
		return "ZQBAPI material call failed"
	}
	message := strings.TrimSpace(e.Message)
	if message == "" && e.Err != nil {
		message = e.Err.Error()
	}
	return fmt.Sprintf("action=%s status=%d code=%s request_id=%s: %s", e.Action, e.StatusCode, e.Code, e.RequestID, message)
}

func (e *zqbapiMaterialCallError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *zqbapiMaterialCallError) Retryable() bool {
	if e == nil {
		return false
	}
	if e.Err != nil && e.StatusCode == 0 {
		return true
	}
	if e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500 {
		return true
	}
	code := strings.ToLower(e.Code)
	return strings.Contains(code, "ratelimit") || strings.Contains(code, "throttl") || strings.Contains(code, "timeout")
}

func classifyZQBAPIMaterialError(stage string, err error) error {
	if err == nil {
		return nil
	}
	var buildErr *zqbapiBuildError
	if errors.As(err, &buildErr) {
		return err
	}
	var callErr *zqbapiMaterialCallError
	if !errors.As(err, &callErr) {
		return newZQBAPIBuildError(zqbapiErrorMaterialTransient, stage, err)
	}
	kind := zqbapiErrorMaterialRejected
	code := strings.ToLower(callErr.Code + " " + callErr.Message)
	switch {
	case callErr.StatusCode == http.StatusUnauthorized || callErr.StatusCode == http.StatusForbidden ||
		strings.Contains(code, "auth") || strings.Contains(code, "permission") || strings.Contains(code, "accessdenied"):
		kind = zqbapiErrorMaterialAuth
	case callErr.StatusCode == http.StatusTooManyRequests || strings.Contains(code, "ratelimit") || strings.Contains(code, "throttl"):
		kind = zqbapiErrorMaterialRateLimit
	case callErr.Retryable():
		kind = zqbapiErrorMaterialTransient
	}
	return &zqbapiBuildError{Kind: kind, Stage: stage, RequestID: callErr.RequestID, Err: err}
}
