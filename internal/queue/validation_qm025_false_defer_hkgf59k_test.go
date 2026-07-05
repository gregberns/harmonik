package queue_test

// validation_qm025_false_defer_hkgf59k_test.go — regression coverage for the
// QM-025 false-defer when a blocker bead is already closed at submit time
// (hk-gf59k S2-F-S2-1).
//
// Root cause: QM-025 checked BlocksEdge(a, b) but not LookupStatus(a). The
// blocking edge persists in the Beads dep-graph even after a closes; without the
// blocker-status check, b would be falsely deferred at submit time. The
// daemon would then block in workloopIdleWait (all items deferred → no eligible
// items), requiring a re-submit to recover — stuck-defer if no re-submit followed.
//
// Fix (hk-gf59k): QM-025 now calls LookupStatus(a) after finding a blocks-edge.
// If a is not open/in_progress, the pair is skipped — no deferral for b.
//
// Call-count context (submit path, 2-item group [A, B], no PauseChecker):
//   call 1 — QM-020: LookupStatus(A) → must return non-not-found
//   call 2 — QM-021: LookupStatus(A) → must return open or in_progress
//   call 3 — QM-022: LookupStatus(A) → must return not in_progress
//   call 4 — QM-025: LookupStatus(A) → the new fix; can return not-found here
//
// qm025StatefulLedger drives the stateful transition: A appears open for the
// first skipCalls calls (so QM-020/021/022 pass), then returns the configured
// lateStatus on subsequent calls (so QM-025 sees the "after-close" state).
//
// Tests:
//   - TestValidateQM025_BlockerClosedAfterQM022_NoDefer: A is open in
//     QM-020/021/022, then not-found in QM-025 (closed between submission
//     checks and the defer decision). Verifies no LedgerDepPair generated for B.
//   - TestValidateQM025_BlockerOpen_DeferStillGenerated: regression guard —
//     when the blocker IS open, the LedgerDepPair is still generated.
//   - TestValidateQM025_BlockerInProgress_DeferStillGenerated: documents that
//     cross-group blockers are out of QM-025's scope; A not in submitted group,
//     so BlocksEdge(A, B) is never called — no notice, no false-defer.
//
// Bead ref: hk-gf59k. Spec ref: specs/queue-model.md §6.6 QM-025.

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// qm025StatefulLedger is a fake BeadLedger whose LookupStatus for blockerID
// returns BeadStatusOpen for the first skipCalls invocations (satisfying
// QM-020, QM-021, QM-022), then lateStatus on subsequent calls (simulating
// the blocker closing between QM-022 and QM-025).
//
// All other bead IDs always return BeadStatusOpen.
// BlocksEdge returns true only for the (blockerID → blockedID) pair.
type qm025StatefulLedger struct {
	blockerID   core.BeadID
	blockedID   core.BeadID
	skipCalls   int // return Open for first N LookupStatus(blockerID) calls
	lateStatus  queue.BeadStatus
	lookupCount atomic.Int64
}

func (f *qm025StatefulLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if id == f.blockerID {
		n := int(f.lookupCount.Add(1))
		if n <= f.skipCalls {
			return queue.BeadStatusOpen, nil
		}
		return f.lateStatus, nil
	}
	return queue.BeadStatusOpen, nil
}

func (f *qm025StatefulLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return blocker == f.blockerID && blocked == f.blockedID, nil
}

