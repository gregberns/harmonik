package daemon_test

// escapedetect_hkooexj_test.go — regression tests for the implementer-escaped-
// worktree detector's false-positive on pre-existing / gitignored untracked
// files in the main repo tree.
//
// Bug (hk-ooexj): checkMainWorkingTreeDirty flagged ANY untracked path outside
// the harmonik churn allowlist as an "escape", even when (a) the file was
// gitignored, or (b) the file already existed before the run started and the
// implementer never touched it. This failed dispatched beads (hk-c6grw twice)
// with `implementer_escaped_worktree: 1 file(s) dirty in main: HANDOFF-flywheel.md`.
//
// The fix:
//   - snapshotUntrackedFiles captures pre-existing untracked paths at run-start.
//   - checkMainWorkingTreeDirty excludes baselined paths AND gitignored paths,
//     so only NET-NEW, non-ignored files outside the worktree flag as escapes.
//
// Helper prefix: escapeFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-ooexj).

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// escapeFixtureGitRepo initialises a git repo in a temp dir with one commit on
// "main" and a `.gitignore` that ignores `HANDOFF-*.md` (mirroring the real
// harmonik repo's gitignore line 60). Returns the repo root.
func escapeFixtureGitRepo(t *testing.T) string {
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

	run("init", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	escapeFixtureWrite(t, dir, ".gitignore", "HANDOFF-*.md\n")
	escapeFixtureWrite(t, dir, "README", "test\n")
	// Track .beads/issues.jsonl so a later modification surfaces as "M
	// .beads/issues.jsonl" (the real repo's churn shape), exercising the
	// isHarmonikChurn allowlist rather than an untracked-dir collapse.
	escapeFixtureWrite(t, dir, ".beads/issues.jsonl", "{}\n")
	run("add", ".gitignore", "README", ".beads/issues.jsonl")
	run("commit", "-m", "init")

	return dir
}

// escapeFixtureWrite writes content to dir/rel, creating parent dirs as needed.
func escapeFixtureWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("escapeFixtureWrite: MkdirAll: %v", err)
	}
	//nolint:gosec // G306: 0644 is fine for a test fixture file
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("escapeFixtureWrite: WriteFile %s: %v", rel, err)
	}
}

