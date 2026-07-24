package daemon

// export_resolvers_test.go — test-seam exports for internal/daemon resolvers
// (RT19.16 split of export_test.go): the moderesolve.go, harnessresolve.go,
// modelpreference.go, pi_profile_resolve.go and standardgraph.go seams. package
// daemon test file; see export_test.go header for the seam rationale. Bead: hk-ecrxy.

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// ExportedLoadStandardGraph parses the embedded standard-bead.dot so tests in
// package daemon_test can inspect node attrs (e.g. review.Harness for hk-ytzj2).
func ExportedLoadStandardGraph(params map[string]string) (*dot.Graph, error) {
	return loadStandardGraph(params)
}

// ExportedResolveWorkflowMode exposes resolveWorkflowMode for tests in package
// daemon_test. See moderesolve.go for semantics.
//
// Bead ref: hk-7om2q.9.
func ExportedResolveWorkflowMode(
	ctx context.Context,
	bead core.BeadRecord,
	daemonDefault core.WorkflowMode,
	bus handlercontract.EventEmitter,
) core.WorkflowMode {
	return resolveWorkflowMode(ctx, bead, daemonDefault, bus)
}

// ExportedResolveWorkflowRef exposes resolveWorkflowRef for tests in package
// daemon_test. See moderesolve.go for semantics.
//
// Bead ref: hk-30q6.
func ExportedResolveWorkflowRef(bead core.BeadRecord, itemWorkflowRef string) string {
	return resolveWorkflowRef(bead, itemWorkflowRef)
}

// ExportedResolveHarness exposes resolveHarness for tests in package daemon_test.
// See harnessresolve.go for semantics.
//
// Bead ref: hk-y01k6 [C4/T4].
func ExportedResolveHarness(
	ctx context.Context,
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
	bus handlercontract.EventEmitter,
) core.AgentType {
	return resolveHarness(ctx, bead, queueDefault, nodeDefault, globalDefault, bus)
}

// ExportedResolveHarnessAgentTypeQuiet exposes resolveHarnessAgentTypeQuiet for
// tests in package daemon_test. See harnessresolve.go (hk-pkugu).
func ExportedResolveHarnessAgentTypeQuiet(
	bead core.BeadRecord,
	queueDefault core.AgentType,
	nodeDefault core.AgentType,
	globalDefault core.AgentType,
) core.AgentType {
	return resolveHarnessAgentTypeQuiet(bead, queueDefault, nodeDefault, globalDefault)
}

// ExportedModelPreferenceError is a type alias for ModelPreferenceError so tests
// in package daemon_test can use errors.As without importing internal types.
//
// Bead ref: hk-xo03m.
type ExportedModelPreferenceError = ModelPreferenceError

// ExportedResolveModelPreference exposes ResolveModelPreference for tests.
//
// Bead ref: hk-bfvk7.
func ExportedResolveModelPreference(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	projectCfg ProjectConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) (model, effort string) {
	return ResolveModelPreference(ctx, beadLabels, agentType, projectCfg, bus, beadID)
}

// ExportedResolvePiProfile exposes resolvePiProfile for tests in package
// daemon_test (pi-provider-switch C5-design). Mirrors the claim-time call
// shape at workloop.go:3099: the caller MUST pass the resolvedAgentType
// produced by ExportedResolveHarnessAgentTypeQuiet (hk-pkugu discipline) so a
// claude/codex-resolved bead never receives a pi tuple. Returns the zero
// PiProfileConfig (all-empty) for a non-pi agentType, an absent/conflicting
// profile: label, or a resolved profile; a *PiProfileUnknownError for an
// unknown profile: reference (fail-loud — the caller must reopen the bead
// rather than launch, matching workloop.go:3103-3109).
//
// Bead ref: hk-m6uu2.5.
func ExportedResolvePiProfile(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	piCfg PiHarnessConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) (PiProfileConfig, error) {
	return resolvePiProfile(ctx, beadLabels, agentType, piCfg, bus, beadID)
}
