package daemon

// quiesce_sleep_veto_hkzqb3_test.go — HandleDaemonSleep SS-INV-005 veto gate (P1-c, hk-zqb3).
//
// Scenarios covered:
//
//  1. drain detector nil → gate skipped; sleep proceeds.
//  2. drain detector wired, fleet fully drained → sleep proceeds.
//  3. drain detector wired, ready beads present → vetoed with ready-bead reason.
//  4. drain detector wired, in-progress beads present → vetoed.
//  5. drain detector wired, in-flight registry runs present → vetoed.
//  6. drain detector wired, non-terminal queue items → vetoed.
//  7. drain detector wired, GatherDrainFacts returns error → vetoed (fail-closed).
//  8. drain detector wired, facts.Unsure true → vetoed (fail-closed).
//  9. force=true bypasses the gate even when work is present → sleep proceeds.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// newVetoTestArbiter returns a minimal QuiesceArbiter wired for veto-gate tests.
// The arbiter has no tmux adapter, no comms bus, and a short poll interval so
// it does not consume significant test time.  The caller is responsible for
// wiring a DrainDetector via SetDrain before calling HandleDaemonSleep.
func newVetoTestArbiter(t *testing.T) *QuiesceArbiter {
	t.Helper()
	arbiter, _, _ := newTestQuiesceArbiter(t, t.TempDir(), NewQueueStore(), 0, 0)
	return arbiter
}

// buildDrainedDetector returns a DrainDetector that reports a fully drained
// fleet (no ready beads, no in-progress, no runs, no queue items).
func buildDrainedDetector(t *testing.T) *DrainDetector {
	t.Helper()
	return NewDrainDetector(
		drainedReady(),
		drainedFullLister(),
		drainedLedger(),
		NewRunRegistry(),
		NewQueueStore(),
		emptyTestProjectDir(t),
	)
}

// TestHandleDaemonSleep_NilDrain_Allowed asserts that when no drain detector
// is wired the sleep gate is skipped and parkAllSessions is called.
func TestHandleDaemonSleep_NilDrain_Allowed(t *testing.T) {
	a := newVetoTestArbiter(t)
	// drain is nil by default — gate skipped.
	if err := a.HandleDaemonSleep(context.Background(), false); err != nil {
		t.Fatalf("HandleDaemonSleep with nil drain: want nil error, got %v", err)
	}
}

// TestHandleDaemonSleep_DrainedFleet_Allowed asserts that a fully drained fleet
// passes the veto gate and the sleep proceeds without error.
func TestHandleDaemonSleep_DrainedFleet_Allowed(t *testing.T) {
	a := newVetoTestArbiter(t)
	a.SetDrain(buildDrainedDetector(t))

	if err := a.HandleDaemonSleep(context.Background(), false); err != nil {
		t.Fatalf("HandleDaemonSleep on drained fleet: want nil error, got %v", err)
	}
}

// TestHandleDaemonSleep_ReadyBeads_Vetoed asserts that ready (dispatchable) beads
// trigger the veto.
func TestHandleDaemonSleep_ReadyBeads_Vetoed(t *testing.T) {
	a := newVetoTestArbiter(t)
	ready := &fakeReady{records: []core.BeadRecord{{BeadID: "hk-ready-1", Title: "work"}}}
	det := NewDrainDetector(ready, drainedFullLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))
	a.SetDrain(det)

	err := a.HandleDaemonSleep(context.Background(), false)
	if err == nil {
		t.Fatal("HandleDaemonSleep with ready beads: want veto error, got nil")
	}
	if !strings.Contains(err.Error(), "ready bead") {
		t.Errorf("veto error %q does not mention ready bead", err.Error())
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("veto error %q does not mention --force override", err.Error())
	}
}

// TestHandleDaemonSleep_InProgressBeads_Vetoed asserts that in-progress beads
// trigger the veto.
func TestHandleDaemonSleep_InProgressBeads_Vetoed(t *testing.T) {
	a := newVetoTestArbiter(t)
	lister := &fullLister{byStatus: map[string][]core.BeadRecord{
		string(core.CoarseStatusInProgress): {
			minimalBead("hk-running", core.CoarseStatusInProgress, "task"),
		},
	}}
	det := NewDrainDetector(drainedReady(), lister, drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))
	a.SetDrain(det)

	err := a.HandleDaemonSleep(context.Background(), false)
	if err == nil {
		t.Fatal("HandleDaemonSleep with in-progress beads: want veto error, got nil")
	}
	if !strings.Contains(err.Error(), "in-progress bead") {
		t.Errorf("veto error %q does not mention in-progress bead", err.Error())
	}
}

