package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestResponsesHTTPRedisWriteContextSurvivesRequestCancellation(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	ctx := newResponsesHTTPContinuationTestContext(t)
	ctx.Request = ctx.Request.WithContext(requestCtx)
	cancelRequest()

	writeCtx, cancelWrite := responsesHTTPRedisWriteContext(ctx)
	defer cancelWrite()
	require.NoError(t, writeCtx.Err())
	require.NotNil(t, writeCtx.Done())
}

func TestResponsesHTTPContinuationRedisWriteCanBeDisabled(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	defaultResponsesHTTPContinuationCache.limits.RedisWriteEnabled = false

	originalRedisEnabled := common.RedisEnabled
	originalRDB := common.RDB
	common.RedisEnabled = true
	common.RDB = nil
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		common.RDB = originalRDB
	})

	ctx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, ctx)
	PrepareResponsesHTTPContinuation(ctx, &dto.OpenAIResponsesRequest{
		Model: "gpt-5",
		Input: json.RawMessage(`"first"`),
	})
	CommitResponsesHTTPContinuation(ctx, "resp_write_disabled", nil)

	require.Equal(t, "local_only", ctx.GetString(responsesHTTPPersistStatusContextKey))
	require.Equal(t, "write_disabled", ctx.GetString(responsesHTTPRedisStatusContextKey))
	require.Equal(t, 1, defaultResponsesHTTPContinuationCache.entryCount())
}

func newResponsesHTTPContinuationTestContext(t testing.TB) *gin.Context {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 7)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 11)
	return ctx
}

func enableResponsesHTTPContinuationPersist(t *testing.T, ctx *gin.Context) {
	t.Helper()
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 42)
	common.SetContextKey(ctx, constant.ContextKeyChannelType, constant.ChannelTypeCodex)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPoolId, 3)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 9)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountPlatform, constant.UpstreamPlatformOpenAI)
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountType, constant.UpstreamAccountTypeOAuth)
	MarkResponsesHTTPContinuationPersistTarget(ctx)
}

func resetResponsesHTTPContinuationTestCache(t *testing.T) {
	t.Helper()
	resetResponsesHTTPContinuationCacheForTest(responsesHTTPContinuationLimits{
		LocalBudgetBytes:   256 << 20,
		LocalMaxEntryBytes: 64 << 20,
		RedisMaxEntryBytes: 128 << 20,
		MaxEntries:         4096,
		TTL:                responsesHTTPContinuationTTL,
		RedisOOMCooldown:   responsesHTTPContinuationRedisOOMCooldown,
		RedisOOMJitter:     0,
	})
	resetResponsesHTTPRedisCircuitForTest()
	originalRedisEnabled := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = originalRedisEnabled
		resetResponsesHTTPContinuationCacheForTest(loadResponsesHTTPContinuationLimits())
		resetResponsesHTTPRedisCircuitForTest()
	})
}

