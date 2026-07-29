package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
)

type UpstreamAccountErrorDisposition int

const (
	UpstreamAccountErrorNotHandled UpstreamAccountErrorDisposition = iota
	UpstreamAccountErrorRetryAccount
	UpstreamAccountErrorRetryTransport
	UpstreamAccountErrorGlobal
)

func (disposition UpstreamAccountErrorDisposition) Handled() bool {
	return disposition != UpstreamAccountErrorNotHandled
}

func (disposition UpstreamAccountErrorDisposition) RetryWithinPool() bool {
	return disposition == UpstreamAccountErrorRetryAccount || disposition == UpstreamAccountErrorRetryTransport
}

type UpstreamAccountEventInput struct {
	AccountId int
	PoolId    int
	ProxyId   int
	ChannelId int
	RequestId string
	LeaseId   string
	EventType string
	Result    string
	Message   string
	Metadata  map[string]any
}

func recordUpstreamAccountEvent(accountId int, proxyId int, eventType string, result string, message string, metadata map[string]any) {
	RecordUpstreamAccountEvent(UpstreamAccountEventInput{
		AccountId: accountId, ProxyId: proxyId, EventType: eventType,
		Result: result, Message: message, Metadata: metadata,
	})
}

func RecordUpstreamAccountEvent(input UpstreamAccountEventInput) {
	if model.DB == nil || (input.AccountId <= 0 && input.PoolId <= 0 && input.ProxyId <= 0) {
		return
	}
	metadataJSON := "{}"
	if len(input.Metadata) > 0 {
		if encoded, err := common.Marshal(input.Metadata); err == nil {
			metadataJSON = string(encoded)
		}
	}
	event := model.UpstreamAccountEvent{
		RequestId: strings.TrimSpace(input.RequestId),
		LeaseId:   strings.TrimSpace(input.LeaseId),
		EventType: strings.TrimSpace(input.EventType),
		Result:    strings.TrimSpace(input.Result),
		Message:   truncateUpstreamEventText(input.Message, 512),
		Metadata:  metadataJSON,
		CreatedAt: common.GetTimestamp(),
	}
	if input.AccountId > 0 {
		event.AccountId = &input.AccountId
	}
	if input.PoolId > 0 {
		event.PoolId = &input.PoolId
	}
	if input.ProxyId > 0 {
		event.ProxyId = &input.ProxyId
	}
	if input.ChannelId > 0 {
		event.ChannelId = &input.ChannelId
	}
	_ = model.DB.Create(&event).Error
}

func RecordUpstreamAccountEventAsync(input UpstreamAccountEventInput) {
	if len(input.Metadata) > 0 {
		metadata := make(map[string]any, len(input.Metadata))
		for key, value := range input.Metadata {
			metadata[key] = value
		}
		input.Metadata = metadata
	}
	gopool.Go(func() {
		RecordUpstreamAccountEvent(input)
	})
}

func truncateUpstreamEventText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func ApplyUpstreamAccountError(accountId int, proxyId int, apiErr *types.NewAPIError) UpstreamAccountErrorDisposition {
	if accountId <= 0 || apiErr == nil {
		return UpstreamAccountErrorNotHandled
	}
	now := common.GetTimestamp()
	message := UpstreamAccountErrorSummary(apiErr)
	updates := map[string]any{
		"error_message": message,
		"updated_at":    now,
	}
	disposition := UpstreamAccountErrorRetryAccount
	switch {
	case apiErr.StatusCode == 401:
		updates["status"] = "error"
		updates["schedulable"] = false
		updates["temp_unschedulable_reason"] = "authentication_failed"
	case apiErr.StatusCode == 403:
		until := now + int64((5 * time.Minute).Seconds())
		updates["temp_unschedulable_until"] = until
		updates["temp_unschedulable_reason"] = "upstream_forbidden"
	case apiErr.StatusCode == 429:
		resetAt, windowStart, windowEnd := upstreamRateLimitState(apiErr, now)
		updates["rate_limited_at"] = now
		updates["rate_limit_reset_at"] = resetAt
		if windowStart != nil && windowEnd != nil {
			updates["session_window_start"] = *windowStart
			updates["session_window_end"] = *windowEnd
			updates["session_window_status"] = "rejected"
		}
	case apiErr.StatusCode == 408 || isUpstreamTransportError(apiErr):
		disposition = UpstreamAccountErrorRetryTransport
		updates = nil
	case apiErr.StatusCode >= 500 && hasUpstreamHTTPResponse(apiErr):
		return UpstreamAccountErrorGlobal
	default:
		return UpstreamAccountErrorNotHandled
	}
	if len(updates) > 0 {
		_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(updates).Error
	}
	if disposition == UpstreamAccountErrorRetryTransport && proxyId > 0 {
		_ = model.DB.Model(&model.UpstreamProxy{}).Where("id = ?", proxyId).Updates(map[string]any{
			"last_test_at": now, "latency_status": "error", "latency_message": message, "updated_at": now,
		}).Error
	}
	return disposition
}

func isUpstreamTransportError(apiErr *types.NewAPIError) bool {
	if apiErr == nil {
		return false
	}
	switch apiErr.GetErrorCode() {
	case types.ErrorCodeDoRequestFailed, types.ErrorCodeReadResponseBodyFailed, types.ErrorCodeBadResponse, types.ErrorCodeEmptyResponse:
		return true
	default:
		return false
	}
}

func hasUpstreamHTTPResponse(apiErr *types.NewAPIError) bool {
	if apiErr == nil {
		return false
	}
	header, body := apiErr.UpstreamResponse()
	return len(header) > 0 || len(body) > 0 || apiErr.GetErrorCode() == types.ErrorCodeBadResponseStatusCode
}

func ShouldRetryUpstreamAccount(failovers int, startedAt time.Time) bool {
	maxFailovers := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_MAX_FAILOVERS", 2)
	if maxFailovers < 0 {
		maxFailovers = 0
	}
	if failovers >= maxFailovers {
		return false
	}
	budgetMs := common.GetEnvOrDefault("UPSTREAM_ACCOUNT_FAILOVER_BUDGET_MS", 5000)
	if budgetMs <= 0 || startedAt.IsZero() {
		return true
	}
	return time.Since(startedAt) < time.Duration(budgetMs)*time.Millisecond
}

func RecordUpstreamAccountSuccess(accountId int) {
	if accountId <= 0 {
		return
	}
	now := common.GetTimestamp()
	_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(map[string]any{
		"last_used_at":  now,
		"error_message": "",
		"updated_at":    now,
	}).Error
}

func UpstreamAccountErrorSummary(apiErr *types.NewAPIError) string {
	if apiErr == nil {
		return ""
	}
	return fmt.Sprintf("status_code=%d, error_code=%s", apiErr.StatusCode, apiErr.GetErrorCode())
}
