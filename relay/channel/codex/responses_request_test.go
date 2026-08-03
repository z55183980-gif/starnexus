package codex

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		PromptCacheOptions:   []byte(`{"mode":"explicit"}`),
		SafetyIdentifier:     []byte(`"user-1"`),
		StreamOptions:        &dto.StreamOptions{},
		TopP:                 &topP,
		User:                 []byte(`"user-1"`),
		Reasoning:            &dto.Reasoning{},
	}
	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Nil(t, request.Metadata)
	require.Nil(t, request.PromptCacheRetention)
	require.Nil(t, request.PromptCacheOptions)
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

func TestNormalizeCodexResponsesInputStripsCacheBreakpointAndEmptyQuality(t *testing.T) {
	t.Parallel()
	input := []byte(`[
		{
			"type":"message",
			"role":"user",
			"content":[
				{"type":"input_text","text":"stable prefix","prompt_cache_breakpoint":{"mode":"explicit"}},
				{"type":"input_image","image_url":"https://example.com/a.png","prompt_cache_breakpoint":{"mode":"explicit"}}
			]
		},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}],"quality":"","size":""},
		{"type":"function_call_output","call_id":"call_1","output":{"quality":"keep-me","prompt_cache_breakpoint":{"mode":"explicit"}}},
		{"type":"image_generation_call","id":"img_1","quality":"high","size":"1024x1024"}
	]`)
	request := dto.OpenAIResponsesRequest{Input: input, PromptCacheOptions: []byte(`{"mode":"explicit"}`)}
	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Nil(t, request.PromptCacheOptions)

	require.False(t, gjson.GetBytes(request.Input, "0.content.0.prompt_cache_breakpoint").Exists())
	require.False(t, gjson.GetBytes(request.Input, "0.content.1.prompt_cache_breakpoint").Exists())
	require.Equal(t, "stable prefix", gjson.GetBytes(request.Input, "0.content.0.text").String())

	require.False(t, gjson.GetBytes(request.Input, "1.quality").Exists())
	require.False(t, gjson.GetBytes(request.Input, "1.size").Exists())

	// Nested business JSON must not be scrubbed recursively.
	require.Equal(t, "keep-me", gjson.GetBytes(request.Input, "2.output.quality").String())
	require.True(t, gjson.GetBytes(request.Input, "2.output.prompt_cache_breakpoint").Exists())

	require.Equal(t, "high", gjson.GetBytes(request.Input, "3.quality").String())
	require.Equal(t, "1024x1024", gjson.GetBytes(request.Input, "3.size").String())
}

func TestNormalizeCodexResponsesInputPreservesLargeIntegers(t *testing.T) {
	t.Parallel()
	input := []byte(`[
		{"type":"function_call_output","call_id":"call_1","output":{"id":9007199254740993}},
		{"type":"message","role":"assistant","quality":"","content":[{"type":"output_text","text":"hi","metadata":{"id":9007199254740995}}],"metadata":{"id":9007199254740997}}
	]`)

	normalized, err := normalizeCodexResponsesInput(input)
	require.NoError(t, err)
	require.Contains(t, string(normalized), `"id":9007199254740993`)
	require.Contains(t, string(normalized), `"id":9007199254740995`)
	require.Contains(t, string(normalized), `"id":9007199254740997`)
	require.False(t, gjson.GetBytes(normalized, "1.quality").Exists())
}

func TestRepairCodexInvalidLocalReasoningItemsIsTopLevelAndNarrow(t *testing.T) {
	t.Parallel()
	input := json.RawMessage(`[
		{"type":"message","role":"user","content":{"id":"item_nested","type":"reasoning"}},
		{"type":"reasoning","id":"rs_valid","encrypted_content":"valid"},
		{"type":"reasoning","id":"item_9ccf846e881144534dd7408d","encrypted_content":"invalid"},
		{"type":"message","id":"item_message","role":"assistant","content":"keep"},
		{"type":"function_call","id":"item_call","call_id":"call_1","name":"lookup","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_1","output":{"id":"item_tool_payload"}}
	]`)

	repaired, result, err := repairCodexInvalidLocalItemIDs(input)
	require.NoError(t, err)
	require.Equal(t, 1, result.DroppedReasoningItems)
	require.Zero(t, result.DroppedItemReferences)
	require.Equal(t, 2, result.FirstDroppedIndex)
	require.Equal(t, 5, result.RemainingItems)
	require.False(t, result.HasOrphanToolOutput)
	require.Equal(t, int64(5), gjson.GetBytes(repaired, "#").Int())
	require.Equal(t, "item_nested", gjson.GetBytes(repaired, "0.content.id").String())
	require.Equal(t, "rs_valid", gjson.GetBytes(repaired, "1.id").String())
	require.Equal(t, "item_message", gjson.GetBytes(repaired, "2.id").String())
	require.Equal(t, "item_call", gjson.GetBytes(repaired, "3.id").String())
	require.Equal(t, "item_tool_payload", gjson.GetBytes(repaired, "4.output.id").String())
}

