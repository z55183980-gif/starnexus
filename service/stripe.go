package service

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/webhook"
)

const (
	stripeTopUpProductName = "Account Top-up"
	defaultStripeCurrency  = "usd"
)

// StripeCheckoutSessionInput captures server-validated data for a one-time top-up.
type StripeCheckoutSessionInput struct {
	TradeNo      string
	UserID       int
	TopUpAmount  int64
	PayMoney     float64
	ChargedMoney float64
	CustomerID   string
	Email        string
	SuccessURL   string
	CancelURL    string
}

// StripeSubscriptionCheckoutInput captures server-validated data for subscriptions.
type StripeSubscriptionCheckoutInput struct {
	TradeNo    string
	UserID     int
	PriceID    string
	CustomerID string
	Email      string
	SuccessURL string
	CancelURL  string
}

// StripeCheckoutSessionResult is returned after creating a Checkout Session.
type StripeCheckoutSessionResult struct {
	URL       string
	SessionID string
}

// StripeCheckoutSessionEvent is the webhook payload shape we care about.
type StripeCheckoutSessionEvent struct {
	ID                string `json:"id"`
	ClientReferenceID string `json:"client_reference_id"`
	Customer          string `json:"customer"`
	Status            string `json:"status"`
	PaymentStatus     string `json:"payment_status"`
	AmountTotal       int64  `json:"amount_total"`
	Currency          string `json:"currency"`
}

// IsStripeConfigured reports whether Stripe top-up can be offered.
func IsStripeConfigured() bool {
	return isStripeSecretValid(setting.StripeApiSecret) &&
		strings.TrimSpace(setting.StripeWebhookSecret) != ""
}

func isStripeSecretValid(secret string) bool {
	secret = strings.TrimSpace(secret)
	return strings.HasPrefix(secret, "sk_") || strings.HasPrefix(secret, "rk_")
}

// StripeMinTopUp returns the minimum top-up amount in display units.
func StripeMinTopUp() int64 {
	minTopup := setting.StripeMinTopUp
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		minTopup = minTopup * int(common.QuotaPerUnit)
	}
	return int64(minTopup)
}

// StripeMaxTopUp returns the maximum top-up amount in the request/display
// unit. Stripe's business limit is 10,000 canonical credit units; TOKENS
// requests therefore use the equivalent raw token limit.
func StripeMaxTopUp() int64 {
	maxTopup := int64(10000)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		maxTopup = int64(float64(maxTopup) * common.QuotaPerUnit)
	}
	return maxTopup
}

// CalculateStripeChargedAmount returns quota units credited after group ratio.
func CalculateStripeChargedAmount(count float64, userGroup string) float64 {
	topUpGroupRatio := common.GetTopupGroupRatio(userGroup)
	if topUpGroupRatio == 0 {
		topUpGroupRatio = 1
	}
	return count * topUpGroupRatio
}

// CalculateStripePayMoney returns the fiat amount to charge for a top-up.
func CalculateStripePayMoney(amount float64, group string) float64 {
	originalAmount := amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = amount / common.QuotaPerUnit
	}

	topupGroupRatio := common.GetTopupGroupRatio(group)
	if topupGroupRatio == 0 {
		topupGroupRatio = 1
	}

	discount := operation_setting.GetAmountDiscountRateForTopupAmount(int64(originalAmount))
	feeRate := operation_setting.GetAmountFeeRateForTopupAmount(int64(originalAmount))

	// Stripe charges 1:1 in the configured settlement currency (no FX conversion).
	return amount * topupGroupRatio * discount * (1 + feeRate)
}

// StripePayMoneyToUnitAmount converts a fiat amount to Stripe's smallest currency unit.
func StripePayMoneyToUnitAmount(payMoney float64) (int64, error) {
	if payMoney <= 0 {
		return 0, fmt.Errorf("stripe pay amount must be positive")
	}
	unitAmount := int64(math.Round(payMoney * 100))
	if unitAmount < 1 {
		return 0, fmt.Errorf("stripe pay amount is too small")
	}
	return unitAmount, nil
}

func stripeCurrency() string {
	return setting.StripeSettlementCurrency()
}

