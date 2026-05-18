package lifecycle

// bi010d_sweep_idempotency_test.go — hk-iuaed.6 Sensor 2: orphan-sweep
// idempotency scenario harness.
//
// Spec ref: specs/beads-integration.md §4.4 BI-010d; §4.10 BI-030;
// specs/process-lifecycle.md §4.5 PL-006 sixth bullet.
//
// The PL-006 sixth-bullet sweep MUST be idempotent: running it twice in
// succession against the same project state (no intervening daemon work)
// produces zero additional `reset` writes on the second invocation. The first
// sweep already reset eligible beads (transitioning them in_progress → open);
// the second sweep queries `br list --status in_progress` and finds an empty
// set — nothing to reset.
//
// This sensor asserts that invariant via an in-process scenario harness that
// injects a stateful fake ledger and resetter. The ledger removes beads from
// its in-progress list once the resetter marks them as reset, mirroring the
// br state change that occurs in production. The second sweep call MUST
// produce resetCount=0 and call ResetBead zero additional times.
//
// Helper prefix: imrestIdmp (per implementer-protocol.md §Helper-prefix).

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// ---------------------------------------------------------------------------
// Stateful fakes for the idempotency scenario
// ---------------------------------------------------------------------------

// imrestIdmpStatefulLedger implements InFlightBeadLedger. It tracks which
// beads have been reset and omits them from subsequent ListInFlightBeads
// calls, simulating the br state transition (in_progress → open) that a
// successful ResetBead write produces.
type imrestIdmpStatefulLedger struct {
	mu      sync.Mutex
	beads   []core.BeadRecord
	resetOf map[core.BeadID]bool
}

func (f *imrestIdmpStatefulLedger) ListInFlightBeads(_ context.Context) ([]core.BeadRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []core.BeadRecord
	for _, b := range f.beads {
		if !f.resetOf[b.BeadID] {
			result = append(result, b)
		}
	}
	return result, nil
}

// markReset removes beadID from the in-progress view, simulating the br
// state change from in_progress → open after a successful reset write.
func (f *imrestIdmpStatefulLedger) markReset(beadID core.BeadID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resetOf[beadID] = true
}

// imrestIdmpCoopResetter implements BeadResetter. On each ResetBead call it
// records the bead ID and notifies the stateful ledger to remove the bead
// from subsequent in-progress queries.
type imrestIdmpCoopResetter struct {
	ledger *imrestIdmpStatefulLedger
	called []core.BeadID
}

func (r *imrestIdmpCoopResetter) ResetBead(
	_ context.Context,
	_ string,
	_ brcli.TimeoutConfig,
	beadID core.BeadID,
	_ core.ProjectHash,
	_ int64,
) error {
	r.called = append(r.called, beadID)
	r.ledger.markReset(beadID)
	return nil
}

// imrestIdmpNewLedger builds a stateful ledger pre-populated with beads.
func imrestIdmpNewLedger(beads []core.BeadRecord) *imrestIdmpStatefulLedger {
	return &imrestIdmpStatefulLedger{
		beads:   beads,
		resetOf: make(map[core.BeadID]bool),
	}
}

// imrestIdmpBaseConfig builds a SweepStaleInProgressBeadsConfig with the
// required scalar fields and a wired ProvenanceChecker that owns all beads —
// simulating a scenario where the claim-intent-based provenance check is
// bypassed via the seam (the realistic path the PL-006 sixth bullet targets).
func imrestIdmpBaseConfig(t *testing.T, ledger *imrestIdmpStatefulLedger, resetter *imrestIdmpCoopResetter, beadIDs ...core.BeadID) SweepStaleInProgressBeadsConfig {
	t.Helper()
	owns := make(map[core.BeadID]bool, len(beadIDs))
	for _, id := range beadIDs {
		owns[id] = true
	}
	return SweepStaleInProgressBeadsConfig{
		Ledger:        ledger,
		Resetter:      resetter,
		Provenance:    &imrestSweepFakeProvenance{owns: owns},
		IntentLogDir:  t.TempDir(),
		ProjectHash:   core.ProjectHash("aabbccddeeff"),
		DaemonStartNS: time.Now().UnixNano(),
	}
}

// ---------------------------------------------------------------------------
// Idempotency scenario tests
// ---------------------------------------------------------------------------

