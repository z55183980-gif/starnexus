package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRaiseCodexResponsesPromptTokens(t *testing.T) {
	t.Parallel()

	require.True(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeOpenAI, "/v1/responses", 4436, 56))
	require.True(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeOpenAI, "/v1/responses/?stream=true", 4436, 56))
	require.False(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeOpenAI, "/v1/chat/completions", 4436, 56))
	require.False(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeOpenAI, "", 4436, 56))
	require.False(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeCodex, "", 0, 56))
	require.False(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeCodex, "", 4000, 0))
	require.False(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeCodex, "", 100, 100))
	require.False(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeCodex, "", 300, 100)) // abs gap < 256
	require.False(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeCodex, "", 500, 300)) // not half
	require.True(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeCodex, "", 4436, 56))
	require.True(t, ShouldRaiseCodexResponsesPromptTokens(constant.ChannelTypeCodex, "", 1000, 56))
}

func TestReconcileCodexResponsesUsageRaisesObviousUndercount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex},
	}
	info.SetEstimatePromptTokens(4436)

	usage := &dto.Usage{
		PromptTokens:     56,
		CompletionTokens: 18,
		TotalTokens:      74,
	}
	usage.PromptTokensDetails.CachedTokens = 0
	usage.PromptTokensDetails.CacheWriteTokens = 12

	ReconcileCodexResponsesUsage(ctx, info, usage)

	require.Equal(t, 4436, usage.PromptTokens)
	require.Equal(t, 4436, usage.InputTokens)
	require.Equal(t, 18, usage.CompletionTokens)
	require.Equal(t, 4454, usage.TotalTokens)
	// Upstream total is garbage (< estimate/10); impute ~13.4% uncached.
	require.Equal(t, 3842, usage.PromptTokensDetails.CachedTokens) // 4436 - 594
	require.Equal(t, 12, usage.PromptTokensDetails.CacheWriteTokens)

	reconcile := getCodexUsageReconcileInfo(ctx)
	require.NotNil(t, reconcile)
	require.Equal(t, 56, reconcile.UpstreamPromptTokens)
	require.Equal(t, 0, reconcile.UpstreamCacheTokens)
	require.Equal(t, codexUsageReconcileReason, reconcile.Reason)
}

func TestReconcileCodexResponsesUsagePreservesUpstreamUncachedRemainder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex},
	}
	info.SetEstimatePromptTokens(5000)

	usage := &dto.Usage{
		PromptTokens:     1000,
		CompletionTokens: 10,
		TotalTokens:      1010,
	}
	usage.PromptTokensDetails.CachedTokens = 800

	ReconcileCodexResponsesUsage(ctx, info, usage)

	require.Equal(t, 5000, usage.PromptTokens)
	require.Equal(t, 5000, usage.InputTokens)
	// Upstream total is still plausible (>= estimate/10); keep uncached=200.
	require.Equal(t, 4800, usage.PromptTokensDetails.CachedTokens)
	reconcile := getCodexUsageReconcileInfo(ctx)
	require.NotNil(t, reconcile)
	require.Equal(t, 1000, reconcile.UpstreamPromptTokens)
	require.Equal(t, 800, reconcile.UpstreamCacheTokens)
}

func TestReconcileOpenAICompatibleResponsesUsageRaisesObviousUndercount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	info := &relaycommon.RelayInfo{
		ChannelMeta:    &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
		RequestURLPath: "/v1/responses",
	}
	info.SetEstimatePromptTokens(4436)
	usage := &dto.Usage{PromptTokens: 56, CompletionTokens: 18, TotalTokens: 74}

	ReconcileCodexResponsesUsage(ctx, info, usage)

	require.Equal(t, 4436, usage.PromptTokens)
	require.Equal(t, 18, usage.CompletionTokens)
	require.Equal(t, 4454, usage.TotalTokens)
	require.Equal(t, 3842, usage.PromptTokensDetails.CachedTokens)
	require.NotNil(t, getCodexUsageReconcileInfo(ctx))
}

func TestReconcileCodexResponsesUsageSkipsNonCodexAndNoise(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)

	openAI := &relaycommon.RelayInfo{
		ChannelMeta:    &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI},
		RequestURLPath: "/v1/chat/completions",
	}
	openAI.SetEstimatePromptTokens(4436)
	usage := &dto.Usage{PromptTokens: 56, CompletionTokens: 18, TotalTokens: 74}
	ReconcileCodexResponsesUsage(ctx, openAI, usage)
	require.Equal(t, 56, usage.PromptTokens)
	require.Nil(t, getCodexUsageReconcileInfo(ctx))

	codex := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeCodex},
	}
	codex.SetEstimatePromptTokens(59)
	small := &dto.Usage{PromptTokens: 56, CompletionTokens: 18, TotalTokens: 74}
	ReconcileCodexResponsesUsage(ctx, codex, small)
	require.Equal(t, 56, small.PromptTokens)
	require.Nil(t, getCodexUsageReconcileInfo(ctx))
}
