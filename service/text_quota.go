package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type textQuotaSummary struct {
	PromptTokens                 int
	CompletionTokens             int
	TotalTokens                  int
	RawPromptTokens              int
	RawCompletionTokens          int
	RawTotalTokens               int
	CacheTokens                  int
	BillingCacheTokens           int
	CacheReclassifiedTokens      int
	CacheBillingTotalInputTokens int
	CacheBillingOffsetBps        int
	CacheBillingApplied          bool
	CacheCreationTokens          int
	CacheCreationTokens5m        int
	CacheCreationTokens1h        int
	ImageTokens                  int
	AudioTokens                  int
	ModelName                    string
	TokenName                    string
	UseTimeSeconds               int64
	UseTimeMilliseconds          int64
	CompletionRatio              float64
	CacheRatio                   float64
	ImageRatio                   float64
	ModelRatio                   float64
	GroupRatio                   float64
	ModelPrice                   float64
	CacheCreationRatio           float64
	CacheCreationRatio5m         float64
	CacheCreationRatio1h         float64
	Quota                        int
	IsClaudeUsageSemantic        bool
	UsageSemantic                string
	WebSearchPrice               float64
	WebSearchCallCount           int
	ClaudeWebSearchPrice         float64
	ClaudeWebSearchCallCount     int
	FileSearchPrice              float64
	FileSearchCallCount          int
	AudioInputPrice              float64
	ImageGenerationCallPrice     float64
	ToolCallSurchargeQuota       decimal.Decimal
	TokenPricingEnabled          bool
	TokenPricingInputRatio       float64
	TokenPricingOutputRatio      float64
	LongContextBillingApplied    bool
}

const (
	openAIAccountLongContextInputThreshold   = 272000
	openAIAccountLongContextInputMultiplier  = 2.0
	openAIAccountLongContextOutputMultiplier = 1.5
)

func (s *textQuotaSummary) hasBillableUsage() bool {
	return s.RawTotalTokens > 0 || !s.ToolCallSurchargeQuota.IsZero()
}

func tokenPricingContextFromRelayInfo(relayInfo *relaycommon.RelayInfo) billing_setting.TokenPricingContext {
	if relayInfo == nil {
		return billing_setting.TokenPricingContext{}
	}
	group := relayInfo.UsingGroup
	if group == "" {
		group = relayInfo.UserGroup
	}
	return billing_setting.TokenPricingContext{
		Model:  relayInfo.OriginModelName,
		Group:  group,
		UserId: relayInfo.UserId,
	}
}

// effectiveChannelGroupRatio returns the same group ratio used by ordinary
// channels. Account-pool routing must not change the user's channel price;
// account rate multipliers remain execution metadata only.
func effectiveChannelGroupRatio(relayInfo *relaycommon.RelayInfo) float64 {
	if relayInfo == nil {
		return 1
	}
	return relayInfo.PriceData.GroupRatioInfo.GroupRatio
}

func cacheWriteTokensTotal(summary textQuotaSummary) int {
	if summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0 {
		splitCacheWriteTokens := summary.CacheCreationTokens5m + summary.CacheCreationTokens1h
		if summary.CacheCreationTokens > splitCacheWriteTokens {
			return summary.CacheCreationTokens
		}
		return splitCacheWriteTokens
	}
	return summary.CacheCreationTokens
}

func applyCacheBillingOffset(summary *textQuotaSummary, priceData types.PriceData, legacyClaudeDerived bool) {
	if summary == nil {
		return
	}
	summary.BillingCacheTokens = summary.CacheTokens
	offsetBps := priceData.CacheBillingOffsetBps
	if priceData.UsePrice || offsetBps <= 0 || offsetBps > 10000 || summary.CacheTokens <= 0 {
		return
	}

	cacheReadTokens := int64(max(summary.CacheTokens, 0))
	cacheCreationTokens := int64(max(cacheWriteTokensTotal(*summary), 0))
	uncachedInputTokens := int64(max(summary.PromptTokens, 0))
	if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
		uncachedInputTokens = max(uncachedInputTokens-cacheReadTokens-cacheCreationTokens, 0)
	}
	totalInputTokens := uncachedInputTokens + cacheReadTokens + cacheCreationTokens
	if totalInputTokens <= 0 {
		return
	}

	// Split the calculation to avoid overflowing totalInputTokens*offsetBps.
	reclassifiedTokens := (totalInputTokens/10000)*int64(offsetBps) +
		(totalInputTokens%10000)*int64(offsetBps)/10000
	if reclassifiedTokens > cacheReadTokens {
		reclassifiedTokens = cacheReadTokens
	}
	if reclassifiedTokens <= 0 {
		return
	}

	summary.CacheBillingApplied = true
	summary.CacheBillingOffsetBps = offsetBps
	summary.CacheBillingTotalInputTokens = int(totalInputTokens)
	summary.CacheReclassifiedTokens = int(reclassifiedTokens)
	summary.BillingCacheTokens = summary.CacheTokens - summary.CacheReclassifiedTokens
}

func isLegacyClaudeDerivedOpenAIUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if relayInfo == nil || usage == nil {
		return false
	}
	if relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return false
	}
	if usage.UsageSource != "" || usage.UsageSemantic != "" {
		return false
	}
	return usage.ClaudeCacheCreation5mTokens > 0 || usage.ClaudeCacheCreation1hTokens > 0
}

func calculateTextToolCallSurcharge(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, summary *textQuotaSummary) decimal.Decimal {
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	var surcharge decimal.Decimal

	if relayInfo.ResponsesUsageInfo != nil {
		if webSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; exists && webSearchTool.CallCount > 0 {
			summary.WebSearchCallCount = webSearchTool.CallCount
			summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
				Mul(decimal.NewFromInt(int64(webSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	} else if strings.HasSuffix(summary.ModelName, "search-preview") {
		summary.WebSearchCallCount = 1
		summary.WebSearchPrice = operation_setting.GetToolPriceForModel("web_search_preview", summary.ModelName)
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.WebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	summary.ClaudeWebSearchCallCount = ctx.GetInt("claude_web_search_requests")
	if summary.ClaudeWebSearchCallCount > 0 {
		summary.ClaudeWebSearchPrice = operation_setting.GetToolPrice("web_search")
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ClaudeWebSearchPrice).
			Div(decimal.NewFromInt(1000)).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit).
			Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))))
	}

	if relayInfo.ResponsesUsageInfo != nil {
		if fileSearchTool, exists := relayInfo.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolFileSearch]; exists && fileSearchTool.CallCount > 0 {
			summary.FileSearchCallCount = fileSearchTool.CallCount
			summary.FileSearchPrice = operation_setting.GetToolPrice("file_search")
			surcharge = surcharge.Add(decimal.NewFromFloat(summary.FileSearchPrice).
				Mul(decimal.NewFromInt(int64(fileSearchTool.CallCount))).
				Div(decimal.NewFromInt(1000)).
				Mul(dGroupRatio).
				Mul(dQuotaPerUnit))
		}
	}

	if ctx.GetBool("image_generation_call") {
		summary.ImageGenerationCallPrice = operation_setting.GetGPTImage1PriceOnceCall(ctx.GetString("image_generation_call_quality"), ctx.GetString("image_generation_call_size"))
		surcharge = surcharge.Add(decimal.NewFromFloat(summary.ImageGenerationCallPrice).
			Mul(dGroupRatio).
			Mul(dQuotaPerUnit))
	}

	return surcharge
}

// noteQuotaClamp records the first quota saturation event onto relayInfo so it
// can later be attached to the consume/task log for admin auditing.
func noteQuotaClamp(relayInfo *relaycommon.RelayInfo, clamp *common.QuotaClamp) {
	if clamp == nil || relayInfo == nil {
		return
	}
	if relayInfo.QuotaClamp == nil {
		relayInfo.QuotaClamp = clamp
	}
}

// AddKnownToolCallSurchargeToPreConsumeQuota reserves tool charges that are
// guaranteed by the request shape before the upstream request is sent.
func AddKnownToolCallSurchargeToPreConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) int {
	if relayInfo == nil {
		return preConsumedQuota
	}
	summary := textQuotaSummary{
		ModelName:  relayInfo.OriginModelName,
		GroupRatio: effectiveChannelGroupRatio(relayInfo),
	}
	surcharge := calculateTextToolCallSurcharge(ctx, relayInfo, &summary)
	if surcharge.IsZero() {
		return preConsumedQuota
	}
	quota, clamp := common.QuotaFromDecimalChecked(
		decimal.NewFromInt(int64(preConsumedQuota)).Add(surcharge.Ceil()),
	)
	noteQuotaClamp(relayInfo, clamp)
	return quota
}