func TestResponsesHTTPContinuationExpandsMatchingFunctionCallOutput(t *testing.T) {
	InitTokenEncoders()
	resetResponsesHTTPContinuationTestCache(t)
	firstCtx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, firstCtx)
	first := &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)}
	PrepareResponsesHTTPContinuation(firstCtx, first)
	CommitResponsesHTTPContinuation(firstCtx, "resp_first", []json.RawMessage{
		json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}`),
	})

	secondCtx := newResponsesHTTPContinuationTestContext(t)
	second := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_first",
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	PrepareResponsesHTTPContinuation(secondCtx, second)
	billingMeta, expanded := ResponsesHTTPContinuationTokenCountMeta(secondCtx, second)
	require.True(t, expanded)
	require.Empty(t, billingMeta.CombineText)
	require.NotNil(t, billingMeta.PrecomputedTextTokens)
	require.Positive(t, *billingMeta.PrecomputedTextTokens)
	require.Nil(t, ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
	require.Equal(t, "expanded", secondCtx.GetString(responsesHTTPStatusContextKey))
	require.Equal(t, int64(3), gjson.GetBytes(second.Input, "#").Int())
	require.Equal(t, "function_call", gjson.GetBytes(second.Input, "1.type").String())
	require.Equal(t, "function_call_output", gjson.GetBytes(second.Input, "2.type").String())
}

func TestResponsesHTTPContinuationTokenCountIsConservativeWithoutCombiningInput(t *testing.T) {
	InitTokenEncoders()
	request := &dto.OpenAIResponsesRequest{
		Model:        "gpt-5",
		Instructions: json.RawMessage(`"follow the tool policy"`),
		Tools:        json.RawMessage(`[{"type":"function","name":"lookup"}]`),
	}
	items := []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"user","content":"first question"}`),
		json.RawMessage(`{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"q\":\"value\"}"}`),
		json.RawMessage(`{"type":"function_call_output","call_id":"call_1","output":"answer"}`),
	}

	encoded, err := common.Marshal(items)
	require.NoError(t, err)
	legacyText := strings.TrimSpace(string(encoded)) + "\n" + string(request.Instructions) + "\n" + string(request.Tools)
	legacyTokens := CountTextToken(legacyText, request.Model)
	streamedTokens := countResponsesHTTPContinuationTextTokens(items, request, request.Model)

	require.GreaterOrEqual(t, streamedTokens, legacyTokens)
	require.LessOrEqual(t, streamedTokens-legacyTokens, len(items)+4)
}

func TestResponsesHTTPContinuationTokenCountPreservesInputFiles(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_image","image_url":"https://example.com/a.png"},{"type":"input_file","file_url":{"url":"https://example.com/a.pdf"}}]}`),
	}

	files := responsesHTTPContinuationFileMeta(items)
	require.Len(t, files, 2)
	require.Equal(t, types.FileTypeImage, files[0].FileType)
	require.Equal(t, types.FileTypeFile, files[1].FileType)
}

func TestEstimateRequestTokenUsesPrecomputedContinuationTokens(t *testing.T) {
	originalCountToken := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = originalCountToken })

	ctx := newResponsesHTTPContinuationTestContext(t)
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, "gpt-5")
	precomputed := 12345
	tokens, err := EstimateRequestToken(ctx, &types.TokenCountMeta{
		CombineText:           "must not be counted",
		PrecomputedTextTokens: &precomputed,
	}, &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses})
	require.NoError(t, err)
	require.Equal(t, precomputed, tokens)
}

func BenchmarkResponsesHTTPContinuationTokenCountMeta(b *testing.B) {
	InitTokenEncoders()
	payload := strings.Repeat("continuation payload ", 2048)
	items := make([]json.RawMessage, 64)
	for i := range items {
		item, err := common.Marshal(map[string]any{
			"type":    "message",
			"role":    "assistant",
			"content": payload,
		})
		require.NoError(b, err)
		items[i] = item
	}
	request := &dto.OpenAIResponsesRequest{
		Model:        "gpt-5",
		Instructions: json.RawMessage(`"follow the instructions"`),
	}
	ctx := newResponsesHTTPContinuationTestContext(b)
	common.SetContextKey(ctx, constant.ContextKeyOriginalModel, request.Model)
	ctx.Set(responsesHTTPContinuationContextKey, &responsesHTTPContinuationPreparation{
		FullInput:       items,
		FullInputExists: true,
		StateFound:      true,
		ReplayComplete:  true,
	})

	b.Run("incremental", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			meta, ok := ResponsesHTTPContinuationTokenCountMeta(ctx, request)
			if !ok || meta.PrecomputedTextTokens == nil {
				b.Fatal("incremental token metadata unavailable")
			}
		}
	})

	b.Run("legacy_combined", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			meta, ok := legacyResponsesHTTPContinuationTokenCountMetaForBenchmark(items, request)
			if !ok {
				b.Fatal("legacy token metadata unavailable")
			}
			_ = CountTextToken(meta.CombineText, request.Model)
		}
	})
}

