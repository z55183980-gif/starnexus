package sub2api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSetupRequestHeaderForwardsResponsesLiteToSub2APIHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name       string
		ingress    bool
		mode       string
		relayMode  int
		wantHeader bool
	}{
		{name: "Responses HTTP", relayMode: relayconstant.RelayModeResponses, wantHeader: true},
		{name: "Responses HTTP bridge", ingress: true, mode: model.UpstreamOpenAIWSModeHTTPBridge, relayMode: relayconstant.RelayModeResponses, wantHeader: true},
		{name: "Responses native WebSocket", ingress: true, mode: model.UpstreamOpenAIWSModeContextPool, relayMode: relayconstant.RelayModeResponses, wantHeader: false},
		{name: "Alpha search", relayMode: relayconstant.RelayModeAlphaSearch, wantHeader: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Request.Header.Set("Content-Type", "application/json")
			c.Request.Header.Set(codex.ResponsesLiteHeader, "true")
			if testCase.ingress {
				common.SetContextKey(c, constant.ContextKeyResponsesWebSocketIngress, true)
			}
			info := &relaycommon.RelayInfo{
				RelayMode: testCase.relayMode,
				ChannelMeta: &relaycommon.ChannelMeta{
					ApiKey: "sub2api-token",
					ChannelOtherSettings: dto.ChannelOtherSettings{
						ResponsesWebSocketV2Mode: testCase.mode,
					},
				},
			}
			headers := make(http.Header)
			require.NoError(t, (&Adaptor{}).SetupRequestHeader(c, &headers, info))
			if testCase.wantHeader {
				require.Equal(t, "true", headers.Get(codex.ResponsesLiteHeader))
			} else {
				require.Empty(t, headers.Get(codex.ResponsesLiteHeader))
			}
		})
	}
}
