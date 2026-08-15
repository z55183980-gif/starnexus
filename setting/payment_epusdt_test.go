package setting

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
)

func TestEffectiveEpUSDTCreditPerUSDT(t *testing.T) {
	original := EpUSDTCreditPerUSDT
	t.Cleanup(func() {
		EpUSDTCreditPerUSDT = original
	})

	EpUSDTCreditPerUSDT = 5
	if got := EffectiveEpUSDTCreditPerUSDT(); got != 5 {
		t.Fatalf("custom ratio = %v, want 5", got)
	}

	for _, invalid := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		EpUSDTCreditPerUSDT = invalid
		if got := EffectiveEpUSDTCreditPerUSDT(); got != DefaultEpUSDTCreditPerUSDT {
			t.Fatalf("invalid ratio %v returned %v, want default %v", invalid, got, DefaultEpUSDTCreditPerUSDT)
		}
	}
}

func TestIsLegacyEpUSDTGatewayAddress(t *testing.T) {
	tests := []struct {
		name      string
		gateway   string
		pay       string
		wantMatch bool
	}{
		{
			name:      "matching with trailing slash",
			gateway:   "https://pay.xingyuapi.com/",
			pay:       "https://pay.xingyuapi.com",
			wantMatch: true,
		},
		{
			name:      "different hosts",
			gateway:   "https://epusdt.example.com",
			pay:       "https://pay.xingyuapi.com",
			wantMatch: false,
		},
		{
			name:      "empty pay address",
			gateway:   "https://pay.xingyuapi.com",
			pay:       "",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsLegacyEpUSDTGatewayAddress(tt.gateway, tt.pay)
			if got != tt.wantMatch {
				t.Fatalf("IsLegacyEpUSDTGatewayAddress(%q, %q) = %v, want %v", tt.gateway, tt.pay, got, tt.wantMatch)
			}
		})
	}
}

func TestEffectiveEpUSDTNotifyURL(t *testing.T) {
	original := EpUSDTNotifyURL
	defer func() {
		EpUSDTNotifyURL = original
	}()

	EpUSDTNotifyURL = ""
	if got := EffectiveEpUSDTNotifyURL("https://api.example.com/"); got != "https://api.example.com/api/epusdt/notify" {
		t.Fatalf("default notify URL = %q", got)
	}

	EpUSDTNotifyURL = "https://callback.example.com/api/epusdt/notify"
	if got := EffectiveEpUSDTNotifyURL("https://api.example.com"); got != "https://callback.example.com/api/epusdt/notify" {
		t.Fatalf("custom notify URL = %q", got)
	}
}

func TestEffectiveEpUSDTGatewayAddressRejectsLegacyValue(t *testing.T) {
	originalGateway := EpUSDTGatewayAddress
	originalPayAddress := operation_setting.PayAddress
	originalEpayId := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	originalPayMethods := operation_setting.PayMethods
	defer func() {
		EpUSDTGatewayAddress = originalGateway
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayId
		operation_setting.EpayKey = originalEpayKey
		operation_setting.PayMethods = originalPayMethods
	}()

	EpUSDTGatewayAddress = "https://pay.xingyuapi.com"
	operation_setting.PayAddress = "https://pay.xingyuapi.com/"
	operation_setting.EpayId = ""
	operation_setting.EpayKey = ""
	if got := EffectiveEpUSDTGatewayAddress("https://pay.xingyuapi.com/"); got != "https://pay.xingyuapi.com" {
		t.Fatalf("expected gateway to be allowed when Epay is not configured, got %q", got)
	}

	operation_setting.EpayId = "1000"
	operation_setting.EpayKey = "secret"
	operation_setting.PayMethods = []map[string]string{
		{"type": "stripe", "enabled": "true"},
	}
	if got := EffectiveEpUSDTGatewayAddress("https://pay.xingyuapi.com/"); got != "https://pay.xingyuapi.com" {
		t.Fatalf("expected gateway allowed when legacy epay is not offered, got %q", got)
	}

	operation_setting.PayMethods = []map[string]string{
		{"type": "alipay", "enabled": "true"},
	}
	if got := EffectiveEpUSDTGatewayAddress("https://pay.xingyuapi.com/"); got != "" {
		t.Fatalf("expected legacy gateway to be rejected when alipay is offered, got %q", got)
	}

	EpUSDTGatewayAddress = "https://epusdt.example.com"
	if got := EffectiveEpUSDTGatewayAddress("https://pay.xingyuapi.com"); got != "https://epusdt.example.com" {
		t.Fatalf("expected valid gateway, got %q", got)
	}

	EpUSDTGatewayAddress = ""
	operation_setting.EpayId = ""
	operation_setting.EpayKey = ""
	if got := EffectiveEpUSDTGatewayAddress("https://legacy.example.com"); got != DefaultEpUSDTGatewayAddress {
		t.Fatalf("expected default gateway when stored value is empty, got %q", got)
	}
}

func TestIsLegacyEpayOfferedInPayMethods(t *testing.T) {
	originalPayMethods := operation_setting.PayMethods
	t.Cleanup(func() {
		operation_setting.PayMethods = originalPayMethods
	})

	operation_setting.PayMethods = []map[string]string{
		{"type": "stripe", "enabled": "true"},
		{"type": "usdt", "enabled": "true"},
	}
	if operation_setting.IsLegacyEpayOfferedInPayMethods() {
		t.Fatal("expected no legacy epay when only stripe/usdt are listed")
	}

	operation_setting.PayMethods = []map[string]string{
		{"type": "alipay", "enabled": "true"},
	}
	if !operation_setting.IsLegacyEpayOfferedInPayMethods() {
		t.Fatal("expected legacy epay when alipay is listed")
	}
}

func TestIsEpayConfigured(t *testing.T) {
	originalPayAddress := operation_setting.PayAddress
	originalEpayId := operation_setting.EpayId
	originalEpayKey := operation_setting.EpayKey
	defer func() {
		operation_setting.PayAddress = originalPayAddress
		operation_setting.EpayId = originalEpayId
		operation_setting.EpayKey = originalEpayKey
	}()

	operation_setting.PayAddress = "https://pay.example.com"
	operation_setting.EpayId = ""
	operation_setting.EpayKey = ""
	if IsEpayConfigured() {
		t.Fatal("expected Epay to be unconfigured when id/key are missing")
	}

	operation_setting.EpayId = "1000"
	operation_setting.EpayKey = "secret"
	if !IsEpayConfigured() {
		t.Fatal("expected Epay to be configured when all fields are set")
	}
}
