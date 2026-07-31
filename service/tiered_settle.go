package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
)

// TieredResultWrapper wraps billingexpr.TieredResult for use at the service layer.
type TieredResultWrapper = billingexpr.TieredResult

// BuildTieredTokenParams constructs billingexpr.TokenParams from a dto.Usage,
// normalizing P and C so they mean "tokens not separately priced by the
// expression". Sub-categories (cache, image, audio) are only subtracted
// when the expression references them via their own variable.
//
// GPT-format APIs report prompt_tokens / completion_tokens as totals that
// include all sub-categories (cache, image, audio). Claude-format APIs
// report them as text-only. This function normalizes to text-only when
// sub-categories are separately priced.
func BuildTieredTokenParams(usage *dto.Usage, isClaudeUsageSemantic bool, usedVars map[string]bool) billingexpr.TokenParams {
	return BuildTieredTokenParamsForContext(usage, isClaudeUsageSemantic, usedVars, billing_setting.TokenPricingContext{})
}

func BuildTieredTokenParamsForContext(usage *dto.Usage, isClaudeUsageSemantic bool, usedVars map[string]bool, tokenPricingCtx billing_setting.TokenPricingContext) billingexpr.TokenParams {
	p := float64(billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokens, tokenPricingCtx))
	c := float64(billing_setting.ApplyOutputTokenPricingForContext(usage.CompletionTokens, tokenPricingCtx))
	cr := float64(billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.CachedTokens, tokenPricingCtx))
	cc5m := float64(billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.CacheCreationTokensTotal(), tokenPricingCtx))
	cc1h := float64(0)

	if usage.UsageSemantic == "anthropic" {
		cc1h = float64(billing_setting.ApplyInputTokenPricingForContext(usage.ClaudeCacheCreation1hTokens, tokenPricingCtx))
		cc5m = float64(billing_setting.ApplyInputTokenPricingForContext(usage.ClaudeCacheCreation5mTokens, tokenPricingCtx))
	}

	img := float64(billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.ImageTokens, tokenPricingCtx))
	ai := float64(billing_setting.ApplyInputTokenPricingForContext(usage.PromptTokensDetails.AudioTokens, tokenPricingCtx))
	imgO := float64(billing_setting.ApplyOutputTokenPricingForContext(usage.CompletionTokenDetails.ImageTokens, tokenPricingCtx))
	ao := float64(billing_setting.ApplyOutputTokenPricingForContext(usage.CompletionTokenDetails.AudioTokens, tokenPricingCtx))

	// len = total input context length for tier condition evaluation.
	// Non-Claude: prompt_tokens already includes everything.
	// Claude: input_tokens is text-only, so add cache read + cache creation.
	inputLen := float64(usage.PromptTokens)
	if isClaudeUsageSemantic {
		inputLen = float64(usage.PromptTokens + usage.PromptTokensDetails.CachedTokens + usage.ClaudeCacheCreation5mTokens + usage.ClaudeCacheCreation1hTokens)
	}

	if !isClaudeUsageSemantic {
		if usedVars["cr"] {
			p -= cr
		}
		if usedVars["cc"] {
			p -= cc5m
		}
		if usedVars["cc1h"] {
			p -= cc1h
		}
		if usedVars["img"] {
			p -= img
		}
		if usedVars["ai"] {
			p -= ai
		}
		if usedVars["img_o"] {
			c -= imgO
		}
		if usedVars["ao"] {
			c -= ao
		}
	}

	if p < 0 {
		p = 0
	}
	if c < 0 {
		c = 0
	}

	return billingexpr.TokenParams{
		P:    p,
		C:    c,
		Len:  inputLen,
		CR:   cr,
		CC:   cc5m,
		CC1h: cc1h,
		Img:  img,
		ImgO: imgO,
		AI:   ai,
		AO:   ao,
	}
}

// TryTieredSettle checks if the request uses tiered_expr billing and, if so,
// computes the actual quota using the frozen BillingSnapshot. Returns:
//   - ok=true, quota, result  when tiered billing applies
//   - ok=false, 0, nil        when it doesn't (caller should fall through to existing logic)
func TryTieredSettle(relayInfo *relaycommon.RelayInfo, params billingexpr.TokenParams, accountMultiplier ...float64) (ok bool, quota int, result *billingexpr.TieredResult) {
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return false, 0, nil
	}
	multiplier := relayInfo.GetAccountRateMultiplier()
	if len(accountMultiplier) > 0 {
		multiplier = accountMultiplier[0]
	}
	if multiplier < 0 {
		multiplier = 1
	}
	if multiplier != 1 {
		// Keep the pre-consume snapshot immutable, but settle against the
		// multiplier of the account that actually produced the response. This
		// matters when an upstream account failover changes the multiplier.
		scaled := *snap
		scaled.GroupRatio *= multiplier
		snap = &scaled
	}

	requestInput := billingexpr.RequestInput{}
	if relayInfo.BillingRequestInput != nil {
		requestInput = *relayInfo.BillingRequestInput
	}

	tr, err := billingexpr.ComputeTieredQuotaWithRequest(snap, params, requestInput)
	if err != nil {
		if multiplier != 1 {
			quota = common.QuotaRound(float64(relayInfo.TieredBillingSnapshot.EstimatedQuotaAfterGroup) * multiplier)
		} else {
			quota = relayInfo.FinalPreConsumedQuota
		}
		if quota <= 0 && multiplier == 1 {
			quota = snap.EstimatedQuotaAfterGroup
		}
		return true, quota, nil
	}

	noteQuotaClamp(relayInfo, tr.Clamp)

	return true, tr.ActualQuotaAfterGroup, &tr
}
