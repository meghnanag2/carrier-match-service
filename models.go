package main

import "time"

// Shipper represents the business requesting a shipment — previously
// missing from this project entirely. Every shipment now belongs to one.
type Shipper struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Carrier represents a freight carrier available to accept shipments.
type Carrier struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Address     string  `json:"address"`      // raw address, geocoded on registration
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	CapacityLbs float64 `json:"capacity_lbs"` // max weight this carrier can take
	IsAvailable bool    `json:"is_available"` // false once booked on an active shipment
}

// Shipment represents a load that needs to be matched to a carrier.
//
// ShipperID, CarrierID, and AgreedPriceUSD are the pieces that were
// previously missing — a shipment now has an owner, and once dispatched,
// a specific assigned carrier and an agreed price, not just a status string
// with nothing backing it.
type Shipment struct {
	ID              string    `json:"id"`
	ShipperID       string    `json:"shipper_id"`
	OriginAddress   string    `json:"origin_address"`
	OriginLat       float64   `json:"origin_lat"`
	OriginLon       float64   `json:"origin_lon"`
	WeightLbs       float64   `json:"weight_lbs"`
	CreatedAt       time.Time `json:"created_at"`
	Status          string    `json:"status"` // see statemachine.go for the full set + allowed transitions

	// Populated only once the shipment is dispatched (empty string / 0 before that).
	CarrierID       string  `json:"carrier_id,omitempty"`
	AgreedPriceUSD  float64 `json:"agreed_price_usd,omitempty"`

	// Populated once payment is authorized (at dispatch) — see payment.go.
	// PaymentStatus mirrors Stripe's own PaymentIntent status values
	// (e.g. "requires_capture", "succeeded", "canceled") rather than
	// inventing a separate vocabulary, so it's directly comparable to what
	// you'd see in the Stripe dashboard.
	PaymentIntentID string `json:"payment_intent_id,omitempty"`
	PaymentStatus   string `json:"payment_status,omitempty"`
}

// MatchResult scores a single carrier against a shipment.
// Mirrors the JD's "scoring assignments" and "expiration of stale offers" language:
// a low-scoring or unfulfillable match is still returned (with Feasible=false)
// so the caller can see *why* a carrier was ranked where it was.
type MatchResult struct {
	CarrierID     string  `json:"carrier_id"`
	CarrierName   string  `json:"carrier_name"`
	DistanceMi    float64 `json:"distance_mi"`
	Feasible      bool    `json:"feasible"`        // false if capacity can't cover the shipment, or carrier is already booked
	Score         float64 `json:"score"`           // higher is better; 0 if infeasible
	EstimatedPriceUSD float64 `json:"estimated_price_usd"` // see pricing.go — a placeholder formula, not real market rates
}
