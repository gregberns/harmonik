package daemon_test

// workloop_queue_oob_close_hk3kq05_test.go — out-of-band promote wedge fix
// (hk-3kq05 / BI-013c terminal-bead routing).
//
// Root cause: BI-013c set queue items to deferred-for-ledger-dep even when the
// bead was CLOSED (promoted out-of-band). ReevaluateDeferred then un-deferred
// the item every tick (no open in-group blocker), so streamEligible always
// picked the earlier-indexed item before downstream pending entries, causing an
// infinite deferred→pending→deferred cycle. Downstream beads (e.g. hk-p195/CD1)
// were perpetually starved.
//
// Fix: BI-013c now detects closed/tombstone status and calls
// evaluateGroupAdvanceWithOutcome(fail), marking the item terminal so
// allItemsTerminal can complete the group.
//
// This test verifies (for closed status):
//  1. ClaimBead is never called.
//  2. The queue item transitions to ItemStatusFailed (terminal), NOT deferred-for-ledger-dep.
//  3. A bead_claim_skipped event is emitted with observed_status=closed.
//
// Helper prefix: queueOOBClose (per implementer-protocol.md §Helper-prefix).

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// queueOOBCloseLedger is a stub BeadLedger where ShowBead returns a fixed
// terminal status for every bead (simulating out-of-band promote or tombstone).
// ClaimBead records any call so the test can assert it was never invoked.
type queueOOBCloseLedger struct {
	mu         sync.Mutex
	claimCalls atomic.Int64
	showCalls  atomic.Int64
	// showSeen is closed after ShowBead has been called at least once, letting
	// the test wait until the BI-013c guard has fired.
	showSeen     chan struct{}
	showOnce     sync.Once
	returnStatus core.CoarseStatus
}

func newQueueOOBCloseLedger() *queueOOBCloseLedger {
	return &queueOOBCloseLedger{showSeen: make(chan struct{}), returnStatus: core.CoarseStatusClosed}
}

func newQueueOOBStatusLedger(status core.CoarseStatus) *queueOOBCloseLedger {
	return &queueOOBCloseLedger{showSeen: make(chan struct{}), returnStatus: status}
}

func (l *queueOOBCloseLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (l *queueOOBCloseLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.showCalls.Add(1)
	l.showOnce.Do(func() { close(l.showSeen) })
	return core.BeadRecord{BeadID: id, Status: l.returnStatus}, nil
}

func (l *queueOOBCloseLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.claimCalls.Add(1)
	return nil
}

func (l *queueOOBCloseLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return nil
}

func (l *queueOOBCloseLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// TestWorkLoop_QueuePath_ClosedBeadReconciled verifies that a queue-path item
// whose bead.status=closed (promoted out-of-band) is reconciled to
// ItemStatusFailed (terminal) by the BI-013c guard, rather than cycling
// deferred-for-ledger-dep→pending indefinitely (hk-3kq05).
//
// Bead ref: hk-3kq05.
func TestWorkLoop_QueuePath_ClosedBeadReconciled(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-3kq05-closed-oob")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "3kq05-closed-oob-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := newQueueOOBCloseLedger()
	bus := &stubEventCollector{}

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Wait until BI-013c has fired (ShowBead returned closed status).
	select {
	case <-ledger.showSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ShowBead was never called — BI-013c guard did not fire within 15s (hk-3kq05)")
	}

	// Poll for the item to reach ItemStatusFailed (terminal).
	// Without the fix: item cycles deferred-for-ledger-dep and never reaches Failed.
	var item queue.Item
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		inFlightQ := daemon.ExportedQueueStoreOf(deps).Queue()
		if inFlightQ != nil && len(inFlightQ.Groups) > 0 && len(inFlightQ.Groups[0].Items) > 0 {
			item = inFlightQ.Groups[0].Items[0]
			if item.Status == queue.ItemStatusFailed {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if item.Status != queue.ItemStatusFailed {
		t.Errorf("item status = %q; want %q — closed bead must be reconciled to failed (hk-3kq05)",
			item.Status, queue.ItemStatusFailed)
	}

	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel (hk-3kq05)")
	}

	// Assert ClaimBead was never called (bead was closed, not claimable).
	if n := ledger.claimCalls.Load(); n != 0 {
		t.Errorf("ClaimBead call count = %d; want 0 — closed bead must not be claimed (hk-3kq05)", n)
	}

	// Assert bead_claim_skipped was emitted with observed_status=closed.
	found := false
	for _, evt := range bus.allEvents() {
		if evt.EventType != string(core.EventTypeBeadClaimSkipped) {
			continue
		}
		var p core.BeadClaimSkippedPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			t.Errorf("bead_claim_skipped payload unmarshal error: %v", err)
			continue
		}
		if p.BeadID != string(beadID) {
			continue
		}
		if p.ObservedStatus != string(core.CoarseStatusClosed) {
			t.Errorf("bead_claim_skipped observed_status = %q; want %q (hk-3kq05)",
				p.ObservedStatus, core.CoarseStatusClosed)
		}
		if p.Reason != "status_changed_between_select_and_claim" {
			t.Errorf("bead_claim_skipped reason = %q; want %q (hk-3kq05)",
				p.Reason, "status_changed_between_select_and_claim")
		}
		found = true
		break
	}
	if !found {
		t.Errorf("bead_claim_skipped event for bead %s not found in bus (hk-3kq05)", beadID)
	}
}

// TestWorkLoop_QueuePath_TombstoneBeadReconciled verifies that the BI-013c
// terminal path also fires for CoarseStatusTombstone (hk-3kq05).
func TestWorkLoop_QueuePath_TombstoneBeadReconciled(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-3kq05-tombstone-oob")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "3kq05-tombstone-oob-test",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadID, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := newQueueOOBStatusLedger(core.CoarseStatusTombstone)
	bus := &stubEventCollector{}

	loopCtx, cancelLoop := context.WithCancel(context.Background())
	defer cancelLoop()

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(loopCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case <-ledger.showSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ShowBead was never called — BI-013c guard did not fire (hk-3kq05 tombstone path)")
	}

	var item queue.Item
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		inFlightQ := daemon.ExportedQueueStoreOf(deps).Queue()
		if inFlightQ != nil && len(inFlightQ.Groups) > 0 && len(inFlightQ.Groups[0].Items) > 0 {
			item = inFlightQ.Groups[0].Items[0]
			if item.Status == queue.ItemStatusFailed {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if item.Status != queue.ItemStatusFailed {
		t.Errorf("tombstone item status = %q; want %q — tombstone bead must be reconciled to failed (hk-3kq05)",
			item.Status, queue.ItemStatusFailed)
	}

	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel (hk-3kq05 tombstone)")
	}

	if n := ledger.claimCalls.Load(); n != 0 {
		t.Errorf("ClaimBead call count = %d; want 0 — tombstone bead must not be claimed (hk-3kq05)", n)
	}
}
