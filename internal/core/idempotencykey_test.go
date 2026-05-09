// Package core — named requirement-traceable sensors for the idempotency key
// derivation contract.
//
// Tests verify beads-integration.md §4.10 BI-029: IdempotencyKey MUST produce a
// deterministic "<run_id>:<transition_id>:<op>" string; identical inputs produce
// identical keys across invocations.
package core

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// idempotencyKeyFixtureIDs returns a stable (runID, transitionID) pair for use in
// idempotency key tests. Both IDs are freshly generated UUIDv7 values.
func idempotencyKeyFixtureIDs(t *testing.T) (RunID, TransitionID) {
	t.Helper()
	return RunID(uuid.Must(uuid.NewV7())), TransitionID(uuid.Must(uuid.NewV7()))
}

// TestIdempotencyKey_BI029Shape verifies that IdempotencyKey produces the exact
// "<run_id>:<transition_id>:<op>" shape required by beads-integration.md §4.10
// BI-029 for each of the three TerminalOp values.
func TestIdempotencyKey_BI029Shape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		op TerminalOp
	}{
		{TerminalOpClaim},
		{TerminalOpClose},
		{TerminalOpReopen},
	}

	for _, tc := range tests {
		t.Run(string(tc.op), func(t *testing.T) {
			t.Parallel()

			runID, transitionID := idempotencyKeyFixtureIDs(t)
			got := IdempotencyKey(runID, transitionID, tc.op)

			runStr := runID.String()
			transStr := transitionID.String()
			opStr := string(tc.op)

			// Key must have exactly three colon-separated segments.
			parts := strings.SplitN(got, ":", 3)
			if len(parts) != 3 {
				t.Errorf("IdempotencyKey %q: want 3 colon-separated segments, got %d", got, len(parts))
				return
			}

			if parts[0] != runStr {
				t.Errorf("IdempotencyKey %q: segment[0] = %q, want run_id %q", got, parts[0], runStr)
			}
			if parts[1] != transStr {
				t.Errorf("IdempotencyKey %q: segment[1] = %q, want transition_id %q", got, parts[1], transStr)
			}
			if parts[2] != opStr {
				t.Errorf("IdempotencyKey %q: segment[2] = %q, want op %q", got, parts[2], opStr)
			}

			want := fmt.Sprintf("%s:%s:%s", runStr, transStr, opStr)
			if got != want {
				t.Errorf("IdempotencyKey = %q, want %q", got, want)
			}
		})
	}
}

// TestIdempotencyKey_BI029Determinism verifies the BI-029 determinism invariant:
// identical (runID, transitionID, op) inputs produce identical keys across N
// invocations of IdempotencyKey.
func TestIdempotencyKey_BI029Determinism(t *testing.T) {
	t.Parallel()

	const invocations = 10
	ops := []TerminalOp{TerminalOpClaim, TerminalOpClose, TerminalOpReopen}

	for _, op := range ops {
		t.Run(string(op), func(t *testing.T) {
			t.Parallel()

			runID, transitionID := idempotencyKeyFixtureIDs(t)
			first := IdempotencyKey(runID, transitionID, op)

			for i := 1; i < invocations; i++ {
				got := IdempotencyKey(runID, transitionID, op)
				if got != first {
					t.Errorf("invocation %d: IdempotencyKey = %q, want %q (non-deterministic)", i, got, first)
				}
			}
		})
	}
}

// TestIdempotencyKey_DistinctPerOp verifies that different op values produce
// distinct keys for the same (runID, transitionID).
func TestIdempotencyKey_DistinctPerOp(t *testing.T) {
	t.Parallel()

	runID, transitionID := idempotencyKeyFixtureIDs(t)

	claimKey := IdempotencyKey(runID, transitionID, TerminalOpClaim)
	closeKey := IdempotencyKey(runID, transitionID, TerminalOpClose)
	reopenKey := IdempotencyKey(runID, transitionID, TerminalOpReopen)

	if claimKey == closeKey {
		t.Errorf("claim and close keys collide: %q", claimKey)
	}
	if claimKey == reopenKey {
		t.Errorf("claim and reopen keys collide: %q", claimKey)
	}
	if closeKey == reopenKey {
		t.Errorf("close and reopen keys collide: %q", closeKey)
	}
}

// TestIdempotencyKey_DistinctPerRunID verifies that different runIDs produce
// distinct keys when transitionID and op are the same.
func TestIdempotencyKey_DistinctPerRunID(t *testing.T) {
	t.Parallel()

	runIDA, transitionID := idempotencyKeyFixtureIDs(t)
	runIDB := RunID(uuid.Must(uuid.NewV7()))

	keyA := IdempotencyKey(runIDA, transitionID, TerminalOpClaim)
	keyB := IdempotencyKey(runIDB, transitionID, TerminalOpClaim)

	if keyA == keyB {
		t.Errorf("distinct run_ids produced the same key: %q", keyA)
	}
}

// TestIdempotencyKey_DistinctPerTransitionID verifies that different transitionIDs
// produce distinct keys when runID and op are the same.
func TestIdempotencyKey_DistinctPerTransitionID(t *testing.T) {
	t.Parallel()

	runID, transitionIDA := idempotencyKeyFixtureIDs(t)
	transitionIDB := TransitionID(uuid.Must(uuid.NewV7()))

	keyA := IdempotencyKey(runID, transitionIDA, TerminalOpClaim)
	keyB := IdempotencyKey(runID, transitionIDB, TerminalOpClaim)

	if keyA == keyB {
		t.Errorf("distinct transition_ids produced the same key: %q", keyA)
	}
}
