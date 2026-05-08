package lifecycle

import (
	"errors"
	"syscall"
)

// cliFixtureSentinelErr is the base type for CLI-fixture sentinel errors.
// These are never surfaced from real OS calls; they are test-only stand-ins
// for the conditions described by ON §8 codes 22 and 23.
type cliFixtureSentinelErr struct{ msg string }

func (e *cliFixtureSentinelErr) Error() string { return e.msg }

// errCLIFixtureNtmUnavailable is the sentinel error for the ntm-unavailable
// condition (ON §8 code 22). It simulates the failure path of PL-021a:
// ntm not on PATH, version-incompatible, or tmux missing.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — ntm absence → ON §8 code 22.
// Spec ref: operator-nfr.md §8, code 22 — ntm-unavailable.
var errCLIFixtureNtmUnavailable = &cliFixtureSentinelErr{"ntm-unavailable"}

// errCLIFixtureOrchestratorAgentUnavailable is the sentinel error for the
// orchestrator-agent-unavailable condition (ON §8 code 23). It simulates the
// failure path of PL-028 step 4: `harmonik runner --orchestrator-agent` cannot
// locate Claude Code.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 step 4 — Claude Code not
// found → ON §8 code 23.
// Spec ref: operator-nfr.md §8, code 23 — orchestrator-agent-unavailable.
var errCLIFixtureOrchestratorAgentUnavailable = &cliFixtureSentinelErr{"orchestrator-agent-unavailable"}

// cliFixtureErrToExitCode maps errors to ON §8 exit codes for the CLI-surface
// fixture. It extends plFixtureErrToExitCode with codes 22 and 23 that are
// specific to the runner and daemon entry points.
//
// Code table (partial — only codes exercised by CLI-surface tests):
//
//	0  — success (nil)
//	5  — pidfile-locked (EWOULDBLOCK / EAGAIN via plFixtureErrToExitCode)
//	6  — socket-bind-failed (EADDRINUSE via plFixtureErrToExitCode)
//	22 — ntm-unavailable
//	23 — orchestrator-agent-unavailable
//
// Spec ref: process-lifecycle.md §4.1 PL-008a — exit-code consumption from ON §8.
// Spec ref: operator-nfr.md §8 — authoritative exit code taxonomy.
func cliFixtureErrToExitCode(err error) int {
	if err == nil {
		return 0
	}
	// Code 22: ntm-unavailable — ntm missing/version-incompatible/tmux absent.
	if errors.Is(err, errCLIFixtureNtmUnavailable) {
		return 22
	}
	// Code 23: orchestrator-agent-unavailable — Claude Code not found.
	if errors.Is(err, errCLIFixtureOrchestratorAgentUnavailable) {
		return 23
	}
	// Delegate to the shared plFixtureErrToExitCode for codes 5 and 6.
	if got := plFixtureErrToExitCode(err); got != 1 {
		return got
	}
	// Code 22 also covers any syscall path where ntm probe fails with ENOENT
	// (binary not on PATH). Map that explicitly.
	if errors.Is(err, syscall.ENOENT) {
		return 22
	}
	return 1
}
