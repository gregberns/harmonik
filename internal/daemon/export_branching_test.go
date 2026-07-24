package daemon

// export_branching_test.go — test-seam exports for internal/daemon branching.go
// (RT19.12 split of export_test.go). package daemon test file; see export_test.go
// header for the seam rationale. Bead: hk-ecrxy.

import (
	"context"
)

// ExportedBranchingConfig is the exported shape of BranchingConfig for tests.
//
// Bead ref: hk-oe6zt.
type ExportedBranchingConfig = BranchingConfig

// ExportedErrProjectBranchingConfig is a type alias for ErrProjectBranchingConfig
// so tests in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-umxx4.
type ExportedErrProjectBranchingConfig = ErrProjectBranchingConfig

// ExportedParseBranchingSection exposes parseBranchingSection for tests in
// package daemon_test. See branching.go for semantics.
//
// Bead ref: hk-oe6zt.
func ExportedParseBranchingSection(beadBody string) (BranchingConfig, error) {
	return parseBranchingSection(beadBody)
}

// ExportedResolveBranching exposes resolveBranching for tests in package daemon_test.
// See branching.go for semantics.
//
// Bead ref: hk-umxx4, hk-ncwb3.
func ExportedResolveBranching(ctx context.Context, beadBody, projectRoot, targetBranch string) (BranchingConfig, error) {
	return resolveBranching(ctx, beadBody, projectRoot, targetBranch)
}

// ExportedResolveParentCommit exposes resolveParentCommit for tests in package
// daemon_test. See branching.go for semantics.
//
// Bead ref: hk-oe6zt, hk-ncwb3.
func ExportedResolveParentCommit(ctx context.Context, repoRoot, beadID, beadBody, targetBranch string) (string, error) {
	return resolveParentCommit(ctx, repoRoot, beadID, beadBody, targetBranch)
}

// ExportedLandsOnProtectedError is a type alias for LandsOnProtectedError so
// tests in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-ncwb3.
type ExportedLandsOnProtectedError = LandsOnProtectedError

// ExportedCrossRepoUnsafeError is a type alias for CrossRepoUnsafeError so
// tests in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-xfuc.
type ExportedCrossRepoUnsafeError = CrossRepoUnsafeError

// ExportedIsInAllowedRepos exposes isInAllowedRepos for tests in package daemon_test.
//
// Bead ref: hk-xfuc.
func ExportedIsInAllowedRepos(targetRepo string, allowedRepos []string) bool {
	return isInAllowedRepos(targetRepo, allowedRepos)
}

// ExportedLandsOnRefError is a type alias for LandsOnRefError so tests in
// package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-icgp1.
type ExportedLandsOnRefError = LandsOnRefError

// ExportedResolveLandsOn exposes resolveLandsOn for tests in package daemon_test.
//
// Bead ref: hk-icgp1.
func ExportedResolveLandsOn(cfg BranchingConfig) string {
	return resolveLandsOn(cfg)
}

// ExportedLandTaskBranch exposes landTaskBranch for tests in package daemon_test.
//
// Bead ref: hk-icgp1.
func ExportedLandTaskBranch(ctx context.Context, repoRoot, mergeWorktreeDir, taskBranch, runID, beadID string, cfg BranchingConfig) error {
	return landTaskBranch(ctx, repoRoot, mergeWorktreeDir, taskBranch, runID, beadID, cfg)
}
