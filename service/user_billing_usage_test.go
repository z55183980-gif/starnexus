package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestProjectUserBillingUsageOpenAIUsesBillingValuesWithoutMutatingRaw(t *testing.T) {
	restore := billing_setting.SetTokenPricingSettingForTest(billing_setting.TokenPricingSetting{
		Enabled:     true,
		InputRatio:  1.05,
		OutputRatio: 1.05,
	})
	t.Cleanup(restore)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-projection-test",
		PriceData: types.PriceData{
			CacheBillingOffsetBps: 300,
		},
	}
	raw := &dto.Usage{
		PromptTokens:     214232,
		CompletionTokens: 168,
		TotalTokens:      214400,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 213760,
		},
		Cost: 12.34,
	}

	projected := ProjectUserBillingUsage(info, raw)

	require.Equal(t, 224944, projected.PromptTokens)
	require.Equal(t, 176, projected.CompletionTokens)
	require.Equal(t, 225120, projected.TotalTokens)
	require.Equal(t, 217700, projected.PromptTokensDetails.CachedTokens)
	require.Equal(t, projected.PromptTokensDetails.CachedTokens, projected.InputTokensDetails.CachedTokens)
	require.Empty(t, projected.UsageSemantic)
	require.Empty(t, projected.UsageSource)
	require.Nil(t, projected.Cost)

	require.Equal(t, 214232, raw.PromptTokens)
	require.Equal(t, 168, raw.CompletionTokens)
	require.Equal(t, 213760, raw.PromptTokensDetails.CachedTokens)
	require.Equal(t, 12.34, raw.Cost)
}

func TestOpenAIConversionsProjectTargetTokenBucketsOnce(t *testing.T) {
	restore := billing_setting.SetTokenPricingSettingForTest(billing_setting.TokenPricingSetting{
		Enabled:     true,
		InputRatio:  1.05,
		OutputRatio: 1.05,
	})
	t.Cleanup(restore)

	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-conversion-projection-test",
		PriceData: types.PriceData{
			CacheBillingOffsetBps: 300,
		},
	}
	response := &dto.OpenAITextResponse{Usage: dto.Usage{
		PromptTokens:     214232,
		CompletionTokens: 168,
		TotalTokens:      214400,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 213760,
		},
	}}

	claude := ResponseOpenAI2Claude(response, info)
	require.NotNil(t, claude.Usage)
	require.Equal(t, 7244, claude.Usage.InputTokens)
	require.Equal(t, 217700, claude.Usage.CacheReadInputTokens)
	require.Equal(t, 224944, claude.Usage.InputTokens+claude.Usage.CacheReadInputTokens)
	require.Equal(t, 176, claude.Usage.OutputTokens)

	gemini := ResponseOpenAI2Gemini(response, info)
	require.Equal(t, 224944, gemini.UsageMetadata.PromptTokenCount)
	require.Equal(t, 217700, gemini.UsageMetadata.CachedContentTokenCount)
	require.Equal(t, 176, gemini.UsageMetadata.CandidatesTokenCount)
	require.Equal(t, 225120, gemini.UsageMetadata.TotalTokenCount)

	// Target-format projection must not mutate the raw settlement usage.
	require.Equal(t, 214232, response.Usage.PromptTokens)
	require.Equal(t, 213760, response.Usage.PromptTokensDetails.CachedTokens)
}

func TestProjectUserBillingUsageClaudeMovesReclassifiedCacheIntoInput(t *testing.T) {
	restore := billing_setting.SetTokenPricingSettingForTest(billing_setting.TokenPricingSetting{
		Enabled:     true,
		InputRatio:  1.05,
		OutputRatio: 1.05,
	})
	t.Cleanup(restore)

	info := &relaycommon.RelayInfo{
		OriginModelName:         "claude-projection-test",
		FinalRequestRelayFormat: types.RelayFormatClaude,
		PriceData: types.PriceData{
			CacheBillingOffsetBps: 300,
		},
	}
	raw := &dto.Usage{
		PromptTokens:     609,
		CompletionTokens: 168,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 9391,
		},
	}

	projected := ProjectUserBillingUsage(info, raw)

	// 609*1.05=639, 9391*1.05=9861, total input=10500,
	// reclassified=315. Anthropic keeps the total by moving that bucket.
	require.Equal(t, 954, projected.PromptTokens)
	require.Equal(t, 9546, projected.PromptTokensDetails.CachedTokens)
	require.Equal(t, 10500, projected.PromptTokens+projected.PromptTokensDetails.CachedTokens)
	require.Equal(t, 176, projected.CompletionTokens)
	require.Equal(t, 1130, projected.TotalTokens)
	require.Equal(t, 609, raw.PromptTokens)
	require.Equal(t, 9391, raw.PromptTokensDetails.CachedTokens)
}

