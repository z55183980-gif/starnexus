package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestEpusdtPayMoneyFromCredit(t *testing.T) {
	originalRatio := setting.EpUSDTCreditPerUSDT
	t.Cleanup(func() {
		setting.EpUSDTCreditPerUSDT = originalRatio
	})
	setting.EpUSDTCreditPerUSDT = 6.8

	tests := []struct {
		credit int64
		want   float64
	}{
		{credit: 68, want: 10},
		{credit: 50, want: 7.35},
		{credit: 7, want: 1.03},
	}

	for _, tt := range tests {
		got := epusdtPayMoneyFromCredit(tt.credit)
		if got != tt.want {
			t.Fatalf("epusdtPayMoneyFromCredit(%d) = %v, want %v", tt.credit, got, tt.want)
		}
	}

	setting.EpUSDTCreditPerUSDT = 5
	if got := epusdtPayMoneyFromCredit(50); got != 10 {
		t.Fatalf("epusdtPayMoneyFromCredit(50) with ratio 5 = %v, want 10", got)
	}
}

func TestIsUsdtTopUpEnabledWithoutPayMethodEntry(t *testing.T) {
	originalToken := setting.EpUSDTApiToken
	originalGateway := setting.EpUSDTGatewayAddress
	originalPayMethods := operation_setting.PayMethods
	originalCompliance := operation_setting.GetPaymentSetting().ComplianceConfirmed
	originalTerms := operation_setting.GetPaymentSetting().ComplianceTermsVersion
	t.Cleanup(func() {
		setting.EpUSDTApiToken = originalToken
		setting.EpUSDTGatewayAddress = originalGateway
		operation_setting.PayMethods = originalPayMethods
		paymentSetting := operation_setting.GetPaymentSetting()
		paymentSetting.ComplianceConfirmed = originalCompliance
		paymentSetting.ComplianceTermsVersion = originalTerms
	})

	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	setting.EpUSDTApiToken = "test-token"
	setting.EpUSDTGatewayAddress = "https://pay.example.com"
	operation_setting.PayMethods = []map[string]string{
		{"name": "Stripe", "type": "stripe", "enabled": "true"},
	}

	if !isUsdtTopUpEnabled() {
		t.Fatal("expected USDT top-up to be enabled when EpUSDT is configured even without PayMethods entry")
	}

	operation_setting.PayMethods = append(operation_setting.PayMethods, map[string]string{
		"name": "USDT", "type": paymentMethodUsdt, "enabled": "false",
	})
	if isUsdtTopUpEnabled() {
		t.Fatal("expected explicit enabled=false in PayMethods to disable USDT top-up")
	}
}

func TestEpusdtGatewayAmountUsesDisplayedUSDTAmount(t *testing.T) {
	originalRatio := setting.EpUSDTCreditPerUSDT
	t.Cleanup(func() {
		setting.EpUSDTCreditPerUSDT = originalRatio
	})
	setting.EpUSDTCreditPerUSDT = 6.8

	pay := epusdtPayMoneyFromCredit(50)
	if pay != 7.35 {
		t.Fatalf("epusdtPayMoneyFromCredit(50) = %v, want 7.35", pay)
	}
	if got := epusdtAmountSignString(pay); got != "7.35" {
		t.Fatalf("epusdtAmountSignString(%v) = %q, want %q", pay, got, "7.35")
	}
}
