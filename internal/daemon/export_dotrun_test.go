package daemon

// export_dotrun_test.go — dot_cascade.go / dot_gate.go test-seam exports.
//
// Split out of export_test.go (RT19.3, P2 E5 export_test.go split) so the DOT
// run-path shims (driveDotWorkflow family, cognition-gate execution and verdict
// readers, per-node model resolution) live in one topic file. Same package
// (daemon), so every daemon_test caller resolves daemon.ExportedX
// byte-identically after the move.
//
// Bead: hk-ecrxy.

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

// ExportedNodeModelForHarness exposes nodeModelForHarness so tests can assert the
// harness-family scoping of a DOT per-node model= pin (hk-lfrub,
// codename:pi-model-leak).
func ExportedNodeModelForHarness(resolvedModel, nodeModelAttr string, effHarness core.AgentType) string {
	return nodeModelForHarness(resolvedModel, nodeModelAttr, effHarness)
}

// DotWorkflowResultExported is the exported shape of dotWorkflowResult for tests
// in package daemon_test. Fields mirror dotWorkflowResult verbatim.
//
// Bead ref: hk-3qjwl.
type DotWorkflowResultExported struct {
	Success        bool
	TerminalNodeID string
	NeedsAttention bool
	Summary        string
	// AdvisoryRC mirrors dotWorkflowResult.advisoryRC (hk-whru3).
	AdvisoryRC bool
	// ApproveVerdict mirrors dotWorkflowResult.approveVerdict (hk-tnui).
	ApproveVerdict *workspace.ReviewVerdict
}

// ExportedDriveDotWorkflow exposes driveDotWorkflow for tests in package
// daemon_test. The result is converted to DotWorkflowResultExported to avoid
// exporting the internal dotWorkflowResult type.
//
// Bead ref: hk-3qjwl (DOT agentic-node dispatch must gate paste-inject on
// agent_ready, exactly as the single-mode and review-loop paths do).
func ExportedDriveDotWorkflow(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
) DotWorkflowResultExported {
	env, rp, handles := runBundlesFromDeps(deps, runID)
	r := driveDotWorkflow(ctx, env, rp, handles, runID, beadID, core.BeadRecord{}, "", "", wtPath, parentSHA, graph, "", "", "", "", nil, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
		AdvisoryRC:     r.advisoryRC,
		ApproveVerdict: r.approveVerdict,
	}
}

// ExportedDriveDotWorkflowFull is like ExportedDriveDotWorkflow but exposes the
// beadTitle, beadDescription, and extraContext parameters so tests can assert on
// context injection (e.g. node role= surfacing, hk-m5lmo).
func ExportedDriveDotWorkflowFull(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	extraContext string,
) DotWorkflowResultExported {
	env, rp, handles := runBundlesFromDeps(deps, runID)
	r := driveDotWorkflow(ctx, env, rp, handles, runID, beadID, core.BeadRecord{}, beadTitle, beadDescription, wtPath, parentSHA, graph, "", "", extraContext, "", nil, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
		AdvisoryRC:     r.advisoryRC,
		ApproveVerdict: r.approveVerdict,
	}
}

// ExportedExecuteCognitionGate drives executeCognitionGate — the PRODUCTION
// cognition-gate dispatch body in dot_gate.go — directly, so a test can assert
// which harness the gate actually resolves without standing up the whole DOT
// cascade (which needs a real git worktree and a real tmux pane).
//
// The call is expected to FAIL somewhere after the launch-spec build (Launch,
// agent_ready wait, or verdict read) under a unit fixture; the returned error is
// informational only. Callers assert on the events the build emitted and on
// whether deps.launchSpecBuilder was consulted.
//
// Bead ref: hk-01vs0.
func ExportedExecuteCognitionGate(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	cp core.ControlPoint,
	wtPath string,
	node *dot.Node,
	beadID core.BeadID,
	beadRecord core.BeadRecord,
	queueDefault core.AgentType,
) error {
	dp := cp.Evaluator.DelegationPath
	if dp == nil {
		return fmt.Errorf("ExportedExecuteCognitionGate: ControlPoint %q has no DelegationPath", cp.Name)
	}
	run := &core.Run{
		RunID:        runID,
		WorkflowMode: core.WorkflowModeDot,
		Context:      map[string]any{},
	}
	env := deps.runEnv(runID, beadRecord, "", nil, nil, 0, "", "", nil, false, "", queueDefault)
	rp, handles := deps.buildRunBundles(env)
	_, err := executeCognitionGate(
		ctx, env, rp, handles, runID, run, cp, *dp, wtPath, "",
		node, 1, "", "",
		beadID, beadRecord, "hk-01vs0 gate fixture bead", "gate fixture body",
		"", "main", core.GateRef(node.GateRef),
		nil, "", "", "",
	)
	return err
}

// ExportedDriveDotWorkflowWithRunner exposes driveDotWorkflow with an explicit
// CommandRunner so tests can assert the remote (runner != nil) path threads the
// runner into the DOT agentic-node shared.LaunchCtx (hk-3sus).
func ExportedDriveDotWorkflowWithRunner(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	runner tmuxPkg.CommandRunner,
) DotWorkflowResultExported {
	env, rp, handles := runBundlesFromDeps(deps, runID)
	r := driveDotWorkflow(ctx, env, rp, handles, runID, beadID, core.BeadRecord{}, beadTitle, beadDescription, wtPath, parentSHA, graph, "", "", "", "", runner, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
	}
}

// ExportedDriveDotWorkflowWithModelEffort exposes driveDotWorkflow with
// explicit resolvedModel and resolvedEffort parameters so tests can assert on
// per-node model/effort override vs. run-level default (hk-q8nqr).
func ExportedDriveDotWorkflowWithModelEffort(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	resolvedModel string,
	resolvedEffort string,
) DotWorkflowResultExported {
	env, rp, handles := runBundlesFromDeps(deps, runID)
	r := driveDotWorkflow(ctx, env, rp, handles, runID, beadID, core.BeadRecord{}, beadTitle, beadDescription, wtPath, parentSHA, graph, resolvedModel, resolvedEffort, "", "", nil, "", "", "", "")
	return DotWorkflowResultExported{
		Success:        r.success,
		TerminalNodeID: r.terminalNodeID,
		NeedsAttention: r.needsAttention,
		Summary:        r.summary,
	}
}

// ExportedReadGateVerdictVia exposes readGateVerdictVia for tests in package
// daemon_test. It allows the contract test to verify that the gate-verdict.json
// read routes through runner on remote runs (hk-hd2w6).
//
// Bead ref: hk-hd2w6.
func ExportedReadGateVerdictVia(ctx context.Context, runner tmuxPkg.CommandRunner, verdictPath string) (core.GateAction, error) {
	return readGateVerdictVia(ctx, runner, verdictPath)
}

// ExportedGateVerdictExistsVia exposes gateVerdictExistsVia for tests in
// package daemon_test. Allows the contract test to assert that the os.Stat
// check on gate-verdict.json routes through runner on remote runs (hk-hd2w6).
//
// Bead ref: hk-hd2w6.
func ExportedGateVerdictExistsVia(ctx context.Context, runner tmuxPkg.CommandRunner, path string) bool {
	return gateVerdictExistsVia(ctx, runner, path)
}
