//go:build scenario

package daemon_test

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/gregberns/harmonik/internal/daemon"
)

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
