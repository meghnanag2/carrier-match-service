package main

import "time"

// Carrier represents a freight carrier available to accept shipments.
type Carrier struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Address     string  `json:"address"`      // raw address, geocoded on registration
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	CapacityLbs float64 `json:"capacity_lbs"` // max weight this carrier can take
}

// Shipment represents a load that needs to be matched to a carrier.
type Shipment struct {
	ID              string    `json:"id"`
	OriginAddress   string    `json:"origin_address"`
	OriginLat       float64   `json:"origin_lat"`
	OriginLon       float64   `json:"origin_lon"`
	WeightLbs       float64   `json:"weight_lbs"`
	CreatedAt       time.Time `json:"created_at"`
	Status          string    `json:"status"` // "pending", "matching", "matched", "expired"
}

// MatchResult scores a single carrier against a shipment.
// Mirrors the JD's "scoring assignments" and "expiration of stale offers" language:
// a low-scoring or unfulfillable match is still returned (with Feasible=false)
// so the caller can see *why* a carrier was ranked where it was.
type MatchResult struct {
	CarrierID   string  `json:"carrier_id"`
	CarrierName string  `json:"carrier_name"`
	DistanceMi  float64 `json:"distance_mi"`
	Feasible    bool    `json:"feasible"` // false if capacity can't cover the shipment
	Score       float64 `json:"score"`    // higher is better; 0 if infeasible
}
