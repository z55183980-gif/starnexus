package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

const upstreamAccountSubscriptionBodyLimit = 1 << 20

var (
	chatGPTAccountsCheckURL = "https://chatgpt.com/backend-api/accounts/check/v4-2023-04-27"
	chatGPTSubscriptionsURL = "https://chatgpt.com/backend-api/subscriptions"
)

type chatGPTAccountSubscriptionInfo struct {
	PlanType  string
	ExpiresAt string
}

// fetchChatGPTAccountSubscriptionInfo mirrors the ChatGPT account enrichment
// used by sub2api. accounts/check is preferred; subscriptions is a fallback for
// Plus accounts whose entitlement no longer includes expires_at.
func fetchChatGPTAccountSubscriptionInfo(ctx context.Context, accessToken, proxyURL, accountID string) *chatGPTAccountSubscriptionInfo {
	if strings.TrimSpace(accessToken) == "" {
		return nil
	}
	client, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		return nil
	}
	info := fetchChatGPTAccountsCheck(ctx, client, accessToken, accountID)
	if info == nil {
		info = &chatGPTAccountSubscriptionInfo{}
	}
	if info.ExpiresAt == "" && strings.TrimSpace(accountID) != "" {
		if plan, expiresAt := fetchChatGPTSubscription(ctx, client, accessToken, accountID); expiresAt != "" {
			info.ExpiresAt = expiresAt
			if info.PlanType == "" {
				info.PlanType = plan
			}
		}
	}
	if info.PlanType == "" && info.ExpiresAt == "" {
		return nil
	}
	return info
}

func fetchChatGPTAccountsCheck(ctx context.Context, client *http.Client, accessToken, accountID string) *chatGPTAccountSubscriptionInfo {
	var result struct {
		Accounts map[string]map[string]any `json:"accounts"`
	}
	if err := requestChatGPTSubscriptionJSON(ctx, client, accessToken, chatGPTAccountsCheckURL, &result); err != nil {
		return nil
	}
	now := time.Now()
	if account := result.Accounts[strings.TrimSpace(accountID)]; usableChatGPTAccount(account, now) {
		return extractChatGPTAccountSubscription(account)
	}
	var defaultInfo, paidInfo, anyInfo *chatGPTAccountSubscriptionInfo
	for _, account := range result.Accounts {
		if !usableChatGPTAccount(account, now) {
			continue
		}
		candidate := extractChatGPTAccountSubscription(account)
		if candidate == nil || candidate.PlanType == "" {
			continue
		}
		if anyInfo == nil {
			anyInfo = candidate
		}
		if details, ok := account["account"].(map[string]any); ok {
			if isDefault, _ := details["is_default"].(bool); isDefault {
				defaultInfo = candidate
			}
		}
		if paidInfo == nil && !strings.EqualFold(candidate.PlanType, "free") {
			paidInfo = candidate
		}
	}
	if defaultInfo != nil {
		return defaultInfo
	}
	if paidInfo != nil {
		return paidInfo
	}
	return anyInfo
}

func fetchChatGPTSubscription(ctx context.Context, client *http.Client, accessToken, accountID string) (string, string) {
	var result struct {
		PlanType    string `json:"plan_type"`
		ActiveUntil string `json:"active_until"`
	}
	endpoint := chatGPTSubscriptionsURL + "?account_id=" + url.QueryEscape(accountID)
	if err := requestChatGPTSubscriptionJSON(ctx, client, accessToken, endpoint, &result); err != nil {
		return "", ""
	}
	expiresAt := strings.TrimSpace(result.ActiveUntil)
	if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		return "", ""
	}
	return strings.TrimSpace(result.PlanType), expiresAt
}

func requestChatGPTSubscriptionJSON(ctx context.Context, client *http.Client, accessToken, url string, target any) error {
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Origin", "https://chatgpt.com")
	request.Header.Set("Referer", "https://chatgpt.com/")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return errors.New("ChatGPT subscription endpoint returned a non-success status")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, upstreamAccountSubscriptionBodyLimit))
	if err != nil {
		return err
	}
	return common.Unmarshal(body, target)
}

func extractChatGPTAccountSubscription(account map[string]any) *chatGPTAccountSubscriptionInfo {
	if account == nil {
		return nil
	}
	info := &chatGPTAccountSubscriptionInfo{}
	if details, ok := account["account"].(map[string]any); ok {
		info.PlanType, _ = details["plan_type"].(string)
	}
	if entitlement, ok := account["entitlement"].(map[string]any); ok {
		if info.PlanType == "" {
			info.PlanType, _ = entitlement["subscription_plan"].(string)
		}
		if expiresAt, _ := entitlement["expires_at"].(string); validRFC3339(expiresAt) {
			info.ExpiresAt = strings.TrimSpace(expiresAt)
		}
	}
	return info
}

func usableChatGPTAccount(account map[string]any, now time.Time) bool {
	if account == nil || chatGPTAccountDeactivated(account) {
		return false
	}
	if details, ok := account["account"].(map[string]any); ok && chatGPTAccountDeactivated(details) {
		return false
	}
	info := extractChatGPTAccountSubscription(account)
	if info == nil || info.ExpiresAt == "" {
		return true
	}
	expiresAt, err := time.Parse(time.RFC3339, info.ExpiresAt)
	return err != nil || expiresAt.After(now)
}

func chatGPTAccountDeactivated(value map[string]any) bool {
	for _, key := range []string{"deactivated", "is_deactivated", "disabled", "is_disabled"} {
		if marked, _ := value[key].(bool); marked {
			return true
		}
	}
	for _, key := range []string{"deactivated_at", "disabled_at", "deleted_at"} {
		if marked, _ := value[key].(string); strings.TrimSpace(marked) != "" {
			return true
		}
	}
	status, _ := value["status"].(string)
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "deactivated", "disabled", "deleted", "inactive", "suspended":
		return true
	}
	return false
}

func validRFC3339(value string) bool {
	_, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	return err == nil
}

func enrichUpstreamOpenAICredentials(ctx context.Context, credentials map[string]any, proxyURL string) {
	accountID := upstreamCredentialMapString(credentials, "account_id")
	if accountID == "" {
		accountID = upstreamCredentialMapString(credentials, "chatgpt_account_id")
	}
	info := fetchChatGPTAccountSubscriptionInfo(ctx, upstreamCredentialMapString(credentials, "access_token"), proxyURL, accountID)
	if info == nil {
		return
	}
	if info.PlanType != "" {
		credentials["plan_type"] = info.PlanType
	}
	if info.ExpiresAt != "" {
		credentials["subscription_expires_at"] = info.ExpiresAt
	}
}
