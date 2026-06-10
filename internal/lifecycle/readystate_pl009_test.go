package lifecycle

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// TestPL009_ReadyCriteria_Met_AllTrue verifies that Met() returns true when
// all five PL-009 criteria are set.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "daemon MUST transition to
// `ready` only when ALL hold."
func TestPL009_ReadyCriteria_Met_AllTrue(t *testing.T) {
	t.Parallel()

	c := ReadyCriteria{
		OrphanSweepDone:            true,
		Cat0PreCheckPassed:         true,
		GitWalkDone:                true,
		InMemoryModelBuilt:         true,
		ReconciliationDispatchDone: true,
	}
	if !c.Met() {
		t.Error("PL-009 Met: all criteria true → Met() must return true")
	}
}

// TestPL009_ReadyCriteria_Met_EachMissing verifies that Met() returns false
// when any single criterion is unset.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "ALL hold" (conjunction).
func TestPL009_ReadyCriteria_Met_EachMissing(t *testing.T) {
	t.Parallel()

	allTrue := ReadyCriteria{
		OrphanSweepDone:            true,
		Cat0PreCheckPassed:         true,
		GitWalkDone:                true,
		InMemoryModelBuilt:         true,
		ReconciliationDispatchDone: true,
	}

	cases := []struct {
		name  string
		mutFn func(c *ReadyCriteria)
	}{
		{
			name:  "orphan-sweep-missing",
			mutFn: func(c *ReadyCriteria) { c.OrphanSweepDone = false },
		},
		{
			name:  "cat0-precheck-missing",
			mutFn: func(c *ReadyCriteria) { c.Cat0PreCheckPassed = false },
		},
		{
			name:  "git-walk-missing",
			mutFn: func(c *ReadyCriteria) { c.GitWalkDone = false },
		},
		{
			name:  "in-memory-model-missing",
			mutFn: func(c *ReadyCriteria) { c.InMemoryModelBuilt = false },
		},
		{
			name:  "reconciliation-dispatch-missing",
			mutFn: func(c *ReadyCriteria) { c.ReconciliationDispatchDone = false },
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			c := allTrue // copy
			tc.mutFn(&c)
			if c.Met() {
				t.Errorf("PL-009 Met %s: expected Met()=false when criterion unset", tc.name)
			}
		})
	}
}

// TestPL009_ReadyCriteria_Met_AllFalse verifies that a zero-value ReadyCriteria
// (no criteria set) returns false from Met().
//
// Spec ref: process-lifecycle.md §4.3 PL-009.
func TestPL009_ReadyCriteria_Met_AllFalse(t *testing.T) {
	t.Parallel()

	var c ReadyCriteria
	if c.Met() {
		t.Error("PL-009 Met all-false: zero-value ReadyCriteria must return Met()=false")
	}
}

// TestPL009_MonotonicNsSinceBoot_Positive verifies that MonotonicNsSinceBoot()
// returns a positive value.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "`ready_at_ns_since_boot` is
// the monotonic-clock companion field (nanoseconds since system boot, sourced
// from CLOCK_MONOTONIC on Linux / mach_absolute_time() translated to ns on
// darwin)."
func TestPL009_MonotonicNsSinceBoot_Positive(t *testing.T) {
	t.Parallel()

	ns, err := MonotonicNsSinceBoot()
	if err != nil {
		t.Fatalf("PL-009 MonotonicNsSinceBoot: unexpected error: %v", err)
	}
	if ns == 0 {
		t.Error("PL-009 MonotonicNsSinceBoot: returned 0, want > 0")
	}
}

// TestPL009_MonotonicNsSinceBoot_Monotonic verifies that two successive calls
// to MonotonicNsSinceBoot() return non-decreasing values.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — monotonic (non-decreasing).
func TestPL009_MonotonicNsSinceBoot_Monotonic(t *testing.T) {
	t.Parallel()

	ns1, err := MonotonicNsSinceBoot()
	if err != nil {
		t.Fatalf("PL-009 MonotonicNsSinceBoot monotonic: first call error: %v", err)
	}

	time.Sleep(time.Millisecond) // ensure the clock advances

	ns2, err := MonotonicNsSinceBoot()
	if err != nil {
		t.Fatalf("PL-009 MonotonicNsSinceBoot monotonic: second call error: %v", err)
	}

	if ns2 < ns1 {
		t.Errorf("PL-009 MonotonicNsSinceBoot monotonic: second call %d < first call %d (clock must be non-decreasing)", ns2, ns1)
	}
}

