package daemon

// export_reviewloop_test.go — reviewloop.go / reviewerharness test-seam exports.
//
// Split out of export_test.go (RT19.2, P2 E5 export_test.go split) so the
// reviewloop.go and reviewerharness_hkiv748.go shims live in one topic file.
// Same package (daemon), so every daemon_test caller resolves daemon.ExportedX
// byte-identically after the move.
//
// Bead: hk-ecrxy.

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ReviewLoopResultExported is the exported shape of reviewLoopResult for tests
// in package daemon_test. Fields mirror reviewLoopResult verbatim.
//
// Bead ref: hk-7om2q.20.
type ReviewLoopResultExported struct {
	Success          bool
	CompletionReason string
	Summary          string
	NeedsAttention   bool
}

// ExportedRunReviewLoop exposes runReviewLoop for tests in package daemon_test.
// The result is converted to ReviewLoopResultExported to avoid exporting the
// internal reviewLoopResult type.
//
// Bead ref: hk-7om2q.20.
func ExportedRunReviewLoop(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	wtPath string,
	parentSHA string,
) ReviewLoopResultExported {
	// nil runner ⇒ LOCAL run: byte-identical to the pre-remote-substrate path.
	env, rp, handles := runBundlesFromDeps(deps, runID)
	r := runReviewLoop(ctx, env, rp, handles, runID, beadID, "", "", wtPath, parentSHA, "", "", "", "", nil, "", "", "", "")
	return ReviewLoopResultExported{
		Success:          r.success,
		CompletionReason: string(r.completionReason),
		Summary:          r.summary,
		NeedsAttention:   r.needsAttention,
	}
}

// ExportedRunReviewLoopWithRunner exposes runReviewLoop with an explicit
// CommandRunner so tests can assert the remote (runner != nil) path threads the
// runner into the implementer/reviewer shared.LaunchCtx (hk-3sus).
func ExportedRunReviewLoopWithRunner(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	wtPath string,
	parentSHA string,
	runner tmuxPkg.CommandRunner,
) ReviewLoopResultExported {
	env, rp, handles := runBundlesFromDeps(deps, runID)
	r := runReviewLoop(ctx, env, rp, handles, runID, beadID, "", "", wtPath, parentSHA, "", "", "", "", runner, "", "", "", "")
	return ReviewLoopResultExported{
		Success:          r.success,
		CompletionReason: string(r.completionReason),
		Summary:          r.summary,
		NeedsAttention:   r.needsAttention,
	}
}

// ExportedSetSubstrateRunnerObserver installs (or clears, with nil) the package
// test seam that captures the CommandRunner passed into newPerRunSubstrate at the
// review-loop and DOT agentic launch sites. Tests use this to assert the
// SUBSTRATE-spawn runner (distinct from the SPEC runner) is the real non-nil
// worker runner for a REMOTE run (hk-fxy9 / hk-538l).
func ExportedSetSubstrateRunnerObserver(f func(tmuxPkg.CommandRunner)) {
	substrateRunnerObserver = f
}

// ExportedSynthesiseClaudeSessionID exposes rlSynthesiseClaudeSessionID for
// tests in package daemon_test.  Tests use this to verify the produced ID
// satisfies the tmux buffer-name regex (hk-lckbv).
func ExportedSynthesiseClaudeSessionID() string {
	return rlSynthesiseClaudeSessionID(substrate.SystemClock{})
}

// ExportedResolveIter1ClaudeSessionID exposes rlResolveIter1ClaudeSessionID for
// tests in package daemon_test (hk-za5mz). Verifies the iteration-1 session-id
// resolution order: interceptor id → real minted id → synthesis.
func ExportedResolveIter1ClaudeSessionID(interceptorID, realMintedID string) string {
	return rlResolveIter1ClaudeSessionID(substrate.SystemClock{}, interceptorID, realMintedID)
}