func legacyResponsesHTTPContinuationTokenCountMetaForBenchmark(items []json.RawMessage, request *dto.OpenAIResponsesRequest) (*types.TokenCountMeta, bool) {
	expanded, err := common.DeepCopy(request)
	if err != nil {
		return nil, false
	}
	expanded.Input, err = common.Marshal(items)
	if err != nil {
		return nil, false
	}
	meta := expanded.GetTokenCountMeta()
	inputJSON := strings.TrimSpace(string(expanded.Input))
	withoutInput, err := common.DeepCopy(expanded)
	if err != nil {
		return nil, false
	}
	withoutInput.Input = nil
	otherMeta := withoutInput.GetTokenCountMeta()
	meta.CombineText = inputJSON
	if strings.TrimSpace(otherMeta.CombineText) != "" {
		meta.CombineText += "\n" + otherMeta.CombineText
	}
	return meta, true
}

func TestResponsesHTTPContinuationExposesCreatingAccountAsAffinityHint(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	firstCtx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, firstCtx)
	PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})
	CommitResponsesHTTPContinuation(firstCtx, "resp_affinity", []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}`),
	})

	secondCtx := newResponsesHTTPContinuationTestContext(t)
	PrepareResponsesHTTPContinuation(secondCtx, &dto.OpenAIResponsesRequest{
		Model: "gpt-5", PreviousResponseID: "resp_affinity", Input: json.RawMessage(`"second"`),
	})
	require.Equal(t, 9, ResponsesHTTPContinuationPreferredAccountID(secondCtx))
}

func TestResponsesHTTPContinuationRollsBackUnansweredCallForPlainMessage(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	firstCtx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, firstCtx)
	PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})
	CommitResponsesHTTPContinuation(firstCtx, "resp_tool", []json.RawMessage{
		json.RawMessage(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}`),
	})

	secondCtx := newResponsesHTTPContinuationTestContext(t)
	second := &dto.OpenAIResponsesRequest{Model: "gpt-5", PreviousResponseID: "resp_tool", Input: json.RawMessage(`"revised"`)}
	PrepareResponsesHTTPContinuation(secondCtx, second)
	require.Nil(t, ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
	require.Equal(t, int64(2), gjson.GetBytes(second.Input, "#").Int())
	require.False(t, gjson.GetBytes(second.Input, `#(type=="function_call")`).Exists())
	require.Equal(t, "revised", gjson.GetBytes(second.Input, "1.content").String())
}

func TestResponsesHTTPContinuationRejectsOrphanToolOutput(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	ctx := newResponsesHTTPContinuationTestContext(t)
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_missing",
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	PrepareResponsesHTTPContinuation(ctx, request)
	apiErr := ApplyResponsesHTTPContinuationForCodex(ctx, request)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
	require.Equal(t, "orphan_tool_output", ctx.GetString(responsesHTTPStatusContextKey))
}

func TestResponsesHTTPContinuationAcceptsSelfContainedToolOutput(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	ctx := newResponsesHTTPContinuationTestContext(t)
	request := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_external",
		Input: json.RawMessage(`[
			{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","output":"ok"}
		]`),
	}
	PrepareResponsesHTTPContinuation(ctx, request)
	require.Nil(t, ApplyResponsesHTTPContinuationForCodex(ctx, request))
}

func TestResponsesHTTPContinuationRecordsOnlyDeliveredToolCall(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	firstCtx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, firstCtx)
	PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})
	RecordDeliveredResponsesHTTPEvent(firstCtx, []byte(`{"type":"response.created","response":{"id":"resp_stream"}}`))
	RecordDeliveredResponsesHTTPEvent(firstCtx, []byte(`{
		"type":"response.output_item.done",
		"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"lookup","arguments":"{}"}
	}`))
	// Intermediate item must not commit yet.
	beforeCtx := newResponsesHTTPContinuationTestContext(t)
	before := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_stream",
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	PrepareResponsesHTTPContinuation(beforeCtx, before)
	require.NotNil(t, ApplyResponsesHTTPContinuationForCodex(beforeCtx, before))
	require.Equal(t, "orphan_tool_output", beforeCtx.GetString(responsesHTTPStatusContextKey))

	FinalizeResponsesHTTPContinuationStream(firstCtx)
	secondCtx := newResponsesHTTPContinuationTestContext(t)
	second := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_stream",
		Input:              json.RawMessage(`[{"type":"function_call_output","call_id":"call_1","output":"ok"}]`),
	}
	PrepareResponsesHTTPContinuation(secondCtx, second)
	require.Nil(t, ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
	require.Equal(t, int64(3), gjson.GetBytes(second.Input, "#").Int())
}

