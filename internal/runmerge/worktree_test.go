package runmerge_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/runmerge"
)

func TestRemoveWorktree_ReturnsFailedReclaim(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", filepath.Join(t.TempDir(), ".claude.json"))

	gitPath := filepath.Join(os.Getenv("PATH"), "git")
	const gitScript = "#!/bin/sh\nprintf '%s\\n' '.git is unreadable' >&2\nexit 7\n"
	//nolint:gosec // G306: the Git fixture must be executable
	if err := os.WriteFile(gitPath, []byte(gitScript), 0o700); err != nil {
		t.Fatalf("write git stub: %v", err)
	}

	err := runmerge.RemoveWorktree(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "worktree"))
	if err == nil {
		t.Fatal("RemoveWorktree returned nil after git worktree remove failed")
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("RemoveWorktree error does not preserve the git exit error: %v", err)
	}
	if got := exitErr.ExitCode(); got != 7 {
		t.Errorf("git worktree remove exit code = %d, want 7", got)
	}
}
