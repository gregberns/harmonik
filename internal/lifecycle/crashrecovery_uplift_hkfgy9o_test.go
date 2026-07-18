package lifecycle

// crashrecovery_uplift_hkfgy9o_test.go — crash-recovery unit-test uplift.
//
// Step (a) of hk-fgy9o (test uplift epic: lifecycle subsystem).
//
// Coverage targets:
//   - reconLockReadCreatorPID: missing line, malformed PID, well-formed line
//   - reconLockIsStale: live-PID path (not stale), malformed-file path (stale)
//   - SweepQueueArchives: resolveKeepCount with invalid env-var value
//   - ProbePidfileLock: AmbiguousStatus path via own-PID + no-lock simulation
//   - AcquirePidfile: missing .harmonik parent directory returns wrapped error
//
// Spec refs: process-lifecycle.md §4.1 PL-002, PL-002a, PL-002b, PL-024;
//            §4.2 PL-006 (reconciliation lock sweep).
//
// Bead: hk-fgy9o step a.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// reconLockReadCreatorPID direct unit tests
// ──────────────────────────────────────────────────────────────────────────────

// reconLockUpliftWriteFile writes content to path, failing the test on error.
func reconLockUpliftWriteFile(t *testing.T, path, content string) {
	t.Helper()
	//nolint:gosec // G306: mode 0600 matches reconciliation-lock convention; path from t.TempDir()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("reconLockUpliftWriteFile: write %q: %v", path, err)
	}
}

// reconLockUpliftOpenForRead opens path O_RDONLY and returns the *os.File.
// Registers t.Cleanup to close the file.
func reconLockUpliftOpenForRead(t *testing.T, path string) *os.File {
	t.Helper()
	//nolint:gosec // G304: path derived from t.TempDir(), not user input
	f, err := os.OpenFile(path, os.O_RDONLY, 0o600)
	if err != nil {
		t.Fatalf("reconLockUpliftOpenForRead: open %q: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() }) //nolint:errcheck // cleanup error unactionable
	return f
}

// reconLockUpliftFindDeadPID returns a PID that kill(pid, 0) reports as absent.
func reconLockUpliftFindDeadPID(t *testing.T) int {
	t.Helper()
	for pid := os.Getpid() + 100_000; pid < os.Getpid()+101_000; pid++ {
		if !plFixtureIsPidLive(pid) {
			return pid
		}
	}
	t.Fatal("could not find a non-existent PID for stale-lock test")
	return 0
}

// reconLockUpliftStartExitedChild starts a child that exits immediately and
// returns after observing EOF from its stdout pipe. At that point the child has
// exited but cmd.Wait has not been called, so POSIX kernels keep it as a zombie
// until the registered cleanup reaps it.
func reconLockUpliftStartExitedChild(t *testing.T) (*exec.Cmd, int) {
	t.Helper()

	testBin := os.Args[0]
	//nolint:gosec // G204: testBin is the current test binary.
	cmd := exec.CommandContext(t.Context(), testBin, "-test.run=^TestReconLockZombieChildStub$")
	cmd.Env = append(os.Environ(), "GO_RECON_LOCK_ZOMBIE_CHILD=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("reconLock zombie child: StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("reconLock zombie child: Start: %v", err)
	}
	childPID := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Wait() //nolint:errcheck // reap the intentionally delayed child
	})

	if _, err := io.ReadAll(stdout); err != nil {
		t.Fatalf("reconLock zombie child: read stdout: %v", err)
	}
	if !plFixtureIsPidLive(childPID) {
		t.Fatalf("reconLock zombie child: PID %d was not visible before Wait", childPID)
	}
	return cmd, childPID
}

// TestReconLockReadCreatorPID_WellFormed verifies that reconLockReadCreatorPID
// extracts the creator_pid integer from a well-formed reconciliation lock file.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "creator_pid=<integer>"
// lock-file line format.
func TestReconLockReadCreatorPID_WellFormed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-001.lock")
	reconLockUpliftWriteFile(t, lockPath,
		"run_id=run-001\ncreator_pid=12345\nstarted_at=2026-05-20T00:00:00Z\n")

	f := reconLockUpliftOpenForRead(t, lockPath)
	pid, err := reconLockReadCreatorPID(f)
	if err != nil {
		t.Fatalf("reconLockReadCreatorPID well-formed: unexpected error: %v", err)
	}
	if pid != 12345 {
		t.Errorf("reconLockReadCreatorPID well-formed: pid = %d, want 12345", pid)
	}
}

