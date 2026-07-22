package daemon

// tmuxsubstrate_orphan_hkbl2k6_test.go — proves killProcessWithGrace reaps the
// tmux pane's whole PROCESS GROUP, so a hosted agent cannot survive as an
// orphan when the daemon kills a run.
//
// Cases:
//   - TestBl2k6KillProcessWithGrace_KillsOrphanedGrandchild: a pane shell whose
//     grandchild IGNORES SIGTERM must not outlive the kill. This is the field
//     regression (hk-bl2k6): signalling the positive pid alone kills the shell,
//     reparents the agent to init, and leaks it for 40+ minutes. Verified to
//     FAIL against the pre-fix positive-pid implementation.
//   - TestBl2k6KillProcessWithGrace_RefusesNonPositivePid: pid 0, 1 and a
//     negative pid are refused and NOTHING is signalled. kill(-pid, …) with
//     pid==0 would signal the daemon's own process group; pid==1 would signal
//     every process the daemon may signal.
//   - TestBl2k6KillProcessWithGrace_FallsBackWhenNotGroupLeader: a process that
//     is NOT a group leader (the handler-style Setpgid+Pgid=daemon-group config)
//     still dies via the positive-pid fallback — the fix must never regress to
//     "nothing gets killed".
//
// Real OS processes are mandatory here: the property under test is a kernel
// signal-delivery property (group vs. process, reparenting to init), which the
// repo's virtual harnesses (internal/runexectest, internal/keepertest) cannot
// express. Precedents: internal/handler/session_test.go,
// internal/lifecycle/orphansweepbr_bi014a_test.go.
//
// Helper prefix: bl2k6 (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead ref: hk-bl2k6.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// bl2k6GracePeriod is the SIGTERM→SIGKILL grace used by these tests. It is far
// shorter than the production killGracePeriod (3s) so the escalation path runs
// fast; the behaviour under test is identical.
const bl2k6GracePeriod = 300 * time.Millisecond

// bl2k6PidLive reports whether pid is still present in the process table.
// A zombie counts as live — callers that spawned the process must reap it
// before probing.
func bl2k6PidLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return !errors.Is(err, syscall.ESRCH)
}

// bl2k6WaitPidGone polls until pid leaves the process table, up to timeout.
// It returns true when the pid is gone.
//
// The polling window is deliberately short (seconds) so PID reuse is not a
// practical concern: for a stale PID to be observed as "live" the kernel would
// have to recycle it within the window, and the only consequence would be a
// spurious FAILURE, never a spurious pass — a leaked process is always
// observed as live.
func bl2k6WaitPidGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !bl2k6PidLive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !bl2k6PidLive(pid)
}

// bl2k6ReadPidFile polls path until it holds a parseable PID, up to timeout.
func bl2k6ReadPidFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path) //nolint:gosec // G304: path is t.TempDir()-rooted
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 1 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("bl2k6: pid file %s never held a parseable PID within %s; the fixture shell failed to start", path, timeout)
	return 0
}

// TestBl2k6KillProcessWithGrace_KillsOrphanedGrandchild is the orphan proof.
//
// Fixture shape (mirrors a tmux pane): a shell that is its OWN process-group
// leader (tmux setsid()s every pane, so a pane PID is always a group leader),
// which backgrounds a grandchild and then waits. The grandchild installs
// `trap "" TERM` — that is load-bearing. Without it a stray SIGTERM would kill
// the grandchild and the test would pass against the broken implementation too,
// i.e. it would prove nothing.
//
// Want: killProcessWithGrace(shellPID, grace) leaves NO surviving grandchild.
// Regression shape: killProcessWithGrace signals the positive pid only, the
// shell dies, the grandchild is reparented to init and survives indefinitely —
// burning CPU and holding a provider slot on the operator's box.
func TestBl2k6KillProcessWithGrace_KillsOrphanedGrandchild(t *testing.T) {
	t.Parallel()

	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")

	// The grandchild ignores SIGTERM and sleeps well past the test's lifetime.
	// `trap "" TERM` sets SIG_IGN, which is inherited across exec and by any
	// child the shell spawns, so the whole grandchild subtree ignores SIGTERM.
	script := `sh -c 'trap "" TERM; sleep 300' &
echo $! > ` + pidFile + `
wait`

	cmd := exec.CommandContext(t.Context(), "sh", "-c", script) //nolint:gosec // G204: fixture script is a test constant
	// Setpgid with Pgid unset (0) makes the shell its own group leader — the
	// tmux-pane configuration this code path operates under.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		t.Fatalf("bl2k6 orphan: start fixture shell: %v", err)
	}
	shellPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(shellPID, syscall.SIGKILL) //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()                              //nolint:errcheck // fixture is signal-killed
	})

	grandchildPID := bl2k6ReadPidFile(t, pidFile, 5*time.Second)

	// Unconditional safety net: this test must never itself leak a process —
	// that would be the exact sin under repair. Runs even if the assertions
	// below fail or panic.
	defer func() {
		_ = syscall.Kill(grandchildPID, syscall.SIGKILL) //nolint:errcheck // cleanup error unactionable
	}()

	if !bl2k6PidLive(grandchildPID) {
		t.Fatalf("bl2k6 orphan: grandchild PID %d not live before the kill; the fixture never established the orphan scenario", grandchildPID)
	}

	killProcessWithGrace(t.Context(), shellPID, bl2k6GracePeriod)

	if !bl2k6WaitPidGone(grandchildPID, 5*time.Second) {
		t.Errorf("bl2k6 orphan: ORPHAN LEAKED — grandchild PID %d (child of pane shell PID %d) is STILL ALIVE 5s after killProcessWithGrace; want dead. "+
			"Regression shape: killProcessWithGrace signalled the positive pid instead of the process group (-pid), so only the pane shell died and the hosted agent was reparented to init, where it survives the daemon's kill (hk-bl2k6).",
			grandchildPID, shellPID)
	}
}

