package daemon_test

// concurrent_remote_agent_ready_hk4hso5_test.go — regression test for hk-4hso5.
//
// Root cause: the ErrAgentReadyTimeout branch in beadRunOne called sess.Wait(ctx)
// with the per-run ctx. For remote sessions where the pane stays alive after
// Kill, runWait polls WindowPanePID until ctx is cancelled — up to 30 min by
// the never-spawned reaper. ReopenBead was then called with a cancelled ctx,
// which causes the br subprocess to fail immediately, leaving the bead
// permanently stuck in_progress.
//
// Fix (hk-4hso5): sess.Wait is now bounded by context.WithTimeout(
// context.Background(), agentReadyKillReapTimeout). ReopenBead and
// emitAgentReadyTimeout use context.Background() so they succeed regardless of
// per-run ctx state.
//
// Test design:
//   - Two beads dispatched concurrently (MaxConcurrent=2).
//   - Fake adapter: WindowPanePID always returns (0, nil) — pane never closes
//     after Kill, simulating a remote pane where SIGKILL doesn't reach.
//   - AgentReadyTimeout = 150ms — triggers ErrAgentReadyTimeout quickly.
//   - agentReadyKillReapTimeout overridden to 50ms — bounded Wait returns fast.
//   - Ledger: ReopenBead returns ctx.Err() if ctx is cancelled (simulating br
//     failure on cancelled ctx), else signals a shared "both reopened" channel.
//   - RED (pre-fix): sess.Wait(ctx) blocks until the 3s test deadline cancels
//     ctx; ReopenBead then fails on cancelled ctx → channel never signaled →
//     test times out.
//   - GREEN (post-fix): sess.Wait(waitCtx) returns after 50ms; ReopenBead
//     called with context.Background() → succeeds → channel signaled within ~300ms.
//
// Helper prefix: remoteAgentReadyFixture.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// remoteAgentReadyFixtureAdapter — fake tmux.Adapter simulating a remote pane
// that stays alive after Kill (WindowPanePID never returns an error).
// ─────────────────────────────────────────────────────────────────────────────

type remoteAgentReadyFixtureAdapter struct {
	mu          sync.Mutex
	paneCounter int
}

func (a *remoteAgentReadyFixtureAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *remoteAgentReadyFixtureAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *remoteAgentReadyFixtureAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *remoteAgentReadyFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.paneCounter++
	paneID := fmt.Sprintf("%%%d", a.paneCounter)
	a.mu.Unlock()
	handle := tmux.WindowHandle(params.Session + ":" + params.WindowName)
	return tmux.Outcome{Handle: handle, PaneID: paneID}
}

func (a *remoteAgentReadyFixtureAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil // no-op: pane "stays alive"
}

// WindowPanePID always returns (0, nil) — simulates a remote pane that
// never closes after Kill. This causes runWait to keep polling, blocking
// sess.Wait(ctx) until ctx is cancelled.
func (a *remoteAgentReadyFixtureAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *remoteAgentReadyFixtureAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *remoteAgentReadyFixtureAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *remoteAgentReadyFixtureAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *remoteAgentReadyFixtureAdapter) PasteBuffer(_ context.Context, _, _ string) error {
	return nil
}
func (a *remoteAgentReadyFixtureAdapter) SendKeysLiteral(_ context.Context, _, _ string) error {
	return nil
}
func (a *remoteAgentReadyFixtureAdapter) SendKeysEnter(_ context.Context, _ string) error {
	return nil
}
func (a *remoteAgentReadyFixtureAdapter) SendKeysQuit(_ context.Context, _ string) error {
	return nil
}
func (a *remoteAgentReadyFixtureAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*remoteAgentReadyFixtureAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// remoteAgentReadyFixtureLedger — bead ledger that signals when both beads
// have been successfully reopened with a live (non-cancelled) context.
// ─────────────────────────────────────────────────────────────────────────────

type remoteAgentReadyFixtureLedger struct {
	mu       sync.Mutex
	ready    []core.BeadID
	inFlight int

	// reopenCount counts ReopenBead calls that received a live context.
	reopenCount atomic.Int32

	// bothReopenedCh is closed when reopenCount reaches 2.
	bothReopenedCh chan struct{}
	bothOnce       sync.Once
}

func (l *remoteAgentReadyFixtureLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *remoteAgentReadyFixtureLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ready) == 0 {
		return nil, nil
	}
	id := l.ready[0]
	l.ready = l.ready[1:]
	return []core.BeadRecord{{BeadID: id}}, nil
}

func (l *remoteAgentReadyFixtureLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	l.mu.Lock()
	l.inFlight++
	l.mu.Unlock()
	return nil
}

func (l *remoteAgentReadyFixtureLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ bool) error {
	l.mu.Lock()
	l.inFlight--
	l.mu.Unlock()
	return nil
}

