package lifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func h3GitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	run("commit", "--allow-empty", "-m", "base")
	return dir
}

func h3CommitWithTrailer(t *testing.T, dir string, beadID core.BeadID) {
	t.Helper()
	fname := filepath.Join(dir, "work-"+string(beadID)+".txt")
	if err := os.WriteFile(fname, []byte("work for "+string(beadID)+"\n"), 0o600); err != nil {
		t.Fatalf("write work file: %v", err)
	}
	add := exec.CommandContext(t.Context(), "git", "add", ".")
	add.Dir = dir
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	cmd := exec.CommandContext(t.Context(), "git", "commit", "-m", //nolint:gosec // G204: static git args + test-controlled beadID, not user input
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

	shaCmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	shaCmd.Dir = dir
	shaOut, err := shaCmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	revCmd := exec.CommandContext(t.Context(), "git", "revert", "--no-edit", string(shaOut[:len(shaOut)-1])) //nolint:gosec // G204: static git args + test-derived SHA, not user input
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