func TestResponsesHTTPContinuationSupportsLocalShellOutput(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	firstCtx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, firstCtx)
	PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"run"`)})
	CommitResponsesHTTPContinuation(firstCtx, "resp_shell", []json.RawMessage{
		json.RawMessage(`{"type":"local_shell_call","id":"sh_1","call_id":"shell_1","action":{"type":"exec","command":"pwd"}}`),
	})

	secondCtx := newResponsesHTTPContinuationTestContext(t)
	second := &dto.OpenAIResponsesRequest{
		Model:              "gpt-5",
		PreviousResponseID: "resp_shell",
		Input:              json.RawMessage(`[{"type":"local_shell_call_output","call_id":"shell_1","output":"/tmp"}]`),
	}
	PrepareResponsesHTTPContinuation(secondCtx, second)
	require.Nil(t, ApplyResponsesHTTPContinuationForCodex(secondCtx, second))
}

func TestResponsesHTTPContinuationRejectsModelConflictForCodex(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	firstCtx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, firstCtx)
	PrepareResponsesHTTPContinuation(firstCtx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})
	CommitResponsesHTTPContinuation(firstCtx, "resp_model", nil)

	secondCtx := newResponsesHTTPContinuationTestContext(t)
	second := &dto.OpenAIResponsesRequest{Model: "gpt-5-mini", PreviousResponseID: "resp_model", Input: json.RawMessage(`"second"`)}
	PrepareResponsesHTTPContinuation(secondCtx, second)
	apiErr := ApplyResponsesHTTPContinuationForCodex(secondCtx, second)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
}

func TestResponsesHTTPContinuationDoesNotPromotePartialHistory(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	partialCtx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, partialCtx)
	partial := &dto.OpenAIResponsesRequest{Model: "gpt-5", PreviousResponseID: "resp_external_missing", Input: json.RawMessage(`"second"`)}
	PrepareResponsesHTTPContinuation(partialCtx, partial)
	CommitResponsesHTTPContinuation(partialCtx, "resp_partial_child", []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}`),
	})

	nextCtx := newResponsesHTTPContinuationTestContext(t)
	next := &dto.OpenAIResponsesRequest{Model: "gpt-5", PreviousResponseID: "resp_partial_child", Input: json.RawMessage(`"third"`)}
	PrepareResponsesHTTPContinuation(nextCtx, next)
	_, expanded := ResponsesHTTPContinuationTokenCountMeta(nextCtx, next)
	require.False(t, expanded)
}

func TestResponsesHTTPContinuationSkipsCommitWithoutPersistTarget(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	ctx := newResponsesHTTPContinuationTestContext(t)
	PrepareResponsesHTTPContinuation(ctx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})
	CommitResponsesHTTPContinuation(ctx, "resp_skipped", []json.RawMessage{
		json.RawMessage(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"x"}]}`),
	})
	require.Equal(t, "skipped_scope", ctx.GetString(responsesHTTPPersistStatusContextKey))
	require.Equal(t, 0, defaultResponsesHTTPContinuationCache.entryCount())
}

func TestResponsesHTTPContinuationRejectsPersistFingerprintMismatch(t *testing.T) {
	resetResponsesHTTPContinuationTestCache(t)
	ctx := newResponsesHTTPContinuationTestContext(t)
	enableResponsesHTTPContinuationPersist(t, ctx)
	PrepareResponsesHTTPContinuation(ctx, &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"first"`)})
	common.SetContextKey(ctx, constant.ContextKeyUpstreamAccountId, 99)
	CommitResponsesHTTPContinuation(ctx, "resp_mismatch", nil)
	require.Equal(t, "skipped_scope", ctx.GetString(responsesHTTPPersistStatusContextKey))
	require.Equal(t, 0, defaultResponsesHTTPContinuationCache.entryCount())
}
