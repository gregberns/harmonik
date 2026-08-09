package daemon_test

// workloop_reservationwindow_test.go — what happens to a reserved queue item
// when the work loop exits before the bead is ever claimed.
//
// The window is narrow and expensive. reserveQueueItem commits the item as
// DISPATCHED with a RunID in one durable write. The bead is claimed some lines
// later. Between those two points the loop can leave, and the item is then
// recorded on disk as handed to a run that never existed.
//
// That state does not self-heal. A dispatched item is never re-selected. The
// boot provenance pass reads a dispatched item as a live owner and excludes its
// bead from the stale sweep, and the Class-B reconcile pass only repairs beads
// the ledger holds in_progress — this bead is still open, because the claim is
// exactly what did not happen. So an item lost in this window is stranded until
// somebody edits the queue file, and any wave group holding it never advances.
//
// # What these tests found
//
// The test backlog recorded this as three unreleased early returns and marked
// it RED TODAY. Driven against the tree, two of the three are not defects:
// both reachable exits leave through exitClean, which calls drainCancelledQueue
// and archives the whole active queue, so the item goes away with the queue
// rather than stranding. Those two paths are pinned below so the protection
// stays honest — it is load-bearing, and nothing was asserting it.
//
// The third exit WAS real: the claim TransitionID generation failure returned
// an error directly instead of draining, so it skipped the drain and left
// exactly the stranded item described above. It had no test because it had no
// seam — TransitionIDGenerator.Next fails only when the UUIDv7 draw fails, and
// nothing let a test induce that.
//
// Both halves are now closed. runloop.TransitionIDSource is the seam, so a test
// can hand the loop a generator that refuses; scheduler.go routes the failure
// through exitFatal, which drains on the way out and still returns the error.
// The third test below drives it.
//
// Mutation that must turn these red: delete the drainCancelledQueue call from
// exitClean in scheduler.go. All three tests below fail; that call is the only
// thing keeping any of these exits safe. For the third alone, the narrower
// mutation is to put back the bare `return fmt.Errorf(...)` at the claim-TID
// failure in scheduler.go.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// haltAtClaimLedger halts dispatch at the moment of the claim — the shape a
// SIGTERM takes when it lands in the reservation window. By the time ClaimBead
// runs, reserveQueueItem has already committed the item as dispatched, so
// cancelling here puts the loop in the window with a durable reservation
// outstanding.
type haltAtClaimLedger struct {
	*stubBeadLedger
	halt     func()
	claimErr error
	claimed  chan struct{}
}

func (l *haltAtClaimLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	select {
	case <-l.claimed:
		// Already fired once; later calls behave normally.
	default:
		close(l.claimed)
		l.halt()
		return l.claimErr
	}
	return l.claimErr
}

// reservationWindowQueue builds a one-item active wave queue, the smallest
// thing that can carry a reservation.
func reservationWindowQueue(t *testing.T, beadID core.BeadID) *queue.Queue {
	t.Helper()
	now := time.Now()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
			CreatedAt:  now,
		}},
	}
}

// assertNoStrandedDispatchedItem is the whole point of this file. It reads the
// canonical queue file back from disk — not the in-memory store, which is
// cleared on exit and would report success for a queue that is still on disk
// holding a dispatched item — and fails if a live queue still claims an item is
// out with a run.
//
// An archived or absent queue passes: the item went away with its queue, which
// is the designed shutdown behaviour and leaves the bead free to be resubmitted.
func assertNoStrandedDispatchedItem(t *testing.T, projectDir string) {
	t.Helper()

	q, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load main queue after shutdown: %v", err)
	}
	if q == nil {
		return // archived — nothing stranded
	}
	if q.Status != queue.QueueStatusActive {
		return // terminal — the next boot will not dispatch from it
	}
	for gi := range q.Groups {
		for ii := range q.Groups[gi].Items {
			item := q.Groups[gi].Items[ii]
			if item.Status == queue.ItemStatusDispatched {
				t.Fatalf(
					"bead %s is left dispatched in a still-active queue after the loop exited before the claim: "+
						"a dispatched item is never re-selected and no reconcile pass repairs an open bead, so it is stranded",
					item.BeadID)
			}
		}
	}
}

// runLoopToExit runs the work loop, waits for it to return, and asserts it left
// cleanly. The caller supplies the call because the runtime type is unexported
// to this package.
//
// The returned error is worth asserting rather than discarding: a clean exit
// goes through exitClean, which returns nil AFTER draining the queue. A non-nil
// error means the loop took one of the direct error returns instead — the
// family this file is about, and the family that skips the drain.
func runLoopToExit(t *testing.T, run func() error) {
	t.Helper()
	if err := runLoopCapturingExit(t, run); err != nil {
		t.Fatalf("work loop exited via a direct error return, which skips the queue drain: %v", err)
	}
}

// runLoopCapturingExit runs the work loop to completion and hands back whatever
// it returned. The claim-TID case below needs the error rather than a failure:
// a fatal error there is CORRECT and must still propagate, and the claim under
// test is that it propagates AND drains, not that it stops happening.
func runLoopCapturingExit(t *testing.T, run func() error) error {
	t.Helper()
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		err = run()
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("work loop did not exit within 30s")
	}
	return err
}

// refusingTIDSource is a TransitionID source that never issues one. It stands in
// for a UUIDv7 draw that fails — the only way the real generator can fail, and a
// thing no test can make the real generator do.
type refusingTIDSource struct {
	calls atomic.Int32
	err   error
}

func (s *refusingTIDSource) Next() (core.TransitionID, error) {
	s.calls.Add(1)
	return core.TransitionID{}, s.err
}

