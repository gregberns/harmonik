package daemon_test

// workloop_shutdowncompletion_test.go — what happens to a queue item whose run
// SUCCEEDS while the daemon context is already cancelled.
//
// The run's terminal event is emitted inside beadRunOne. The queue write that
// records the item's outcome happens one level up, in runDispatchedBead, AFTER
// beadRunOne returns. Everything else the run does on the way out already
// detaches from the cancelled context — the run branch is merged under
// context.WithoutCancel, the bead is closed under it, and the terminal event
// swaps a cancelled context for a live one. Only the queue write did not, and
// the queue writer refuses a cancelled context outright.
//
// So a run that reached success as the daemon stopped left its work merged, its
// bead closed, its completion announced — and its queue item still recorded as
// dispatched. A dispatched item is never re-selected, and its group cannot reach
// all-terminal, so that queue does not advance again inside this daemon's life.
//
// # Why this test drives the shutdown drain rather than racing it
//
// The reported reproduction is a race: the run finishes at the same instant the
// daemon stops. A race is the wrong shape for a merge-gate test. The drain gives
// the same ordering by construction — the handler commits real work and then
// refuses to finish, so the run CANNOT reach its own terminal, and the only exit
// is the drain, which merges the commit and closes the bead. The run reaches
// success with the daemon context already cancelled every time, which is the
// exact state the repair is about.
//
// # Why the queue has two groups
//
// A one-group, one-item queue that completes successfully is a COMPLETED queue:
// the final-completion path unlinks the canonical file. The assertion would then
// have to read an ABSENT file as success, and the neighbouring
// workloop_reservationwindow_test.go calls that same disk state the operator's
// submitted work deleted without a receipt. The second group keeps the queue
// alive so the item's own recorded status is what this test reads.
//
// # Why it reads the file immediately, and does not run the next start
//
// assertNoStrandedDispatchedItem in workloop_reservationwindow_test.go runs a
// fresh daemon start before it judges. That start's reconcile pass advances a
// dispatched item whose bead is closed straight to completed — which is exactly
// this defect's state, and exactly the masking that would let this test pass
// against the unrepaired code. This file reads the canonical queue file itself,
// once, as soon as the loop returns. Do not reach for that helper here.
//
// Mutation that must turn this red: in runDispatchedBead in scheduler.go, take
// the detached completion context back out, so completionCtx stays the cancelled
// daemonCtx. The item is then left dispatched.
//
// The sibling mutation — dropping the `runOK &&` guard so every run detaches —
// is caught by TestWorkLoop_ARunTheShutdownCutShortIsNotRecordedAsFailed, the
// second test in THIS file, and by nothing else. Do not read the two tests as
// redundant. TestScenario_ConcurrentMultiQueue_N2_MidRunKill looks like the
// guard and is not one: it asserts on events and on br's view of the bead but
// never reads the queue file, so it stays green under that mutation, and it
// carries //go:build scenario, so it does not exist in the default gate at all.
// Each mutation above was measured to redden exactly one of these two tests and
// leave the other green.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestWorkLoop_ASuccessfulRunRecordsItsOutcomeWhenTheDaemonContextIsCancelled(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	const beadID = core.BeadID("shutdown-completion-bead-001")
	const successorBeadID = core.BeadID("shutdown-completion-bead-002")

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(filepath.Join(projectDir, "workflow.dot"), []byte(dotFixtureGraph), 0o644); err != nil {
		t.Fatalf("write workflow.dot: %v", err)
	}

	q := shutdownCompletionQueue(t, beadID, successorBeadID)
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist queue: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	// The handler commits, announces the commit, and then refuses to finish. The
	// cancellation therefore always lands on a run that has work worth keeping
	// and no terminal of its own.
	marker := filepath.Join(t.TempDir(), "commit-ready")
	handlerScript := dotFixtureHandlerScript(t, "shutdown-completion-implementer.sh",
		dotFixtureCommitLines(beadID)+"touch "+marker+"\nwhile :; do sleep 1; done\n")

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	go dotShutdownDrainCancelAfterMarker(ctx, cancel, marker)

	ledger := &stubBeadLedger{}
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{handlerScript},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		HookStore:        dotFixtureHookStore{},
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	})

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck // the loop's own error is not the claim; the recorded item is
	}()
	awaitLoopTeardown(t, loopDone, "shutdown-completion work loop")

	// Two preconditions before the claim itself. Without them a red result here
	// says nothing about the completion write: a run that never committed, or one
	// that failed, has no successful outcome to record in the first place.
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the handler never committed, so the cancellation did not land on a run holding work: %v", err)
	}
	if closed, reopened := ledger.closedIDs(), ledger.reopenedIDs(); len(closed) != 1 || closed[0] != beadID || len(reopened) != 0 {
		t.Fatalf("drained run closed=%v reopened=%v; want only %s closed — the run did not succeed, so this test proved nothing about recording a successful outcome",
			closed, reopened, beadID)
	}

	persisted, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load the canonical main queue file the exit left behind: %v", err)
	}
	if persisted == nil {
		t.Fatal("the canonical main queue file is gone; a clean shutdown parks the queue in place, so an absent file is the submitted work deleted rather than an item recorded")
	}
	if len(persisted.Groups) == 0 || len(persisted.Groups[0].Items) == 0 {
		t.Fatalf("the persisted queue lost its first group's item: %+v", persisted.Groups)
	}

	if got := persisted.Groups[0].Items[0].Status; got != queue.ItemStatusCompleted {
		t.Fatalf("persisted item for %s = %q, want %q: the run merged its commit, closed its bead and announced completion, "+
			"but its outcome was never written to the queue. Nothing re-selects a dispatched item and its group cannot reach "+
			"all-terminal, so this queue does not advance again",
			beadID, got, queue.ItemStatusCompleted)
	}

	// Recording the outcome must not have resumed the queue. The exit still owes
	// the next start a parked queue carrying the one-shot restart intent.
	if persisted.Status != queue.QueueStatusPausedByDrain || !persisted.ResumeOnStart {
		t.Errorf("persisted queue status = %q (resume_on_start=%v), want %q with the restart intent: recording an outcome must not cost the drain its park",
			persisted.Status, persisted.ResumeOnStart, queue.QueueStatusPausedByDrain)
	}
}

