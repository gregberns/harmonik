package daemon

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

func beadWorkLandedOn(ctx context.Context, repoDir, branch string, beadID core.BeadID) bool {
	if repoDir == "" || branch == "" {
		return false
	}
	scanner := lifecycle.GitMergeCommitScanner{ProjectDir: repoDir, TargetBranch: branch}
	landed, err := scanner.HasMergeCommitForBead(ctx, beadID)
	if err != nil {
		return false
	}
	return landed
}
