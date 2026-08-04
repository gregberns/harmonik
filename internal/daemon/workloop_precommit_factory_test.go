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

// workloopFixturePreCommitWorktreeFactory wraps the production worktree factory
// and creates a dummy commit in the worktree so that HEAD advances past the
// parent SHA. This satisfies the no-commit guard (hk-mmh8f, workloop.go).
//
// Do NOT wire it as TestRuntimeParams.WorktreeFactory in a work-loop test. The
// factory runs BEFORE the agent launches, and the graph node reads the worktree
// HEAD just before that launch and keeps it as the node baseline. A commit made
// here is already in the baseline, so the node's no-advance guard correctly
// refuses a run whose agent produced nothing. Use
// workloopFixtureAdvanceHeadHandlerArgs instead, which commits during the run.
// The same lesson is written up on mergeToMainCommittingFactory.
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

// workloopFixtureAdvanceHeadHandlerArgs returns the `/bin/sh -c` argument pair
// for a fake agent that advances HEAD by one empty commit while it runs, then
// exits 0.
//
// Wire it as TestRuntimeParams.HandlerArgs and leave WorktreeFactory nil so the
// production factory cuts the worktree. It models what a real implementer does:
// it commits DURING its run, so the commit lands after the node baseline is
// read. A fixture that commits in the worktree factory models an agent that
// never existed, because no implementer produces its commit before it starts.
//
// The commit is --allow-empty rather than a file. A file-based commit makes
// `D <filename>` appear in `git status` between update-ref and reset --hard in
// mergeRunBranchToMain, and checkMainWorkingTreeDirty then false-positives for
// any other bead running at the same time. An empty commit advances HEAD, which
// is all the guard asks for, and touches no path.
//
// git is called by absolute path because handler.Launch replaces the child
// environment with LaunchSpec.Env, which carries no PATH. The script sends its
// own stdout to stderr, because the handler contract reads the child's stdout as
// an NDJSON event stream and git chatter there reads as a malformed line.
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
