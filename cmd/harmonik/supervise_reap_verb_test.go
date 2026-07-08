package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	supervisecmd "github.com/gregberns/harmonik/cmd/harmonik/supervise"
)

// uniqueFlywheelHash mints a fresh 12-hex hash so the test's synthetic flywheel
// session name can NEVER collide with a real harmonik-<realhash>-flywheel.
func uniqueFlywheelHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// TestSuperviseReapVerb_KillsDeadFlywheel_LeavesDefault drives the CLI verb end
// to end against a real tmux server: it creates a synthetic DEAD flywheel
// session (remain-on-exit, command exits → pane_dead=1) and a LIVE -default
// session sharing the same hash, then runs `supervise reap`. The dead flywheel
// must be killed and a tmux_orphan_reaped event emitted; the -default session
// must survive untouched (CONTRACT.md invariant I3).
func TestSuperviseReapVerb_KillsDeadFlywheel_LeavesDefault(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	hash := uniqueFlywheelHash(t)
	flywheel := "harmonik-" + hash + "-flywheel"
	def := "harmonik-" + hash + "-default"

	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", "="+flywheel).Run()
		_ = exec.Command("tmux", "kill-session", "-t", "="+def).Run()
	})

	// Dead flywheel: launch a LONG-lived command so the child cannot exit before
	// we set remain-on-exit and force the kill — this removes the spawn race that
	// a `sleep 1` fixture has on a saturated runner. Sequence: spawn `sleep 300`,
	// turn on remain-on-exit while the child is still alive, THEN deterministically
	// kill the pane's child PID so the pane transitions to pane_dead=1 (mirrors a
	// crashed shim under remain-on-exit).
	createOut, err := exec.Command("tmux", "new-session", "-d", "-s", flywheel, "sleep", "300").CombinedOutput()
	if err != nil {
		t.Skipf("tmux new-session (flywheel) failed (may lack a server): %v: %s", err, createOut)
	}
	_ = exec.Command("tmux", "set-option", "-t", flywheel, "remain-on-exit", "on").Run()
	// Force the child dead deterministically: read the pane's child PID and SIGKILL
	// it. With remain-on-exit on, the pane stays as pane_dead=1 rather than closing.
	pidOut, err := exec.Command("tmux", "list-panes", "-t", "="+flywheel, "-F", "#{pane_pid}").CombinedOutput()
	if err != nil {
		t.Skipf("could not read flywheel pane_pid: %v: %s", err, pidOut)
	}
	if pid := strings.TrimSpace(string(pidOut)); pid != "" {
		_ = exec.Command("kill", "-KILL", pid).Run()
	}
	// Wait for the child to die → pane_dead=1.
	if !waitPaneDead(t, flywheel) {
		t.Skip("could not get flywheel pane into pane_dead=1 (tmux remain-on-exit unsupported?)")
	}

	// Live -default: long-lived pane.
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", def, "sleep", "300").CombinedOutput(); err != nil {
		t.Skipf("tmux new-session (default) failed: %v: %s", err, out)
	}

	// Project dir with a daemon pidfile whose mtime is LATER than the flywheel's
	// creation, so the predate gate marks the flywheel as an orphan.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Sleep a touch so the pidfile mtime is strictly after session_created.
	time.Sleep(1100 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, ".harmonik", "daemon.pid"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Gate on the reaper's OWN pane_dead view (list-sessions), not just
	// list-panes: under load the two lag out of sync and the reap would
	// (correctly) skip a pane list-sessions still reports live, false-failing the
	// assertion below. If list-sessions never surfaces the dead pane, this is a
	// tmux env limitation — skip, consistent with the tmux-availability guards.
	if !waitPaneDeadViaListSessions(t, flywheel) {
		t.Skip("list-sessions never surfaced pane_dead for the flywheel (tmux version/env limitation)")
	}

	var out, errOut bytes.Buffer
	code := supervisecmd.RunReap([]string{"--project", dir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("RunReap exit %d: %s", code, errOut.String())
	}

	// The flywheel must be gone.
	if err := exec.Command("tmux", "has-session", "-t", "="+flywheel).Run(); err == nil {
		t.Errorf("flywheel session %q still exists after reap — expected it to be reaped", flywheel)
	}
	// The -default must survive.
	if err := exec.Command("tmux", "has-session", "-t", "="+def).Run(); err != nil {
		t.Errorf("-default session %q was killed by reap — invariant I3 violated", def)
	}
	// A tmux_orphan_reaped event must have been emitted for the flywheel.
	if !strings.Contains(out.String(), "tmux_orphan_reaped") || !strings.Contains(out.String(), flywheel) {
		t.Errorf("expected a tmux_orphan_reaped event for %q in stdout; got:\n%s", flywheel, out.String())
	}
}

// TestSuperviseReapVerb_NoTmuxServer_CleanNoOp verifies that when the synthetic
// project has no flywheel orphans (and even if tmux is absent), the verb exits 0
// with no kills. We assert the no-orphan case by pointing at a fresh project dir
// with no matching sessions: RunReap must exit 0 and report zero reaped.
func TestSuperviseReapVerb_NoOrphans_CleanNoOp(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := supervisecmd.RunReap([]string{"--project", dir, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("RunReap exit %d (expected clean no-op): %s", code, errOut.String())
	}
	// JSON summary should report zero reaped.
	if !strings.Contains(out.String(), `"reaped":[]`) {
		t.Errorf("expected empty reaped list in JSON summary; got: %s", out.String())
	}
}

// waitPaneDead polls until the session's active pane reports pane_dead=1, up to
// ~10s. Returns false if it never goes dead.
func waitPaneDead(t *testing.T, session string) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "list-panes", "-t", "="+session, "-F", "#{pane_dead}").CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// waitPaneDeadViaListSessions polls the SAME view the reaper uses — `tmux
// list-sessions -F #{pane_dead}` — until the target session reports its pane
// dead, up to ~10s. The reaper (reap_osadapter.go) reads pane_dead from
// list-sessions, NOT list-panes; under a saturated runner the two views lag out
// of sync (list-panes flips the pane dead before list-sessions surfaces it for
// the session's active pane). Gating the reap assertion on this view removes
// that false-fail. Returns false if list-sessions never reports the pane dead —
// a tmux version/env that doesn't resolve pane_dead in session context, which
// the caller treats as a skip, not a reap-logic failure.
func waitPaneDeadViaListSessions(t *testing.T, session string) bool {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}\x1f#{pane_dead}").CombinedOutput()
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				f := strings.Split(strings.TrimRight(line, "\r"), "\x1f")
				if len(f) == 2 && strings.TrimSpace(f[0]) == session && strings.TrimSpace(f[1]) == "1" {
					return true
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
