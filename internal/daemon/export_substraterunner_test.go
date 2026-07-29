package daemon

// export_substraterunner_test.go — the substrate-runner observer test seam.
//
// Formerly export_reviewloop_test.go, which also carried the runReviewLoop and
// rl* session-id shims. Those went with the review-loop driver (EM-015d); the
// runner observer stayed because it instruments newPerRunSubstrate on the DOT
// agentic launch path, which is very much alive.
//
// Bead: hk-ecrxy.

import (
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ExportedSetSubstrateRunnerObserver installs (or clears, with nil) the package
// test seam that captures the CommandRunner passed into newPerRunSubstrate at the
// DOT agentic launch site. Tests use this to assert the SUBSTRATE-spawn runner
// (distinct from the SPEC runner) is the real non-nil worker runner for a REMOTE
// run (hk-fxy9 / hk-538l).
func ExportedSetSubstrateRunnerObserver(f func(tmuxPkg.CommandRunner)) {
	substrateRunnerObserver = f
}
