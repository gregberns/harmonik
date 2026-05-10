package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestPL024_RemoveStalePidfile_Removes verifies that RemoveStalePidfile
// deletes the pidfile from disk and returns nil.
//
// Spec ref: process-lifecycle.md §4.8 PL-024 — "The next harmonik daemon
// invocation MUST detect a stale pidfile … remove the stale pidfile, and
// proceed with startup per §PL-005."
func TestPL024_RemoveStalePidfile_Removes(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")

	// Write a synthetic stale pidfile (no flock held — simulating crash).
	if err := os.WriteFile(pidfilePath, []byte("99999\n99999\nunknown\n"), 0o600); err != nil {
		t.Fatalf("PL-024 remove: WriteFile: %v", err)
	}

	// Pre-condition: file exists.
	if _, err := os.Stat(pidfilePath); os.IsNotExist(err) {
		t.Fatal("PL-024 remove: pidfile absent before RemoveStalePidfile")
	}

	if err := RemoveStalePidfile(projectDir); err != nil {
		t.Fatalf("PL-024 remove: RemoveStalePidfile: %v", err)
	}

	// Post-condition: file is gone.
	if _, err := os.Stat(pidfilePath); !os.IsNotExist(err) {
		t.Error("PL-024 remove: pidfile still exists after RemoveStalePidfile; want removed")
	}
}

// TestPL024_RemoveStalePidfile_Absent verifies that RemoveStalePidfile returns
// an error wrapping os.ErrNotExist when the pidfile does not exist.
//
// Spec ref: process-lifecycle.md §4.8 PL-024.
func TestPL024_RemoveStalePidfile_Absent(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	// No pidfile has been written — RemoveStalePidfile must return a not-exist error.
	err := RemoveStalePidfile(projectDir)
	if err == nil {
		t.Fatal("PL-024 remove absent: expected error for absent pidfile, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("PL-024 remove absent: error = %v; want errors.Is(err, os.ErrNotExist)", err)
	}
}

// TestPL024_StalePidfileDetectionAndRemoval verifies the full PL-024 detection +
// removal cycle: ProbePidfileLock returns Stale, then RemoveStalePidfile removes
// the file so a subsequent AcquirePidfile can succeed.
//
// This test exercises the three-step startup recovery path:
//  1. Detect stale via ProbePidfileLock (StatusStale + dead creator PID).
//  2. Remove stale via RemoveStalePidfile.
//  3. Acquire fresh pidfile via AcquirePidfile (should succeed).
//
// Spec ref: process-lifecycle.md §4.8 PL-024 — "detect … remove … proceed with
// startup per §PL-005."
// Spec ref: process-lifecycle.md §4.1 PL-002a — "probe with kill(pid, 0)."
func TestPL024_StalePidfileDetectionAndRemoval(t *testing.T) {
	t.Parallel()

	const deadPID = 99989
	if plFixtureIsPidLive(deadPID) {
		t.Skipf("PL-024 full-cycle: PID %d is live on this host; skipping", deadPID)
	}

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")

	// Write a stale pidfile with a known-dead PID (no flock held — kernel
	// releases the flock on crash).
	staleContent := []byte("99989\n1\nunknown\n")
	if err := os.WriteFile(pidfilePath, staleContent, 0o600); err != nil {
		t.Fatalf("PL-024 full-cycle: WriteFile: %v", err)
	}

	// Step 1: detect via ProbePidfileLock.
	status, probedPID, probeErr := ProbePidfileLock(projectDir)
	if status != PidfileLockStatusStale {
		t.Fatalf("PL-024 full-cycle: ProbePidfileLock status = %d, want PidfileLockStatusStale (%d); err = %v",
			status, PidfileLockStatusStale, probeErr)
	}
	if probedPID != deadPID {
		t.Errorf("PL-024 full-cycle: probedPID = %d, want %d", probedPID, deadPID)
	}

	// Step 2: remove the stale pidfile.
	if err := RemoveStalePidfile(projectDir); err != nil {
		t.Fatalf("PL-024 full-cycle: RemoveStalePidfile: %v", err)
	}

	// Verify removal.
	if _, err := os.Stat(pidfilePath); !os.IsNotExist(err) {
		t.Error("PL-024 full-cycle: pidfile still on disk after removal")
	}

	// Step 3: acquire a fresh pidfile (simulates daemon restart after recovery).
	myPID := os.Getpid()
	myPGID, _ := syscall.Getpgid(myPID) //nolint:errcheck // os.Getpid() is always valid
	pf, err := AcquirePidfile(projectDir, myPID, myPGID, "01950000-0000-7000-8000-000000000099")
	if err != nil {
		t.Fatalf("PL-024 full-cycle: AcquirePidfile after stale removal: %v", err)
	}
	t.Cleanup(func() { _ = pf.Release() }) //nolint:errcheck // cleanup error unactionable

	// New pidfile must exist and be readable.
	pid, _, instanceID, err := ReadPidfile(projectDir)
	if err != nil {
		t.Fatalf("PL-024 full-cycle: ReadPidfile after acquire: %v", err)
	}
	if pid != myPID {
		t.Errorf("PL-024 full-cycle: ReadPidfile PID = %d, want %d", pid, myPID)
	}
	if instanceID != "01950000-0000-7000-8000-000000000099" {
		t.Errorf("PL-024 full-cycle: ReadPidfile instanceID = %q, want %q",
			instanceID, "01950000-0000-7000-8000-000000000099")
	}
}

// TestPL024_TornPidfileIsTreatedAsStale verifies that a torn (unparseable)
// pidfile — as produced by an incomplete write at crash time — is treated as
// stale by ProbePidfileLock and is removable by RemoveStalePidfile.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "A torn or unparseable pidfile
// observed by a subsequent daemon startup MUST be treated as stale per PL-024."
func TestPL024_TornPidfileIsTreatedAsStale(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := filepath.Join(projectDir, ".harmonik", "daemon.pid")

	// Write a torn pidfile (truncated mid-write — empty, unparseable).
	if err := os.WriteFile(pidfilePath, []byte(""), 0o600); err != nil {
		t.Fatalf("PL-024 torn: WriteFile: %v", err)
	}

	status, _, _ := ProbePidfileLock(projectDir)
	if status != PidfileLockStatusStale {
		t.Errorf("PL-024 torn: ProbePidfileLock status = %d, want PidfileLockStatusStale (%d)",
			status, PidfileLockStatusStale)
	}

	// Must be removable.
	if err := RemoveStalePidfile(projectDir); err != nil {
		t.Fatalf("PL-024 torn: RemoveStalePidfile: %v", err)
	}
	if _, err := os.Stat(pidfilePath); !os.IsNotExist(err) {
		t.Error("PL-024 torn: torn pidfile still exists after removal")
	}
}
