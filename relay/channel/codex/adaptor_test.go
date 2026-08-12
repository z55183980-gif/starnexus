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
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLAlphaSearch(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeCodex,
			ChannelBaseUrl: "https://chatgpt.com",
		},
		RelayMode: relayconstant.RelayModeAlphaSearch,
	}

	url, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", url)
}

func TestSetupRequestHeaderForwardsResponsesLiteOnlyForHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name       string
		ingress    bool
		mode       string
		relayMode  int
		wantHeader bool
	}{
		{name: "ordinary HTTP", relayMode: relayconstant.RelayModeResponses, wantHeader: true},
		{name: "WebSocket HTTP bridge", ingress: true, mode: model.UpstreamOpenAIWSModeHTTPBridge, relayMode: relayconstant.RelayModeResponses, wantHeader: true},
		{name: "native WebSocket", ingress: true, mode: model.UpstreamOpenAIWSModeContextPool, relayMode: relayconstant.RelayModeResponses, wantHeader: false},
		{name: "alpha search strips Responses header", relayMode: relayconstant.RelayModeAlphaSearch, wantHeader: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set(ResponsesLiteHeader, "true")
			if testCase.ingress {
				common.SetContextKey(c, constant.ContextKeyResponsesWebSocketIngress, true)
			}
			info := &relaycommon.RelayInfo{
				RelayMode: testCase.relayMode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiKey: `{"access_token":"token","account_id":"account"}`,
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ResponsesWebSocketV2Mode: testCase.mode,
					},
				},
			}
			headers := make(http.Header)
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
			if testCase.relayMode == relayconstant.RelayModeResponses {
				require.Equal(t, "text/event-stream", headers.Get("Accept"))
			}
			if testCase.wantHeader {
				require.Equal(t, "true", headers.Get(ResponsesLiteHeader))
			} else {
				require.Empty(t, headers.Get(ResponsesLiteHeader))
			}
			require.Equal(t, defaultCodexUserAgent, headers.Get("User-Agent"))
		})
	}
}

func TestSetupRequestHeaderAppliesCodexUserAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: `{"access_token":"token","account_id":"account"}`,
		},
	}

	t.Run("empty ua uses default", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		headers := make(http.Header)
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
		require.Equal(t, defaultCodexUserAgent, headers.Get("User-Agent"))
	})

	t.Run("browser ua is replaced", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
		headers := make(http.Header)
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
		require.Equal(t, defaultCodexUserAgent, headers.Get("User-Agent"))
	})

	t.Run("cli ua is converged", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request.Header.Set("User-Agent", "codex_cli_rs/0.100.0 (Ubuntu 22.4.0; x86_64) xterm-256color")
		headers := make(http.Header)
		require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
		require.Equal(t, defaultCodexUserAgent, headers.Get("User-Agent"))
		require.Equal(t, "codex-tui", headers.Get("originator"))
		require.Equal(t, "0.146.0", headers.Get("version"))
	})
}

func TestSetupRequestHeaderDerivesSessionFromPromptCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyUserId, 7)
	common.SetContextKey(c, constant.ContextKeyTokenId, 11)
	info := &relaycommon.RelayInfo{
		Request:     &dto.OpenAIResponsesRequest{PromptCacheKey: []byte(`"cache-key"`)},
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: `{"access_token":"token","account_id":"account"}`},
	}
	headers := make(http.Header)
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
	want := isolateCodexSessionHeader(c, "cache-key")
	require.Equal(t, want, headers.Get("session_id"))
	require.Equal(t, want, headers.Get("conversation_id"))
}

func TestSetupRequestHeaderPromptCacheKeyOverridesClientSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("session_id", "client-session")
	c.Request.Header.Set("conversation_id", "client-conversation")
	common.SetContextKey(c, constant.ContextKeyUserId, 7)
	common.SetContextKey(c, constant.ContextKeyTokenId, 11)
	info := &relaycommon.RelayInfo{
		Request:     &dto.OpenAIResponsesRequest{PromptCacheKey: []byte(`"cache-key"`)},
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: `{"access_token":"token","account_id":"account"}`},
	}
	headers := make(http.Header)
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
	want := isolateCodexSessionHeader(c, "cache-key")
	require.Equal(t, want, headers.Get("session_id"))
	require.Equal(t, want, headers.Get("conversation_id"))
}

func TestSetupRequestHeaderIgnoresInvalidPromptCacheKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("session_id", "client-session")
	info := &relaycommon.RelayInfo{
		Request:     &dto.OpenAIResponsesRequest{PromptCacheKey: []byte(`123`)},
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{ApiKey: `{"access_token":"token","account_id":"account"}`},
	}
	headers := make(http.Header)
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
	require.Equal(t, isolateCodexSessionHeader(c, "client-session"), headers.Get("session_id"))
}

func TestSetupRequestHeaderNativeWebSocketDoesNotDerivePromptCacheSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(c, constant.ContextKeyResponsesWebSocketIngress, true)
	info := &relaycommon.RelayInfo{
		Request:   &dto.OpenAIResponsesRequest{PromptCacheKey: []byte(`"cache-key"`)},
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey:               `{"access_token":"token","account_id":"account"}`,
			ChannelOtherSettings: dto.ChannelOtherSettings{ResponsesWebSocketV2Mode: model.UpstreamOpenAIWSModeContextPool},
		},
	}
	headers := make(http.Header)
	require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
	require.Empty(t, headers.Get("session_id"))
	require.Empty(t, headers.Get("conversation_id"))
}
