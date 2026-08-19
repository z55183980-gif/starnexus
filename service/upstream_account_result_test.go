package service

import (
	"errors"
	"fmt"
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

func TestApplyUpstreamAccountErrorFansOutDeactivatedTeamWorkspace(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	teamID := fmt.Sprintf("team-%d", time.Now().UnixNano())
	trigger := createTeamLinkedTestAccount(t, "team-trigger", constant.UpstreamAccountTypeOAuth, teamID)
	sibling := createTeamLinkedTestAccount(t, "team-sibling", constant.UpstreamAccountTypeOAuth, teamID)
	otherTeam := createTeamLinkedTestAccount(t, "team-other", constant.UpstreamAccountTypeOAuth, teamID+"-other")
	apiKey := createTeamLinkedTestAccount(t, "team-apikey", constant.UpstreamAccountTypeAPIKey, teamID)

	apiErr := types.NewErrorWithStatusCode(errors.New("workspace deactivated"), types.ErrorCodeBadResponseStatusCode, http.StatusPaymentRequired)
	apiErr.SetUpstreamResponse(nil, []byte(`{"detail":{"code":"deactivated_workspace"}}`))

	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(trigger.Id, 0, apiErr))

	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, sibling.Id).Error)
	require.Equal(t, constant.UpstreamStatusError, updated.Status)
	require.False(t, updated.Schedulable)
	require.Contains(t, updated.ErrorMessage, fmt.Sprintf("account #%d", trigger.Id))
	require.NoError(t, model.DB.First(&updated, otherTeam.Id).Error)
	require.Equal(t, constant.UpstreamStatusActive, updated.Status)
	require.True(t, updated.Schedulable)
	require.NoError(t, model.DB.First(&updated, apiKey.Id).Error)
	require.Equal(t, constant.UpstreamStatusActive, updated.Status)
	require.True(t, updated.Schedulable)
	require.True(t, isUpstreamAccountRuntimeBlocked(sibling.Id, time.Now()))
	require.NoError(t, RecoverUpstreamAccountRuntimeState(sibling.Id, UpstreamAccountRecoveryAll))
	require.False(t, isUpstreamAccountRuntimeBlocked(sibling.Id, time.Now()))
}

func TestApplyUpstreamAccountErrorDoesNotFanOutGenericPaymentRequired(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	teamID := fmt.Sprintf("team-generic-%d", time.Now().UnixNano())
	trigger := createTeamLinkedTestAccount(t, "generic-trigger", constant.UpstreamAccountTypeOAuth, teamID)
	sibling := createTeamLinkedTestAccount(t, "generic-sibling", constant.UpstreamAccountTypeOAuth, teamID)

	apiErr := types.NewErrorWithStatusCode(errors.New("insufficient balance"), types.ErrorCodeBadResponseStatusCode, http.StatusPaymentRequired)
	apiErr.SetUpstreamResponse(nil, []byte(`{"error":{"message":"insufficient balance"}}`))

	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(trigger.Id, 0, apiErr))
	var unchanged model.UpstreamAccount
	require.NoError(t, model.DB.First(&unchanged, sibling.Id).Error)
	require.Equal(t, constant.UpstreamStatusActive, unchanged.Status)
	require.True(t, unchanged.Schedulable)
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

func TestApplyUpstreamAccountErrorKeepsCapacityTransient(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "overloaded-account")
	apiErr := types.NewErrorWithStatusCode(errors.New("overloaded"), types.ErrorCodeBadResponseStatusCode, 529)
	apiErr.SetUpstreamResponse(http.Header{"Content-Type": []string{"application/json"}}, []byte(`{"error":{"message":"Overloaded"}}`))

	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.Nil(t, updated.OverloadUntil)
	require.Equal(t, constant.UpstreamStatusActive, updated.Status)
	require.True(t, updated.Schedulable)
	require.True(t, updated.IsSchedulableAt(time.Now().Unix()))
}

func TestApplyUpstreamAccountCapacityBypassesCustomCooldownRule(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "capacity-rule-account")
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).Update(
		"extra",
		`{"temp_unschedulable_enabled":true,"temp_unschedulable_rules":[{"error_code":529,"keywords":["overloaded"],"duration_minutes":60}]}`,
	).Error)
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Our servers are currently overloaded. Please try again later.",
		Code:    "server_is_overloaded",
	}, 529)

	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var unchanged model.UpstreamAccount
	require.NoError(t, model.DB.First(&unchanged, account.Id).Error)
	require.Nil(t, unchanged.TempUnschedulableUntil)
	require.Empty(t, unchanged.TempUnschedulableReason)
}

