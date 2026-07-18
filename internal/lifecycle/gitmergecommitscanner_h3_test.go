package lifecycle

// gitmergecommitscanner_h3_test.go — H3 regression: HasMergeCommitForBead must
// report the change STILL PRESENT, not merely that a commit once carried the
// Harmonik-Bead-ID trailer. A reverted (or otherwise superseded) commit means the
// bead's work is gone from the tree, so it MUST NOT be reported as subsumed —
// otherwise the Cat 3c reconciler auto-closes a bead whose work was undone.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// h3GitRepo initialises a throwaway git repo on branch main with one base commit.
func h3GitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	run("commit", "--allow-empty", "-m", "base")
	return dir
}

func h3CommitWithTrailer(t *testing.T, dir string, beadID core.BeadID) {
	t.Helper()
	// Write real content so the commit has a diff that a later `git revert` can
	// undo (an empty commit cannot be reverted — "nothing to commit").
	fname := filepath.Join(dir, "work-"+string(beadID)+".txt")
	if err := os.WriteFile(fname, []byte("work for "+string(beadID)+"\n"), 0o644); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd := exec.Command("git", "commit", "-m",
		"feat: work\n\nHarmonik-Bead-ID: "+string(beadID))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit trailer: %v\n%s", err, out)
	}
}

func TestH3_HasMergeCommitForBead_PresentUnreverted_True(t *testing.T) {
	t.Parallel()
	dir := h3GitRepo(t)
	bid := core.BeadID("hk-present")
	h3CommitWithTrailer(t, dir, bid)

	s := GitMergeCommitScanner{ProjectDir: dir, TargetBranch: "main"}
	got, err := s.HasMergeCommitForBead(context.Background(), bid)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: %v", err)
	}
	if !got {
		t.Errorf("HasMergeCommitForBead = false; want true (trailer commit present, not reverted)")
	}
}

func TestH3_HasMergeCommitForBead_Reverted_False(t *testing.T) {
	t.Parallel()
	dir := h3GitRepo(t)
	bid := core.BeadID("hk-reverted")
	h3CommitWithTrailer(t, dir, bid)

	// Find the trailer commit SHA and revert it.
	shaCmd := exec.Command("git", "rev-parse", "HEAD")
	shaCmd.Dir = dir
	shaOut, err := shaCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	revCmd := exec.Command("git", "revert", "--no-edit", string(shaOut[:len(shaOut)-1]))
	revCmd.Dir = dir
	if out, revErr := revCmd.CombinedOutput(); revErr != nil {
		t.Fatalf("git revert: %v\n%s", revErr, out)
	}

	s := GitMergeCommitScanner{ProjectDir: dir, TargetBranch: "main"}
	got, err := s.HasMergeCommitForBead(context.Background(), bid)
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: %v", err)
	}
	if got {
		t.Errorf("HasMergeCommitForBead = true; want false (trailer commit was reverted — work no longer present)")
	}
}

func TestH3_HasMergeCommitForBead_NoCommit_False(t *testing.T) {
	t.Parallel()
	dir := h3GitRepo(t)

	s := GitMergeCommitScanner{ProjectDir: dir, TargetBranch: "main"}
	got, err := s.HasMergeCommitForBead(context.Background(), core.BeadID("hk-absent"))
	if err != nil {
		t.Fatalf("HasMergeCommitForBead: %v", err)
	}
	if got {
		t.Errorf("HasMergeCommitForBead = true; want false (no trailer commit exists)")
	}
}
