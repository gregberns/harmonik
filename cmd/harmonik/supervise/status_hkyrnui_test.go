package supervisecmd

// status_hkyrnui_test.go — tests for the keeper-loop fallback in buildStatus.
//
// pen9 supervisor-up false positive: pidfile-based detector cries 'supervisor
// down' on a live hand-relaunched keeper loop.  buildStatusWithProbe must call
// keeperProbe when the pidfile is absent or stale, and report Running=true /
// PresenceSource="keeper-loop" when the probe returns true.
//
// Bead ref: hk-yrnui.

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildStatus_KeeperLoopFallback_NoPidfile verifies that when supervisor.pid
// is absent and the keeper probe returns true, buildStatus reports running.
func TestBuildStatus_KeeperLoopFallback_NoPidfile(t *testing.T) {
	dir := t.TempDir()
	cognitionDir := filepath.Join(dir, ".harmonik", "cognition")
	if err := os.MkdirAll(cognitionDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No supervisor.pid written — simulates hand-relaunched keeper loop.

	result := buildStatusWithProbe(dir, func(_ string) bool { return true })

	if !result.Running {
		t.Errorf("expected Running=true when keeper probe returns true, got false")
	}
	if result.Status != "running" {
		t.Errorf("expected Status=running, got %q", result.Status)
	}
	if result.PresenceSource != "keeper-loop" {
		t.Errorf("expected PresenceSource=keeper-loop, got %q", result.PresenceSource)
	}
}

// TestBuildStatus_KeeperLoopFallback_DeadPid verifies that when supervisor.pid
// holds a dead PID and the keeper probe returns true, buildStatus reports running.
func TestBuildStatus_KeeperLoopFallback_DeadPid(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "cognition"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a guaranteed-dead PID.
	deadPID := spawnAndWait(t)
	if err := WritePidfile(dir, deadPID); err != nil {
		t.Fatalf("WritePidfile: %v", err)
	}

	result := buildStatusWithProbe(dir, func(_ string) bool { return true })

	if !result.Running {
		t.Errorf("expected Running=true when keeper probe returns true after dead PID, got false")
	}
	if result.PresenceSource != "keeper-loop" {
		t.Errorf("expected PresenceSource=keeper-loop, got %q", result.PresenceSource)
	}
}

// TestBuildStatus_KeeperLoopFallback_NoLoop verifies that when supervisor.pid
// is absent and the keeper probe returns false, buildStatus reports stopped.
func TestBuildStatus_KeeperLoopFallback_NoLoop(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "cognition"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No pidfile; keeper probe returns false.

	result := buildStatusWithProbe(dir, func(_ string) bool { return false })

	if result.Running {
		t.Errorf("expected Running=false when keeper probe returns false, got true")
	}
	if result.Status != "stopped" {
		t.Errorf("expected Status=stopped, got %q", result.Status)
	}
	if result.PresenceSource != "" {
		t.Errorf("expected PresenceSource empty when stopped, got %q", result.PresenceSource)
	}
}

// TestBuildStatus_PidfileAlive verifies the normal path: live PID in pidfile →
// Running=true, PresenceSource=pidfile (keeper probe is not consulted).
func TestBuildStatus_PidfileAlive(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "cognition"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write our own PID — guaranteed alive.
	if err := WritePidfile(dir, os.Getpid()); err != nil {
		t.Fatalf("WritePidfile: %v", err)
	}

	probeCallCount := 0
	result := buildStatusWithProbe(dir, func(_ string) bool {
		probeCallCount++
		return false
	})

	if !result.Running {
		t.Errorf("expected Running=true for live PID, got false")
	}
	if result.PresenceSource != "pidfile" {
		t.Errorf("expected PresenceSource=pidfile, got %q", result.PresenceSource)
	}
	if probeCallCount != 0 {
		t.Errorf("keeper probe should not be called when pidfile is live, got %d calls", probeCallCount)
	}
}

// spawnAndWait starts a short-lived child and waits for it to exit, returning
// its PID which is now guaranteed dead (mirrors mustStartAndWait in watchdog tests).
func spawnAndWait(t *testing.T) int {
	t.Helper()
	proc, err := os.StartProcess("/bin/sh", []string{"/bin/sh", "-c", "exit 0"}, &os.ProcAttr{})
	if err != nil {
		t.Fatalf("spawnAndWait: start: %v", err)
	}
	pid := proc.Pid
	if _, err := proc.Wait(); err != nil {
		t.Logf("spawnAndWait: wait: %v", err)
	}
	return pid
}
