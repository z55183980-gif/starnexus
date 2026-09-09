package service

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const proxyFailureMetadataContextKey = "upstream_proxy_failure_metadata"

type ProxyFailureMetadata struct {
	ProxyFailure      bool   `json:"proxy_failure"`
	ProxyElapsedMs    int64  `json:"proxy_elapsed_ms"`
	ProxyErrorClass   string `json:"proxy_error_class"`
	ProxyErrorStage   string `json:"proxy_error_stage"`
	ProxyTimeout      bool   `json:"proxy_timeout"`
	ProxyEndpoint     string `json:"proxy_endpoint,omitempty"`
	ProxyRequestModel string `json:"proxy_request_model,omitempty"`
}

// SetProxyFailureMetadata stores the latest real-request proxy failure on the
// request context. It is consumed by the existing request_error event writer.
func SetProxyFailureMetadata(c *gin.Context, metadata ProxyFailureMetadata) {
	if c == nil || !metadata.ProxyFailure {
		return
	}
	c.Set(proxyFailureMetadataContextKey, metadata)
}

func ClearProxyFailureMetadata(c *gin.Context) {
	if c != nil {
		c.Set(proxyFailureMetadataContextKey, nil)
	}
}

func consumeProxyFailureMetadata(c *gin.Context) (ProxyFailureMetadata, bool) {
	if c == nil {
		return ProxyFailureMetadata{}, false
	}
	value, ok := c.Get(proxyFailureMetadataContextKey)
	if !ok {
		return ProxyFailureMetadata{}, false
	}
	c.Set(proxyFailureMetadataContextKey, nil)
	metadata, ok := value.(ProxyFailureMetadata)
	return metadata, ok && metadata.ProxyFailure
}

// ConsumeProxyFailureMetadata returns metadata for the existing request_error
// event and clears it so a later retry cannot attribute the old attempt twice.
func ConsumeProxyFailureMetadata(c *gin.Context) (map[string]any, bool) {
	metadata, ok := consumeProxyFailureMetadata(c)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"proxy_failure":       metadata.ProxyFailure,
		"proxy_elapsed_ms":    metadata.ProxyElapsedMs,
		"proxy_error_class":   metadata.ProxyErrorClass,
		"proxy_error_stage":   metadata.ProxyErrorStage,
		"proxy_timeout":       metadata.ProxyTimeout,
		"proxy_endpoint":      metadata.ProxyEndpoint,
		"proxy_request_model": metadata.ProxyRequestModel,
	}, true
}

// ClassifyProxyFailure classifies only transport failures from a real request.
// HTTP responses (including upstream 4xx/5xx) never reach this function.
func ClassifyProxyFailure(err error, proxyURL string, elapsed time.Duration, modelName string) ProxyFailureMetadata {
	metadata := ProxyFailureMetadata{
		ProxyFailure:      true,
		ProxyElapsedMs:    elapsed.Milliseconds(),
		ProxyErrorClass:   "proxy_connect_error",
		ProxyErrorStage:   "proxy_dial",
		ProxyRequestModel: strings.TrimSpace(modelName),
	}
	if metadata.ProxyElapsedMs < 0 {
		metadata.ProxyElapsedMs = 0
	}
	if parsed, parseErr := url.Parse(strings.TrimSpace(proxyURL)); parseErr == nil && parsed != nil {
		scheme := strings.ToLower(parsed.Scheme)
		if host := parsed.Hostname(); host != "" {
			port := parsed.Port()
			if port == "" {
				port = defaultProxyPort(scheme)
			}
			metadata.ProxyEndpoint = net.JoinHostPort(host, port)
		}
		if scheme == "socks5" || scheme == "socks5h" {
			metadata.ProxyErrorStage = "socks5_dial"
		} else if scheme == "http" || scheme == "https" {
			metadata.ProxyErrorStage = "http_connect"
		}
	}
	if err != nil {
		var netErr net.Error
		metadata.ProxyTimeout = errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout()
		lower := strings.ToLower(err.Error())
		if metadata.ProxyTimeout || strings.Contains(lower, "timeout") || strings.Contains(lower, "i/o timeout") {
			metadata.ProxyTimeout = true
			metadata.ProxyErrorClass = "proxy_connect_timeout"
		} else if strings.Contains(lower, "socks") || strings.Contains(lower, "proxyconnect") || strings.Contains(lower, "proxy") {
			metadata.ProxyErrorClass = "proxy_handshake_error"
		}
	}
	return metadata
}

// IsRealProxyFailureError excludes client cancellation and local request
// construction/body errors. The remaining errors are transport failures that
// occurred while a configured proxy was being used for a real request.
func IsRealProxyFailureError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "proxy") ||
		strings.Contains(lower, "socks") ||
		strings.Contains(lower, "dial tcp") ||
		strings.Contains(lower, "tls handshake") ||
		strings.Contains(lower, "i/o timeout")
}

func defaultProxyPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}

type ProxyRealRequestHealth struct {
	FailureCount24h     int64
	TimeoutCount24h     int64
	LastFailureAt       *int64
	ConsecutiveFailures int64
}

type ProxyRealRequestFailure struct {
	ID         int64  `json:"id"`
	CreatedAt  int64  `json:"created_at"`
	AccountID  *int   `json:"account_id,omitempty"`
	RequestID  string `json:"request_id"`
	Result     string `json:"result"`
	Message    string `json:"message"`
	ElapsedMs  int64  `json:"elapsed_ms"`
	ErrorClass string `json:"error_class"`
	ErrorStage string `json:"error_stage"`
	Timeout    bool   `json:"timeout"`
	Endpoint   string `json:"endpoint,omitempty"`
	Model      string `json:"model,omitempty"`
}

type proxyHealthEventRow struct {
	ID        int64
	ProxyID   *int
	AccountID *int
	RequestID string
	EventType string
	Result    string
	Message   string
	Metadata  string
	CreatedAt int64
}

func loadProxyHealthEventRows(db context.Context, proxyIDs []int, since int64) ([]proxyHealthEventRow, error) {
	query := model.DB.WithContext(db).Model(&model.UpstreamAccountEvent{}).
		Select("id, proxy_id, account_id, request_id, event_type, result, message, metadata, created_at").
		Where("event_type IN ? AND created_at >= ?", []string{"request_error", "request_success"}, since).
		Order("created_at DESC, id DESC")
	if len(proxyIDs) > 0 {
		query = query.Where("proxy_id IN ?", proxyIDs)
	}
	var rows []proxyHealthEventRow
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func parseProxyFailureMetadata(raw string) (ProxyFailureMetadata, bool) {
	var metadata ProxyFailureMetadata
	if strings.TrimSpace(raw) == "" || common.UnmarshalJsonStr(raw, &metadata) != nil {
		return ProxyFailureMetadata{}, false
	}
	return metadata, metadata.ProxyFailure
}

func LoadProxyRealRequestHealth(proxyIDs []int, window time.Duration) (map[int]ProxyRealRequestHealth, error) {
	result := make(map[int]ProxyRealRequestHealth, len(proxyIDs))
	if len(proxyIDs) == 0 {
		return result, nil
	}
	cutoff := common.GetTimestamp() - int64(window/time.Second)
	rows, err := loadProxyHealthEventRows(context.Background(), proxyIDs, cutoff)
	if err != nil {
		return nil, err
	}
	allowed := make(map[int]struct{}, len(proxyIDs))
	for _, id := range proxyIDs {
		allowed[id] = struct{}{}
	}
	seen := make(map[int]bool, len(proxyIDs))
	for _, row := range rows {
		if row.ProxyID == nil {
			continue
		}
		proxyID := *row.ProxyID
		if _, ok := allowed[proxyID]; !ok {
			continue
		}
		health := result[proxyID]
		metadata, proxyFailure := parseProxyFailureMetadata(row.Metadata)
		if row.EventType == "request_error" && proxyFailure {
			health.FailureCount24h++
			if metadata.ProxyTimeout {
				health.TimeoutCount24h++
			}
			if health.LastFailureAt == nil {
				created := row.CreatedAt
				health.LastFailureAt = &created
			}
			if !seen[proxyID] {
				health.ConsecutiveFailures++
			}
		} else if row.EventType == "request_success" && !seen[proxyID] {
			seen[proxyID] = true
		}
		result[proxyID] = health
	}
	return result, nil
}

func ListProxyRealRequestFailures(proxyID int, limit int) ([]ProxyRealRequestFailure, error) {
	if proxyID <= 0 {
		return nil, errors.New("invalid upstream proxy id")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	cutoff := common.GetTimestamp() - 24*60*60
	rows, err := loadProxyHealthEventRows(context.Background(), []int{proxyID}, cutoff)
	if err != nil {
		return nil, err
	}
	result := make([]ProxyRealRequestFailure, 0, limit)
	for _, row := range rows {
		if len(result) >= limit || row.EventType != "request_error" {
			continue
		}
		metadata, ok := parseProxyFailureMetadata(row.Metadata)
		if !ok {
			continue
		}
		result = append(result, ProxyRealRequestFailure{
			ID: row.ID, CreatedAt: row.CreatedAt, AccountID: row.AccountID,
			RequestID: row.RequestID, Result: row.Result, Message: row.Message,
			ElapsedMs: metadata.ProxyElapsedMs, ErrorClass: metadata.ProxyErrorClass,
			ErrorStage: metadata.ProxyErrorStage, Timeout: metadata.ProxyTimeout,
			Endpoint: metadata.ProxyEndpoint, Model: metadata.ProxyRequestModel,
		})
	}
	return result, nil
}

func ParsePositiveLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 50
	}
	return limit
}
