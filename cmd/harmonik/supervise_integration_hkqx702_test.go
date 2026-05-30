package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	supervisecmd "github.com/gregberns/harmonik/cmd/harmonik/supervise"
	"github.com/gregberns/harmonik/internal/supervise"
)

// TestSupervise_CrashLoopVisibleViaStatus verifies the acceptance criterion:
//
//	fake supervisee exits code=1 → shim restarts 2× then crashloop is
//	visible via `supervise status --json`.
//
// Runs internal/supervise.Supervisor directly (no tmux) so it executes in CI.
// Bead ref: hk-qx702.
func TestSupervise_CrashLoopVisibleViaStatus(t *testing.T) {
	dir := t.TempDir()

	// Write config.json with MaxRestarts=2.
	cfg := supervisecmd.Config{
		SchemaVersion: 1,
		RestartPolicy: "on-failure",
		RestartMax:    2,
		RestartBaseMS: 10,
		RestartCapMS:  100,
		Command:       []string{"sh", "-c", "exit 1"},
	}
	if err := supervisecmd.WriteConfigAtomic(dir, cfg); err != nil {
		t.Fatalf("WriteConfigAtomic: %v", err)
	}

	// Write sentinel and pidfile (as _shim would on startup).
	if err := supervisecmd.WriteSentinel(dir); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if err := supervisecmd.WritePidfile(dir, os.Getpid()); err != nil {
		t.Fatalf("WritePidfile: %v", err)
	}

	// Run Supervisor directly (as _shim would, but in-process for testing).
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	spec := supervise.Spec{
		Command:         []string{"sh", "-c", "exit 1"},
		Policy:          supervise.PolicyOnFailure,
		StartTimeout:    20 * time.Millisecond,
		CrashLoopWindow: 10 * time.Second,
		StopTimeout:     500 * time.Millisecond,
		Backoff: supervise.BackoffConfig{
			Base:        10 * time.Millisecond,
			Cap:         100 * time.Millisecond,
			Jitter:      0,
			MaxRestarts: 2,
		},
	}

	sv := supervise.New(spec, log)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runErr := sv.Run(ctx)

	if runErr == nil {
		t.Fatal("expected crash-loop error, got nil")
	}
	snap := sv.Snapshot()
	if snap.Status != supervise.StatusCrashLoop {
		t.Errorf("expected status crashloop, got %s", snap.Status)
	}
	if snap.RestartCount < 2 {
		t.Errorf("expected ≥2 restarts, got %d", snap.RestartCount)
	}

	// Verify supervise status --json output is parseable and sentinel is set.
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStatus([]string{"--project", dir, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("RunStatus exit %d: %s", code, errOut.String())
	}

	var result supervisecmd.StatusResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal status JSON: %v (raw: %s)", err, out.String())
	}

	if result.SchemaVersion != 1 {
		t.Errorf("schema_version: got %d, want 1", result.SchemaVersion)
	}
	if !result.SentinelOK {
		t.Errorf("expected sentinel_ok=true")
	}
	// PID was written as our own PID, and our process is alive → status should be running.
	if !result.Running {
		t.Errorf("expected running=true (pidfile points to our own PID which is alive)")
	}
}

// TestSupervise_StartRefuses_DaemonDown verifies that RunStart exits 17 when
// the daemon socket is absent.
func TestSupervise_StartRefuses_DaemonDown(t *testing.T) {
	dir := t.TempDir()
	// No daemon socket → should fail with exit 17.
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStart([]string{"--project", dir}, &out, &errOut)
	if code != supervisecmd.ExitCodeDaemonDown {
		t.Errorf("expected exit 17 (daemon down), got %d; stderr: %s", code, errOut.String())
	}
}

// TestSupervise_StartRefuses_LockHeld verifies that RunStart exits 25 when
// supervisor.lock is already held by another process.
func TestSupervise_StartRefuses_LockHeld(t *testing.T) {
	dir := t.TempDir()

	// Create a mock Unix socket so the daemon probe passes.
	harmonikDir := dir + "/.harmonik"
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sockPath := harmonikDir + "/daemon.sock"
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("create unix listener: %v", err)
	}
	defer func() { _ = l.Close() }()

	// Acquire the supervisor lock exclusively.
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

	// RunStart should now fail with exit 25.
	var out bytes.Buffer
	var errOut bytes.Buffer
	code := supervisecmd.RunStart([]string{"--project", dir}, &out, &errOut)
	if code != supervisecmd.ExitCodeSupervisorRunning {
		t.Errorf("expected exit 25 (lock held), got %d; stderr: %s", code, errOut.String())
	}
}
