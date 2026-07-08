package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// pidfileLockFixtureWritePidfile writes a three-line pidfile at the canonical
// pidfile path with the given pid, pgid, and "unknown" as the instance ID. The
// file is written WITHOUT acquiring the flock, so ProbePidfileLock's flock
// probe succeeds and proceeds to the kill(pid, 0) step.
func pidfileLockFixtureWritePidfile(t *testing.T, projectDir string, pid, pgid int) {
	t.Helper()

	pidfilePath := plFixturePidfilePath(projectDir)
	content := fmt.Sprintf("%d\n%d\nunknown\n", pid, pgid)
	if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("pidfileLockFixtureWritePidfile: WriteFile: %v", err)
	}
}

// pidfileLockFixtureSpawnAndKill starts a child process using `true` (exits
// immediately with code 0), waits for it to exit, and returns its PID. The
// returned PID is guaranteed dead: kill(pid, 0) will return ESRCH.
//
// exec.CommandContext is used per the noctx lint rule.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — kill(pid, 0) = ESRCH path
// requires a genuinely dead PID.
func pidfileLockFixtureSpawnAndKill(t *testing.T) (int, error) {
	t.Helper()

	//nolint:gosec // G204: "true" is a compile-time constant, not user input
	cmd := exec.CommandContext(t.Context(), "true")
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("pidfileLockFixtureSpawnAndKill: Start: %w", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		// `true` always exits 0; log but do not fail — PID is still recorded.
		t.Logf("pidfileLockFixtureSpawnAndKill: Wait: %v (non-fatal; pid=%d)", err, pid)
	}
	return pid, nil
}

// TestProbePidfileLock_HeldByLiveDaemon verifies that ProbePidfileLock returns
// PidfileLockStatusHeld and ErrPidfileLocked when another fd holds the exclusive
// flock on the pidfile. The returned PID must be zero (lock-contention path
// does not parse the file content).
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "(a) pidfile present, lock
// held by live process → exit 5."
func TestProbePidfileLock_HeldByLiveDaemon(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // os.Getpid() is always valid

	// Simulate a live daemon holding the pidfile lock.
	release, err := plFixtureAcquirePidfile(t, projectDir, pid, pgid, "01950004-0000-7001-8000-000000000001")
	if err != nil {
		t.Fatalf("TestProbePidfileLock_HeldByLiveDaemon: acquire: %v", err)
	}
	t.Cleanup(release)

	status, probedPID, probeErr := ProbePidfileLock(projectDir)

	if status != PidfileLockStatusHeld {
		t.Errorf("TestProbePidfileLock_HeldByLiveDaemon: status = %d, want PidfileLockStatusHeld (%d)", status, PidfileLockStatusHeld)
	}
	if probedPID != 0 {
		t.Errorf("TestProbePidfileLock_HeldByLiveDaemon: pid = %d, want 0 (not parsed on lock-held path)", probedPID)
	}
	if !errors.Is(probeErr, ErrPidfileLocked) {
		t.Errorf("TestProbePidfileLock_HeldByLiveDaemon: errors.Is(err, ErrPidfileLocked) = false; err = %v", probeErr)
	}
}

// TestProbePidfileLock_StaleDeadPID verifies that ProbePidfileLock returns
// PidfileLockStatusStale when the pidfile is present (no flock held) and the
// recorded PID is dead (ESRCH from kill(pid, 0)).
//
// The test spawns a short-lived child, records its PID, and waits for it to
// exit before writing the pidfile — guaranteeing ESRCH on kill(pid, 0).
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "(b) pidfile present, no lock,
// recorded PID not live → stale pidfile per PL-024."
func TestProbePidfileLock_StaleDeadPID(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestProbePidfileLock_StaleDeadPID: skipping on %s (POSIX kill(pid,0) only)", runtime.GOOS)
	}

	projectDir := plFixtureTempProjectDir(t)

	deadPID, err := pidfileLockFixtureSpawnAndKill(t)
	if err != nil {
		t.Fatalf("TestProbePidfileLock_StaleDeadPID: spawn child: %v", err)
	}

	myPGID, _ := syscall.Getpgid(os.Getpid()) //nolint:errcheck // always valid
	pidfileLockFixtureWritePidfile(t, projectDir, deadPID, myPGID)

	status, probedPID, probeErr := ProbePidfileLock(projectDir)

	if status != PidfileLockStatusStale {
		t.Errorf("TestProbePidfileLock_StaleDeadPID: status = %d, want PidfileLockStatusStale (%d)", status, PidfileLockStatusStale)
	}
	if probeErr != nil {
		t.Errorf("TestProbePidfileLock_StaleDeadPID: err = %v, want nil", probeErr)
	}
	if probedPID != deadPID {
		t.Errorf("TestProbePidfileLock_StaleDeadPID: pid = %d, want %d", probedPID, deadPID)
	}
}

