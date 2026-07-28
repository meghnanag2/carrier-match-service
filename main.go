package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "carrier_match.db"
	}

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer store.Close()
	log.Printf("using SQLite database at %s (data persists across restarts)", dbPath)

	geocoder := NewGeocoder()
	workers := NewMatchWorkerPool(store, 4 /* workers */, 100 /* queue size */)
	server := NewServer(store, geocoder, workers)

	mux := http.NewServeMux()
	// Go 1.22+ ServeMux supports method + path-variable patterns natively —
	// no third-party router (chi/gorilla) needed for a service this size.
	mux.HandleFunc("GET /health", server.handleHealth)
	mux.HandleFunc("POST /carriers", server.handleCreateCarrier)
	mux.HandleFunc("GET /carriers", server.handleListCarriers)
	mux.HandleFunc("POST /shipments", server.handleCreateShipment)
	mux.HandleFunc("GET /shipments/{id}/matches", server.handleGetMatches)

	addr := ":8080"
	log.Printf("carrier-match-service listening on %s", addr)
	if err := http.ListenAndServe(addr, withCORS(mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