// TestSweepStaleInProgressBeads_Idempotency_SecondSweepZeroResets is the
// binding sensor for hk-iuaed.6 Sensor 2.
//
// Verifies that the orphan-sweep is idempotent: the second invocation against
// the same project state (no intervening daemon work) produces zero additional
// reset writes because the first sweep already transitioned eligible beads to
// open, and the ledger returns an empty in-progress list on the second call.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (idempotency);
// beads-integration.md §4.4 BI-010d.
func TestSweepStaleInProgressBeads_Idempotency_SecondSweepZeroResets(t *testing.T) {
	t.Parallel()

	bid := core.BeadID("hk-iuaed.sensor2")
	ledger := imrestIdmpNewLedger([]core.BeadRecord{imrestSweepBead(string(bid))})
	resetter := &imrestIdmpCoopResetter{ledger: ledger}
	cfg := imrestIdmpBaseConfig(t, ledger, resetter, bid)

	ctx := context.Background()

	// First sweep: one bead in in_progress → one reset.
	result1, err := SweepStaleInProgressBeads(ctx, cfg)
	if err != nil {
		t.Fatalf("first sweep: unexpected error: %v", err)
	}
	if result1.ResetCount != 1 {
		t.Errorf("first sweep: resetCount = %d, want 1", result1.ResetCount)
	}
	if len(resetter.called) != 1 || resetter.called[0] != bid {
		t.Errorf("first sweep: ResetBead calls = %v, want [%s]", resetter.called, bid)
	}

	// Second sweep: ledger now returns empty (bead is open); zero resets.
	beforeLen := len(resetter.called)
	result2, err := SweepStaleInProgressBeads(ctx, cfg)
	if err != nil {
		t.Fatalf("second sweep: unexpected error: %v", err)
	}
	if result2.ResetCount != 0 {
		t.Errorf("BI-010d idempotency: second sweep resetCount = %d, want 0 "+
			"(first sweep already transitioned bead to open; no in_progress beads remain)",
			result2.ResetCount,
		)
	}
	if len(resetter.called) != beforeLen {
		t.Errorf("BI-010d idempotency: second sweep issued %d additional ResetBead calls, want 0; "+
			"all calls: %v",
			len(resetter.called)-beforeLen, resetter.called,
		)
	}
}

// TestSweepStaleInProgressBeads_Idempotency_MultiBeads_SecondSweepZeroResets
// extends the single-bead idempotency test to multiple beads: after both are
// reset on the first sweep, the second sweep finds an empty in-progress list
// and issues zero resets.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (idempotency).
func TestSweepStaleInProgressBeads_Idempotency_MultiBeads_SecondSweepZeroResets(t *testing.T) {
	t.Parallel()

	bidA := core.BeadID("hk-iuaed.sensor2a")
	bidB := core.BeadID("hk-iuaed.sensor2b")
	ledger := imrestIdmpNewLedger([]core.BeadRecord{
		imrestSweepBead(string(bidA)),
		imrestSweepBead(string(bidB)),
	})
	resetter := &imrestIdmpCoopResetter{ledger: ledger}
	cfg := imrestIdmpBaseConfig(t, ledger, resetter, bidA, bidB)

	ctx := context.Background()

	// First sweep: two beads → two resets.
	result1, err := SweepStaleInProgressBeads(ctx, cfg)
	if err != nil {
		t.Fatalf("first sweep: unexpected error: %v", err)
	}
	if result1.ResetCount != 2 {
		t.Errorf("first sweep: resetCount = %d, want 2", result1.ResetCount)
	}

	// Second sweep: both beads are open; zero resets.
	beforeLen := len(resetter.called)
	result2, err := SweepStaleInProgressBeads(ctx, cfg)
	if err != nil {
		t.Fatalf("second sweep: unexpected error: %v", err)
	}
	if result2.ResetCount != 0 {
		t.Errorf("BI-010d idempotency (multi-bead): second sweep resetCount = %d, want 0", result2.ResetCount)
	}
	if len(resetter.called) != beforeLen {
		t.Errorf("BI-010d idempotency (multi-bead): second sweep issued %d additional calls, want 0",
			len(resetter.called)-beforeLen)
	}
}

// TestSweepStaleInProgressBeads_Idempotency_EmptyInitialSet_AlwaysZero
// verifies that when the initial ledger is empty (no in_progress beads at
// all), both the first and second sweeps produce zero resets — consistent
// with the idempotency invariant under the degenerate case.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet.
func TestSweepStaleInProgressBeads_Idempotency_EmptyInitialSet_AlwaysZero(t *testing.T) {
	t.Parallel()

	ledger := imrestIdmpNewLedger(nil) // no in_progress beads
	resetter := &imrestIdmpCoopResetter{ledger: ledger}
	cfg := imrestIdmpBaseConfig(t, ledger, resetter)

	ctx := context.Background()

	result1, err := SweepStaleInProgressBeads(ctx, cfg)
	if err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if result1.ResetCount != 0 {
		t.Errorf("first sweep (empty): count = %d, want 0", result1.ResetCount)
	}

	result2, err := SweepStaleInProgressBeads(ctx, cfg)
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if result2.ResetCount != 0 {
		t.Errorf("second sweep (empty): count = %d, want 0", result2.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("idempotency empty: ResetBead called %d times, want 0", len(resetter.called))
	}
}
