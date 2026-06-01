package daemon_test

// workloop_nocommit_guard_hk4ie1z_test.go — regression test for hk-4ie1z.
//
// Bug: the SINGLE-MODE no-commit guard (workloop.go beadRunOne) had a buggy
// `mainAdvanced` escape hatch (added for hk-cwxow). When the implementer's
// worktree HEAD never advanced past the parent (NO commit by THIS bead's
// agent), the guard is SUPPOSED to fail the run as `no_commit` and reopen the
// bead. But the escape checked whether refs/heads/main was still at the parent
// SHA — i.e. "did main move at all?" — NOT "did THIS bead's work land?".
//
// Under concurrent/wave dispatch, SIBLING beads merge to main while this bead
// sits in its commitPollTimeout window. By the time the guard ran, main had
// advanced for UNRELATED reasons → mainAdvanced=true → the guard was bypassed →
// the run fell through to the success branch → emitOutcomeEmitted(approved) +
// CloseBead + run_completed("auto-close: exit=0"). Result: a no-commit run was
// FALSELY closed as success and the bead's code never landed (confirmed live:
// hk-tigaf.4 closed this way, NQ-B1 code absent from main).
//
// Fix: replace the `mainAdvanced` ("did main move?") escape with a POSITIVE
// per-bead check (noCommitGuardShouldReopen → beadAlreadySubsumedInMain, the
// `Refs: <beadID>` trailer grep). Fail+reopen UNLESS THIS bead's own work is
// genuinely on main. This mirrors the review-loop guard (reviewloop.go ~567),
// which never had the escape.
//
// These tests exercise noCommitGuardShouldReopen — the decision predicate the
// guard now calls — against a real git repository:
//
//   (A) SIBLING-ON-MAIN (the live bug): HEAD == parent (no commit) + main
//       advanced by a SIBLING bead (different Refs trailer) + THIS bead NOT in
//       main → MUST reopen (true). On current main, where the `mainAdvanced`
//       escape exists, this scenario was bypassed (the run was auto-closed as
//       success); the inline old logic would return "do not reopen". The
//       assertion here therefore fails against the pre-fix escape and passes
//       with the fix.
//
//   (B) HEAD advanced (a real commit) → guard does NOT fire (false).
//
//   (C) SUBSUMED: HEAD == parent (no commit) but THIS bead's Refs trailer IS on
//       main (a prior run landed it) → do NOT reopen (false); the legitimate
//       fall-through is preserved.
//
// Bead ref: hk-4ie1z.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hk4ie1zFixtureRepo creates a minimal git repo on `main` with an initial
// commit and returns (repoDir, parentSHA) where parentSHA is the SHA of that
// initial commit — i.e. the fork point a worktree HEAD would sit at if its
// implementer made no commit.
func hk4ie1zFixtureRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimRight(string(out), "\n")
	}

	run("init", "--initial-branch=main")
	run("config", "user.email", "daemon@harmonik.local")
	run("config", "user.name", "Harmonik Daemon")

	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("init"), 0o644); err != nil {
		t.Fatalf("hk4ie1zFixtureRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")

	parentSHA := run("rev-parse", "refs/heads/main")
	return dir, parentSHA
}

// hk4ie1zAdvanceMain adds a commit to main whose message carries a
// `Refs: <beadID>` trailer, simulating a bead landing. Used to model a SIBLING
// bead merging to main (case A) or THIS bead being subsumed by a prior run
// (case C).
func hk4ie1zAdvanceMain(t *testing.T, dir string, beadID core.BeadID, fileLabel string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	fname := fileLabel + ".txt"
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(filepath.Join(dir, fname), []byte("work for "+fileLabel), 0o644); err != nil {
		t.Fatalf("hk4ie1zAdvanceMain: WriteFile: %v", err)
	}
	run("add", fname)
	run("commit", "-m", "feat: "+fileLabel+"\n\nRefs: "+string(beadID))
}

