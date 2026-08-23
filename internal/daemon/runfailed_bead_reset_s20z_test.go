package daemon_test

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

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "/bin/sh",
		HandlerArgs:         []string{"-c", "exit 1"},
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:    NewEmptySealedAdapterRegistryForTest(t),
		WorkflowModeDefault: core.WorkflowModeSingle,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	select {
	case <-ledger.reopened:
		cancel()
	case <-ctx.Done():
		t.Errorf("TestRunFailed_BeadResetToOpen_s20z: timed out waiting for ReopenBead: "+
			"claimCount=%d reopenCount=%d — bead may be stuck in_progress after run_failed (hk-s20z)",
			ledger.getClaimCount_s20z(), ledger.getReopenCount_s20z())
	}

	awaitLoopTeardown(t, loopDone, "work loop")

	if reopens := ledger.getReopenCount_s20z(); reopens < 1 {
		t.Errorf("TestRunFailed_BeadResetToOpen_s20z: ReopenBead call count = %d; want >= 1 "+
			"(bead must be reset to open after run_failed without commit — hk-s20z)",
			reopens)
	}

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