// TestReconLockReadCreatorPID_MissingLine verifies that reconLockReadCreatorPID
// returns an error when no "creator_pid=" line is present in the file.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — lock file must carry
// creator_pid; missing line → parse error → treated as stale by reconLockIsStale.
func TestReconLockReadCreatorPID_MissingLine(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-missing.lock")
	// File has other content but no creator_pid line.
	reconLockUpliftWriteFile(t, lockPath, "run_id=run-missing\nstarted_at=2026-05-20T00:00:00Z\n")

	f := reconLockUpliftOpenForRead(t, lockPath)
	_, err := reconLockReadCreatorPID(f)
	if err == nil {
		t.Fatal("reconLockReadCreatorPID missing-line: expected error for absent creator_pid line, got nil")
	}
	if !strings.Contains(err.Error(), "creator_pid") {
		t.Errorf("reconLockReadCreatorPID missing-line: error %q does not mention creator_pid", err.Error())
	}
}

// TestReconLockReadCreatorPID_EmptyFile verifies that reconLockReadCreatorPID
// returns an error for an empty file (no lines at all).
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — empty lock file has no
// creator_pid; treated as stale per PL-024 discipline.
func TestReconLockReadCreatorPID_EmptyFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-empty.lock")
	reconLockUpliftWriteFile(t, lockPath, "")

	f := reconLockUpliftOpenForRead(t, lockPath)
	_, err := reconLockReadCreatorPID(f)
	if err == nil {
		t.Fatal("reconLockReadCreatorPID empty-file: expected error, got nil")
	}
}

// TestReconLockReadCreatorPID_MalformedPID verifies that reconLockReadCreatorPID
// returns an error when the creator_pid value is not a valid integer.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — malformed PID → parse error
// → reconLockIsStale treats as stale (removes file).
func TestReconLockReadCreatorPID_MalformedPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-malformed.lock")
	reconLockUpliftWriteFile(t, lockPath, "creator_pid=not-an-int\n")

	f := reconLockUpliftOpenForRead(t, lockPath)
	_, err := reconLockReadCreatorPID(f)
	if err == nil {
		t.Fatal("reconLockReadCreatorPID malformed-pid: expected parse error, got nil")
	}
}