// ReopenBead checks whether ctx is live. If ctx is cancelled (as it would be
// when the never-spawned reaper fires), return ctx.Err() — simulating the br
// subprocess failing when invoked with a cancelled context. If ctx is live
// (context.Background() after the fix), signal bothReopenedCh when the second
// call arrives.
func (l *remoteAgentReadyFixtureLedger) ReopenBead(ctx context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	if err := ctx.Err(); err != nil {
		// ctx already cancelled: br would fail. Return the error so the
		// production log path fires ("ReopenBead FAILED") and the bead stays
		// in_progress — this is the pre-fix failure mode the test detects.
		return fmt.Errorf("remoteAgentReadyFixtureLedger: ctx cancelled: %w", err)
	}
	l.mu.Lock()
	l.inFlight--
	l.mu.Unlock()
	if n := l.reopenCount.Add(1); n >= 2 {
		l.bothOnce.Do(func() { close(l.bothReopenedCh) })
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Test
// ─────────────────────────────────────────────────────────────────────────────

// TestConcurrentRemoteAgentReady_BothBeadsReopened verifies that under
// MaxConcurrent=2, both concurrent runs that hit ErrAgentReadyTimeout call
// ReopenBead with a live (non-cancelled) context within a bounded time, even
// when the remote session's WindowPanePID never returns an error (pane stays
// alive indefinitely after Kill).
//
// Pre-fix (RED): sess.Wait(ctx) blocks indefinitely until the test deadline
// cancels the per-run ctx; ReopenBead receives a cancelled ctx → returns error
// → bothReopenedCh never closed → test fails by timeout.
//
// Post-fix (GREEN): sess.Wait uses a 50ms bounded context; ReopenBead receives
// context.Background() → succeeds → bothReopenedCh closed within ~300ms.
//
// Bead ref: hk-4hso5.
func TestConcurrentRemoteAgentReady_BothBeadsReopened(t *testing.T) {
	// NOT parallel: ExportedSetAgentReadyKillReapTimeout modifies a package global.

	// Override kill-reap timeout to 50ms so the bounded Wait returns quickly
	// without waiting the production 10s. Restore on test exit.
	t.Cleanup(daemon.ExportedSetAgentReadyKillReapTimeout(50 * time.Millisecond))

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("remote-agent-ready-A")
		beadB = core.BeadID("remote-agent-ready-B")
	)

	fakeAdapter := &remoteAgentReadyFixtureAdapter{}
	substrate := daemon.NewTmuxSubstrate(fakeAdapter, "test-session")

	ledger := &remoteAgentReadyFixtureLedger{
		ready:          []core.BeadID{beadA, beadB},
		bothReopenedCh: make(chan struct{}),
	}
	collector := &stubEventCollector{}

	// worktreeFactory creates a minimal worktree with agent-task.md so
	// buildClaudeLaunchSpec can find the required file.
	worktreeFactory := func(_ context.Context, _ string, runID string, _ string) (string, func(), error) {
		wtDir, err := os.MkdirTemp("", "remote-agent-ready-wt-"+runID[:8]+"-")
		if err != nil {
			return "", nil, fmt.Errorf("remoteAgentReadyFixture: MkdirTemp: %w", err)
		}
		cleanup := func() { os.RemoveAll(wtDir) } //nolint:errcheck
		harmonikDir := filepath.Join(wtDir, ".harmonik")
		//nolint:gosec // G301: test-only temp dir
		if mkErr := os.MkdirAll(harmonikDir, 0o755); mkErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("remoteAgentReadyFixture: MkdirAll: %w", mkErr)
		}
		taskFile := filepath.Join(harmonikDir, "agent-task.md")
		//nolint:gosec // G306: test-only
		if wErr := os.WriteFile(taskFile, []byte("Please read .harmonik/agent-task.md and begin.\n"), 0o644); wErr != nil {
			cleanup()
			return "", nil, fmt.Errorf("remoteAgentReadyFixture: WriteFile: %w", wErr)
		}
		return wtDir, cleanup, nil
	}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               collector,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		MaxConcurrent:     2,
		Substrate:         substrate,
		AdapterRegistry2:  NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:   worktreeFactory,
		AgentReadyTimeout: 150 * time.Millisecond,
	})

	// 3 s total budget: AgentReadyTimeout (150ms) + bounded Wait (50ms) +
	// overhead ≪ 1s. The 3s deadline is the "test hangs" sentinel — with OLD
	// code the test would hit it (ctx cancelled → ReopenBead fails → no signal).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// Assert: both beads reopened with a live context, well before the deadline.
	select {
	case <-ledger.bothReopenedCh:
		// GREEN: fix is in place — both ReopenBead calls received context.Background().
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for both beads to be reopened with a live context; " +
			"pre-fix: sess.Wait(ctx) blocks until test deadline, ReopenBead then " +
			"receives a cancelled ctx and returns an error")
	}

	cancel()
	select {
	case <-loopDone:
	case <-time.After(5 * time.Second):
		t.Fatal("work loop did not exit after context cancellation")
	}
}
