package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type Server struct {
	store    Store
	geocoder *Geocoder
	workers  *MatchWorkerPool
	payments *PaymentClient // nil if STRIPE_SECRET_KEY isn't configured — payment steps are skipped, not fatal
}

func NewServer(store Store, geocoder *Geocoder, workers *MatchWorkerPool, payments *PaymentClient) *Server {
	return &Server{store: store, geocoder: geocoder, workers: workers, payments: payments}
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
		IsAvailable: true,
	}
	_ = s.store.SaveCarrier(carrier)
	writeJSON(w, http.StatusCreated, carrier)
}

func (s *Server) handleListCarriers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListCarriers())
}

// POST /shippers  { "name": "...", "email": "..." }
func (s *Server) handleCreateShipper(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Email == "" {
		writeError(w, http.StatusBadRequest, "name and email are required")
		return
	}

	shipper := Shipper{ID: newID(), Name: req.Name, Email: req.Email}
	_ = s.store.SaveShipper(shipper)
	writeJSON(w, http.StatusCreated, shipper)
}

// POST /shipments  { "shipper_id": "...", "origin_address": "...", "weight_lbs": 12000 }
func (s *Server) handleCreateShipment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ShipperID     string  `json:"shipper_id"`
		OriginAddress string  `json:"origin_address"`
		WeightLbs     float64 `json:"weight_lbs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ShipperID == "" {
		writeError(w, http.StatusBadRequest, "shipper_id is required — register a shipper first via POST /shippers")
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
		ShipperID:     req.ShipperID,
		OriginAddress: req.OriginAddress,
		OriginLat:     lat,
		OriginLon:     lon,
		WeightLbs:     req.WeightLbs,
		CreatedAt:     time.Now(),
		Status:        StatusPending,
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
		"status":                "ok",
		"geocode_cache_hits":    hits,
		"geocode_cache_misses":  misses,
	})
}

// POST /shipments/{id}/dispatch  { "carrier_id": "...", "price_usd": 250.00 (optional) }
//
// The real "accept the load" step that was previously missing — matches
// only ranked carriers, this actually books one. If price_usd is omitted,
// the price is computed the same way match results price them
// (pricing.go), so a client can dispatch straight off a /matches result
// without needing to compute or guess a number itself.
func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	shipmentID := r.PathValue("id")

	var req struct {
		CarrierID string   `json:"carrier_id"`
		PriceUSD  *float64 `json:"price_usd"` // pointer: nil means "not provided, compute it"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CarrierID == "" {
		writeError(w, http.StatusBadRequest, "carrier_id is required")
		return
	}

	shipment, err := s.store.GetShipment(shipmentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "shipment not found")
		return
	}
	if err := validateTransition(shipment.Status, StatusDispatched); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	carrier, err := s.store.GetCarrier(req.CarrierID)
	if err != nil {
		writeError(w, http.StatusNotFound, "carrier not found")
		return
	}

	price := req.PriceUSD
	if price == nil {
		dist := haversineDistanceMi(carrier.Lat, carrier.Lon, shipment.OriginLat, shipment.OriginLon)
		computed := estimatePriceUSD(dist, shipment.WeightLbs)
		price = &computed
	}

	updated, err := s.store.Dispatch(shipmentID, req.CarrierID, *price)
	if err == ErrCarrierUnavailable {
		writeError(w, http.StatusConflict, "carrier is no longer available — it may have just been booked by another request")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "dispatch failed: "+err.Error())
		return
	}

	// Payment authorization is a deliberately separate, best-effort step —
	// NOT inside the same transaction as Dispatch(). The atomic carrier
	// booking above is the correctness-critical guarantee (no double-
	// booking); if the Stripe call below fails, the shipment stays validly
	// dispatched, just without a payment hold recorded. A stricter design
	// could roll back the dispatch on payment failure — not done here,
	// since "carrier booked but payment pending retry" is arguably the more
	// realistic real-world behavior than un-booking a carrier because a
	// payment API had a transient failure.
	if s.payments != nil {
		result, err := s.payments.AuthorizePayment(*price, shipmentID)
		if err != nil {
			// Logged, not fatal to the response — the dispatch itself
			// already succeeded and was already committed to the database.
			log.Printf("payment authorization failed for shipment %s: %v", shipmentID, err)
		} else {
			_ = s.store.UpdateShipmentPayment(shipmentID, result.ID, result.Status)
			updated.PaymentIntentID = result.ID
			updated.PaymentStatus = result.Status
		}
	}

	writeJSON(w, http.StatusOK, updated)
}

// PATCH /shipments/{id}/status  { "status": "in_transit" }
// Basic tracking: moves a dispatched shipment through in_transit ->
// delivered (or cancelled), rejecting any transition not explicitly
// allowed by statemachine.go.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	shipmentID := r.PathValue("id")

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	shipment, err := s.store.GetShipment(shipmentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "shipment not found")
		return
	}

	if err := validateTransition(shipment.Status, req.Status); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	if err := s.store.UpdateShipmentStatus(shipmentID, req.Status); err != nil {
		writeError(w, http.StatusInternalServerError, "status update failed: "+err.Error())
		return
	}

	// Payment follow-through, tied to the same real-world events that
	// trigger it: delivery captures the hold (the actual charge), a
	// cancellation after dispatch releases it. Same best-effort-not-fatal
	// reasoning as the authorization step in handleDispatch — a Stripe
	// hiccup here shouldn't block the status update that already succeeded.
	if s.payments != nil && shipment.PaymentIntentID != "" {
		var result *PaymentIntentResult
		var payErr error

		switch req.Status {
		case StatusDelivered:
			result, payErr = s.payments.CapturePayment(shipment.PaymentIntentID)
		case StatusCancelled:
			result, payErr = s.payments.CancelPayment(shipment.PaymentIntentID)
		}

		if payErr != nil {
			log.Printf("payment follow-through failed for shipment %s (status %s): %v", shipmentID, req.Status, payErr)
		} else if result != nil {
			_ = s.store.UpdateShipmentPayment(shipmentID, result.ID, result.Status)
		}
	}

	// Free the carrier back up on cancellation — the bug the cancellation
	// flow diagram in README.md surfaced: without this, a cancelled
	// shipment's carrier stayed permanently marked unavailable. Only
	// relevant if the shipment had actually been dispatched to a carrier
	// (CarrierID is empty for a shipment cancelled while still "pending").
	if req.Status == StatusCancelled && shipment.CarrierID != "" {
		if err := s.store.ReleaseCarrier(shipment.CarrierID); err != nil {
			log.Printf("failed to release carrier %s for cancelled shipment %s: %v", shipment.CarrierID, shipmentID, err)
		}
	}

	updated, _ := s.store.GetShipment(shipmentID)
	writeJSON(w, http.StatusOK, updated)
}
