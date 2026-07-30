package service

import (
	"context"
	"io"
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
	requestHeaders := make(chan http.Header, 1)
	requestBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization <- r.Header.Get("Authorization")
		requestHeaders <- r.Header.Clone()
		body, _ := io.ReadAll(r.Body)
		requestBody <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_probe\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_text.delta\",\"delta\":\"STARNEXUS_PROBE_OK\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\ndata: [DONE]\n\n"))
	}))
	defer upstream.Close()

	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "probe-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true,
		},
		Credentials: map[string]any{
			"api_key": "probe-secret", "base_url": upstream.URL,
			"model_mapping":           map[string]any{upstreamAccountProbeDefaultOpenAIModel: "probe-mapped-model"},
			"header_override_enabled": true,
			"header_overrides":        map[string]any{"x-relay-key": "relay-secret"},
		},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := TestUpstreamAccount(ctx, input.Account.Id)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, http.StatusOK, result.StatusCode)
	require.Positive(t, result.FirstOutputLatencyMs)
	require.Equal(t, "http_sse_responses", result.Protocol)
	require.Equal(t, "Bearer probe-secret", <-authorization)
	headers := <-requestHeaders
	require.Equal(t, upstreamAccountProbeOpenAIUserAgent, headers.Get("User-Agent"))
	require.Equal(t, upstreamAccountProbeOpenAIVersion, headers.Get("version"))
	require.Equal(t, "codex_cli_rs", headers.Get("originator"))
	require.Equal(t, "responses=experimental", headers.Get("OpenAI-Beta"))
	require.NotEmpty(t, headers.Get("X-Codex-Window-ID"))
	require.Equal(t, "relay-secret", headers.Get("X-Relay-Key"))
	body := <-requestBody
	require.Contains(t, body, `"model":"probe-mapped-model"`)
	require.Contains(t, body, `"text":"hi"`)
	require.NotContains(t, body, `"store"`)

	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.True(t, stored.Schedulable)
	require.Empty(t, stored.ErrorMessage)
}

func TestOpenAIAccountProbeClientIsolatesHTTP2TransportWithProxy(t *testing.T) {
	ResetProxyClientCache()
	t.Cleanup(ResetProxyClientCache)
	client, err := NewOpenAIUpstreamHttpClient("socks5://127.0.0.1:1080")
	require.NoError(t, err)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, transport.ForceAttemptHTTP2)
}

func TestOpenAIAccountProbeClientIsolatesHTTP2TransportWithoutProxy(t *testing.T) {
	previousClient := httpClient
	previousProtectedClient := ssrfProtectedHTTPClient
	InitHttpClient()
	t.Cleanup(func() {
		httpClient = previousClient
		ssrfProtectedHTTPClient = previousProtectedClient
	})

	client, err := NewOpenAIUpstreamHttpClient("")
	require.NoError(t, err)
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.True(t, transport.ForceAttemptHTTP2)
}

func TestUpstreamAccountProbeAcceptsCompletedWithoutVisibleOutput(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	userAgent := make(chan string, 1)
	requestBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent <- r.Header.Get("User-Agent")
		body, _ := io.ReadAll(r.Body)
		requestBody <- string(body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_probe\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\n"))
	}))
	defer upstream.Close()

	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "completed-only", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusError, Schedulable: false, ErrorMessage: "no_visible_output",
		},
		Credentials: map[string]any{"access_token": "probe-secret", "account_id": "account-id", "base_url": upstream.URL},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := TestUpstreamAccount(ctx, input.Account.Id, UpstreamAccountTestOptions{Model: "gpt-5.6"})
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "ok", result.Result)
	require.Zero(t, result.FirstOutputLatencyMs)
	require.Equal(t, "response.completed", result.TerminalType)
	require.Equal(t, []string{"response.created", "response.completed"}, result.EventTypes)
	require.Contains(t, result.ContentType, "text/event-stream")
	require.Positive(t, result.BodyBytes)
	require.Equal(t, upstreamAccountProbeOpenAIUserAgent, <-userAgent)
	body := <-requestBody
	require.Contains(t, body, `"model":"gpt-5.6-sol"`)
	require.Contains(t, body, `"instructions":"`)
	require.Contains(t, body, `"store":false`)

	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.Equal(t, constant.UpstreamStatusActive, stored.Status)
	require.True(t, stored.Schedulable)
	require.Empty(t, stored.ErrorMessage)
}

func TestUpstreamAccountProbeRejectsFailedTerminalEvent(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"failed\"}}}\n\n"))
	}))
	defer upstream.Close()

	input := createProbeTestAccount(t, upstream.URL)
	result, err := TestUpstreamAccount(context.Background(), input.Account.Id)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "upstream_rejected", result.Result)
	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.Nil(t, stored.TempUnschedulableUntil)
	require.Empty(t, stored.ErrorMessage)
}

