package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestWM018a_MergeNodeKind_Valid verifies that Valid() accepts the two declared
// MergeNodeKind constants and rejects anything else.
//
// Spec ref: workspace-model.md §4.5 WM-018a — two shapes only.
func TestWM018a_MergeNodeKind_Valid(t *testing.T) {
	t.Parallel()

	valid := []MergeNodeKind{MergeNodeKindNonAgentic, MergeNodeKindAgentic}
	for _, k := range valid {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			if !k.Valid() {
				t.Errorf("WM-018a MergeNodeKind.Valid: %q should be valid", k)
			}
		})
	}

	invalid := []MergeNodeKind{"", "unknown", "both", "none"}
	for _, k := range invalid {
		k := k
		t.Run("invalid/"+string(k), func(t *testing.T) {
			t.Parallel()
			if k.Valid() {
				t.Errorf("WM-018a MergeNodeKind.Valid: %q should be invalid", k)
			}
		})
	}
}

// TestWM018a_MergeNodeDispatch_Valid verifies that Valid() accepts well-formed
// MergeNodeDispatch and rejects incomplete or invalid ones.
//
// Spec ref: workspace-model.md §4.5 WM-018a.
func TestWM018a_MergeNodeDispatch_Valid(t *testing.T) {
	t.Parallel()

	daemonID := MergeIdentity{Name: "Harmonik Daemon", Email: "no-reply@harmonik.local"}
	agentID := MergeIdentity{Name: "Claude Agent", Email: "agent@harmonik.local"}

	t.Run("non-agentic-valid", func(t *testing.T) {
		t.Parallel()
		d := MergeNodeDispatch{
			Kind:      MergeNodeKindNonAgentic,
			Author:    daemonID,
			Committer: daemonID,
		}
		if !d.Valid() {
			t.Errorf("WM-018a MergeNodeDispatch.Valid: non-agentic should be valid; %+v", d)
		}
	})

	t.Run("agentic-valid", func(t *testing.T) {
		t.Parallel()
		d := MergeNodeDispatch{
			Kind:      MergeNodeKindAgentic,
			Author:    agentID,
			Committer: daemonID,
		}
		if !d.Valid() {
			t.Errorf("WM-018a MergeNodeDispatch.Valid: agentic should be valid; %+v", d)
		}
	})

	t.Run("invalid-kind", func(t *testing.T) {
		t.Parallel()
		d := MergeNodeDispatch{
			Kind:      "unknown",
			Author:    daemonID,
			Committer: daemonID,
		}
		if d.Valid() {
			t.Error("WM-018a MergeNodeDispatch.Valid: invalid kind should be invalid")
		}
	})

	t.Run("empty-author-name", func(t *testing.T) {
		t.Parallel()
		d := MergeNodeDispatch{
			Kind:      MergeNodeKindNonAgentic,
			Author:    MergeIdentity{Name: "", Email: "x@x"},
			Committer: daemonID,
		}
		if d.Valid() {
			t.Error("WM-018a MergeNodeDispatch.Valid: empty author name should be invalid")
		}
	})

	t.Run("empty-committer-email", func(t *testing.T) {
		t.Parallel()
		d := MergeNodeDispatch{
			Kind:      MergeNodeKindNonAgentic,
			Author:    daemonID,
			Committer: MergeIdentity{Name: "Daemon", Email: ""},
		}
		if d.Valid() {
			t.Error("WM-018a MergeNodeDispatch.Valid: empty committer email should be invalid")
		}
	})
}

// TestWM018a_DetectSquashMergeConflict_NoConflict verifies that
// DetectSquashMergeConflict returns HasConflict=false when the merge is clean.
//
// Spec ref: workspace-model.md §4.5 WM-018a — "a non-zero exit from
// `git merge --squash` OR the presence of conflict markers in
// `git status --porcelain` output MUST be treated as conflict entry per WM-020."
func TestWM018a_DetectSquashMergeConflict_NoConflict(t *testing.T) {
	t.Parallel()

	// Use mergeBackFixtureSetupTaskBranch (from mergedispatch_wm018a_test.go).
	repo, sha := mergeBackFixtureSetupTaskBranch(t,
		"0196b100-0000-7000-8000-00000028a001",
		[]string{"clean change"},
	)

	integPath := mergeBackFixtureMakeIntegWorktree(t, repo, sha, "integ-028a-noclash")
	taskBranch := "run/0196b100-0000-7000-8000-00000028a001"

	result, err := DetectSquashMergeConflict(integPath, taskBranch)
	if err != nil {
		t.Fatalf("WM-018a DetectConflict no-conflict: %v", err)
	}
	if result.HasConflict {
		t.Errorf("WM-018a DetectConflict no-conflict: HasConflict = true, want false (reason: %q)", result.Reason)
	}
}

