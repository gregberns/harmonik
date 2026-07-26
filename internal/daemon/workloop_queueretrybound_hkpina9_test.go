package daemon_test

// workloop_queueretrybound_hkpina9_test.go — queue-path pre-claim ShowBead
// bounded-retry tests (hk-pina9).
//
// Purpose: prove that a queue item whose pre-claim `br show` keeps failing is
// abandoned after maxItemAttempts and the QUEUE ADVANCES, instead of the item
// sitting pending at the head of its group and being re-selected every tick
// forever (the wedge hk-pina9 fixes).
//
// Cases:
//   - TestWorkLoop_QueuePreClaimShowBeadBoundedAndQueueAdvances: ShowBead fails
//     permanently for the stream head; the head item must reach ItemStatusFailed
//     with LastFailureReason="show_bead_failed" AND the NEXT item in the stream
//     must actually be claimed/dispatched. Head-of-line blocking is the whole
//     defect, so "the next bead dispatched" is the load-bearing assertion — not
//     merely "the loop exited".
//   - TestWorkLoop_QueuePreClaimShowBeadTransientErrorStillDispatches: ShowBead
//     fails twice (under the bound of 3) then succeeds; the item must still be
//     claimed. Guards against a bound that breaks legitimate retry.
//   - TestWorkLoop_QueuePreClaimShowBeadRetryShutdownExitsClean: cancelling the
//     context while the retry loop is mid-sleep must still exit cleanly.
//
// Helper prefix: pina9
//
// Bead ref: hk-pina9. Sibling (br-ready path): hk-kupeo / hk-6pspu / hk-fvpz5.

import (
	"context"
	"errors"
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

// pina9Ledger is a stub bead ledger whose ShowBead fails for one designated
// bead. failBudget controls how many times it fails:
//
//	failBudget < 0 → fail forever (permanent-error case)
//	failBudget = n → fail the first n calls, then succeed (transient case)
//
// Every other bead resolves as open so the workloop takes the normal path.
type pina9Ledger struct {
	mu sync.Mutex

	failBead   core.BeadID
	failBudget int32 // <0 = always fail; otherwise remaining failures

	showCalls map[core.BeadID]int32
	claimed   []core.BeadID
	closed    []core.BeadID
	reopened  []core.BeadID

	showCallTotal atomic.Int32
}

func newPina9Ledger(failBead core.BeadID, failBudget int32) *pina9Ledger {
	return &pina9Ledger{
		failBead:   failBead,
		failBudget: failBudget,
		showCalls:  make(map[core.BeadID]int32),
	}
}

// Ready returns nothing: these tests exercise the QUEUE path only. A non-empty
// ready list would let the br-ready fallback mask a wedged queue.
func (p *pina9Ledger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil
}

func (p *pina9Ledger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	p.showCallTotal.Add(1)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.showCalls[id]++

	if id == p.failBead {
		if p.failBudget < 0 {
			return core.BeadRecord{}, errors.New("br show: simulated permanent ledger error")
		}
		if p.failBudget > 0 {
			p.failBudget--
			return core.BeadRecord{}, errors.New("br show: simulated transient ledger error")
		}
	}
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "pina9-test-bead"}, nil
}

func (p *pina9Ledger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claimed = append(p.claimed, id)
	return nil
}

func (p *pina9Ledger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = append(p.closed, id)
	return nil
}

func (p *pina9Ledger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reopened = append(p.reopened, id)
	return nil
}

func (p *pina9Ledger) showCallCount(id core.BeadID) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.showCalls[id]
}

func (p *pina9Ledger) claimedIDs() []core.BeadID {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]core.BeadID, len(p.claimed))
	copy(out, p.claimed)
	return out
}

func (p *pina9Ledger) wasClaimed(id core.BeadID) bool {
	for _, got := range p.claimedIDs() {
		if got == id {
			return true
		}
	}
	return false
}