func TestCodexResponsesRepairsInvalidItemReferenceWithPairedToolContext(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"continue"},
			{"type":"item_reference","id":"item_local_reasoning"},
			{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, int64(3), gjson.GetBytes(request.Input, "#").Int())
	require.False(t, gjson.GetBytes(request.Input, `#(type=="item_reference")`).Exists())
	require.True(t, gjson.GetBytes(request.Input, `#(id=="fc_1")`).Exists())
	value, exists := ctx.Get("codex_input_repair_admin_info")
	require.True(t, exists)
	require.Equal(t, 1, value.(map[string]interface{})["dropped_item_references"])
}

func TestCodexResponsesRejectsRepairThatWouldOrphanToolOutput(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"message","role":"user","content":"continue"},
			{"type":"item_reference","id":"item_local_call"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
	})
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestCodexResponsesRejectsInputContainingOnlyInvalidLocalReasoning(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`{"type":"reasoning","id":"item_local_reasoning","encrypted_content":"invalid"}`),
	})
	require.Error(t, err)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	require.True(t, types.IsSkipRetryError(apiErr))
}

func TestCodexCompactDoesNotApplyOrdinaryResponsesHistoryRepair(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"item_compact_owned"},
			{"type":"message","role":"user","content":"compact"}
		]`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, "item_compact_owned", gjson.GetBytes(request.Input, "0.id").String())
	_, exists := ctx.Get("codex_input_repair_admin_info")
	require.False(t, exists)
}

func TestNormalizeCodexResponsesPromotesSystemMessages(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{
		Instructions: []byte(`"Existing."`),
		Input: []byte(`[
			{"type":"message","role":"system","content":"First."},
			{"type":"message","role":"system","content":[
				{"type":"input_text","text":"Second "},
				{"type":"output_text","text":"and third."},
				{"type":"input_image","image_url":"https://example.com/a.png","metadata":{"id":9007199254740993}}
			]},
			{"type":"message","role":"user","content":"hello"},
			{"type":"function_call_output","call_id":"call_1","output":{"role":"system","id":9007199254740995}}
		]`),
	}

	require.NoError(t, normalizeCodexResponsesRequest(&request))
	require.Equal(t, "First.\n\nSecond and third.\n\nExisting.", gjson.ParseBytes(request.Instructions).String())
	require.Equal(t, "developer", gjson.GetBytes(request.Input, "0.role").String())
	require.Equal(t, "developer", gjson.GetBytes(request.Input, "1.role").String())
	require.Equal(t, "user", gjson.GetBytes(request.Input, "2.role").String())
	require.Equal(t, "system", gjson.GetBytes(request.Input, "3.output.role").String())
	require.Contains(t, string(request.Input), `"id":9007199254740993`)
	require.Contains(t, string(request.Input), `"id":9007199254740995`)
}

func TestPromoteCodexSystemMessagesIsIdempotent(t *testing.T) {
	t.Parallel()
	request := dto.OpenAIResponsesRequest{
		Instructions: []byte(`"Existing."`),
		Input:        []byte(`[{"role":"system","content":"System."},{"role":"user","content":"hello"}]`),
	}
	require.NoError(t, promoteCodexSystemMessages(&request))
	firstInput := append([]byte(nil), request.Input...)
	firstInstructions := append([]byte(nil), request.Instructions...)
	require.NoError(t, promoteCodexSystemMessages(&request))
	require.JSONEq(t, string(firstInput), string(request.Input))
	require.JSONEq(t, string(firstInstructions), string(request.Instructions))
	require.Equal(t, 1, strings.Count(gjson.ParseBytes(request.Instructions).String(), "System."))
}

func TestCodexSystemPromptMergeModes(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name         string
		instructions json.RawMessage
		override     bool
		want         string
	}{
		{name: "missing client instructions keeps channel prefix", want: "Channel.\nClient."},
		{name: "client instructions without override", instructions: json.RawMessage(`"Existing."`), want: "Client.\n\nExisting."},
		{name: "client instructions with override", instructions: json.RawMessage(`"Existing."`), override: true, want: "Channel.\nClient.\n\nExisting."},
	}
	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
				RelayMode: relayconstant.RelayModeResponses,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{
					SystemPrompt:         "Channel.",
					SystemPromptOverride: testCase.override,
				}},
			}, dto.OpenAIResponsesRequest{
				Model:        "gpt-5.6-sol",
				Instructions: testCase.instructions,
				Input:        []byte(`[{"role":"system","content":"Client."},{"role":"user","content":"hello"}]`),
			})
			require.NoError(t, err)
			request := converted.(dto.OpenAIResponsesRequest)
			require.Equal(t, testCase.want, gjson.ParseBytes(request.Instructions).String())
			require.Equal(t, "developer", gjson.GetBytes(request.Input, "0.role").String())
		})
	}
}

func TestCodexCompactPromotesSystemWithoutBroadNormalization(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	falseValue := false
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponsesCompact,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, dto.OpenAIResponsesRequest{
		Model:    "gpt-5.6-sol",
		Input:    []byte(`{"role":"system","content":"Compact guidance.","metadata":{"id":9007199254740993}}`),
		Stream:   &falseValue,
		Metadata: []byte(`{"keep":"compact"}`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, byte('{'), bytes.TrimSpace(request.Input)[0])
	require.Equal(t, "developer", gjson.GetBytes(request.Input, "role").String())
	require.Equal(t, "Compact guidance.", gjson.ParseBytes(request.Instructions).String())
	require.Contains(t, string(request.Input), `"id":9007199254740993`)
	require.NotNil(t, request.Stream)
	require.False(t, *request.Stream)
	require.JSONEq(t, `{"keep":"compact"}`, string(request.Metadata))
}

func TestChatCompletionsSystemIsNotDuplicatedByCodexNormalization(t *testing.T) {
	t.Parallel()
	var chatRequest dto.GeneralOpenAIRequest
	require.NoError(t, common.UnmarshalJsonStr(`{
		"model":"gpt-5.6-sol",
		"messages":[
			{"role":"system","content":"Chat guidance."},
			{"role":"user","content":"hello"}
		]
	}`, &chatRequest))
	responsesRequest, err := service.ChatCompletionsRequestToResponsesRequest(&chatRequest)
	require.NoError(t, err)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, *responsesRequest)
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	instructions := gjson.ParseBytes(request.Instructions).String()
	require.Equal(t, "Chat guidance.", instructions)
	require.Equal(t, 1, strings.Count(instructions, "Chat guidance."))
	require.False(t, gjson.GetBytes(request.Input, `#(role=="system")`).Exists())
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