// TestNoCommitGuard_SiblingAdvancedMain_ThisBeadAbsent_Reopens is the core
// regression for hk-4ie1z: a genuine no-commit run for THIS bead, where main
// has advanced because a DIFFERENT (sibling) bead merged concurrently, MUST
// reopen — it must NOT be treated as a fall-through-to-success.
//
// This is the exact live scenario (hk-tigaf.4 with sibling .7/.8/.10 landing).
// Against the pre-fix `mainAdvanced` escape the guard was bypassed (the run was
// falsely auto-closed); this assertion (reopen == true) fails on that code path
// and passes with the positive per-bead check.
func TestNoCommitGuard_SiblingAdvancedMain_ThisBeadAbsent_Reopens(t *testing.T) {
	t.Parallel()

	const thisBead = core.BeadID("hk-tigaf.4")     // the no-commit run's bead (NQ-B1)
	const siblingBead = core.BeadID("hk-tigaf.10") // a concurrent sibling that DID land

	dir, parentSHA := hk4ie1zFixtureRepo(t)

	// A sibling bead merges to main (different Refs trailer). main advances past
	// parentSHA, but THIS bead's work is NOT present on main.
	hk4ie1zAdvanceMain(t, dir, siblingBead, "sibling-work")

	// Implementer for thisBead made NO commit → its worktree HEAD is still at the
	// fork point (curHeadSHA == parentSHA).
	curHeadSHA := parentSHA

	got := daemon.ExportedNoCommitGuardShouldReopen(t.Context(), dir, curHeadSHA, parentSHA, thisBead)
	if !got {
		t.Fatalf("noCommitGuardShouldReopen = false; want true.\n"+
			"A no-commit run for %s must be REOPENED even though a sibling (%s) advanced main — "+
			"the bug (hk-4ie1z) was the `mainAdvanced` escape treating sibling progress as this bead's success.",
			thisBead, siblingBead)
	}
}

// TestNoCommitGuard_HeadAdvanced_DoesNotFire verifies that when the implementer
// actually advanced HEAD (a real commit on the run branch), the guard does NOT
// fire — there is work to merge.
func TestNoCommitGuard_HeadAdvanced_DoesNotFire(t *testing.T) {
	t.Parallel()

	const thisBead = core.BeadID("hk-4ie1z-head-advanced-001")

	dir, parentSHA := hk4ie1zFixtureRepo(t)

	// The implementer advanced HEAD: curHeadSHA differs from parentSHA. (The
	// concrete value is irrelevant; the guard predicate only compares equality.)
	curHeadSHA := parentSHA + "deadbeef"

	got := daemon.ExportedNoCommitGuardShouldReopen(t.Context(), dir, curHeadSHA, parentSHA, thisBead)
	if got {
		t.Fatalf("noCommitGuardShouldReopen = true; want false when HEAD advanced past parent (a commit exists)")
	}
}

// TestNoCommitGuard_NoCommit_ButThisBeadSubsumed_DoesNotReopen verifies the one
// legitimate fall-through: the run made no commit, but THIS bead's own work is
// genuinely on main (a prior run subsumed it, carrying its Refs trailer). The
// guard must NOT reopen — closing as subsumed-success is correct.
func TestNoCommitGuard_NoCommit_ButThisBeadSubsumed_DoesNotReopen(t *testing.T) {
	t.Parallel()

	const thisBead = core.BeadID("hk-4ie1z-subsumed-001")

	dir, parentSHA := hk4ie1zFixtureRepo(t)

	// A PRIOR run already landed THIS bead's work on main (Refs: thisBead).
	hk4ie1zAdvanceMain(t, dir, thisBead, "this-bead-prior-landing")

	// The current run made no commit → curHeadSHA == parentSHA.
	curHeadSHA := parentSHA

	got := daemon.ExportedNoCommitGuardShouldReopen(t.Context(), dir, curHeadSHA, parentSHA, thisBead)
	if got {
		t.Fatalf("noCommitGuardShouldReopen = true; want false when this bead's own work is already on main (subsumed)")
	}
}
