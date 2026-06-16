package model

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

func TestTopUpRechargeUSDUsesUserRechargeAmount(t *testing.T) {
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	t.Cleanup(func() {
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
	})

	topUp := &TopUp{
		Amount:          50,
		Money:           7.35,
		PaymentProvider: PaymentProviderEpusdt,
	}
	got := TopUpRechargeUSD(topUp)
	want := decimal.NewFromInt(50)
	if !got.Equal(want) {
		t.Fatalf("epusdt recharge USD = %s, want %s", got, want)
	}

	epayTopUp := &TopUp{
		Amount:          100,
		Money:           730,
		PaymentProvider: PaymentProviderEpay,
	}
	got = TopUpRechargeUSD(epayTopUp)
	want = decimal.NewFromInt(100)
	if !got.Equal(want) {
		t.Fatalf("epay recharge USD = %s, want %s", got, want)
	}

	waffoTopUp := &TopUp{
		Amount:          20,
		Money:           146,
		PaymentProvider: PaymentProviderWaffo,
	}
	got = TopUpRechargeUSD(waffoTopUp)
	want = decimal.NewFromInt(20)
	if !got.Equal(want) {
		t.Fatalf("waffo recharge USD = %s, want %s", got, want)
	}
}

func TestTopUpRechargeUSDStripeUsesCreditedMoney(t *testing.T) {
	topUp := &TopUp{
		Amount:          100,
		Money:           120,
		PaymentProvider: PaymentProviderStripe,
	}
	got := TopUpRechargeUSD(topUp)
	want := decimal.NewFromInt(120)
	if !got.Equal(want) {
		t.Fatalf("stripe recharge USD = %s, want %s", got, want)
	}
}