func TestApplyUpstreamAccountErrorDoesNotPenalizeHTMLForbidden(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "html-forbidden-account")
	apiErr := types.NewErrorWithStatusCode(errors.New("upstream forbidden"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	apiErr.SetUpstreamResponse(http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, []byte(`<!doctype html><html><title>Just a moment</title></html>`))

	require.Equal(t, UpstreamAccountErrorRetryRequest, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var unchanged model.UpstreamAccount
	require.NoError(t, model.DB.First(&unchanged, account.Id).Error)
	require.Nil(t, unchanged.TempUnschedulableUntil)
	require.Nil(t, unchanged.OverloadUntil)
	require.Empty(t, unchanged.ErrorMessage)
}

func TestApplyUpstreamAccountErrorHTMLForbiddenBypassesCustomCooldownRule(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "html-rule-account")
	require.NoError(t, model.DB.Model(&model.UpstreamAccount{}).Where("id = ?", account.Id).Update(
		"extra",
		`{"temp_unschedulable_enabled":true,"temp_unschedulable_rules":[{"error_code":403,"keywords":["cloudflare"],"duration_minutes":30}]}`,
	).Error)
	apiErr := types.NewErrorWithStatusCode(errors.New("cloudflare challenge"), types.ErrorCodeBadResponseStatusCode, http.StatusForbidden)
	apiErr.SetUpstreamResponse(http.Header{"Content-Type": []string{"text/html"}}, []byte(`<html>cloudflare challenge</html>`))

	require.Equal(t, UpstreamAccountErrorRetryRequest, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var unchanged model.UpstreamAccount
	require.NoError(t, model.DB.First(&unchanged, account.Id).Error)
	require.Nil(t, unchanged.TempUnschedulableUntil)
}

func TestApplyUpstreamAccountErrorRecognizesOpenAIOverloadWrappedAs500(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "openai-overloaded-account")
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Our servers are currently overloaded. Please try again later.",
		Type:    "server_error",
		Code:    "server_is_overloaded",
	}, http.StatusInternalServerError)

	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.Nil(t, updated.OverloadUntil)
	require.Empty(t, updated.TempUnschedulableReason)
	require.True(t, updated.IsSchedulableAt(time.Now().Unix()))
}

func TestApplyUpstreamAccountErrorRecognizesCapacityMessageWrappedAs500(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "openai-capacity-account")
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Selected model is at capacity. Please try a different model.",
		Type:    "server_error",
		Code:    "server_error",
	}, http.StatusInternalServerError)

	require.Equal(t, UpstreamAccountErrorRetryAccount, ApplyUpstreamAccountError(account.Id, 0, apiErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.Nil(t, updated.OverloadUntil)
}

func TestApplyUpstreamAccountErrorDoesNotRetryOrdinary500(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "ordinary-server-error-account")
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Message: "Internal server error",
		Type:    "server_error",
		Code:    "server_error",
	}, http.StatusInternalServerError)

	disposition := ApplyUpstreamAccountError(account.Id, 0, apiErr)
	require.False(t, disposition.RetryWithinPool())
	var unchanged model.UpstreamAccount
	require.NoError(t, model.DB.First(&unchanged, account.Id).Error)
	require.Nil(t, unchanged.OverloadUntil)
	require.Empty(t, unchanged.TempUnschedulableReason)
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

func TestShouldRetryUpstreamAccountDefaultsToThreeFailovers(t *testing.T) {
	t.Setenv("UPSTREAM_ACCOUNT_MAX_FAILOVERS", "")
	t.Setenv("UPSTREAM_ACCOUNT_FAILOVER_BUDGET_MS", "0")
	require.True(t, ShouldRetryUpstreamAccount(2, time.Time{}))
	require.False(t, ShouldRetryUpstreamAccount(3, time.Time{}))
}

func TestIsUpstreamCapacityErrorRecognizesSlowDownCode(t *testing.T) {
	apiErr := types.WithOpenAIError(types.OpenAIError{
		Code:    "slow_down",
		Message: "Please retry later.",
	}, http.StatusServiceUnavailable)
	require.True(t, IsUpstreamCapacityError(apiErr))
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

func createTeamLinkedTestAccount(t *testing.T, name, accountType, teamID string) model.UpstreamAccount {
	t.Helper()
	credentials := map[string]any{"chatgpt_account_id": teamID}
	if accountType == constant.UpstreamAccountTypeOAuth {
		credentials["access_token"] = name + "-secret"
		credentials["account_id"] = name + "-account"
	} else {
		credentials["api_key"] = name + "-key"
	}
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: name, Platform: constant.UpstreamPlatformOpenAI, Type: accountType,
			Extra: "{}", Concurrency: 1, Priority: 1, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true, AutoPauseOnExpired: true,
		},
		Credentials: credentials,
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	return input.Account
}
