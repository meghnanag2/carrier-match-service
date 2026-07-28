package main

import (
	"math"
	"testing"
)

// Known reference distance: JFK Airport (NYC) to LAX (LA) is ~2,475 miles
// great-circle. Used to sanity-check the haversine implementation against
// a real-world figure rather than only self-consistent unit values.
func TestHaversineDistance_KnownRoute(t *testing.T) {
	jfkLat, jfkLon := 40.6413, -73.7781
	laxLat, laxLon := 33.9416, -118.4085

	got := haversineDistanceMi(jfkLat, jfkLon, laxLat, laxLon)
	want := 2475.0
	tolerance := 25.0 // miles — haversine assumes a perfect sphere, so expect small drift

	if math.Abs(got-want) > tolerance {
		t.Errorf("haversineDistanceMi(JFK, LAX) = %.1f, want ~%.1f (+/- %.0f)", got, want, tolerance)
	}
}

func TestHaversineDistance_SamePoint(t *testing.T) {
	got := haversineDistanceMi(40.0, -75.0, 40.0, -75.0)
	if got != 0 {
		t.Errorf("distance between identical points = %.4f, want 0", got)
	}
}

func TestScoreCarrier_InfeasibleWhenUnderCapacity(t *testing.T) {
	carrier := Carrier{ID: "c1", Name: "Test Carrier", Lat: 40.0, Lon: -75.0, CapacityLbs: 5000, IsAvailable: true}
	shipment := Shipment{OriginLat: 40.0, OriginLon: -75.0, WeightLbs: 10000}

	result := scoreCarrier(carrier, shipment)

	if result.Feasible {
		t.Error("expected Feasible=false when carrier capacity < shipment weight")
	}
	if result.Score != 0 {
		t.Errorf("expected Score=0 for infeasible match, got %.1f", result.Score)
	}
}

func TestScoreCarrier_InfeasibleWhenAlreadyBooked(t *testing.T) {
	carrier := Carrier{ID: "c1", Name: "Test Carrier", Lat: 40.0, Lon: -75.0, CapacityLbs: 50000, IsAvailable: false}
	shipment := Shipment{OriginLat: 40.0, OriginLon: -75.0, WeightLbs: 1000}

	result := scoreCarrier(carrier, shipment)

	if result.Feasible {
		t.Error("expected Feasible=false for a carrier that's already booked (IsAvailable=false), even with plenty of capacity")
	}
}

func TestScoreCarrier_CloserCarrierScoresHigher(t *testing.T) {
	shipment := Shipment{OriginLat: 40.0, OriginLon: -75.0, WeightLbs: 1000}

	near := Carrier{ID: "near", Lat: 40.1, Lon: -75.1, CapacityLbs: 5000, IsAvailable: true}
	far := Carrier{ID: "far", Lat: 45.0, Lon: -80.0, CapacityLbs: 5000, IsAvailable: true}

	nearResult := scoreCarrier(near, shipment)
	farResult := scoreCarrier(far, shipment)

	if nearResult.Score <= farResult.Score {
		t.Errorf("expected nearer carrier to score higher: near=%.1f far=%.1f",
			nearResult.Score, farResult.Score)
	}
}

func TestRankCarriers_FeasibleSortedBeforeInfeasible(t *testing.T) {
	shipment := Shipment{OriginLat: 40.0, OriginLon: -75.0, WeightLbs: 10000}
	carriers := []Carrier{
		{ID: "too-small", Lat: 40.0, Lon: -75.0, CapacityLbs: 5000, IsAvailable: true},   // infeasible (capacity)
		{ID: "big-enough", Lat: 41.0, Lon: -76.0, CapacityLbs: 20000, IsAvailable: true}, // feasible
	}

	ranked := RankCarriers(carriers, shipment)

	if len(ranked) != 2 {
		t.Fatalf("expected 2 results, got %d", len(ranked))
	}
	if !ranked[0].Feasible {
		t.Error("expected feasible carrier ranked first")
	}
	if ranked[0].CarrierID != "big-enough" {
		t.Errorf("expected 'big-enough' ranked first, got %q", ranked[0].CarrierID)
	}
}
