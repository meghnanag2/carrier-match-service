package main

import (
	"crypto/rand"
	"encoding/hex"
)

// newID returns a random 16-byte hex-encoded ID.
// Not a spec-compliant UUID (no version/variant bits set) — deliberately
// kept simple so the project has zero external dependencies and doesn't
// need `go get`. Swap for github.com/google/uuid if RFC 4122 compliance
// ever actually matters (e.g. interop with a system that validates UUID
// format).
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failing is effectively unrecoverable (no entropy
		// source) — panic rather than silently handing out a zero-value ID.
		panic("newID: failed to read random bytes: " + err.Error())
	}
	return hex.EncodeToString(b)
}
