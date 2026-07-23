package channel_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestDoResponsesWssRequest(t *testing.T) {
	t.Parallel()

	headers := make(chan http.Header, 1)
	paths := make(chan string, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Clone()
		paths <- r.URL.Path
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()
	}))
	defer server.Close()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	ctx.Request.Header.Set("Originator", "Codex CLI")
	ctx.Request.Header.Set("X-Codex-Window-Id", "window-123")

	info := &relaycommon.RelayInfo{
		RelayMode:      relayconstant.RelayModeResponses,
		RelayFormat:    types.RelayFormatOpenAIResponses,
		RequestURLPath: "/v1/responses",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: server.URL,
			ApiKey:         "upstream-key",
		},
	}
	adaptor := &openai.Adaptor{}
	adaptor.Init(info)

	conn, resp, err := channel.DoResponsesWssRequest(adaptor, ctx, info)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	require.NotNil(t, conn)
	require.NoError(t, conn.Close())

	requestHeaders := <-headers
	require.Equal(t, "/v1/responses", <-paths)
	require.Equal(t, "Bearer upstream-key", requestHeaders.Get("Authorization"))
	require.Equal(t, "responses_websockets=2026-02-06", requestHeaders.Get("OpenAI-Beta"))
	require.Equal(t, "Codex CLI", requestHeaders.Get("Originator"))
	require.Equal(t, "window-123", requestHeaders.Get("X-Codex-Window-Id"))
}