// TestProbePidfileLock_NoPidfile verifies that ProbePidfileLock returns an
// error wrapping os.ErrNotExist when the pidfile does not exist. The caller
// uses errors.Is(err, os.ErrNotExist) to distinguish absent-pidfile (normal
// first-start) from stale-pidfile.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — absent pidfile is a normal
// first-start case; ProbePidfileLock surfaces the error for the caller.
func TestProbePidfileLock_NoPidfile(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	// Do NOT write a pidfile.

	_, _, err := ProbePidfileLock(projectDir)
	if err == nil {
		t.Fatal("TestProbePidfileLock_NoPidfile: expected error for absent pidfile, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("TestProbePidfileLock_NoPidfile: errors.Is(err, os.ErrNotExist) = false; err = %v", err)
	}
}

// TestProbePidfileLock_CorruptContentIsStale verifies that ProbePidfileLock
// returns PidfileLockStatusStale when the pidfile exists (no flock held) but
// its content is not a valid PID. An unparseable pidfile is treated as stale
// per PL-024.
//
// Spec ref: process-lifecycle.md §4.8 PL-024 — "Torn/unparseable pidfile →
// stale."
func TestProbePidfileLock_CorruptContentIsStale(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := plFixturePidfilePath(projectDir)

	// Write garbage (no flock held).
	if err := os.WriteFile(pidfilePath, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("TestProbePidfileLock_CorruptContentIsStale: WriteFile: %v", err)
	}

	status, _, probeErr := ProbePidfileLock(projectDir)

	if status != PidfileLockStatusStale {
		t.Errorf("TestProbePidfileLock_CorruptContentIsStale: status = %d, want PidfileLockStatusStale (%d)", status, PidfileLockStatusStale)
	}
	if probeErr != nil {
		t.Errorf("TestProbePidfileLock_CorruptContentIsStale: err = %v, want nil", probeErr)
	}
}

// TestProbePidfileLock_EmptyFileIsStale verifies that ProbePidfileLock returns
// PidfileLockStatusStale for an empty pidfile (no flock held). An empty file
// is treated as stale per PL-024 (torn write).
//
// Spec ref: process-lifecycle.md §4.8 PL-024 — "Torn/unparseable pidfile →
// stale."
func TestProbePidfileLock_EmptyFileIsStale(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := plFixturePidfilePath(projectDir)

	if err := os.WriteFile(pidfilePath, []byte{}, 0o600); err != nil {
		t.Fatalf("TestProbePidfileLock_EmptyFileIsStale: WriteFile: %v", err)
	}

	status, _, probeErr := ProbePidfileLock(projectDir)

	if status != PidfileLockStatusStale {
		t.Errorf("TestProbePidfileLock_EmptyFileIsStale: status = %d, want PidfileLockStatusStale (%d)", status, PidfileLockStatusStale)
	}
	if probeErr != nil {
		t.Errorf("TestProbePidfileLock_EmptyFileIsStale: err = %v, want nil", probeErr)
	}
}

// TestProbePidfileLock_ReleaseThenProbeNotHeld verifies that ProbePidfileLock
// does NOT return PidfileLockStatusHeld after the lock has been released.
// After Release(), the flock is free; the probe's flock step succeeds.
//
// The exact status (Stale or Ambiguous) depends on whether kill(pid, 0) returns
// ESRCH for our own PID (it won't — we're alive) and whether platform-specific
// corroboration is available. We assert only that status != Held and that the
// probe does not return ErrPidfileLocked.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — fd-lifetime semantics; closing
// the fd releases the flock immediately.
func TestProbePidfileLock_ReleaseThenProbeNotHeld(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestProbePidfileLock_ReleaseThenProbeNotHeld: skipping on %s (POSIX flock only)", runtime.GOOS)
	}

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // always valid

	pf, err := AcquirePidfile(projectDir, pid, pgid, "01950004-0000-7001-8000-000000000020")
	if err != nil {
		t.Fatalf("TestProbePidfileLock_ReleaseThenProbeNotHeld: AcquirePidfile: %v", err)
	}
	if err := pf.Release(); err != nil {
		t.Fatalf("TestProbePidfileLock_ReleaseThenProbeNotHeld: Release: %v", err)
	}

	// A concurrent exec.Command fork(2) elsewhere in this parallel test binary
	// can transiently keep the just-released flock alive via an inherited fd
	// copy until that child's exec(2) closes it, making the probe briefly
	// report Held right after Release() — see plFixtureEventuallyTrue. Retry
	// until the probe settles rather than a single fixed sleep.
	var status PidfileLockStatus
	var probedPID int
	var probeErr error
	plFixtureEventuallyTrue(t, 2*time.Second, func() bool {
		status, probedPID, probeErr = ProbePidfileLock(projectDir)
		return status != PidfileLockStatusHeld && !errors.Is(probeErr, ErrPidfileLocked)
	})

	if errors.Is(probeErr, ErrPidfileLocked) {
		t.Errorf("TestProbePidfileLock_ReleaseThenProbeNotHeld: got ErrPidfileLocked after release; flock must be free")
	}
	if status == PidfileLockStatusHeld {
		t.Errorf("TestProbePidfileLock_ReleaseThenProbeNotHeld: status = Held after Release; want Stale or Ambiguous")
	}
	if probedPID != pid {
		t.Errorf("TestProbePidfileLock_ReleaseThenProbeNotHeld: probedPID = %d, want %d (our PID)", probedPID, pid)
	}
}

// TestProbePidfileLock_ErrSentinels verifies the ErrPidfileAmbiguous sentinel
// wraps a non-nil error and that errors.Is works correctly on ErrPidfileLocked
// and ErrPidfileAmbiguous.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — sentinels map to caller
// decisions (exit 5, proceed, refuse).
func TestProbePidfileLock_ErrSentinels(t *testing.T) {
	t.Parallel()

	if !errors.Is(ErrPidfileLocked, ErrPidfileLocked) {
		t.Error("errors.Is(ErrPidfileLocked, ErrPidfileLocked) must be true")
	}
	if !errors.Is(ErrPidfileAmbiguous, ErrPidfileAmbiguous) {
		t.Error("errors.Is(ErrPidfileAmbiguous, ErrPidfileAmbiguous) must be true")
	}
	if errors.Is(ErrPidfileLocked, ErrPidfileAmbiguous) {
		t.Error("ErrPidfileLocked and ErrPidfileAmbiguous must be distinct errors")
	}
	if errors.Is(ErrPidfileAmbiguous, ErrPidfileLocked) {
		t.Error("ErrPidfileAmbiguous and ErrPidfileLocked must be distinct errors")
	}
}
