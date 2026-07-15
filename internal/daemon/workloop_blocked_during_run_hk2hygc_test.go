package daemon_test

// workloop_blocked_during_run_hk2hygc_test.go — Tests for the scenario where a
// bead becomes blocked DURING a run (a dependency is added after claim succeeds).
//
// This is distinct from hk-n91y0 (blocked at claim time). Here ClaimBead
// succeeds, the handler runs and completes, but CloseBead fails because the
// bead was made blocked mid-run (someone added a dependency while the handler
// was working).
//
// The workloop handles this by emitting a failed run (run_failed terminal) and
// logging the close error. The bead remains in_progress — it is NOT reopened
// automatically in this path.
//
// Bead ref: hk-2hygc.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// blockedDuringRunLedger is a stub where ClaimBead succeeds but CloseBead fails
// with a "blocked" error for a specific bead, simulating a dependency added
// mid-run. All other operations behave normally.
type blockedDuringRunLedger struct {
	mu             sync.Mutex
	closeBlockedID core.BeadID
	claimed        []core.BeadID
	closed         []core.BeadID
	reopened       []core.BeadID
	closeCallCount int // number of CloseBead calls for closeBlockedID
}

func (b *blockedDuringRunLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil // queue-only dispatch
}

func (b *blockedDuringRunLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen, Title: "test-bead"}, nil
}

func (b *blockedDuringRunLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.claimed = append(b.claimed, id)
	return nil // claim always succeeds
}

func (b *blockedDuringRunLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id == b.closeBlockedID {
		b.closeCallCount++
		return errors.New("brcli: SchemaMismatch (exit 4): stderr=\"Error: Validation failed: close: cannot close blocked issue: has open dependencies hk-newdep1\\n\"")
	}
	b.closed = append(b.closed, id)
	return nil
}

func (b *blockedDuringRunLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reopened = append(b.reopened, id)
	return nil
}

func (b *blockedDuringRunLedger) getClaimedIDs() []core.BeadID {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.BeadID, len(b.claimed))
	copy(out, b.claimed)
	return out
}

func (b *blockedDuringRunLedger) getClosedIDs() []core.BeadID {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.BeadID, len(b.closed))
	copy(out, b.closed)
	return out
}

func (b *blockedDuringRunLedger) getReopenedIDs() []core.BeadID {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]core.BeadID, len(b.reopened))
	copy(out, b.reopened)
	return out
}

func (b *blockedDuringRunLedger) getCloseCallCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closeCallCount
}

// blockedDuringRunCommittingFactory wraps productionWorktreeFactory to create a
// commit in the worktree so the no-commit guard does not fire before CloseBead.
func blockedDuringRunCommittingFactory(t *testing.T) func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	t.Helper()
	return func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
		if err != nil {
			return "", nil, err
		}

		workFile := filepath.Join(wtPath, "work.txt")
		//nolint:gosec // G306: test fixture
		if err2 := os.WriteFile(workFile, []byte("agent work for hk-2hygc\n"), 0o644); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("blockedDuringRunCommittingFactory: WriteFile: %w", err2)
		}

		addCmd := exec.CommandContext(ctx, "git", "add", "work.txt")
		addCmd.Dir = wtPath
		if out, err2 := addCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("blockedDuringRunCommittingFactory: git add: %v\n%s", err2, out)
		}

		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "feat: agent work",
			"--trailer", "Harmonik-Run-ID: "+runID,
		)
		commitCmd.Dir = wtPath
		if out, err2 := commitCmd.CombinedOutput(); err2 != nil {
			cleanup()
			return "", nil, fmt.Errorf("blockedDuringRunCommittingFactory: git commit: %v\n%s", err2, out)
		}

		return wtPath, cleanup, nil
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TestWorkLoop_BeadBlockedDuringRun (hk-2hygc)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_BeadBlockedDuringRun verifies that when a bead is claimed and
// the handler completes successfully, but CloseBead fails because the bead
// became blocked mid-run (a dependency was added while the handler was working),
// the workloop handles this gracefully:
//
//  1. ClaimBead succeeds (the bead was unblocked at claim time).
//  2. The handler runs and exits 0 (simulated with /bin/sh -c "exit 0").
//  3. CloseBead is called and returns a "blocked" error.
//  4. The run is marked as failed (run_failed event emitted).
//  5. The bead is NOT closed — it remains in_progress.
//  6. The queue item is marked failed.
//
// This verifies the close-error path in beadRunOne's single-mode completion
// branch (workloop.go lines ~1670-1675).
//
// Bead ref: hk-2hygc.
func TestWorkLoop_BeadBlockedDuringRun(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// workloopFixtureGitRepo already creates a bare clone as "origin" and
	// pushes the initial commit, so mergeRunBranchToMain's push succeeds.

	const beadA = core.BeadID("hk-2hygc-blocked-during-run")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hk2hygc-test-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadA, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	ledger := &blockedDuringRunLedger{closeBlockedID: beadA}
	bus := &stubEventCollector{}
	exitCtx, cancelExit := context.WithCancel(context.Background())

	p := daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:        qs,
		MaxConcurrent:     1,
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		CancelOnQueueExit: cancelExit,
		WorktreeFactory:   blockedDuringRunCommittingFactory(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit within timeout — CloseBead failure may have caused a hang")
	}

	// Verify: ClaimBead was called for beadA.
	claimed := ledger.getClaimedIDs()
	foundClaim := false
	for _, id := range claimed {
		if id == beadA {
			foundClaim = true
		}
	}
	if !foundClaim {
		t.Errorf("beadA was never claimed; claimed=%v", claimed)
	}

	// Verify: CloseBead was attempted (and failed) for beadA.
	closeCount := ledger.getCloseCallCount()
	if closeCount == 0 {
		t.Error("CloseBead was never called for beadA — handler may not have reached the close path")
	}

	// Verify: beadA was NOT successfully closed (it should remain in_progress).
	closedIDs := ledger.getClosedIDs()
	for _, id := range closedIDs {
		if id == beadA {
			t.Error("beadA was closed despite CloseBead returning an error — the error was silently swallowed")
		}
	}

	// Verify: a run_failed event was emitted with a close-error summary.
	evtTypes := bus.eventTypes()
	foundRunFailed := false
	for _, et := range evtTypes {
		if et == string(core.EventTypeRunFailed) {
			foundRunFailed = true
		}
	}
	if !foundRunFailed {
		t.Errorf("expected run_failed event for beadA (CloseBead error path); got event types: %v", evtTypes)
	}

	// Verify the run_failed payload mentions close-error.
	allEvts := bus.allEvents()
	foundCloseError := false
	for _, evt := range allEvts {
		if evt.EventType == string(core.EventTypeRunFailed) {
			if strings.Contains(string(evt.Payload), "close-error") {
				foundCloseError = true
			}
		}
	}
	if !foundCloseError {
		t.Error("run_failed event payload does not contain 'close-error' — expected close-error summary from the CloseBead failure path")
	}

	// Verify: queue ended up in a terminal state (paused-by-failure since the item failed).
	finalQ := daemon.ExportedQueueStoreOf(deps).Queue()
	if finalQ != nil {
		if finalQ.Status != queue.QueueStatusPausedByFailure {
			t.Errorf("queue.Status = %q; want %q (beadA CloseBead failure should mark item failed)",
				finalQ.Status, queue.QueueStatusPausedByFailure)
		}
	}
}