// TestBl2k6KillProcessWithGrace_RefusesNonPositivePid asserts the guard.
//
// Want: for pid <= 1, killProcessWithGrace issues ZERO signals. This is the
// single most dangerous line in the function: kill(-0, sig) signals the
// CALLER'S OWN process group — the daemon and every sibling it spawned — and
// kill(-1, sig) signals every process the daemon is permitted to signal.
//
// Not parallel: it swaps the package-level killProcessSignal seam.
func TestBl2k6KillProcessWithGrace_RefusesNonPositivePid(t *testing.T) {
	type bl2k6Signal struct {
		pid int
		sig syscall.Signal
	}

	var got []bl2k6Signal
	orig := killProcessSignal
	killProcessSignal = func(pid int, sig syscall.Signal) error {
		got = append(got, bl2k6Signal{pid: pid, sig: sig})
		return nil
	}
	t.Cleanup(func() { killProcessSignal = orig })

	for _, pid := range []int{0, 1, -1, -42} {
		got = nil
		killProcessWithGrace(t.Context(), pid, bl2k6GracePeriod)
		if len(got) != 0 {
			t.Errorf("bl2k6 guard: killProcessWithGrace(%d) issued %d signal(s) %+v; want 0. "+
				"Regression shape: an unguarded kill(-pid, sig) with pid<=1 signals the daemon's own process group (pid 0) or every signalable process (pid 1), killing the daemon and every concurrent run.",
				pid, len(got), got)
		}
	}
}

// TestBl2k6KillProcessWithGrace_FallsBackWhenNotGroupLeader asserts the
// fallback: when pid names a process that is NOT a group leader, kill(-pid, …)
// returns ESRCH and the whole TERM→grace→KILL sequence must fall back to the
// positive pid so behaviour never regresses to "nothing gets killed".
//
// The fixture uses the PRODUCTION handler spawn config (Setpgid with
// Pgid=daemon's group per HC-044 / PL-006a): the child JOINS the test's group
// rather than leading its own. It also ignores SIGTERM, so only the SIGKILL
// escalation on the fallback target can reap it.
func TestBl2k6KillProcessWithGrace_FallsBackWhenNotGroupLeader(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "sh", "-c", `trap "" TERM; sleep 300`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: syscall.Getpgrp()}

	if err := cmd.Start(); err != nil {
		t.Fatalf("bl2k6 fallback: start fixture: %v", err)
	}
	childPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(childPID, syscall.SIGKILL) //nolint:errcheck // cleanup error unactionable
		_ = cmd.Wait()                              //nolint:errcheck // fixture is signal-killed
	})

	// Let the shell install its trap.
	time.Sleep(100 * time.Millisecond)
	if !bl2k6PidLive(childPID) {
		t.Fatalf("bl2k6 fallback: child PID %d not live after Start", childPID)
	}
	// Sanity-check the fixture really is NOT a group leader, otherwise this
	// test would silently exercise the group path instead of the fallback.
	if pgid, err := syscall.Getpgid(childPID); err != nil {
		t.Fatalf("bl2k6 fallback: Getpgid(%d): %v", childPID, err)
	} else if pgid == childPID {
		t.Fatalf("bl2k6 fallback: child PID %d is its own group leader (pgid %d); fixture must NOT be a group leader for this case to exercise the fallback", childPID, pgid)
	}

	killProcessWithGrace(t.Context(), childPID, bl2k6GracePeriod)

	// Reap before probing: a SIGKILLed direct child lingers as a zombie, and a
	// zombie answers kill(pid, 0) successfully.
	_ = cmd.Wait() //nolint:errcheck // we expect a signal-killed exit

	if bl2k6PidLive(childPID) {
		t.Errorf("bl2k6 fallback: child PID %d still live after killProcessWithGrace and reap; want dead. "+
			"Regression shape: the group signal returned ESRCH (not a group leader) and the code failed to fall back to the positive pid, so nothing was killed at all (hk-bl2k6).",
			childPID)
	}
}
