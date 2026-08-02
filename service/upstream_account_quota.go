package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
)

const (
	upstreamAccountQuotaTimeout     = 20 * time.Second
	upstreamAccountQuotaBodyLimit   = 1 << 20
	upstreamAccountUsagePath        = "/backend-api/wham/usage"
	upstreamAccountCreditsPath      = "/backend-api/wham/rate-limit-reset-credits"
	upstreamAccountResetCreditsPath = "/backend-api/wham/rate-limit-reset-credits/consume"
	upstreamAccountQuotaCacheTTL    = 5 * time.Minute
)

type UpstreamAccountQuotaQueryOptions struct {
	Force          bool
	IncludeCredits bool
}

type upstreamAccountQuotaCacheEntry struct {
	usage             UpstreamAccountQuotaUsage
	cachedAt          time.Time
	includeCredits    bool
	credentialVersion int64
}

var (
	upstreamAccountQuotaCache  sync.Map
	upstreamAccountQuotaFlight singleflight.Group
)

type UpstreamAccountRateLimitWindow struct {
	UsedPercent        float64                     `json:"used_percent"`
	LimitWindowSeconds int64                       `json:"limit_window_seconds"`
	ResetAfterSeconds  int64                       `json:"reset_after_seconds"`
	ResetAt            int64                       `json:"reset_at"`
	WindowStats        *UpstreamAccountWindowStats `json:"window_stats,omitempty"`
}

type UpstreamAccountRateLimit struct {
	PlanType        string                          `json:"plan_type,omitempty"`
	Allowed         bool                            `json:"allowed"`
	LimitReached    bool                            `json:"limit_reached"`
	PrimaryWindow   *UpstreamAccountRateLimitWindow `json:"primary_window,omitempty"`
	SecondaryWindow *UpstreamAccountRateLimitWindow `json:"secondary_window,omitempty"`
}

type UpstreamAccountAdditionalRateLimit struct {
	LimitName      string                    `json:"limit_name,omitempty"`
	MeteredFeature string                    `json:"metered_feature,omitempty"`
	RateLimit      *UpstreamAccountRateLimit `json:"rate_limit,omitempty"`
}

type UpstreamAccountResetCreditDetail struct {
	ExpiresAt string `json:"expires_at,omitempty"`
}

type UpstreamAccountResetCredits struct {
	AvailableCount int                                `json:"available_count"`
	Credits        []UpstreamAccountResetCreditDetail `json:"credits,omitempty"`
}

type UpstreamAccountQuotaUsage struct {
	UserId                string                               `json:"user_id,omitempty"`
	AccountId             string                               `json:"account_id,omitempty"`
	Email                 string                               `json:"email,omitempty"`
	PlanType              string                               `json:"plan_type,omitempty"`
	RateLimit             *UpstreamAccountRateLimit            `json:"rate_limit,omitempty"`
	AdditionalRateLimits  []UpstreamAccountAdditionalRateLimit `json:"additional_rate_limits,omitempty"`
	RateLimitResetCredits *UpstreamAccountResetCredits         `json:"rate_limit_reset_credits,omitempty"`
	FetchedAt             int64                                `json:"fetched_at"`
}

