package billingexpr

import (
	"math"

	"github.com/QuantumNous/new-api/common"
)

// quotaConversion converts raw expression output to quota based on the
// expression version. This is the central dispatch point for future versions
// that may use a different conversion formula.
func quotaConversion(exprOutput float64, snap *BillingSnapshot) float64 {
	switch snap.ExprVersion {
	default: // v1: coefficients are $/1M tokens prices
		return exprOutput / 1_000_000 * snap.QuotaPerUnit
	}
}

// ComputeTieredQuota runs the Expr from a frozen BillingSnapshot against
// actual token counts and returns the settlement result.
func ComputeTieredQuota(snap *BillingSnapshot, params TokenParams) (TieredResult, error) {
	return ComputeTieredQuotaWithRequest(snap, params, RequestInput{})
}

func ComputeTieredQuotaWithRequest(snap *BillingSnapshot, params TokenParams, request RequestInput) (TieredResult, error) {
	params, cacheBilling := applyTieredCacheBillingOffset(snap, params)
	cost, trace, err := RunExprByHashWithRequest(snap.ExprString, snap.ExprHash, params, request)
	if err != nil {
		return TieredResult{}, err
	}

	quotaBeforeGroup := quotaConversion(cost, snap)
	afterGroup, clamp := common.QuotaRoundChecked(quotaBeforeGroup * snap.GroupRatio)
	crossed := trace.MatchedTier != snap.EstimatedTier

	return TieredResult{
		ActualQuotaBeforeGroup: quotaBeforeGroup,
		ActualQuotaAfterGroup:  afterGroup,
		MatchedTier:            trace.MatchedTier,
		CrossedTier:            crossed,
		CacheBilling:           cacheBilling,
		Clamp:                  clamp,
	}, nil
}

func applyTieredCacheBillingOffset(snap *BillingSnapshot, params TokenParams) (TokenParams, *CacheBillingAdjustment) {
	if snap == nil || snap.CacheBillingOffsetBps <= 0 || snap.CacheBillingOffsetBps > 10000 || params.CR <= 0 {
		return params, nil
	}
	// If cr is absent, OpenAI-style normalization deliberately leaves cache
	// reads in p. Reclassifying again would double-charge the same tokens.
	if !UsedVars(snap.ExprString)["cr"] {
		return params, nil
	}

	totalInputTokens := boundedTokenInt64(params.BillingInputTotal)
	rawCacheReadTokens := boundedTokenInt64(params.CR)
	if totalInputTokens <= 0 || rawCacheReadTokens <= 0 {
		return params, nil
	}

	offsetBps := int64(snap.CacheBillingOffsetBps)
	reclassifiedTokens := (totalInputTokens/10000)*offsetBps +
		(totalInputTokens%10000)*offsetBps/10000
	if reclassifiedTokens > rawCacheReadTokens {
		reclassifiedTokens = rawCacheReadTokens
	}
	if reclassifiedTokens <= 0 {
		return params, nil
	}

	params.P += float64(reclassifiedTokens)
	params.CR -= float64(reclassifiedTokens)
	return params, &CacheBillingAdjustment{
		OffsetBps:              snap.CacheBillingOffsetBps,
		TotalInputTokens:       totalInputTokens,
		RawCacheReadTokens:     rawCacheReadTokens,
		BillingCacheReadTokens: rawCacheReadTokens - reclassifiedTokens,
		ReclassifiedTokens:     reclassifiedTokens,
	}
}

func boundedTokenInt64(value float64) int64 {
	if value <= 0 || math.IsNaN(value) {
		return 0
	}
	if math.IsInf(value, 1) || value >= math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}
