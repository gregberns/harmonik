package daemon_test

// mergetomain_dirtyledger_hk3yz2d_test.go — regression test for the pre-rebase
// churn-cleanup step added in hk-3yz2d and generalized in hk-aiw63.
//
// Bug (hk-3yz2d): a live DOT-mode run walked the whole graph correctly but
// FAILED at the final merge-to-main with:
//
//	rebase_conflict: exit status 1
//	error: cannot rebase: You have unstaged changes.
//	error: Please commit or stash them.
//
// The first unstaged change was .beads/issues.jsonl — a TRACKED file that the
// daemon's own `br` operations dirty (the shared SQLite DB flushes to the
// per-worktree JSONL during a run). `git rebase main` refuses to start when the
// worktree has unstaged changes, so the merge failed even though the agent
// produced a clean, committed run-branch.
//
// Bug (hk-aiw63): the IDENTICAL error persisted after the hk-3yz2d ledger fix.
// The remaining culprit was .claude/settings.json — also a TRACKED file (the
// root .gitignore covers only /.claude/worktrees/). The daemon's per-launch
// MaterializeClaudeSettings (CHB-001..005) merges the hook-bridge entries +
// permissions.allow into the worktree copy, and claude itself may further mutate
// it, leaving it modified-but-unstaged. hk-3yz2d was deliberately narrow (ledger
// only), so settings.json still aborted the rebase.
//
// The fix discards ALL UNCOMMITTED churn matching isHarmonikChurn before the
// rebase (discardDirtyChurn) — the .beads ledger AND .claude/* — while leaving
// any NON-churn dirty file untouched so a genuine implementer escape still fails
// the rebase loudly (hk-i1n7j safety property).
//
// This is GENERAL (affects all workflow modes), not DOT-specific: every mode
// routes its success path through mergeRunBranchToMain → the same in-worktree
// `git rebase main`.
//
// Spec ref: specs/execution-model.md §4.12 EM-052 step 2.
// Beads: hk-3yz2d, hk-aiw63.

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
	// Commit a TRACKED .claude/settings.json so tests can dirty it the way a
	// real run does (this repo tracks the file; the daemon's per-launch
	// MaterializeClaudeSettings then mutates it). hk-aiw63.
	claudeDir := filepath.Join(mainRepo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil { //nolint:gosec // G301 test fixture
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	writeFile(t, filepath.Join(claudeDir, "settings.json"), `{"hooks":{}}`+"\n")
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

// TestDiscardDirtyChurn_AllowsRebase verifies the regression: a worktree
// whose .beads/issues.jsonl is dirty (uncommitted) can rebase onto main after
// discardDirtyChurn runs. Without the fix, `git rebase main` aborts with
// "cannot rebase: You have unstaged changes".
func TestDiscardDirtyChurn_AllowsRebase(t *testing.T) {
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
	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)

	// The worktree must now be clean.
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after discardDirtyChurn: expected clean worktree; got:\n%s", status)
	}

	// And the rebase must now succeed.
	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, err := rebaseCmd.CombinedOutput(); err != nil {
		t.Fatalf("git rebase main after cleanup: %v\n%s", err, out)
	}
}

// TestDiscardDirtyChurn_DiscardsClaudeSettings is the hk-aiw63 regression: a
// dirty TRACKED .claude/settings.json (mutated by the per-launch
// MaterializeClaudeSettings / claude itself) is restored so the rebase proceeds.
// Before hk-aiw63, discardDirtyBeadsLedger handled only the ledger, so this
// blocked every real merge-to-main where claude touched settings.
func TestDiscardDirtyChurn_DiscardsClaudeSettings(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Dirty ONLY .claude/settings.json (the ledger stays clean here — this
	// isolates the hk-aiw63 culprit from the hk-3yz2d one).
	writeFile(t, filepath.Join(wtPath, ".claude", "settings.json"),
		`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"harmonik"}]}]},"permissions":{"allow":["Read","Write"]}}`+"\n")

	// Sanity: settings.json is dirty, so a rebase would refuse.
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, ".claude/settings.json") {
		t.Fatalf("precondition: expected dirty .claude/settings.json; got status:\n%s", status)
	}

	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)

	// The worktree must now be clean.
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after discardDirtyChurn: expected clean worktree; got:\n%s", status)
	}

	// And the rebase must now succeed.
	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	if out, err := rebaseCmd.CombinedOutput(); err != nil {
		t.Fatalf("git rebase main after cleanup: %v\n%s", err, out)
	}
}

// TestDiscardDirtyChurn_PreservesOtherDirtyFiles verifies the fix stays
// DISCRIMINATING: it discards every churn-allowlisted dirty path (the ledger AND
// .claude/settings.json) but leaves a NON-churn dirty file (real code) intact —
// that must still surface as a rebase failure rather than be silently reset, so
// an implementer that escaped its worktree fails loudly (hk-i1n7j safety
// property, preserved across the hk-aiw63 generalization).
func TestDiscardDirtyChurn_PreservesOtherDirtyFiles(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	// Dirty BOTH churn paths AND a real code file.
	writeFile(t, filepath.Join(wtPath, ".beads", "issues.jsonl"),
		`{"id":"a","status":"open"}`+"\n"+`{"id":"c","status":"closed"}`+"\n")
	writeFile(t, filepath.Join(wtPath, ".claude", "settings.json"),
		`{"hooks":{},"permissions":{"allow":["Read"]}}`+"\n")
	writeFile(t, filepath.Join(wtPath, "code.txt"), "code\nagent work\nUNCOMMITTED EDIT\n")

	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)

	status := dirtyLedgerGit(t, wtPath, "status", "--porcelain")
	if strings.Contains(status, ".beads/issues.jsonl") {
		t.Errorf("discardDirtyChurn should have restored the ledger; status:\n%s", status)
	}
	if strings.Contains(status, ".claude/settings.json") {
		t.Errorf("discardDirtyChurn should have restored .claude/settings.json; status:\n%s", status)
	}
	if !strings.Contains(status, "code.txt") {
		t.Errorf("discardDirtyChurn must NOT touch non-churn dirty files; expected code.txt dirty, got:\n%s", status)
	}

	// A genuine implementer escape (code.txt dirty) must STILL block the rebase.
	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	out, err := rebaseCmd.CombinedOutput()
	if err == nil {
		t.Fatalf("rebase should have FAILED with a non-churn dirty file present; it succeeded:\n%s", out)
	}
	if !strings.Contains(string(out), "unstaged changes") {
		t.Errorf("expected 'unstaged changes' rebase abort; got: %v\n%s", err, out)
	}
	// Clean up the aborted rebase state so the worktree is not left mid-rebase.
	abortCmd := exec.CommandContext(t.Context(), "git", "rebase", "--abort")
	abortCmd.Dir = wtPath
	_ = abortCmd.Run()
}

// TestDiscardDirtyChurn_NoOpOnCleanWorktree verifies the helper is a no-op
// when no churn paths are dirty (no spurious git writes / errors).
func TestDiscardDirtyChurn_NoOpOnCleanWorktree(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	before := dirtyLedgerGit(t, wtPath, "status", "--porcelain")
	daemon.ExportedDiscardDirtyChurn(context.Background(), wtPath)
	after := dirtyLedgerGit(t, wtPath, "status", "--porcelain")

	if before != after {
		t.Errorf("no-op expected on clean worktree; before=%q after=%q", before, after)
	}
}
