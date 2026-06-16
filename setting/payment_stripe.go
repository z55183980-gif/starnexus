package setting

import "strings"

var StripeApiSecret = ""
var StripeWebhookSecret = ""
var StripePriceId = ""
var StripeCurrency = "usd"
var StripeUnitPrice = 8.0
var StripeMinTopUp = 1
var StripePromotionCodesEnabled = false

var supportedStripeCurrencies = map[string]struct{}{
	"usd": {},
	"cny": {},
	"hkd": {},
}

// NormalizeStripeCurrency returns a supported Stripe currency code.
func NormalizeStripeCurrency(currency string) string {
	normalized := strings.ToLower(strings.TrimSpace(currency))
	if _, ok := supportedStripeCurrencies[normalized]; ok {
		return normalized
	}
	return "usd"
}

// StripeSettlementCurrency returns the currency sent to Stripe Checkout.
func StripeSettlementCurrency() string {
	return NormalizeStripeCurrency(StripeCurrency)
}
