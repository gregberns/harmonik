package keeper

// keeper_revive_flock_hk220lv_test.go — pins the SIGNAL the daemon's
// keeper-revive watcher (internal/daemon/keeperrevive.go) polls: the exclusive
// flock, NOT the existence of the lockfile.
//
// This distinction is the whole bead. A dead keeper process leaves its
// .harmonik/keeper/<agent>.lock file on disk with its stale PID inside; the
// kernel drops only the advisory flock, silently. Any liveness check written
// against file existence (or mtime, or the PID text) reads a dead keeper as
// alive and the revive watcher never fires — which is precisely the 43h
// unmonitored-crew field case.
//
// Tests cover:
//   - A lockfile that EXISTS but is not flocked reports NO live keeper.
//   - AcquireLock → LiveKeeperPresent true → Release → LiveKeeperPresent false,
//     with the lockfile still present on disk after Release.
//
// Helper prefix: krf
//
// Bead ref: hk-220lv.

import (
	"os"
	"path/filepath"
	"testing"
)

// krfLockPath returns the lockfile path LiveKeeperPresent probes for agent.
func krfLockPath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".lock")
}

// TestLiveKeeperPresent_FlockNotFileExistence_hk220lv: the flock is the liveness
// signal. The lockfile survives the keeper process; only the flock encodes
// liveness, and it vanishes without a trace when the holder dies.
func TestLiveKeeperPresent_FlockNotFileExistence_hk220lv(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	const agent = "revive-probe"

	lock, err := AcquireLock(projectDir, agent)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	if !LiveKeeperPresent(projectDir, agent) {
		t.Fatal("LiveKeeperPresent = false while the exclusive flock is held; want true " +
			"(regression: the revive watcher would re-arm a HEALTHY keeper on every scan)")
	}

	if relErr := lock.Release(); relErr != nil {
		t.Fatalf("Release: %v", relErr)
	}

	// Releasing the flock is what a dying keeper process does implicitly. The
	// lockfile itself is untouched — this is the trap.
	if _, statErr := os.Stat(krfLockPath(projectDir, agent)); statErr != nil {
		t.Fatalf("lockfile must still exist after Release (that is the whole point of this test): %v", statErr)
	}
	if LiveKeeperPresent(projectDir, agent) {
		t.Error("LiveKeeperPresent = true after the flock was released but the lockfile remains; want false " +
			"(regression: liveness is being read off FILE EXISTENCE or the stale PID text instead of the flock, " +
			"so a dead keeper reads as alive and the crew runs unmonitored forever)")
	}
}

// TestLiveKeeperPresent_UnflockedLockfile_hk220lv: a lockfile created out-of-band
// (never flocked — e.g. left behind by a crashed keeper) reports no live keeper.
func TestLiveKeeperPresent_UnflockedLockfile_hk220lv(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	const agent = "stale-lockfile"

	dir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches AcquireLock's own .harmonik dir convention — the corpse must look exactly like a real one
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// A plausible corpse: the file exists and even carries a PID line.
	if err := os.WriteFile(krfLockPath(projectDir, agent), []byte("4242\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if LiveKeeperPresent(projectDir, agent) {
		t.Error("LiveKeeperPresent = true for an existing but never-flocked lockfile; want false " +
			"(regression: a crashed keeper's leftover lockfile masks the death from the revive watcher)")
	}
}