// TestEscapeDetect_GitignoredPreExistingNotFlagged is the primary regression
// test (hk-ooexj): a run completes with TWO pre-existing files present in the
// project root before the run started — one gitignored (HANDOFF-flywheel.md)
// and one untracked-but-not-ignored (scratch-note.txt). Neither must be flagged
// as an escape.
func TestEscapeDetect_GitignoredPreExistingNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Pre-existing files present BEFORE the run starts.
	escapeFixtureWrite(t, dir, "HANDOFF-flywheel.md", "scratch handoff\n") // gitignored
	escapeFixtureWrite(t, dir, "scratch-note.txt", "a note\n")             // untracked, not ignored

	// Snapshot the baseline at run-start (the daemon does this before launching
	// the implementer).
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// After the run, with the implementer having touched nothing in main, the
	// escape check must report clean.
	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline, "")
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected NO escape for pre-existing gitignored + untracked files, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_GitignoredNotFlaggedEvenWithoutBaseline verifies the
// gitignore prong on its own: even with a nil baseline (e.g. snapshot failed at
// run-start), a gitignored file present at check time is NOT flagged — git
// status omits it by default and the explicit check-ignore pass is belt-and-
// suspenders.
func TestEscapeDetect_GitignoredNotFlaggedEvenWithoutBaseline(t *testing.T) {
	dir := escapeFixtureGitRepo(t)
	escapeFixtureWrite(t, dir, "HANDOFF-flywheel.md", "scratch handoff\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, nil, "")
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected gitignored file to be ignored, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_NetNewUntrackedStillFlagged is the positive test: a NET-NEW,
// non-ignored file created DURING the run (not in the run-start baseline) is
// still flagged as an escape. This is the real cross-contamination the detector
// must catch.
func TestEscapeDetect_NetNewUntrackedStillFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Pre-existing baseline contains scratch-note.txt only.
	escapeFixtureWrite(t, dir, "scratch-note.txt", "a note\n")
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// Implementer escapes its worktree and writes a NEW file into main. Written
	// at the repo root (not a new subdir) so git porcelain reports the full
	// filename rather than collapsing a new directory to "dir/".
	escapeFixtureWrite(t, dir, "leaked.go", "package main\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline, "")
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if !dirty {
		t.Fatalf("expected NET-NEW file to be flagged as escape, got dirty=%v files=%v", dirty, files)
	}
	found := false
	for _, f := range files {
		if f == "leaked.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected leaked.go in escape list, got %v", files)
	}
	// The pre-existing baselined scratch-note.txt must NOT appear.
	for _, f := range files {
		if f == "scratch-note.txt" {
			t.Fatalf("pre-existing baselined file scratch-note.txt should not be flagged, got %v", files)
		}
	}
}

// TestEscapeDetect_NetNewGitignoredNotFlagged verifies that even a NET-NEW file
// the implementer creates is NOT flagged when it is gitignored — gitignored
// paths are never a real escape (they would not be committed regardless).
func TestEscapeDetect_NetNewGitignoredNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// Implementer writes a NEW gitignored file during the run.
	escapeFixtureWrite(t, dir, "HANDOFF-newthread.md", "new handoff\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline, "")
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected NET-NEW gitignored file to be ignored, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_HarmonikChurnNotFlagged is a guard for the existing
// allowlist: expected harmonik churn must still be excluded regardless of
// baseline. Exercises a MODIFIED tracked .beads/issues.jsonl (the real repo's
// churn shape) and untracked files under .harmonik/ and .claude/.
func TestEscapeDetect_HarmonikChurnNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Modify the tracked bead ledger → "M .beads/issues.jsonl".
	escapeFixtureWrite(t, dir, ".beads/issues.jsonl", "{\"x\":1}\n")
	// Untracked daemon/orchestrator state under the churn-prefix dirs.
	escapeFixtureWrite(t, dir, ".harmonik/queue.json", "{}\n")
	escapeFixtureWrite(t, dir, ".claude/scratch.json", "{}\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, nil, "")
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected harmonik churn to be excluded, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_AgentCommsNotFlagged is the regression for hk-77q8e case 2:
// AGENT_COMMS.md dropped at the repo root by a concurrent orchestrator agent
// mid-run must NOT be flagged as an implementer escape (it is known churn).
func TestEscapeDetect_AgentCommsNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Baseline is empty — AGENT_COMMS.md did not exist at run-start.
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}

	// Concurrent agent creates AGENT_COMMS.md during the run.
	escapeFixtureWrite(t, dir, "AGENT_COMMS.md", "## ts · orchestrator\nhello\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline, "")
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected AGENT_COMMS.md to be excluded as churn, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_SiblingMergeRaceNotFlagged is the regression for hk-77q8e
// case 1: files modified in main by a sibling bead's update-ref (before
// reset-hard runs) must not be flagged as implementer escapes.
//
// The scenario simulates the update-ref / reset-hard race window:
//  1. Record preRunMainSHA (main at run-start = commit A).
//  2. A sibling bead advances main to commit B via update-ref, updating HEAD
//     without touching the working tree (reset-hard hasn't run yet).
//  3. The escape check fires: git status sees sibling.go as "M" (HEAD=B says
//     one thing, working tree from A says another).
//  4. With hk-77q8e fix, sibling.go is excluded from escape candidates.
func TestEscapeDetect_SiblingMergeRaceNotFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	// Capture baseline and main HEAD at run-start (commit A).
	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}
	preRunMainSHA := daemon.ExportedSnapshotMainHEAD(t.Context(), dir)
	if preRunMainSHA == "" {
		t.Fatal("snapshotMainHEAD returned empty")
	}

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

	// Simulate a sibling bead: create a new file in a branch, then advance
	// refs/heads/main to that branch tip using update-ref (skipping reset-hard
	// to reproduce the race window).
	run("checkout", "-b", "sibling-run")
	escapeFixtureWrite(t, dir, "sibling.go", "package daemon\n")
	run("add", "sibling.go")
	run("commit", "-m", "sibling bead lands sibling.go")
	siblingTip := run("rev-parse", "HEAD")
	run("checkout", "main")

	// update-ref advances main to sibling tip WITHOUT updating the working tree
	// (no reset-hard). This is the race window: HEAD is at siblingTip but the
	// working tree still lacks sibling.go, so git status reports it as deleted
	// (or the reverse for a newly-added file) and escape check would false-flag.
	run("update-ref", "refs/heads/main", siblingTip)

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline, preRunMainSHA)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if dirty {
		t.Fatalf("expected sibling merge files to be excluded from escape check, got dirty=%v files=%v", dirty, files)
	}
}

// TestEscapeDetect_SiblingMergeRaceRealEscapeStillFlagged confirms that when an
// implementer writes a DIFFERENT file (not in the sibling diff) to the main
// working tree during the same window, that escape is still detected.
func TestEscapeDetect_SiblingMergeRaceRealEscapeStillFlagged(t *testing.T) {
	dir := escapeFixtureGitRepo(t)

	baseline, err := daemon.ExportedSnapshotUntrackedFiles(t.Context(), dir)
	if err != nil {
		t.Fatalf("snapshotUntrackedFiles: %v", err)
	}
	preRunMainSHA := daemon.ExportedSnapshotMainHEAD(t.Context(), dir)

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

	// Sibling bead lands sibling.go.
	run("checkout", "-b", "sibling-run2")
	escapeFixtureWrite(t, dir, "sibling.go", "package daemon\n")
	run("add", "sibling.go")
	run("commit", "-m", "sibling lands")
	siblingTip := run("rev-parse", "HEAD")
	run("checkout", "main")
	run("update-ref", "refs/heads/main", siblingTip)

	// Implementer ALSO escapes its worktree and writes escaped.go (NOT in the
	// sibling diff) directly into the main working tree.
	escapeFixtureWrite(t, dir, "escaped.go", "package main\n")

	dirty, files, checkErr := daemon.ExportedCheckMainWorkingTreeDirty(t.Context(), dir, baseline, preRunMainSHA)
	if checkErr != nil {
		t.Fatalf("checkMainWorkingTreeDirty: %v", checkErr)
	}
	if !dirty {
		t.Fatalf("expected real escape (escaped.go) to be flagged even with sibling merge, got dirty=%v files=%v", dirty, files)
	}
	found := false
	for _, f := range files {
		if f == "escaped.go" {
			found = true
		}
	}
	if !found {
		t.Fatalf("escaped.go not in dirty list, got %v", files)
	}
	// sibling.go must NOT appear in the escape list.
	for _, f := range files {
		if f == "sibling.go" {
			t.Fatalf("sibling.go incorrectly flagged as escape, got %v", files)
		}
	}
}
