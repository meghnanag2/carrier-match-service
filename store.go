package main

import (
	"errors"
	"sync"
)

var ErrNotFound = errors.New("not found")
var ErrCarrierUnavailable = errors.New("carrier is not available")

// Store is the persistence interface the rest of the app depends on.
// Two implementations exist: MemStore (this file, in-memory, disposable)
// and SQLiteStore (sqlite_store.go, persistent). Neither handlers.go nor
// worker.go depend on which one is in use — see main.go for the one line
// that picks.
type Store interface {
	SaveCarrier(c Carrier) error
	ListCarriers() []Carrier
	GetCarrier(id string) (Carrier, error)

	SaveShipper(s Shipper) error

	SaveShipment(s Shipment) error
	GetShipment(id string) (Shipment, error)
	UpdateShipmentStatus(id string, status string) error

	// Dispatch atomically assigns carrierID to shipmentID at the given
	// price: marks the carrier unavailable, sets the shipment's carrier
	// and price, and moves its status to "dispatched" — all as one
	// operation, specifically so two concurrent dispatch requests for the
	// same carrier can't both succeed. Returns ErrCarrierUnavailable if
	// the carrier was already booked (by this call losing a race, or
	// simply already being unavailable).
	Dispatch(shipmentID, carrierID string, priceUSD float64) (Shipment, error)

	// UpdateShipmentPayment records the result of a Stripe API call
	// (payment.go) against a shipment. Deliberately separate from Dispatch:
	// payment authorization is a best-effort external API call that can
	// fail independently of the atomic booking guarantee Dispatch provides
	// — see README for why these aren't in one transaction.
	UpdateShipmentPayment(shipmentID, paymentIntentID, paymentStatus string) error
}

// MemStore is a simple mutex-guarded in-memory store.
// Correctness note: a single sync.RWMutex is coarse-grained on purpose —
// fine enough for this project's scale. It's also what makes Dispatch
// straightforward to implement correctly here: the whole operation runs
// under one Lock(), so "check availability, then book" can't be
// interleaved with another goroutine's dispatch attempt on the same
// carrier — the mutex itself is the concurrency guard, the same role
// SQLite's transaction + WHERE clause play in sqlite_store.go.
type MemStore struct {
	mu        sync.RWMutex
	carriers  map[string]Carrier
	shippers  map[string]Shipper
	shipments map[string]Shipment
}

func NewMemStore() *MemStore {
	return &MemStore{
		carriers:  make(map[string]Carrier),
		shippers:  make(map[string]Shipper),
		shipments: make(map[string]Shipment),
	}
}

func (s *MemStore) SaveCarrier(c Carrier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.carriers[c.ID] = c
	return nil
}

func (s *MemStore) ListCarriers() []Carrier {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Carrier, 0, len(s.carriers))
	for _, c := range s.carriers {
		out = append(out, c)
	}
	return out
}

func (s *MemStore) GetCarrier(id string) (Carrier, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.carriers[id]
	if !ok {
		return Carrier{}, ErrNotFound
	}
	return c, nil
}

func (s *MemStore) SaveShipper(sh Shipper) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shippers[sh.ID] = sh
	return nil
}

func (s *MemStore) SaveShipment(sh Shipment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shipments[sh.ID] = sh
	return nil
}

func (s *MemStore) GetShipment(id string) (Shipment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sh, ok := s.shipments[id]
	if !ok {
		return Shipment{}, ErrNotFound
	}
	return sh, nil
}

func (s *MemStore) UpdateShipmentStatus(id string, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh, ok := s.shipments[id]
	if !ok {
		return ErrNotFound
	}
	sh.Status = status
	s.shipments[id] = sh
	return nil
}

func (s *MemStore) Dispatch(shipmentID, carrierID string, priceUSD float64) (Shipment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sh, ok := s.shipments[shipmentID]
	if !ok {
		return Shipment{}, ErrNotFound
	}
	carrier, ok := s.carriers[carrierID]
	if !ok {
		return Shipment{}, ErrNotFound
	}
	if !carrier.IsAvailable {
		return Shipment{}, ErrCarrierUnavailable
	}

	carrier.IsAvailable = false
	s.carriers[carrierID] = carrier

	sh.CarrierID = carrierID
	sh.AgreedPriceUSD = priceUSD
	sh.Status = StatusDispatched
	s.shipments[shipmentID] = sh

	return sh, nil
}

func (s *MemStore) UpdateShipmentPayment(shipmentID, paymentIntentID, paymentStatus string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sh, ok := s.shipments[shipmentID]
	if !ok {
		return ErrNotFound
	}
	sh.PaymentIntentID = paymentIntentID
	sh.PaymentStatus = paymentStatus
	s.shipments[shipmentID] = sh
	return nil
}
