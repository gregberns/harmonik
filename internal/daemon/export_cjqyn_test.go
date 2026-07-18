package daemon

// export_cjqyn_test.go — test seam for the remote runWait SSH-drop false-green
// regression (hk-cjqyn). Kept in its own file (not the shared export_test.go)
// to avoid a shared-index race with other crews editing export_test.go
// concurrently.

import (
	"context"

	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// errPanePIDAdapter is a noopTmuxAdapter whose WindowPanePID poll always fails
// with a fixed error. Used by the hk-cjqyn remote-runWait seam to model a
// worker-side pane poll that errors (SSH transport drop vs genuine pane-gone).
type errPanePIDAdapter struct {
	noopTmuxAdapter
	err error
}

func (a *errPanePIDAdapter) WindowPanePID(_ context.Context, _ tmuxpkg.WindowHandle) (int, error) {
	return 0, a.err
}

// ExportedRunWaitRemotePanePIDErr drives runWait for a REMOTE session whose
// worker-side WindowPanePID poll returns panePIDErr, then returns the exit code
// recorded in outcome. This exercises the tick-poll worker branch (not the
// ctx-cancel branch): an SSH transport drop (a 255-coded error) must latch
// exitCodeUnknown so incomplete work is not auto-closed as a false green, while
// a genuine pane-gone must latch exitCodeClean (hk-cjqyn).
func ExportedRunWaitRemotePanePIDErr(panePIDErr error) ExportedRunWaitResult {
	sess := &tmuxSubstrateSession{
		adapter:  &errPanePIDAdapter{err: panePIDErr},
		handle:   "test-session:hk-cjqyn-win",
		pid:      0,
		remote:   true,
		waitDone: make(chan struct{}),
	}
	// No ctx cancel: runWait blocks until the first ticker tick (~500ms), where
	// the worker WindowPanePID poll fires and returns via panePIDErr.
	sess.runWait(context.Background())
	return ExportedRunWaitResult{ExitCode: sess.outcome.ExitCode}
}