func TestUpstreamAPIKeyProbeUsesChatWhenResponsesProbeWasUnsupported(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	requestPath := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath <- r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "chat-probe", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: `{"openai_responses_supported":false}`, Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true,
		},
		Credentials: map[string]any{"api_key": "probe-secret", "base_url": upstream.URL},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	result, err := TestUpstreamAccount(context.Background(), input.Account.Id)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "http_sse_chat", result.Protocol)
	require.Equal(t, "/v1/chat/completions", <-requestPath)
}

func TestUpstreamAccountProbeSuccessPreservesManualPause(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\n"))
	}))
	defer upstream.Close()

	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "paused-probe", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusInactive, Schedulable: false,
		},
		Credentials: map[string]any{"api_key": "probe-secret", "base_url": upstream.URL},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	result, err := TestUpstreamAccount(context.Background(), input.Account.Id)
	require.NoError(t, err)
	require.True(t, result.Success)
	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.Equal(t, constant.UpstreamStatusInactive, stored.Status)
	require.False(t, stored.Schedulable)
}

func TestUpstreamAccountProbeRejectsStreamWithoutTerminalEvent(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_probe\"}}\n\n"))
	}))
	defer upstream.Close()

	input := createProbeTestAccount(t, upstream.URL)
	result, err := TestUpstreamAccount(context.Background(), input.Account.Id)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Equal(t, "incomplete_response", result.Result)
	require.Equal(t, []string{"response.created"}, result.EventTypes)
}

func TestUpstreamAccountProbeParsesSSEWithoutContentType(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Content-Type"] = nil
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_probe\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{}}\n\n"))
	}))
	defer upstream.Close()

	input := createProbeTestAccount(t, upstream.URL)
	result, err := TestUpstreamAccount(context.Background(), input.Account.Id)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, "response.completed", result.TerminalType)
	require.Equal(t, []string{"response.created", "response.completed"}, result.EventTypes)
}

func TestUpstreamClaudeProbeAcceptsCleanEOFLikeSub2API(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_probe\"}}\n\n"))
	}))
	defer upstream.Close()

	account := &model.UpstreamAccount{
		Platform: constant.UpstreamPlatformAnthropic,
		Type:     constant.UpstreamAccountTypeAPIKey,
		Extra:    "{}",
	}
	probe, err := probeUpstreamAccountGeneration(context.Background(), upstream.Client(), account, map[string]any{
		"api_key": "probe-secret", "base_url": upstream.URL,
	}, "claude-test", UpstreamAccountTestModeDefault)
	require.NoError(t, err)
	require.True(t, probe.Completed)
	require.False(t, probe.Failed)
	require.Equal(t, "eof", probe.TerminalType)
}

func TestUpstreamOpenAICompactProbeUsesCompactPathAndMapping(t *testing.T) {
	setupUpstreamAdminTestDB(t)
	requestPath := make(chan string, 1)
	requestAccept := make(chan string, 1)
	requestBody := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath <- r.URL.Path
		requestAccept <- r.Header.Get("Accept")
		body, _ := io.ReadAll(r.Body)
		requestBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmp_probe","output":[]}`))
	}))
	defer upstream.Close()

	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "compact-probe", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true,
		},
		Credentials: map[string]any{
			"api_key": "probe-secret", "base_url": upstream.URL,
			"compact_model_mapping": map[string]any{"gpt-5.*": "gpt-5.4-openai-compact"},
		},
	}
	require.NoError(t, CreateUpstreamAccount(&input))

	result, err := TestUpstreamAccount(context.Background(), input.Account.Id, UpstreamAccountTestOptions{
		Model: "gpt-5.6", Mode: UpstreamAccountTestModeCompact,
	})
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Equal(t, UpstreamAccountTestModeCompact, result.Mode)
	require.Equal(t, "http_json_responses_compact", result.Protocol)
	require.Equal(t, "http.2xx", result.TerminalType)
	require.Equal(t, "/v1/responses/compact", <-requestPath)
	require.Equal(t, "application/json", <-requestAccept)
	body := <-requestBody
	require.Contains(t, body, `"model":"gpt-5.4-openai-compact"`)
	require.Contains(t, body, `"content":"Respond with OK."`)
	require.NotContains(t, body, `"stream"`)
	var stored model.UpstreamAccount
	require.NoError(t, model.DB.First(&stored, input.Account.Id).Error)
	require.Contains(t, stored.Extra, `"openai_compact_supported":true`)
}

func createProbeTestAccount(t *testing.T, baseURL string) UpstreamAccountCreateInput {
	t.Helper()
	input := UpstreamAccountCreateInput{
		Account: model.UpstreamAccount{
			Name: "probe-account", Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeAPIKey,
			Extra: "{}", Concurrency: 1, Priority: 50, Weight: 1,
			Status: constant.UpstreamStatusActive, Schedulable: true,
		},
		Credentials: map[string]any{"api_key": "probe-secret", "base_url": baseURL},
	}
	require.NoError(t, CreateUpstreamAccount(&input))
	return input
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
