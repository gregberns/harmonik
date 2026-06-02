package daemon_test

// workloop_queue_deferred_hk6ri5k_test.go — queue-path deferred-status guard (hk-6ri5k).
//
// Root cause: the queue-path in workloop.go stamped a bead as dispatched and
// proceeded to ClaimBead even when the bead's status was deferred (or any
// non-open status). The br-ready path had a ShowBead pre-claim guard
// (hk-p4xbw) that caught this, but the queue path skipped it.
//
// Fix: insert a ShowBead status check for queue-path items between Phase 2
// (handler-pause gate) and Phase 3 (write-lock stamp). If the bead is not
// open, the loop holds the item in pending without incrementing Attempts and
// retries on the next tick.
//
// This test verifies:
//   1. ClaimBead is never called when the bead is deferred.
//   2. The queue item stays pending with Attempts == 0.
//
// Helper prefix: queueDeferredGuard (per implementer-protocol.md
// §Helper-prefix discipline; bead hk-6ri5k).

import (
	"context"
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
	// the test cancel the workloop as soon as the guard fires.
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

// TestWorkLoop_QueuePath_DeferredBeadHeld verifies that a queue-path item
// whose bead.status=deferred is held (not claimed, no Attempts increment)
// until the bead becomes open.
//
// Bead ref: hk-6ri5k.
func TestWorkLoop_QueuePath_DeferredBeadHeld(t *testing.T) {
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

	// cancelLoop is used to stop the workloop after we've confirmed the guard
	// has fired (showSeen channel is closed).
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

	// Wait until the guard has fired at least once (ShowBead observed deferred
	// status and skipped Phase 3).
	select {
	case <-ledger.showSeen:
	case <-time.After(15 * time.Second):
		t.Fatal("ShowBead was never called — deferred guard did not fire within 15s (hk-6ri5k)")
	}

	// Read queue state NOW — before cancelling — because drainCancelledQueue
	// clears the store on clean exit. The guard has fired but Phase 3 has not
	// run; the item must still be pending with Attempts == 0.
	inFlightQ := daemon.ExportedQueueStoreOf(deps).Queue()
	if inFlightQ == nil {
		t.Fatal("queue is nil after guard fired but before cancel")
	}
	if len(inFlightQ.Groups) == 0 {
		t.Fatal("queue has no groups")
	}
	item := inFlightQ.Groups[0].Items[0]
	if item.Status != queue.ItemStatusPending {
		t.Errorf("item status = %q; want %q — deferred bead must remain pending (hk-6ri5k)",
			item.Status, queue.ItemStatusPending)
	}
	if item.Attempts != 0 {
		t.Errorf("item.Attempts = %d; want 0 — deferred guard must not consume attempts (hk-6ri5k)",
			item.Attempts)
	}

	// Now cancel the loop.
	cancelLoop()

	// Drain the loop.
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runWorkLoop did not exit after context cancel (hk-6ri5k)")
	}

	// ── Assert ClaimBead was never called ────────────────────────────────────

	if n := ledger.claimCalls.Load(); n != 0 {
		t.Errorf("ClaimBead call count = %d; want 0 — deferred bead must not be claimed (hk-6ri5k)", n)
	}
}