// TestReconLockReadCreatorPID_FirstLineIsPID verifies that reconLockReadCreatorPID
// correctly finds the creator_pid line even when it is the only content.
func TestReconLockReadCreatorPID_FirstLineIsPID(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-first.lock")
	reconLockUpliftWriteFile(t, lockPath, "creator_pid=99777\n")

	f := reconLockUpliftOpenForRead(t, lockPath)
	pid, err := reconLockReadCreatorPID(f)
	if err != nil {
		t.Fatalf("reconLockReadCreatorPID first-line: unexpected error: %v", err)
	}
	if pid != 99777 {
		t.Errorf("reconLockReadCreatorPID first-line: pid = %d, want 99777", pid)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// reconLockProbeStale direct unit tests
// ──────────────────────────────────────────────────────────────────────────────

// TestReconLockProbeStale_LivePIDNotStale verifies that reconLockProbeStale
// returns (nil, false, nil) when the recorded creator_pid is live (our own PID)
// and the flock is acquirable. A live PID means the lock is NOT stale — the
// creator might still be cleaning up.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Stale lock files (acquirable +
// the recorded creator-PID does NOT respond to kill(pid, 0)) MUST be removed."
// A live PID means the second condition is false → not stale.
func TestReconLockProbeStale_LivePIDNotStale(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-live.lock")
	// Use our own PID — guaranteed to be alive.
	ownPID := os.Getpid()
	reconLockUpliftWriteFile(t, lockPath, fmt.Sprintf("creator_pid=%d\n", ownPID))

	held, stale, err := reconLockProbeStale(lockPath)
	if err != nil {
		t.Fatalf("reconLockProbeStale live-PID: unexpected error: %v", err)
	}
	if stale {
		t.Errorf("reconLockProbeStale live-PID: got stale=true for live creator PID %d; want false", ownPID)
	}
	if held != nil {
		t.Error("reconLockProbeStale live-PID: got non-nil held file for non-stale lock")
	}
}

// TestReconLockProbeStale_DeadPIDIsStaleAndHoldsFlock verifies that
// reconLockProbeStale returns stale=true for a dead creator PID AND keeps the
// flock held on the returned file (RC-002a regression: the sweep must hold the
// lock across the unlink so another daemon cannot acquire the lock — and start
// a live reconciliation — between the staleness verdict and the unlink).
//
// Spec ref: process-lifecycle.md §4.2 PL-006; specs/reconciliation/spec.md
// §4.1 RC-002a — the flock probe is the serialization point.
func TestReconLockProbeStale_DeadPIDIsStaleAndHoldsFlock(t *testing.T) {
	t.Parallel()

	deadPID := reconLockUpliftFindDeadPID(t)

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-dead.lock")
	reconLockUpliftWriteFile(t, lockPath, fmt.Sprintf("creator_pid=%d\n", deadPID))

	held, stale, err := reconLockProbeStale(lockPath)
	if err != nil {
		t.Fatalf("reconLockProbeStale dead-PID: unexpected error: %v", err)
	}
	if !stale {
		t.Fatalf("reconLockProbeStale dead-PID: got stale=false for dead PID %d; want true", deadPID)
	}
	if held == nil {
		t.Fatal("reconLockProbeStale dead-PID: stale=true but held file is nil")
	}
	defer func() { _ = held.Close() }()

	// The flock must STILL be held: a competing acquirer (fresh fd on the same
	// path, LOCK_EX|LOCK_NB) must observe EWOULDBLOCK. flock locks are held per
	// open file description, so a second open in this process conflicts exactly
	// like a second daemon would.
	competitor, openErr := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if openErr != nil {
		t.Fatalf("open competitor fd: %v", openErr)
	}
	defer func() { _ = competitor.Close() }()
	flockErr := syscall.Flock(int(competitor.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if flockErr == nil {
		t.Error("RC-002a regression: competing flock succeeded while probe result outstanding — flock was released before unlink")
	}
}

// TestReconLockProbeStale_ZombiePIDNotStale verifies the current PL-024
// liveness contract for defunct children: a zombie remains visible to
// kill(pid, 0), so reconLockProbeStale must not classify the lock as stale
// until the child is reaped and disappears from the process table.
func TestReconLockProbeStale_ZombiePIDNotStale(t *testing.T) {
	t.Parallel()

	_, zombiePID := reconLockUpliftStartExitedChild(t)

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-zombie.lock")
	reconLockUpliftWriteFile(t, lockPath, fmt.Sprintf("creator_pid=%d\n", zombiePID))

	held, stale, err := reconLockProbeStale(lockPath)
	if err != nil {
		t.Fatalf("reconLockProbeStale zombie-PID: unexpected error: %v", err)
	}
	if stale || held != nil {
		t.Errorf("reconLockProbeStale zombie-PID: got stale=%v held=%v for defunct but in-table PID %d; want false/nil", stale, held != nil, zombiePID)
	}
}

// TestReconLockProbeStale_MalformedPIDIsSkipped verifies that when the lock
// file's creator_pid line cannot be parsed, reconLockProbeStale returns an
// error (skip) rather than declaring the file stale: an unparseable creator
// PID cannot be confirmed dead, and PL-006's staleness definition requires
// "the recorded creator-PID does NOT respond to kill(pid, 0)" — which cannot
// be established without a PID.
func TestReconLockProbeStale_MalformedPIDIsSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-malformed-pid.lock")
	reconLockUpliftWriteFile(t, lockPath, "creator_pid=not-a-number\n")

	held, stale, err := reconLockProbeStale(lockPath)
	if err == nil {
		t.Error("reconLockProbeStale malformed-pid: got nil error; unparseable PID must error (skip, not remove)")
	}
	if stale || held != nil {
		t.Errorf("reconLockProbeStale malformed-pid: got stale=%v held=%v; want false/nil", stale, held != nil)
	}
}

// TestReconLockProbeStale_MissingCreatorPIDLineIsSkipped verifies that a lock
// file with no creator_pid line at all errors (skip) rather than being
// declared stale and removed.
func TestReconLockProbeStale_MissingCreatorPIDLineIsSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	lockPath := filepath.Join(dir, "run-no-pid-line.lock")
	reconLockUpliftWriteFile(t, lockPath, "run_id=run-no-pid\nstarted_at=2026-05-20T00:00:00Z\n")

	held, stale, err := reconLockProbeStale(lockPath)
	if err == nil {
		t.Error("reconLockProbeStale missing-creator-pid: got nil error; missing creator_pid must error (skip, not remove)")
	}
	if stale || held != nil {
		t.Errorf("reconLockProbeStale missing-creator-pid: got stale=%v held=%v; want false/nil", stale, held != nil)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// resolveKeepCount / SweepQueueArchives env-var edge cases
// ──────────────────────────────────────────────────────────────────────────────

// TestSweepQueueArchives_InvalidEnvVarFallsBackToDefault verifies that when
// HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT is set to a non-integer value the sweep
// falls back to the default keep count (5) rather than panicking or erroring.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 (Gap-4) — archive sweep keep
// count is configurable; invalid values must be tolerated with a sensible
// fallback.
//
// Note: t.Parallel() is intentionally omitted — t.Setenv requires sequential
// execution to avoid data races on the process environment.
func TestSweepQueueArchives_InvalidEnvVarFallsBackToDefault(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	// Create 8 archives of one category.
	for i := range 8 {
		archiveSweepCreateFile(t, projectDir,
			"queue.json.failed-202605200000"+string(rune('0'+i+1)))
	}

	// Set env var to an invalid string.
	t.Setenv(queueArchiveEnvVar, "not-a-number")

	// KeepCount == 0 → env var invalid → default 5 → delete 3, retain 5.
	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{})
	if err != nil {
		t.Fatalf("SweepQueueArchives invalid-env: unexpected error: %v", err)
	}
	if result.Deleted != 3 {
		t.Errorf("SweepQueueArchives invalid-env: Deleted = %d, want 3 (default keep=5)", result.Deleted)
	}
	if result.Retained != 5 {
		t.Errorf("SweepQueueArchives invalid-env: Retained = %d, want 5 (default keep=5)", result.Retained)
	}
}

// TestSweepQueueArchives_ZeroEnvVarFallsBackToDefault verifies that when
// HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT is set to "0" (a valid int but not > 0)
// the sweep falls back to the default keep count (5).
//
// Spec ref: resolveKeepCount — "if n, err := strconv.Atoi(envVal); err == nil && n > 0".
//
// Note: t.Parallel() is intentionally omitted — t.Setenv requires sequential
// execution to avoid data races on the process environment.
func TestSweepQueueArchives_ZeroEnvVarFallsBackToDefault(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	for i := range 7 {
		archiveSweepCreateFile(t, projectDir,
			"queue.json.cancelled-202605200000"+string(rune('0'+i+1)))
	}

	t.Setenv(queueArchiveEnvVar, "0") // 0 is not > 0 → fallback to default 5

	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{})
	if err != nil {
		t.Fatalf("SweepQueueArchives zero-env: unexpected error: %v", err)
	}
	if result.Deleted != 2 {
		t.Errorf("SweepQueueArchives zero-env: Deleted = %d, want 2 (7 archives, keep=5)", result.Deleted)
	}
	if result.Retained != 5 {
		t.Errorf("SweepQueueArchives zero-env: Retained = %d, want 5", result.Retained)
	}
}

// TestSweepQueueArchives_ConfigKeepCountOverridesEnvVar verifies that an
// explicit cfg.KeepCount > 0 takes precedence over the env var.
//
// Spec ref: resolveKeepCount — "cfg.KeepCount > 0 → use it (highest priority)."
//
// Note: t.Parallel() is intentionally omitted — t.Setenv requires sequential
// execution to avoid data races on the process environment.
func TestSweepQueueArchives_ConfigKeepCountOverridesEnvVar(t *testing.T) {
	projectDir := archiveSweepMakeHarmonikDir(t)

	for i := range 10 {
		archiveSweepCreateFile(t, projectDir,
			"queue.json.panicked-202605200000"+string(rune('0'+i+1)))
	}

	// Env var says keep 2, but config says keep 7.
	t.Setenv(queueArchiveEnvVar, "2")

	result, err := SweepQueueArchives(projectDir, SweepQueueArchivesConfig{KeepCount: 7})
	if err != nil {
		t.Fatalf("SweepQueueArchives config-override: unexpected error: %v", err)
	}
	if result.Deleted != 3 {
		t.Errorf("SweepQueueArchives config-override: Deleted = %d, want 3 (10 archives, keep=7)", result.Deleted)
	}
	if result.Retained != 7 {
		t.Errorf("SweepQueueArchives config-override: Retained = %d, want 7", result.Retained)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ProbePidfileLock: ambiguous-status path
// ──────────────────────────────────────────────────────────────────────────────

// TestProbePidfileLock_Ambiguous_OwnPIDNoLock verifies that ProbePidfileLock
// returns PidfileLockStatusAmbiguous when:
//   - the flock is acquirable (not held by any process), AND
//   - the recorded PID is alive (our own PID).
//
// This is the "possible PID recycling after OS reboot" path (OQ-PL-007). On
// darwin (where probePidCmdline returns ok=false), the spec mandates the
// conservative Ambiguous return. On Linux, the cmdline corroboration may
// classify the result differently if the test binary path happens to contain
// "harmonik"; we accept either Ambiguous or Stale to keep the test portable.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — "behavior on ambiguity is to
// refuse startup with a specific exit code (OQ-PL-007)."
func TestProbePidfileLock_Ambiguous_OwnPIDNoLock(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := plFixturePidfilePath(projectDir)

	// Write a pidfile containing our own live PID, but do NOT hold the flock.
	// This simulates a pidfile left by a process that lost its flock fd without
	// crashing (e.g. a force-closed fd) — the recorded PID is still alive but
	// no lock is held.
	ownPID := os.Getpid()
	ownPGID, _ := syscall.Getpgid(ownPID) //nolint:errcheck // os.Getpid() is always valid
	content := fmt.Sprintf("%d\n%d\nunknown\n", ownPID, ownPGID)
	if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("ProbePidfileLock ambiguous: WriteFile: %v", err)
	}

	status, probedPID, probeErr := ProbePidfileLock(projectDir)

	// The probe must NOT return Held (we are not holding the flock).
	if status == PidfileLockStatusHeld {
		t.Fatalf("ProbePidfileLock ambiguous: status = Held; expected Ambiguous or Stale (flock is not held)")
	}

	// On darwin the cmdline corroboration is unavailable → always Ambiguous.
	// On Linux the test binary path may or may not contain "harmonik" →
	// either Ambiguous (harmonik in path) or Stale (non-harmonik process) is valid.
	switch status {
	case PidfileLockStatusAmbiguous:
		// ErrPidfileAmbiguous must be returned.
		if !errors.Is(probeErr, ErrPidfileAmbiguous) {
			t.Errorf("ProbePidfileLock ambiguous: status=Ambiguous but errors.Is(err, ErrPidfileAmbiguous)=false; err=%v", probeErr)
		}
		if probedPID != ownPID {
			t.Errorf("ProbePidfileLock ambiguous: probedPID = %d, want %d", probedPID, ownPID)
		}
		t.Logf("ProbePidfileLock ambiguous: status=Ambiguous (as expected on darwin / harmonik-cmdline)")
	case PidfileLockStatusStale:
		// Acceptable on Linux when cmdline corroboration shows non-harmonik binary.
		if probeErr != nil {
			t.Errorf("ProbePidfileLock ambiguous (stale path): err = %v, want nil", probeErr)
		}
		t.Logf("ProbePidfileLock ambiguous: status=Stale (non-harmonik cmdline on Linux)")
	default:
		t.Errorf("ProbePidfileLock ambiguous: unexpected status = %d", status)
	}
}

// TestProbePidfileLock_NonExistentPIDIsDefinitivelyStale verifies that an
// acquirable pidfile whose recorded PID is absent returns a definitive Stale
// result, not the conservative AmbiguousStatus path.
func TestProbePidfileLock_NonExistentPIDIsDefinitivelyStale(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := plFixturePidfilePath(projectDir)
	deadPID := reconLockUpliftFindDeadPID(t)

	content := fmt.Sprintf("%d\n%d\nunknown\n", deadPID, deadPID)
	if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("ProbePidfileLock dead-PID: WriteFile: %v", err)
	}

	status, probedPID, probeErr := ProbePidfileLock(projectDir)
	if probeErr != nil {
		t.Fatalf("ProbePidfileLock dead-PID: unexpected error: %v", probeErr)
	}
	if status == PidfileLockStatusAmbiguous {
		t.Fatalf("ProbePidfileLock dead-PID: status = Ambiguous; want definitive Stale")
	}
	if status != PidfileLockStatusStale {
		t.Fatalf("ProbePidfileLock dead-PID: status = %d, want Stale (%d)", status, PidfileLockStatusStale)
	}
	if probedPID != deadPID {
		t.Errorf("ProbePidfileLock dead-PID: probedPID = %d, want %d", probedPID, deadPID)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// AcquirePidfile: missing .harmonik directory error path
// ──────────────────────────────────────────────────────────────────────────────

// TestAcquirePidfile_MissingHarmonikDir verifies that AcquirePidfile returns a
// wrapped error when the .harmonik directory does not exist. This exercises
// the os.OpenFile failure path (step 1 of PL-002b) without needing filesystem
// manipulation beyond t.TempDir().
//
// Spec ref: process-lifecycle.md §4.1 PL-002b step 1 — O_RDWR|O_CREAT|O_CLOEXEC
// on <projectDir>/.harmonik/daemon.pid; the open must fail if the directory is absent.
func TestAcquirePidfile_MissingHarmonikDir(t *testing.T) {
	t.Parallel()

	// Use a raw TempDir with no .harmonik/ subdirectory.
	projectDir := t.TempDir()
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // os.Getpid() is always valid

	_, err := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7000-8000-000000000099")
	if err == nil {
		t.Fatal("AcquirePidfile missing-dir: expected error for absent .harmonik/ directory, got nil")
	}

	// The error must not be ErrPidfileLocked — it is an open-failure, not a contention error.
	if errors.Is(err, ErrPidfileLocked) {
		t.Errorf("AcquirePidfile missing-dir: error is ErrPidfileLocked; expected open failure, not contention")
	}

	// The underlying error should be a path error (os.PathError / syscall error).
	// We verify the error message references the pidfile path.
	if !strings.Contains(err.Error(), "daemon.pid") {
		t.Errorf("AcquirePidfile missing-dir: error %q does not reference daemon.pid", err.Error())
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Pidfile lock: cross-project isolation (PL-INV-001 regression guard)
// ──────────────────────────────────────────────────────────────────────────────

// TestPidfileLock_CrossProjectIsolation verifies that two separate project
// directories each acquire their own independent pidfile locks without
// interfering with each other. This guards the PL-INV-001 invariant at the
// cross-project scope.
//
// Spec ref: process-lifecycle.md §5 PL-INV-001 — "For each project directory
// at any instant, at most one daemon process MUST hold the pidfile lock."
// The "at most one per project directory" phrasing implies distinct directories
// have independent lock namespaces.
func TestPidfileLock_CrossProjectIsolation(t *testing.T) {
	t.Parallel()

	projectDir1 := plFixtureTempProjectDir(t)
	projectDir2 := plFixtureTempProjectDir(t)

	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // os.Getpid() is always valid

	// Both projects must be independently acquirable simultaneously.
	pf1, err1 := AcquirePidfile(projectDir1, pid, pgid, "01950000-0000-7000-8000-000000000080")
	if err1 != nil {
		t.Fatalf("cross-project isolation: projectDir1 acquire: %v", err1)
	}
	t.Cleanup(func() { _ = pf1.Release() }) //nolint:errcheck // cleanup error unactionable

	pf2, err2 := AcquirePidfile(projectDir2, pid, pgid, "01950000-0000-7000-8000-000000000081")
	if err2 != nil {
		t.Fatalf("cross-project isolation: projectDir2 acquire: %v", err2)
	}
	t.Cleanup(func() { _ = pf2.Release() }) //nolint:errcheck // cleanup error unactionable

	// Verify each pidfile records our PID.
	gotPID1, _, id1, err := ReadPidfile(projectDir1)
	if err != nil {
		t.Fatalf("cross-project isolation: ReadPidfile projectDir1: %v", err)
	}
	gotPID2, _, id2, err := ReadPidfile(projectDir2)
	if err != nil {
		t.Fatalf("cross-project isolation: ReadPidfile projectDir2: %v", err)
	}

	if gotPID1 != pid {
		t.Errorf("cross-project isolation: projectDir1 PID = %d, want %d", gotPID1, pid)
	}
	if gotPID2 != pid {
		t.Errorf("cross-project isolation: projectDir2 PID = %d, want %d", gotPID2, pid)
	}
	if id1 == id2 {
		t.Errorf("cross-project isolation: both pidfiles have same instanceID %q; each must be distinct", id1)
	}
}

// TestReconLockZombieChildStub is a self-exec child for the zombie-PID
// reconciliation-lock test. It exits immediately when the sentinel is set.
func TestReconLockZombieChildStub(t *testing.T) {
	t.Parallel()

	if os.Getenv("GO_RECON_LOCK_ZOMBIE_CHILD") != "1" {
		return
	}
	os.Exit(0)
}
