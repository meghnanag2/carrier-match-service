package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no cgo/C compiler required
)

// SQLiteStore is a persistent, file-backed implementation of the Store
// interface (see store.go). Data survives a process restart — it's a real
// file on disk (e.g. "carrier_match.db"), not in-memory.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database file at dbPath and
// ensures the schema exists, including adding any columns that were added
// to this project after a database file was first created (see
// addColumnIfMissing below) — so upgrading doesn't require deleting your
// existing carrier_match.db.
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

	CREATE TABLE IF NOT EXISTS shippers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		email TEXT NOT NULL
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
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Columns added after the original schema (see TECHNICAL_WALKTHROUGH.md
	// for why this project doesn't use a real migrations tool): applied as
	// best-effort ALTER TABLE statements, tolerating the "duplicate column"
	// error on a database that already has them. This is a lightweight
	// substitute for real migration tooling (golang-migrate/goose), not a
	// replacement for it — fine for one project-scale SQLite file, not
	// something to rely on for anything with real schema history.
	lateColumns := []struct{ table, ddl string }{
		{"carriers", "ALTER TABLE carriers ADD COLUMN is_available INTEGER NOT NULL DEFAULT 1"},
		{"shipments", "ALTER TABLE shipments ADD COLUMN shipper_id TEXT NOT NULL DEFAULT ''"},
		{"shipments", "ALTER TABLE shipments ADD COLUMN carrier_id TEXT NOT NULL DEFAULT ''"},
		{"shipments", "ALTER TABLE shipments ADD COLUMN agreed_price_usd REAL NOT NULL DEFAULT 0"},
		{"shipments", "ALTER TABLE shipments ADD COLUMN payment_intent_id TEXT NOT NULL DEFAULT ''"},
		{"shipments", "ALTER TABLE shipments ADD COLUMN payment_status TEXT NOT NULL DEFAULT ''"},
	}
	for _, col := range lateColumns {
		if _, err := s.db.Exec(col.ddl); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue // already applied on a previous run — fine
			}
			return fmt.Errorf("adding column (%s): %w", col.ddl, err)
		}
	}
	return nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) SaveCarrier(c Carrier) error {
	_, err := s.db.Exec(
		`INSERT INTO carriers (id, name, address, lat, lon, capacity_lbs, is_available)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name=excluded.name, address=excluded.address,
		   lat=excluded.lat, lon=excluded.lon, capacity_lbs=excluded.capacity_lbs`,
		c.ID, c.Name, c.Address, c.Lat, c.Lon, c.CapacityLbs, boolToInt(c.IsAvailable),
	)
	if err != nil {
		return fmt.Errorf("saving carrier: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListCarriers() []Carrier {
	rows, err := s.db.Query(`SELECT id, name, address, lat, lon, capacity_lbs, is_available FROM carriers`)
	if err != nil {
		return []Carrier{}
	}
	defer rows.Close()

	var carriers []Carrier
	for rows.Next() {
		var c Carrier
		var isAvailable int
		if err := rows.Scan(&c.ID, &c.Name, &c.Address, &c.Lat, &c.Lon, &c.CapacityLbs, &isAvailable); err != nil {
			continue
		}
		c.IsAvailable = isAvailable != 0
		carriers = append(carriers, c)
	}
	return carriers
}

func (s *SQLiteStore) GetCarrier(id string) (Carrier, error) {
	var c Carrier
	var isAvailable int
	row := s.db.QueryRow(
		`SELECT id, name, address, lat, lon, capacity_lbs, is_available FROM carriers WHERE id = ?`, id,
	)
	err := row.Scan(&c.ID, &c.Name, &c.Address, &c.Lat, &c.Lon, &c.CapacityLbs, &isAvailable)
	if err == sql.ErrNoRows {
		return Carrier{}, ErrNotFound
	}
	if err != nil {
		return Carrier{}, fmt.Errorf("querying carrier: %w", err)
	}
	c.IsAvailable = isAvailable != 0
	return c, nil
}

func (s *SQLiteStore) SaveShipper(sh Shipper) error {
	_, err := s.db.Exec(
		`INSERT INTO shippers (id, name, email) VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, email=excluded.email`,
		sh.ID, sh.Name, sh.Email,
	)
	if err != nil {
		return fmt.Errorf("saving shipper: %w", err)
	}
	return nil
}

func (s *SQLiteStore) SaveShipment(sh Shipment) error {
	createdAt, err := sh.CreatedAt.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshaling created_at: %w", err)
	}

	_, err = s.db.Exec(
		`INSERT INTO shipments
		   (id, shipper_id, origin_address, origin_lat, origin_lon, weight_lbs, status, created_at,
		    carrier_id, agreed_price_usd, payment_intent_id, payment_status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   origin_address=excluded.origin_address, origin_lat=excluded.origin_lat,
		   origin_lon=excluded.origin_lon, weight_lbs=excluded.weight_lbs,
		   status=excluded.status`,
		sh.ID, sh.ShipperID, sh.OriginAddress, sh.OriginLat, sh.OriginLon, sh.WeightLbs,
		sh.Status, string(createdAt), sh.CarrierID, sh.AgreedPriceUSD,
		sh.PaymentIntentID, sh.PaymentStatus,
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
		`SELECT id, shipper_id, origin_address, origin_lat, origin_lon, weight_lbs,
		        status, created_at, carrier_id, agreed_price_usd, payment_intent_id, payment_status
		 FROM shipments WHERE id = ?`, id,
	)
	err := row.Scan(
		&sh.ID, &sh.ShipperID, &sh.OriginAddress, &sh.OriginLat, &sh.OriginLon, &sh.WeightLbs,
		&sh.Status, &createdAtRaw, &sh.CarrierID, &sh.AgreedPriceUSD,
		&sh.PaymentIntentID, &sh.PaymentStatus,
	)
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

// Dispatch is the operation that actually prevents double-booking.
//
// The core of it: `UPDATE carriers SET is_available = 0 WHERE id = ? AND
// is_available = 1`. If two concurrent requests both try to dispatch the
// same carrier, only ONE of those UPDATE statements can affect a row —
// the second one runs against a carrier that's already is_available = 0
// (because the first one already flipped it), so its WHERE clause matches
// zero rows. Checking RowsAffected() after the UPDATE is how the code
// tells "I won the race" from "I lost it" — this is a real, standard
// optimistic-concurrency pattern (a conditional update, not a full
// transaction lock), and it works correctly here because SQLite serializes
// writers at the database-file level regardless.
func (s *SQLiteStore) Dispatch(shipmentID, carrierID string, priceUSD float64) (Shipment, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Shipment{}, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() // no-op if Commit() already succeeded

	result, err := tx.Exec(
		`UPDATE carriers SET is_available = 0 WHERE id = ? AND is_available = 1`,
		carrierID,
	)
	if err != nil {
		return Shipment{}, fmt.Errorf("booking carrier: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Shipment{}, fmt.Errorf("checking booking result: %w", err)
	}
	if rowsAffected == 0 {
		// Either the carrier doesn't exist, or (the interesting case) it
		// was already booked — either by a previous dispatch or by a
		// concurrent request that won the race.
		return Shipment{}, ErrCarrierUnavailable
	}

	_, err = tx.Exec(
		`UPDATE shipments SET carrier_id = ?, agreed_price_usd = ?, status = ? WHERE id = ?`,
		carrierID, priceUSD, StatusDispatched, shipmentID,
	)
	if err != nil {
		return Shipment{}, fmt.Errorf("updating shipment: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Shipment{}, fmt.Errorf("committing dispatch: %w", err)
	}

	return s.GetShipment(shipmentID)
}

// UpdateShipmentPayment records the outcome of a Stripe API call (payment.go)
// against a shipment. A plain UPDATE, not wrapped in the same transaction as
// Dispatch — see README for why payment authorization is deliberately a
// separate, best-effort step rather than folded into the atomic booking
// guarantee.
func (s *SQLiteStore) UpdateShipmentPayment(shipmentID, paymentIntentID, paymentStatus string) error {
	result, err := s.db.Exec(
		`UPDATE shipments SET payment_intent_id = ?, payment_status = ? WHERE id = ?`,
		paymentIntentID, paymentStatus, shipmentID,
	)
	if err != nil {
		return fmt.Errorf("updating shipment payment: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking payment update result: %w", err)
	}
	if rowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
