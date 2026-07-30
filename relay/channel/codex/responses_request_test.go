package codex

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeCodexResponsesRequestMatchesOAuthSchema(t *testing.T) {
	t.Parallel()
	topP := 0.8
	request := dto.OpenAIResponsesRequest{
		Input:                []byte(`"hello"`),
		Metadata:             []byte(`{"trace":"client"}`),
		PromptCacheRetention: []byte(`"24h"`),
		SafetyIdentifier:     []byte(`"user-1"`),
		StreamOptions:        &dto.StreamOptions{},
		TopP:                 &topP,
		User:                 []byte(`"user-1"`),
		Reasoning:            &dto.Reasoning{},
	}
	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Nil(t, request.Metadata)
	require.Nil(t, request.PromptCacheRetention)
	require.Nil(t, request.SafetyIdentifier)
	require.Nil(t, request.StreamOptions)
	require.Nil(t, request.TopP)
	require.Nil(t, request.User)
	require.True(t, gjson.GetBytes(request.Input, "#").Int() == 1)
	require.Equal(t, "hello", gjson.GetBytes(request.Input, "0.content").String())

	var include []string
	require.NoError(t, common.Unmarshal(request.Include, &include))
	require.Contains(t, include, "reasoning.encrypted_content")
}

func TestIsolateCodexSessionHeaderByUserAndToken(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyUserId, 7)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 11)

	first := isolateCodexSessionHeader(ctx, "client-session")
	require.NotEqual(t, "client-session", first)
	require.Equal(t, first, isolateCodexSessionHeader(ctx, "client-session"))

	common.SetContextKey(ctx, constant.ContextKeyTokenId, 12)
	require.NotEqual(t, first, isolateCodexSessionHeader(ctx, "client-session"))
}

func TestCodexPreviousResponseIDOnlyUsesNativeResponsesWebSocket(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		ingress  bool
		mode     string
		wantPrev bool
	}{
		{name: "ordinary HTTP", wantPrev: false},
		{name: "HTTP bridge", ingress: true, mode: model.UpstreamOpenAIWSModeHTTPBridge, wantPrev: false},
		{name: "context pool", ingress: true, mode: model.UpstreamOpenAIWSModeContextPool, wantPrev: true},
		{name: "passthrough", ingress: true, mode: model.UpstreamOpenAIWSModePassthrough, wantPrev: true},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			if testCase.ingress {
				common.SetContextKey(ctx, constant.ContextKeyResponsesWebSocketIngress, true)
			}
			info := &relaycommon.RelayInfo{}
			info.ChannelMeta = &relaycommon.ChannelMeta{}
			info.ChannelOtherSettings.ResponsesWebSocketV2Mode = testCase.mode
			converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, dto.OpenAIResponsesRequest{
				Model: "gpt-5", Input: []byte(`[]`), PreviousResponseID: "resp_previous",
			})
			require.NoError(t, err)
			request, ok := converted.(dto.OpenAIResponsesRequest)
			require.True(t, ok)
			if testCase.wantPrev {
				require.Equal(t, "resp_previous", request.PreviousResponseID)
			} else {
				require.Empty(t, request.PreviousResponseID)
			}
		})
	}
}
