package watch_test

// digest_we_soak2_b2_test.go — RED→GREEN acceptance tests for hk-8yh32.2
// (WE-SOAK-2/B2: watch digest freshness + pruning).
//
// P3 (DIGEST STALENESS): Ledger.WriteDigest must stamp updated_at on every
//   write regardless of what the caller placed in the struct.  Before the fix,
//   a caller that omits updated_at produces a stale (or empty) timestamp;
//   after the fix, WriteDigest is the authoritative stamper.
//
// P4 (DIGEST NOISE): appendDigestFlag (via EscalationEngine.Process) must
//   evict RESOLVED/NOTED flags and enforce a cap of PendingFlagsMaxLen so that
//   pending_flags never grows unbounded.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/watch"
)

// TestWatchDigest_UpdatedAtAlwaysStampedByWrite_B2_P3 asserts that
// Ledger.WriteDigest stamps updated_at with the current time regardless of
// what the caller set.  RED before fix (WriteDigest doesn't override the
// caller's value), GREEN after.
func TestWatchDigest_UpdatedAtAlwaysStampedByWrite_B2_P3(t *testing.T) {
	t.Parallel()

	_, harmonikDir, _ := ledgerFixtureDir(t)

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	before := time.Now()

	// Write a digest with NO updated_at supplied by the caller.
	d := watch.WatchDigest{
		Cursor: "0193a2b3-dead-7000-0000-000000000001",
		// UpdatedAt intentionally omitted — WriteDigest must stamp it itself.
	}
	if err := ledger.WriteDigest(d); err != nil {
		t.Fatalf("WriteDigest: %v", err)
	}

	after := time.Now()

	got := readDigestFile(t, harmonikDir)
	if got == nil {
		t.Fatal("P3: WriteDigest did not write latest.json")
	}

	if got.UpdatedAt == "" {
		t.Fatal("P3: updated_at is empty after WriteDigest — WriteDigest must stamp it")
	}

	ts, parseErr := time.Parse(time.RFC3339, got.UpdatedAt)
	if parseErr != nil {
		t.Fatalf("P3: updated_at %q is not RFC3339: %v", got.UpdatedAt, parseErr)
	}

	if ts.Before(before.Truncate(time.Second)) || ts.After(after.Add(time.Second)) {
		t.Errorf("P3: updated_at %s is outside expected window [%s, %s]",
			got.UpdatedAt,
			before.Format(time.RFC3339),
			after.Format(time.RFC3339))
	}
}

// TestWatchDigest_UpdatedAtAdvancesOnSecondWrite_B2_P3b asserts that a stale
// caller-supplied updated_at is overwritten by WriteDigest on every call.
// RED before fix (second write keeps the stale value the caller set), GREEN
// after (WriteDigest overwrites it with a fresh timestamp).
func TestWatchDigest_UpdatedAtAdvancesOnSecondWrite_B2_P3b(t *testing.T) {
	t.Parallel()

	_, harmonikDir, _ := ledgerFixtureDir(t)

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	staleTs := "2026-01-01T06:02:35Z" // ~9.8 h ago (the SOAK-1 incident timestamp)

	// Write a digest that a misbehaving caller stamped with a stale time.
	stale := watch.WatchDigest{
		Cursor:    "0193a2b3-dead-7000-0000-000000000001",
		UpdatedAt: staleTs,
	}
	if err := ledger.WriteDigest(stale); err != nil {
		t.Fatalf("WriteDigest(stale): %v", err)
	}

	got := readDigestFile(t, harmonikDir)
	if got == nil {
		t.Fatal("P3b: WriteDigest did not write latest.json")
	}

	if got.UpdatedAt == staleTs {
		t.Fatalf("P3b: updated_at is still the caller-supplied stale value %q — WriteDigest must overwrite it", staleTs)
	}
	if got.UpdatedAt == "" {
		t.Fatal("P3b: updated_at is empty after WriteDigest")
	}
}

