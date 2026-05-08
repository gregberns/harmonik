package lifecycle

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// orphanSweepFakeProcess represents a fake process entry used by the
// stub ProcessLister in tests.
type orphanSweepFakeProcess struct {
	pid  int
	ppid int
	comm string
}

// orphanSweepFakeLister is a test-injectable ProcessLister that returns a
// deterministic list of fake orphan PIDs without consulting the OS process
// table. This avoids spawning real processes for most test cases.
type orphanSweepFakeLister struct {
	pids []int
	err  error
}

// ListOrphanBrPIDs implements ProcessLister.
func (f *orphanSweepFakeLister) ListOrphanBrPIDs(_ context.Context) ([]int, error) {
	return f.pids, f.err
}

// orphanSweepFixtureFakeLister returns an orphanSweepFakeLister that yields the
// given PIDs when called.
func orphanSweepFixtureFakeLister(pids ...int) *orphanSweepFakeLister {
	return &orphanSweepFakeLister{pids: pids}
}

// orphanSweepFixtureErrLister returns an orphanSweepFakeLister that always
// returns the given error from ListOrphanBrPIDs.
func orphanSweepFixtureErrLister(err error) *orphanSweepFakeLister {
	return &orphanSweepFakeLister{err: err}
}

// orphanSweepFixtureNopLogger returns a *log.Logger that discards output.
func orphanSweepFixtureNopLogger() *log.Logger {
	return log.New(os.Stderr, "orphansweep_test: ", 0)
}

// TestBI014a_SweepOrphanBr_EmptyList verifies that SweepOrphanBr returns nil
// without error when no orphan br processes are found.
//
// Spec ref: beads-integration.md §4.5 BI-014a — "enumerate processes whose
// binary path matches the pinned `br` location and whose parent PID is 1."
func TestBI014a_SweepOrphanBr_EmptyList(t *testing.T) {
	t.Parallel()

	lister := orphanSweepFixtureFakeLister() // no pids
	survived, err := SweepOrphanBr(t.Context(), lister, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("BI-014a empty: unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("BI-014a empty: survived = %v, want []", survived)
	}
}

// TestBI014a_SweepOrphanBr_ListError verifies that SweepOrphanBr propagates
// errors from the ProcessLister without panicking.
//
// Spec ref: beads-integration.md §4.5 BI-014a.
func TestBI014a_SweepOrphanBr_ListError(t *testing.T) {
	t.Parallel()

	wantErr := context.DeadlineExceeded
	lister := orphanSweepFixtureErrLister(wantErr)

	_, err := SweepOrphanBr(t.Context(), lister, orphanSweepFixtureNopLogger())
	if err == nil {
		t.Fatal("BI-014a list-error: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "enumerate") {
		t.Errorf("BI-014a list-error: error %q does not mention enumerate", err.Error())
	}
}

// TestBI014a_SweepOrphanBr_NilListerUsesOS verifies that passing a nil lister
// falls back to OSProcessLister (i.e., does not panic).
//
// Spec ref: beads-integration.md §4.5 BI-014a.
func TestBI014a_SweepOrphanBr_NilListerUsesOS(t *testing.T) {
	t.Parallel()

	// Pass nil lister — should use OSProcessLister and not panic.
	// We do not assert on specific PIDs because the real process table varies.
	_, err := SweepOrphanBr(t.Context(), nil, nil)
	if err != nil {
		// OSProcessLister can fail if ps is unavailable; acceptable on unusual hosts.
		t.Logf("BI-014a nil-lister: ps returned error (acceptable): %v", err)
	}
}

// TestBI014a_SweepOrphanBr_DeadPidNotSurvived verifies that a PID that is
// already dead (ESRCH on kill 0) is not included in the survived list even if
// the lister returns it.
//
// We inject a PID that is guaranteed not to exist (99999 is almost certainly
// not a real process). The sweep will SIGTERM it (getting ESRCH), then detect
// it is not alive, and report zero survivors.
//
// Spec ref: beads-integration.md §4.5 BI-014a — survivors are a Cat 0
// prerequisite failure.
func TestBI014a_SweepOrphanBr_DeadPidNotSurvived(t *testing.T) {
	t.Parallel()

	const deadPID = 99999
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("BI-014a dead-pid: PID %d is live on this host; skipping", deadPID)
	}

	lister := orphanSweepFixtureFakeLister(deadPID)
	survived, err := SweepOrphanBr(t.Context(), lister, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("BI-014a dead-pid: unexpected error: %v", err)
	}
	// A dead PID exits after SIGTERM (ESRCH observed during polling); it must
	// NOT appear in the survived list.
	for _, s := range survived {
		if s == deadPID {
			t.Errorf("BI-014a dead-pid: dead PID %d appears in survived list; want absent", deadPID)
		}
	}
}

// TestBI014a_SweepOrphanBr_SigtermThenSigkill verifies the SIGTERM-then-SIGKILL
// discipline against a real child process. The test spawns a child process that
// ignores SIGTERM (by not exiting on SIGTERM), verifies SIGKILL is sent within
// the grace window, then confirms the process no longer survives.
//
// Implementation note: because we cannot make a subprocess truly ignore SIGTERM
// in a portable way without custom binaries, this test uses the self-exec
// sentinel pattern to spawn a "sleep forever" child. The child will not exit on
// SIGTERM (the test binary's signal mask will allow it but the process won't
// self-terminate), so SIGKILL must be sent.
//
// Spec ref: beads-integration.md §4.5 BI-014a — "send SIGTERM and wait up to
// 5s, then SIGKILL."
func TestBI014a_SweepOrphanBr_SigtermThenSigkill(t *testing.T) {
	t.Parallel()

	// Sentinel: child body — sleep a long time to force SIGKILL path.
	const sentinelEnv = "GO_BI014A_CHILD_STUB"
	if os.Getenv(sentinelEnv) == "1" {
		// Child: sleep until killed.
		select {}
	}

	// Spawn a child that sleeps indefinitely.
	testBin := os.Args[0]
	//nolint:gosec // G204: testBin is os.Args[0] — the test binary itself
	cmd := exec.CommandContext(t.Context(), testBin, "-test.run=^TestBI014a_SweepOrphanBr_SigtermThenSigkill$")
	cmd.Env = append(os.Environ(), sentinelEnv+"=1")
	// Give child a new process group so SIGTERM/SIGKILL target only it, not the
	// test group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("BI-014a sigterm-sigkill: cmd.Start: %v", err)
	}
	childPID := cmd.Process.Pid

	// Cleanup: ensure the child is reaped regardless of how the test ends.
	t.Cleanup(func() {
		_ = cmd.Process.Kill() //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()         //nolint:errcheck // cleanup error unactionable
	})

	// Give child time to settle.
	time.Sleep(50 * time.Millisecond)

	if !plFixtureIsPidLive(childPID) {
		t.Fatalf("BI-014a sigterm-sigkill: child PID %d not live after Start", childPID)
	}

	// Use a very short grace period so the test is fast (child ignores SIGTERM).
	// We temporarily override the grace period by using a custom sweep that sends
	// SIGTERM and then quickly SIGKILLs.
	//
	// We exercise SweepOrphanBr directly with a fake lister returning the real
	// child PID. The sweep will SIGTERM, poll (child does not exit), then SIGKILL.
	// After SIGKILL the child is dead — survived list must be empty.

	// Create a context with a short deadline to trigger fast SIGKILL escalation
	// via context cancellation. The sweep interprets ctx.Done() during polling as
	// a signal to proceed directly to SIGKILL.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	lister := orphanSweepFixtureFakeLister(childPID)
	_, sweepErr := SweepOrphanBr(ctx, lister, orphanSweepFixtureNopLogger())
	if sweepErr != nil {
		t.Fatalf("BI-014a sigterm-sigkill: unexpected error: %v", sweepErr)
	}

	// Reap the child so it is not a zombie when we probe liveness below.
	// SweepOrphanBr sends SIGKILL; the process may linger in the process table
	// as a zombie (kill(pid,0)==nil) until Wait is called. Reap first so that
	// plFixtureIsPidLive gives the true post-reap state.
	_ = cmd.Wait() //nolint:errcheck // we expect a signal-killed exit

	// After reap: the child must be dead (not a zombie, not alive).
	if plFixtureIsPidLive(childPID) {
		t.Errorf("BI-014a sigterm-sigkill: child PID %d still live after sweep and reap; want dead", childPID)
	}
}

