package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/bytedance/gopkg/util/gopool"
)

type UpstreamAccountErrorDisposition int

var (
	upstreamSensitiveAssignmentPattern = regexp.MustCompile(`(?i)\b(access_token|refresh_token|api[_-]?key|token|authorization)\s*([:=])\s*([^\s,;]+)`)
	upstreamBearerPattern              = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
)

const (
	UpstreamAccountErrorNotHandled UpstreamAccountErrorDisposition = iota
	UpstreamAccountErrorRetryAccount
	UpstreamAccountErrorRetryTransport
	UpstreamAccountErrorGlobal
)

const upstreamAccountOverloadCooldown = time.Minute

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
	refreshAfterUpdate := false
	disposition := UpstreamAccountErrorRetryAccount

	var account model.UpstreamAccount
	accountLoaded := false
	loadAccount := func() bool {
		if accountLoaded {
			return true
		}
		if err := model.DB.First(&account, accountId).Error; err != nil {
			return false
		}
		accountLoaded = true
		return true
	}

	if matched, until, reason := matchUpstreamTempUnschedulableRule(apiErr, loadAccount, &account); matched {
		updates["temp_unschedulable_until"] = until
		updates["temp_unschedulable_reason"] = reason
		_ = model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", accountId).Updates(updates).Error
		return UpstreamAccountErrorRetryAccount
	}

	switch {
	case apiErr.StatusCode == 401:
		if !loadAccount() {
			return UpstreamAccountErrorNotHandled
		}
		if isPermanentUpstreamAuthenticationError(apiErr) || !canRefreshUpstreamAuthentication(&account) {
			updates["status"] = constant.UpstreamStatusError
			updates["schedulable"] = false
			updates["temp_unschedulable_until"] = nil
			if isRefreshableUpstreamOAuthAccount(account.Platform, account.Type) &&
				account.OAuthRefreshOwner == constant.UpstreamOAuthRefreshOwnerStarNexus {
				updates["temp_unschedulable_reason"] = constant.UpstreamAccountReasonOAuthRefreshPermanent
			} else {
				updates["temp_unschedulable_reason"] = constant.UpstreamAccountReasonAuthenticationFailed
			}
		} else {
			updates["status"] = constant.UpstreamStatusActive
			updates["schedulable"] = true
			updates["temp_unschedulable_until"] = now + int64((10 * time.Minute).Seconds())
			updates["temp_unschedulable_reason"] = constant.UpstreamAccountReasonOAuthRefreshPending
			refreshAfterUpdate = true
		}
	case apiErr.StatusCode == 402:
		updates["status"] = constant.UpstreamStatusError
		updates["schedulable"] = false
		updates["temp_unschedulable_until"] = nil
		updates["temp_unschedulable_reason"] = "payment_required"
	case apiErr.StatusCode == 403:
		until := now + int64((5 * time.Minute).Seconds())
		updates["temp_unschedulable_until"] = until
		updates["temp_unschedulable_reason"] = message
	case apiErr.StatusCode == 429:
		resetAt, windowStart, windowEnd := upstreamRateLimitState(apiErr, now)
		updates["rate_limited_at"] = now
		updates["rate_limit_reset_at"] = resetAt
		updates["temp_unschedulable_reason"] = "rate_limited"
		if windowStart != nil && windowEnd != nil {
			updates["session_window_start"] = *windowStart
			updates["session_window_end"] = *windowEnd
			updates["session_window_status"] = "rejected"
		}
	case apiErr.StatusCode == 529:
		updates["overload_until"] = now + int64(upstreamAccountOverloadCooldown.Seconds())
		updates["temp_unschedulable_reason"] = "upstream_overloaded"
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
	if refreshAfterUpdate {
		refreshUpstreamAccountAfterAuthenticationFailure(accountId)
	}
	if disposition == UpstreamAccountErrorRetryTransport && proxyId > 0 {
		_ = model.DB.Model(&model.UpstreamProxy{}).Where("id = ?", proxyId).Updates(map[string]any{
			"last_test_at": now, "latency_status": "error", "latency_message": message, "updated_at": now,
		}).Error
	}
	return disposition
}

const upstreamTempUnschedReasonLimit = 128

