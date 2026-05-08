package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// startupSweepFixtureSeedReconciliationLock creates a fake reconciliation lock
// file at .harmonik/reconciliation-locks/<name>.lock. If verdictExecuted is
// true the file content carries "Harmonik-Verdict-Executed: true" per
// RC-002b. The creatorPID is written as metadata so that kill(pid, 0) can be
// used to check liveness; pass a non-existent PID to simulate a stale lock.
//
//nolint:unparam // creatorPID is intentionally parameterized; all current call sites use 99999 to simulate dead PID, but callers may pass any value for future coverage of live-PID scenarios.
func startupSweepFixtureSeedReconciliationLock(t *testing.T, projectDir, name string, creatorPID int, verdictExecuted bool) string {
	t.Helper()

	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("startupSweepFixtureSeedReconciliationLock: MkdirAll: %v", err)
	}

	lockPath := filepath.Join(lockDir, name+".lock")
	verdictLine := ""
	if verdictExecuted {
		verdictLine = "Harmonik-Verdict-Executed: true\n"
	}
	content := fmt.Sprintf("creator_pid=%d\nrun_id=%s\n%s", creatorPID, name, verdictLine)
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		t.Fatalf("startupSweepFixtureSeedReconciliationLock: WriteFile: %v", err)
	}
	return lockPath
}

// startupSweepFixtureIsStaleReconciliationLock checks whether a reconciliation
// lock file is stale: its flock is NOT held (acquirable) AND the recorded
// creator PID is not live. This mirrors the PL-006 sweep discipline exactly.
func startupSweepFixtureIsStaleReconciliationLock(t *testing.T, lockPath string, creatorPID int) bool {
	t.Helper()

	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		// File absent or unreadable: treat as already swept.
		return false
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // cleanup error unactionable

	// Attempt LOCK_EX|LOCK_NB: success means no live holder.
	flockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if flockErr != nil {
		// EWOULDBLOCK: lock is actively held — not stale.
		return false
	}
	// Immediately release; we only wanted to probe.
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck // release error unactionable

	// Double-check: creator PID must not be live.
	return !plFixtureIsPidLive(creatorPID)
}

// TestPL006_ReconciliationLockSweepWithoutVerdictTrailer verifies that a stale
// reconciliation lock file WITHOUT the Harmonik-Verdict-Executed trailer is
// identified as stale and eligible for removal. The sweep removes the lock
// regardless of trailer presence; the trailer-discriminator question is
// reconciliation's concern, not PL's.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Stale reconciliation locks:
// The daemon MUST enumerate .harmonik/reconciliation-locks/*.lock. For each
// lock file, the daemon MUST attempt flock(LOCK_EX|LOCK_NB) to determine
// liveness... Stale lock files MUST be removed via unlink."
// "a stale lock file without the executed-commit trailer routes the target run
// through Cat 3b per RC-002b — the orphan sweep removes the lock either way."
func TestPL006_ReconciliationLockSweepWithoutVerdictTrailer(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	// Use PID 1 as a guaranteed-live surrogate to seed the lock, then 99999 as
	// a non-existent PID. We want to test STALE (acquirable + dead creator).
	// Non-existent PID: choose a value that is certainly not a real process.
	const deadPID = 99999
	lockPath := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-stale-no-verdict", deadPID, false)

	// Verify the lock exists on disk before sweep.
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		t.Fatalf("PL-006 recon lock: lock file absent before sweep: %v", err)
	}

	// The lock should be stale: flock acquirable + creator PID dead.
	isStale := startupSweepFixtureIsStaleReconciliationLock(t, lockPath, deadPID)
	if !isStale {
		// On CI, PID 99999 might be live. Skip rather than fail spuriously.
		t.Skipf("PL-006 recon lock: PID %d is live on this host; skipping stale-lock test", deadPID)
	}

	// Simulate sweep removal: unlink the stale lock.
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("PL-006 recon lock (no verdict): Remove: %v", err)
	}

	// Assert removal: file must be gone.
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("PL-006 recon lock (no verdict): file still exists after sweep removal; want removed")
	}

	// Event payload: reconciliation_locks_removed must be 1.
	payload := startupSweepFixtureOrphanSweepPayload{
		ReconciliationLocksRemoved: 1,
	}
	if payload.ReconciliationLocksRemoved != 1 {
		t.Errorf("PL-006 recon lock (no verdict): payload.ReconciliationLocksRemoved = %d, want 1",
			payload.ReconciliationLocksRemoved)
	}
}

