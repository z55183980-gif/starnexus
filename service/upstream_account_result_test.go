package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestApplyUpstreamAccountErrorOwnership(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "result-account")

	clientErr := types.NewErrorWithStatusCode(errors.New("bad input"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	require.Equal(t, UpstreamAccountErrorNotHandled, ApplyUpstreamAccountError(account.Id, 0, clientErr))

	rateErr := types.NewErrorWithStatusCode(errors.New("rate limited token=secret"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, rateErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.NotNil(t, updated.RateLimitResetAt)
	require.NotContains(t, updated.ErrorMessage, "secret")

	authErr := types.NewErrorWithStatusCode(errors.New("invalid bearer"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, authErr))
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.Equal(t, constant.UpstreamStatusError, updated.Status)
	require.False(t, updated.Schedulable)
}

func TestUpstreamHTTPServerErrorDoesNotMarkProxyUnhealthy(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	proxyInput := UpstreamProxyCreateInput{
		Proxy: model.UpstreamProxy{
			Name: "proxy", Protocol: constant.UpstreamProxyProtocolHTTP,
			Host: "127.0.0.1", Port: 8080, Status: constant.UpstreamStatusActive,
			FallbackMode: constant.UpstreamProxyFallbackNone,
		},
	}
	require.NoError(t, CreateUpstreamProxy(&proxyInput))
	account := createRouterTestAccountWithoutPool(t, "server-error-account")
	serverErr := types.NewErrorWithStatusCode(errors.New("upstream unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	require.Equal(t, UpstreamAccountErrorGlobal, ApplyUpstreamAccountError(account.Id, proxyInput.Proxy.Id, serverErr))
	var unchanged model.UpstreamAccount
	require.NoError(t, model.DB.First(&unchanged, account.Id).Error)
	require.Nil(t, unchanged.TempUnschedulableUntil)
	require.Empty(t, unchanged.ErrorMessage)
	var proxy model.UpstreamProxy
	require.NoError(t, model.DB.First(&proxy, proxyInput.Proxy.Id).Error)
	require.Empty(t, proxy.LatencyStatus)

	transportErr := types.NewErrorWithStatusCode(errors.New("connection reset"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	require.Equal(t, UpstreamAccountErrorRetryTransport, ApplyUpstreamAccountError(account.Id, proxyInput.Proxy.Id, transportErr))
	require.NoError(t, model.DB.First(&proxy, proxyInput.Proxy.Id).Error)
	require.Equal(t, "error", proxy.LatencyStatus)
	require.NoError(t, model.DB.First(&unchanged, account.Id).Error)
	require.Nil(t, unchanged.TempUnschedulableUntil)
}

func TestApplyUpstreamAccountErrorUsesCodexResetWindow(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "rate-window-account")
	apiErr := types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	header := http.Header{}
	header.Set("x-codex-primary-used-percent", "100")
	header.Set("x-codex-primary-reset-after-seconds", "7200")
	header.Set("x-codex-primary-window-minutes", "10080")
	header.Set("x-codex-secondary-used-percent", "100")
	header.Set("x-codex-secondary-reset-after-seconds", "300")
	header.Set("x-codex-secondary-window-minutes", "300")
	apiErr.SetUpstreamResponse(header, nil)

	before := time.Now().Unix()
	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.NotNil(t, updated.RateLimitResetAt)
	require.GreaterOrEqual(t, *updated.RateLimitResetAt, before+7200)
	require.NotNil(t, updated.SessionWindowEnd)
	require.GreaterOrEqual(t, *updated.SessionWindowEnd, before+300)
	require.Equal(t, "rejected", updated.SessionWindowStatus)
	require.Equal(t, constant.UpstreamStatusActive, updated.Status)
	require.True(t, updated.Schedulable)
	require.False(t, updated.IsSchedulableAt(time.Now().Unix()))
}

func TestApplyUpstreamAccountErrorStoresRevokedTokenMessage(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "revoked-token-account")
	apiErr := types.NewErrorWithStatusCode(errors.New("unauthorized"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	apiErr.SetUpstreamResponse(nil, []byte(`{"error":{"code":"token_revoked","message":"Encountered invalidated oauth token for user, failing request"}}`))

	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.Equal(t, constant.UpstreamStatusError, updated.Status)
	require.False(t, updated.Schedulable)
	require.Equal(t, "Token revoked (401): Encountered invalidated oauth token for user, failing request", updated.ErrorMessage)
}

func TestApplyUpstreamAccountErrorUsesOverloadCooldown(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "overloaded-account")
	apiErr := types.NewErrorWithStatusCode(errors.New("overloaded"), types.ErrorCodeBadResponseStatusCode, 529)
	apiErr.SetUpstreamResponse(http.Header{"Content-Type": []string{"application/json"}}, []byte(`{"error":{"message":"Overloaded"}}`))

	before := time.Now().Unix()
	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.NotNil(t, updated.OverloadUntil)
	require.GreaterOrEqual(t, *updated.OverloadUntil, before+int64((10*time.Minute).Seconds()))
	require.Equal(t, constant.UpstreamStatusActive, updated.Status)
	require.True(t, updated.Schedulable)
	require.False(t, updated.IsSchedulableAt(time.Now().Unix()))
}

func TestApplyUpstreamAccountErrorMatchesTempUnschedulableRules(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "temp-unsched-rule-account")
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).Update(
		"extra",
		`{"temp_unschedulable_enabled":true,"temp_unschedulable_rules":[{"error_code":503,"keywords":["unavailable","maintenance"],"duration_minutes":30,"description":"Service unavailable - pause 30 minutes"}]}`,
	).Error)

	apiErr := types.NewErrorWithStatusCode(errors.New("unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
	apiErr.SetUpstreamResponse(nil, []byte(`{"error":{"message":"service unavailable for maintenance"}}`))

	before := time.Now().Unix()
	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.NotNil(t, updated.TempUnschedulableUntil)
	require.GreaterOrEqual(t, *updated.TempUnschedulableUntil, before+30*60)
	require.Equal(t, "Service unavailable - pause 30 minutes", updated.TempUnschedulableReason)
	require.Equal(t, constant.UpstreamStatusActive, updated.Status)
	require.True(t, updated.Schedulable)
	require.False(t, updated.IsSchedulableAt(time.Now().Unix()))
}

func TestApplyUpstreamAccountErrorTempUnschedulableRulesPreferCredentials(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "temp-unsched-cred-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: `{"temp_unschedulable_enabled":false}`, Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{
			"access_token":               "secret",
			"account_id":                 "acct-temp",
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       429,
					"keywords":         []any{"rate limit", "too many requests"},
					"duration_minutes": 10,
					"description":      "Rate limited - pause 10 minutes",
				},
			},
		},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	apiErr := types.NewErrorWithStatusCode(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	apiErr.SetUpstreamResponse(nil, []byte(`{"error":{"message":"too many requests"}}`))

	before := time.Now().Unix()
	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(input.Account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, input.Account.Id).Error)
	require.NotNil(t, updated.TempUnschedulableUntil)
	require.GreaterOrEqual(t, *updated.TempUnschedulableUntil, before+10*60)
	require.Nil(t, updated.RateLimitResetAt)
	require.Equal(t, "Rate limited - pause 10 minutes", updated.TempUnschedulableReason)
}

func TestApplyUpstreamAccountErrorTempUnschedulableMatchesErrorTextWithoutBody(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "temp-unsched-error-text")
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).Update(
		"extra",
		`{"temp_unschedulable_enabled":true,"temp_unschedulable_rules":[{"error_code":429,"keywords":["rate limit"],"duration_minutes":15,"description":"rate limit rule"}]}`,
	).Error)

	apiErr := types.NewErrorWithStatusCode(errors.New("rate limited by upstream"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)

	before := time.Now().Unix()
	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.NotNil(t, updated.TempUnschedulableUntil)
	require.GreaterOrEqual(t, *updated.TempUnschedulableUntil, before+15*60)
	require.Nil(t, updated.RateLimitResetAt)
	require.Equal(t, "rate limit rule", updated.TempUnschedulableReason)
}

func TestShouldRetryUpstreamAccountHonorsCountAndBudget(t *testing.T) {
	t.Setenv("UPSTREAM_ACCOUNT_MAX_FAILOVERS", "2")
	t.Setenv("UPSTREAM_ACCOUNT_FAILOVER_BUDGET_MS", "100")
	require.True(t, ShouldRetryUpstreamAccount(0, time.Now()))
	require.False(t, ShouldRetryUpstreamAccount(2, time.Now()))
	require.False(t, ShouldRetryUpstreamAccount(0, time.Now().Add(-time.Second)))
}

func createRouterTestAccountWithoutPool(t *testing.T, name string) model.UpstreamAccount {
	t.Helper()
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: map[string]any{"access_token": name + "-secret", "account_id": "acct-" + name},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	return input.Account
}
