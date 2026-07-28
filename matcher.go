package main

import (
	"math"
	"sort"
)

const earthRadiusMi = 3958.8

// haversineDistanceMi returns the great-circle distance in miles between
// two lat/lon points. Standard haversine formula — not an approximation
// via flat-earth Pythagorean distance, which breaks down over longer
// freight routes.
func haversineDistanceMi(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }

	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusMi * c
}

// scoreCarrier produces a MatchResult for a single carrier against a shipment.
//
// Scoring approach (deliberately simple and explainable, not a black box):
//   - Infeasible immediately if the carrier is already booked on another
//     active shipment (IsAvailable=false), or if capacity can't cover the
//     shipment's weight — score 0, Feasible=false, still returned so the
//     caller can see it was considered and why it was excluded.
//   - Otherwise, score is inversely proportional to distance: closer
//     carriers score higher. Capped distance influence at 500mi so a
//     carrier 2,000mi away doesn't score meaningfully different from one
//     600mi away — both are "far," and finer-grained scoring there isn't
//     useful without also weighing e.g. carrier reliability or appointment
//     availability (explicitly out of scope for this project; see README).
//   - EstimatedPriceUSD is a placeholder formula (pricing.go), included so
//     a price is visible before dispatch, not a claim about real market rates.
func scoreCarrier(carrier Carrier, shipment Shipment) MatchResult {
	result := MatchResult{
		CarrierID:   carrier.ID,
		CarrierName: carrier.Name,
	}

	if !carrier.IsAvailable || carrier.CapacityLbs < shipment.WeightLbs {
		result.Feasible = false
		result.Score = 0
		return result
	}

	dist := haversineDistanceMi(
		carrier.Lat, carrier.Lon,
		shipment.OriginLat, shipment.OriginLon,
	)
	result.DistanceMi = math.Round(dist*10) / 10
	result.Feasible = true
	result.EstimatedPriceUSD = estimatePriceUSD(dist, shipment.WeightLbs)

	cappedDist := math.Min(dist, 500)
	result.Score = math.Round((1 - cappedDist/500) * 1000) / 10 // 0-100 scale, 1 decimal

	return result
}

// RankCarriers scores every carrier against a shipment and returns them
// sorted best-match first (feasible + highest score first, infeasible last).
func RankCarriers(carriers []Carrier, shipment Shipment) []MatchResult {
	results := make([]MatchResult, 0, len(carriers))
	for _, c := range carriers {
		results = append(results, scoreCarrier(c, shipment))
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Feasible != results[j].Feasible {
			return results[i].Feasible // feasible carriers sort first
		}
		return results[i].Score > results[j].Score
	})

	return results
}
