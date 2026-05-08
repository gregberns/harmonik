package core

import (
	"sync"

	"github.com/google/uuid"
)

// TransitionIDGenerator generates strictly monotonic TransitionID values within a
// single daemon process per execution-model.md §4.4.EM-018a.
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
// Generation MUST occur in the daemon process (not in agent subprocesses) so
// that a single generation locus exists per project per EM-018a. Within a
// single run, TransitionID values MUST be unique — this generator provides
// that guarantee mechanically.
//
// Cross-run uniqueness of TransitionID values is NOT required: the run-scoped
// sibling-file path (.harmonik/transitions/<run_id>/<transition_id>.json per
// EM-018) provides the structural collision guarantee.
type TransitionIDGenerator struct {
	mu   sync.Mutex
	last uuid.UUID // zero value before first Next call

	// newV7 is the factory for fresh UUIDv7 values. Defaults to uuid.NewV7.
	// Settable for testing to inject deterministic values.
	newV7 func() (uuid.UUID, error)
}

// NewTransitionIDGenerator returns a fresh generator backed by the real wall clock.
func NewTransitionIDGenerator() *TransitionIDGenerator {
	return &TransitionIDGenerator{
		newV7: uuid.NewV7,
	}
}

// Next returns the next strictly-monotonic TransitionID.
//
// Two consecutive calls always satisfy a.Lex < b.Lex when the 16-byte
// big-endian representations are compared as unsigned 128-bit integers
// (EM-018a, execution-model.md §4.4).
//
// If the underlying clock produces a value that is not strictly greater than
// the last issued value (e.g. same millisecond, clock rollback), Next
// increments the last value by 1 (treating the 16 bytes as a big-endian
// 128-bit integer). The version nibble (bits 76–79) MAY be perturbed by an
// increment that carries through byte 6; this is explicitly permitted by
// RFC 9562 §6.2 method 1.
func (g *TransitionIDGenerator) Next() (TransitionID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	fresh, err := g.newV7()
	if err != nil {
		return TransitionID{}, err
	}

	var candidate uuid.UUID
	if uuidGT(fresh, g.last) {
		candidate = fresh
	} else {
		candidate = increment128(g.last)
	}

	g.last = candidate
	return TransitionID(candidate), nil
}
