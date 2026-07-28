package main

import "fmt"

// Shipment lifecycle. Previously (before this file existed), Status was
// just a string anyone could set to anything — "delivered" could silently
// go back to "pending" with nothing stopping it. allowedTransitions makes
// illegal transitions an explicit, rejected error instead.
const (
	StatusPending    = "pending"
	StatusDispatched = "dispatched"
	StatusInTransit  = "in_transit"
	StatusDelivered  = "delivered"
	StatusCancelled  = "cancelled"
	StatusNoMatch    = "no_match"
)

var allowedTransitions = map[string][]string{
	StatusPending:    {StatusDispatched, StatusNoMatch},
	StatusDispatched: {StatusInTransit, StatusCancelled},
	StatusInTransit:  {StatusDelivered, StatusCancelled},
	StatusDelivered:  {}, // terminal
	StatusCancelled:  {}, // terminal
	StatusNoMatch:    {}, // terminal
}

type ErrInvalidTransition struct {
	From string
	To   string
}

func (e *ErrInvalidTransition) Error() string {
	return fmt.Sprintf("cannot transition shipment from %q to %q", e.From, e.To)
}

// validateTransition returns an error if moving from `from` to `to` isn't
// an allowed transition. Unknown source statuses are treated as invalid
// (fails closed, not open).
func validateTransition(from, to string) error {
	allowed, ok := allowedTransitions[from]
	if !ok {
		return &ErrInvalidTransition{From: from, To: to}
	}
	for _, a := range allowed {
		if a == to {
			return nil
		}
	}
	return &ErrInvalidTransition{From: from, To: to}
}
