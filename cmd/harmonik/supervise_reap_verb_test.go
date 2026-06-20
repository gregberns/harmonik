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

	// Dead flywheel: launch a brief sleep so we can set remain-on-exit BEFORE the
	// command exits, then the pane goes pane_dead=1 (mirrors a crashed shim under
	// remain-on-exit). Setting remain-on-exit AFTER the command exits is too late
	// (tmux would close the window instead of keeping a dead pane).
	createOut, err := exec.Command("tmux", "new-session", "-d", "-s", flywheel, "sleep", "1").CombinedOutput()
	if err != nil {
		t.Skipf("tmux new-session (flywheel) failed (may lack a server): %v: %s", err, createOut)
	}
	_ = exec.Command("tmux", "set-option", "-t", flywheel, "remain-on-exit", "on").Run()
	// Wait for the sleep to exit → pane_dead=1.
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
// ~3s. Returns false if it never goes dead.
func waitPaneDead(t *testing.T, session string) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "list-panes", "-t", "="+session, "-F", "#{pane_dead}").CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) == "1" {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
