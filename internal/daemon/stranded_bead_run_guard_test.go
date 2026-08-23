package daemon_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

const strandedGuardBead = core.BeadID("hk-stranded-guard")

const strandedGuardRunID = "0f0e0d0c-0b0a-7908-8706-050403020177"

// TestStrandedBeadGuard_AnUnreadableRegistryReportsARunRatherThanNone drives the
// error branch and both of its neighbours, because the branch only means
// something next to them.
//
// All three cases ask the same question of the same function. The empty registry
// is what "no run" looks like. The seeded record is what "a run" looks like. The
// unreadable registry has to answer like the second, not the first: the reset it
// gates destroys an agent's work in progress, and the cost of being wrong the
// other way is one bead that waits for the next sweep.
func TestStrandedBeadGuard_AnUnreadableRegistryReportsARunRatherThanNone(t *testing.T) {
	t.Parallel()

	t.Run("no record", func(t *testing.T) {
		t.Parallel()
		if daemon.ExportedStrandedBeadHasOnDiskRun(t.TempDir(), strandedGuardBead) {
			t.Error("the guard reports a run for a bead in an empty registry.\n" +
				"A guard that answers yes to everything blocks every stranded-bead reset for " +
				"ever, and would pass the unreadable-registry case below while distinguishing " +
				"nothing.")
		}
	})

	t.Run("a record for this bead", func(t *testing.T) {
		t.Parallel()
		projectDir := t.TempDir()
		if err := runpkg.Write(projectDir, runpkg.Record{
			SchemaVersion: 1,
			RunID:         strandedGuardRunID,
			BeadID:        string(strandedGuardBead),
			SessionName:   "harmonik-abcdef012345-run-0f0e0d0c0b0a",
			StartedAt:     time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("strandedGuard: write the record: %v", err)
		}
		snapshot, err := runpkg.ScanRegistry(projectDir)
		if err != nil {
			t.Fatalf("strandedGuard: the production reader refused the seeded record: %v", err)
		}
		if len(snapshot.Legacy) != 1 || snapshot.Legacy[0].BeadID != string(strandedGuardBead) {
			t.Fatalf("strandedGuard: the registry holds %d legacy and %d dispatch records, want "+
				"exactly one legacy record for %s.\n"+
				"The check below would then be measuring the error branch, not the record.",
				len(snapshot.Legacy), len(snapshot.Dispatch), strandedGuardBead)
		}
		if !daemon.ExportedStrandedBeadHasOnDiskRun(projectDir, strandedGuardBead) {
			t.Error("the guard reports no run for a bead whose record is on disk.\n" +
				"It is not reading the registry at all, so the unreadable-registry case below " +
				"would be measuring something other than what it names.")
		}
	})

	t.Run("the registry cannot be read", func(t *testing.T) {
		t.Parallel()
		projectDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o750); err != nil {
			t.Fatalf("strandedGuard: make .harmonik: %v", err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, ".harmonik", "runs"),
			[]byte("not a directory\n"), 0o600); err != nil {
			t.Fatalf("strandedGuard: put a file where the registry directory goes: %v", err)
		}
		if _, err := runpkg.ScanRegistry(projectDir); err == nil {
			t.Fatal("the registry listed cleanly, so this case is not driving the error branch " +
				"and the assertion below would pass on the ordinary no-record answer")
		}

		if !daemon.ExportedStrandedBeadHasOnDiskRun(projectDir, strandedGuardBead) {
			t.Error("the guard reports no run when it could not read the registry at all.\n" +
				"An unreadable registry is UNKNOWN, not empty. Reading it as empty lets the " +
				"stranded-bead auto-reset fire on a bead whose adoption goroutine is watching a " +
				"live session — the one case the guard was added to prevent — and the disk " +
				"problem that caused it is invisible in the outcome.")
		}
	})
}
