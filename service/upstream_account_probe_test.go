package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestUpstreamAPIKeyAccountProbeUsesStoredCredential(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	authorization := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "probe-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true,
		},
		Credentials: map[string]any{"api_key": "probe-secret", "base_url": upstream.URL},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := TestUpstreamAccount(ctx, input.Account.Id)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Equal(t, "Bearer probe-secret", <-authorization)

	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.True(t, stored.Schedulable)
	require.Empty(t, stored.ErrorMessage)
}

func TestUpstreamProxyProbePersistsObservedExit(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ip":"203.0.113.9","country":"TH","region":"Bangkok","city":"Bangkok"}`))
	}))
	defer proxyServer.Close()
	parsed, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)
	port, err := strconv.Atoi(parsed.Port())
	require.NoError(t, err)

	input := UpstreamProxyCreateInput{
		Proxy: model.UpstreamProxy{
			Name: "probe-proxy", Protocol: constant.UpstreamProxyProtocolHTTP,
			Host: parsed.Hostname(), Port: port, Status: constant.UpstreamStatusActive,
			FallbackMode: constant.UpstreamProxyFallbackNone,
		},
	}
	require.NoError(t, CreateUpstreamProxy(&input))
	t.Setenv("UPSTREAM_PROXY_TEST_URL", "http://probe.invalid/json")
	ResetProxyClientCache()
	t.Cleanup(ResetProxyClientCache)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := TestUpstreamProxy(ctx, input.Proxy.Id)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "203.0.113.9", result.IP)
	require.Equal(t, "TH", result.Country)

	var stored model.UpstreamProxy
	require.NoError(t, model.DB.First(&stored, input.Proxy.Id).Error)
	require.Equal(t, "203.0.113.9", stored.ObservedIp)
	require.Equal(t, "ok", stored.LatencyStatus)
	require.True(t, strings.Contains(stored.ObservedRegion, "Bangkok"))
}
