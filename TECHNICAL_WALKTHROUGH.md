# carrier-match-service — Full Technical Walkthrough

A study guide for the project, not marketing copy. Every feature listed here
is something actually in the code, with the exact file/line context and the
real reason it's there — written so you could explain any single line of
this project if asked about it directly.

---

## 1. What the project actually does, end to end

1. A client registers a **shipper** (the business requesting freight) and
   **carriers** (name, address, capacity) → carrier/shipment addresses get
   geocoded via a real call to OpenStreetMap's Nominatim API.
2. A client registers a **shipment**, owned by a shipper.
3. A client asks for **matches** → a pool of background workers scores every
   available, capable carrier by distance and returns them ranked, each
   with an estimated price.
4. A client **dispatches** the shipment to a specific carrier → this is the
   real booking step: the carrier is atomically marked unavailable so a
   concurrent dispatch attempt on the same carrier can't also succeed, and
   a price is agreed.
5. A client advances the shipment's **status** as it moves through the real
   world (`dispatched → in_transit → delivered`), validated against an
   explicit state machine that rejects nonsensical transitions.

Nothing here is a toy simulation — every step above is a real, working piece
of code doing the actual work described.

---

## 2. Concurrency — the part of Go this project leans on hardest

### Goroutines
**Where:** `worker.go`, line 32 — `go pool.runWorker(i)`, called once per
worker when the pool starts up (`NewMatchWorkerPool`).

**What a goroutine is:** a function that runs concurrently with the rest of
your program, started by putting `go` in front of a function call. Go's
runtime schedules many goroutines onto a small number of OS threads — they
cost a few KB of memory each (vs. megabytes for an OS thread), so spinning
up thousands is normal in Go in a way it isn't in most languages.

