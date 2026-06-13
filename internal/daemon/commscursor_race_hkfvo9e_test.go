package daemon

// commscursor_race_hkfvo9e_test.go — reproduction + regression for the
// multi-daemon (cross-process) cursor race (hk-fvo9e).
//
// The per-agent in-process mutex (AgentMu, hk-fww4e) serializes recv ops within
// ONE process, but two daemons / two processes doing concurrent recv for the
// SAME agent share no in-process lock. Each reads the durable cursor, scans, and
// blindly Advance-overwrites it. A process that scanned an OLDER snapshot can
// rename an OLDER event_id over a NEWER one, moving the cursor BACKWARD — which
// re-delivers already-consumed messages (or, framed the other way, loses an
// advance). The cursor's load-bearing invariant is monotonic-advance: it must
// NEVER move to an earlier (smaller) event_id.
//
// These tests drive that invariant directly against CursorStore.Advance. They
// FAIL on the pre-fix blind-overwrite implementation and PASS once Advance
// enforces monotonicity under a cross-process file lock.
//
// Bead ref: hk-fvo9e.

import (
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// orderedV7 returns n UUIDv7 strings in strictly ascending byte order, matching
// the chronological ordering ScanAfter relies on (EV-002). They are sorted so
// the test does not depend on uuid.NewV7's intra-millisecond monotonicity.
func orderedV7(t *testing.T, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for len(ids) < n {
		u, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("uuid.NewV7: %v", err)
		}
		s := u.String()
		// Reject any non-ascending value (clock regression within the loop) and
		// retry, so the slice is guaranteed strictly ascending.
		if len(ids) == 0 || s > ids[len(ids)-1] {
			ids = append(ids, s)
		}
	}
	return ids
}

// TestCursorStore_AdvanceIsMonotonic is the core reproduction: a NEWER cursor
// is already persisted, then a laggard writer (the second process that scanned
// an older snapshot) calls Advance with an OLDER event_id. The cursor MUST NOT
// regress. Pre-fix, Advance blindly overwrites and the cursor moves backward.
func TestCursorStore_AdvanceIsMonotonic(t *testing.T) {
	t.Parallel()
	cs := NewCursorStore(t.TempDir())

	ids := orderedV7(t, 2)
	older, newer := ids[0], ids[1]

	// Process A advances to the newer event_id.
	if err := cs.Advance("agent-x", newer); err != nil {
		t.Fatalf("Advance(newer): %v", err)
	}

	// Process B (scanned an older snapshot) tries to advance to the older id.
	// This must be a no-op: the cursor may not move backward. The pre-fix code
	// blindly renames the older value into place.
	if err := cs.Advance("agent-x", older); err != nil {
		t.Fatalf("Advance(older): %v", err)
	}

	got, err := cs.Get("agent-x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != newer {
		t.Fatalf("cursor regressed: want %q (newer), got %q — a backward Advance moved the cursor back, re-delivering consumed messages", newer, got)
	}
}

// TestCursorStore_ConcurrentAdvanceNeverRegresses stresses the monotonic
// invariant from N goroutines (simulating N processes racing recv for the same
// agent). Each goroutine advances to a random one of a fixed ascending set; the
// final persisted cursor MUST equal the maximum, never a smaller value. Run
// with -race to also catch the temp+rename data race on the shared file.
func TestCursorStore_ConcurrentAdvanceNeverRegresses(t *testing.T) {
	t.Parallel()
	cs := NewCursorStore(t.TempDir())

	const goroutines = 16
	ids := orderedV7(t, goroutines)
	maxID := ids[len(ids)-1]

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			// Each writer advances to its own id; ordering across goroutines is
			// nondeterministic, so a non-monotonic store will sometimes land a
			// smaller id last.
			if err := cs.Advance("racer", id); err != nil {
				t.Errorf("Advance(%s): %v", id, err)
			}
		}(ids[i])
	}
	wg.Wait()

	got, err := cs.Get("racer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != maxID {
		t.Fatalf("after %d concurrent advances the cursor is %q, want max %q — a smaller id won the race and the cursor regressed", goroutines, got, maxID)
	}
}

// TestCursorStore_AdvanceEqualIsStable verifies advancing to the already-stored
// value is a stable no-op (not an error, not a regression) — important because
// at-least-once re-delivery can re-present the same tail event_id.
func TestCursorStore_AdvanceEqualIsStable(t *testing.T) {
	t.Parallel()
	cs := NewCursorStore(t.TempDir())

	id := orderedV7(t, 1)[0]
	if err := cs.Advance("agent-eq", id); err != nil {
		t.Fatalf("first Advance: %v", err)
	}
	if err := cs.Advance("agent-eq", id); err != nil {
		t.Fatalf("equal Advance: %v", err)
	}
	got, err := cs.Get("agent-eq")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != id {
		t.Fatalf("equal Advance changed cursor: want %q, got %q", id, got)
	}
}

// TestCursorStore_TwoStoresNeverRegress simulates the actual multi-daemon case:
// TWO independent CursorStore instances pointed at the SAME directory (as two
// daemons / two processes would be). They have SEPARATE in-process AgentMu maps,
// so the hk-fww4e per-agent mutex provides NO serialization between them — only
// the cross-process flock + monotonic guard in Advance can keep the cursor from
// regressing. Each store races a disjoint half of an ascending id set; the final
// persisted cursor MUST be the global maximum.
func TestCursorStore_TwoStoresNeverRegress(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	csA := NewCursorStore(dir) // "daemon A"
	csB := NewCursorStore(dir) // "daemon B", same dir, independent AgentMu map

	const half = 12
	ids := orderedV7(t, 2*half)
	maxID := ids[len(ids)-1]

	var wg sync.WaitGroup
	advanceAll := func(cs *CursorStore, set []string) {
		defer wg.Done()
		for _, id := range set {
			if err := cs.Advance("multi", id); err != nil {
				t.Errorf("Advance(%s): %v", id, err)
			}
		}
	}
	wg.Add(2)
	// Interleave: A gets the odd indices, B the even — so the global max may be
	// written by either store, and a non-cross-process-safe impl would let a
	// laggard from the other store clobber it.
	var setA, setB []string
	for i, id := range ids {
		if i%2 == 0 {
			setB = append(setB, id)
		} else {
			setA = append(setA, id)
		}
	}
	go advanceAll(csA, setA)
	go advanceAll(csB, setB)
	wg.Wait()

	got, err := csA.Get("multi")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != maxID {
		t.Fatalf("two-store race regressed cursor: got %q, want global max %q — the cross-process flock+monotonic guard did not hold", got, maxID)
	}
}

// TestCursorStore_AdvanceForwardStillWorks guards against the fix over-rejecting:
// a strictly-forward Advance (the normal case) must still move the cursor.
func TestCursorStore_AdvanceForwardStillWorks(t *testing.T) {
	t.Parallel()
	cs := NewCursorStore(t.TempDir())

	ids := orderedV7(t, 4)
	for i, id := range ids {
		if err := cs.Advance("agent-fwd", id); err != nil {
			t.Fatalf("Advance #%d (%s): %v", i, id, err)
		}
		got, err := cs.Get("agent-fwd")
		if err != nil {
			t.Fatalf("Get #%d: %v", i, err)
		}
		if got != id {
			t.Fatalf("forward Advance #%d did not take: want %q, got %q", i, id, got)
		}
	}
}

// ensure fmt is referenced even if a future edit drops its only use.
var _ = fmt.Sprintf
