package workspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM003_CreateWorktree verifies that CreateWorktree issues
// `git worktree add -b <branch> <path> <parentCommit>` in the mandatory form
// required by workspace-model.md §4.1 WM-003.
//
// Spec ref: workspace-model.md §4.1 WM-003 — "The workspace manager MUST create
// a worktree and a fresh task branch atomically via `git worktree add -b <branch>
// <path> <parent_commit>` against the backing repository."
func TestWM003_CreateWorktree(t *testing.T) {
	t.Parallel()

	t.Run("creates-worktree-at-canonical-path", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7003-8a1b-2c3d4e5f0003"

		if err := CreateWorktree(t.Context(), repo, runID, sha, nil); err != nil {
			t.Fatalf("WM-003: CreateWorktree: %v", err)
		}

		// Canonical path per WM-002.
		wantPath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		if _, err := os.Stat(wantPath); os.IsNotExist(err) {
			t.Errorf("WM-003: worktree not found at canonical path %q", wantPath)
		}

		// Valid git worktree: must contain a .git file (worktree-gitdir link).
		dotGit := filepath.Join(wantPath, ".git")
		if _, err := os.Stat(dotGit); os.IsNotExist(err) {
			t.Errorf("WM-003: .git absent in worktree at %q", wantPath)
		}
	})

	t.Run("creates-task-branch-pinned-to-parent-commit", func(t *testing.T) {
		t.Parallel()

		// The -b form creates the task branch from the explicit parentCommit.
		// The branch tip MUST equal parentCommit.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7003-8a1b-2c3d4e5f0004"
		branch := TaskBranchName(runID)

		if err := CreateWorktree(t.Context(), repo, runID, sha, nil); err != nil {
			t.Fatalf("WM-003: CreateWorktree: %v", err)
		}

		// Confirm the task branch tip equals parentCommit.
		out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "rev-parse", branch).Output()
		if err != nil {
			t.Fatalf("WM-003: rev-parse %q: %v", branch, err)
		}
		branchTip := strings.TrimRight(string(out), "\n")
		if branchTip != sha {
			t.Errorf("WM-003: branch %q tip = %q, want parentCommit %q", branch, branchTip, sha)
		}
	})

	t.Run("parent-dir-created-if-absent", func(t *testing.T) {
		t.Parallel()

		// CreateWorktree must create <repo>/.harmonik/worktrees/ if absent.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7003-8a1b-2c3d4e5f0005"

		// Confirm the worktree root does NOT exist before the call.
		worktreeRoot := filepath.Join(repo, ".harmonik", "worktrees")
		if _, err := os.Stat(worktreeRoot); !os.IsNotExist(err) {
			t.Skip("worktree root already exists; cannot exercise MkdirAll branch")
		}

		if err := CreateWorktree(t.Context(), repo, runID, sha, nil); err != nil {
			t.Fatalf("WM-003: CreateWorktree with absent parent: %v", err)
		}

		if _, err := os.Stat(worktreeRoot); err != nil {
			t.Errorf("WM-003: worktree root %q not created: %v", worktreeRoot, err)
		}
	})

	t.Run("worktree-registered-in-git", func(t *testing.T) {
		t.Parallel()

		// git worktree list --porcelain must show the new worktree as registered.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7003-8a1b-2c3d4e5f0006"
		worktreePath := WorktreePath(repo, runID, nil)

		if err := CreateWorktree(t.Context(), repo, runID, sha, nil); err != nil {
			t.Fatalf("WM-003: CreateWorktree: %v", err)
		}

		out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "worktree", "list", "--porcelain").Output()
		if err != nil {
			t.Fatalf("WM-003: git worktree list: %v", err)
		}

		if !strings.Contains(string(out), worktreePath) {
			t.Errorf("WM-003: worktree %q not found in `git worktree list` output:\n%s", worktreePath, out)
		}
	})

	t.Run("invalid-parent-commit-returns-worktree-creation-error", func(t *testing.T) {
		t.Parallel()

		// Providing a bogus parent commit SHA causes git to fail; CreateWorktree
		// must return ErrWorktreeCreationFailed.
		repo, _ := tempRepo(t)
		runID := "0196a1b2-c3d4-7003-8a1b-2c3d4e5f0007"
		bogusCommit := "0000000000000000000000000000000000000000"

		err := CreateWorktree(t.Context(), repo, runID, bogusCommit, nil)
		if err == nil {
			t.Fatal("WM-003: expected error for bogus parentCommit, got nil")
		}
		if !errors.Is(err, ErrWorktreeCreationFailed) {
			t.Errorf("WM-003: expected ErrWorktreeCreationFailed, got: %v", err)
		}
	})

	t.Run("context-cancellation-propagates", func(t *testing.T) {
		t.Parallel()

		// A cancelled context must cause CreateWorktree to return an error.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7003-8a1b-2c3d4e5f0008"

		ctx, cancel := context.WithCancel(t.Context())
		cancel() // cancel immediately

		err := CreateWorktree(ctx, repo, runID, sha, nil)
		if err == nil {
			t.Fatal("WM-003: expected error on cancelled context, got nil")
		}
	})
}
