package daemon_test

// workloop_reviewloop_reopen_hktrfif_test.go — regression test for 5adcdcf fix (hk-trfif).
//
// Root cause (pre-fix): when runReviewLoop returned success=false (e.g. no_commit,
// error), beadRunOne called CloseBead, incorrectly marking the bead as done with
// a failed status. This caused beads to be closed-without-impl.
//
// Fix (5adcdcf): beadRunOne now calls ReopenBead on the review-loop failure path
// so failed beads remain available for retry.
//
// This test exercises the full beadRunOne code path via ExportedRunWorkLoop in
// review-loop mode. The handler exits 0 without committing anything, which
// triggers the no-commit path in runReviewLoop (success=false). The test asserts
// that ReopenBead is called and CloseBead is NOT called.
//
// Helper prefix: rlReopenFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-trfif).

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// TestWorkLoop_ReviewLoopFailure_ReopensBeadNotCloses_Hktrfif verifies that when
// the review loop fails (no_commit: implementer exits 0 without advancing HEAD),
// beadRunOne calls ReopenBead and does NOT call CloseBead.
//
// Regression guard for commit 5adcdcf: before the fix, the failure path called
// CloseBead, incorrectly terminating the bead as done.
//
// Bead ref: hk-trfif.
func TestWorkLoop_ReviewLoopFailure_ReopensBeadNotCloses_Hktrfif(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("trfif-rl-fail-reopen-001")
	ledger := &stubBeadLedger{
		ready: []core.BeadID{beadID},
	}
	collector := &stubEventCollector{}

	// Handler exits 0 without committing — triggers the no-commit path in
	// runReviewLoop so it returns success=false.
	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 0"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait until ReopenBead is called (review-loop failure path) or until a
	// premature CloseBead call reveals the regression.
	deadline := time.After(25 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 || len(ledger.closedIDs()) > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for bead transition; closed=%v reopened=%v events=%v",
				ledger.closedIDs(), ledger.reopenedIDs(), collector.eventTypes())
		case <-time.After(50 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}

	// ReopenBead must have been called — review-loop failure reopens for retry.
	reopened := ledger.reopenedIDs()
	if len(reopened) == 0 {
		t.Fatalf("ReopenBead was not called; bead should be reopened on review-loop failure (regression for 5adcdcf). closed=%v events=%v",
			ledger.closedIDs(), collector.eventTypes())
	}
	if reopened[0] != beadID {
		t.Errorf("reopened bead = %q; want %q", reopened[0], beadID)
	}

	// CloseBead must NOT have been called (the pre-fix regression).
	if closed := ledger.closedIDs(); len(closed) > 0 {
		t.Errorf("CloseBead called with %v; bead must be reopened, not closed, on review-loop failure (regression for 5adcdcf fix)",
			closed)
	}
}
