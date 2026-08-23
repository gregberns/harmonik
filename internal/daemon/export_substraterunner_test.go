package daemon

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
