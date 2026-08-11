package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/imroc/req/v3"
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
	info, _ := fetchChatGPTAccountSubscriptionInfoDetailed(ctx, accessToken, proxyURL, accountID)
	return info
}

func fetchChatGPTAccountSubscriptionInfoDetailed(ctx context.Context, accessToken, proxyURL, accountID string) (*chatGPTAccountSubscriptionInfo, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, errors.New("OpenAI subscription metadata requires an access token")
	}
	client, err := newChatGPTBrowserClient(proxyURL)
	if err != nil {
		return nil, err
	}
	info, accountsErr := fetchChatGPTAccountsCheck(ctx, client, accessToken, accountID)
	if info == nil {
		info = &chatGPTAccountSubscriptionInfo{}
	}
	var subscriptionErr error
	if info.ExpiresAt == "" && strings.TrimSpace(accountID) != "" {
		if plan, expiresAt, err := fetchChatGPTSubscription(ctx, client, accessToken, accountID); err != nil {
			subscriptionErr = err
		} else if expiresAt != "" {
			info.ExpiresAt = expiresAt
			if info.PlanType == "" {
				info.PlanType = plan
			}
		}
	}
	if info.PlanType == "" && info.ExpiresAt == "" {
		return nil, errors.Join(accountsErr, subscriptionErr)
	}
	return info, errors.Join(accountsErr, subscriptionErr)
}

func newChatGPTBrowserClient(proxyURL string) (*req.Client, error) {
	client := req.C().SetTimeout(30 * time.Second).ImpersonateChrome()
	if proxyURL = strings.TrimSpace(proxyURL); proxyURL != "" {
		parsedProxy, err := url.Parse(proxyURL)
		if err != nil {
			return nil, err
		}
		switch strings.ToLower(parsedProxy.Scheme) {
		case "http", "https", "socks5", "socks5h":
		default:
			return nil, fmt.Errorf("unsupported proxy scheme %q", parsedProxy.Scheme)
		}
		client.SetProxyURL(proxyURL)
	}
	return client, nil
}

func fetchChatGPTAccountsCheck(ctx context.Context, client *req.Client, accessToken, accountID string) (*chatGPTAccountSubscriptionInfo, error) {
	var result struct {
		Accounts map[string]map[string]any `json:"accounts"`
	}
	if err := requestChatGPTSubscriptionJSON(ctx, client, accessToken, chatGPTAccountsCheckURL, &result); err != nil {
		return nil, err
	}
	now := time.Now()
	if account := result.Accounts[strings.TrimSpace(accountID)]; usableChatGPTAccount(account, now) {
		return extractChatGPTAccountSubscription(account), nil
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
		return defaultInfo, nil
	}
	if paidInfo != nil {
		return paidInfo, nil
	}
	return anyInfo, nil
}

func fetchChatGPTSubscription(ctx context.Context, client *req.Client, accessToken, accountID string) (string, string, error) {
	var result struct {
		PlanType    string `json:"plan_type"`
		ActiveUntil string `json:"active_until"`
	}
	endpoint := chatGPTSubscriptionsURL + "?account_id=" + url.QueryEscape(accountID)
	if err := requestChatGPTSubscriptionJSON(ctx, client, accessToken, endpoint, &result); err != nil {
		return "", "", err
	}
	expiresAt := strings.TrimSpace(result.ActiveUntil)
	if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		return "", "", errors.New("ChatGPT subscription response contains an invalid active_until")
	}
	return strings.TrimSpace(result.PlanType), expiresAt, nil
}

func requestChatGPTSubscriptionJSON(ctx context.Context, client *req.Client, accessToken, url string, target any) error {
	requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	response, err := client.R().
		SetContext(requestCtx).
		SetHeader("Authorization", "Bearer "+accessToken).
		SetHeader("Origin", "https://chatgpt.com").
		SetHeader("Referer", "https://chatgpt.com/").
		SetHeader("Accept", "application/json").
		Get(url)
	if err != nil {
		return err
	}
	if !response.IsSuccessState() {
		return fmt.Errorf("ChatGPT subscription endpoint returned status %d", response.StatusCode)
	}
	body := response.Bytes()
	if len(body) > upstreamAccountSubscriptionBodyLimit {
		return errors.New("ChatGPT subscription response exceeds size limit")
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
	identity, identityOK := extractCodexOAuthIdentityFromCredentials(credentials)
	if identityOK {
		if identity.Email != "" {
			credentials["email"] = identity.Email
		}
		if identity.PlanType != "" {
			credentials["plan_type"] = identity.PlanType
		}
		if identity.SubscriptionExpiresAt != "" {
			credentials["subscription_expires_at"] = identity.SubscriptionExpiresAt
		}
	}
	accountID := upstreamCredentialMapString(credentials, "account_id")
	if accountID == "" {
		accountID = upstreamCredentialMapString(credentials, "chatgpt_account_id")
	}
	info, err := fetchChatGPTAccountSubscriptionInfoDetailed(ctx, upstreamCredentialMapString(credentials, "access_token"), proxyURL, accountID)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("OpenAI subscription metadata enrichment failed: account_id_present=%t error=%v", accountID != "", err))
	}
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
