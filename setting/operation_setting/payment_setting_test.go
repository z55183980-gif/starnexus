package operation_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestGetAmountFeeRateUsesExactAmountAndIgnoresInvalidRates(t *testing.T) {
	originalFees := paymentSetting.AmountFee
	t.Cleanup(func() {
		paymentSetting.AmountFee = originalFees
	})

	paymentSetting.AmountFee = map[string]float64{
		"100": 0.03,
		"200": 0,
		"300": 1.01,
	}

	if got := GetAmountFeeRate(100); got != 0.03 {
		t.Fatalf("GetAmountFeeRate(100) = %v, want 0.03", got)
	}
	if got := GetAmountFeeRate(101); got != 0 {
		t.Fatalf("GetAmountFeeRate(101) = %v, want 0", got)
	}
	if got := GetAmountFeeRate(200); got != 0 {
		t.Fatalf("GetAmountFeeRate(200) = %v, want 0", got)
	}
	if got := GetAmountFeeRate(300); got != 0 {
		t.Fatalf("GetAmountFeeRate(300) = %v, want 0", got)
	}
}

func TestAmountRatesNormalizeTokenRequestsToCanonicalCredit(t *testing.T) {
	originalDisplayType := generalSetting.QuotaDisplayType
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscounts := paymentSetting.AmountDiscount
	originalFees := paymentSetting.AmountFee
	t.Cleanup(func() {
		generalSetting.QuotaDisplayType = originalDisplayType
		common.QuotaPerUnit = originalQuotaPerUnit
		paymentSetting.AmountDiscount = originalDiscounts
		paymentSetting.AmountFee = originalFees
	})

	generalSetting.QuotaDisplayType = QuotaDisplayTypeTokens
	common.QuotaPerUnit = 500000
	paymentSetting.AmountDiscount = map[int]float64{100: 0.9}
	paymentSetting.AmountFee = map[string]float64{"100": 0.05}

	key, ok := GetPaymentAmountKey(50_000_000)
	if !ok || key != 100 {
		t.Fatalf("GetPaymentAmountKey(50000000) = (%d, %t), want (100, true)", key, ok)
	}
	if got := GetAmountDiscountRateForTopupAmount(50_000_000); got != 0.9 {
		t.Fatalf("token discount = %v, want 0.9", got)
	}
	if got := GetAmountFeeRateForTopupAmount(50_000_000); got != 0.05 {
		t.Fatalf("token fee = %v, want 0.05", got)
	}
	if _, ok := GetPaymentAmountKey(50_000_001); ok {
		t.Fatal("expected non-integral token amount to have no exact pricing key")
	}
}

func TestAmountFeeRateCanBeScopedToPaymentMethod(t *testing.T) {
	originalFees := paymentSetting.AmountFee
	t.Cleanup(func() { paymentSetting.AmountFee = originalFees })

	paymentSetting.AmountFee = map[string]float64{
		"stripe:100": 0.03,
		"usdt:100":   0.05,
	}
	if got := GetAmountFeeRateForTopupAmountAndMethod(100, "stripe"); got != 0.03 {
		t.Fatalf("stripe fee = %v, want 0.03", got)
	}
	if got := GetAmountFeeRateForTopupAmountAndMethod(100, "usdt"); got != 0.05 {
		t.Fatalf("usdt fee = %v, want 0.05", got)
	}
	if got := GetAmountFeeRateForTopupAmountAndMethod(100, "alipay"); got != 0 {
		t.Fatalf("unconfigured channel fee = %v, want 0", got)
	}
}

func TestChannelFeeCanExplicitlyDisableLegacyFee(t *testing.T) {
	originalFees := paymentSetting.AmountFee
	t.Cleanup(func() { paymentSetting.AmountFee = originalFees })

	paymentSetting.AmountFee = map[string]float64{
		"100":        0.03,
		"stripe:100": 0,
	}
	if got := GetAmountFeeRateForTopupAmountAndMethod(100, "stripe"); got != 0 {
		t.Fatalf("explicit zero channel fee = %v, want 0", got)
	}
	if got := GetAmountFeeRateForTopupAmountAndMethod(100, "usdt"); got != 0 {
		t.Fatalf("unconfigured channel fee = %v, want 0", got)
	}
	if got := GetAmountFeeRateForTopupAmount(100); got != 0 {
		t.Fatalf("fee without selected channel = %v, want 0", got)
	}
}
