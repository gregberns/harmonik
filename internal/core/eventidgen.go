package core

import (
	"sync"

	"github.com/google/uuid"
)

// EventIDGenerator generates strictly monotonic EventID values within a single
// process per event-model.md §4.1 EV-002a.
//
// Method (RFC 9562 §6.2 method 1 — clock-rollback prevention):
//
//	Generate a fresh UUIDv7 from the current wall clock. If the new value
//	would be lexicographically <= the most recently issued value, return the
//	most-recent + 1 (incrementing the lowest-bit nanosecond fraction so
//	strict monotonicity holds even within the same millisecond).
//
// Concurrency: Next is safe to call from multiple goroutines. The internal
// mutex serialises access; the generator's outputs across all goroutines are
// strictly monotonic.
//
// Cross-process and cross-restart monotonicity are NOT guaranteed by this type
// (covered by hk-hqwn.4 daemon-routing and hk-hqwn.5 high-water-mark).
type EventIDGenerator struct {
	mu   sync.Mutex
	last uuid.UUID // zero value before first Next call

	// newV7 is the factory for fresh UUIDv7 values. Defaults to uuid.NewV7.
	// Settable for testing to inject deterministic values.
	newV7 func() (uuid.UUID, error)
}

// NewEventIDGenerator returns a fresh generator backed by the real wall clock.
func NewEventIDGenerator() *EventIDGenerator {
	return &EventIDGenerator{
		newV7: uuid.NewV7,
	}
}

// Next returns the next strictly-monotonic EventID.
//
// Two consecutive calls always satisfy a.Lex < b.Lex when the 16-byte
// big-endian representations are compared as unsigned 128-bit integers
// (EV-002a, event-model.md §4.1).
//
// If the underlying clock produces a value that is not strictly greater than
// the last issued value (e.g. same millisecond, clock rollback), Next
// increments the last value by 1 (treating the 16 bytes as a big-endian
// 128-bit integer). The version nibble (bits 76–79) MAY be perturbed by an
// increment that carries through byte 6; this is explicitly permitted by
// RFC 9562 §6.2 method 1.
func (g *EventIDGenerator) Next() (EventID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	fresh, err := g.newV7()
	if err != nil {
		return EventID{}, err
	}

	var candidate uuid.UUID
	if uuidGT(fresh, g.last) {
		candidate = fresh
	} else {
		candidate = increment128(g.last)
	}

	g.last = candidate
	return EventID(candidate), nil
}

// uuidGT reports whether a is strictly greater than b when each is
// interpreted as a 16-byte big-endian unsigned integer.
func uuidGT(a, b uuid.UUID) bool {
	for i := 0; i < 16; i++ {
		if a[i] > b[i] {
			return true
		}
		if a[i] < b[i] {
			return false
		}
	}
	return false // equal
}

// increment128 adds 1 to v treated as a 16-byte big-endian unsigned integer.
// If v is the maximum value (all 0xFF), it wraps to zero; this is a degenerate
// case that cannot occur in practice within a single process lifetime.
func increment128(v uuid.UUID) uuid.UUID {
	result := v
	for i := 15; i >= 0; i-- {
		result[i]++
		if result[i] != 0 {
			break
		}
		// carry
	}
	return result
}
