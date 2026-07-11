package daemon_test

// dot_auto_status_hkd9snr_test.go — regression test for the process-group
// kill treatment applied to the go build/vet auto-status gate (hk-d9snr,
// commit a63275e1).
//
// Before the fix, runAutoStatusInspection's LOCAL go build/vet exec.Cmd had
// no SysProcAttr.Setpgid, no custom Cancel, and no WaitDelay — the same risk
// class the sh -c gate hit before hk-me8ru: on ctx cancellation/timeout, only
// the direct `go` PID is signaled. If `go` has forked a child that outlives
// it and still holds the command's stdout pipe open, CombinedOutput blocks
// until that orphan exits on its own — regardless of the ctx deadline.
//
// This test replaces `go` on PATH with a fake script that backgrounds a
// long-sleeping grandchild and then ITSELF keeps running past ctx's deadline
// (simulating that risk class deterministically, without depending on the
// real go toolchain's actual forking behavior). The leader staying alive
// matters: Go's exec.Cmd.Cancel is only invoked while the direct child
// process is still running at ctx-done time — if the leader had already
// exited on its own, Cancel is never called at all (verified empirically),
// and only the separate WaitDelay mechanism would unblock the caller, without
// actually killing anything. Keeping the leader alive exercises the real
// hk-me8ru/hk-d9snr mechanism: ctx fires while the leader is still up,
// Cancel's negative-PGID SIGKILL reaps the whole group (leader + orphan)
// together. Without the fix, only the leader's PID would be signaled,
// leaving the orphan to run for its full lifetime.
//
// Bead ref: hk-d9snr.

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// hkd9snrProcessAlive reports whether pid is still alive, via a signal-0
// probe (POSIX: kill(pid, 0) succeeds iff the process exists and is
// signalable). Used instead of timing alone: Go's exec.Cmd.WaitDelay forces
// the command's own I/O pipes closed once the direct child has exited,
// independent of whether SysProcAttr.Setpgid + a group-wide Cancel actually
// reaped the process group — so bounding CombinedOutput's elapsed time alone
// cannot distinguish "the orphan was actually killed" from "WaitDelay merely
// stopped waiting on it while it keeps running." Checking the orphan's own
// liveness closes that gap.
func hkd9snrProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// TestAutoStatusInspection_ProcessGroupKillsOrphanedGrandchild is the
// hk-d9snr regression: an orphaned grandchild of the local `go` build/vet
// command, still holding the stdout pipe open past ctx's deadline, must be
// reaped via the whole process group — not left to block CombinedOutput for
// its full natural lifetime, and not left running as a leaked orphan even if
// something else (e.g. WaitDelay's pipe-close fallback) unblocks the caller.
func TestAutoStatusInspection_ProcessGroupKillsOrphanedGrandchild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Setpgid/negative-PID kill is POSIX-only")
	}
	// Mutates the process PATH env var — cannot run in parallel with tests
	// that assume the real `go` toolchain resolves on PATH.

	dir := t.TempDir()
	autoStatusWriteFile(t, dir, "go.mod", "module example.com/hkd9snrtest\n\ngo 1.21\n")

	// Fake `go`: forks a grandchild that sleeps far longer than the ctx
	// deadline below and inherits stdout, THEN the leader itself also keeps
	// running (well past the ctx deadline) so ctx fires while the leader is
	// still alive — mirroring the "sh -c forks a lingering child" shape
	// hk-me8ru fixed for the shell gate, reproduced here for the go build/vet
	// gate. It records the grandchild's PID to pidFile so the test can
	// directly verify the orphan was actually killed (not merely
	// stopped-waiting-on).
	fakeBinDir := t.TempDir()
	pidFile := filepath.Join(fakeBinDir, "orphan.pid")
	fakeGoScript := "#!/bin/sh\nsleep 20 &\necho $! > " + pidFile + "\nsleep 30\n"
	fakeGoPath := filepath.Join(fakeBinDir, "go")
	//nolint:gosec // G306: must be executable
	if err := os.WriteFile(fakeGoPath, []byte(fakeGoScript), 0o755); err != nil {
		t.Fatalf("write fake go script: %v", err)
	}

	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	daemon.ExportedRunAutoStatusInspection(ctx, dir)
	elapsed := time.Since(start)

	// Generous ceiling (10s) — well above the 5s WaitDelay backstop (the
	// fallback path if the group kill were somehow incomplete) and the ctx's
	// 500ms deadline, but far below the leader's 30s / orphan's 20s sleep, so
	// a saturated -race runner can't blow it while still cleanly
	// distinguishing "reaped promptly" from "blocked on the full lifetime."
	if elapsed > 10*time.Second {
		t.Fatalf("runAutoStatusInspection took %v; want < 10s — the orphaned "+
			"grandchild of the fake `go` binary was not reaped via process-group "+
			"kill (hk-d9snr regression)", elapsed)
	}

	// The load-bearing assertion: the orphan itself must actually be dead —
	// not merely "stopped being waited on" (which WaitDelay alone would give
	// even without a real process-group kill). Poll briefly: SIGKILL delivery
	// and reaping aren't instantaneous under a loaded runner.
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read orphan pidfile: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		t.Fatalf("parse orphan pid %q: %v", pidBytes, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for hkd9snrProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if hkd9snrProcessAlive(pid) {
		t.Fatalf("orphan grandchild pid %d is still alive after runAutoStatusInspection "+
			"returned — process-group kill did not reap it (hk-d9snr regression); a bounded "+
			"elapsed time alone can look fine here because WaitDelay force-closes the pipe "+
			"independent of whether the group was actually killed", pid)
	}
}