// CreateTopUpCheckoutSession creates a one-time Checkout Session using server-side price_data.
func CreateTopUpCheckoutSession(input StripeCheckoutSessionInput) (*StripeCheckoutSessionResult, error) {
	if !IsStripeConfigured() {
		return nil, fmt.Errorf("stripe is not configured")
	}
	if strings.TrimSpace(input.TradeNo) == "" {
		return nil, fmt.Errorf("trade number is required")
	}

	if input.PayMoney <= 0 {
		return nil, fmt.Errorf("pay money must be positive")
	}
	unitAmount, err := StripePayMoneyToUnitAmount(input.PayMoney)
	if err != nil {
		return nil, err
	}

	successURL := input.SuccessURL
	if successURL == "" {
		successURL = PaymentReturnURL("/console/log")
	}
	cancelURL := input.CancelURL
	if cancelURL == "" {
		cancelURL = PaymentReturnURL("/console/topup")
	}

	priceData := &stripe.CheckoutSessionLineItemPriceDataParams{
		Currency:   stripe.String(stripeCurrency()),
		UnitAmount: stripe.Int64(unitAmount),
		ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
			Name: stripe.String(stripeTopUpProductName),
		},
	}
	if productID := strings.TrimSpace(setting.StripePriceId); strings.HasPrefix(productID, "prod_") {
		priceData.Product = stripe.String(productID)
		priceData.ProductData = nil
	}

	lineItem := &stripe.CheckoutSessionLineItemParams{
		Quantity:  stripe.Int64(1),
		PriceData: priceData,
	}

	params := &stripe.CheckoutSessionParams{
		Params: stripe.Params{
			IdempotencyKey: stripe.String("topup-" + input.TradeNo),
		},
		ClientReferenceID:   stripe.String(input.TradeNo),
		SuccessURL:          stripe.String(successURL),
		CancelURL:           stripe.String(cancelURL),
		Mode:                stripe.String(string(stripe.CheckoutSessionModePayment)),
		LineItems:           []*stripe.CheckoutSessionLineItemParams{lineItem},
		AllowPromotionCodes: stripe.Bool(setting.StripePromotionCodesEnabled),
		Metadata: map[string]string{
			"trade_no":      input.TradeNo,
			"user_id":       fmt.Sprintf("%d", input.UserID),
			"topup_amount":  fmt.Sprintf("%d", input.TopUpAmount),
			"charged_money": fmt.Sprintf("%.4f", input.ChargedMoney),
		},
	}
	applyStripeCustomer(params, input.CustomerID, input.Email)

	stripe.Key = setting.StripeApiSecret
	result, err := session.New(params)
	if err != nil {
		return nil, err
	}
	return &StripeCheckoutSessionResult{
		URL:       result.URL,
		SessionID: result.ID,
	}, nil
}

// CreateSubscriptionCheckoutSession creates a subscription Checkout Session.
func CreateSubscriptionCheckoutSession(input StripeSubscriptionCheckoutInput) (*StripeCheckoutSessionResult, error) {
	if !IsStripeConfigured() {
		return nil, fmt.Errorf("stripe is not configured")
	}
	priceID := strings.TrimSpace(input.PriceID)
	if !strings.HasPrefix(priceID, "price_") {
		return nil, fmt.Errorf("invalid stripe subscription price id")
	}

	successURL := input.SuccessURL
	if successURL == "" {
		successURL = PaymentReturnURL("/console/topup")
	}
	cancelURL := input.CancelURL
	if cancelURL == "" {
		cancelURL = PaymentReturnURL("/console/topup")
	}

	params := &stripe.CheckoutSessionParams{
		Params: stripe.Params{
			IdempotencyKey: stripe.String("subscription-" + input.TradeNo),
		},
		ClientReferenceID: stripe.String(input.TradeNo),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		Metadata: map[string]string{
			"trade_no": input.TradeNo,
			"user_id":  fmt.Sprintf("%d", input.UserID),
			"kind":     "subscription",
		},
	}
	applyStripeCustomer(params, input.CustomerID, input.Email)

	stripe.Key = setting.StripeApiSecret
	result, err := session.New(params)
	if err != nil {
		return nil, err
	}
	return &StripeCheckoutSessionResult{
		URL:       result.URL,
		SessionID: result.ID,
	}, nil
}

func applyStripeCustomer(params *stripe.CheckoutSessionParams, customerID, email string) {
	customerID = strings.TrimSpace(customerID)
	if customerID != "" {
		params.Customer = stripe.String(customerID)
		return
	}
	if strings.TrimSpace(email) != "" {
		params.CustomerEmail = stripe.String(email)
	}
	params.CustomerCreation = stripe.String(string(stripe.CheckoutSessionCustomerCreationAlways))
}

// ConstructStripeWebhookEvent verifies and parses a Stripe webhook payload.
func ConstructStripeWebhookEvent(payload []byte, signature string) (stripe.Event, error) {
	return webhook.ConstructEventWithOptions(
		payload,
		signature,
		setting.StripeWebhookSecret,
		webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true,
		},
	)
}

// ParseCheckoutSessionEvent extracts checkout session fields from a webhook event.
func ParseCheckoutSessionEvent(event stripe.Event) (*StripeCheckoutSessionEvent, error) {
	var checkoutSession StripeCheckoutSessionEvent
	if err := common.Unmarshal(event.Data.Raw, &checkoutSession); err != nil {
		return nil, err
	}
	if checkoutSession.ClientReferenceID == "" {
		return nil, fmt.Errorf("checkout session missing client_reference_id")
	}
	return &checkoutSession, nil
}

// BuildStripeFulfillmentPayload serializes webhook data for order audit logs.
func BuildStripeFulfillmentPayload(session *StripeCheckoutSessionEvent, eventType string) string {
	payload := map[string]any{
		"session_id":     session.ID,
		"customer":       session.Customer,
		"amount_total":   session.AmountTotal,
		"currency":       strings.ToUpper(session.Currency),
		"event_type":     eventType,
		"payment_status": session.PaymentStatus,
	}
	return common.GetJsonString(payload)
}
