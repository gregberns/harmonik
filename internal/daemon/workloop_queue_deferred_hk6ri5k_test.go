package daemon_test

// workloop_queue_deferred_hk6ri5k_test.go — queue-path non-open status guard (hk-6ri5k / BI-013c).
//
// Root cause: the queue-path in workloop.go stamped a bead as dispatched and
// proceeded to ClaimBead even when the bead's status was non-open. The
// br-ready path had a ShowBead pre-claim guard (hk-p4xbw) that caught this,
// but the queue path skipped it.
//
// Fix (BI-013c): insert a ShowBead status check for queue-path items between
// Phase 2 (handler-pause gate) and Phase 3 (write-lock stamp). If the bead is
// not open, the loop emits bead_claim_skipped and sets the item to
// deferred-for-ledger-dep without incrementing Attempts (per BI-013c).
//
// This test verifies:
//  1. ClaimBead is never called when the bead is deferred.
//  2. The queue item transitions to deferred-for-ledger-dep (not pending) with Attempts == 0.
//  3. A bead_claim_skipped event is emitted with the correct observed_status.
//
// Helper prefix: queueDeferredGuard (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-6ri5k).

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

// queueDeferredGuardLedger is a stub BeadLedger where ShowBead returns
// CoarseStatusDeferred for every bead. ClaimBead records any call so the test
// can assert it was never invoked.
type queueDeferredGuardLedger struct {
	mu         sync.Mutex
	claimCalls atomic.Int64
	showCalls  atomic.Int64
	// showSeen is closed after ShowBead has been called at least once, letting
	// the test wait until the guard has fired.
	showSeen chan struct{}
	showOnce sync.Once
}

func newQueueDeferredGuardLedger() *queueDeferredGuardLedger {
	return &queueDeferredGuardLedger{showSeen: make(chan struct{})}
}

func (l *queueDeferredGuardLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (l *queueDeferredGuardLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.showCalls.Add(1)
	l.showOnce.Do(func() { close(l.showSeen) })
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusDeferred}, nil
}

func (l *queueDeferredGuardLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.claimCalls.Add(1)
	return nil
}

func (l *queueDeferredGuardLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return nil
}

func (l *queueDeferredGuardLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// TestWorkLoop_QueuePath_DeferredBeadSkipped verifies that a queue-path item
// whose bead.status=deferred is skipped per BI-013c: no claim is attempted,
// a bead_claim_skipped event is emitted, and the item transitions to
// deferred-for-ledger-dep with Attempts == 0.
//
// Bead ref: hk-6ri5k.
func TestWorkLoop_QueuePath_DeferredBeadSkipped(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-6ri5k-deferred")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "6ri5k-deferred-guard-test",
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

	ledger := newQueueDeferredGuardLedger()
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

	// Wait until the guard has fired at least once (ShowBead observed deferred status).
	select {
	case <-ledger.showSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ShowBead was never called — BI-013c guard did not fire within 15s (hk-6ri5k)")
	}

	// Poll for the item to reach deferred-for-ledger-dep (the write lock
	// completes asynchronously after ShowBead returns).
	var item queue.Item
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		inFlightQ := daemon.ExportedQueueStoreOf(deps).Queue()
		if inFlightQ != nil && len(inFlightQ.Groups) > 0 && len(inFlightQ.Groups[0].Items) > 0 {
			item = inFlightQ.Groups[0].Items[0]
			if item.Status == queue.ItemStatusDeferredForLedgerDep {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if item.Status != queue.ItemStatusDeferredForLedgerDep {
		t.Errorf("item status = %q; want %q — BI-013c: deferred bead must transition to deferred-for-ledger-dep (hk-6ri5k)",
			item.Status, queue.ItemStatusDeferredForLedgerDep)
	}
	if item.Attempts != 0 {
		t.Errorf("item.Attempts = %d; want 0 — BI-013c guard must not consume attempts (hk-6ri5k)",
			item.Attempts)
	}

	// Cancel the loop before checking events (bus is populated asynchronously).
	cancelLoop()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel (hk-6ri5k)")
	}

	// Assert ClaimBead was never called.
	if n := ledger.claimCalls.Load(); n != 0 {
		t.Errorf("ClaimBead call count = %d; want 0 — deferred bead must not be claimed (hk-6ri5k)", n)
	}

	// Assert bead_claim_skipped event was emitted (BI-013c).
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
		if p.ObservedStatus != string(core.CoarseStatusDeferred) {
			t.Errorf("bead_claim_skipped observed_status = %q; want %q (BI-013c)",
				p.ObservedStatus, core.CoarseStatusDeferred)
		}
		if p.Reason != "status_changed_between_select_and_claim" {
			t.Errorf("bead_claim_skipped reason = %q; want %q (BI-013c)",
				p.Reason, "status_changed_between_select_and_claim")
		}
		found = true
		break
	}
	if !found {
		t.Errorf("bead_claim_skipped event for bead %s not found in bus (BI-013c)", beadID)
	}
}
