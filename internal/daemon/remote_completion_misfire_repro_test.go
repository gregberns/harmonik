package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

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
	const workerPID = 0x3FFFFFFF // ~1.07e9, far above any real local PID

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
	}

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
		returnedEarly = true
	case <-ctx.Done():
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
