package lifecycle

import (
	"errors"
	"syscall"
)

type cliFixtureSentinelErr struct{ msg string }

func (e *cliFixtureSentinelErr) Error() string { return e.msg }

var errCLIFixtureNtmUnavailable = &cliFixtureSentinelErr{"ntm-unavailable"}

var errCLIFixtureOrchestratorAgentUnavailable = &cliFixtureSentinelErr{"orchestrator-agent-unavailable"}

func cliFixtureErrToExitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, errCLIFixtureNtmUnavailable) {
		return 22
	}
	if errors.Is(err, errCLIFixtureOrchestratorAgentUnavailable) {
		return 23
	}
	if got := plFixtureErrToExitCode(err); got != 1 {
		return got
	}
	if errors.Is(err, syscall.ENOENT) {
		return 22
	}
	return 1
}
