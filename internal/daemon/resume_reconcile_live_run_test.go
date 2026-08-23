package daemon

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

func liveResumeLog(t *testing.T, runs map[core.RunID]core.BeadID) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writer, err := eventbus.OpenJSONLWriter(path)
	if err != nil {
		t.Fatalf("liveResume: open the event log: %v", err)
	}
	defer func() {
		if closeErr := writer.Close(); closeErr != nil {
			t.Errorf("liveResume: close the event log: %v", closeErr)
		}
	}()
	bus := eventbus.NewBusImplWithWriter(core.NewRedactionRegistry(), writer)
	for runID, beadID := range runs {
		emitRunStarted(context.Background(), bus, runID, beadID, "/tmp/workspace", nil, nil,
			standardBeadDescriptor, core.WorkflowModeDot, core.ReviewPolicyReviewed,
			core.WorkflowSelectionEmbeddedDefault, nil, nil)
	}
	return path
}

// TestResumeReconcile_ThePrimaryLoopLeavesABeadWithALiveRunAlone is the pass the
// other two guards never covered.
//
// Both runs below look identical in the event log: a run_started, no terminal
// event. One of them has an agent still working in a tmux session that outlived
// the daemon, and the registry says so. The other is what the reconcile exists
// for — the daemon died mid-run, nothing is working that bead, and it must go
// back on the queue.
//
// The crashed run is the control. "The live bead was left alone" is a claim that
// something did NOT happen, which is free in a reconcile that reset nothing. The
// crashed bead proves the loop ran, reached this ledger, and reset what it was
// meant to reset in the same call.
//
// The event check is the second half and it is not decoration either. Reporting
// a live run FAILED is its own damage even when the bead survives: the queue
// advances past a run that is still going, and the terminal event the agent
// eventually produces arrives after the run was already written off.
func TestResumeReconcile_ThePrimaryLoopLeavesABeadWithALiveRunAlone(t *testing.T) {
	t.Parallel()

	const liveBead = core.BeadID("hk-agent-still-working-it")
	const crashedBead = core.BeadID("hk-daemon-died-under-it")
	liveRun := core.RunID(uuid.MustParse("01942b3c-0000-7000-8000-0000000000a1"))
	crashedRun := core.RunID(uuid.MustParse("01942b3c-0000-7000-8000-0000000000a2"))

	eventsPath := liveResumeLog(t, map[core.RunID]core.BeadID{
		liveRun:    liveBead,
		crashedRun: crashedBead,
	})

	ledger := &noWriterLedger{}
	liveRunBeadIDs := map[core.BeadID]struct{}{liveBead: {}}

	reconciled := reconcileOrphanedRunsOnResume(
		t.Context(),
		eventsPath,
		eventbus.NewBusImpl(),
		ledger,
		ledger,
		t.TempDir(),
		core.ProjectHash("abcdef012345"),
		0,
		nil, // no durable queue in this fixture: the primary loop is the subject
		liveRunBeadIDs,
	)

	if !ledger.wasReset(crashedBead) {
		t.Fatalf("the reconcile did not reset %s, the bead it exists to recover.\n"+
			"Nothing below means anything until this pass is shown to act. A reconcile that "+
			"resets nothing leaves every bead alone, including the live one, and would pass the "+
			"check underneath while protecting nothing.", crashedBead)
	}

	if ledger.wasReset(liveBead) {
		t.Errorf("the primary loop reset %s, whose agent is still working it in a session that "+
			"outlived the daemon.\n"+
			"A surviving run emits run_started and, by construction, no terminal event before the "+
			"crash — so this loop is the first pass to reach it, and it is the one pass that used "+
			"to reset unconditionally. The bead goes back on the queue and a second agent is "+
			"dispatched to work it.", liveBead)
	}

	if reconciled != 1 {
		t.Errorf("the reconcile reported %d orphaned run(s), want 1.\n"+
			"Only the crashed run is an orphan. Counting the live one means a run_failed was "+
			"emitted for a run that has not ended: the queue advances past it, and the terminal "+
			"event its agent eventually produces lands after the run was written off.", reconciled)
	}
}
