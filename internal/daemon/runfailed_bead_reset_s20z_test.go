package daemon_test

// runfailed_bead_reset_s20z_test.go — bead is reset to open after run_failed (hk-s20z).
//
// When a run terminates with run_failed (no commit, no merge), the daemon MUST call
// ReopenBead to transition the bead from in_progress back to open so that a
// subsequent `harmonik queue dry-run --beads <id>` succeeds (not -32015
// bead_already_dispatched).
//
// This test verifies the invariant using a stub beadLedger that records ReopenBead
// calls. The handler is `/bin/sh -c "exit 1"` — it exits without committing,
// triggering the no_commit_during_implementer failure path in beadRunOne.
//
// Assertions:
//
//	(a) ReopenBead is called on the stub ledger (bead reset to open).
//	(b) run_failed event is emitted in the collector.
//
// Helper prefix: rfs20z (bead hk-s20z).
// Namespace suffix: _s20z per hk-s20z CONCURRENCY NOTE (sibling hk-xfuc dispatches
// concurrently; helpers must not collide on names).
//
// Bead: hk-s20z.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// rfs20z fixtures
// ─────────────────────────────────────────────────────────────────────────────

// rfs20zProjectDir creates the minimal project directory for this test:
// .harmonik/events/ and .harmonik/beads-intents/.
func rfs20zProjectDir_s20z(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "events"), 0o755); err != nil {
		t.Fatalf("rfs20zProjectDir_s20z: mkdir events: %v", err)
	}
	//nolint:gosec // G301: test-only temp directory
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "beads-intents"), 0o755); err != nil {
		t.Fatalf("rfs20zProjectDir_s20z: mkdir beads-intents: %v", err)
	}
	return dir
}

// ─────────────────────────────────────────────────────────────────────────────
// rfs20zLedger — stub beadLedger that records ReopenBead calls
// ─────────────────────────────────────────────────────────────────────────────

// rfs20zLedger is a stub beadLedger for the run-failed bead-reset test.
// It seeds one bead via Ready, records all ClaimBead and ReopenBead calls,
// and signals via the reopened channel on the first ReopenBead call.
type rfs20zLedger_s20z struct {
	mu sync.Mutex

	beadID core.BeadID

	// readyQueue is drained by Ready.
	readyQueue []core.BeadID

	// claimCount records how many times ClaimBead was called.
	claimCount int

	// reopenCount records how many times ReopenBead was called.
	reopenCount int

	// reopened is closed on the first ReopenBead call.
	reopened chan struct{}
	once     sync.Once
}

func newRfs20zLedger_s20z(beadID core.BeadID) *rfs20zLedger_s20z {
	return &rfs20zLedger_s20z{
		beadID:     beadID,
		readyQueue: []core.BeadID{beadID},
		reopened:   make(chan struct{}),
	}
}

func (l *rfs20zLedger_s20z) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.readyQueue) == 0 {
		return []core.BeadRecord{}, nil
	}
	id := l.readyQueue[0]
	l.readyQueue = l.readyQueue[1:]
	return []core.BeadRecord{{BeadID: id, Status: core.CoarseStatusOpen}}, nil
}

func (l *rfs20zLedger_s20z) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *rfs20zLedger_s20z) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.mu.Lock()
	l.claimCount++
	l.mu.Unlock()
	return nil
}

func (l *rfs20zLedger_s20z) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *rfs20zLedger_s20z) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	l.mu.Lock()
	l.reopenCount++
	l.mu.Unlock()
	l.once.Do(func() { close(l.reopened) })
	return nil
}

func (l *rfs20zLedger_s20z) getReopenCount_s20z() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopenCount
}

func (l *rfs20zLedger_s20z) getClaimCount_s20z() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.claimCount
}

// ─────────────────────────────────────────────────────────────────────────────
// TestRunFailed_BeadResetToOpen_s20z
// ─────────────────────────────────────────────────────────────────────────────

// TestRunFailed_BeadResetToOpen_s20z verifies that after run_failed (no commit),
// the daemon calls ReopenBead to transition the bead back to open.
//
// Guards the reliability gap reported in hk-s20z / hk-k0eg: when a run reaches
// run_failed without merging, the bead must NOT remain stuck in in_progress.
// A stuck bead causes subsequent `harmonik queue dry-run --beads <id>` to return
// -32015 (bead_already_dispatched), blocking re-dispatch indefinitely.
//
// The handler is `/bin/sh -c "exit 1"` — it exits without making a git commit.
// beadRunOne detects no HEAD advancement (no_commit_during_implementer path),
// calls ReopenBead, then emits run_failed.
//
// Bead: hk-s20z.
func TestRunFailed_BeadResetToOpen_s20z(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-s20z-reset-test-001")

	projectDir := rfs20zProjectDir_s20z(t)
	workloopFixtureGitRepo(t, projectDir)

	ledger := newRfs20zLedger_s20z(beadID)
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 1"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewEmptySealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeSingle,
	})

	// Allow enough headroom: worktree creation + process launch + no-commit
	// detection + ReopenBead call. The handler exits instantly, so this is
	// dominated by git worktree setup time (~1-2s).
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for ReopenBead (bead reset to open) or test timeout.
	select {
	case <-ledger.reopened:
		// ReopenBead observed — cancel the loop, then verify.
		cancel()
	case <-ctx.Done():
		t.Errorf("TestRunFailed_BeadResetToOpen_s20z: timed out waiting for ReopenBead: "+
			"claimCount=%d reopenCount=%d — bead may be stuck in_progress after run_failed (hk-s20z)",
			ledger.getClaimCount_s20z(), ledger.getReopenCount_s20z())
	}

	// Wait for the loop to exit.
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Error("TestRunFailed_BeadResetToOpen_s20z: work loop did not exit within 5s after context cancellation")
	}

	// ── Assertion (a): ReopenBead was called ─────────────────────────────────
	//
	// ReopenBead transitions the bead from in_progress → open. Without this
	// call, the bead is stuck in_progress and queue submit returns -32015.

	if reopens := ledger.getReopenCount_s20z(); reopens < 1 {
		t.Errorf("TestRunFailed_BeadResetToOpen_s20z: ReopenBead call count = %d; want >= 1 "+
			"(bead must be reset to open after run_failed without commit — hk-s20z)",
			reopens)
	}

	// ── Assertion (b): run_failed was emitted ────────────────────────────────
	//
	// The no_commit_during_implementer path calls ReopenBead THEN emits
	// run_failed. We accept run_failed appearing anywhere in the collected
	// events (the run terminal is emitted after ReopenBead returns).

	runFailedFound := false
	for _, et := range collector.eventTypes() {
		if et == string(core.EventTypeRunFailed) {
			runFailedFound = true
			break
		}
	}
	if !runFailedFound {
		t.Errorf("TestRunFailed_BeadResetToOpen_s20z: run_failed event not emitted; got event types: %v",
			collector.eventTypes())
	}

	t.Logf("TestRunFailed_BeadResetToOpen_s20z PASS: claimCount=%d reopenCount=%d events=%v",
		ledger.getClaimCount_s20z(), ledger.getReopenCount_s20z(), collector.eventTypes())
}
