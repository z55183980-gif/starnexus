package service

import "github.com/QuantumNous/new-api/common"

// minimumTokenConsumeUSD is the smallest charge for a billable request that
// has a positive upstream token count. Requests without token usage keep the
// existing zero-charge/refund behavior.
const minimumTokenConsumeUSD = 0.001

// applyMinimumTokenConsumeQuota applies the token-request floor in internal
// quota units. The caller must pass only actual upstream token counts (not
// locally estimated pre-consume tokens).
func applyMinimumTokenConsumeQuota(quota, tokenCount int, billable bool) int {
	if tokenCount <= 0 || !billable || common.QuotaPerUnit <= 0 {
		return quota
	}

	minimumQuota := common.QuotaRound(minimumTokenConsumeUSD * common.QuotaPerUnit)
	if quota < minimumQuota {
		return minimumQuota
	}
	return quota
}