func TestCodexNativeResponsesWebSocketRepairsOnlyInvalidLocalReasoning(t *testing.T) {
	t.Parallel()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyResponsesWebSocketIngress, true)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
	info.ChannelOtherSettings.ResponsesWebSocketV2Mode = model.UpstreamOpenAIWSModePassthrough
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, info, dto.OpenAIResponsesRequest{
		Model:              "gpt-5.6-sol",
		PreviousResponseID: "resp_previous",
		Input: json.RawMessage(`[
			{"type":"reasoning","id":"item_local_reasoning"},
			{"type":"reasoning","id":"rs_valid","encrypted_content":"valid"},
			{"type":"message","role":"user","content":"continue"}
		]`),
	})
	require.NoError(t, err)
	request := converted.(dto.OpenAIResponsesRequest)
	require.Equal(t, "resp_previous", request.PreviousResponseID)
	require.Equal(t, int64(2), gjson.GetBytes(request.Input, "#").Int())
	require.Equal(t, "rs_valid", gjson.GetBytes(request.Input, "0.id").String())
	require.Equal(t, "user", gjson.GetBytes(request.Input, "1.role").String())
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
		json.RawMessage(`{"type":"reasoning","id":"item_cached_local_reasoning","encrypted_content":"invalid"}`),
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
	require.False(t, gjson.GetBytes(prepared.Input, `#(id=="item_cached_local_reasoning")`).Exists())
	value, exists := ctx.Get("codex_input_repair_admin_info")
	require.True(t, exists)
	require.Equal(t, 1, value.(map[string]interface{})["dropped_reasoning_items"])
}

func TestCodexHTTPContinuationPromotesReplayedSystemOnce(t *testing.T) {
	firstCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	firstCtx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(firstCtx, constant.ContextKeyUserId, 8301)
	common.SetContextKey(firstCtx, constant.ContextKeyTokenId, 9301)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelId, 43)
	common.SetContextKey(firstCtx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPoolId, 4)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountId, 10)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(firstCtx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	service.PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{
		Model: "gpt-5.6-sol",
		Input: []byte(`[
			{"role":"system","content":"Replayed guidance."},
			{"role":"user","content":"first turn"}
		]`),
	})
	service.MarkResponsesHTTPContinuationPersistTarget(firstCtx)
	service.CommitResponsesHTTPContinuation(firstCtx, "resp_codex_system", []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"first answer"}]}`),
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 8301)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 9301)
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 43)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPoolId, 4)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 10)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	request := dto.OpenAIResponsesRequest{
		Model:              "gpt-5.6-sol",
		PreviousResponseID: "resp_codex_system",
		Input:              []byte(`[{"role":"user","content":"second turn"}]`),
	}
	service.PrepareResponsesHTTPContinuation(ctx, &request)
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(ctx, &relaycommon.RelayInfo{
		RelayMode:   relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}, request)
	require.NoError(t, err)
	prepared := converted.(dto.OpenAIResponsesRequest)
	require.Empty(t, prepared.PreviousResponseID)
	require.Equal(t, "Replayed guidance.", gjson.ParseBytes(prepared.Instructions).String())
	require.Equal(t, 1, strings.Count(gjson.ParseBytes(prepared.Instructions).String(), "Replayed guidance."))
	require.Equal(t, "developer", gjson.GetBytes(prepared.Input, "0.role").String())
	require.Equal(t, "second turn", gjson.GetBytes(prepared.Input, "3.content").String())
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
