package common

// Affiliate (邀请返利) 全局配置 — 通过 options 表持久化
var (
	AffiliateEnabled                 = true
	AffiliateRebateRate              = 0.0 // 百分比 0-100
	AffiliateRebateFreezeHours       = 0
	AffiliateRebateDurationDays      = 0
	AffiliateRebatePerInviteeCapUSD  = 0.0 // 0 = 无上限
)

const (
	AffiliateRebateRateDefault         = 0.0
	AffiliateRebateRateMin             = 0.0
	AffiliateRebateRateMax             = 100.0
	AffiliateRebateFreezeHoursMax      = 720
	AffiliateRebateDurationDaysMax     = 3650
	AffiliateCodeMinLength             = 4
	AffiliateCodeMaxLength             = 32
	AffiliateLedgerActionAccrue        = "accrue"
	AffiliateLedgerActionTransfer      = "transfer"
	AffiliateTopUpAuditApplied         = "AFFILIATE_REBATE_APPLIED"
	AffiliateTopUpAuditSkipped         = "AFFILIATE_REBATE_SKIPPED"
)
