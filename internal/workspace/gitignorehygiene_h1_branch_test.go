package workspace

// gitignorehygiene_h1_branch_test.go — H1 regression: the .gitignore hygiene
// commit MUST land on the dedicated harmonik/gitignore-init branch, NEVER on the
// operator's working branch (main). Previously gitignoreCommit ran git add + git
// commit --allow-empty against current HEAD, injecting daemon-state onto main.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func h1GitOut(t *testing.T, repo string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", repo}, args...)
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// TestH1_GitignoreCommit_LandsOnDedicatedBranch verifies that after
// EnsureGitignoreHygiene adds missing entries, (1) the commit lands on
// harmonik/gitignore-init, (2) the operator's main branch tip is unchanged, and
// (3) HEAD is RESTORED to the operator's branch — never left parked on the
// dedicated branch (hk-3edb1).
func TestH1_GitignoreCommit_LandsOnDedicatedBranch(t *testing.T) {
	repo, _ := tempRepo(t)

	mainTipBefore := h1GitOut(t, repo, "rev-parse", "main")

	// Absent .gitignore → all entries missing → a commit is required.
	if err := os.Remove(filepath.Join(repo, ".gitignore")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove .gitignore: %v", err)
	}
	if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
		t.Fatalf("EnsureGitignoreHygiene: %v", err)
	}

	// HEAD must be RESTORED to the operator's branch (hk-3edb1), NOT left on the
	// dedicated branch. Before the fix, gitignoreCommit checked out
	// harmonik/gitignore-init and never switched back, parking the operator
	// checkout on the daemon-state branch.
	if branch := h1GitOut(t, repo, "rev-parse", "--abbrev-ref", "HEAD"); branch != "main" {
		t.Errorf("HEAD branch = %q; want %q (operator HEAD must be restored after the hygiene commit)", branch, "main")
	}

	// The dedicated branch tip must carry the hygiene commit (looked up by branch
	// name, since HEAD is no longer on it).
	subject := h1GitOut(t, repo, "log", "-1", "--format=%s", GitignoreBranchName)
	if !strings.Contains(subject, "WM-013e") {
		t.Errorf("dedicated-branch tip subject = %q; want the WM-013e hygiene commit", subject)
	}

	// The operator's main tip MUST be unchanged — no daemon-state commit injected.
	if mainTipAfter := h1GitOut(t, repo, "rev-parse", "main"); mainTipAfter != mainTipBefore {
		t.Errorf("main tip changed: before=%s after=%s; hygiene commit leaked onto operator branch", mainTipBefore, mainTipAfter)
	}

	// The required entries MUST still be present in the operator's working tree
	// (an uncommitted change) so daemon control-plane state stays ignored even
	// though the commit lives only on the dedicated branch (hk-3edb1).
	data, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore after hygiene: %v", err)
	}
	for _, entry := range RequiredGitignoreEntries {
		if !gitignoreEntryPresent(string(data), entry) {
			t.Errorf("working-tree .gitignore missing %q after HEAD restore; entries must be re-materialized", entry)
		}
	}
}

// TestH1_GitignoreCommit_NoEmptyCommit verifies the forced --allow-empty behavior
// is gone: running hygiene twice does not stack an empty commit on the second run.
func TestH1_GitignoreCommit_NoEmptyCommit(t *testing.T) {
	repo, _ := tempRepo(t)
	if err := os.Remove(filepath.Join(repo, ".gitignore")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove .gitignore: %v", err)
	}
	if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
		t.Fatalf("EnsureGitignoreHygiene (1): %v", err)
	}
	countAfterFirst := h1GitOut(t, repo, "rev-list", "--count", GitignoreBranchName)

	// A second call: all entries already present → idempotent no-op, no commit.
	if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
		t.Fatalf("EnsureGitignoreHygiene (2): %v", err)
	}
	countAfterSecond := h1GitOut(t, repo, "rev-list", "--count", GitignoreBranchName)
	if countAfterFirst != countAfterSecond {
		t.Errorf("commit count changed on idempotent re-run: %s → %s (an empty commit was stacked)", countAfterFirst, countAfterSecond)
	}
}
