package operation_setting

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/shopspring/decimal"
)

type PaymentSetting struct {
	AmountOptions  []int              `json:"amount_options"`
	AmountDiscount map[int]float64    `json:"amount_discount"` // 充值金额对应的折扣，例如 100 元 0.9 表示 100 元充值享受 9 折优惠
	AmountFee      map[string]float64 `json:"amount_fee"`      // 充值手续费；纯金额键为历史全渠道规则，渠道:金额键仅对指定渠道生效

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
	AmountFee:      map[string]float64{},
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
	rate, ok := paymentSetting.AmountFee[strconv.Itoa(amount)]
	if !ok || rate <= 0 || rate > 1 {
		return 0
	}
	return rate
}

func amountFeeRuleKey(amount int, paymentMethod string) string {
	paymentMethod = strings.TrimSpace(paymentMethod)
	if paymentMethod == "" {
		return strconv.Itoa(amount)
	}
	return paymentMethod + ":" + strconv.Itoa(amount)
}

func getAmountFeeRateByKey(key string) (float64, bool) {
	rate, ok := paymentSetting.AmountFee[key]
	if !ok || rate < 0 || rate > 1 {
		return 0, false
	}
	return rate, true
}

func hasChannelSpecificAmountFee(amountKey string) bool {
	for key := range paymentSetting.AmountFee {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 && parts[0] != "" && parts[1] == amountKey {
			return true
		}
	}
	return false
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
	return GetAmountFeeRateForTopupAmountAndMethod(amount, "")
}

// GetAmountFeeRateForTopupAmountAndMethod returns the exact configured fee for
// a request amount and payment channel. Channel-specific rules take priority;
// amount-only rules remain the backward-compatible all-channel fallback.
func GetAmountFeeRateForTopupAmountAndMethod(amount int64, paymentMethod string) float64 {
	if key, ok := GetPaymentAmountKey(amount); ok {
		if paymentMethod != "" {
			if rate, exists := getAmountFeeRateByKey(amountFeeRuleKey(key, paymentMethod)); exists {
				return rate
			}
			if GetQuotaDisplayType() == QuotaDisplayTypeTokens {
				if rate, exists := getAmountFeeRateByKey(amountFeeRuleKey(int(amount), paymentMethod)); exists {
					return rate
				}
			}
			if hasChannelSpecificAmountFee(strconv.Itoa(key)) {
				return 0
			}
			if rate, exists := getAmountFeeRateByKey(strconv.Itoa(key)); exists {
				return rate
			}
		} else if hasChannelSpecificAmountFee(strconv.Itoa(key)) {
			return 0
		}
		if rate, exists := getAmountFeeRateByKey(strconv.Itoa(key)); exists {
			return rate
		}
	}

	// Backward compatibility for installations that keyed TOKENS tiers by raw
	// token count before canonical-unit normalization was introduced.
	if GetQuotaDisplayType() == QuotaDisplayTypeTokens && amount <= int64(^uint(0)>>1) {
		if paymentMethod != "" {
			if rate, exists := getAmountFeeRateByKey(amountFeeRuleKey(int(amount), paymentMethod)); exists {
				return rate
			}
			if hasChannelSpecificAmountFee(strconv.Itoa(int(amount))) {
				return 0
			}
			if rate, exists := getAmountFeeRateByKey(strconv.Itoa(int(amount))); exists {
				return rate
			}
		} else if hasChannelSpecificAmountFee(strconv.Itoa(int(amount))) {
			return 0
		}
		if rate, exists := getAmountFeeRateByKey(strconv.Itoa(int(amount))); exists {
			return rate
		}
	}
	return 0
}

// GetLegacyAmountFee returns historical global rules for API consumers.
func GetLegacyAmountFee() map[int]float64 {
	result := make(map[int]float64)
	for key, rate := range paymentSetting.AmountFee {
		if strings.Contains(key, ":") {
			continue
		}
		amount, err := strconv.Atoi(key)
		if err != nil || amount <= 0 || rate < 0 || rate > 1 {
			continue
		}
		result[amount] = rate
	}
	return result
}

// GetChannelAmountFee returns channel-specific rules grouped by payment type.
func GetChannelAmountFee() map[string]map[int]float64 {
	result := make(map[string]map[int]float64)
	for key, rate := range paymentSetting.AmountFee {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			continue
		}
		amount, err := strconv.Atoi(parts[1])
		if err != nil || amount <= 0 || rate < 0 || rate > 1 {
			continue
		}
		method := strings.TrimSpace(parts[0])
		if result[method] == nil {
			result[method] = make(map[int]float64)
		}
		result[method][amount] = rate
	}
	return result
}

func IsPaymentComplianceConfirmed() bool {
	return paymentSetting.ComplianceConfirmed &&
		paymentSetting.ComplianceTermsVersion == CurrentComplianceTermsVersion
}
