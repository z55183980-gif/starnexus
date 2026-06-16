package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestEpusdtPayMoneyFromCredit(t *testing.T) {
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

func TestEpusdtGatewayFiatAmount(t *testing.T) {
	got := epusdtGatewayFiatAmount(100)
	if got != 100 {
		t.Fatalf("epusdtGatewayFiatAmount(100) = %v, want 100", got)
	}

	pay := epusdtPayMoneyFromCredit(100)
	if pay != 14.71 {
		t.Fatalf("epusdtPayMoneyFromCredit(100) = %v, want 14.71", pay)
	}
}
