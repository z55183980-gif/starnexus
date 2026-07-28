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

func ApplyUpstreamAccountError(accountId int, proxyId int, apiErr *types.NewAPIError) bool {
	if accountId <= 0 || apiErr == nil {
		return false
	}
	now := common.GetTimestamp()
	message := UpstreamAccountErrorSummary(apiErr)
	updates := map[string]any{
		"error_message": message,
		"updated_at":    now,
	}
	owned := true
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
		resetAt := now + int64(time.Minute.Seconds())
		updates["rate_limited_at"] = now
		updates["rate_limit_reset_at"] = resetAt
	case apiErr.StatusCode == 408 || apiErr.StatusCode >= 500 || apiErr.GetErrorCode() == types.ErrorCodeDoRequestFailed:
		until := now + int64((30 * time.Second).Seconds())
		updates["temp_unschedulable_until"] = until
		updates["temp_unschedulable_reason"] = "upstream_transport"
	default:
		owned = false
	}
	if !owned {
		return false
	}
	_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(updates).Error
	if proxyId > 0 && (apiErr.StatusCode == 408 || apiErr.GetErrorCode() == types.ErrorCodeDoRequestFailed) {
		_ = model.DB.Model(&model.UpstreamProxy{}).Where("id = ?", proxyId).Updates(map[string]any{
			"last_test_at": now, "latency_status": "error", "latency_message": message, "updated_at": now,
		}).Error
	}
	return true
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
