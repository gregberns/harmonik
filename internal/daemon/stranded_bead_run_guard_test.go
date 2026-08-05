package daemon_test

// stranded_bead_run_guard_test.go — what the stranded-bead guard does when it
// cannot read the run registry at all.
//
// The stranded-bead auto-reset takes a bead back when nothing is working it. The
// guard is the one question it asks first: is there a run on this bead? A "yes"
// means an adoption goroutine is watching a live tmux session, and resetting then
// races an agent that is mid-flight.
//
// The interesting answer is the third one. A filesystem error is not an empty
// registry — it is an UNKNOWN registry — and the guard reports "a run may exist"
// so the caller skips the reset rather than racing a session it failed to see.
// That branch had a test seam written for it and no test behind the seam, so it
// had never run. It is also the branch that is hardest to reason about from the
// source, because it looks like a defect: a function that returns true on error
// reads as fail-open until you know what the true means.
//
// Helper prefix: strandedGuard.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

const strandedGuardBead = core.BeadID("hk-stranded-guard")

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
			RunID:         "0f0e0d0c-0b0a-0908-0706-050403020177",
			BeadID:        string(strandedGuardBead),
			SessionName:   "harmonik-abcdef012345-run-0f0e0d0c0b0a",
		}); err != nil {
			t.Fatalf("strandedGuard: write the record: %v", err)
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
		// .harmonik/runs/ is where the records live. A plain file in its place makes
		// every directory read fail with a real operating-system error, which is the
		// closest a test can get to the disk problem this branch is for without
		// depending on permissions the test runner may or may not have.
		if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o750); err != nil {
			t.Fatalf("strandedGuard: make .harmonik: %v", err)
		}
		if err := os.WriteFile(filepath.Join(projectDir, ".harmonik", "runs"),
			[]byte("not a directory\n"), 0o600); err != nil {
			t.Fatalf("strandedGuard: put a file where the registry directory goes: %v", err)
		}
		if _, err := runpkg.List(projectDir); err == nil {
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