// TestWorkLoop_DispatchHaltAfterTheReservationDoesNotLeaveTheItemDispatched
// drives the reachable half of the window: dispatch is halted while the claim
// is in flight, with the item already durably reserved.
//
// Two sub-cases split on what the claim itself did, because the loop reads them
// through different branches — one returns before the release block, the other
// after it.
func TestWorkLoop_DispatchHaltAfterTheReservationDoesNotLeaveTheItemDispatched(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	cases := []struct {
		name     string
		claimErr error
	}{
		{
			// The claim failed AND dispatch is halting. The loop returns at the
			// dispatchCtx check above the release block, so the release that
			// exists for an ordinary claim failure never runs.
			name:     "claim fails as dispatch halts",
			claimErr: context.Canceled,
		},
		{
			// The claim succeeded but dispatch is halting anyway. The item is
			// dispatched and the bead is claimed; the loop unwinds from a later
			// point.
			name:     "claim succeeds as dispatch halts",
			claimErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir, _ := workloopFixtureProjectDir(t)
			workloopFixtureGitRepo(t, projectDir)

			const beadID = core.BeadID("reservation-window-bead-001")
			q := reservationWindowQueue(t, beadID)
			if err := queue.Persist(context.Background(), projectDir, q); err != nil {
				t.Fatalf("persist queue: %v", err)
			}

			qs := daemon.ExportedNewQueueStore()
			qs.SetQueue(q)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			ledger := &haltAtClaimLedger{
				stubBeadLedger: &stubBeadLedger{labels: workloopFixtureSingleLabels},
				halt:           cancel,
				claimErr:       tc.claimErr,
				claimed:        make(chan struct{}),
			}

			deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
				BrAdapter:        ledger,
				Bus:              &stubEventCollector{},
				ProjectDir:       projectDir,
				HandlerBinary:    "/bin/sh",
				HandlerArgs:      []string{"-c", "exit 0"},
				IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
				AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
				QueueStore:       qs,
			})

			runLoopToExit(t, func() error { return daemon.ExportedRunWorkLoop(ctx, deps) })

			select {
			case <-ledger.claimed:
			default:
				t.Fatal("the claim never ran, so the loop never entered the reservation window; this test proved nothing")
			}

			assertNoStrandedDispatchedItem(t, projectDir)
		})
	}
}

// TestWorkLoop_AClaimTransitionIDFailureDoesNotStrandTheReservedItem drives the
// third exit — the one that used to strand. The loop reserves the item, asks for
// a TransitionID to stamp the claim with, and is refused.
//
// Two things must both hold, and they pull in opposite directions, which is why
// they are asserted together:
//
//   - The error propagates. A refused TransitionID is a real fault and the loop
//     must not swallow it and carry on.
//   - The queue is drained anyway. The item was already committed to disk as
//     dispatched before the refusal, so an exit that only reports the error
//     leaves it owned by a run that never started.
//
// The failing source is the point of the seam: the real generator refuses only
// on a UUIDv7 fault, so without an injectable source this exit is unreachable
// from a test and the fix for it is unverifiable in exactly the way the bug was.
func TestWorkLoop_AClaimTransitionIDFailureDoesNotStrandTheReservedItem(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("reservation-window-bead-003")
	q := reservationWindowQueue(t, beadID)
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist queue: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	refused := errors.New("uuid: no entropy available")
	tidGen := &refusingTIDSource{err: refused}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        &stubBeadLedger{labels: workloopFixtureSingleLabels},
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		QueueStore:       qs,
		TIDGen:           tidGen,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := runLoopCapturingExit(t, func() error { return daemon.ExportedRunWorkLoop(ctx, deps) })

	// Positive evidence that the loop reached the window at all. Without this the
	// two assertions below are satisfied for free by a loop that never dispatched.
	if got := tidGen.calls.Load(); got == 0 {
		t.Fatal("the loop never asked for a TransitionID, so it never reached the claim; this test proved nothing")
	}

	if err == nil {
		t.Fatal("the loop exited nil after the TransitionID source refused; a refused TransitionID is a fault and must propagate")
	}
	if !errors.Is(err, refused) {
		t.Errorf("the loop exited with %v, which does not wrap the refusal it was given; the cause is lost", err)
	}
	if !strings.Contains(err.Error(), "claim TransitionID") {
		t.Errorf("the exit error %q does not name the claim TransitionID step, so an operator cannot tell which generation failed", err)
	}

	assertNoStrandedDispatchedItem(t, projectDir)
}

// TestWorkLoop_AnActiveQueueIsNeverLeftLiveOnDiskAfterExit states the property
// the case above depends on, on its own: the drain is what makes a halt in the
// reservation window survivable, so it is worth pinning where a reader will
// find it rather than only as a side effect.
//
// This is the cheaper, broader version — no reservation, just an active queue
// and an immediate halt.
func TestWorkLoop_AnActiveQueueIsNeverLeftLiveOnDiskAfterExit(t *testing.T) {
	skipRealDaemonE2EInShort(t)
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	q := reservationWindowQueue(t, core.BeadID("reservation-window-bead-002"))
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist queue: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        &stubBeadLedger{},
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		QueueStore:       qs,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runLoopToExit(t, func() error { return daemon.ExportedRunWorkLoop(ctx, deps) })

	// The canonical file must be gone: CancelQueueOnShutdown archives it under a
	// .cancelled-<ts> suffix. A queue left active here blocks the next start on
	// the QM-027 guard as well as stranding whatever it holds.
	if _, statErr := os.Stat(filepath.Join(projectDir, ".harmonik", "queues", queue.QueueNameMain+".json")); statErr == nil {
		t.Error("the active queue is still at its canonical path after exit; it was never drained")
	} else if !os.IsNotExist(statErr) {
		t.Errorf("stat canonical queue path: %v", statErr)
	}

	assertNoStrandedDispatchedItem(t, projectDir)
}
