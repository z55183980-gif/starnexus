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
