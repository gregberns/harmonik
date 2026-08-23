package daemon_test

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

	if persisted.Status != queue.QueueStatusPausedByDrain || !persisted.ResumeOnStart {
		t.Errorf("persisted queue status = %q (resume_on_start=%v), want %q with the restart intent: recording an outcome must not cost the drain its park",
			persisted.Status, persisted.ResumeOnStart, queue.QueueStatusPausedByDrain)
	}
}

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
	}
}
