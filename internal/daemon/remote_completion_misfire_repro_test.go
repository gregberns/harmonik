package daemon

// remote_completion_misfire_repro_test.go — RED reproduction of the remote
// review-loop "implementer_phase_complete exit=0 at ~5s while claude is still
// running on the worker" misfire.
//
// ROOT CAUSE (structural / deterministic — hypothesis H1):
//
// tmuxSubstrateSession.runWait's fast path (tmuxsubstrate.go ~line 1999-2009)
// concludes process death via deadFn(s.pid). In production deadFn is the
// package-level processDead, which is a LOCAL syscall:
//
//	syscall.Kill(s.pid, 0)  -> ESRCH => "dead"
//
// For a REMOTE run, s.pid is the WORKER's tmux pane PID (resolved over SSH at
// SpawnWindow time via the remote adapter — see spawnWindowVia, where
// adapter.WindowPanePID(ctx, pidTarget) runs on the worker's tmux server). That
// PID identifies a process in the WORKER's process table, NOT the daemon host's.
// kill(workerPID, 0) on the daemon host returns ESRCH because no such local
// process exists, so processDead returns true on the very first 500ms tick and
// runWait returns exitCodeClean=0 — while claude is still running on the worker.
//
// The liveness wait NEVER consults the run's SSHRunner: runWait uses local
// processDead(s.pid) on the fast path and only s.adapter.WindowPanePID on the
// s.pid==0 SLOW path. Because the remote spawn-time PID fetch SUCCEEDS on a fast
// LAN (s.pid > 0), the slow path is never reached and the local-syscall fast
// path is taken EVERY remote run — hence the CONSISTENT ~5s exit_code=0, not a
// flaky SSH-timeout (which rules out H2).
//
// This test pins that misfire. It constructs a tmuxSubstrateSession exactly as
// the remote spawn path leaves it — a worker-side pane PID that is NOT present in
// the local process table, an adapter whose WindowPanePID reports the worker pane
// ALIVE — drives the PRODUCTION runWait (deadFn = nil => real processDead), and
// asserts that runWait does NOT declare a clean exit while the worker process is
// alive.
//
// THE FIX (hk-r1zq): a session spawned on the worker is now marked remote:true
// (set by perRunSubstrate.spawnWindowRemote via spawnWindowVia). runWait skips
// the local-kill fast path for remote sessions and polls worker-side liveness via
// s.adapter.WindowPanePID — which, for a remote run, resolves #{pane_pid} on the
// WORKER's tmux server over the run's SSH runner. This test is the REGRESSION PIN:
// it passes with the fix, and fails again if the `&& !s.remote` guard is removed
// from runWait's fast path (the remote session would resume taking the local
// kill(workerPID,0) path and misfire to exitCodeClean while the worker pane lives).

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// workerLivePaneAdapter models the WORKER's tmux server for a remote run: its
// pane PID is always resolvable and never errors, i.e. the pane (and claude
// inside it) is ALIVE for the duration of the test. Only WindowPanePID is
// exercised by runWait; every other method is an inert stub.
type workerLivePaneAdapter struct {
	workerPID int
	calls     atomic.Int32
}

func (a *workerLivePaneAdapter) WindowPanePID(_ context.Context, _ tmuxPkg.WindowHandle) (int, error) {
	a.calls.Add(1)
	return a.workerPID, nil // pane alive on the worker — no error, valid PID
}

