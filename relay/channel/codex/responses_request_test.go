package codex

import (
	"encoding/json"
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
	"github.com/QuantumNous/new-api/types"
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

func TestCodexResponsesForcesOnlyOrdinaryRequestsToStream(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	falseValue := false

	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`[]`), Stream: &falseValue})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.NotNil(t, request.Stream)
	require.True(t, *request.Stream)

	converted, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`[]`), Stream: &falseValue})
	require.NoError(t, err)
	compactRequest := converted.(dto.OpenAIResponsesRequest)
	require.NotNil(t, compactRequest.Stream)
	require.False(t, *compactRequest.Stream)
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

func TestCodexHTTPExpandsGatewayContinuationBeforeDroppingPreviousResponseID(t *testing.T) {
	firstCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(firstCtx, constant.ContextKeyUserId, 8201)
	common.SetContextKey(firstCtx, constant.ContextKeyTokenId, 9201)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelId, 42)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPoolId, 3)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountId, 9)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: []byte(`"lookup"`)})
	service.MarkResponsesHTTPContinuationPersistTarget(firstCtx)
	service.CommitResponsesHTTPContinuation(firstCtx, "resp_codex_http", []json.RawMessage{
		json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}`),
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 8201)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 9201)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 42)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPoolId, 3)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 9)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	request := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_codex_http",
		Input:              []byte(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	service.PrepareResponsesHTTPContinuation(ctx, &request)
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.Empty(t, prepared.PreviousResponseID)
	require.Equal(t, int64(3), gjson.GetBytes(prepared.Input, "#").Int())
}

func TestCodexHTTPRejectsOrphanFunctionCallOutput(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 8202)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 9202)
	request := dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_missing_codex_http",
		Input:              []byte(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	service.PrepareResponsesHTTPContinuation(ctx, &request)
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}, request)
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
}
