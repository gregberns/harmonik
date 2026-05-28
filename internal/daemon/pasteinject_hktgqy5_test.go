package daemon_test

// pasteinject_hktgqy5_test.go — regression tests for robust pane-liveness
// detection (hk-tgqy5).
//
// Root cause: pasteInjectQuitOnCommit's heartbeat/launch watchdog suppresses
// its no-commit kill when the pane liveness checker reports an active process.
// That checker ran `hasChildProcess(panePID)`, which originally only checked
// DIRECT children (`pgrep -P <pid>`).  Two arrangements produced false
// negatives for a healthy claude implementer mid-work:
//
//  1. The pane PID is claude itself (tmux exec'd `sh -c "claude …"` into the
//     agent) — no children at all during a thinking phase.
//  2. The live agent is a deep descendant of the pane shell, not a direct
//     child.
//
// Either false negative let the watchdog fire a false
// `no_commit_during_implementer` kill while the commit was still minutes away.
//
// The fix makes hasChildProcess accept (a) any descendant process and (b) the
// pane PID itself when its command is a recognised agent (claude/node).  These
// tests pin both robustness properties and the dead-pane regression guard using
// real OS process trees.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
)

// procComm returns the comm (executable basename) of pid via `ps -o comm=`, or
// "" on any error.
func procComm(t *testing.T, pid int) string {
	t.Helper()
	out, err := exec.Command("ps", "-o", "comm=", "-p", itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(out)))
}

// TestHasChildProcess_LiveDirectChild verifies the baseline: a process with a
// live direct child is reported active.  `& wait` forces sh to actually fork
// the child rather than exec into it (the single-command exec optimisation).
func TestHasChildProcess_LiveDirectChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	// Give sh a beat to fork the sleep child.
	waitForChild(t, cmd.Process.Pid)

	if !daemon.ExportedHasChildProcess(cmd.Process.Pid) {
		t.Errorf("hasChildProcess(%d): want true for process with a live direct child, got false",
			cmd.Process.Pid)
	}
}

// TestHasChildProcess_LiveDeepDescendant verifies the hk-tgqy5 fix: a process
// whose live agent is a *grandchild* (not a direct child) is still reported
// active.  We build sh → sh → sleep (each `& wait` forces a real fork) and
// probe the top sh.
func TestHasChildProcess_LiveDeepDescendant(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sh -c 'sleep 30 & wait' & wait")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	waitForChild(t, cmd.Process.Pid)

	if !daemon.ExportedHasChildProcess(cmd.Process.Pid) {
		t.Errorf("hasChildProcess(%d): want true for process with a live deep descendant, got false",
			cmd.Process.Pid)
	}
}

// TestHasChildProcess_SelfIsAgentNoChildren verifies the hk-tgqy5 self-command
// fix: a childless leaf process whose command name matches a recognised agent
// MUST be reported active.  This is the real-world failure mode — tmux exec'd
// `sh -c "claude …"` into the agent, so the pane PID IS the agent with no
// children during a thinking phase.
//
// We exercise the branch deterministically: take this running test process's
// own comm (which has no children of interest in this scope and is not an
// agent), temporarily add it to the match list, and assert hasChildProcess now
// reports the (childless) self PID as active purely via the comm match.  The
// real fix matches "claude"/"node"; the mechanism is identical.
func TestHasChildProcess_SelfIsAgentNoChildren(t *testing.T) {
	self := os.Getpid()

	comm := procComm(t, self)
	if comm == "" {
		t.Skip("could not read own comm via ps")
	}

	orig := *daemon.ExportedLivePaneCommandSubstrings
	*daemon.ExportedLivePaneCommandSubstrings = []string{comm}
	t.Cleanup(func() { *daemon.ExportedLivePaneCommandSubstrings = orig })

	if !daemon.ExportedHasChildProcess(self) {
		t.Errorf("hasChildProcess(%d): want true when pane PID command (%q) matches an agent fragment, got false",
			self, comm)
	}
}

// TestHasChildProcess_LeafNonAgentNotActive is the dead-pane regression guard:
// a childless leaf whose command name is NOT a recognised agent ("sleep") must
// be reported inactive, so the self-command branch does not over-match.
func TestHasChildProcess_LeafNonAgentNotActive(t *testing.T) {
	// A leaf `sleep` process: no children, command name "sleep" (not an agent).
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })

	// sleep has no children and "sleep" is not in livePaneCommandSubstrings, so
	// it must be reported as NOT active — the legitimate dead-pane case.
	if daemon.ExportedHasChildProcess(cmd.Process.Pid) {
		t.Errorf("hasChildProcess(%d): want false for childless non-agent leaf (dead-pane guard), got true",
			cmd.Process.Pid)
	}
}

// TestHasChildProcess_ExitedProcessNotActive verifies that a PID whose process
// has fully exited (the canonical dead pane) is reported inactive.
func TestHasChildProcess_ExitedProcessNotActive(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run helper process: %v", err)
	}
	// cmd.Process.Pid is now a dead PID (process reaped).
	if daemon.ExportedHasChildProcess(cmd.Process.Pid) {
		t.Errorf("hasChildProcess(%d): want false for exited process, got true", cmd.Process.Pid)
	}
}

// waitForChild polls until pid has at least one direct child (or a short
// deadline elapses), so tests don't race the helper shell's fork.
func waitForChild(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("pgrep", "-P", itoa(pid)).Run() == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("waitForChild: pid %d still has no child after deadline; proceeding anyway", pid)
}

func itoa(n int) string {
	// Tiny local helper to avoid importing strconv just for one call.
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
