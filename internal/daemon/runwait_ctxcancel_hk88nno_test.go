package daemon_test

// runwait_ctxcancel_hk88nno_test.go — unit tests for the runWait ctx.Done() path.
//
// Bead: hk-88nno — processDead EPERM/ESRCH disambiguation + runWait ctx-cancel test.
//
// These tests drive tmuxSubstrateSession.runWait through a forced ctx cancellation
// and assert the exit code recorded in outcome.  A deterministic processDead stub
// (injected via ExportedRunWaitWithDeadFn) replaces the OS-level syscall.Kill so
// no real OS process is required.  Tests are parallel and free of time-based flakes.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// TestRunWait_CtxCancel_DeadPid_ExitCode0 verifies that when ctx is cancelled
// and the pid is dead (processDead returns true), runWait reports exitCode=0.
// This is the correct path when claude exits cleanly and the ctx is then cancelled
// by the grace timer or workloop teardown (hk-cj0gm / hk-ajhqw root cause).
func TestRunWait_CtxCancel_DeadPid_ExitCode0(t *testing.T) {
	t.Parallel()

	deadFn := func(_ int) bool { return true } // always dead
	result := daemon.ExportedRunWaitWithDeadFn(12345, deadFn)

	if result.ExitCode != 0 {
		t.Errorf("runWait ctx-cancel with dead pid: got exitCode=%d, want 0", result.ExitCode)
	}
}

// TestRunWait_CtxCancel_AlivePid_ExitCodeMinus1 verifies that when ctx is cancelled
// and the pid is alive (processDead returns false — e.g. EPERM after PID recycle),
// runWait reports exitCode=-1 (unknown, not misclassified as clean exit).
func TestRunWait_CtxCancel_AlivePid_ExitCodeMinus1(t *testing.T) {
	t.Parallel()

	deadFn := func(_ int) bool { return false } // always alive
	result := daemon.ExportedRunWaitWithDeadFn(12345, deadFn)

	if result.ExitCode != -1 {
		t.Errorf("runWait ctx-cancel with alive pid: got exitCode=%d, want -1", result.ExitCode)
	}
}

// TestRunWait_CtxCancel_ZeroPid_ExitCodeMinus1 verifies that when pid==0 (PID
// lookup failed at spawn time), ctx-cancel produces exitCode=-1 because the
// s.pid > 0 guard in the ctx.Done() branch is not satisfied.
func TestRunWait_CtxCancel_ZeroPid_ExitCodeMinus1(t *testing.T) {
	t.Parallel()

	deadFn := func(_ int) bool { return true } // would return dead, but pid==0 skips the check
	result := daemon.ExportedRunWaitWithDeadFn(0, deadFn)

	if result.ExitCode != -1 {
		t.Errorf("runWait ctx-cancel with pid==0: got exitCode=%d, want -1", result.ExitCode)
	}
}
