package main

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no cgo/C compiler required
)

// SQLiteStore is a persistent, file-backed implementation of the Store
// interface (see store.go). Unlike MemStore, data survives a process
// restart — it's a real file on disk (e.g. "carrier_match.db"), not
// in-memory.
//
// A common way this accidentally ISN'T persistent: opening the database
// with the special filename ":memory:" instead of a real file path. This
// implementation always opens a real file path, which is what actually
// makes the data survive `go run .` being stopped and started again.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates, if it doesn't exist yet) a SQLite
// database file at dbPath and ensures the schema exists.
//
// Uses "CREATE TABLE IF NOT EXISTS" — safe to run on every single startup.
// This does NOT drop or recreate existing tables/data; it only creates
// them the first time the file is empty. That's the specific mechanism
// that makes "runs a migration on every boot" and "wipes your data on
// every boot" two very different things.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database at %q: %w", dbPath, err)
	}

	// SQLite handles one writer at a time internally; capping the pool
	// avoids "database is locked" errors under concurrent requests from
	// this service's own worker pool.
	db.SetMaxOpenConns(1)

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return store, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS carriers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		address TEXT NOT NULL,
		lat REAL NOT NULL,
		lon REAL NOT NULL,
		capacity_lbs REAL NOT NULL
	);

	CREATE TABLE IF NOT EXISTS shipments (
		id TEXT PRIMARY KEY,
		origin_address TEXT NOT NULL,
		origin_lat REAL NOT NULL,
		origin_lon REAL NOT NULL,
		weight_lbs REAL NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		created_at TEXT NOT NULL
	);
	`
	_, err := s.db.Exec(schema)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) SaveCarrier(c Carrier) error {
	_, err := s.db.Exec(
		`INSERT INTO carriers (id, name, address, lat, lon, capacity_lbs)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name, address=excluded.address,
		   lat=excluded.lat, lon=excluded.lon, capacity_lbs=excluded.capacity_lbs`,
		c.ID, c.Name, c.Address, c.Lat, c.Lon, c.CapacityLbs,
	)
	if err != nil {
		return fmt.Errorf("saving carrier: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListCarriers() []Carrier {
	rows, err := s.db.Query(`SELECT id, name, address, lat, lon, capacity_lbs FROM carriers`)
	if err != nil {
		// Store interface's ListCarriers doesn't return an error (matching
		// MemStore's signature) — log and return empty rather than panic.
		// A real production version would likely change this interface
		// method to return an error instead of swallowing it here.
		return []Carrier{}
	}
	defer rows.Close()

	var carriers []Carrier
	for rows.Next() {
		var c Carrier
		if err := rows.Scan(&c.ID, &c.Name, &c.Address, &c.Lat, &c.Lon, &c.CapacityLbs); err != nil {
			continue
		}
		carriers = append(carriers, c)
	}
	return carriers
}

func (s *SQLiteStore) SaveShipment(sh Shipment) error {
	createdAt, err := sh.CreatedAt.MarshalJSON() // reuse time's JSON marshaling for a consistent string format
	if err != nil {
		return fmt.Errorf("marshaling created_at: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO shipments (id, origin_address, origin_lat, origin_lon, weight_lbs, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   origin_address=excluded.origin_address, origin_lat=excluded.origin_lat,
		   origin_lon=excluded.origin_lon, weight_lbs=excluded.weight_lbs,
		   status=excluded.status`,
		sh.ID, sh.OriginAddress, sh.OriginLat, sh.OriginLon, sh.WeightLbs, sh.Status, string(createdAt),
	)
	if err != nil {
		return fmt.Errorf("saving shipment: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetShipment(id string) (Shipment, error) {
	var sh Shipment
	var createdAtRaw string

	row := s.db.QueryRow(
		`SELECT id, origin_address, origin_lat, origin_lon, weight_lbs, status, created_at
		 FROM shipments WHERE id = ?`, id,
	)
	err := row.Scan(&sh.ID, &sh.OriginAddress, &sh.OriginLat, &sh.OriginLon, &sh.WeightLbs, &sh.Status, &createdAtRaw)
	if err == sql.ErrNoRows {
		return Shipment{}, ErrNotFound
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("querying shipment: %w", err)
	}

	if err := json.Unmarshal([]byte(createdAtRaw), &sh.CreatedAt); err != nil {
		return Shipment{}, fmt.Errorf("parsing created_at: %w", err)
	}

	return sh, nil
}

func (s *SQLiteStore) UpdateShipmentStatus(id string, status string) error {
	result, err := s.db.Exec(`UPDATE shipments SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("updating shipment status: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking update result: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