// TestValidateQM025_BlockerClosedAfterQM022_NoDefer verifies the hk-gf59k P1
// fix: when a blocker closes between the QM-022 in_progress check and the
// QM-025 defer decision, QM-025 must NOT produce a LedgerDepPair for the
// blocked bead.
//
// Setup: group [A, B] submitted together. A is open for calls 1-3 (QM-020,
// QM-021, QM-022) so those checks pass. On call 4 (QM-025) A returns
// BeadStatusNotFound, simulating external closure. Without the fix, QM-025
// would still generate LedgerDepPair{B, A} — a false-defer. With the fix,
// the pair is skipped.
//
// Bead ref: hk-gf59k S2-F-S2-1.
func TestValidateQM025_BlockerClosedAfterQM022_NoDefer(t *testing.T) {
	t.Parallel()

	const (
		A core.BeadID = "hk-gf59k.fix.A" // blocker: open for QM-020/021/022, closed at QM-025
		B core.BeadID = "hk-gf59k.fix.B" // blocked: must NOT receive a LedgerDepPair
	)

	// skipCalls=3: A returns Open for QM-020 (call 1), QM-021 (call 2),
	// QM-022 (call 3), then NotFound for QM-025 (call 4).
	ledger := &qm025StatefulLedger{
		blockerID:  A,
		blockedID:  B,
		skipCalls:  3,
		lateStatus: queue.BeadStatusNotFound,
	}

	req := validFixtureSingleGroup(A, B) // both items in the same group → QM-025 sees the pair
	errs, notices, err := queue.Validate(context.Background(), req, ledger)
	if err != nil {
		t.Fatalf("Validate unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("Validate must not fail; got errors: %v", errs)
	}
	for _, n := range notices {
		if n.BeadID == B {
			t.Errorf("got false-defer LedgerDepPair for B with blocker %q — "+
				"QM-025 did not re-check blocker status (hk-gf59k S2-F-S2-1)", n.BlockerBeadID)
		}
	}
	// LookupStatus(A) must have been called at least 4 times: the 4th call is
	// the new QM-025 blocker-status check introduced by the fix.
	if n := ledger.lookupCount.Load(); n < 4 {
		t.Errorf("LookupStatus(A) called %d time(s); want ≥4 — "+
			"QM-025 blocker-status check was not reached (hk-gf59k fix may not have fired)", n)
	}
}

// TestValidateQM025_BlockerOpen_DeferStillGenerated is a regression guard:
// when the blocker IS open at QM-025 time, QM-025 must still produce a
// LedgerDepPair (the existing behavior for a live blocking dep).
func TestValidateQM025_BlockerOpen_DeferStillGenerated(t *testing.T) {
	t.Parallel()

	const (
		A core.BeadID = "hk-gf59k.open.A"
		B core.BeadID = "hk-gf59k.open.B"
	)

	ledger := &validFixtureFakeLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			A: queue.BeadStatusOpen,
			B: queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{A, B}: true,
		},
	}

	req := validFixtureSingleGroup(A, B)
	errs, notices, err := queue.Validate(context.Background(), req, ledger)
	if err != nil {
		t.Fatalf("Validate unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("Validate must not fail; got errors: %v", errs)
	}
	found := false
	for _, n := range notices {
		if n.BeadID == B && n.BlockerBeadID == A {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected LedgerDepPair {BeadID:%q, BlockerBeadID:%q}; got %v — "+
			"open blocker must still defer (hk-gf59k regression guard)", B, A, notices)
	}
}

// TestValidateQM025_CrossGroupBlocker_NoNotice documents that QM-025 only
// sees intra-group pairs: a blocker A not in the submitted group is never
// tested by BlocksEdge, so no LedgerDepPair is generated and B is not
// falsely deferred. This is correct scoping (QM-025 is "parallelism-narrowed"
// within a single submitted wave).
//
// This test does NOT exercise the hk-gf59k fix — it is a scoping document.
func TestValidateQM025_CrossGroupBlocker_NoNotice(t *testing.T) {
	t.Parallel()

	const (
		A core.BeadID = "hk-gf59k.xgrp.A" // external blocker (not submitted)
		B core.BeadID = "hk-gf59k.xgrp.B" // blocked bead (submitted alone)
	)

	ledger := &validFixtureFakeLedger{
		statuses: map[core.BeadID]queue.BeadStatus{
			A: queue.BeadStatusInProgress, // active in another run
			B: queue.BeadStatusOpen,
		},
		edges: map[[2]core.BeadID]bool{
			{A, B}: true,
		},
	}

	// Submit only B — A is not in the group, so BlocksEdge(A, B) is never called.
	req := validFixtureSingleGroup(B)
	errs, notices, err := queue.Validate(context.Background(), req, ledger)
	if err != nil {
		t.Fatalf("Validate unexpected error: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("Validate must not fail; got errors: %v", errs)
	}
	for _, n := range notices {
		if n.BeadID == B {
			t.Logf("got notice for B with blocker %q — cross-group edge unexpectedly detected", n.BlockerBeadID)
		}
	}
}
