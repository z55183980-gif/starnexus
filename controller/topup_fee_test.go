package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func TestValidateAmountFeeJSON(t *testing.T) {
	require.NoError(t, validateAmountFeeJSON(`{}`))
	require.NoError(t, validateAmountFeeJSON(`{"100":0.03,"200":1}`))
	require.NoError(t, validateAmountFeeJSON(`{"stripe:100":0.03,"usdt:100":0}`))
	require.Error(t, validateAmountFeeJSON(`null`))
	require.Error(t, validateAmountFeeJSON(`{"0":0.03}`))
	require.Error(t, validateAmountFeeJSON(`{"100":1.01}`))
	require.Error(t, validateAmountFeeJSON(`{"100":-0.01}`))
	require.Error(t, validateAmountFeeJSON(`{"stripe:":0.03}`))
	require.Error(t, validateAmountFeeJSON(`{" stripe:100":0.03}`))
	require.Error(t, validateAmountFeeJSON(`{"stripe:100:extra":0.03}`))
}

func TestGetPayMoneyNormalizesTokenAmountBeforeFeeLookup(t *testing.T) {
	originalPrice := operation_setting.Price
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalFees := operation_setting.GetPaymentSetting().AmountFee
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		operation_setting.GetPaymentSetting().AmountFee = originalFees
	})

	operation_setting.Price = 1
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{100: 0.9}
	operation_setting.GetPaymentSetting().AmountFee = map[string]float64{"100": 0.05}

	if got := getPayMoney(50_000_000, "default"); got != 94.5 {
		t.Fatalf("getPayMoney(50000000, default) = %v, want 94.5", got)
	}
}

func TestGetPayMoneyAppliesFeeAfterDiscount(t *testing.T) {
	originalPrice := operation_setting.Price
	originalDiscounts := operation_setting.GetPaymentSetting().AmountDiscount
	originalFees := operation_setting.GetPaymentSetting().AmountFee
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
		operation_setting.GetPaymentSetting().AmountDiscount = originalDiscounts
		operation_setting.GetPaymentSetting().AmountFee = originalFees
	})

	operation_setting.Price = 1
	operation_setting.GetPaymentSetting().AmountDiscount = map[int]float64{100: 0.9}
	operation_setting.GetPaymentSetting().AmountFee = map[string]float64{"100": 0.05}

	if got := getPayMoney(100, "default"); got != 94.5 {
		t.Fatalf("getPayMoney(100, default) = %v, want 94.5", got)
	}
}

func TestGetPayMoneyUsesPaymentChannelFee(t *testing.T) {
	originalPrice := operation_setting.Price
	originalFees := operation_setting.GetPaymentSetting().AmountFee
	t.Cleanup(func() {
		operation_setting.Price = originalPrice
		operation_setting.GetPaymentSetting().AmountFee = originalFees
	})

	operation_setting.Price = 1
	operation_setting.GetPaymentSetting().AmountFee = map[string]float64{
		"100":        0.02,
		"stripe:100": 0.05,
	}

	if got := getPayMoney(100, "default", "stripe"); got != 105 {
		t.Fatalf("stripe pay money = %v, want 105", got)
	}
	if got := getPayMoney(100, "default", "usdt"); got != 100 {
		t.Fatalf("usdt pay money = %v, want 100", got)
	}
}
