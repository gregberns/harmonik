package daemon_test

// worktree_teardown_hk68pvl_test.go — regression tests for hk-68pvl.
//
// Bug: in a review-loop harmonik run, the daemon could call
// `git worktree remove --force` on an implementer's worktree WHILE the
// implementer claude session was still live (mid `go test`), because on the
// tmux substrate path tmuxSubstrateSession.Wait returns ctx.Err() the instant
// the run ctx is cancelled even though the hosted process is still alive. The
// agent's `git add`/commit then landed in a deleted directory and the run was
// recorded as a false `no_commit_during_implementer ... exit=0`.
//
// Fix (load-bearing): forceTeardownSession force-kills the session and blocks
// until the hosted process is reaped, and the drivers register it as a deferred
// backstop so it always runs before the beadRunOne-level wtCleanup removes the
// worktree.
//
// This test asserts the ordering invariant on the seam the fix introduced:
// when the worktree removal is sequenced after forceTeardownSession (as the
// drivers now arrange via defer LIFO), the removal NEVER runs while the session
// still reports a live process — even when the run ctx is already cancelled
// (the production trigger).

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// hk68pvlRacySession models the production substrate session for this race:
//
//   - alive starts true (the hosted claude is running, e.g. mid `go test`).
//   - Wait returns ctx.Err() immediately when ctx is cancelled WITHOUT making
//     the process dead — exactly the tmuxSubstrateSession.Wait early-return that
//     caused the bug.
//   - Kill blocks for a short, observable interval (simulating
//     killProcessWithGrace) and only then marks the process dead. This is the
//     synchronous teardown the fix relies on.
type hk68pvlRacySession struct {
	alive     atomic.Bool
	killCalls atomic.Int32
	killDelay time.Duration
}

func newHk68pvlRacySession(killDelay time.Duration) *hk68pvlRacySession {
	s := &hk68pvlRacySession{killDelay: killDelay}
	s.alive.Store(true)
	return s
}

func (s *hk68pvlRacySession) IsAlive() bool { return s.alive.Load() }

func (s *hk68pvlRacySession) SendInput(_ context.Context, _ string) error { return nil }

// Kill blocks until the (simulated) process group is reaped, then marks the
// session dead. Idempotent: subsequent calls are no-ops once dead.
func (s *hk68pvlRacySession) Kill(_ context.Context) error {
	s.killCalls.Add(1)
	if !s.alive.Load() {
		return nil
	}
	// Simulate killProcessWithGrace's synchronous SIGTERM→grace→SIGKILL window.
	time.Sleep(s.killDelay)
	s.alive.Store(false)
	return nil
}

