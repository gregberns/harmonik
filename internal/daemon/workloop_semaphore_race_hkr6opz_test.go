package daemon_test

// workloop_semaphore_race_hkr6opz_test.go — verifies that the workloop exits
// cleanly when the context is cancelled while a goroutine is blocked on the
// claim semaphore (all dispatch slots occupied).
//
// The race under test: MaxConcurrent=1, one bead is in-flight (handler blocks
// on a channel), a second bead is ready and waiting for the semaphore. When the
// context is cancelled, the select in the semaphore-acquire path picks up
// dispatchCtx.Done() and the loop exits without deadlock or panic.
//
// Helper prefix: semRaceFixture (per implementer-protocol.md §Helper-prefix).
//
// Bead ref: hk-r6opz.

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
// semRaceFixtureLedger — stub bead ledger that blocks the handler on ClaimBead
// until released, ensuring the semaphore stays full.
// ─────────────────────────────────────────────────────────────────────────────

type semRaceFixtureLedger struct {
	mu    sync.Mutex
	ready []core.BeadID

	// claimedCh is closed when the first bead has been claimed, signalling
	// the test that the semaphore slot is occupied.
	claimedCh   chan struct{}
	claimedOnce sync.Once
}

func (l *semRaceFixtureLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ready) == 0 {
		return nil, nil
	}
	id := l.ready[0]
	l.ready = l.ready[1:]
	return []core.BeadRecord{{BeadID: id}}, nil
}

func (l *semRaceFixtureLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *semRaceFixtureLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.claimedOnce.Do(func() { close(l.claimedCh) })
	return nil
}

func (l *semRaceFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	return nil
}

func (l *semRaceFixtureLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkLoop_SemaphoreShutdownRace verifies that when the workloop's context
// is cancelled while a dispatch is blocked waiting for a semaphore slot (all
// MaxConcurrent slots occupied), the loop exits cleanly without deadlock or
// panic.
//
// Setup:
//   - MaxConcurrent=1
//   - Two beads are ready: bead-A and bead-B
//   - The handler binary blocks indefinitely (sleep 3600) so the single
//     semaphore slot stays occupied by bead-A's goroutine
//   - bead-B arrives at the semaphore acquire select and blocks
//   - Test cancels context → workloop must exit within the timeout
//
// Bead ref: hk-r6opz.
func TestWorkLoop_SemaphoreShutdownRace(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("sem-race-A")
		beadB = core.BeadID("sem-race-B")
	)

	ledger := &semRaceFixtureLedger{
		ready:     []core.BeadID{beadA, beadB},
		claimedCh: make(chan struct{}),
	}
	collector := &stubEventCollector{}

	// worktreeFactory creates a minimal temp directory so the workloop
	// goroutine can proceed past worktree creation without needing a real git
	// repo worktree (avoids git worktree contention under test parallelism).
	worktreeFactory := func(_ context.Context, _, runID, _ string) (string, func(), error) {
		wtDir, err := os.MkdirTemp("", "sem-race-wt-"+runID[:8]+"-")
		if err != nil {
			return "", nil, err
		}
		harmonikDir := filepath.Join(wtDir, ".harmonik")
		//nolint:gosec // G301: test-only
		if mkErr := os.MkdirAll(harmonikDir, 0o755); mkErr != nil {
			os.RemoveAll(wtDir) //nolint:errcheck
			return "", nil, mkErr
		}
		return wtDir, func() { os.RemoveAll(wtDir) }, nil //nolint:errcheck
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              collector,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "sleep 3600"}, // blocks indefinitely; killed on ctx cancel
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:    1,
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		WorktreeFactory:  worktreeFactory,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Wait for the first bead to be claimed, confirming the single semaphore
	// slot is now occupied by the in-flight bead-A goroutine.
	select {
	case <-ledger.claimedCh:
		t.Log("bead-A claimed — semaphore slot occupied")
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for bead-A to be claimed")
	}

	// Give the workloop a moment to poll bead-B and reach the semaphore
	// acquire select (where it will block because MaxConcurrent=1 and the
	// slot is held by bead-A's long-running handler).
	time.Sleep(500 * time.Millisecond)

	// Cancel context — the blocked semaphore acquire should pick up
	// dispatchCtx.Done() and the loop should exit cleanly.
	cancel()

	// The workloop MUST exit within a generous timeout. If it deadlocks,
	// this test will fail with a clear timeout message.
	select {
	case err := <-loopDone:
		if err != nil {
			t.Fatalf("workloop returned unexpected error: %v", err)
		}
		t.Log("workloop exited cleanly after context cancellation during semaphore contention")
	case <-time.After(30 * time.Second):
		t.Fatal("DEADLOCK: workloop did not exit within 30s after context cancellation — semaphore race bug")
	}
}
