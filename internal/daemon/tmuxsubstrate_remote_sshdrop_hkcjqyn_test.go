package daemon_test

// tmuxsubstrate_remote_sshdrop_hkcjqyn_test.go — regression for the remote
// runWait SSH-drop false-green (hk-cjqyn).
//
// Bug: for a REMOTE session, runWait polls worker-side liveness via
// s.adapter.WindowPanePID over the run's SSH runner. If the SSH tunnel drops
// mid-run (network blip, sshd restart), ssh exits 255 and WindowPanePID errors.
// The worker-poll branch treated ANY error identically to "pane closed because
// claude exited" and latched ExitCode=exitCodeClean(0) — while claude is still
// running on the worker. With the Stop-hook socket outcome absent (it never
// arrived over the dropped tunnel), workloop's close-on-exit-0 heuristic
// (socketOutcome==nil && exitCode==exitCodeClean && !watcherFailed) then
// auto-closes the incomplete remote work as a false green (G8/H4/H5).
//
// Fix (hk-cjqyn): an SSH transport failure (a 255-coded error, surfaced as
// *tmux.ErrTmuxFailure{ExitCode:255} over the OSAdapter+SSHRunner path) is now
// distinguished from a genuine pane-gone. On an SSH drop runWait latches
// exitCodeUnknown ("unverified"), matching the ctx-cancel sibling, so the
// crashed/incomplete branch handles it instead of the close-on-exit-0 fallback.

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestRemoteRunWait_SSHDrop_LatchesUnknown_HKCJQYN asserts that an SSH transport
// drop mid-run (ErrTmuxFailure{ExitCode:255} from the worker pane poll) latches
// exitCodeUnknown (-1), NOT exitCodeClean (0). exitCodeClean would drive
// workloop's close-on-exit-0 fallback to auto-close incomplete remote work.
func TestRemoteRunWait_SSHDrop_LatchesUnknown_HKCJQYN(t *testing.T) {
	// The exact error shape produced when a tmux OSAdapter command runs over the
	// SSHRunner and ssh exits 255 on a transport failure (osadapter wraps the
	// ssh *exec.ExitError as *ErrTmuxFailure, capturing its exit code).
	sshDrop := &tmux.ErrTmuxFailure{Op: "display-message", ExitCode: 255, Stderr: "ssh: connect to host worker port 22: Connection refused"}

	res := daemon.ExportedRunWaitRemotePanePIDErr(sshDrop)

	const exitCodeUnknown = -1
	if res.ExitCode != exitCodeUnknown {
		t.Fatalf("hk-cjqyn regression: SSH-drop mid-run latched ExitCode=%d, want exitCodeUnknown(%d). "+
			"exitCodeClean(0) here drives workloop's close-on-exit-0 fallback to auto-close incomplete "+
			"remote work as a false green (G8/H4/H5).", res.ExitCode, exitCodeUnknown)
	}
}

// TestRemoteRunWait_PaneGone_LatchesClean_HKCJQYN is the counter-case: a genuine
// pane-gone (a non-SSH error, e.g. the worker pane closed because claude
// exited) still latches exitCodeClean(0) so the normal completion path fires.
// This guards against the fix over-correcting into a false-red.
func TestRemoteRunWait_PaneGone_LatchesClean_HKCJQYN(t *testing.T) {
	paneGone := errors.New("tmux: display-message exited 1: can't find window")

	res := daemon.ExportedRunWaitRemotePanePIDErr(paneGone)

	const exitCodeClean = 0
	if res.ExitCode != exitCodeClean {
		t.Fatalf("hk-cjqyn: genuine pane-gone latched ExitCode=%d, want exitCodeClean(%d). "+
			"A non-SSH pane-gone means claude exited and the worker pane closed — the normal "+
			"completion path must still fire.", res.ExitCode, exitCodeClean)
	}
}
