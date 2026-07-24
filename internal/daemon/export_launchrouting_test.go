package daemon

// export_launchrouting_test.go — harnessregistry launch-routing test seams.
//
// Split out of export_test.go (RT19.11, P2 E5 export_test.go split) so the 8
// harnessregistry.go launch-routing builders (the harness-registry constructors,
// effective-model resolution, and the routed / observed-routed / pinned /
// pi-process-exit / codex-process-exit LaunchSpec builders) live in one topic
// file. These are the builders RT18.11 re-points, so isolating them shrinks that
// diff. Same package (daemon), so every daemon_test caller resolves
// daemon.ExportedX byte-identically after the move.
//
// Bead: hk-ecrxy.

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// ExportedNewHarnessRegistry exposes newHarnessRegistry for tests in package
// daemon_test. It returns the daemon's HarnessRegistry with ClaudeHarness
// registered for core.AgentTypeClaudeCode. Pi harness is registered with an
// empty PiHarnessConfig (test-only; Pi fields are non-empty only in production
// when harnesses.pi is configured).
//
// Bead ref: hk-hj9ld.
func ExportedNewHarnessRegistry() (*handlercontract.HarnessRegistry, error) {
	return newHarnessRegistry(projectconfig.PiHarnessConfig{})
}

// ExportedNewHarnessRegistryWithPi exposes newHarnessRegistry with a configured
// PiHarnessConfig for tests that verify the config→harness seam (hk-f8u5j).
func ExportedNewHarnessRegistryWithPi(piCfg projectconfig.PiHarnessConfig) (*handlercontract.HarnessRegistry, error) {
	return newHarnessRegistry(piCfg)
}

// ExportedEffectiveModel exposes effectiveModel for tests in package daemon_test.
// Bead ref: hk-7z6l8.
func ExportedEffectiveModel(h handlercontract.Harness, model string) string {
	return effectiveModel(h, shared.LaunchCtx{Model: model})
}

// ExportedPiProcessExitLaunchSpecBuilder returns a launchSpecBuilder that
// produces a handler.LaunchSpec for the provided shell script and stamps
// resolvedAgentType = core.AgentTypePi on the returned artifacts.
//
// Use in tests that exercise the hk-j6wm7 Pi retain-on-failure + stdout/stderr
// capture behaviour: wire an AdapterRegistry (empty or claude-only, so
// waitAgentReady is skipped for the shell fixture) and a HarnessRegistry from
// ExportedNewHarnessRegistry (which registers the Pi harness →
// SessionIDPolicy() == SessionIDCaptured → the exec path + StdoutWrapper fire),
// then supply this builder so beadRunOne resolves the run as a Pi run.
//
// Bead ref: hk-j6wm7.
func ExportedPiProcessExitLaunchSpecBuilder(scriptPath string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		spec := handler.LaunchSpec{
			Binary:  "/bin/sh",
			Args:    []string{scriptPath},
			WorkDir: rc.WorkspacePath,
			Role:    string(rc.Phase),
		}
		arts := shared.LaunchArtifacts{
			ResolvedAgentType: core.AgentTypePi,
		}
		return spec, arts, nil
	}
}

// ExportedCodexProcessExitLaunchSpecBuilder returns a launchSpecBuilder that
// produces a handler.LaunchSpec for the provided shell script and stamps
// resolvedAgentType = core.AgentTypeCodex on the returned artifacts.
//
// Use in tests that exercise the hk-f6g7 ProcessExit-skips-waitAgentReady gate:
// wire AdapterRegistry2 with RegisterCodex and HarnessRegistry from
// ExportedNewHarnessRegistry, then supply this builder so beadRunOne looks up
// the codex adapter + harness (Completion() == CompletionProcessExit).
//
// The script is expected to run in the worktree directory (handler.LaunchSpec.WorkDir
// = shared.LaunchCtx.WorkspacePath).  It MUST make a "Refs: <beadID>" git commit and
// exit 0; it MUST NOT emit agent_ready.
//
// Bead ref: hk-f6g7.
func ExportedCodexProcessExitLaunchSpecBuilder(scriptPath string) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	return func(_ context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		spec := handler.LaunchSpec{
			Binary:  "/bin/sh",
			Args:    []string{scriptPath},
			WorkDir: rc.WorkspacePath,
			Role:    string(rc.Phase),
		}
		arts := shared.LaunchArtifacts{
			ResolvedAgentType: core.AgentTypeCodex,
		}
		return spec, arts, nil
	}
}

// ExportedRoutedLaunchSpecBuilder exposes routedLaunchSpecBuilder for tests in
// package daemon_test. It returns a builder that resolves the harness via the
// four-tier precedence walk and the HarnessRegistry, then (for the claude
// harness) delegates to buildClaudeLaunchSpec. The returned closure has the same
// shape as the workLoopDeps.launchSpecBuilder hook.
//
// Bead ref: hk-hj9ld.
func ExportedRoutedLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
	return routedLaunchSpecBuilder(reg, bead, queueDefault, nodeDefault, globalDefault, bus)
}

// ExportedObservedRoutedLaunchSpecBuilder returns the REAL production
// routedLaunchSpecBuilder in the INTERNAL workLoopDeps.launchSpecBuilder shape
// (so it can be installed via WorkLoopDepsParams.LaunchSpecBuilder), wrapped in
// a call observer. It is exactly the builder beadRunOne installs in production
// (workloop.go: routedLaunchSpecBuilder(reg, beadRecord, "", "", defaultHarness,
// bus)), so a test can assert whether a downstream dispatch site CONSULTED it or
// correctly REPLACED it. onCall fires once per invocation, before the build runs.
//
// Bead ref: hk-01vs0.
func ExportedObservedRoutedLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
	onCall func(),
) func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
	builder := routedLaunchSpecBuilder(reg, bead, core.AgentType(""), core.AgentType(""), globalDefault, bus)
	return func(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		if onCall != nil {
			onCall()
		}
		return builder(ctx, rc)
	}
}

// ExportedPinnedHarnessLaunchSpecBuilder exposes pinnedHarnessLaunchSpecBuilder
// for tests in package daemon_test. It returns a builder that uses agentType
// directly (bypassing resolveHarness) so tests can assert the node-level pin
// wins unconditionally over any bead label (hk-2jxqg).
func ExportedPinnedHarnessLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	agentType core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
	return pinnedHarnessLaunchSpecBuilder(reg, bead, agentType, bus)
}