// TestHandleDaemonSleep_InFlightRuns_Vetoed asserts that registry in-flight runs
// trigger the veto.
func TestHandleDaemonSleep_InFlightRuns_Vetoed(t *testing.T) {
	a := newVetoTestArbiter(t)
	runs := NewRunRegistry()
	runs.Register(
		core.RunID(uuid.MustParse("01960084-0000-7000-8000-000000000abc")),
		&RunHandle{BeadID: "hk-running"},
	)
	det := NewDrainDetector(drainedReady(), drainedFullLister(), drainedLedger(), runs, NewQueueStore(), emptyTestProjectDir(t))
	a.SetDrain(det)

	err := a.HandleDaemonSleep(context.Background(), false)
	if err == nil {
		t.Fatal("HandleDaemonSleep with in-flight run: want veto error, got nil")
	}
	if !strings.Contains(err.Error(), "in-flight run") {
		t.Errorf("veto error %q does not mention in-flight run", err.Error())
	}
}

// TestHandleDaemonSleep_QueuedItems_Vetoed asserts that non-terminal queue items
// trigger the veto.
func TestHandleDaemonSleep_QueuedItems_Vetoed(t *testing.T) {
	a := newVetoTestArbiter(t)
	qs := NewQueueStore()
	qs.SetQueue(buildActiveQueueWithPendingItem(t))
	det := NewDrainDetector(drainedReady(), drainedFullLister(), drainedLedger(), NewRunRegistry(), qs, emptyTestProjectDir(t))
	a.SetDrain(det)

	err := a.HandleDaemonSleep(context.Background(), false)
	if err == nil {
		t.Fatal("HandleDaemonSleep with queued items: want veto error, got nil")
	}
	if !strings.Contains(err.Error(), "queued item") {
		t.Errorf("veto error %q does not mention queued item", err.Error())
	}
}

// TestHandleDaemonSleep_GatherError_Vetoed asserts that a GatherDrainFacts error
// causes a veto (fail-closed).
func TestHandleDaemonSleep_GatherError_Vetoed(t *testing.T) {
	a := newVetoTestArbiter(t)
	sentinel := errors.New("br unavailable")
	det := NewDrainDetector(&fakeReady{err: sentinel}, drainedFullLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))
	a.SetDrain(det)

	err := a.HandleDaemonSleep(context.Background(), false)
	if err == nil {
		t.Fatal("HandleDaemonSleep when GatherDrainFacts errors: want veto error, got nil")
	}
	if !strings.Contains(err.Error(), "cannot determine fleet state") {
		t.Errorf("veto error %q does not mention 'cannot determine fleet state'", err.Error())
	}
}

// TestHandleDaemonSleep_Unsure_Vetoed asserts that an Unsure fleet state causes
// a veto (fail-closed).
func TestHandleDaemonSleep_Unsure_Vetoed(t *testing.T) {
	a := newVetoTestArbiter(t)
	// Nil lister/ledger causes facts.Unsure = true.
	det := NewDrainDetector(drainedReady(), nil, nil, NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))
	a.SetDrain(det)

	err := a.HandleDaemonSleep(context.Background(), false)
	if err == nil {
		t.Fatal("HandleDaemonSleep when fleet state is Unsure: want veto error, got nil")
	}
	if !strings.Contains(err.Error(), "fleet state uncertain") {
		t.Errorf("veto error %q does not mention 'fleet state uncertain'", err.Error())
	}
}

// TestHandleDaemonSleep_Force_BypassesVeto asserts that force=true bypasses the
// veto gate even when the fleet has ready beads.
func TestHandleDaemonSleep_Force_BypassesVeto(t *testing.T) {
	a := newVetoTestArbiter(t)
	ready := &fakeReady{records: []core.BeadRecord{{BeadID: "hk-ready-1"}}}
	det := NewDrainDetector(ready, drainedFullLister(), drainedLedger(), NewRunRegistry(), NewQueueStore(), emptyTestProjectDir(t))
	a.SetDrain(det)

	// force=true: veto gate skipped even with ready beads.
	if err := a.HandleDaemonSleep(context.Background(), true); err != nil {
		t.Fatalf("HandleDaemonSleep force=true with ready beads: want nil error, got %v", err)
	}
}

// buildActiveQueueWithPendingItem returns a *queue.Queue in active status with one
// pending item, suitable for use with QueueStore.SetQueue in veto gate tests.
func buildActiveQueueWithPendingItem(t *testing.T) *queue.Queue {
	t.Helper()
	return &queue.Queue{
		SchemaVersion: 1,
		Name:          "test-veto-queue",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				Items: []queue.Item{
					{BeadID: "hk-queued-1", Status: queue.ItemStatusPending},
				},
			},
		},
	}
}
