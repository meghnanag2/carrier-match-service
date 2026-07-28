package main

import (
	"encoding/json"
	"net/http"
	"time"
)

type Server struct {
	store    Store
	geocoder *Geocoder
	workers  *MatchWorkerPool
}

func NewServer(store Store, geocoder *Geocoder, workers *MatchWorkerPool) *Server {
	return &Server{store: store, geocoder: geocoder, workers: workers}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// POST /carriers  { "name": "...", "address": "...", "capacity_lbs": 40000 }
// Geocodes the address via the real Nominatim API call before storing.
func (s *Server) handleCreateCarrier(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string  `json:"name"`
		Address     string  `json:"address"`
		CapacityLbs float64 `json:"capacity_lbs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Address == "" {
		writeError(w, http.StatusBadRequest, "name and address are required")
		return
	}

	lat, lon, err := s.geocoder.GeocodeAddress(req.Address)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not geocode address: "+err.Error())
		return
	}

	carrier := Carrier{
		ID:          newID(),
		Name:        req.Name,
		Address:     req.Address,
		Lat:         lat,
		Lon:         lon,
		CapacityLbs: req.CapacityLbs,
	}
	_ = s.store.SaveCarrier(carrier)
	writeJSON(w, http.StatusCreated, carrier)
}

func (s *Server) handleListCarriers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListCarriers())
}

// POST /shipments  { "origin_address": "...", "weight_lbs": 12000 }
func (s *Server) handleCreateShipment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OriginAddress string  `json:"origin_address"`
		WeightLbs     float64 `json:"weight_lbs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.OriginAddress == "" {
		writeError(w, http.StatusBadRequest, "origin_address is required")
		return
	}

	lat, lon, err := s.geocoder.GeocodeAddress(req.OriginAddress)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not geocode address: "+err.Error())
		return
	}

	shipment := Shipment{
		ID:            newID(),
		OriginAddress: req.OriginAddress,
		OriginLat:     lat,
		OriginLon:     lon,
		WeightLbs:     req.WeightLbs,
		CreatedAt:     time.Now(),
		Status:        "pending",
	}
	_ = s.store.SaveShipment(shipment)
	writeJSON(w, http.StatusCreated, shipment)
}

// GET /shipments/{id}/matches
// Submits an async match job to the worker pool and blocks (with a timeout)
// for the result — demonstrates the background-job path end-to-end while
// still giving the HTTP caller a synchronous response.
func (s *Server) handleGetMatches(w http.ResponseWriter, r *http.Request) {
	shipmentID := r.PathValue("id")

	if _, err := s.store.GetShipment(shipmentID); err != nil {
		writeError(w, http.StatusNotFound, "shipment not found")
		return
	}

	resultCh := s.workers.Submit(shipmentID)

	select {
	case results := <-resultCh:
		writeJSON(w, http.StatusOK, results)
	case <-time.After(15 * time.Second):
		writeError(w, http.StatusGatewayTimeout, "matching timed out")
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	hits, misses := s.geocoder.cache.Stats()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":            "ok",
		"geocode_cache_hits":   hits,
		"geocode_cache_misses": misses,
	})
}
