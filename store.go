package main

import (
	"errors"
	"sync"
)

var ErrNotFound = errors.New("not found")

// Store is the persistence interface the rest of the app depends on.
// This project ships an in-memory implementation (MemStore) so it runs
// with zero external dependencies. Swapping in a Postgres-backed store
// (via database/sql + pgx, or sqlx) means implementing this same interface
// against real tables — the handlers and matcher never need to change.
type Store interface {
	SaveCarrier(c Carrier) error
	ListCarriers() []Carrier

	SaveShipment(s Shipment) error
	GetShipment(id string) (Shipment, error)
	UpdateShipmentStatus(id string, status string) error
}

// MemStore is a simple mutex-guarded in-memory store.
// Correctness note: a single sync.RWMutex is coarse-grained on purpose —
// fine enough for this project's scale, but the first thing to revisit
// (e.g. per-shard locking, or just moving to Postgres row locks) if this
// were handling real concurrent write volume.
type MemStore struct {
	mu        sync.RWMutex
	carriers  map[string]Carrier
	shipments map[string]Shipment
}

func NewMemStore() *MemStore {
	return &MemStore{
		carriers:  make(map[string]Carrier),
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
