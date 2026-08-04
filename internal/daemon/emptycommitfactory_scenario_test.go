//go:build scenario

package daemon_test

// emptycommitfactory_scenario_test.go — the empty-commit worktree factory,
// which only the scenario tier still uses.
//
// It lived in run_w3cp1_boiwe_hiqrl_test.go, which carries no build tag. The
// fixture repair that stopped the work-loop tests committing inside their
// factory removed the last untagged caller, so under the default build the
// helper compiled and nothing called it. `unused` reports that, and it is
// right: a helper that is dead in the build you run every day is dead code.
//
// Every remaining caller is behind //go:build scenario, so the helper now sits
// behind the same tag. That keeps it compiled exactly when its callers are.

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/gregberns/harmonik/internal/daemon"
)

// emptyCommitWorktreeFactory wraps productionWorktreeFactory and creates an
// allow-empty commit so HEAD advances past headSHA (satisfying the no-commit
// guard, hk-mmh8f) without adding any files to the working tree.
//
// Using --allow-empty avoids the race in concurrent-bead tests: a file-based
// commit causes `D <filename>` to appear in `git status` between `update-ref`
// and `reset --hard` in mergeRunBranchToMain, which triggers a false positive
// in checkMainWorkingTreeDirty for any other concurrently-running bead.
func emptyCommitWorktreeFactory(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
	if err != nil {
		return "", nil, err
	}
	//nolint:gosec // G204: git args are test-internal literals
	cmd := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "test: advance HEAD for "+runID)
	cmd.Dir = wtPath
	if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("emptyCommitWorktreeFactory: git commit: %v\n%s", cmdErr, out)
	}
	return wtPath, cleanup, nil
}