// pina9StreamQueue builds a single-group STREAM queue over beads. Stream (not
// wave) is deliberate: streamEligible returns ONLY the earliest eligible item,
// so beads[1:] are head-of-line blocked by beads[0]. That is the exact shape of
// the wedge — a wave group would let a sibling slip past and hide it.
func pina9StreamQueue(queueID string, beads ...core.BeadID) *queue.Queue {
	now := time.Now()
	items := make([]queue.Item, 0, len(beads))
	for _, b := range beads {
		items = append(items, queue.Item{BeadID: b, Status: queue.ItemStatusPending})
	}
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items:      items,
				CreatedAt:  now,
			},
		},
	}
}

// pina9WaitFor polls cond until true or the deadline elapses. Returns false on
// timeout. Every wait in this file is bounded: the symptom of the unfixed bug
// is an infinite retry loop, so an unbounded test wait would be
// indistinguishable from the bug it is meant to catch.
func pina9WaitFor(d time.Duration, cond func() bool) bool {
	deadline := time.After(d)
	for {
		if cond() {
			return true
		}
		select {
		case <-deadline:
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// pina9AwaitLoopExit fails the test if the work loop has not returned within d.
func pina9AwaitLoopExit(t *testing.T, loopDone <-chan error, d time.Duration) {
	t.Helper()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned error = %v; want nil (clean exit)", err)
		}
	case <-time.After(d):
		t.Fatalf("work loop did not exit within %s after context cancellation", d)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Case (a) — permanent ShowBead error: bounded AND the queue advances
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_QueuePreClaimShowBeadBoundedAndQueueAdvances verifies that a
// queue item whose pre-claim ShowBead errors on every attempt is failed after
// maxItemAttempts and the next item in the same stream group is dispatched.
//
// Regression shape being pinned: before hk-pina9 the queue-path guard had NO
// attempt counter — it logged, slept, and `continue`d, leaving the item pending
// at the head of its group so it was re-selected every tick forever. beadNext
// was never claimed and the queue was wedged permanently.
func TestWorkLoop_QueuePreClaimShowBeadBoundedAndQueueAdvances(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadWedge = core.BeadID("hk-pina9-show-fails")
		beadNext  = core.BeadID("hk-pina9-next-in-stream")
		queueID   = "pina9-bounded-advance"
	)

	ledger := newPina9Ledger(beadWedge, -1) // fail forever
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(pina9StreamQueue(queueID, beadWedge, beadNext))

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Budget: 3 pre-claim attempts * 2s poll interval ≈ 4s, plus one dispatch
	// of beadNext (worktree + handler). 45s is generous but still bounded.
	advanced := pina9WaitFor(45*time.Second, func() bool { return ledger.wasClaimed(beadNext) })
	// Snapshot the queue BEFORE cancelling: exitClean drains the active queue on
	// shutdown, after which QueueStore.Queue() is nil.
	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	cancel()
	pina9AwaitLoopExit(t, loopDone, 25*time.Second)

	if !advanced {
		t.Fatalf("beadNext (%s) was never claimed; want it dispatched after the wedged head item exhausted its %d pre-claim attempts — regression: the queue-path ShowBead guard retries forever and head-of-line blocks the group; claimed=%v",
			beadNext, queue.MaxItemAttempts, ledger.claimedIDs())
	}

	if got := ledger.wasClaimed(beadWedge); got {
		t.Errorf("wasClaimed(%s) = %v; want false (ShowBead never succeeded, so it must never be claimed)", beadWedge, got)
	}

	// ShowBead must be called a BOUNDED number of times for the wedged bead.
	// Exactly maxItemAttempts is expected; allow a small margin for a re-select
	// racing the terminal write, but keep the ceiling tight enough that an
	// unbounded retry (dozens of calls over 45s) fails loudly.
	if got, wantMax := ledger.showCallCount(beadWedge), int32(queue.MaxItemAttempts+2); got > wantMax {
		t.Errorf("ShowBead call count for %s = %d; want <= %d (bounded by maxItemAttempts=%d) — regression: unbounded pre-claim retry",
			beadWedge, got, wantMax, queue.MaxItemAttempts)
	}
	if got := ledger.showCallCount(beadWedge); got == 0 {
		t.Errorf("ShowBead call count for %s = 0; want >= 1 (test setup error: the guard was never reached)", beadWedge)
	}

	if finalQ == nil {
		t.Fatal("queue = nil while the work loop was still running; want the pina9 test queue")
	}
	wedgeItem := finalQ.Groups[0].Items[0]
	if wedgeItem.BeadID != beadWedge {
		t.Fatalf("item[0].BeadID = %q; want %q (test setup error)", wedgeItem.BeadID, beadWedge)
	}
	if got, want := wedgeItem.Status, queue.ItemStatusFailed; got != want {
		t.Errorf("wedged item Status = %q; want %q — regression: a bound that leaves the item pending does not unwedge the group, and a non-terminal status blocks allItemsTerminal so the group never advances",
			got, want)
	}
	if got, want := wedgeItem.LastFailureReason, "show_bead_failed"; got != want {
		t.Errorf("wedged item LastFailureReason = %q; want %q (operators need the WHY in queue.json)", got, want)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Case (b) — transient ShowBead error under the bound still dispatches
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_QueuePreClaimShowBeadTransientErrorStillDispatches verifies the
// bound does not break legitimate retry: ShowBead fails twice (one under
// maxItemAttempts=3) and then succeeds, and the item must still be claimed.
//
// Regression shape being pinned: a bound that counts failures against the
// item's persisted Attempts budget — or that fails the item on the FIRST error
// — would kill a bead that was about to recover.
func TestWorkLoop_QueuePreClaimShowBeadTransientErrorStillDispatches(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadFlaky = core.BeadID("hk-pina9-transient")
		queueID   = "pina9-transient"
	)
	// maxItemAttempts is 3; fail one fewer time so the third call succeeds.
	transientFailures := int32(queue.MaxItemAttempts - 1)

	ledger := newPina9Ledger(beadFlaky, transientFailures)
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(pina9StreamQueue(queueID, beadFlaky))

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	dispatched := pina9WaitFor(45*time.Second, func() bool { return ledger.wasClaimed(beadFlaky) })
	// Snapshot before cancelling — exitClean drains the queue on shutdown.
	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	cancel()
	pina9AwaitLoopExit(t, loopDone, 25*time.Second)

	if !dispatched {
		t.Fatalf("wasClaimed(%s) = false; want true after %d transient ShowBead errors (under maxItemAttempts=%d) — regression: the retry bound abandons a bead that would have recovered; claimed=%v",
			beadFlaky, transientFailures, queue.MaxItemAttempts, ledger.claimedIDs())
	}

	if finalQ == nil {
		t.Fatal("queue = nil while the work loop was still running; want the pina9 test queue")
	}
	if got, notWant := finalQ.Groups[0].Items[0].LastFailureReason, "show_bead_failed"; got == notWant {
		t.Errorf("item LastFailureReason = %q; want anything but %q (transient errors under the bound must not fail the item)", got, notWant)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Case (c) — shutdown mid-retry still exits cleanly
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_QueuePreClaimShowBeadRetryShutdownExitsClean verifies that
// cancelling the dispatch context while the pre-claim retry is in flight still
// exits the loop cleanly. hk-pina9 inserted a new branch into that guard; the
// dispatchCtx.Err() early return and the workloopSleep cancellation path must
// be unchanged.
func TestWorkLoop_QueuePreClaimShowBeadRetryShutdownExitsClean(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadWedge = core.BeadID("hk-pina9-shutdown")
		queueID   = "pina9-shutdown"
	)

	ledger := newPina9Ledger(beadWedge, -1) // fail forever
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(pina9StreamQueue(queueID, beadWedge))

	p := daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		MaxConcurrent:    1,
		AdapterRegistry2: NewSealedAdapterRegistryForTest(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Cancel as soon as the retry loop is demonstrably running (first ShowBead
	// failure observed), i.e. while it is sleeping between attempts.
	if !pina9WaitFor(20*time.Second, func() bool { return ledger.showCallTotal.Load() >= 1 }) {
		cancel()
		<-loopDone
		t.Fatal("ShowBead was never called; want >= 1 (test setup error: the pre-claim guard was never reached)")
	}
	cancel()

	pina9AwaitLoopExit(t, loopDone, 25*time.Second)
}
