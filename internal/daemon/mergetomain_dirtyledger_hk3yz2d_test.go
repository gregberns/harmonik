package daemon_test

// mergetomain_dirtyledger_hk3yz2d_test.go — regression test for the pre-rebase
// ledger-cleanup step added in hk-3yz2d.
//
// Bug: a live DOT-mode run walked the whole graph correctly but FAILED at the
// final merge-to-main with:
//
//	rebase_conflict: exit status 1
//	error: cannot rebase: You have unstaged changes.
//	error: Please commit or stash them.
//
// The unstaged change was .beads/issues.jsonl — a TRACKED file that the daemon's
// own `br` operations dirty (the shared SQLite DB flushes to the per-worktree
// JSONL during a run). `git rebase main` refuses to start when the worktree has
// unstaged changes, so the merge failed even though the agent produced a clean,
// committed run-branch.
//
// The fix discards the UNCOMMITTED ledger churn in the worktree before the
// rebase (discardDirtyBeadsLedger), because main is the canonical source of
// truth for the ledger and the daemon owns all terminal bead transitions.
//
// This is GENERAL (affects all workflow modes), not DOT-specific: every mode
// routes its success path through mergeRunBranchToMain → the same in-worktree
// `git rebase main`.
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 2.
// Bead: hk-3yz2d.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// dirtyLedgerGit runs a git command in dir and fails the test on error.
func dirtyLedgerGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (dir=%s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// dirtyLedgerSetup builds a main repo with a committed .beads/issues.jsonl, a
// run worktree branched from the initial commit, an agent commit on the run
// branch (code only), and an advanced main (so the rebase has real work to do).
// It returns the worktree path.
func dirtyLedgerSetup(t *testing.T) string {
	t.Helper()

	mainRepo := t.TempDir()
	dirtyLedgerGit(t, mainRepo, "init", "--initial-branch=main", ".")
	dirtyLedgerGit(t, mainRepo, "config", "user.email", "t@t.com")
	dirtyLedgerGit(t, mainRepo, "config", "user.name", "t")

	beadsDir := filepath.Join(mainRepo, ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil { //nolint:gosec // G301 test fixture
		t.Fatalf("MkdirAll .beads: %v", err)
	}
	ledgerPath := filepath.Join(beadsDir, "issues.jsonl")
	writeFile(t, ledgerPath, `{"id":"a","status":"open"}`+"\n")
	writeFile(t, filepath.Join(mainRepo, "code.txt"), "code\n")
	dirtyLedgerGit(t, mainRepo, "add", "-A")
	dirtyLedgerGit(t, mainRepo, "commit", "-m", "init")
	baseSHA := dirtyLedgerGit(t, mainRepo, "rev-parse", "HEAD")

	// Run worktree from the base commit, on its own branch.
	wtPath := filepath.Join(t.TempDir(), "wt")
	dirtyLedgerGit(t, mainRepo, "worktree", "add", "-b", "runbranch", wtPath, baseSHA)

	// Agent makes a code-only commit on the run branch.
	writeFile(t, filepath.Join(wtPath, "code.txt"), "code\nagent work\n")
	dirtyLedgerGit(t, wtPath, "add", "code.txt")
	dirtyLedgerGit(t, wtPath, "commit", "-m", "agent work")

	// Advance main with a real ledger commit so the rebase has something to do
	// (mirrors the daemon committing its own claim/transition churn elsewhere).
	writeFile(t, ledgerPath, `{"id":"a","status":"open"}`+"\n"+`{"id":"b","status":"in_progress"}`+"\n")
	dirtyLedgerGit(t, mainRepo, "add", ".beads/issues.jsonl")
	dirtyLedgerGit(t, mainRepo, "commit", "-m", "daemon: claim bead b")

	return wtPath
}

// writeFile is a small helper for fixture writes.
func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil { //nolint:gosec // G306 test fixture
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// TestDiscardDirtyBeadsLedger_AllowsRebase verifies the regression: a worktree
// whose .beads/issues.jsonl is dirty (uncommitted) can rebase onto main after
// discardDirtyBeadsLedger runs. Without the fix, `git rebase main` aborts with
// "cannot rebase: You have unstaged changes".
func TestDiscardDirtyBeadsLedger_AllowsRebase(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Dirty the worktree's ledger (simulates the daemon's own br flush landing
	// in the per-worktree JSONL during the run).
	writeFile(t, filepath.Join(wtPath, ".beads", "issues.jsonl"),
		`{"id":"a","status":"open"}`+"\n"+`{"id":"c","status":"closed"}`+"\n")

	// Sanity: the worktree is dirty in the ledger, so a rebase would refuse.
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, ".beads/issues.jsonl") {
		t.Fatalf("precondition: expected dirty .beads/issues.jsonl; got status:\n%s", status)
	}

	// Apply the fix.
	daemon.ExportedDiscardDirtyBeadsLedger(context.Background(), wtPath)

	// The worktree must now be clean.
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after discardDirtyBeadsLedger: expected clean worktree; got:\n%s", status)
	}

	// And the rebase must now succeed.
	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, err := rebaseCmd.CombinedOutput(); err != nil {
		t.Fatalf("git rebase main after cleanup: %v\n%s", err, out)
	}
}

// TestDiscardDirtyBeadsLedger_PreservesOtherDirtyFiles verifies the fix is
// narrow: it only discards .beads/issues.jsonl and leaves other uncommitted
// changes intact (those must still surface as a rebase failure rather than be
// silently reset — an implementer that escaped its worktree must fail loudly).
func TestDiscardDirtyBeadsLedger_PreservesOtherDirtyFiles(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Dirty both the ledger AND a real code file.
	writeFile(t, filepath.Join(wtPath, ".beads", "issues.jsonl"),
		`{"id":"a","status":"open"}`+"\n"+`{"id":"c","status":"closed"}`+"\n")
	writeFile(t, filepath.Join(wtPath, "code.txt"), "code\nagent work\nUNCOMMITTED EDIT\n")

	daemon.ExportedDiscardDirtyBeadsLedger(context.Background(), wtPath)

	status := dirtyLedgerGit(t, wtPath, "status", "--porcelain")
	if strings.Contains(status, ".beads/issues.jsonl") {
		t.Errorf("discardDirtyBeadsLedger should have restored the ledger; status:\n%s", status)
	}
	if !strings.Contains(status, "code.txt") {
		t.Errorf("discardDirtyBeadsLedger must NOT touch other dirty files; expected code.txt dirty, got:\n%s", status)
	}
}

// TestDiscardDirtyBeadsLedger_NoOpOnCleanWorktree verifies the helper is a no-op
// when the ledger is already clean (no spurious git writes / errors).
func TestDiscardDirtyBeadsLedger_NoOpOnCleanWorktree(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	before := dirtyLedgerGit(t, wtPath, "status", "--porcelain")
	daemon.ExportedDiscardDirtyBeadsLedger(context.Background(), wtPath)
	after := dirtyLedgerGit(t, wtPath, "status", "--porcelain")

	if before != after {
		t.Errorf("no-op expected on clean worktree; before=%q after=%q", before, after)
	}
}