// TestWatchDigest_PrunesResolvedAndNotedFlags_B2_P4 asserts that
// EscalationEngine.Process (PULL-DIGEST path) evicts flags containing
// "RESOLVED" or "NOTED" and enforces a cap of PendingFlagsMaxLen.
// RED before fix (flags accumulate unbounded with terminal entries present),
// GREEN after.
func TestWatchDigest_PrunesResolvedAndNotedFlags_B2_P4(t *testing.T) {
	t.Parallel()

	harmonikDir, _ := escalationFixtureDir(t)

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	sender := &mockSender{}
	engine := &watch.EscalationEngine{Ledger: ledger, Sender: sender}

	// Append a mix of active and terminal flags via the PULL-DIGEST path.
	for i := 0; i < 8; i++ {
		flag := fmt.Sprintf("crew paul stale on epic hk-%04d; check needed (%d)", i, i)
		ev := escalationFixtureEvent(t, "run_stale")
		if err := engine.Process(ev, flag); err != nil {
			t.Fatalf("Process(active %d): %v", i, err)
		}
	}

	// Add terminal flags — these must be evicted by the pruner.
	for i := 0; i < 7; i++ {
		resolvedFlag := fmt.Sprintf("RESOLVED: crew paul reconnected on epic hk-%04d (%d)", i, i)
		ev := escalationFixtureEvent(t, "run_stale")
		if err := engine.Process(ev, resolvedFlag); err != nil {
			t.Fatalf("Process(RESOLVED %d): %v", i, err)
		}
	}
	for i := 0; i < 4; i++ {
		notedFlag := fmt.Sprintf("NOTED: minor drift on queue wake-economy-%d, no action (%d)", i, i)
		ev := escalationFixtureEvent(t, "run_stale")
		if err := engine.Process(ev, notedFlag); err != nil {
			t.Fatalf("Process(NOTED %d): %v", i, err)
		}
	}

	d := readDigestFile(t, harmonikDir)
	if d == nil {
		t.Fatal("P4: latest.json was not written")
	}

	// No RESOLVED or NOTED flags must survive.
	for _, f := range d.PendingFlags {
		if strings.Contains(f, "RESOLVED") {
			t.Errorf("P4: RESOLVED flag was not evicted: %q", f)
		}
		if strings.Contains(f, "NOTED") {
			t.Errorf("P4: NOTED flag was not evicted: %q", f)
		}
	}

	// Total must not exceed the cap.
	if len(d.PendingFlags) > watch.PendingFlagsMaxLen {
		t.Fatalf("P4: pending_flags len %d exceeds cap %d: %v",
			len(d.PendingFlags), watch.PendingFlagsMaxLen, d.PendingFlags)
	}
}

// TestWatchDigest_CapEnforcedOnActiveFlags_B2_P4b asserts that when pending_flags
// would exceed PendingFlagsMaxLen even after terminal eviction, the oldest
// active entries are dropped.  RED before fix (no cap), GREEN after.
func TestWatchDigest_CapEnforcedOnActiveFlags_B2_P4b(t *testing.T) {
	t.Parallel()

	harmonikDir, _ := escalationFixtureDir(t)

	ledger, err := watch.NewLedger(harmonikDir)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}
	sender := &mockSender{}
	engine := &watch.EscalationEngine{Ledger: ledger, Sender: sender}

	// Append more active flags than the cap.
	excess := watch.PendingFlagsMaxLen + 5
	for i := 0; i < excess; i++ {
		flag := fmt.Sprintf("crew paul stale on epic hk-%04d; check needed (%d)", i, i)
		ev := escalationFixtureEvent(t, "run_stale")
		if err := engine.Process(ev, flag); err != nil {
			t.Fatalf("Process(%d): %v", i, err)
		}
	}

	d := readDigestFile(t, harmonikDir)
	if d == nil {
		t.Fatal("P4b: latest.json was not written")
	}

	if len(d.PendingFlags) > watch.PendingFlagsMaxLen {
		t.Fatalf("P4b: cap not enforced: len=%d want ≤%d", len(d.PendingFlags), watch.PendingFlagsMaxLen)
	}

	// The MOST RECENT flags must be retained (oldest evicted).
	lastFlag := fmt.Sprintf("crew paul stale on epic hk-%04d; check needed (%d)", excess-1, excess-1)
	found := false
	for _, f := range d.PendingFlags {
		if f == lastFlag {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("P4b: most-recent flag missing from pruned set; flags: %v", d.PendingFlags)
	}
}
