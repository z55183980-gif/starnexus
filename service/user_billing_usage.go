package service

import (
	"math"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

// ProjectUserBillingUsage returns the settled, user-visible token buckets for
// an upstream usage object. It deliberately carries no pricing ratios, cache
// offsets, raw counts, or other policy metadata. Callers must keep the original
// usage object for settlement and administrator auditing.
func ProjectUserBillingUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) *dto.Usage {
	if usage == nil {
		return nil
	}

	projected := *usage
	// Upstream cost is supplier/accounting data. Public responses expose only
	// the projected token buckets; the settlement path keeps the raw object.
	projected.Cost = nil
	if usage.InputTokensDetails != nil {
		inputDetails := *usage.InputTokensDetails
		projected.InputTokensDetails = &inputDetails
	}
	if relayInfo == nil {
		clampUserVisibleUsage(&projected)
		projected.UsageSemantic = ""
		projected.UsageSource = ""
		return &projected
	}

	rawPromptTokens := usage.PromptTokens
	if rawPromptTokens == 0 && usage.InputTokens > 0 {
		rawPromptTokens = usage.InputTokens
	}
	rawCompletionTokens := usage.CompletionTokens
	if rawCompletionTokens == 0 && usage.OutputTokens > 0 {
		rawCompletionTokens = usage.OutputTokens
	}

	tokenPricingCtx := tokenPricingContextFromRelayInfo(relayInfo)
	projected.PromptTokens = billing_setting.ApplyInputTokenPricingForContext(rawPromptTokens, tokenPricingCtx)
	projected.CompletionTokens = billing_setting.ApplyOutputTokenPricingForContext(rawCompletionTokens, tokenPricingCtx)

	inputDetails := usage.PromptTokensDetails
	if usage.InputTokensDetails != nil {
		inputDetails = mergeInputTokenDetails(inputDetails, *usage.InputTokensDetails)
	}
	projected.PromptTokensDetails = projectInputTokenDetails(inputDetails, tokenPricingCtx)
	projected.CompletionTokenDetails = projectOutputTokenDetails(usage.CompletionTokenDetails, tokenPricingCtx)

	projected.ClaudeCacheCreation5mTokens = billing_setting.ApplyInputTokenPricingForContext(usage.ClaudeCacheCreation5mTokens, tokenPricingCtx)
	projected.ClaudeCacheCreation1hTokens = billing_setting.ApplyInputTokenPricingForContext(usage.ClaudeCacheCreation1hTokens, tokenPricingCtx)
	projected.PromptTokensDetails.CachedCreationTokens = billingMaxInt(
		projected.PromptTokensDetails.CachedCreationTokens,
		projected.ClaudeCacheCreation5mTokens+projected.ClaudeCacheCreation1hTokens,
	)

	cacheTokens := projected.PromptTokensDetails.CachedTokens
	cacheCreationTokens := projected.PromptTokensDetails.CacheCreationTokensTotal()
	isClaudeUsageSemantic := usageSemanticFromUsage(relayInfo, usage) == "anthropic"
	if reclassified := projectedCacheReclassifiedTokens(relayInfo, projected.PromptTokens, cacheTokens, cacheCreationTokens, isClaudeUsageSemantic); reclassified > 0 {
		projected.PromptTokensDetails.CachedTokens -= reclassified
		if isClaudeUsageSemantic {
			projected.PromptTokens += reclassified
		}
	}

	projected.PromptCacheHitTokens = projected.PromptTokensDetails.CachedTokens
	projected.InputTokens = projected.PromptTokens
	projected.OutputTokens = projected.CompletionTokens
	projected.TotalTokens = projected.PromptTokens + projected.CompletionTokens
	clampUserVisibleUsage(&projected)
	projected.UsageSemantic = ""
	projected.UsageSource = ""

	inputDetailsCopy := projected.PromptTokensDetails
	projected.InputTokensDetails = &inputDetailsCopy
	return &projected
}