type UpstreamAccountQuotaResetCredit struct {
	Id              string `json:"id,omitempty"`
	ResetType       string `json:"reset_type,omitempty"`
	Status          string `json:"status,omitempty"`
	GrantedAt       string `json:"granted_at,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	RedeemStartedAt string `json:"redeem_started_at,omitempty"`
	RedeemedAt      string `json:"redeemed_at,omitempty"`
}

type UpstreamAccountQuotaResetResult struct {
	Code         string                           `json:"code"`
	Credit       *UpstreamAccountQuotaResetCredit `json:"credit,omitempty"`
	WindowsReset int                              `json:"windows_reset"`
}

type upstreamAccountQuotaCall struct {
	client      *http.Client
	baseURL     string
	accessToken string
	accountId   string
}

type upstreamAccountQuotaHTTPError struct {
	statusCode int
	message    string
}

func (err *upstreamAccountQuotaHTTPError) Error() string {
	return err.message
}

func QueryUpstreamAccountQuota(ctx context.Context, accountId int, queryOptions ...UpstreamAccountQuotaQueryOptions) (*UpstreamAccountQuotaUsage, error) {
	if accountId <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	var account model.UpstreamAccount
	if err := model.DB.Select("id", "credential_version").First(&account, accountId).Error; err != nil {
		return nil, err
	}
	options := UpstreamAccountQuotaQueryOptions{IncludeCredits: true}
	if len(queryOptions) > 0 {
		options = queryOptions[0]
	}
	if !options.Force {
		if cached := cachedUpstreamAccountQuota(accountId, account.CredentialVersion, options.IncludeCredits); cached != nil {
			attachOpenAIWindowStats(&account, cached, time.Now())
			return cached, nil
		}
	}
	flightKey := strconv.Itoa(accountId) + ":" + strconv.FormatInt(account.CredentialVersion, 10) + ":" + strconv.FormatBool(options.IncludeCredits)
	result, err, _ := upstreamAccountQuotaFlight.Do(flightKey, func() (any, error) {
		if !options.Force {
			if cached := cachedUpstreamAccountQuota(accountId, account.CredentialVersion, options.IncludeCredits); cached != nil {
				return cached, nil
			}
		}
		usage, queryErr := queryUpstreamAccountQuota(ctx, accountId, options.IncludeCredits)
		if queryErr != nil {
			return nil, queryErr
		}
		upstreamAccountQuotaCache.Store(accountId, upstreamAccountQuotaCacheEntry{
			usage: *usage, cachedAt: time.Now(), includeCredits: options.IncludeCredits, credentialVersion: account.CredentialVersion,
		})
		return usage, nil
	})
	if err != nil {
		return nil, err
	}
	usage, _ := result.(*UpstreamAccountQuotaUsage)
	attachOpenAIWindowStats(&account, usage, time.Now())
	return usage, nil
}

func attachOpenAIWindowStats(account *model.UpstreamAccount, usage *UpstreamAccountQuotaUsage, now time.Time) {
	if account == nil || usage == nil {
		return
	}
	fiveHour, sevenDay := quotaUsageWindows(usage)
	fiveStats, fiveErr := queryUpstreamWindowStats(account.Id, upstreamWindowStatsStart(windowResetAt(fiveHour), 5*time.Hour, now))
	if fiveErr == nil {
		if fiveHour == nil {
			fiveHour = &UpstreamAccountRateLimitWindow{LimitWindowSeconds: int64((5 * time.Hour).Seconds())}
			setQuotaUsageWindow(usage, fiveHour, false)
		}
		fiveHour.WindowStats = fiveStats
	}
	sevenStats, sevenErr := queryUpstreamWindowStats(account.Id, upstreamWindowStatsStart(windowResetAt(sevenDay), 7*24*time.Hour, now))
	if sevenErr == nil {
		if sevenDay == nil {
			sevenDay = &UpstreamAccountRateLimitWindow{LimitWindowSeconds: int64((7 * 24 * time.Hour).Seconds())}
			setQuotaUsageWindow(usage, sevenDay, true)
		}
		sevenDay.WindowStats = sevenStats
	}
}

func quotaUsageWindows(usage *UpstreamAccountQuotaUsage) (*UpstreamAccountRateLimitWindow, *UpstreamAccountRateLimitWindow) {
	if usage == nil || usage.RateLimit == nil {
		return nil, nil
	}
	primary := usage.RateLimit.PrimaryWindow
	secondary := usage.RateLimit.SecondaryWindow
	if primary != nil && primary.LimitWindowSeconds >= 24*60*60 {
		return secondary, primary
	}
	if secondary != nil && secondary.LimitWindowSeconds < 24*60*60 {
		return secondary, primary
	}
	return primary, secondary
}

func windowResetAt(window *UpstreamAccountRateLimitWindow) int64 {
	if window == nil {
		return 0
	}
	return window.ResetAt
}

func setQuotaUsageWindow(usage *UpstreamAccountQuotaUsage, window *UpstreamAccountRateLimitWindow, weekly bool) {
	if usage.RateLimit == nil {
		usage.RateLimit = &UpstreamAccountRateLimit{Allowed: true}
	}
	if weekly {
		usage.RateLimit.SecondaryWindow = window
	} else {
		usage.RateLimit.PrimaryWindow = window
	}
}

func cachedUpstreamAccountQuota(accountId int, credentialVersion int64, includeCredits bool) *UpstreamAccountQuotaUsage {
	if accountId <= 0 {
		return nil
	}
	value, ok := upstreamAccountQuotaCache.Load(accountId)
	if !ok {
		return nil
	}
	entry, ok := value.(upstreamAccountQuotaCacheEntry)
	if !ok || entry.credentialVersion != credentialVersion || time.Since(entry.cachedAt) >= upstreamAccountQuotaCacheTTL || (includeCredits && !entry.includeCredits) {
		upstreamAccountQuotaCache.Delete(accountId)
		return nil
	}
	usage := entry.usage
	return &usage
}

func queryUpstreamAccountQuota(ctx context.Context, accountId int, includeCredits bool) (*UpstreamAccountQuotaUsage, error) {
	call, err := prepareUpstreamAccountQuotaCall(ctx, accountId)
	if err != nil {
		return nil, err
	}
	var usage UpstreamAccountQuotaUsage
	if err := call.request(ctx, http.MethodGet, upstreamAccountUsagePath, nil, &usage); err != nil {
		refreshed, retryErr := refreshUpstreamAccountQuotaCall(ctx, accountId, err)
		if retryErr != nil {
			return nil, retryErr
		}
		if refreshed == nil {
			return nil, err
		}
		call = refreshed
		if err := call.request(ctx, http.MethodGet, upstreamAccountUsagePath, nil, &usage); err != nil {
			return nil, err
		}
	}
	if includeCredits {
		var creditPayload any
		if err := call.request(ctx, http.MethodGet, upstreamAccountCreditsPath, nil, &creditPayload); err == nil {
			if credits := parseUpstreamAccountResetCredits(creditPayload); credits != nil {
				usage.RateLimitResetCredits = credits
			}
		}
	}
	usage.FetchedAt = time.Now().Unix()
	return &usage, nil
}

func parseUpstreamAccountResetCredits(payload any) *UpstreamAccountResetCredits {
	credits := &UpstreamAccountResetCredits{}
	var rawCredits []any
	switch value := payload.(type) {
	case []any:
		rawCredits = value
	case map[string]any:
		for _, key := range []string{"available_count", "availableCount"} {
			if count, ok := quotaNumber(value[key]); ok {
				credits.AvailableCount = int(count)
				break
			}
		}
		for _, key := range []string{"credits", "rate_limit_reset_credits", "items", "data"} {
			if items, ok := value[key].([]any); ok {
				rawCredits = items
				break
			}
		}
	default:
		return nil
	}
	availableFromItems := 0
	for _, item := range rawCredits {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		resetType := quotaString(entry, "reset_type", "resetType")
		if resetType != "" && !strings.EqualFold(resetType, "codex_rate_limits") {
			continue
		}
		status := quotaString(entry, "status")
		if status != "" && !strings.EqualFold(status, "available") {
			continue
		}
		availableFromItems++
		if expiresAt := quotaString(entry, "expires_at", "expiresAt"); expiresAt != "" {
			credits.Credits = append(credits.Credits, UpstreamAccountResetCreditDetail{ExpiresAt: expiresAt})
		}
	}
	if credits.AvailableCount == 0 && len(rawCredits) > 0 {
		credits.AvailableCount = availableFromItems
	}
	return credits
}

func quotaString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func quotaNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		var parsed float64
		if _, err := fmt.Sscan(strings.TrimSpace(typed), &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func ResetUpstreamAccountQuota(ctx context.Context, accountId int) (*UpstreamAccountQuotaResetResult, error) {
	call, err := prepareUpstreamAccountQuotaCall(ctx, accountId)
	if err != nil {
		return nil, err
	}
	payload := map[string]string{"redeem_request_id": uuid.NewString()}
	var result UpstreamAccountQuotaResetResult
	if err := call.request(ctx, http.MethodPost, upstreamAccountResetCreditsPath, payload, &result); err != nil {
		refreshed, retryErr := refreshUpstreamAccountQuotaCall(ctx, accountId, err)
		if retryErr != nil {
			return nil, retryErr
		}
		if refreshed == nil {
			return nil, err
		}
		if err := refreshed.request(ctx, http.MethodPost, upstreamAccountResetCreditsPath, payload, &result); err != nil {
			return nil, err
		}
	}
	upstreamAccountQuotaCache.Delete(accountId)
	return &result, nil
}

func refreshUpstreamAccountQuotaCall(ctx context.Context, accountId int, requestErr error) (*upstreamAccountQuotaCall, error) {
	var upstreamErr *upstreamAccountQuotaHTTPError
	if !errors.As(requestErr, &upstreamErr) || (upstreamErr.statusCode != http.StatusUnauthorized && upstreamErr.statusCode != http.StatusForbidden) {
		return nil, nil
	}
	var account model.UpstreamAccount
	if err := model.DB.First(&account, accountId).Error; err != nil {
		return nil, err
	}
	if account.OAuthRefreshOwner != constant.UpstreamOAuthRefreshOwnerStarNexus {
		return nil, nil
	}
	if _, err := RefreshUpstreamOAuthAccount(ctx, accountId); err != nil {
		return nil, err
	}
	return prepareUpstreamAccountQuotaCall(ctx, accountId)
}

func prepareUpstreamAccountQuotaCall(ctx context.Context, accountId int) (*upstreamAccountQuotaCall, error) {
	if accountId <= 0 {
		return nil, errors.New("invalid upstream account id")
	}
	var account model.UpstreamAccount
	if err := model.DB.First(&account, accountId).Error; err != nil {
		return nil, err
	}
	if account.Platform != constant.UpstreamPlatformOpenAI || account.Type != constant.UpstreamAccountTypeOAuth {
		return nil, errors.New("quota operations require an OpenAI OAuth account")
	}
	credentials, err := DecryptUpstreamAccountCredentials(&account)
	if err != nil {
		return nil, errors.New("account credential cannot be decrypted")
	}
	accessToken := upstreamCredentialMapString(credentials, "access_token")
	chatGPTAccountId := upstreamCredentialMapString(credentials, "account_id")
	if chatGPTAccountId == "" {
		chatGPTAccountId = upstreamCredentialMapString(credentials, "chatgpt_account_id")
	}
	if chatGPTAccountId == "" {
		chatGPTAccountId = upstreamCredentialMapString(credentials, "organization_id")
	}
	if accessToken == "" || chatGPTAccountId == "" {
		return nil, errors.New("OpenAI OAuth credential is missing access_token or account_id")
	}
	_, proxyURL, err := resolveUpstreamProxy(ctx, &account, nil, "")
	if err != nil {
		return nil, err
	}
	client, err := NewProxyHttpClient(proxyURL)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(upstreamCredentialMapString(credentials, "base_url"), "/")
	if baseURL == "" {
		baseURL = "https://chatgpt.com"
	}
	return &upstreamAccountQuotaCall{client: client, baseURL: baseURL, accessToken: accessToken, accountId: chatGPTAccountId}, nil
}

func (call *upstreamAccountQuotaCall) request(ctx context.Context, method string, path string, payload any, target any) error {
	if call == nil || call.client == nil {
		return errors.New("quota client is unavailable")
	}
	callCtx, cancel := context.WithTimeout(ctx, upstreamAccountQuotaTimeout)
	defer cancel()
	var body io.Reader
	if payload != nil {
		encoded, err := common.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(callCtx, method, call.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+call.accessToken)
	request.Header.Set("chatgpt-account-id", call.accountId)
	request.Header.Set("openai-beta", "codex-1")
	request.Header.Set("oai-language", "zh-CN")
	request.Header.Set("originator", "Codex Desktop")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Sec-Fetch-Site", "none")
	request.Header.Set("Sec-Fetch-Mode", "no-cors")
	request.Header.Set("Sec-Fetch-Dest", "empty")
	request.Header.Set("Priority", "u=4, i")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := call.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, upstreamAccountQuotaBodyLimit))
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(responseBody))
		if len(message) > 240 {
			message = message[:240]
		}
		return &upstreamAccountQuotaHTTPError{
			statusCode: response.StatusCode,
			message:    fmt.Sprintf("OpenAI quota upstream returned %d: %s", response.StatusCode, message),
		}
	}
	if target == nil || len(responseBody) == 0 {
		return nil
	}
	if err := common.Unmarshal(responseBody, target); err != nil {
		return fmt.Errorf("failed to decode OpenAI quota response: %w", err)
	}
	return nil
}