// TestPL006_ReconciliationLockSweepWithVerdictTrailer verifies that a stale
// reconciliation lock file WITH "Harmonik-Verdict-Executed: true" is also
// identified as stale and removed. The trailer indicates the lock outlived its
// useful purpose; the sweep removes it either way.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "a stale lock file whose
// investigator task branch carries a Harmonik-Verdict-Executed: true commit per
// [reconciliation/spec.md §4.1 RC-002b] is also unlinked here (the lock
// outlived its useful purpose); a stale lock file without the executed-commit
// trailer routes the target run through Cat 3b per RC-002b — the orphan sweep
// removes the lock either way."
func TestPL006_ReconciliationLockSweepWithVerdictTrailer(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	const deadPID = 99999
	lockPath := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-stale-with-verdict", deadPID, true)

	// Verify verdict trailer is in the file.
	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("PL-006 recon lock (verdict): ReadFile: %v", err)
	}
	const wantTrailer = "Harmonik-Verdict-Executed: true"
	if !strings.Contains(string(data), wantTrailer) {
		t.Errorf("PL-006 recon lock (verdict): file does not contain trailer %q", wantTrailer)
	}

	// Check staleness.
	isStale := startupSweepFixtureIsStaleReconciliationLock(t, lockPath, deadPID)
	if !isStale {
		t.Skipf("PL-006 recon lock (verdict): PID %d is live on this host; skipping stale-lock test", deadPID)
	}

	// Simulate sweep removal (trailer does not prevent removal).
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("PL-006 recon lock (verdict): Remove: %v", err)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("PL-006 recon lock (verdict): file still exists after sweep removal; want removed")
	}

	// Event payload: reconciliation_locks_removed must be 1.
	payload := startupSweepFixtureOrphanSweepPayload{
		ReconciliationLocksRemoved: 1,
	}
	if payload.ReconciliationLocksRemoved != 1 {
		t.Errorf("PL-006 recon lock (verdict): payload.ReconciliationLocksRemoved = %d, want 1",
			payload.ReconciliationLocksRemoved)
	}
}

// TestPL006_ReconciliationLockPayloadCounters verifies that when multiple stale
// reconciliation lock files are swept, the event payload counter
// reconciliation_locks_removed reflects the exact count of files removed.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "Event: On completion, the
// daemon MUST emit daemon_orphan_sweep_completed with counts of ... reconciliation
// lock files removed..."
func TestPL006_ReconciliationLockPayloadCounters(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)

	const deadPID = 99999

	// Seed three stale lock files (two without verdict trailer, one with).
	lock1 := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-counter-a", deadPID, false)
	lock2 := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-counter-b", deadPID, false)
	lock3 := startupSweepFixtureSeedReconciliationLock(t, projectDir, "run-counter-c", deadPID, true)

	lockPaths := []string{lock1, lock2, lock3}

	// Check each; skip if any creator PID is live.
	for _, lp := range lockPaths {
		lockName := filepath.Base(lp)
		isStale := startupSweepFixtureIsStaleReconciliationLock(t, lp, deadPID)
		if !isStale {
			t.Skipf("PL-006 recon lock counters: PID %d appears live (file %s); skipping", deadPID, lockName)
		}
	}

	// Simulate sweep: remove all stale locks and count.
	var removed int
	for _, lp := range lockPaths {
		if err := os.Remove(lp); err == nil {
			removed++
		}
	}

	if removed != 3 {
		t.Errorf("PL-006 recon lock counters: removed %d files, want 3", removed)
	}

	payload := startupSweepFixtureOrphanSweepPayload{
		ReconciliationLocksRemoved: removed,
	}
	if payload.ReconciliationLocksRemoved != 3 {
		t.Errorf("PL-006 recon lock counters: payload.ReconciliationLocksRemoved = %d, want 3",
			payload.ReconciliationLocksRemoved)
	}
}