func composeTieredTextQuota(relayInfo *relaycommon.RelayInfo, summary textQuotaSummary, tieredQuota int, tieredResult *billingexpr.TieredResult) int {
	if summary.ToolCallSurchargeQuota.IsZero() {
		return tieredQuota
	}

	if tieredResult != nil {
		if relayInfo.TieredBillingSnapshot != nil {
			quota, clamp := common.QuotaFromDecimalChecked(decimal.NewFromFloat(tieredResult.ActualQuotaBeforeGroup).
				Mul(decimal.NewFromFloat(summary.GroupRatio)).
				Add(summary.ToolCallSurchargeQuota))
			noteQuotaClamp(relayInfo, clamp)
			return quota
		}
	}

	quota, clamp := common.QuotaFromDecimalChecked(
		decimal.NewFromInt(int64(tieredQuota)).Add(summary.ToolCallSurchargeQuota),
	)
	noteQuotaClamp(relayInfo, clamp)
	return quota
}

func calculateTextQuotaSummary(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) textQuotaSummary {
	elapsed := time.Since(relayInfo.StartTime)
	if elapsed < 0 {
		elapsed = 0
	}
	summary := textQuotaSummary{
		ModelName:            relayInfo.OriginModelName,
		TokenName:            ctx.GetString("token_name"),
		UseTimeSeconds:       int64(elapsed / time.Second),
		UseTimeMilliseconds:  elapsed.Milliseconds(),
		CompletionRatio:      relayInfo.PriceData.CompletionRatio,
		CacheRatio:           relayInfo.PriceData.CacheRatio,
		ImageRatio:           relayInfo.PriceData.ImageRatio,
		ModelRatio:           relayInfo.PriceData.ModelRatio,
		GroupRatio:           effectiveChannelGroupRatio(relayInfo),
		ModelPrice:           relayInfo.PriceData.ModelPrice,
		CacheCreationRatio:   relayInfo.PriceData.CacheCreationRatio,
		CacheCreationRatio5m: relayInfo.PriceData.CacheCreation5mRatio,
		CacheCreationRatio1h: relayInfo.PriceData.CacheCreation1hRatio,
		UsageSemantic:        usageSemanticFromUsage(relayInfo, usage),
	}
	summary.IsClaudeUsageSemantic = summary.UsageSemantic == "anthropic"

	if usage == nil {
		usage = &dto.Usage{
			PromptTokens:     relayInfo.GetEstimatePromptTokens(),
			CompletionTokens: 0,
			TotalTokens:      relayInfo.GetEstimatePromptTokens(),
		}
	}

	summary.RawPromptTokens = usage.PromptTokens
	summary.RawCompletionTokens = usage.CompletionTokens
	summary.RawTotalTokens = usage.PromptTokens + usage.CompletionTokens

	tokenPricingCtx := tokenPricingContextFromRelayInfo(relayInfo)
	tokenPricing := billing_setting.GetEffectiveTokenPricing(tokenPricingCtx)
	summary.TokenPricingEnabled = tokenPricing.Enabled
	summary.TokenPricingInputRatio = tokenPricing.InputRatio
	summary.TokenPricingOutputRatio = tokenPricing.OutputRatio

	summary.PromptTokens = billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokens, tokenPricingCtx)
	summary.CompletionTokens = billing_setting.ApplyOutputTokenPricingForContext(usage.CompletionTokens, tokenPricingCtx)
	summary.TotalTokens = summary.PromptTokens + summary.CompletionTokens
	summary.CacheTokens = billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.CachedTokens, tokenPricingCtx)
	summary.CacheCreationTokens = billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.CacheCreationTokensTotal(), tokenPricingCtx)
	summary.CacheCreationTokens5m = billing_setting.ApplyInputTokenPricingForContext(usage.ClaudeCacheCreation5mTokens, tokenPricingCtx)
	summary.CacheCreationTokens1h = billing_setting.ApplyInputTokenPricingForContext(usage.ClaudeCacheCreation1hTokens, tokenPricingCtx)
	summary.ImageTokens = billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.ImageTokens, tokenPricingCtx)
	summary.AudioTokens = billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.AudioTokens, tokenPricingCtx)
	legacyClaudeDerived := isLegacyClaudeDerivedOpenAIUsage(relayInfo, usage)
	isOpenRouterClaudeBilling := relayInfo.ChannelMeta != nil &&
		relayInfo.ChannelType == constant.ChannelTypeOpenRouter &&
		summary.IsClaudeUsageSemantic

	if isOpenRouterClaudeBilling {
		summary.PromptTokens -= summary.CacheTokens
		isUsingCustomSettings := relayInfo.PriceData.UsePrice || hasCustomModelRatio(summary.ModelName, relayInfo.PriceData.ModelRatio)
		if summary.CacheCreationTokens == 0 && relayInfo.PriceData.CacheCreationRatio != 1 && usage.Cost != 0 && !isUsingCustomSettings {
			maybeCacheCreationTokens := CalcOpenRouterCacheCreateTokens(*usage, relayInfo.PriceData)
			billingCacheCreationTokens := billing_setting.ApplyInputTokenPricingForContext(maybeCacheCreationTokens, tokenPricingCtx)
			if maybeCacheCreationTokens >= 0 && summary.PromptTokens >= billingCacheCreationTokens {
				summary.CacheCreationTokens = billingCacheCreationTokens
			}
		}
		summary.PromptTokens -= summary.CacheCreationTokens
	}
	applyCacheBillingOffset(&summary, relayInfo.PriceData, legacyClaudeDerived)

	dPromptTokens := decimal.NewFromInt(int64(summary.PromptTokens))
	dRawCacheTokens := decimal.NewFromInt(int64(summary.CacheTokens))
	dBillingCacheTokens := decimal.NewFromInt(int64(summary.BillingCacheTokens))
	dCacheReclassifiedTokens := decimal.NewFromInt(int64(summary.CacheReclassifiedTokens))
	dImageTokens := decimal.NewFromInt(int64(summary.ImageTokens))
	dAudioTokens := decimal.NewFromInt(int64(summary.AudioTokens))
	dCompletionTokens := decimal.NewFromInt(int64(summary.CompletionTokens))
	dCachedCreationTokens := decimal.NewFromInt(int64(summary.CacheCreationTokens))
	dCompletionRatio := decimal.NewFromFloat(summary.CompletionRatio)
	dCacheRatio := decimal.NewFromFloat(summary.CacheRatio)
	dImageRatio := decimal.NewFromFloat(summary.ImageRatio)
	dModelRatio := decimal.NewFromFloat(summary.ModelRatio)
	dGroupRatio := decimal.NewFromFloat(summary.GroupRatio)
	dModelPrice := decimal.NewFromFloat(summary.ModelPrice)
	dCacheCreationRatio := decimal.NewFromFloat(summary.CacheCreationRatio)
	dCacheCreationRatio5m := decimal.NewFromFloat(summary.CacheCreationRatio5m)
	dCacheCreationRatio1h := decimal.NewFromFloat(summary.CacheCreationRatio1h)
	dQuotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)

	ratio := dModelRatio.Mul(dGroupRatio)
	summary.ToolCallSurchargeQuota = calculateTextToolCallSurcharge(ctx, relayInfo, &summary)

	var audioInputQuota decimal.Decimal
	if !relayInfo.PriceData.UsePrice {
		baseTokens := dPromptTokens

		var cachedTokensWithRatio decimal.Decimal
		if !dRawCacheTokens.IsZero() {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dRawCacheTokens)
			}
			cachedTokensWithRatio = dBillingCacheTokens.Mul(dCacheRatio)
		}

		var cachedCreationTokensWithRatio decimal.Decimal
		hasSplitCacheCreationTokens := summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0
		if !dCachedCreationTokens.IsZero() || hasSplitCacheCreationTokens {
			if !summary.IsClaudeUsageSemantic && !legacyClaudeDerived {
				baseTokens = baseTokens.Sub(dCachedCreationTokens)
				cachedCreationTokensWithRatio = dCachedCreationTokens.Mul(dCacheCreationRatio)
			} else {
				remaining := summary.CacheCreationTokens - summary.CacheCreationTokens5m - summary.CacheCreationTokens1h
				if remaining < 0 {
					remaining = 0
				}
				cachedCreationTokensWithRatio = decimal.NewFromInt(int64(remaining)).Mul(dCacheCreationRatio)
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens5m)).Mul(dCacheCreationRatio5m))
				cachedCreationTokensWithRatio = cachedCreationTokensWithRatio.Add(decimal.NewFromInt(int64(summary.CacheCreationTokens1h)).Mul(dCacheCreationRatio1h))
			}
		}

		var imageTokensWithRatio decimal.Decimal
		if !dImageTokens.IsZero() {
			baseTokens = baseTokens.Sub(dImageTokens)
			imageTokensWithRatio = dImageTokens.Mul(dImageRatio)
		}

		if !dAudioTokens.IsZero() {
			summary.AudioInputPrice = operation_setting.GetGeminiInputAudioPricePerMillionTokens(summary.ModelName)
			if summary.AudioInputPrice > 0 {
				baseTokens = baseTokens.Sub(dAudioTokens)
				audioInputQuota = decimal.NewFromFloat(summary.AudioInputPrice).
					Div(decimal.NewFromInt(1000000)).Mul(dAudioTokens).Mul(dGroupRatio).Mul(dQuotaPerUnit)
			}
		}

		// Native OpenAI cache read/write counts are unadjusted prefixes and may
		// overlap, so their sum can exceed prompt_tokens.
		if baseTokens.IsNegative() {
			baseTokens = decimal.Zero
		}

		promptQuota := baseTokens.Add(dCacheReclassifiedTokens).Add(cachedTokensWithRatio).Add(imageTokensWithRatio).Add(cachedCreationTokensWithRatio)
		completionQuota := dCompletionTokens.Mul(dCompletionRatio)
		if ctx.GetBool(string(constant.ContextKeyUpstreamOpenAILongContextBilling)) &&
			summary.RawPromptTokens > openAIAccountLongContextInputThreshold {
			promptQuota = promptQuota.Mul(decimal.NewFromFloat(openAIAccountLongContextInputMultiplier))
			completionQuota = completionQuota.Mul(decimal.NewFromFloat(openAIAccountLongContextOutputMultiplier))
			summary.LongContextBillingApplied = true
		}
		quotaCalculateDecimal := promptQuota.Add(completionQuota).Mul(ratio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		quotaCalculateDecimal = relayInfo.PriceData.ApplyOtherRatiosToDecimal(quotaCalculateDecimal)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)

		if !ratio.IsZero() && quotaCalculateDecimal.LessThanOrEqual(decimal.Zero) {
			quotaCalculateDecimal = decimal.NewFromInt(1)
		}
		quota, clamp := common.QuotaFromDecimalChecked(quotaCalculateDecimal)
		summary.Quota = quota
		noteQuotaClamp(relayInfo, clamp)
	} else {
		quotaCalculateDecimal := dModelPrice.Mul(dQuotaPerUnit).Mul(dGroupRatio)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(audioInputQuota)
		quotaCalculateDecimal = relayInfo.PriceData.ApplyOtherRatiosToDecimal(quotaCalculateDecimal)
		quotaCalculateDecimal = quotaCalculateDecimal.Add(summary.ToolCallSurchargeQuota)
		quota, clamp := common.QuotaFromDecimalChecked(quotaCalculateDecimal)
		summary.Quota = quota
		noteQuotaClamp(relayInfo, clamp)
	}

	if !summary.hasBillableUsage() {
		summary.Quota = 0
	} else if !ratio.IsZero() && summary.Quota == 0 {
		summary.Quota = 1
	}

	return summary
}

func usageSemanticFromUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

func PostTextConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) {
	originUsage := usage
	if usage == nil {
		extraContent = append(extraContent, "上游无计费信息")
	}
	if originUsage != nil {
		ObserveChannelAffinityUsageCacheByRelayFormat(ctx, usage, relayInfo.GetFinalRequestRelayFormat())
	}
	// Raise obvious Codex under-counts on the settlement object. Response paths
	// independently project an equivalent reconciled copy for the client.
	ReconcileCodexResponsesUsage(ctx, relayInfo, usage)

	adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	summary := calculateTextQuotaSummary(ctx, relayInfo, usage)

	var tieredResult *billingexpr.TieredResult
	tieredBillingApplied := false
	if originUsage != nil {
		var tieredUsedVars map[string]bool
		if snap := relayInfo.TieredBillingSnapshot; snap != nil {
			tieredUsedVars = billingexpr.UsedVars(snap.ExprString)
		}
		tieredOk, tieredQuota, tieredRes := TryTieredSettle(relayInfo, BuildTieredTokenParamsForContext(usage, summary.IsClaudeUsageSemantic, tieredUsedVars, tokenPricingContextFromRelayInfo(relayInfo)))
		if tieredOk {
			tieredBillingApplied = true
			tieredResult = tieredRes
			summary.Quota = composeTieredTextQuota(relayInfo, summary, tieredQuota, tieredRes)
		}
	}
	// Apply the minimum only when the upstream supplied actual tokens. Keep
	// the existing no-usage path unchanged so pre-consume can be refunded.
	if originUsage != nil {
		billable := summary.GroupRatio > 0 &&
			(summary.ModelRatio > 0 || summary.ModelPrice > 0 || tieredBillingApplied)
		summary.Quota = applyMinimumTokenConsumeQuota(summary.Quota, summary.RawTotalTokens, billable)
	}

	if summary.WebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Web Search 调用 %d 次，调用花费 %s", summary.WebSearchCallCount, decimal.NewFromFloat(summary.WebSearchPrice).Mul(decimal.NewFromInt(int64(summary.WebSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ClaudeWebSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Claude Web Search 调用 %d 次，调用花费 %s", summary.ClaudeWebSearchCallCount, decimal.NewFromFloat(summary.ClaudeWebSearchPrice).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).Mul(decimal.NewFromInt(int64(summary.ClaudeWebSearchCallCount))).String()))
	}
	if summary.FileSearchCallCount > 0 {
		extraContent = append(extraContent, fmt.Sprintf("File Search 调用 %d 次，调用花费 %s", summary.FileSearchCallCount, decimal.NewFromFloat(summary.FileSearchPrice).Mul(decimal.NewFromInt(int64(summary.FileSearchCallCount))).Div(decimal.NewFromInt(1000)).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Audio Input 花费 %s", decimal.NewFromFloat(summary.AudioInputPrice).Div(decimal.NewFromInt(1000000)).Mul(decimal.NewFromInt(int64(summary.AudioTokens))).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}
	if summary.ImageGenerationCallPrice > 0 {
		extraContent = append(extraContent, fmt.Sprintf("Image Generation Call 花费 %s", decimal.NewFromFloat(summary.ImageGenerationCallPrice).Mul(decimal.NewFromFloat(summary.GroupRatio)).Mul(decimal.NewFromFloat(common.QuotaPerUnit)).String()))
	}

	if !summary.hasBillableUsage() {
		extraContent = append(extraContent, "上游没有返回计费信息，无法扣费（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, summary.ModelName, relayInfo.FinalPreConsumedQuota))
	} else {
		model.UpdateUserUsedQuotaAndRequestCount(relayInfo.UserId, summary.Quota)
		model.UpdateChannelUsedQuota(relayInfo.ChannelId, summary.Quota)
	}

	if err := SettleBilling(ctx, relayInfo, summary.Quota); err != nil {
		logger.LogError(ctx, "error settling billing: "+err.Error())
	}

	logModel := summary.ModelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}

	logContent := strings.Join(extraContent, ", ")
	var other map[string]interface{}
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo,
			summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio,
			summary.CacheTokens, summary.CacheRatio,
			summary.CacheCreationTokens, summary.CacheCreationRatio,
			summary.CacheCreationTokens5m, summary.CacheCreationRatio5m,
			summary.CacheCreationTokens1h, summary.CacheCreationRatio1h,
			summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
		other["usage_semantic"] = "anthropic"
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.ModelRatio, summary.GroupRatio, summary.CompletionRatio, summary.CacheTokens, summary.CacheRatio, summary.ModelPrice, relayInfo.PriceData.GroupRatioInfo.GroupSpecialRatio)
	}
	if adminRejectReason != "" {
		other["reject_reason"] = adminRejectReason
	}
	if summary.ImageTokens != 0 {
		other["image"] = true
		other["image_ratio"] = summary.ImageRatio
		other["image_output"] = summary.ImageTokens
	}
	if summary.WebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.WebSearchCallCount
		other["web_search_price"] = summary.WebSearchPrice
	} else if summary.ClaudeWebSearchCallCount > 0 {
		other["web_search"] = true
		other["web_search_call_count"] = summary.ClaudeWebSearchCallCount
		other["web_search_price"] = summary.ClaudeWebSearchPrice
	}
	if summary.FileSearchCallCount > 0 {
		other["file_search"] = true
		other["file_search_call_count"] = summary.FileSearchCallCount
		other["file_search_price"] = summary.FileSearchPrice
	}
	if summary.AudioInputPrice > 0 && summary.AudioTokens > 0 {
		other["audio_input_seperate_price"] = true
		other["audio_input_token_count"] = summary.AudioTokens
		other["audio_input_price"] = summary.AudioInputPrice
	}
	if summary.ImageGenerationCallPrice > 0 {
		other["image_generation_call"] = true
		other["image_generation_call_price"] = summary.ImageGenerationCallPrice
	}
	if summary.CacheCreationTokens > 0 {
		other["cache_creation_tokens"] = summary.CacheCreationTokens
		other["cache_creation_ratio"] = summary.CacheCreationRatio
	}
	if summary.CacheCreationTokens5m > 0 {
		other["cache_creation_tokens_5m"] = summary.CacheCreationTokens5m
		other["cache_creation_ratio_5m"] = summary.CacheCreationRatio5m
	}
	if summary.CacheCreationTokens1h > 0 {
		other["cache_creation_tokens_1h"] = summary.CacheCreationTokens1h
		other["cache_creation_ratio_1h"] = summary.CacheCreationRatio1h
	}
	if summary.TokenPricingEnabled {
		other["token_pricing_enabled"] = true
		other["token_pricing_input_ratio"] = summary.TokenPricingInputRatio
		other["token_pricing_output_ratio"] = summary.TokenPricingOutputRatio
		if ruleNames := billing_setting.GetEffectiveTokenPricing(tokenPricingContextFromRelayInfo(relayInfo)).RuleNames; len(ruleNames) > 0 {
			other["token_pricing_rules"] = ruleNames
		}
		other["raw_prompt_tokens"] = summary.RawPromptTokens
		other["raw_completion_tokens"] = summary.RawCompletionTokens
		other["raw_total_tokens"] = summary.RawTotalTokens
		other["billing_prompt_tokens"] = summary.PromptTokens
		other["billing_completion_tokens"] = summary.CompletionTokens
		other["billing_total_tokens"] = summary.TotalTokens
	}
	cacheWriteTokens := cacheWriteTokensTotal(summary)
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other["cache_write_tokens"] = cacheWriteTokens
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && usage != nil && usage.UsageSource != "" && usage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		// Only write this field when upstream/current conversion has already provided a
		// reliable total input value and tagged the usage source. Do not infer it from
		// prompt/cache fields here, otherwise old upstream payloads may be double-counted.
		other["input_tokens_total"] = usage.InputTokens
	}
	other["cache_observation"] = textCacheObservation(originUsage, summary, relayInfo)
	if summary.CacheBillingApplied {
		adminInfo, ok := other["admin_info"].(map[string]interface{})
		if !ok {
			adminInfo = make(map[string]interface{})
			other["admin_info"] = adminInfo
		}
		adminInfo["cache_billing"] = map[string]interface{}{
			"offset_bps":                summary.CacheBillingOffsetBps,
			"total_input_tokens":        summary.CacheBillingTotalInputTokens,
			"raw_cache_read_tokens":     summary.CacheTokens,
			"billing_cache_read_tokens": summary.BillingCacheTokens,
			"reclassified_tokens":       summary.CacheReclassifiedTokens,
		}
	}
	if tieredBillingApplied {
		InjectTieredBillingInfo(other, relayInfo, tieredResult)
	}
	attachQuotaSaturation(ctx, relayInfo, other)

	model.RecordConsumeLog(ctx, relayInfo.UserId, model.RecordConsumeLogParams{
		ChannelId:           relayInfo.ChannelId,
		PromptTokens:        summary.PromptTokens,
		CompletionTokens:    summary.CompletionTokens,
		ModelName:           logModel,
		TokenName:           summary.TokenName,
		Quota:               summary.Quota,
		Content:             logContent,
		TokenId:             relayInfo.TokenId,
		UseTimeSeconds:      int(summary.UseTimeSeconds),
		UseTimeMilliseconds: common.GetPointer(summary.UseTimeMilliseconds),
		IsStream:            relayInfo.IsStream,
		Group:               relayInfo.UsingGroup,
		Other:               other,
	})
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(summary.CompletionTokens))
	})
}

func textCacheObservation(originUsage *dto.Usage, summary textQuotaSummary, relayInfo *relaycommon.RelayInfo) string {
	if originUsage == nil || summary.RawPromptTokens <= 0 {
		return "unknown"
	}
	if relayInfo != nil && relayInfo.IsStream && relayInfo.StreamStatus != nil && !relayInfo.StreamStatus.IsNormalEnd() {
		return "unknown"
	}
	if summary.CacheTokens > 0 {
		return "hit"
	}
	return "miss"
}
