package daemon_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func workloopFixturePreCommitWorktreeFactory(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
	if err != nil {
		return "", nil, err
	}
	touchFile := filepath.Join(wtPath, "test-advance-head-"+runID)
	if writeErr := os.WriteFile(touchFile, []byte("advance HEAD\n"), 0o644); writeErr != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("workloopFixturePreCommitWorktreeFactory: write: %w", writeErr)
	}
	for _, args := range [][]string{
		{"add", "test-advance-head-" + runID},
		{"commit", "-m", "test: advance HEAD past parent for " + runID},
	} {
		//nolint:gosec // G204: git args are test-internal literals
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = wtPath
		if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
			if cleanup != nil {
				cleanup()
			}
			return "", nil, fmt.Errorf("workloopFixturePreCommitWorktreeFactory: git %v: %v\n%s", args, cmdErr, out)
		}
	}
	return wtPath, cleanup, nil
}

func workloopFixtureAdvanceHeadHandlerArgs(t *testing.T) []string {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("workloopFixtureAdvanceHeadHandlerArgs: git not found on PATH: %v", err)
	}
	script := fmt.Sprintf(
		"exec 1>&2\nset -e\n%s commit -q --allow-empty -m 'test: agent advanced HEAD'\n",
		gitPath,
	)
	return []string{"-c", script}
}
