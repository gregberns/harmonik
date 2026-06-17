package daemon_test

// workloop_predispatch_subsume_hkf38n_test.go — regression test for hk-f38n.
//
// Bug: the pre-dispatch subsumption check (hk-ly0hg Fix-2 / hk-wcv) called
// beadAlreadySubsumedInMain — a bare "Refs: <id>" git-log grep — and closed
// the bead before dispatch when it matched.  For multi-aspect /
// partially-committed beads this was a false-positive: old partial commits
// carrying the same bead ID caused the daemon to conclude the whole bead was
// done and close it without running the remaining work.
//
// Live incident: hk-cmry was pre-dispatch-closed because two earlier partial
// commits (d21d8bcb commitResidualDelta + 46b426de severity-join) both carried
// "Refs: hk-cmry".  The remaining rebase-DROP data-loss fix never ran.  The
// bead had to be refiled as hk-zmpd to escape the poisoned grep-match.
//
// Fix: the pre-dispatch subsumption block was removed from workloop.go
// (hk-f38n).  Crash-restart recovery is now handled entirely by the RUNTIME
// paths — noChange-timeout (pasteInjectQuitOnCommit → noChangeTimeoutCh →
// beadAlreadySubsumedInMain → CloseBead) and noCommitGuard — which both
// inspect whether an agent actually committed before deciding to close.
//
// These tests exercise beadAlreadySubsumedInMain to document the false-positive
// behaviour that made the pre-dispatch use unsafe, and to verify that genuinely-
// landed beads are still detected by the runtime path.
//
// Test cases:
//   (A) PARTIAL-COMMIT (the live bug): a bead has one old commit on main with
//       its Refs trailer, but a second piece of work for the same bead ID is NOT
//       yet on main.  beadAlreadySubsumedInMain returns true — proving that the
//       old pre-dispatch block would have FALSELY closed the bead.  This is why
//       the block was removed: the function alone cannot distinguish "all work
//       done" from "some work done".
//
//   (B) FULLY-LANDED: a bead's one piece of work IS on main.
//       beadAlreadySubsumedInMain returns true — the runtime noChange path uses
//       this correctly (the agent makes no commit → timeout fires → close).
//
//   (C) NOT-LANDED: a bead has no Refs commit on main at all.
//       beadAlreadySubsumedInMain returns false — neither the removed pre-dispatch
//       block nor the runtime path would close it.
//
// Bead ref: hk-f38n.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// hkf38nFixtureRepo creates a minimal git repo on `main` with an initial
// commit and returns the repo directory.
func hkf38nFixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "--initial-branch=main")
	run("config", "user.email", "daemon@harmonik.local")
	run("config", "user.name", "Harmonik Daemon")

	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("init"), 0o644); err != nil {
		t.Fatalf("hkf38nFixtureRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "init")

	return dir
}

// hkf38nCommitToMain adds a commit to main with the given Refs trailer,
// simulating one aspect of a bead landing.
func hkf38nCommitToMain(t *testing.T, dir string, beadID core.BeadID, fileLabel string) {
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
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(filepath.Join(dir, fname), []byte("aspect: "+fileLabel), 0o644); err != nil {
		t.Fatalf("hkf38nCommitToMain: WriteFile: %v", err)
	}
	run("add", fname)
	run("commit", "-m", "feat: "+fileLabel+"\n\nRefs: "+string(beadID))
}

// TestBeadAlreadySubsumedInMain_PartialCommit_FalsePositive documents the root
// cause of hk-f38n: a bead with a PARTIAL old commit on main (one aspect done,
// another not) causes beadAlreadySubsumedInMain to return true.
//
// In the old pre-dispatch block this true return triggered CloseBead, falsely
// marking the bead as fully done.  The block was removed (hk-f38n) so this
// false-positive can no longer cause an incorrect close.
//
// The runtime noChange path only fires when the agent itself makes no commit; a
// partial-commit bead whose remaining work is not on main will have the agent
// produce a new commit, so the noChange path never fires — the bead proceeds
// normally.
func TestBeadAlreadySubsumedInMain_PartialCommit_FalsePositive(t *testing.T) {
	t.Parallel()

	// hk-cmry is the real victim bead; we reproduce the shape here.
	const beadID = core.BeadID("hk-cmry-test")

	dir := hkf38nFixtureRepo(t)

	// Commit ONE aspect of the bead (e.g. commitResidualDelta, analogous to
	// d21d8bcb in the live incident).  A SECOND aspect (the rebase-DROP fix) has
	// NOT yet landed.
	hkf38nCommitToMain(t, dir, beadID, "partial-aspect-1")

	// beadAlreadySubsumedInMain returns true because the bare Refs grep matches.
	// This is the false-positive: the function cannot tell that only one of two
	// aspects is done.
	got := daemon.ExportedBeadAlreadySubsumedInMain(t.Context(), dir, beadID)
	if !got {
		t.Fatalf("beadAlreadySubsumedInMain = false for partial commit; want true.\n"+
			"The function performs a bare Refs: %s grep and will match even partial commits.\n"+
			"This test documents why using it at pre-dispatch to CLOSE the bead was unsafe.",
			beadID)
	}
	// The test passes (true is expected) — documenting the false-positive.
	// The pre-dispatch close block that acted on this return value was removed by
	// hk-f38n, so this true return no longer causes an incorrect bead close.
}

// TestBeadAlreadySubsumedInMain_FullyLanded_ReturnsTrue verifies that a bead
// whose work IS fully on main is detected by beadAlreadySubsumedInMain.
// This covers the runtime noChange path: when an agent makes no commit and
// the function returns true, the noChange-timeout fires CloseBead — correctly,
// because the bead really is done (crash-restart scenario).
func TestBeadAlreadySubsumedInMain_FullyLanded_ReturnsTrue(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-f38n-landed-001")

	dir := hkf38nFixtureRepo(t)

	// The single piece of work for this bead is on main.
	hkf38nCommitToMain(t, dir, beadID, "full-landing")

	got := daemon.ExportedBeadAlreadySubsumedInMain(t.Context(), dir, beadID)
	if !got {
		t.Fatalf("beadAlreadySubsumedInMain = false for fully-landed bead; want true.\n"+
			"The runtime noChange path relies on this returning true to close crash-restart beads.")
	}
}

// TestBeadAlreadySubsumedInMain_NotLanded_ReturnsFalse verifies that a bead
// with no Refs commit on main is NOT detected as subsumed.
func TestBeadAlreadySubsumedInMain_NotLanded_ReturnsFalse(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-f38n-notlanded-001")

	dir := hkf38nFixtureRepo(t)

	// No commit for this bead on main at all.
	got := daemon.ExportedBeadAlreadySubsumedInMain(t.Context(), dir, beadID)
	if got {
		t.Fatalf("beadAlreadySubsumedInMain = true for a bead with no Refs commit; want false.")
	}
}

// TestBeadAlreadySubsumedInMain_SiblingCommit_DoesNotMatch verifies that a
// commit carrying a DIFFERENT bead's Refs trailer does not cause the target
// bead to be detected as subsumed.
func TestBeadAlreadySubsumedInMain_SiblingCommit_DoesNotMatch(t *testing.T) {
	t.Parallel()

	const targetBead = core.BeadID("hk-f38n-target-001")
	const siblingBead = core.BeadID("hk-f38n-sibling-001")

	dir := hkf38nFixtureRepo(t)

	// A sibling bead landed; the target bead is still absent.
	hkf38nCommitToMain(t, dir, siblingBead, "sibling-work")

	got := daemon.ExportedBeadAlreadySubsumedInMain(t.Context(), dir, targetBead)
	if got {
		t.Fatalf("beadAlreadySubsumedInMain = true for target bead when only a sibling Refs commit exists; want false.\n"+
			"Sibling %s must not be confused with target %s.", siblingBead, targetBead)
	}
}