func TestProjectUserBillingUsageTieredRequiresExplicitCacheVariable(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens: 10000,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 9391,
		},
	}

	withoutCache := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:           billing_setting.BillingModeTieredExpr,
			ExprString:            `tier("base", p * 2)`,
			CacheBillingOffsetBps: 300,
		},
	}
	require.Equal(t, 9391, ProjectUserBillingUsage(withoutCache, usage).PromptTokensDetails.CachedTokens)

	withCache := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:           billing_setting.BillingModeTieredExpr,
			ExprString:            `tier("base", p * 2 + cr * 0.2)`,
			CacheBillingOffsetBps: 300,
		},
	}
	require.Equal(t, 9091, ProjectUserBillingUsage(withCache, usage).PromptTokensDetails.CachedTokens)
}

func TestProjectClaudeAndGeminiUsageForUser(t *testing.T) {
	restore := billing_setting.SetTokenPricingSettingForTest(billing_setting.TokenPricingSetting{
		Enabled:     true,
		InputRatio:  2,
		OutputRatio: 3,
	})
	t.Cleanup(restore)

	info := &relaycommon.RelayInfo{PriceData: types.PriceData{CacheBillingOffsetBps: 1000}}
	claude := ProjectClaudeUsageForUser(info, &dto.ClaudeUsage{
		InputTokens:              10,
		OutputTokens:             4,
		CacheReadInputTokens:     90,
		CacheCreationInputTokens: 10,
		CacheCreation: &dto.ClaudeCacheCreationUsage{
			Ephemeral5mInputTokens: 6,
			Ephemeral1hInputTokens: 4,
		},
	})
	require.Equal(t, 42, claude.InputTokens)
	require.Equal(t, 12, claude.OutputTokens)
	require.Equal(t, 158, claude.CacheReadInputTokens)
	require.Equal(t, 20, claude.CacheCreationInputTokens)
	require.Equal(t, 12, claude.CacheCreation.Ephemeral5mInputTokens)
	require.Equal(t, 8, claude.CacheCreation.Ephemeral1hInputTokens)

	gemini := ProjectGeminiUsageForUser(info, dto.GeminiUsageMetadata{
		PromptTokenCount:        80,
		ToolUsePromptTokenCount: 20,
		CandidatesTokenCount:    3,
		ThoughtsTokenCount:      1,
		TotalTokenCount:         104,
		CachedContentTokenCount: 90,
	}, 0)
	require.Equal(t, 160, gemini.PromptTokenCount)
	require.Equal(t, 40, gemini.ToolUsePromptTokenCount)
	require.Equal(t, 9, gemini.CandidatesTokenCount)
	require.Equal(t, 3, gemini.ThoughtsTokenCount)
	require.Equal(t, 212, gemini.TotalTokenCount)
	require.Equal(t, 160, gemini.CachedContentTokenCount)
}

func TestUsageJSONRewritesExposeOnlyProjectedNumbers(t *testing.T) {
	usage := &dto.Usage{
		PromptTokens:     120,
		CompletionTokens: 30,
		TotalTokens:      150,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 80,
			TextTokens:   10,
		},
		CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 4},
	}
	body := []byte(`{"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":90,"text_tokens":10},"completion_tokens_details":{"reasoning_tokens":4},"cache_read_input_tokens":90,"cost":0.42,"cost_details":{"upstream_inference_cost":0.21},"token_pricing_input_ratio":1.2,"raw_prompt_tokens":100,"admin_info":{"cache_billing":{"offset_bps":300}}},"internal":"keep"}`)

	rewritten, err := RewriteOpenAIUsageJSON(body, "usage", usage, false)
	require.NoError(t, err)
	require.JSONEq(t, `{"usage":{"prompt_tokens":120,"completion_tokens":30,"total_tokens":150,"prompt_tokens_details":{"cached_tokens":80,"text_tokens":10},"completion_tokens_details":{"reasoning_tokens":4},"cache_read_input_tokens":80},"internal":"keep"}`, string(rewritten))
	require.NotContains(t, string(rewritten), "ratio")
	require.NotContains(t, string(rewritten), "offset")
	require.NotContains(t, string(rewritten), "raw_")
	require.NotContains(t, string(rewritten), "cost")

	geminiWithoutUsage := []byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]}}]}`)
	unchanged, err := RewriteGeminiUsageJSON(geminiWithoutUsage, dto.GeminiUsageMetadata{PromptTokenCount: 120})
	require.NoError(t, err)
	require.Equal(t, geminiWithoutUsage, unchanged)
}

func TestProjectUserBillingUsageClampsNegativeVisibleCounts(t *testing.T) {
	projected := ProjectUserBillingUsage(&relaycommon.RelayInfo{}, &dto.Usage{
		PromptTokens:     -1,
		CompletionTokens: -2,
		TotalTokens:      -3,
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: -4,
		},
	})
	require.Zero(t, projected.PromptTokens)
	require.Zero(t, projected.CompletionTokens)
	require.Zero(t, projected.TotalTokens)
	require.Zero(t, projected.PromptTokensDetails.CachedTokens)
}