// ProjectClaudeUsageForUser applies the same user-visible projection while
// preserving Anthropic's mutually exclusive input/cache token buckets.
func ProjectClaudeUsageForUser(relayInfo *relaycommon.RelayInfo, usage *dto.ClaudeUsage) *dto.ClaudeUsage {
	if usage == nil {
		return nil
	}

	raw := &dto.Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens,
		UsageSemantic:    "anthropic",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens:         usage.CacheReadInputTokens,
			CachedCreationTokens: usage.CacheCreationInputTokens,
		},
		ClaudeCacheCreation5mTokens: usage.GetCacheCreation5mTokens(),
		ClaudeCacheCreation1hTokens: usage.GetCacheCreation1hTokens(),
	}
	projected := ProjectUserBillingUsage(relayInfo, raw)
	if projected == nil {
		return nil
	}

	result := *usage
	result.InputTokens = projected.PromptTokens
	result.OutputTokens = projected.CompletionTokens
	result.CacheReadInputTokens = projected.PromptTokensDetails.CachedTokens
	result.CacheCreationInputTokens = projected.PromptTokensDetails.CacheCreationTokensTotal()
	result.InputTokens = billingMaxInt(result.InputTokens, 0)
	result.OutputTokens = billingMaxInt(result.OutputTokens, 0)
	result.CacheReadInputTokens = billingMaxInt(result.CacheReadInputTokens, 0)
	result.CacheCreationInputTokens = billingMaxInt(result.CacheCreationInputTokens, 0)

	cacheCreation1h := billingMinInt(projected.ClaudeCacheCreation1hTokens, result.CacheCreationInputTokens)
	cacheCreation5m := result.CacheCreationInputTokens - cacheCreation1h
	if usage.CacheCreation != nil || cacheCreation5m > 0 || cacheCreation1h > 0 {
		result.CacheCreation = &dto.ClaudeCacheCreationUsage{
			Ephemeral5mInputTokens: cacheCreation5m,
			Ephemeral1hInputTokens: cacheCreation1h,
		}
	} else {
		result.CacheCreation = nil
	}
	return &result
}

// ProjectGeminiUsageForUser projects Gemini usage metadata without exposing
// any internal rule fields. Gemini promptTokenCount is cache-inclusive, so a
// cache reclassification changes only cachedContentTokenCount, not prompt total.
func ProjectGeminiUsageForUser(relayInfo *relaycommon.RelayInfo, metadata dto.GeminiUsageMetadata, fallbackPromptTokens int) dto.GeminiUsageMetadata {
	rawPromptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if rawPromptTokens <= 0 && fallbackPromptTokens > 0 {
		rawPromptTokens = fallbackPromptTokens
	}
	rawCompletionTokens := metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount
	if rawCompletionTokens <= 0 && metadata.TotalTokenCount > rawPromptTokens {
		rawCompletionTokens = metadata.TotalTokenCount - rawPromptTokens
	}
	raw := &dto.Usage{
		PromptTokens:     rawPromptTokens,
		CompletionTokens: rawCompletionTokens,
		TotalTokens:      rawPromptTokens + rawCompletionTokens,
		UsageSemantic:    "openai",
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: metadata.CachedContentTokenCount,
		},
	}
	projected := ProjectUserBillingUsage(relayInfo, raw)
	if projected == nil {
		return metadata
	}

	tokenPricingCtx := tokenPricingContextFromRelayInfo(relayInfo)
	result := metadata
	projectedToolTokens := billing_setting.ApplyInputTokenPricingForContext(metadata.ToolUsePromptTokenCount, tokenPricingCtx)
	projectedToolTokens = billingMinInt(projectedToolTokens, projected.PromptTokens)
	result.ToolUsePromptTokenCount = projectedToolTokens
	result.PromptTokenCount = projected.PromptTokens - projectedToolTokens

	projectedThoughtTokens := billing_setting.ApplyOutputTokenPricingForContext(metadata.ThoughtsTokenCount, tokenPricingCtx)
	projectedThoughtTokens = billingMinInt(projectedThoughtTokens, projected.CompletionTokens)
	result.ThoughtsTokenCount = projectedThoughtTokens
	result.CandidatesTokenCount = projected.CompletionTokens - projectedThoughtTokens
	result.CachedContentTokenCount = projected.PromptTokensDetails.CachedTokens
	result.TotalTokenCount = projected.TotalTokens
	result.PromptTokensDetails = projectGeminiTokenDetails(metadata.PromptTokensDetails, tokenPricingCtx, true)
	result.ToolUsePromptTokensDetails = projectGeminiTokenDetails(metadata.ToolUsePromptTokensDetails, tokenPricingCtx, true)
	result.CandidatesTokensDetails = projectGeminiTokenDetails(metadata.CandidatesTokensDetails, tokenPricingCtx, false)
	clampGeminiUsageMetadata(&result)
	return result
}