**Why here:** 4 worker goroutines start when the server boots and run for
the program's entire lifetime, each in an infinite loop (`for job := range
p.jobs`) pulling match requests off a shared queue. This is what makes
matching happen "in the background" instead of blocking the HTTP request
thread while it computes.

### Channels
**Where:** `worker.go` — `jobs chan MatchJob` (line 22) and
`ResultCh chan []MatchResult` (line 17).

**What a channel is:** a typed pipe that goroutines use to send values to
each other safely, without needing to manually manage locks. `chan MatchJob`
means "a channel that only carries `MatchJob` values" — Go's type system
enforces this at compile time.

**Two different channels, two different jobs, in this project:**
- `jobs` (buffered, size 100) — the shared queue every worker goroutine
  reads from. Multiple workers can safely read from the same channel; Go
  guarantees each value is delivered to exactly one reader.
- `ResultCh` (buffered, size 1, created fresh per request in `Submit()`) —
  a private, one-shot channel just for handing one result back to one
  caller. Created in `handlers.go`'s `handleGetMatches`, read from there too.

**Why buffered vs. unbuffered:** `jobs` is buffered so submitting a job
doesn't block the HTTP handler if all 4 workers are currently busy — it just
queues (up to 100 deep) instead of stalling the request. `ResultCh` is
buffered at exactly 1 because exactly one value will ever be sent to it —
an unbuffered channel would also work here, but buffering by 1 means the
worker goroutine doesn't have to wait for the HTTP handler to be ready to
receive before it can move on.

### `select` with a timeout
**Where:** `handlers.go`, `handleGetMatches`:
```go
select {
case results := <-resultCh:
    writeJSON(w, http.StatusOK, results)
case <-time.After(15 * time.Second):
    writeError(w, http.StatusGatewayTimeout, "matching timed out")
}
```

**What this does:** `select` waits on multiple channel operations at once
and proceeds with whichever one becomes ready first. Here, it's a race
between "the worker pool finished and sent a result" and "15 seconds
passed." This is the actual mechanism that turns an async background job
into something an HTTP client experiences as a normal (if occasionally slow)
synchronous request — with a guaranteed upper bound on how long it'll wait,
instead of hanging forever if something's stuck.

### Mutexes (`sync.RWMutex`)
**Where:** `store.go`'s `MemStore` and `cache.go`'s `geocodeCache`.

**What it's for:** protecting a plain Go map from concurrent access. Maps in
Go are **not** safe for concurrent read/write — if two goroutines write to
the same map at the same time with no protection, the program can literally
crash (`fatal error: concurrent map writes`), not just produce a subtle bug.

**Why `RWMutex` specifically, not a plain `Mutex`:** `RWMutex` allows
**multiple simultaneous readers**, only blocking when a writer needs
exclusive access. `ListCarriers()` (a read) can run concurrently with
another goroutine's `ListCarriers()` call; only `SaveCarrier()` (a write)
needs to fully lock everyone else out. This matters here because reads
(checking the geocode cache, listing carriers for matching) happen far more
often than writes (registering a new carrier).

### Concurrency control isn't only in-process — `sqlite_store.go`'s `Dispatch()`

Everything above is about protecting a Go map inside one running process.
`Dispatch()` solves a related but distinct problem: **two separate HTTP
requests**, potentially handled concurrently, both trying to book the same
carrier.

```go
result, err := tx.Exec(
    `UPDATE carriers SET is_available = 0 WHERE id = ? AND is_available = 1`,
    carrierID,
)
rowsAffected, _ := result.RowsAffected()
if rowsAffected == 0 {
    return Shipment{}, ErrCarrierUnavailable
}
```

**Why this specific pattern (a conditional `UPDATE`, not a `SELECT` then an
`UPDATE`):** if the code instead did "`SELECT is_available` → check in Go →
`UPDATE`", two concurrent requests could both `SELECT` and see `true` before
either one's `UPDATE` runs — a classic check-then-act race condition. By
putting the availability check **inside the `WHERE` clause of the `UPDATE`
itself**, the check and the act happen atomically, as one operation the
database guarantees can't be interleaved. Only one of two racing requests
can ever get `rowsAffected == 1`; the other gets `0` and is told the carrier
is gone. This is a real, standard technique (optimistic concurrency control
via a conditional write), not something specific to this project.

---

## 3. Interfaces — how the storage layer can be swapped without touching the rest of the app

**Where:** `store.go`:
```go
type Store interface {
    SaveCarrier(c Carrier) error
    ListCarriers() []Carrier
    SaveShipment(s Shipment) error
    GetShipment(id string) (Shipment, error)
    UpdateShipmentStatus(id string, status string) error
}
```

**What this is:** Go interfaces are satisfied *implicitly* — any type that
happens to have all these methods automatically counts as a `Store`, with no
`implements Store` declaration needed (unlike Java). Both `MemStore` and
`SQLiteStore` implement every one of these methods, so both are valid
`Store` values.

**Why this is the single most important design decision in the project:**
`handlers.go`, `worker.go`, and `main.go` all depend only on the `Store`
interface — never on `MemStore` or `SQLiteStore` directly. That's why
switching from an in-memory map to a real SQLite file (see `main.go`,
`NewSQLiteStore(dbPath)` vs. the earlier `NewMemStore()`) required changing
exactly **one line** in `main.go` and adding one new file
(`sqlite_store.go`) — nothing in `handlers.go` or `worker.go` had to change
at all, because they never knew or cared which concrete implementation they
were talking to.

---

## 4. Error handling — Go's explicit, no-exceptions approach

Go has no `try`/`catch`. Every function that can fail returns an `error` as
an extra return value, and the caller is required to check it.

### Sentinel errors
**Where:** `store.go`, `var ErrNotFound = errors.New("not found")`

**What it's for:** a specific, comparable error value that calling code can
check for by identity. This lets `handlers.go`'s `handleGetMatches`
distinguish "this shipment genuinely doesn't exist" (→ 404) from any other
kind of database failure (→ 500), rather than treating every error
identically.

### Error wrapping with `%w`
**Where:** everywhere in `sqlite_store.go` and `geocode.go`, e.g.:
```go
return fmt.Errorf("saving carrier: %w", err)
```

**What `%w` specifically does (vs. `%v` or `%s`):** it wraps the original
error inside a new one *while preserving the original error's identity*, so
code further up the call stack can still use `errors.Is`/`errors.As` to
check what the underlying error actually was, even though a human-readable
prefix ("saving carrier: ...") was added on top. This is why almost every
error return in this project reads as a small chain — "saving carrier:
database is locked" — instead of losing the original detail.

### Named return values
**Where:** `geocode.go`:
```go
func (g *Geocoder) GeocodeAddress(address string) (lat float64, lon float64, err error)
```

**What this is:** `lat`, `lon`, and `err` are declared as part of the
function signature, not just inside the function body. This is a Go-specific
feature — mostly used here for readability (the signature itself documents
what's being returned, not just the types).

---

## 5. Standard library depth — what didn't need a third-party package

### `net/http` with Go 1.22's route patterns
**Where:** `main.go`:
```go
mux.HandleFunc("GET /shipments/{id}/matches", server.handleGetMatches)
```

Before Go 1.22, matching a URL pattern with a path variable like `{id}`
needed a third-party router (`chi`, `gorilla/mux`, `gin`). Go 1.22 added
method+pattern matching directly to the standard library's `http.ServeMux`.
`r.PathValue("id")` (used in `handleGetMatches`) retrieves that captured
segment. This entire REST API uses zero routing dependencies.

### `database/sql` — the standard interface, `modernc.org/sqlite` — just the driver
**Where:** `sqlite_store.go`.

Go's `database/sql` package defines a database-agnostic interface
(`sql.DB`, `sql.Rows`, `sql.Row`) — the actual driver
(`modernc.org/sqlite`) is imported only for its side effect of registering
itself (`import _ "modernc.org/sqlite"` — the blank `_` identifier means
"import this only to run its `init()`, I'm not calling anything on it
directly"). This is the standard Go pattern for pluggable database drivers:
the application code (`sql.Open("sqlite", dbPath)`, parameterized queries
with `?` placeholders) would look almost identical if this were Postgres or
MySQL instead — only the import and the driver name string would change.

### `crypto/rand`, not `math/rand`
**Where:** `id.go`.

**Why this distinction matters:** `math/rand` is a fast, predictable
pseudo-random generator — fine for things like jitter in retry logic, *not*
safe for anything used as an identifier where guessability matters.
`crypto/rand` reads from the OS's actual entropy source. Using the wrong one
here would be a real, if minor, security mistake — worth knowing the
difference exists, since Go makes you choose explicitly rather than having
one "random" function that's secretly unsafe for some uses.

### A third-party API with no SDK — `payment.go`'s Stripe client
**Where:** `payment.go`.

Stripe publishes an official Go SDK (`stripe-go`). This project deliberately
doesn't use it — `payment.go` calls Stripe's REST API directly with
`net/http`, `net/url.Values` for form encoding, and `req.SetBasicAuth()` for
their auth scheme (secret key as the HTTP Basic username, empty password).

**Why this is worth knowing as a pattern, not just a fact about this
project:** most "REST API" third-party services are, underneath their SDK,
just HTTP POST/GET with a specific auth header and a JSON or form-encoded
body. An SDK is a convenience wrapper, not a requirement — and skipping it
here means this project's only external Go dependency is
`modernc.org/sqlite`, not two. The trade-off, stated honestly: no
automatic request retries, no typed response structs beyond what's manually
defined (`stripePaymentIntentResponse`), and no help from the compiler if
Stripe changes a field name — an SDK would catch that at compile time, this
approach would only surface it at runtime.

---

## 6. Testing — what's real vs. what's mocked

**Where:** `matcher_test.go`.

Uses Go's built-in `testing` package (`func TestX(t *testing.T)`,
`t.Errorf`, `t.Fatalf`) — no external test framework. One test,
`TestHaversineDistance_KnownRoute`, checks the haversine distance
calculation against JFK→LAX's real-world distance (~2,475 miles) with a
tolerance, rather than only checking the math is internally consistent —
catching the class of bug where the formula is coded correctly but produces
numbers that don't match reality (e.g. a wrong Earth-radius constant).

**Honest limitation:** these tests only cover `matcher.go`'s pure functions.
`sqlite_store.go` and `geocode.go` have **no automated tests** — testing
those properly would mean either a real SQLite file created and torn down
per test run, or mocking the Nominatim HTTP call, neither of which is built
yet. Worth saying this plainly if asked "is this tested" — partially, and
specifically the part that's easiest to get subtly wrong (the distance math)
is the part that is.

---

## 7. What's genuinely achieved vs. simplified — the honest scorecard

| Area | Real | Simplified/Not done |
|---|---|---|
| Concurrency | Goroutines, channels, `select`+timeout, `RWMutex`, atomic conditional-`UPDATE` for dispatch — all real, all doing actual work | No `context.Context` for cancellation — a slow Nominatim call currently can't be cancelled mid-flight |
| Persistence | Real SQLite file, survives restarts, parameterized queries (SQL-injection-safe) | Migrations are hand-written `ALTER TABLE` + "ignore duplicate column" (see `sqlite_store.go`'s `migrate()`), not real migration tooling |
| Business logic | Shipper entity, atomic dispatch (no double-booking), explicit status state machine, real Stripe test-mode payment (authorize/capture/cancel) | Pricing formula is a labeled placeholder, not real freight rates; payment auth isn't in the same transaction as dispatch (see README) |
| HTTP API | Real REST API, real JSON, real status codes, real CORS handling | No auth, no rate limiting |
| External API | Real geocoding calls, real caching in front of them | No retry/backoff logic if Nominatim is briefly down |
| Frontend | Real, compiled, type-checked TypeScript, including dispatch + tracking UI | Not yet verified against a live running backend by me — only by you |
| Testing | Real distance-math test, checked against reality; new test for the availability check | Storage, geocoding, and dispatch race-condition behavior are untested |

---

## 8. If asked "why did you build it this way" — the one-sentence version

Every simplification above was a deliberate boundary, not an oversight: the
`Store` interface and the worker pool's `Submit()` function were built as
the two seams specifically so the "simplified" side of that table could be
replaced later without touching the "real" side — which is the same
architectural instinct a larger production system needs, just demonstrated
at a scale small enough to build, test, and actually explain in full.