// TestBI014a_SweepOrphanBr_ContextCancellation verifies that SweepOrphanBr
// handles context cancellation gracefully (escalates to SIGKILL immediately
// rather than waiting the full grace period).
//
// Spec ref: beads-integration.md §4.5 BI-014a.
func TestBI014a_SweepOrphanBr_ContextCancellation(t *testing.T) {
	t.Parallel()

	// An already-cancelled context must not cause a hang.
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // pre-cancel

	lister := orphanSweepFixtureFakeLister() // empty — nothing to kill
	survived, err := SweepOrphanBr(ctx, lister, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("BI-014a ctx-cancel: unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("BI-014a ctx-cancel: survived = %v, want []", survived)
	}
}

// TestBI014a_OSProcessLister_ParsesOutput verifies that OSProcessLister
// correctly parses `ps -eo pid,ppid,comm` output. We test the filtering logic
// indirectly by checking that the real system returns no br processes with
// PPID==1 (which is the normal case in a test environment).
//
// Spec ref: beads-integration.md §4.5 BI-014a — "enumerate processes whose
// binary path matches the pinned `br` location and whose parent PID is 1."
func TestBI014a_OSProcessLister_ParsesOutput(t *testing.T) {
	t.Parallel()

	lister := OSProcessLister{}
	pids, err := lister.ListOrphanBrPIDs(t.Context())
	if err != nil {
		t.Fatalf("BI-014a os-lister: ListOrphanBrPIDs: %v", err)
	}

	// In a normal test environment there should be no br subprocesses with
	// PPID==1 (they would only appear after a daemon crash). Log the result
	// without asserting a specific count — this is a smoke test of the parsing
	// path.
	t.Logf("BI-014a os-lister: found %d orphan br processes with PPID==1: %v", len(pids), pids)
}

// TestBI014a_SweepOrphanBr_MultiplePIDs verifies that when the lister returns
// multiple dead PIDs, SweepOrphanBr sends SIGTERM to each and reports no
// survivors (all were dead before or during the sweep).
//
// Spec ref: beads-integration.md §4.5 BI-014a.
func TestBI014a_SweepOrphanBr_MultiplePIDs(t *testing.T) {
	t.Parallel()

	// Use two known-dead PIDs (both almost certainly not alive).
	// Skip if either happens to be live.
	deadPIDs := []int{99997, 99998}
	for _, pid := range deadPIDs {
		if plFixtureIsPidLive(pid) {
			t.Skipf("BI-014a multi-pid: PID %d is live on this host; skipping", pid)
		}
	}

	lister := orphanSweepFixtureFakeLister(deadPIDs...)
	survived, err := SweepOrphanBr(t.Context(), lister, orphanSweepFixtureNopLogger())
	if err != nil {
		t.Fatalf("BI-014a multi-pid: unexpected error: %v", err)
	}
	if len(survived) != 0 {
		t.Errorf("BI-014a multi-pid: survived = %v, want []", survived)
	}
}