// TestPL009_BuildDaemonReadyPayload_Valid verifies that BuildDaemonReadyPayload
// returns a well-formed payload that passes core.DaemonReadyPayload.Valid().
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "daemon MUST emit `daemon_ready`
// with {ready_at, ready_at_ns_since_boot, investigator_run_ids[]}."
//
// Spec ref: event-model.md §8.7.2 — DaemonReadyPayload schema.
func TestPL009_BuildDaemonReadyPayload_Valid(t *testing.T) {
	t.Parallel()

	payload, err := BuildDaemonReadyPayload(nil)
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload: unexpected error: %v", err)
	}

	if !payload.Valid() {
		t.Errorf("PL-009 BuildDaemonReadyPayload: payload.Valid() = false, want true; payload = %+v", payload)
	}
}

// TestPL009_BuildDaemonReadyPayload_ReadyAtFormat verifies that the ReadyAt
// field is a non-empty RFC 3339 timestamp.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "`ready_at` is the wall-clock
// time at emission (RFC 3339 with ms)."
func TestPL009_BuildDaemonReadyPayload_ReadyAtFormat(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC()
	payload, err := BuildDaemonReadyPayload(nil)
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload ReadyAt: unexpected error: %v", err)
	}

	if payload.ReadyAt == "" {
		t.Fatal("PL-009 BuildDaemonReadyPayload ReadyAt: ReadyAt is empty, want RFC 3339 timestamp")
	}

	// Must parse as RFC 3339.
	parsed, parseErr := time.Parse(time.RFC3339Nano, payload.ReadyAt)
	if parseErr != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload ReadyAt: parse RFC3339 %q: %v", payload.ReadyAt, parseErr)
	}

	// Must be bracketed by before/after.
	if parsed.Before(before) {
		t.Errorf("PL-009 BuildDaemonReadyPayload ReadyAt: parsed time %v is before before-time %v", parsed, before)
	}
	if parsed.After(after) {
		t.Errorf("PL-009 BuildDaemonReadyPayload ReadyAt: parsed time %v is after after-time %v", parsed, after)
	}
}

// TestPL009_BuildDaemonReadyPayload_MonotonicCompanionPositive verifies that
// ReadyAtNsSinceBoot is positive in the built payload.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "`ready_at_ns_since_boot` must
// be > 0" (derived from core.DaemonReadyPayload.Valid()).
func TestPL009_BuildDaemonReadyPayload_MonotonicCompanionPositive(t *testing.T) {
	t.Parallel()

	payload, err := BuildDaemonReadyPayload(nil)
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload monotonic: unexpected error: %v", err)
	}

	if payload.ReadyAtNsSinceBoot == 0 {
		t.Error("PL-009 BuildDaemonReadyPayload monotonic: ReadyAtNsSinceBoot = 0, want > 0")
	}
}

// TestPL009_BuildDaemonReadyPayload_NilRunIDsBecomesEmpty verifies that a nil
// investigatorRunIDs slice is normalised to an empty (non-nil) slice so that
// JSON serialization emits [] rather than null.
//
// Spec ref: event-model.md §8.7.2 — investigator_run_ids array (never null).
func TestPL009_BuildDaemonReadyPayload_NilRunIDsBecomesEmpty(t *testing.T) {
	t.Parallel()

	payload, err := BuildDaemonReadyPayload(nil)
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload nil-run-ids: unexpected error: %v", err)
	}

	if payload.InvestigatorRunIDs == nil {
		t.Error("PL-009 BuildDaemonReadyPayload nil-run-ids: InvestigatorRunIDs is nil, want non-nil empty slice")
	}
	if len(payload.InvestigatorRunIDs) != 0 {
		t.Errorf("PL-009 BuildDaemonReadyPayload nil-run-ids: len(InvestigatorRunIDs) = %d, want 0", len(payload.InvestigatorRunIDs))
	}
}