// shutdownCompletionQueue builds a two-group queue: one active group holding the
// bead the run drains, and one pending successor. The successor exists only to
// keep the queue from completing itself out of existence when the first group
// succeeds — see the file header.
func shutdownCompletionQueue(t *testing.T, active, successor core.BeadID) *queue.Queue {
	t.Helper()
	now := time.Now()
	group := func(index int, bead core.BeadID, status queue.GroupStatus) queue.Group {
		return queue.Group{
			GroupIndex: index,
			Kind:       queue.GroupKindWave,
			Status:     status,
			Items: []queue.Item{{
				BeadID:       bead,
				Status:       queue.ItemStatusPending,
				WorkflowMode: string(core.WorkflowModeDot),
			}},
			CreatedAt: now,
		}
	}
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			group(0, active, queue.GroupStatusActive),
			group(1, successor, queue.GroupStatusPending),
		},
	}
}

// TestWorkLoop_ARunTheShutdownCutShortIsNotRecordedAsFailed pins the OTHER half
// of the same repair, and it exists because the obvious sibling does not pin it.
//
// The completion write is allowed through on a cancelled daemon context only for
// a run that SUCCEEDED. A shutdown does not fail the run it interrupts, it PARKS
// it: with no commit to keep, the drain reopens the bead for re-dispatch. Writing
// that item failed would move the queue to paused-by-failure over work that is
// going straight back into the pool, and no operator asked for that pause.
//
// TestScenario_ConcurrentMultiQueue_N2_MidRunKill covers the same cancellation
// but asserts only on events and on br's view of the bead — it never reads the
// queue file, so it stays green when the success gate is removed. This test reads
// the file.
//
// Mutation that must turn this red: drop the `runOK &&` from the detach
// condition in runDispatchedBead, so every run detaches. The item is then
// recorded failed.
func TestWorkLoop_ARunTheShutdownCutShortIsNotRecordedAsFailed(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	const beadID = core.BeadID("shutdown-park-bead-001")
	const successorBeadID = core.BeadID("shutdown-park-bead-002")

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(filepath.Join(projectDir, "workflow.dot"), []byte(dotFixtureGraph), 0o644); err != nil {
		t.Fatalf("write workflow.dot: %v", err)
	}

	q := shutdownCompletionQueue(t, beadID, successorBeadID)
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist queue: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	// The same handler as the sibling test WITHOUT the commit: it announces that
	// it is running and then refuses to finish. The drain therefore finds no work
	// to keep and takes the reopen ladder instead of the merge one.
	marker := filepath.Join(t.TempDir(), "run-live")
	handlerScript := dotFixtureHandlerScript(t, "shutdown-park-implementer.sh",
		"touch "+marker+"\nwhile :; do sleep 1; done\n")

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	go dotShutdownDrainCancelAfterMarker(ctx, cancel, marker)

	ledger := &stubBeadLedger{}
	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:        ledger,
		Bus:              &stubEventCollector{},
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{handlerScript},
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:       qs,
		HookStore:        dotFixtureHookStore{},
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	})

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck // the loop's own error is not the claim; the recorded item is
	}()
	awaitLoopTeardown(t, loopDone, "shutdown-park work loop")

	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the handler never ran, so the cancellation did not land on a live run: %v", err)
	}
	if closed := ledger.closedIDs(); len(closed) != 0 {
		t.Fatalf("parked run closed=%v; want none — a run the shutdown cut short has no success to record, so this test proved nothing", closed)
	}

	persisted, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load the canonical main queue file the exit left behind: %v", err)
	}
	if persisted == nil {
		t.Fatal("the canonical main queue file is gone; a clean shutdown parks the queue in place, so an absent file is the submitted work deleted")
	}
	if len(persisted.Groups) == 0 || len(persisted.Groups[0].Items) == 0 {
		t.Fatalf("the persisted queue lost its first group's item: %+v", persisted.Groups)
	}

	switch got := persisted.Groups[0].Items[0].Status; got {
	case queue.ItemStatusFailed:
		t.Fatalf("persisted item for %s = %q: the shutdown PARKED this run, it did not fail it — the bead was reopened for "+
			"re-dispatch, and recording a failure pauses the whole queue over work that is going back into the pool",
			beadID, got)
	case queue.ItemStatusCompleted:
		t.Fatalf("persisted item for %s = %q: the run committed nothing and closed no bead, so there was no success to record",
			beadID, got)
	default:
		// Left for the next start, which reverts a dispatched item whose bead is
		// open back to pending. That is the disposition this path owes.
	}
}