func (a *workerLivePaneAdapter) ProbeTmux(_ context.Context) error                { return nil }
func (a *workerLivePaneAdapter) ListSessions(_ context.Context) ([]string, error) { return nil, nil }
func (a *workerLivePaneAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (a *workerLivePaneAdapter) NewWindowIn(_ context.Context, _ tmuxPkg.NewWindowIn) tmuxPkg.Outcome {
	return tmuxPkg.Outcome{}
}

func (a *workerLivePaneAdapter) KillWindow(_ context.Context, _ tmuxPkg.WindowHandle) error {
	return nil
}

func (a *workerLivePaneAdapter) WindowPaneID(_ context.Context, _ tmuxPkg.WindowHandle) (string, error) {
	return "", nil
}
func (a *workerLivePaneAdapter) KillSession(_ context.Context, _ string) error          { return nil }
func (a *workerLivePaneAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (a *workerLivePaneAdapter) PasteBuffer(_ context.Context, _, _ string) error       { return nil }
func (a *workerLivePaneAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (a *workerLivePaneAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (a *workerLivePaneAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (a *workerLivePaneAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

// TestRemoteCompletionMisfire_FastPathUsesLocalKillOnWorkerPID reproduces the
// deterministic remote misfire. It is the production code path: deadFn is left
// nil so runWait uses the real package-level processDead (local syscall.Kill).
func TestRemoteCompletionMisfire_FastPathUsesLocalKillOnWorkerPID(t *testing.T) {
	// workerPID: a PID that identifies a LIVE process inside the worker's table
	// but is NOT present in the daemon host's process table. We model that with a
	// PID above the OS allocation ceiling so kill(pid,0) is guaranteed ESRCH
	// locally — exactly the steady-state local view of any remote worker PID.
	const workerPID = 0x3FFFFFFF // ~1.07e9, far above any real local PID

	// Sanity: confirm the local view of this worker PID is "dead" (ESRCH). This is
	// the structural fact runWait wrongly relies on for a remote run.
	if !processDead(workerPID) {
		t.Skipf("worker PID %d unexpectedly maps to a live LOCAL process; "+
			"cannot model the remote 'local kill sees ESRCH' condition on this host", workerPID)
	}

	adapter := &workerLivePaneAdapter{workerPID: workerPID}

	sess := &tmuxSubstrateSession{
		adapter:   adapter,
		handle:    "worker-default:hk-remote-misfire/i1",
		paneID:    "%4242",
		pidTarget: tmuxPkg.WindowHandle("%4242"),
		pid:       workerPID, // remote spawn captured the WORKER's pane PID
		remote:    true,      // hk-r1zq: worker-hosted — runWait must poll worker liveness, not local kill
		waitDone:  make(chan struct{}),
		// isProcessDead left nil => runWait uses the PRODUCTION processDead
		// (local syscall.Kill) on the fast path. With remote:true the fast path is
		// skipped and runWait polls s.adapter.WindowPanePID (the worker pane), which
		// reports ALIVE for the whole window — so a CORRECT runWait keeps polling.
	}

	// Drive runWait with a live (non-cancelled) ctx, bounded so a correct
	// implementation that keeps polling the live worker pane does not hang the
	// test. The pane is alive for the whole window, so a correct runWait must NOT
	// return on its own before pollWindow elapses.
	const pollWindow = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), pollWindow)
	defer cancel()

	done := make(chan struct{})
	go func() {
		sess.runWait(ctx)
		close(done)
	}()

	var returnedEarly bool
	select {
	case <-done:
		// runWait returned on its own BEFORE ctx expired — it concluded the
		// process exited while the worker pane is still alive. That is the misfire.
		returnedEarly = true
	case <-ctx.Done():
		// ctx expired first; runWait kept polling the live worker pane (correct).
		<-done // let runWait observe ctx.Done and finish
		returnedEarly = false
	}

	if returnedEarly && sess.outcome.ExitCode == exitCodeClean {
		t.Fatalf(
			"REMOTE COMPLETION MISFIRE REPRODUCED: runWait declared exitCodeClean=%d "+
				"after %s while the worker pane was still ALIVE (WindowPanePID returned "+
				"worker PID %d, no error, %d times).\n"+
				"Cause: runWait's fast path called processDead(s.pid) = local "+
				"syscall.Kill(%d, 0), which returns ESRCH because the WORKER's pane PID "+
				"is not in the daemon host's process table. The liveness wait never "+
				"consults the run's SSHRunner / worker tmux for the fast path.\n"+
				"Expected: runWait must NOT conclude a clean exit while the worker "+
				"process is alive (it should poll worker liveness over the run's runner).",
			sess.outcome.ExitCode, sess.outcome.Duration, workerPID, adapter.calls.Load(), workerPID,
		)
	}

	if returnedEarly {
		t.Fatalf("runWait returned early with exit=%d (non-clean) while worker pane alive; "+
			"still a misfire — it must keep polling the live worker process", sess.outcome.ExitCode)
	}
}