// Wait mimics tmuxSubstrateSession.Wait: returns the instant ctx is cancelled
// while leaving the process alive (the racy early return). With a live ctx it
// blocks until the process is dead.
func (s *hk68pvlRacySession) Wait(ctx context.Context) error {
	for {
		if !s.alive.Load() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func (s *hk68pvlRacySession) Outcome() handler.Outcome { return handler.Outcome{} }
func (s *hk68pvlRacySession) Stdout() io.Reader        { return nil }
func (s *hk68pvlRacySession) Stderr() io.Reader        { return nil }
func (s *hk68pvlRacySession) CloseStdin() error        { return nil }
func (s *hk68pvlRacySession) Machine() *hclifecycle.Machine {
	return hclifecycle.New("stub", "stub")
}

var _ handler.Session = (*hk68pvlRacySession)(nil)

// TestForceTeardownSession_RemovalNeverRacesLiveSession asserts the core fix
// invariant: when the worktree removal is sequenced after forceTeardownSession
// (the LIFO-defer arrangement the drivers now use), the removal observes the
// session already dead — even though the run ctx was cancelled, which is what
// waitWithSocketGrace's sess.Wait(ctx) would have seen on the substrate path.
func TestForceTeardownSession_RemovalNeverRacesLiveSession(t *testing.T) {
	sess := newHk68pvlRacySession(40 * time.Millisecond)

	// Run ctx is already cancelled — the production trigger. A bare sess.Wait
	// here returns immediately while the process is still alive.
	runCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sess.Wait(runCtx); err == nil {
		t.Fatal("precondition: Wait should return ctx.Err() while the session is still alive")
	}
	if !sess.IsAlive() {
		t.Fatal("precondition: cancelled Wait must NOT have killed the live session")
	}

	// Model the driver+beadRunOne arrangement: teardown runs, THEN removal.
	var aliveAtRemoval bool
	removeWorktree := func() { aliveAtRemoval = sess.IsAlive() }

	daemon.ExportedForceTeardownSession(sess) // deferred backstop in production
	removeWorktree()                          // deferred wtCleanup in production

	if aliveAtRemoval {
		t.Fatal("worktree removal ran while the session was still live — hk-68pvl race not closed")
	}
	if sess.killCalls.Load() == 0 {
		t.Fatal("forceTeardownSession did not Kill the session")
	}
}

// TestForceTeardownSession_NilIsNoop guards the nil-session path so the deferred
// backstop is safe to register before a session is confirmed launched.
func TestForceTeardownSession_NilIsNoop(t *testing.T) {
	// Must not panic.
	daemon.ExportedForceTeardownSession(nil)
}

// TestForceTeardownSession_Idempotent confirms the backstop is safe to call
// more than once (the drivers may Kill in the happy path and again via defer).
func TestForceTeardownSession_Idempotent(t *testing.T) {
	sess := newHk68pvlRacySession(0)
	daemon.ExportedForceTeardownSession(sess)
	daemon.ExportedForceTeardownSession(sess)
	if sess.IsAlive() {
		t.Fatal("session should be dead after teardown")
	}
	if got := sess.killCalls.Load(); got < 2 {
		t.Fatalf("expected Kill called on each invocation, got %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-68pvl production defer-ordering — real beadRunOne/runReviewLoop path
// ─────────────────────────────────────────────────────────────────────────────

// hk68pvlFakeSubstrateSession is a handler.SubstrateSession backed by a real OS
// process spawned with context.Background(). Its Wait(ctx) returns ctx.Err()
// immediately when ctx is cancelled WITHOUT killing the process — this is the
// racy tmux-substrate behaviour that caused hk-68pvl. Kill terminates the
// process synchronously (SIGKILL + wait for reap), so after Kill returns the
// PID no longer exists in the process table.
type hk68pvlFakeSubstrateSession struct {
	pid    int
	waitCh chan struct{} // closed by background goroutine when cmd.Wait() returns
	once   sync.Once
}

// Wait returns ctx.Err() immediately when ctx is cancelled (racy: process alive).
// Blocks until the process exits when ctx is live.
func (s *hk68pvlFakeSubstrateSession) Wait(ctx context.Context) error {
	select {
	case <-s.waitCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Kill sends SIGKILL and blocks until the OS process is fully reaped. Idempotent.
func (s *hk68pvlFakeSubstrateSession) Kill(_ context.Context) error {
	s.once.Do(func() {
		proc, err := os.FindProcess(s.pid)
		if err == nil {
			_ = proc.Kill()
		}
		<-s.waitCh // wait for cmd.Wait() goroutine to signal reap
	})
	return nil
}

func (s *hk68pvlFakeSubstrateSession) PID() int                 { return s.pid }
func (s *hk68pvlFakeSubstrateSession) Stdout() io.Reader        { return nil } // nil = no watcher (tmux-path)
func (s *hk68pvlFakeSubstrateSession) Outcome() handler.Outcome { return handler.Outcome{ExitCode: 1} }

var _ handler.SubstrateSession = (*hk68pvlFakeSubstrateSession)(nil)

// hk68pvlFakeSubstrate is a handler.Substrate that spawns a real OS process
// using exec.Command (not exec.CommandContext), so ctx cancellation in the
// workloop does NOT automatically kill the subprocess. This creates the
// condition where sess.Wait(ctx) returns early (ctx.Err()) while the process
// is still alive — the production trigger for hk-68pvl.
//
// lastSess is updated on each SpawnWindow call so the test can observe the
// spawned process's PID after the run completes.
type hk68pvlFakeSubstrate struct {
	lastSess atomic.Pointer[hk68pvlFakeSubstrateSession]
}

func (sub *hk68pvlFakeSubstrate) SpawnWindow(_ context.Context, in handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	if len(in.Argv) == 0 {
		return nil, fmt.Errorf("hk68pvlFakeSubstrate: empty Argv")
	}
	// exec.Command (not exec.CommandContext) so ctx cancel does not kill it.
	cmd := exec.Command(in.Argv[0], in.Argv[1:]...) //nolint:gosec // G204: test fixture
	cmd.Env = in.Env
	cmd.Dir = in.Cwd
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("hk68pvlFakeSubstrate: cmd.Start: %w", err)
	}
	sess := &hk68pvlFakeSubstrateSession{
		pid:    cmd.Process.Pid,
		waitCh: make(chan struct{}),
	}
	go func() {
		_ = cmd.Wait()
		close(sess.waitCh)
	}()
	sub.lastSess.Store(sess)
	return sess, nil
}

var _ handler.Substrate = (*hk68pvlFakeSubstrate)(nil)

// TestBeadRunOne_DeferOrdering_WorktreeCleanupAfterSessionTeardown asserts that
// the worktree-factory cleanup ("removal") is sequenced after all implementer
// sessions have been torn down in the real beadRunOne → runReviewLoop dispatch
// path.
//
// Setup: a fake Substrate whose Wait(ctx) returns early on ctx cancel (the
// hk-68pvl racy behaviour), with an OS process that stays alive until Kill is
// called. A spy worktreeFactory checks whether the process is still alive when
// its cleanup function fires. After ExportedRunWorkLoop returns (which drains
// all in-flight goroutines via the internal WaitGroup), the test asserts the
// process was dead when cleanup ran.
//
// This test covers the gap flagged by the hk-68pvl reviewer: the existing
// TestForceTeardownSession_RemovalNeverRacesLiveSession exercises
// forceTeardownSession in isolation, but does NOT verify the LIFO-defer wiring
// inside beadRunOne/runReviewLoop. The present test fails if the defers are
// removed or misorderd so that cleanup runs while a session is still live.
//
// Bead ref: hk-82jwm (strengthens hk-68pvl regression coverage).
func TestBeadRunOne_DeferOrdering_WorktreeCleanupAfterSessionTeardown(t *testing.T) {
	t.Parallel()

	// workloopFixtureProjectDir + workloopFixtureGitRepo provide a real git
	// repository so resolveParentCommit (inside beadRunOne) succeeds.
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	substrate := &hk68pvlFakeSubstrate{}

	// processAliveAtCleanup records whether the spawned OS process was still
	// alive when the spy worktreeFactory cleanup function fired.
	var processAliveAtCleanup bool

	// spyFactory creates a minimal temp worktree directory (not a real git
	// worktree — buildClaudeLaunchSpec only needs a writable directory) and
	// returns a cleanup that probes the spawned process's liveness.
	spyFactory := func(_ context.Context, _, runID, _ string) (string, func(), error) {
		sfx := runID
		if len(sfx) > 8 {
			sfx = sfx[:8]
		}
		wtDir, err := os.MkdirTemp("", "hk68pvl-order-"+sfx+"-")
		if err != nil {
			return "", nil, err
		}
		//nolint:gosec // G301: test-only temp directory
		if mkErr := os.MkdirAll(filepath.Join(wtDir, ".harmonik"), 0o755); mkErr != nil {
			_ = os.RemoveAll(wtDir)
			return "", nil, mkErr
		}
		cleanup := func() {
			defer func() { _ = os.RemoveAll(wtDir) }()
			sess := substrate.lastSess.Load()
			if sess == nil {
				// No session spawned (e.g. buildClaudeLaunchSpec failed).
				return
			}
			// On Unix, os.FindProcess always succeeds; Signal(0) is the liveness probe:
			// returns nil when the process exists (alive or zombie), ESRCH when reaped.
			proc, findErr := os.FindProcess(sess.pid)
			if findErr != nil {
				processAliveAtCleanup = false
				return
			}
			processAliveAtCleanup = (proc.Signal(syscall.Signal(0)) == nil)
		}
		return wtDir, cleanup, nil
	}

	ledger := &stubBeadLedger{
		ready: []core.BeadID{"hk-68pvl-defer-order-001"},
	}
	collector := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:  ledger,
		Bus:        collector,
		ProjectDir: projectDir,
		// /bin/sh -c "sleep 60" runs a real OS process that the fake substrate
		// will NOT kill on ctx cancel, reproducing the hk-68pvl racy scenario.
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "sleep 60"},
		IntentLogDir:  filepath.Join(projectDir, ".harmonik", "beads-intents"),
		// Empty registry: ForAgent returns an error → waitAgentReady is skipped,
		// which is required so the test does not block on an agent_ready event
		// that the fake substrate never emits.
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		// review-loop mode: exercises the runReviewLoop path where
		// defer forceTeardownSession(implSessForTeardown) guards the outer
		// defer wtCleanup() in beadRunOne.
		WorkflowModeDefault: core.WorkflowModeReviewLoop,
		WorktreeFactory:     spyFactory,
		Substrate:           substrate,
	})

	// 500 ms: short enough that the context cancels while "sleep 60" is running,
	// triggering the racy substrate path (sess.Wait returns ctx.Err() with the
	// process still alive).
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps)
	}()

	// runWorkLoop drains all in-flight goroutines via its internal WaitGroup
	// before returning, so loopDone closing guarantees wtCleanup has already
	// fired. Allow 30 s for the ~3 s stopHookGrace window + processing.
	select {
	case <-loopDone:
	case <-time.After(30 * time.Second):
		t.Fatal("workloop did not exit within 30 s — possible deadlock")
	}

	if substrate.lastSess.Load() == nil {
		t.Skip("no session was spawned (buildClaudeLaunchSpec failed) — test is inconclusive")
	}

	if processAliveAtCleanup {
		t.Fatal("worktree cleanup ran while the implementer process was still alive — " +
			"hk-68pvl defer ordering not maintained in beadRunOne/runReviewLoop")
	}
}
