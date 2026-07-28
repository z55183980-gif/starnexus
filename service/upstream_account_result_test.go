package service

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestApplyUpstreamAccountErrorOwnership(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	account := createRouterTestAccountWithoutPool(t, "result-account")

	clientErr := types.NewErrorWithStatusCode(errors.New("bad input"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	require.False(t, ApplyUpstreamAccountError(account.Id, 0, clientErr))

	rateErr := types.NewErrorWithStatusCode(errors.New("rate limited token=secret"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	require.True(t, ApplyUpstreamAccountError(account.Id, 0, rateErr))
	var updated model.UpstreamAccount
	require.NoError(t, model.DB.First(&updated, account.Id).Error)
	require.NotNil(t, updated.RateLimitResetAt)
	require.NotContains(t, updated.ErrorMessage, "secret")

	authErr := types.NewErrorWithStatusCode(errors.New("invalid bearer"), types.ErrorCodeBadResponseStatusCode, http.StatusUnauthorized)
	require.True(t, ApplyUpstreamAccountError(account.Id, 0, authErr))
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
	require.True(t, ApplyUpstreamAccountError(account.Id, proxyInput.Proxy.Id, serverErr))
	var proxy model.UpstreamProxy
	require.NoError(t, model.DB.First(&proxy, proxyInput.Proxy.Id).Error)
	require.Empty(t, proxy.LatencyStatus)

	transportErr := types.NewErrorWithStatusCode(errors.New("connection reset"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	require.True(t, ApplyUpstreamAccountError(account.Id, proxyInput.Proxy.Id, transportErr))
	require.NoError(t, model.DB.First(&proxy, proxyInput.Proxy.Id).Error)
	require.Equal(t, "error", proxy.LatencyStatus)
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
