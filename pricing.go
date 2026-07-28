package main

import "math"

// Pricing here is a deliberately simple, explicit formula — NOT real
// freight market rates. Actual freight pricing depends on fuel costs,
// lane-specific supply/demand, accessorial fees, carrier-specific
// contracts, and market indices that change daily. Faking sophistication
// here would be worse than being plain about it: this is a placeholder
// that makes the "agree on a price before dispatch" step real and
// end-to-end testable, not a claim about knowing freight pricing.
const (
	baseFeeUSD          = 75.00 // flat fee for any dispatched load
	ratePerMileUSD      = 2.10  // illustrative — real per-mile rates vary by lane, season, fuel prices
	ratePerHundredLbsUSD = 0.15 // illustrative weight-based component
)

// estimatePriceUSD computes a placeholder price for moving a shipment of
// the given weight over the given distance.
func estimatePriceUSD(distanceMi float64, weightLbs float64) float64 {
	price := baseFeeUSD + (distanceMi * ratePerMileUSD) + (weightLbs / 100 * ratePerHundredLbsUSD)
	return math.Round(price*100) / 100 // round to cents
}
