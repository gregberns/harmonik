package daemon

// export_substrate_test.go — tmux substrate / per-run-substrate test seams.
//
// Split out of export_test.go (RT19.7, P2 E5 export_test.go split) so the
// tmuxsubstrate.go and per-run-substrate shims (spawn-cap slots, crew session
// naming, session teardown, the newPerRunSubstrate constructors, runWait
// ctx-cancel driving, the srt argv-wrap shell-quote and remote stat seams) live
// in one topic file. Same package (daemon), so every daemon_test caller resolves
// daemon.ExportedX byte-identically after the move.
//
// Bead: hk-ecrxy.

import (
	"context"

	"github.com/gregberns/harmonik/internal/handler"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runlaunch"
)

// ExportedForceTeardownSession exposes runlaunch.ForceTeardownSession for the hk-68pvl
// worktree-teardown-ordering regression test.
func ExportedForceTeardownSession(sess handler.Session) {
	runlaunch.ForceTeardownSession(sess)
}

// ExportedSpawnSlotsInUse exposes the spawn-semaphore slots-in-use count of a
// substrate returned by NewTmuxSubstrate, for the hk-4l7zs slot-leak tests.
// Returns 0 when sub is not a *tmuxSubstrate or has no cap configured.
func ExportedSpawnSlotsInUse(sub handler.Substrate) int {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		return ts.SpawnSlotsInUse()
	}
	return 0
}

// ExportedSpawnCapSize exposes the non-terminal spawn-cap ceiling of a
// substrate returned by NewTmuxSubstrate, for the hk-omvan live-resize tests.
// Returns 0 when sub is not a *tmuxSubstrate or has no cap configured.
func ExportedSpawnCapSize(sub handler.Substrate) int {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		return ts.SpawnCapSize()
	}
	return 0
}

// ExportedSetSpawnCap exposes SetSpawnCap on a substrate returned by
// NewTmuxSubstrate, for the hk-omvan live-resize tests. No-op when sub is not
// a *tmuxSubstrate.
func ExportedSetSpawnCap(sub handler.Substrate, n int) {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		ts.SetSpawnCap(n)
	}
}

// ExportedCrewSessionName exposes the crewSessionName method of a substrate
// returned by NewTmuxSubstrate, for fleet-portability T2 naming tests (hk-ohd).
// Returns ("", nil) when sub is not a *tmuxSubstrate; otherwise propagates the
// (name, err) result — err is non-nil when no project hash is configured
// (hk-rmy1, slice C: the legacy "hk-crew-<name>" fallback was removed).
func ExportedCrewSessionName(sub handler.Substrate, crewName string) (string, error) {
	if ts, ok := sub.(*tmuxSubstrate); ok {
		return ts.crewSessionName(crewName)
	}
	return "", nil
}

// ExportedNewPerRunSubstrate wraps newPerRunSubstrate for tests in package
// daemon_test that need per-run pane isolation without importing the unexported
// type directly.
//
// Returns nil when sub is nil or is not a *tmuxSubstrate (matching
// newPerRunSubstrate semantics). Tests that call WriteLastPane on the returned
// value must call SpawnWindow first to capture the pane target.
//
// Passes "" for handlerBinary so agentCommandFragments defaults to
// livePaneCommandSubstrings, preserving the existing test behaviour.
//
// Bead ref: hk-jfh59, hk-vhped.
func ExportedNewPerRunSubstrate(sub handler.Substrate) handler.Substrate {
	prs := newPerRunSubstrate(sub, "", nil)
	if prs == nil {
		return nil
	}
	return prs
}

// ExportedStatTaskFileVia exposes statTaskFileVia for unit tests in package
// daemon_test.  The runner is used for remote stat checks (hk-hh5e); nil runner
// falls back to local os.Stat (same as statTaskFile).
//
// Bead: hk-hh5e.
func ExportedStatTaskFileVia(ctx context.Context, runner tmuxPkg.CommandRunner, path string) error {
	return statTaskFileVia(ctx, runner, path)
}

// ExportedRunWaitResult is the exported result of a runWait call for tests.
//
// Bead ref: hk-88nno.
type ExportedRunWaitResult struct {
	ExitCode int
}

// ExportedRunWaitWithDeadFn drives tmuxSubstrateSession.runWait through a
// forced ctx.Done() and returns the exit code recorded in outcome.
//
// pid is set on the session. deadFn replaces processDead for this call —
// pass a function that returns true to simulate a dead process, false for alive.
// The caller-supplied ctx is cancelled immediately after runWait is launched so
// that the ctx.Done() branch fires on the first select iteration.
//
// Bead ref: hk-88nno.
func ExportedRunWaitWithDeadFn(pid int, deadFn func(int) bool) ExportedRunWaitResult {
	sess := &tmuxSubstrateSession{
		adapter:       &noopTmuxAdapter{},
		handle:        "test-session:hk-88nno-win",
		pid:           pid,
		waitDone:      make(chan struct{}),
		isProcessDead: deadFn,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so ctx.Done() fires on the first select
	sess.runWait(ctx)
	return ExportedRunWaitResult{ExitCode: sess.outcome.ExitCode}
}

// ExportedShellQuoteArg exposes shellQuoteArg for unit tests in package daemon_test.
//
// Bead ref: hk-rpr6.
func ExportedShellQuoteArg(s string) string {
	return shellQuoteArg(s)
}

// ExportedNewPerRunSubstrateWithSandbox wraps newPerRunSubstrate and sets
// sandboxSpawn for tests exercising the srt argv-wrap path (hk-rlxgx).
//
// Returns nil when sub is nil or is not a *tmuxSubstrate (matching
// newPerRunSubstrate semantics).
//
// Bead: hk-rlxgx.
func ExportedNewPerRunSubstrateWithSandbox(sub handler.Substrate, cfg *SrtSpawnConfig) handler.Substrate {
	prs := newPerRunSubstrate(sub, "", nil)
	if prs == nil {
		return nil
	}
	prs.sandboxSpawn = cfg
	return prs
}
