package workspace

// gitignorehygiene_hk3edb1_restore_test.go — regression guard for hk-3edb1:
// EnsureGitignoreHygiene commits the daemon-state .gitignore on the dedicated
// harmonik/gitignore-init branch, then MUST restore the operator's ORIGINAL HEAD.
// Before the fix, gitignoreCommit checked out the dedicated branch and never
// switched back, so — wired per its own doc ("call BEFORE creating any worktree"
// at daemon startup) — it would park the operator checkout on the daemon branch.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHK3edb1_EnsureGitignoreHygiene_RestoresOriginalBranch runs the hygiene from
// a NON-main operator branch and asserts HEAD is restored to THAT branch (not a
// hardcoded "main"), the commit lands on the dedicated branch, the operator
// branch tip is untouched, and the required entries remain in the working tree.
func TestHK3edb1_EnsureGitignoreHygiene_RestoresOriginalBranch(t *testing.T) {
	repo, _ := tempRepo(t)

	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// Put the operator on a distinctive non-main branch.
	const operatorBranch = "operator/feature-x"
	git("checkout", "-b", operatorBranch)
	operatorTipBefore := git("rev-parse", operatorBranch)

	// Absent .gitignore → all entries missing → a commit is required.
	if err := os.Remove(filepath.Join(repo, ".gitignore")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove .gitignore: %v", err)
	}

	if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
		t.Fatalf("EnsureGitignoreHygiene: %v", err)
	}

	// (1) HEAD restored to the ORIGINAL operator branch — proves the restore
	// targets the captured branch, not a hardcoded "main".
	if got := git("rev-parse", "--abbrev-ref", "HEAD"); got != operatorBranch {
		t.Errorf("HEAD = %q after hygiene; want restored to %q (hk-3edb1)", got, operatorBranch)
	}

	// (2) The dedicated branch carries the hygiene commit.
	if subj := git("log", "-1", "--format=%s", GitignoreBranchName); !strings.Contains(subj, "WM-013e") {
		t.Errorf("dedicated-branch tip subject = %q; want the WM-013e hygiene commit", subj)
	}

	// (3) The operator branch tip is unchanged — no daemon-state commit injected.
	if got := git("rev-parse", operatorBranch); got != operatorTipBefore {
		t.Errorf("operator branch tip changed: before=%s after=%s; hygiene commit leaked onto the operator branch",
			operatorTipBefore, got)
	}

	// (4) Required entries remain in the operator's working tree (uncommitted) so
	// daemon control-plane state stays ignored despite the commit living only on
	// the dedicated branch.
	data, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore after hygiene: %v", err)
	}
	for _, entry := range RequiredGitignoreEntries {
		if !gitignoreEntryPresent(string(data), entry) {
			t.Errorf("working-tree .gitignore missing %q after HEAD restore", entry)
		}
	}
}

// TestHK3edb1_EnsureGitignoreHygiene_RestoresDetachedHead covers the detached-HEAD
// branch of the restore: gitignoreCapturedHead falls back to the commit SHA and
// restoreGitignoreHead re-detaches onto it, rather than coercing HEAD onto a
// branch (hk-3edb1).
func TestHK3edb1_EnsureGitignoreHygiene_RestoresDetachedHead(t *testing.T) {
	repo, _ := tempRepo(t)

	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// Detach HEAD onto the current commit.
	headSHA := git("rev-parse", "HEAD")
	git("checkout", "--detach", headSHA)
	if git("rev-parse", "--abbrev-ref", "HEAD") != "HEAD" {
		t.Fatalf("precondition: HEAD is not detached")
	}

	if err := os.Remove(filepath.Join(repo, ".gitignore")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove .gitignore: %v", err)
	}

	if err := EnsureGitignoreHygiene(t.Context(), repo); err != nil {
		t.Fatalf("EnsureGitignoreHygiene: %v", err)
	}

	// HEAD must remain detached on the ORIGINAL commit (not coerced onto a branch).
	if got := git("rev-parse", "--abbrev-ref", "HEAD"); got != "HEAD" {
		t.Errorf("HEAD = %q after hygiene; want to remain detached (hk-3edb1 detached round-trip)", got)
	}
	if got := git("rev-parse", "HEAD"); got != headSHA {
		t.Errorf("detached HEAD moved: got %s, want original %s", got, headSHA)
	}

	// The dedicated branch still carries the hygiene commit, and the working-tree
	// entries are re-materialized.
	if subj := git("log", "-1", "--format=%s", GitignoreBranchName); !strings.Contains(subj, "WM-013e") {
		t.Errorf("dedicated-branch tip subject = %q; want the WM-013e hygiene commit", subj)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore after hygiene: %v", err)
	}
	for _, entry := range RequiredGitignoreEntries {
		if !gitignoreEntryPresent(string(data), entry) {
			t.Errorf("working-tree .gitignore missing %q after detached-HEAD restore", entry)
		}
	}
}
