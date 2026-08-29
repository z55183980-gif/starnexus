package operation_setting

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/shopspring/decimal"
)

type PaymentSetting struct {
	AmountOptions  []int           `json:"amount_options"`
	AmountDiscount map[int]float64 `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	AmountFee      map[int]float64 `json:"amount_fee"`      // 充值金额对应的手续费率，例如 100 元 0.03 表示加收 3% 手续费

	ComplianceConfirmed    bool   `json:"compliance_confirmed"`
	ComplianceTermsVersion string `json:"compliance_terms_version"`
	ComplianceConfirmedAt  int64  `json:"compliance_confirmed_at"`
	ComplianceConfirmedBy  int    `json:"compliance_confirmed_by"`
	ComplianceConfirmedIP  string `json:"compliance_confirmed_ip"`
}

const CurrentComplianceTermsVersion = "v1"

// 默认配置
var paymentSetting = PaymentSetting{
	AmountOptions:  []int{10, 20, 50, 100, 200, 500},
	AmountDiscount: map[int]float64{},
	AmountFee:      map[int]float64{},
}

func init() {
	// 注册到全局配置管理器
	config.GlobalConfig.Register("payment_setting", &paymentSetting)
}

func GetPaymentSetting() *PaymentSetting {
	return &paymentSetting
}

// GetAmountFeeRate returns the configured fee rate for an exact recharge
// amount. Invalid or out-of-range values are ignored so a malformed option
// cannot unexpectedly make an order unpayable.
func GetAmountFeeRate(amount int) float64 {
	rate, ok := paymentSetting.AmountFee[amount]
	if !ok || rate <= 0 || rate > 1 {
		return 0
	}
	return rate
}

// GetPaymentAmountKey converts a top-up request amount into the canonical
// amount key used by AmountDiscount/AmountFee. Configuration keys are always
// expressed in the site's credit currency (USD/CNY/custom), while TOKENS
// requests carry the raw token amount. A non-integral token amount has no
// exact configured key and therefore returns false instead of silently
// truncating into a different pricing tier.
func GetPaymentAmountKey(amount int64) (int, bool) {
	if amount <= 0 {
		return 0, false
	}
	if GetQuotaDisplayType() != QuotaDisplayTypeTokens {
		if amount > int64(^uint(0)>>1) {
			return 0, false
		}
		return int(amount), true
	}
	if common.QuotaPerUnit <= 0 {
		return 0, false
	}
	canonical := decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit))
	if !canonical.IsInteger() {
		return 0, false
	}
	value := canonical.IntPart()
	if value <= 0 || value > int64(^uint(0)>>1) {
		return 0, false
	}
	return int(value), true
}

// IsPaymentAmountAligned reports whether a positive request amount can be
// represented exactly by the stored canonical credit-unit amount. Nonpositive
// values are left to the endpoint's minimum-amount validation.
func IsPaymentAmountAligned(amount int64) bool {
	if amount <= 0 || GetQuotaDisplayType() != QuotaDisplayTypeTokens {
		return true
	}
	_, ok := GetPaymentAmountKey(amount)
	return ok
}

// GetAmountDiscountRateForTopupAmount returns the exact configured discount
// for a request amount, normalizing TOKENS requests to the canonical credit
// amount first. Invalid or missing discounts fall back to 1 (no discount).
func GetAmountDiscountRateForTopupAmount(amount int64) float64 {
	if key, ok := GetPaymentAmountKey(amount); ok {
		if rate, exists := paymentSetting.AmountDiscount[key]; exists && rate > 0 && rate <= 1 {
			return rate
		}
	}
	// Before TOKENS request units were normalized, some installations stored
	// discount keys as raw tokens. Keep that legacy form working while new
	// configurations use canonical credit-unit keys.
	if GetQuotaDisplayType() == QuotaDisplayTypeTokens && amount <= int64(^uint(0)>>1) {
		if rate, exists := paymentSetting.AmountDiscount[int(amount)]; exists && rate > 0 && rate <= 1 {
			return rate
		}
	}
	return 1
}

// GetAmountFeeRateForTopupAmount returns the exact configured fee for a
// request amount, normalizing TOKENS requests to the canonical credit amount
// first. Invalid or missing fees fall back to 0.
func GetAmountFeeRateForTopupAmount(amount int64) float64 {
	if key, ok := GetPaymentAmountKey(amount); ok {
		if rate := GetAmountFeeRate(key); rate > 0 {
			return rate
		}
	}
	// Backward compatibility for installations that keyed TOKENS tiers by raw
	// token count before the canonical-unit contract was introduced.
	if GetQuotaDisplayType() == QuotaDisplayTypeTokens && amount <= int64(^uint(0)>>1) {
		return GetAmountFeeRate(int(amount))
	}
	return 0
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}