// TestWM018a_DetectSquashMergeConflict_WithConflict verifies that
// DetectSquashMergeConflict returns HasConflict=true when the branches have
// conflicting changes to the same file.
//
// Spec ref: workspace-model.md §4.5 WM-018a — conflict detection.
func TestWM018a_DetectSquashMergeConflict_WithConflict(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196b100-0000-7000-8000-00000028a002"
	taskBranch := "run/" + runID
	taskPath := filepath.Join(repo, ".harmonik", "worktrees", runID)
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatalf("MkdirAll task: %v", err)
	}

	// Create task branch from sha: modify shared.txt with one value.
	gitRun := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	gitRun(repo, "worktree", "add", "-b", taskBranch, taskPath, sha)
	// Write conflicting change in task branch.
	if err := os.WriteFile(filepath.Join(taskPath, "shared.txt"), []byte("task version\n"), 0o644); err != nil {
		t.Fatalf("WriteFile task: %v", err)
	}
	gitRun(taskPath, "add", ".")
	gitRun(taskPath, "commit", "-m", "task: change shared.txt")

	// Create integration branch from sha: modify the same file with different content.
	integSuffix := "integ-028a-clash"
	integPath := filepath.Join(repo, ".harmonik", "worktrees", integSuffix)
	if err := os.MkdirAll(filepath.Dir(integPath), 0o755); err != nil {
		t.Fatalf("MkdirAll integ: %v", err)
	}
	integBranch := "harmonik/integration/" + integSuffix
	gitRun(repo, "worktree", "add", "-b", integBranch, integPath, sha)
	// Write different conflicting change in integration branch.
	if err := os.WriteFile(filepath.Join(integPath, "shared.txt"), []byte("integration version\n"), 0o644); err != nil {
		t.Fatalf("WriteFile integ: %v", err)
	}
	gitRun(integPath, "add", ".")
	gitRun(integPath, "commit", "-m", "integ: change shared.txt")

	// Now detect conflict: merging task branch into integ must conflict.
	result, err := DetectSquashMergeConflict(integPath, taskBranch)
	if err != nil {
		t.Fatalf("WM-018a DetectConflict with-conflict: %v", err)
	}
	if !result.HasConflict {
		t.Errorf("WM-018a DetectConflict with-conflict: HasConflict = false, want true")
	}
	if result.Reason == "" {
		t.Error("WM-018a DetectConflict with-conflict: Reason is empty, want non-empty")
	}
}

// TestWM018a_IsConflictMarker verifies the porcelain conflict-marker detection.
//
// Spec ref: workspace-model.md §4.5 WM-018a — "conflict markers in
// `git status --porcelain` output."
func TestWM018a_IsConflictMarker(t *testing.T) {
	t.Parallel()

	type tc struct {
		xy      string
		want    bool
		comment string
	}

	cases := []tc{
		{"UU", true, "both unmerged"},
		{"AU", true, "added by us, unmerged"},
		{"UD", true, "unmerged, deleted by them"},
		{"AA", true, "both added"},
		{"DD", true, "both deleted"},
		{"M ", false, "modified, staged"},
		{" M", false, "modified, unstaged"},
		{"A ", false, "added, staged"},
		{"?? ", false, "untracked (3 chars — won't reach conflict check)"},
		{"", false, "empty string"},
		{"U", false, "single char — too short"},
	}

	for _, c := range cases {
		c := c
		t.Run(c.xy+"/"+c.comment, func(t *testing.T) {
			t.Parallel()
			got := isConflictMarker(c.xy)
			if got != c.want {
				t.Errorf("isConflictMarker(%q): got %v, want %v (%s)", c.xy, got, c.want, c.comment)
			}
		})
	}
}
