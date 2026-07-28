package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PaymentClient is a minimal client for Stripe's REST API — built directly
// against Stripe's HTTP API rather than the official stripe-go SDK.
//
// Why not the SDK: stripe-go is a large dependency for what's underneath
// just a handful of form-encoded POST requests. Calling the REST API
// directly keeps this project's total external dependency count at exactly
// one (modernc.org/sqlite), the same philosophy as geocode.go calling
// Nominatim's HTTP API directly instead of pulling in a geocoding library.
//
// Runs entirely in Stripe's TEST mode — no real money ever moves, and it's
// genuinely free: create a free Stripe account (no card required), grab a
// test secret key (starts with sk_test_) from
// https://dashboard.stripe.com/test/apikeys, and set it as STRIPE_SECRET_KEY.
type PaymentClient struct {
	httpClient *http.Client
	secretKey  string
}

func NewPaymentClient(secretKey string) *PaymentClient {
	return &PaymentClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		secretKey:  secretKey,
	}
}

// testPaymentMethod is one of Stripe's own published test tokens (always
// succeeds, never a real card). Not a secret — Stripe documents this
// publicly: https://stripe.com/docs/testing. Using it lets the whole
// authorize -> capture flow run via plain server-to-server API calls, since
// this project has no frontend checkout form (Stripe Elements) collecting
// real card details.
const testPaymentMethod = "pm_card_visa"

type PaymentIntentResult struct {
	ID     string
	Status string // e.g. "requires_capture", "succeeded", "canceled"
}

type stripePaymentIntentResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type stripeErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *PaymentClient) post(path string, form url.Values) (*PaymentIntentResult, error) {
	req, err := http.NewRequest(
		http.MethodPost,
		"https://api.stripe.com/v1/"+path,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("building stripe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(c.secretKey, "") // Stripe's auth scheme: secret key as HTTP Basic username, empty password

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling stripe API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading stripe response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var stripeErr stripeErrorResponse
		_ = json.Unmarshal(body, &stripeErr)
		if stripeErr.Error.Message != "" {
			return nil, fmt.Errorf("stripe error: %s", stripeErr.Error.Message)
		}
		return nil, fmt.Errorf("stripe API returned status %d", resp.StatusCode)
	}

	var pi stripePaymentIntentResponse
	if err := json.Unmarshal(body, &pi); err != nil {
		return nil, fmt.Errorf("parsing stripe response: %w", err)
	}
	return &PaymentIntentResult{ID: pi.ID, Status: pi.Status}, nil
}

// AuthorizePayment creates and confirms a PaymentIntent for amountUSD, using
// capture_method=manual — this places a HOLD on the (test) payment method
// without capturing funds yet. Mirrors how real freight brokers often
// handle payment: authorize at dispatch, capture at delivery, release the
// hold if the shipment is cancelled first.
func (c *PaymentClient) AuthorizePayment(amountUSD float64, shipmentID string) (*PaymentIntentResult, error) {
	amountCents := int64(math.Round(amountUSD * 100))

	form := url.Values{}
	form.Set("amount", strconv.FormatInt(amountCents, 10))
	form.Set("currency", "usd")
	form.Set("capture_method", "manual")
	form.Set("confirm", "true")
	form.Set("payment_method", testPaymentMethod)
	form.Set("payment_method_types[]", "card")
	form.Set("metadata[shipment_id]", shipmentID)

	return c.post("payment_intents", form)
}

// CapturePayment completes a previously-authorized hold — the actual
// "charge" step, called when a shipment is marked delivered.
func (c *PaymentClient) CapturePayment(paymentIntentID string) (*PaymentIntentResult, error) {
	return c.post("payment_intents/"+paymentIntentID+"/capture", url.Values{})
}

// CancelPayment releases a hold without charging anything — called when a
// dispatched shipment is cancelled before delivery.
func (c *PaymentClient) CancelPayment(paymentIntentID string) (*PaymentIntentResult, error) {
	return c.post("payment_intents/"+paymentIntentID+"/cancel", url.Values{})
}
