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
// scenario with remain-on-exit leaving the pane visible). With the hk-li14r
// pre-flight check, the refusal now happens before writing any files.
//
// Bead ref: hk-izs8s (second flywheel start refused, exit 24).
func TestSupervise_StartRefuses_FlywheelSessionExists(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	dir := socketSafeTempDir(t)

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
	// RunStart should detect the existing session via pre-flight has-session check
	// and exit 24 without writing sentinel or config.
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStart([]string{"--project", dir, "--command", "true"}, &out, &errOut)
	if code != supervisecmd.ExitCodeFlywheelSessionExists {
		t.Errorf("expected exit %d (flywheel session exists), got %d; stderr: %s",
			supervisecmd.ExitCodeFlywheelSessionExists, code, errOut.String())
	}
}

// TestSupervise_StartDoesNotCorruptExistingSentinel verifies that when a flywheel
// tmux session already exists (remain-on-exit), RunStart exits 24 WITHOUT removing
// the existing supervisor.sentinel file. This prevents dual-orchestrator collision
// when a shim crashes but the Pi is reparented (--watch-restart path): the
// reparented Pi still needs the sentinel to survive the next daemon orphan sweep
// (PL-006d). The pre-flight tmux has-session check (hk-li14r) closes this gap.
//
// Bead ref: hk-li14r.
func TestSupervise_StartDoesNotCorruptExistingSentinel(t *testing.T) {
	if !tmuxAvailable() {
		t.Skip("tmux not available")
	}

	dir := socketSafeTempDir(t)

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

	// Pre-create the flywheel tmux session (simulates remain-on-exit pane after
	// shim crash with the Pi still alive via reparenting).
	sessionName := supervisecmd.FlywheelSessionName(dir)
	createOut, err := exec.Command("tmux", "new-session", "-d", "-s", sessionName).CombinedOutput()
	if err != nil {
		t.Skipf("tmux new-session failed (may lack a server): %v: %s", err, createOut)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", sessionName).Run()
	})

	// Write a pre-existing sentinel file (as the crashed shim would have left it).
	if err := os.MkdirAll(supervisecmd.CognitionDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := supervisecmd.WriteSentinel(dir); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStart([]string{"--project", dir, "--command", "true"}, &out, &errOut)
	if code != supervisecmd.ExitCodeFlywheelSessionExists {
		t.Errorf("expected exit %d (flywheel session exists), got %d; stderr: %s",
			supervisecmd.ExitCodeFlywheelSessionExists, code, errOut.String())
	}

	// Verify the existing sentinel was NOT removed by RunStart. A reparented Pi
	// relies on this sentinel to survive the next daemon orphan sweep (PL-006d).
	if _, statErr := os.Stat(supervisecmd.SentinelPath(dir)); os.IsNotExist(statErr) {
		t.Error("RunStart removed the existing sentinel — must preserve it when flywheel session pre-exists (hk-li14r)")
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