// TestPL009_BuildDaemonReadyPayload_WithInvestigatorRunIDs verifies that
// investigator run IDs are preserved in the payload.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "investigator_run_ids[]
// contains run IDs of any reconciliation investigators dispatched before ready
// (per PL-009a)."
func TestPL009_BuildDaemonReadyPayload_WithInvestigatorRunIDs(t *testing.T) {
	t.Parallel()

	id1 := core.RunID(uuid.MustParse("01950000-0000-7000-8000-000000000001"))
	id2 := core.RunID(uuid.MustParse("01950000-0000-7000-8000-000000000002"))
	runIDs := []core.RunID{id1, id2}

	payload, err := BuildDaemonReadyPayload(runIDs)
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload run-ids: unexpected error: %v", err)
	}

	if len(payload.InvestigatorRunIDs) != 2 {
		t.Fatalf("PL-009 BuildDaemonReadyPayload run-ids: len(InvestigatorRunIDs) = %d, want 2", len(payload.InvestigatorRunIDs))
	}
	if payload.InvestigatorRunIDs[0] != id1 {
		t.Errorf("PL-009 BuildDaemonReadyPayload run-ids: [0] = %v, want %v", payload.InvestigatorRunIDs[0], id1)
	}
	if payload.InvestigatorRunIDs[1] != id2 {
		t.Errorf("PL-009 BuildDaemonReadyPayload run-ids: [1] = %v, want %v", payload.InvestigatorRunIDs[1], id2)
	}

	if !payload.Valid() {
		t.Error("PL-009 BuildDaemonReadyPayload run-ids: payload.Valid() = false, want true")
	}
}

// TestPL009_BuildDaemonReadyPayload_ReadyAtIsUTC verifies that the ReadyAt
// timestamp is emitted in UTC.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — "RFC 3339 with ms".
func TestPL009_BuildDaemonReadyPayload_ReadyAtIsUTC(t *testing.T) {
	t.Parallel()

	payload, err := BuildDaemonReadyPayload(nil)
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload UTC: unexpected error: %v", err)
	}

	// UTC timestamps end with 'Z' (RFC 3339 UTC designator) or '+00:00'.
	if !strings.HasSuffix(payload.ReadyAt, "Z") && !strings.HasSuffix(payload.ReadyAt, "+00:00") {
		t.Errorf("PL-009 BuildDaemonReadyPayload UTC: ReadyAt %q is not UTC (must end with Z or +00:00)", payload.ReadyAt)
	}
}

// TestPL009_ReadyCriteria_ZeroValue verifies that a zero-value ReadyCriteria
// is a valid Go struct (no panics) with all criteria false.
//
// Spec ref: process-lifecycle.md §4.3 PL-009.
func TestPL009_ReadyCriteria_ZeroValue(t *testing.T) {
	t.Parallel()

	var c ReadyCriteria
	// No panic, zero value is the "nothing done" state.
	if c.OrphanSweepDone || c.Cat0PreCheckPassed || c.GitWalkDone || c.InMemoryModelBuilt || c.ReconciliationDispatchDone {
		t.Error("PL-009 ReadyCriteria zero-value: expected all criteria false")
	}
	if c.Met() {
		t.Error("PL-009 ReadyCriteria zero-value: Met() must be false for zero value")
	}
}

// TestPL009_BuildDaemonReadyPayload_MonotonicBracketedByCallTimes verifies
// that ReadyAtNsSinceBoot is bracketed by before/after monotonic readings.
//
// Spec ref: process-lifecycle.md §4.3 PL-009 — monotonic companion captured
// at the ready transition.
func TestPL009_BuildDaemonReadyPayload_MonotonicBracketedByCallTimes(t *testing.T) {
	t.Parallel()

	before, err := MonotonicNsSinceBoot()
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload bracket: before MonotonicNsSinceBoot: %v", err)
	}

	payload, buildErr := BuildDaemonReadyPayload(nil)
	if buildErr != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload bracket: BuildDaemonReadyPayload: %v", buildErr)
	}

	after, err := MonotonicNsSinceBoot()
	if err != nil {
		t.Fatalf("PL-009 BuildDaemonReadyPayload bracket: after MonotonicNsSinceBoot: %v", err)
	}

	ns := payload.ReadyAtNsSinceBoot
	if ns < before {
		t.Errorf("PL-009 BuildDaemonReadyPayload bracket: ReadyAtNsSinceBoot %d < before %d", ns, before)
	}
	if ns > after {
		t.Errorf("PL-009 BuildDaemonReadyPayload bracket: ReadyAtNsSinceBoot %d > after %d", ns, after)
	}
}
