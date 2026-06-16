package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
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
	t.Cleanup(func() {
		setting.StripeUnitPrice = originalUnitPrice
	})

	setting.StripeUnitPrice = 8
	payMoney := CalculateStripePayMoney(10, "unknown-group")
	require.Equal(t, float64(10), payMoney)
}

func TestCalculateStripeChargedAmountUsesGroupRatio(t *testing.T) {
	// Uses global group ratio map; default unknown group should still return count.
	charged := CalculateStripeChargedAmount(10, "unknown-group")
	require.Equal(t, float64(10), charged)
}

