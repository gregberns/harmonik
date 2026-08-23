package daemon

import (
	"context"

	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

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
	sess.runWait(context.Background())
	return ExportedRunWaitResult{ExitCode: sess.outcome.ExitCode}
}
