package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
	"github.com/stripe/stripe-go/v81"
)

func TestIsStripeConfigured(t *testing.T) {
	originalAPISecret := setting.StripeApiSecret
	originalWebhookSecret := setting.StripeWebhookSecret
	t.Cleanup(func() {
		setting.StripeApiSecret = originalAPISecret
		setting.StripeWebhookSecret = originalWebhookSecret
	})

	setting.StripeApiSecret = "sk_test_123"
	setting.StripeWebhookSecret = "whsec_test"
	require.True(t, IsStripeConfigured())

	setting.StripeWebhookSecret = ""
	require.False(t, IsStripeConfigured())
}

func TestCalculateStripePayMoneyNormalizesTokenFeeLookup(t *testing.T) {
	originalDisplayType := operation_setting.GetGeneralSetting().QuotaDisplayType
	originalQuotaPerUnit := common.QuotaPerUnit
	originalFees := operation_setting.GetPaymentSetting().AmountFee
	t.Cleanup(func() {
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetPaymentSetting().AmountFee = originalFees
	})

	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
	common.QuotaPerUnit = 500000
	operation_setting.GetPaymentSetting().AmountFee = map[int]float64{100: 0.05}

	require.Equal(t, int64(5_000_000_000), StripeMaxTopUp())
	require.Equal(t, 105.0, CalculateStripePayMoney(50_000_000, "unknown-group"))
}

func TestStripePayMoneyToUnitAmount(t *testing.T) {
	unitAmount, err := StripePayMoneyToUnitAmount(12.34)
	require.NoError(t, err)
	require.Equal(t, int64(1234), unitAmount)

	_, err = StripePayMoneyToUnitAmount(0)
	require.Error(t, err)
}

func TestParseCheckoutSessionEvent(t *testing.T) {
	raw := []byte(`{
		"id":"cs_test_1",
		"client_reference_id":"ref_abc",
		"customer":"cus_test",
		"status":"complete",
		"payment_status":"paid",
		"amount_total":2500,
		"currency":"usd"
	}`)

	event := stripe.Event{
		Data: &stripe.EventData{Raw: raw},
	}

	session, err := ParseCheckoutSessionEvent(event)
	require.NoError(t, err)
	require.Equal(t, "ref_abc", session.ClientReferenceID)
	require.Equal(t, "cus_test", session.Customer)
	require.Equal(t, int64(2500), session.AmountTotal)

	payload := BuildStripeFulfillmentPayload(session, "checkout.session.completed")
	require.Contains(t, payload, "cs_test_1")
	require.Contains(t, payload, "2500")
}

func TestNormalizeStripeCurrency(t *testing.T) {
	require.Equal(t, "usd", setting.NormalizeStripeCurrency(""))
	require.Equal(t, "cny", setting.NormalizeStripeCurrency("CNY"))
	require.Equal(t, "hkd", setting.NormalizeStripeCurrency("hkd"))
	require.Equal(t, "usd", setting.NormalizeStripeCurrency("eur"))
}

func TestCalculateStripePayMoneyUsesOneToOneRate(t *testing.T) {
	originalUnitPrice := setting.StripeUnitPrice
	originalFees := operation_setting.GetPaymentSetting().AmountFee
	t.Cleanup(func() {
		setting.StripeUnitPrice = originalUnitPrice
		operation_setting.GetPaymentSetting().AmountFee = originalFees
	})

	setting.StripeUnitPrice = 8
	payMoney := CalculateStripePayMoney(10, "unknown-group")
	require.Equal(t, float64(10), payMoney)
}

func TestCalculateStripePayMoneyAppliesExactAmountFee(t *testing.T) {
	originalFees := operation_setting.GetPaymentSetting().AmountFee
	t.Cleanup(func() {
		operation_setting.GetPaymentSetting().AmountFee = originalFees
	})

	operation_setting.GetPaymentSetting().AmountFee = map[int]float64{100: 0.03}
	require.Equal(t, 103.0, CalculateStripePayMoney(100, "unknown-group"))
	require.Equal(t, 101.0, CalculateStripePayMoney(101, "unknown-group"))
}

func TestCalculateStripeChargedAmountUsesGroupRatio(t *testing.T) {
	// Uses global group ratio map; default unknown group should still return count.
	charged := CalculateStripeChargedAmount(10, "unknown-group")
	require.Equal(t, float64(10), charged)
}
