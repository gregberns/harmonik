package daemon

// harnessregistry.go — daemon-side HarnessRegistry wiring + registry-routed
// launchSpecBuilder (codex-harness C1/T3, hk-hj9ld).
//
// This file closes the first of the two declared harness seam points
// (harness.go §"the two declared seam points"): the launchSpecBuilder lookup is
// routed through resolveHarness (the four-tier precedence walk, harnessresolve.go)
// and HarnessRegistry.ForAgent (the per-agent-type route table).
//
// CLAUDE-ONLY, NO BEHAVIOR CHANGE: only ClaudeHarness is registered (codex is
// added by a later bead, hk-m57va C2/T8). The default precedence resolution lands
// on core.AgentTypeClaudeCode, so for the existing claude path the routed builder
// resolves to ClaudeHarness and produces a LaunchSpec byte-identical to calling
// buildClaudeLaunchSpec directly — it delegates straight to buildClaudeLaunchSpec
// to preserve the claudeRunArtifacts (session IDs, preExecMsgs) the workloop and
// review-loop require alongside the LaunchSpec.
//
// Spec: specs/harness-contract.md §2 N5.
// See also: handlercontract/harnessregistry.go (the registry type),
// claudeharness.go (the ClaudeHarness adapter), harnessresolve.go (resolveHarness).

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// newHarnessRegistry builds the daemon's HarnessRegistry with the claude harness
// registered for core.AgentTypeClaudeCode.
//
// CLAUDE-ONLY: only ClaudeHarness is registered. Codex is added by a later bead.
// Returns a non-nil error only if Register fails (a duplicate or sealed-registry
// defect), which is impossible for the single registration here but surfaced so
// callers fail-closed if this grows.
func newHarnessRegistry() (*handlercontract.HarnessRegistry, error) {
	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, NewClaudeHarness()); err != nil {
		return nil, fmt.Errorf("daemon: newHarnessRegistry: register claude harness: %w", err)
	}
	return reg, nil
}

// routedLaunchSpecBuilder returns a launchSpecBuilder (the workLoopDeps hook
// shape: func(ctx, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error))
// that routes through resolveHarness + reg.ForAgent before building the spec.
//
// Precedence/selection: resolveHarness walks bead>queue>node>global and falls
// back to core.AgentTypeClaudeCode. The resolved agent_type is looked up in reg;
// an unregistered type returns a well-defined error (the routed builder fails the
// run rather than silently launching claude for an unknown type).
//
// NO BEHAVIOR CHANGE for the claude path: when the resolved harness is the claude
// harness (the default, and the only registered one in T3), the builder delegates
// directly to buildClaudeLaunchSpec(ctx, rc) so the returned LaunchSpec and
// claudeRunArtifacts are byte-identical to the pre-T3 direct call. The Harness
// interface's LaunchSpec returns only a SpawnSpec (no artifacts), so the routed
// builder cannot go through Harness.LaunchSpec for the claude path without losing
// the artifacts the workloop needs — hence the type-asserted direct delegation.
// The registry lookup is the seam; the build itself is unchanged.
//
// The bead argument carries the labels resolveHarness reads for the tier-1
// harness:<agent-type> override. Production passes the dispatch-time BeadRecord;
// callers with no bead (legacy/test) may pass a zero BeadRecord, which resolves to
// the claude default.
func routedLaunchSpecBuilder(
	reg *handlercontract.HarnessRegistry,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) func(context.Context, claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
	return func(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error) {
		agentType := resolveHarness(ctx, bead, queueDefault, nodeDefault, globalDefault, bus)

		h, err := reg.ForAgent(agentType)
		if err != nil {
			return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
				"daemon: routedLaunchSpecBuilder: resolve harness %q: %w", agentType, err)
		}

		// Claude path: delegate to buildClaudeLaunchSpec directly so the returned
		// LaunchSpec AND claudeRunArtifacts are byte-identical to the pre-T3 call.
		// Harness.LaunchSpec returns only a SpawnSpec, so routing the claude build
		// through it would drop the artifacts the workloop/review-loop consume.
		if _, ok := h.(*ClaudeHarness); ok {
			return buildClaudeLaunchSpec(ctx, rc)
		}

		// Non-claude harnesses are not wired into the claudeRunArtifacts seam in
		// T3 (codex lands in a later bead with its own artifacts threading). Fail
		// closed rather than silently mis-launch.
		return handler.LaunchSpec{}, claudeRunArtifacts{}, fmt.Errorf(
			"daemon: routedLaunchSpecBuilder: harness %q resolved but not wired into the "+
				"claudeRunArtifacts launch path in C1/T3 (claude-only); a later bead adds it",
			agentType)
	}
}
