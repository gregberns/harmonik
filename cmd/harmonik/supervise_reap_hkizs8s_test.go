package main

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"syscall"
	"testing"

	supervisecmd "github.com/gregberns/harmonik/cmd/harmonik/supervise"
)

// tmuxAvailable returns true when tmux is on PATH and usable.
func tmuxAvailable() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// TestSupervise_StopReapsFlywheelSession verifies that RunStop kills the flywheel
// tmux session (child-tree reap), not just the supervisor process.
//
// Bead ref: hk-izs8s (reap — supervise stop reaps child tree).
func TestSupervise_StopReapsFlywheelSession(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	dir := t.TempDir()

	// Create the flywheel tmux session as start would.
	sessionName := supervisecmd.FlywheelSessionName(dir)
	createOut, err := exec.Command("tmux", "new-session", "-d", "-s", sessionName).CombinedOutput()
	if err != nil {
		t.Skipf("tmux new-session failed (may lack a server): %v: %s", err, createOut)
	}
	t.Cleanup(func() {
		// Best-effort: kill session if test didn't clean it up.
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	// Start a real background process as the fake supervisor so RunStop has a
	// live PID to SIGTERM without killing the test process itself.
	fakeSupervisor := exec.Command("sleep", "300")
	if err := fakeSupervisor.Start(); err != nil {
		t.Fatalf("start fake supervisor: %v", err)
	}
	t.Cleanup(func() { _ = fakeSupervisor.Process.Kill(); _ = fakeSupervisor.Wait() })

	if err := os.MkdirAll(supervisecmd.CognitionDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := supervisecmd.WritePidfile(dir, fakeSupervisor.Process.Pid); err != nil {
		t.Fatalf("WritePidfile: %v", err)
	}
	if err := supervisecmd.WriteSentinel(dir); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStop([]string{"--project", dir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("RunStop exit %d: %s", code, errOut.String())
	}

	// Verify the tmux session is gone.
	checkOut, _ := exec.Command("tmux", "has-session", "-t", sessionName).CombinedOutput()
	_ = checkOut
	if err := exec.Command("tmux", "has-session", "-t", sessionName).Run(); err == nil {
		t.Errorf("tmux session %q still exists after RunStop — expected it to be reaped", sessionName)
	}
}

// TestSupervise_StartRefuses_FlywheelSessionExists verifies that RunStart exits 24
// when the flywheel tmux session already exists but the lock is free (shim-crash
// scenario with remain-on-exit leaving the pane visible).
//
// Bead ref: hk-izs8s (second flywheel start refused, exit 24).
func TestSupervise_StartRefuses_FlywheelSessionExists(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	dir := t.TempDir()

	// Create a mock Unix socket so the daemon probe passes.
	harmonikDir := dir + "/.harmonik"
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", harmonikDir+"/daemon.sock")
	if err != nil {
		t.Fatalf("create unix listener: %v", err)
	}
	defer func() { _ = l.Close() }()

	// Pre-create the flywheel session (simulates remain-on-exit pane after shim crash).
	sessionName := supervisecmd.FlywheelSessionName(dir)
	createOut, err := exec.Command("tmux", "new-session", "-d", "-s", sessionName).CombinedOutput()
	if err != nil {
		t.Skipf("tmux new-session failed (may lack a server): %v: %s", err, createOut)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	// Lock must NOT be held (shim crashed and released it).
	// RunStart should acquire lock, write sentinel+config, try new-session, get
	// "duplicate session", and exit 24.
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStart([]string{"--project", dir, "--command", "true"}, &out, &errOut)
	if code != supervisecmd.ExitCodeFlywheelSessionExists {
		t.Errorf("expected exit %d (flywheel session exists), got %d; stderr: %s",
			supervisecmd.ExitCodeFlywheelSessionExists, code, errOut.String())
	}
}

// TestSupervise_StartRefuses_FlywheelSessionExists_LockAlreadyHeld confirms that
// when the lock IS held (normal running supervisor), the exit is still 25 (lock
// held) and not 24, preserving the existing behaviour.
func TestSupervise_StartRefuses_FlywheelSessionExists_LockAlreadyHeld(t *testing.T) {
	// Use a short path so the Unix socket path stays within the 104-byte limit.
	dir, err := os.MkdirTemp("", "hk-reap-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	harmonikDir := dir + "/.harmonik"
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", harmonikDir+"/daemon.sock")
	if err != nil {
		t.Fatalf("create unix listener: %v", err)
	}
	defer func() { _ = l.Close() }()

	if err := os.MkdirAll(supervisecmd.CognitionDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	lockFd, err := os.OpenFile(supervisecmd.LockPath(dir), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatalf("open lock: %v", err)
	}
	defer func() { _ = lockFd.Close() }()
	if err := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("flock: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStart([]string{"--project", dir, "--command", "true"}, &out, &errOut)
	if code != supervisecmd.ExitCodeSupervisorRunning {
		t.Errorf("expected exit 25 (lock held), got %d; stderr: %s", code, errOut.String())
	}
}
