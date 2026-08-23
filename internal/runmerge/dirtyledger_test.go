package runmerge_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/runmerge"
)

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
	claudeDir := filepath.Join(mainRepo, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil { //nolint:gosec // G301 test fixture
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	writeFile(t, filepath.Join(claudeDir, "settings.json"), `{"hooks":{}}`+"\n")
	writeFile(t, filepath.Join(mainRepo, "code.txt"), "code\n")
	dirtyLedgerGit(t, mainRepo, "add", "-A")
	dirtyLedgerGit(t, mainRepo, "commit", "-m", "init")
	baseSHA := dirtyLedgerGit(t, mainRepo, "rev-parse", "HEAD")

	wtPath := filepath.Join(t.TempDir(), "wt")
	dirtyLedgerGit(t, mainRepo, "worktree", "add", "-b", "runbranch", wtPath, baseSHA)

	writeFile(t, filepath.Join(wtPath, "code.txt"), "code\nagent work\n")
	dirtyLedgerGit(t, wtPath, "add", "code.txt")
	dirtyLedgerGit(t, wtPath, "commit", "-m", "agent work")

	writeFile(t, ledgerPath, `{"id":"a","status":"open"}`+"\n"+`{"id":"b","status":"in_progress"}`+"\n")
	dirtyLedgerGit(t, mainRepo, "add", ".beads/issues.jsonl")
	dirtyLedgerGit(t, mainRepo, "commit", "-m", "daemon: claim bead b")

	return wtPath
}

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

	writeFile(t, filepath.Join(wtPath, ".beads", "issues.jsonl"),
		`{"id":"a","status":"open"}`+"\n"+`{"id":"c","status":"closed"}`+"\n")

	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, ".beads/issues.jsonl") {
		t.Fatalf("precondition: expected dirty .beads/issues.jsonl; got status:\n%s", status)
	}

	runmerge.DiscardDirtyChurn(context.Background(), wtPath)

	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after discardDirtyChurn: expected clean worktree; got:\n%s", status)
	}

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

	writeFile(t, filepath.Join(wtPath, ".claude", "settings.json"),
		`{"hooks":{"Stop":[{"matcher":"","hooks":[{"type":"command","command":"harmonik"}]}]},"permissions":{"allow":["Read","Write"]}}`+"\n")

	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, ".claude/settings.json") {
		t.Fatalf("precondition: expected dirty .claude/settings.json; got status:\n%s", status)
	}

	runmerge.DiscardDirtyChurn(context.Background(), wtPath)

	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); status != "" {
		t.Fatalf("after discardDirtyChurn: expected clean worktree; got:\n%s", status)
	}

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

	writeFile(t, filepath.Join(wtPath, ".beads", "issues.jsonl"),
		`{"id":"a","status":"open"}`+"\n"+`{"id":"c","status":"closed"}`+"\n")
	writeFile(t, filepath.Join(wtPath, ".claude", "settings.json"),
		`{"hooks":{},"permissions":{"allow":["Read"]}}`+"\n")
	writeFile(t, filepath.Join(wtPath, "code.txt"), "code\nagent work\nUNCOMMITTED EDIT\n")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath)

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

	rebaseCmd := exec.CommandContext(t.Context(), "git", "rebase", "main")
	rebaseCmd.Dir = wtPath
	out, err := rebaseCmd.CombinedOutput()
	if err == nil {
		t.Fatalf("rebase should have FAILED with a non-churn dirty file present; it succeeded:\n%s", out)
	}
	if !strings.Contains(string(out), "unstaged changes") {
		t.Errorf("expected 'unstaged changes' rebase abort; got: %v\n%s", err, out)
	}
	abortCmd := exec.CommandContext(t.Context(), "git", "rebase", "--abort")
	abortCmd.Dir = wtPath
	if out, abortErr := abortCmd.CombinedOutput(); abortErr != nil &&
		!strings.Contains(string(out), "no rebase in progress") {
		t.Errorf("git rebase --abort: %v\n%s", abortErr, out)
	}
}

// TestDiscardDirtyChurn_NoOpOnCleanWorktree verifies the helper is a no-op
// when no churn paths are dirty (no spurious git writes / errors).
func TestDiscardDirtyChurn_NoOpOnCleanWorktree(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)

	before := dirtyLedgerGit(t, wtPath, "status", "--porcelain")
	runmerge.DiscardDirtyChurn(context.Background(), wtPath)
	after := dirtyLedgerGit(t, wtPath, "status", "--porcelain")

	if before != after {
		t.Errorf("no-op expected on clean worktree; before=%q after=%q", before, after)
	}
}
