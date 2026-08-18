package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPrepareChatCompletionsViaResponsesBodyNormalizesLitePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set(codex.ResponsesLiteHeader, "true")
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(c, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)

	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType: constant.ChannelTypeCodex,
		},
	}
	convertedRequest := map[string]any{
		"model": "gpt-5.6-sol",
		"input": "hello",
		"reasoning": map[string]any{
			"context": "current_turn",
		},
	}

	prepared, apiErr := prepareChatCompletionsViaResponsesBody(c, info, &codex.Adaptor{}, convertedRequest)
	require.Nil(t, apiErr)
	require.Equal(t, "all_turns", gjson.GetBytes(prepared, "reasoning.context").String())
	require.Equal(t, "reasoning.encrypted_content", gjson.GetBytes(prepared, "include.0").String())
}