func matchUpstreamTempUnschedulableRule(
	apiErr *types.NewAPIError,
	loadAccount func() bool,
	account *model.UpstreamAccount,
) (bool, int64, string) {
	if apiErr == nil || apiErr.StatusCode <= 0 || account == nil || !loadAccount() {
		return false, 0, ""
	}
	credentials, _ := DecryptUpstreamAccountCredentials(account)
	options, err := model.ParseUpstreamAccountOptionsWithCredentials(account.Extra, credentials)
	if err != nil {
		options, err = model.ParseUpstreamAccountOptions(account.Extra)
		if err != nil {
			return false, 0, ""
		}
	}
	if !options.TempUnschedulableEnabled || len(options.TempUnschedulableRules) == 0 {
		return false, 0, ""
	}
	_, body := apiErr.UpstreamResponse()
	var searchParts []string
	if len(body) > 0 {
		searchParts = append(searchParts, string(body))
	}
	if msg := strings.TrimSpace(apiErr.Error()); msg != "" {
		searchParts = append(searchParts, msg)
	}
	if summary := UpstreamAccountErrorSummary(apiErr); summary != "" {
		searchParts = append(searchParts, summary)
	}
	searchText := strings.ToLower(strings.Join(searchParts, "\n"))
	if strings.TrimSpace(searchText) == "" {
		return false, 0, ""
	}
	for _, rule := range options.TempUnschedulableRules {
		if rule.ErrorCode != apiErr.StatusCode {
			continue
		}
		matchedKeyword := matchUpstreamTempUnschedKeyword(searchText, rule.Keywords)
		if matchedKeyword == "" {
			continue
		}
		until := common.GetTimestamp() + int64(rule.DurationMinutes)*60
		reason := strings.TrimSpace(rule.Description)
		if reason == "" {
			reason = matchedKeyword
		}
		return true, until, truncateUpstreamEventText(reason, upstreamTempUnschedReasonLimit)
	}
	return false, 0, ""
}

func matchUpstreamTempUnschedKeyword(bodyLower string, keywords []string) string {
	if bodyLower == "" {
		return ""
	}
	for _, keyword := range keywords {
		k := strings.TrimSpace(keyword)
		if k == "" {
			continue
		}
		if strings.Contains(bodyLower, strings.ToLower(k)) {
			return k
		}
	}
	return ""
}

func refreshUpstreamAccountAfterAuthenticationFailure(accountId int) {
	gopool.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = RefreshUpstreamOAuthAccount(ctx, accountId)
	})
}

func canRefreshUpstreamAuthentication(account *model.UpstreamAccount) bool {
	if account == nil || !isRefreshableUpstreamOAuthAccount(account.Platform, account.Type) ||
		account.OAuthRefreshOwner != constant.UpstreamOAuthRefreshOwnerStarNexus {
		return false
	}
	credentials, err := DecryptUpstreamAccountCredentials(account)
	return err == nil && upstreamCredentialMapString(credentials, "refresh_token") != ""
}

func isPermanentUpstreamAuthenticationError(apiErr *types.NewAPIError) bool {
	code, message := upstreamErrorDetails(apiErr)
	code = strings.ToLower(strings.TrimSpace(code))
	message = strings.ToLower(strings.TrimSpace(message))
	return code == "token_invalidated" || code == "token_revoked" ||
		message == "unauthorized" || strings.Contains(message, "invalidated oauth token") ||
		strings.Contains(message, "token revoked")
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
	_, upstreamMessage := upstreamErrorDetails(apiErr)
	if upstreamMessage == "" {
		upstreamMessage = strings.TrimSpace(apiErr.Error())
	}
	if upstreamMessage == "" {
		upstreamMessage = string(apiErr.GetErrorCode())
	}
	var summary string
	switch {
	case apiErr.StatusCode == 401 && isPermanentUpstreamAuthenticationError(apiErr):
		summary = fmt.Sprintf("Token revoked (401): %s", upstreamMessage)
	case apiErr.StatusCode == 401:
		summary = fmt.Sprintf("Authentication failed (401): %s", upstreamMessage)
	case apiErr.StatusCode == 402:
		summary = fmt.Sprintf("Payment required (402): %s", upstreamMessage)
	case apiErr.StatusCode > 0:
		summary = fmt.Sprintf("%s (%d)", upstreamMessage, apiErr.StatusCode)
	default:
		summary = upstreamMessage
	}
	return sanitizeUpstreamErrorSummary(summary)
}

func sanitizeUpstreamErrorSummary(summary string) string {
	summary = common.MaskSensitiveInfo(summary)
	summary = upstreamSensitiveAssignmentPattern.ReplaceAllString(summary, "$1$2***")
	return upstreamBearerPattern.ReplaceAllString(summary, "Bearer ***")
}

func upstreamErrorDetails(apiErr *types.NewAPIError) (string, string) {
	if apiErr == nil {
		return "", ""
	}
	_, body := apiErr.UpstreamResponse()
	if len(body) == 0 {
		return "", ""
	}
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return "", ""
	}
	errorPayload, _ := payload["error"].(map[string]any)
	detailPayload, _ := payload["detail"].(map[string]any)
	code := firstUpstreamErrorString(errorPayload["code"], detailPayload["code"], payload["code"])
	message := firstUpstreamErrorString(errorPayload["message"], detailPayload["message"], payload["detail"], payload["message"])
	return code, message
}

func firstUpstreamErrorString(values ...any) string {
	for _, value := range values {
		switch typed := value.(type) {
		case string:
			if result := strings.TrimSpace(typed); result != "" {
				return result
			}
		case fmt.Stringer:
			if result := strings.TrimSpace(typed.String()); result != "" {
				return result
			}
		case float64:
			return fmt.Sprintf("%g", typed)
		}
	}
	return ""
}