func clampUserVisibleUsage(usage *dto.Usage) {
	if usage == nil {
		return
	}
	usage.PromptTokens = billingMaxInt(usage.PromptTokens, 0)
	usage.CompletionTokens = billingMaxInt(usage.CompletionTokens, 0)
	usage.TotalTokens = billingMaxInt(usage.TotalTokens, 0)
	usage.PromptCacheHitTokens = billingMaxInt(usage.PromptCacheHitTokens, 0)
	usage.InputTokens = billingMaxInt(usage.InputTokens, 0)
	usage.OutputTokens = billingMaxInt(usage.OutputTokens, 0)
	usage.PromptTokensDetails = clampInputTokenDetails(usage.PromptTokensDetails)
	usage.CompletionTokenDetails = clampOutputTokenDetails(usage.CompletionTokenDetails)
	usage.ClaudeCacheCreation5mTokens = billingMaxInt(usage.ClaudeCacheCreation5mTokens, 0)
	usage.ClaudeCacheCreation1hTokens = billingMaxInt(usage.ClaudeCacheCreation1hTokens, 0)
	if usage.InputTokensDetails != nil {
		inputDetails := clampInputTokenDetails(*usage.InputTokensDetails)
		usage.InputTokensDetails = &inputDetails
	}
}

func clampInputTokenDetails(details dto.InputTokenDetails) dto.InputTokenDetails {
	details.CachedTokens = billingMaxInt(details.CachedTokens, 0)
	details.CachedCreationTokens = billingMaxInt(details.CachedCreationTokens, 0)
	details.CacheWriteTokens = billingMaxInt(details.CacheWriteTokens, 0)
	details.TextTokens = billingMaxInt(details.TextTokens, 0)
	details.AudioTokens = billingMaxInt(details.AudioTokens, 0)
	details.ImageTokens = billingMaxInt(details.ImageTokens, 0)
	return details
}

func clampOutputTokenDetails(details dto.OutputTokenDetails) dto.OutputTokenDetails {
	details.TextTokens = billingMaxInt(details.TextTokens, 0)
	details.AudioTokens = billingMaxInt(details.AudioTokens, 0)
	details.ImageTokens = billingMaxInt(details.ImageTokens, 0)
	details.ReasoningTokens = billingMaxInt(details.ReasoningTokens, 0)
	return details
}

func clampGeminiUsageMetadata(metadata *dto.GeminiUsageMetadata) {
	if metadata == nil {
		return
	}
	metadata.PromptTokenCount = billingMaxInt(metadata.PromptTokenCount, 0)
	metadata.ToolUsePromptTokenCount = billingMaxInt(metadata.ToolUsePromptTokenCount, 0)
	metadata.CandidatesTokenCount = billingMaxInt(metadata.CandidatesTokenCount, 0)
	metadata.TotalTokenCount = billingMaxInt(metadata.TotalTokenCount, 0)
	metadata.ThoughtsTokenCount = billingMaxInt(metadata.ThoughtsTokenCount, 0)
	metadata.CachedContentTokenCount = billingMaxInt(metadata.CachedContentTokenCount, 0)
	for i := range metadata.PromptTokensDetails {
		metadata.PromptTokensDetails[i].TokenCount = billingMaxInt(metadata.PromptTokensDetails[i].TokenCount, 0)
	}
	for i := range metadata.ToolUsePromptTokensDetails {
		metadata.ToolUsePromptTokensDetails[i].TokenCount = billingMaxInt(metadata.ToolUsePromptTokensDetails[i].TokenCount, 0)
	}
	for i := range metadata.CandidatesTokensDetails {
		metadata.CandidatesTokensDetails[i].TokenCount = billingMaxInt(metadata.CandidatesTokensDetails[i].TokenCount, 0)
	}
}

func projectInputTokenDetails(details dto.InputTokenDetails, ctx billing_setting.TokenPricingContext) dto.InputTokenDetails {
	return dto.InputTokenDetails{
		CachedTokens:         billing_setting.ApplyInputTokenPricingForContext(details.CachedTokens, ctx),
		CachedCreationTokens: billing_setting.ApplyInputTokenPricingForContext(details.CachedCreationTokens, ctx),
		CacheWriteTokens:     billing_setting.ApplyInputTokenPricingForContext(details.CacheWriteTokens, ctx),
		TextTokens:           billing_setting.ApplyInputTokenPricingForContext(details.TextTokens, ctx),
		AudioTokens:          billing_setting.ApplyInputTokenPricingForContext(details.AudioTokens, ctx),
		ImageTokens:          billing_setting.ApplyInputTokenPricingForContext(details.ImageTokens, ctx),
	}
}

