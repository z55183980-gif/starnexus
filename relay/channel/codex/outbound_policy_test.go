package codex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexOutboundPolicyConvergesBodyHeadersAndRouting(t *testing.T) {
	c := newCodexPolicyTestContext(t)
	selection := &service.UpstreamAccountSelection{
		Account: model.UpstreamAccount{
			Id:       42,
			Platform: constant.UpstreamPlatformOpenAI,
			Type:     constant.UpstreamAccountTypeOAuth,
			Extra:    `{"codex_fingerprint_mode":"session"}`,
		},
		Credentials: map[string]any{"account_id": "external-account", "access_token": "selected-token"},
	}
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountId, 42)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountSelection, selection)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}

	body, err := (&Adaptor{}).FinalizeOutboundJSONBody(c, info, []byte(`{"model":"gpt-5.4","service_tier":"fast","prompt_cache_key":"thread-a"}`))
	require.NoError(t, err)
	require.NotEmpty(t, gjson.GetBytes(body, "client_metadata.x-codex-installation-id").String())
	require.NotEmpty(t, gjson.GetBytes(body, "client_metadata.session_id").String())
	require.NotEmpty(t, gjson.GetBytes(body, "client_metadata.thread_id").String())

	request := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", nil)
	request.Header.Set("User-Agent", "spoofed-client")
	request.Header.Set("originator", "spoofed-originator")
	request.Header.Set("version", "0.1.0")
	request.Header.Set("OpenAI-Beta", "responses=experimental")
	request.Header.Set("Authorization", "Bearer spoofed-token")
	request.Header.Set("chatgpt-account-id", "spoofed-account")
	request.Header.Set(codexRoutingHintHeader, "model=spoofed;tier=flex")
	require.NoError(t, (&Adaptor{}).FinalizeOutboundRequest(c, info, request))

	require.Equal(t, defaultCodexUserAgent, request.Header.Get("User-Agent"))
	require.Equal(t, "codex-tui", request.Header.Get("originator"))
	require.Equal(t, "0.146.0", request.Header.Get("version"))
	require.Equal(t, "Bearer selected-token", request.Header.Get("Authorization"))
	require.Equal(t, "external-account", request.Header.Get("chatgpt-account-id"))
	require.Empty(t, request.Header.Get("OpenAI-Beta"))
	require.Equal(t, "model=gpt-5.4;tier=priority", request.Header.Get(codexRoutingHintHeader))
	require.NotEmpty(t, request.Header.Get("x-codex-installation-id"))
	require.NotEmpty(t, request.Header.Get("session-id"))
}

func TestCodexOutboundPolicyLeavesNonOAuthIdentityUntouched(t *testing.T) {
	c := newCodexPolicyTestContext(t)
	selection := &service.UpstreamAccountSelection{
		Account: model.UpstreamAccount{
			Id:       44,
			Platform: constant.UpstreamPlatformOpenAI,
			Type:     constant.UpstreamAccountTypeAPIKey,
		},
		Credentials: map[string]any{"api_key": "selected-key"},
	}
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountSelection, selection)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	request.Header.Set("User-Agent", "custom-api-client")
	request.Header.Set("originator", "custom-originator")
	request.Header.Set("version", "9.9.9")

	require.NoError(t, (&Adaptor{}).FinalizeOutboundRequest(c, info, request))
	require.Equal(t, "custom-api-client", request.Header.Get("User-Agent"))
	require.Equal(t, "custom-originator", request.Header.Get("originator"))
	require.Equal(t, "9.9.9", request.Header.Get("version"))
}

func TestCodexOutboundPolicyNormalizesExplicitDeviceID(t *testing.T) {
	c := newCodexPolicyTestContext(t)
	selection := &service.UpstreamAccountSelection{
		Account: model.UpstreamAccount{Id: 43, Platform: constant.UpstreamPlatformOpenAI, Type: constant.UpstreamAccountTypeOAuth, Extra: `{"codex_fingerprint_mode":"device"}`},
		Credentials: map[string]any{
			"account_id":       "external-account-2",
			"access_token":     "selected-token-2",
			"openai_device_id": "device\r\nspoof",
		},
	}
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountId, 43)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountSelection, selection)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}
	_, err := (&Adaptor{}).FinalizeOutboundJSONBody(c, info, []byte(`{"model":"gpt-5.4"}`))
	require.NoError(t, err)
	require.NotEmpty(t, info.CodexOutboundState.Fingerprint.InstallationID)
	require.NotContains(t, info.CodexOutboundState.Fingerprint.InstallationID, "\r")
	require.NotContains(t, info.CodexOutboundState.Fingerprint.InstallationID, "\n")
}

func TestCodexOutboundPolicyRemovesAllOrdinaryBetaTokens(t *testing.T) {
	c := newCodexPolicyTestContext(t)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeResponses}
	request := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", nil)
	request.Header.Set("OpenAI-Beta", "responses=experimental, responses_websockets=2026-02-06")
	require.NoError(t, (&Adaptor{}).FinalizeOutboundRequest(c, info, request))
	require.Empty(t, request.Header.Get("OpenAI-Beta"))
}

func TestCodexOutboundPolicyUsesWebSocketBetaAndRejectsSpoofedRoute(t *testing.T) {
	c := newCodexPolicyTestContext(t)
	common.SetContextKey(c, constant.ContextKeyResponsesWebSocketIngress, true)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelOtherSettings: dto.ChannelOtherSettings{
			ResponsesWebSocketV2Mode: model.UpstreamOpenAIWSModeContextPool,
		}},
		CodexOutboundState: &relaycommon.CodexOutboundState{FinalModel: "bad;model", ServiceTier: "flex"},
	}
	request := httptest.NewRequest(http.MethodPost, "/backend-api/codex/responses", nil)
	request.Header.Set("OpenAI-Beta", "responses=experimental")
	request.Header.Set(codexRoutingHintHeader, "model=spoofed")

	require.NoError(t, (&Adaptor{}).FinalizeOutboundRequest(c, info, request))
	require.Equal(t, "responses_websockets=2026-02-06", request.Header.Get("OpenAI-Beta"))
	require.Empty(t, request.Header.Get(codexRoutingHintHeader))
}

func TestCodexOutboundPolicyStripsAlphaSessionHeaders(t *testing.T) {
	c := newCodexPolicyTestContext(t)
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAlphaSearch}
	request := httptest.NewRequest(http.MethodPost, "/backend-api/codex/alpha/search", nil)
	for key, value := range map[string]string{
		"OpenAI-Beta":             "responses=experimental",
		"session_id":              "session",
		"x-codex-installation-id": "installation",
		ResponsesLiteHeader:       "true",
	} {
		request.Header.Set(key, value)
	}

	require.NoError(t, (&Adaptor{}).FinalizeOutboundRequest(c, info, request))
	require.Empty(t, request.Header.Get("OpenAI-Beta"))
	require.Empty(t, request.Header.Get("session_id"))
	require.Empty(t, request.Header.Get("x-codex-installation-id"))
	require.Empty(t, request.Header.Get(ResponsesLiteHeader))
}

func newCodexPolicyTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c
}