func projectOutputTokenDetails(details dto.OutputTokenDetails, ctx billing_setting.TokenPricingContext) dto.OutputTokenDetails {
	return dto.OutputTokenDetails{
		TextTokens:      billing_setting.ApplyOutputTokenPricingForContext(details.TextTokens, ctx),
		AudioTokens:     billing_setting.ApplyOutputTokenPricingForContext(details.AudioTokens, ctx),
		ImageTokens:     billing_setting.ApplyOutputTokenPricingForContext(details.ImageTokens, ctx),
		ReasoningTokens: billing_setting.ApplyOutputTokenPricingForContext(details.ReasoningTokens, ctx),
	}
}

func projectGeminiTokenDetails(details []dto.GeminiPromptTokensDetails, ctx billing_setting.TokenPricingContext, input bool) []dto.GeminiPromptTokensDetails {
	if details == nil {
		return nil
	}
	projected := make([]dto.GeminiPromptTokensDetails, len(details))
	for i, detail := range details {
		projected[i] = detail
		if input {
			projected[i].TokenCount = billing_setting.ApplyInputTokenPricingForContext(detail.TokenCount, ctx)
		} else {
			projected[i].TokenCount = billing_setting.ApplyOutputTokenPricingForContext(detail.TokenCount, ctx)
		}
	}
	return projected
}

func mergeInputTokenDetails(primary dto.InputTokenDetails, alternate dto.InputTokenDetails) dto.InputTokenDetails {
	primary.CachedTokens = billingMaxInt(primary.CachedTokens, alternate.CachedTokens)
	primary.CachedCreationTokens = billingMaxInt(primary.CachedCreationTokens, alternate.CachedCreationTokens)
	primary.CacheWriteTokens = billingMaxInt(primary.CacheWriteTokens, alternate.CacheWriteTokens)
	primary.TextTokens = billingMaxInt(primary.TextTokens, alternate.TextTokens)
	primary.AudioTokens = billingMaxInt(primary.AudioTokens, alternate.AudioTokens)
	primary.ImageTokens = billingMaxInt(primary.ImageTokens, alternate.ImageTokens)
	return primary
}

func projectedCacheReclassifiedTokens(relayInfo *relaycommon.RelayInfo, promptTokens, cacheTokens, cacheCreationTokens int, isClaudeUsageSemantic bool) int {
	if relayInfo == nil || cacheTokens <= 0 {
		return 0
	}

	offsetBps := 0
	if snap := relayInfo.TieredBillingSnapshot; snap != nil && snap.BillingMode == billing_setting.BillingModeTieredExpr {
		if !billingexpr.UsedVars(snap.ExprString)["cr"] {
			return 0
		}
		offsetBps = snap.CacheBillingOffsetBps
	} else if !relayInfo.PriceData.UsePrice {
		offsetBps = relayInfo.PriceData.CacheBillingOffsetBps
	}
	if offsetBps <= 0 || offsetBps > 10000 {
		return 0
	}

	prompt := int64(billingMaxInt(promptTokens, 0))
	cache := int64(billingMaxInt(cacheTokens, 0))
	cacheCreation := int64(billingMaxInt(cacheCreationTokens, 0))
	uncachedInput := prompt
	if !isClaudeUsageSemantic {
		uncachedInput = billingMaxInt64(prompt-cache-cacheCreation, 0)
	}
	totalInput := saturatingAddInt64(saturatingAddInt64(uncachedInput, cache), cacheCreation)
	if totalInput <= 0 {
		return 0
	}

	offset := int64(offsetBps)
	reclassified := (totalInput/10000)*offset + (totalInput%10000)*offset/10000
	if reclassified > cache {
		reclassified = cache
	}
	if reclassified <= 0 {
		return 0
	}
	if reclassified > int64(math.MaxInt) {
		return math.MaxInt
	}
	return int(reclassified)
}

func saturatingAddInt64(a, b int64) int64 {
	if b > 0 && a > math.MaxInt64-b {
		return math.MaxInt64
	}
	return a + b
}

func billingMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func billingMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func billingMaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
