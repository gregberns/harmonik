package daemon

// sub_workflow_runner.go — concrete SubWorkflowRunner for the DOT cascade.
//
// dotSubWorkflowRunner implements handler.SubWorkflowRunner by wiring the
// three-tier graph resolution (SW-004), acyclicity check (SW-003), expansion
// (SW-001/SW-002), event emission (SW-005), node dispatch (SW-007), and
// verbatim outcome escape (SW-006) into a single Run call.
//
// This type is constructed per sub-workflow node dispatch in driveDotWorkflow
// and is the only site that calls workflow.DispatchSubWorkflow for the DOT
// cascade (SW-007 boundary).
//
// Spec refs:
//
//	specs/sub-workflow-dispatch.md SW-001..SW-010
//	specs/execution-model.md §4.8.EM-034..EM-036a
//	specs/workflow-graph.md §4 WG-006
//
// Bead: hk-oe6
// Tags: mechanism

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

func newDotSubWorkflowRunner(
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
	runID core.RunID,
	beadID core.BeadID,
	beadRecord core.BeadRecord,
	beadTitle, beadDescription string,
	activeRepo string, // hk-pq3ex: the repo this bead's work lands in; env.ProjectDir for a local bead
	wtPath, parentSHA, daemonSocket string,
	iterationCount *int,
	claudeSessionID *string,
	resolvedModel, resolvedEffort string,
	piProfile projectconfig.PiProfileConfig, // hk-yo9g6: the per-bead Pi provider tuple
	extraContext, baseBranch string,
	run *core.Run,
	cycles *core.CycleCounter,
	parentGraph *dot.Graph,
	runner tmux.CommandRunner, // remote-substrate: SSHRunner for remote runs; nil for local (NFR7)
	workerBinaryPath string, // hk-538l: worker harmonik path for remote sub-workflow nodes; "" = local
	workerSessionName string, // hk-538l: worker tmux session for remote sub-workflow spawn; "" = local
	workerSessionCwd string, // hk-538l: worker repo cwd for the worker tmux session; "" = local
) *dotSubWorkflowRunner {
	parentName := parentGraphName(parentGraph)
	return &dotSubWorkflowRunner{
		env:                env,
		ports:              ports,
		handles:            handles,
		runID:              runID,
		beadID:             beadID,
		beadRecord:         beadRecord,
		beadTitle:          beadTitle,
		beadDescription:    beadDescription,
		activeRepo:         activeRepo,
		wtPath:             wtPath,
		parentSHA:          parentSHA,
		daemonSocket:       daemonSocket,
		iterationCount:     iterationCount,
		claudeSessionID:    claudeSessionID,
		resolvedModel:      resolvedModel,
		resolvedEffort:     resolvedEffort,
		piProfile:          piProfile,
		extraContext:       extraContext,
		baseBranch:         baseBranch,
		run:                run,
		cycles:             cycles,
		parentGraph:        parentGraph,
		parentWorkflowName: parentName,
		runner:             runner,
		workerBinaryPath:   workerBinaryPath,
		workerSessionName:  workerSessionName,
		workerSessionCwd:   workerSessionCwd,
	}
}

func parentGraphName(g *dot.Graph) string {
	if g == nil {
		return "__root__"
	}
	if g.Name != "" {
		return g.Name
	}
	if g.Version != "" {
		return g.Version
	}
	return "__root__"
}

type dotSubWorkflowRunner struct {
	env             runloop.RunEnv
	ports           runloop.RunPorts
	handles         runloop.SharedHandles
	runID           core.RunID
	beadID          core.BeadID
	beadRecord      core.BeadRecord
	beadTitle       string
	beadDescription string
	activeRepo      string
	wtPath          string
	parentSHA       string
	daemonSocket    string
	iterationCount  *int
	claudeSessionID *string
	resolvedModel   string
	resolvedEffort  string
	piProfile       projectconfig.PiProfileConfig
	extraContext    string
	baseBranch      string
	run             *core.Run
	cycles          *core.CycleCounter
	// runner is the run's CommandRunner (SSHRunner for a remote-substrate worker,
	// nil for local). Threaded into nested dispatchDotAgenticNode calls so the
	// sub-workflow's worktree probes + spawn target the worker (NFR7: nil = local).
	runner tmux.CommandRunner
	// hk-538l: worker-launch params propagated into nested dispatchDotAgenticNode
	// calls so a REMOTE sub-workflow agentic node resolves its hook command to the
	// worker's harmonik path and spawns into the worker's tmux session. All empty
	// for a LOCAL run (NFR7).
	workerBinaryPath  string
	workerSessionName string
	workerSessionCwd  string
	// parentGraph is the loaded dot.Graph of the parent workflow. Its Name is
	// used as the "parent" vertex when building the sub-workflow reference graph
	// for the acyclicity check (SW-003 / EM-034b).
	parentGraph        *dot.Graph
	parentWorkflowName string // graph.Name, or a stable placeholder when empty
}

// Run implements handler.SubWorkflowRunner. It is called by the DOT cascade
// dispatch loop when a node of type core.NodeTypeSubWorkflow is encountered.
//
// Steps per SW-007:
//  1. Resolve the target sub-workflow graph (three-tier: SW-004).
//  2. Check acyclicity (SW-003).
//  3. Check no review-loop sub-workflow (SW-010).
//  4. Build SubWorkflowExpansionPin and call workflow.ExpandSubWorkflowGraph (SW-001/SW-002).
//  5. Create a per-node SubWorkflowNodeRunner closure.
//  6. Call workflow.DispatchSubWorkflow, which emits entered/exited (SW-005)
//     and returns the terminal Outcome (SW-006).
//
// Structural failures (acyclicity, resolution, review-loop) are returned as
// Outcomes with Status=FAIL and FailureClass=structural. Infrastructure
// failures (event emission) are returned as errors.
func (r *dotSubWorkflowRunner) Run(ctx context.Context, spec handler.SubWorkflowRunSpec) (core.Outcome, error) {
	if !spec.Valid() {
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: invalid SubWorkflowRunSpec", spec.ParentNodeID),
		}, nil
	}

	subGraph, _, resolveErr := resolveSubWorkflowGraph(
		string(spec.SubWorkflowRef),
		r.env.ProjectDir,
	)
	if resolveErr != nil {
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: graph resolution failed: %v", spec.ParentNodeID, resolveErr),
		}, nil
	}

	if cycleErr := checkSubWorkflowAcyclicity(r.parentWorkflowName, string(spec.SubWorkflowRef), r.parentGraph, subGraph); cycleErr != nil {
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: acyclicity violation: %v", spec.ParentNodeID, cycleErr),
		}, nil
	}

	if strings.EqualFold(subGraph.WorkflowClass, "review-loop") {
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: references a review-loop sub-workflow (SW-010)", spec.ParentNodeID),
		}, nil
	}

	resolvedWorkflowID := subGraph.WorkflowID
	pin := core.SubWorkflowExpansionPin{
		SubWorkflowRef:     spec.SubWorkflowRef,
		SubWorkflowVersion: spec.SubWorkflowVersion,
		ResolvedWorkflowID: resolvedWorkflowID,
	}
	if !pin.Valid() {
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: expansion pin is invalid", spec.ParentNodeID),
		}, nil
	}

	expansion, expandErr := workflow.ExpandSubWorkflowGraph(spec.ParentNodeID, pin, subGraph)
	if expandErr != nil {
		return core.Outcome{}, fmt.Errorf("sub-workflow node %q: expand: %w", spec.ParentNodeID, expandErr)
	}

	namespacedNodes := make(map[core.NodeID]*dot.Node, len(subGraph.Nodes))
	for _, n := range subGraph.Nodes {
		ns := core.NamespaceNodeID(spec.ParentNodeID, core.NodeID(n.ID))
		nCopy := *n
		namespacedNodes[ns] = &nCopy
	}

	subRunner := &dotSubWorkflowRunner{
		env:                r.env,
		ports:              r.ports,
		handles:            r.handles,
		runID:              r.runID,
		beadID:             r.beadID,
		beadRecord:         r.beadRecord,
		beadTitle:          r.beadTitle,
		beadDescription:    r.beadDescription,
		wtPath:             r.wtPath,
		parentSHA:          r.parentSHA,
		daemonSocket:       r.daemonSocket,
		iterationCount:     r.iterationCount,
		claudeSessionID:    r.claudeSessionID,
		resolvedModel:      r.resolvedModel,
		resolvedEffort:     r.resolvedEffort,
		extraContext:       r.extraContext,
		baseBranch:         r.baseBranch,
		run:                r.run,
		cycles:             r.cycles,
		parentGraph:        subGraph,
		parentWorkflowName: string(spec.SubWorkflowRef),
		runner:             r.runner,
		workerBinaryPath:   r.workerBinaryPath,
		workerSessionName:  r.workerSessionName,
		workerSessionCwd:   r.workerSessionCwd,
	}

	nodeRunner := func(ctx context.Context, nodeID core.NodeID, nodeType core.NodeType) (core.Outcome, error) {
		n := namespacedNodes[nodeID]
		if n == nil {
			fc := core.FailureClassStructural
			return core.Outcome{
				Status:       core.OutcomeStatusFail,
				FailureClass: &fc,
				Notes:        fmt.Sprintf("sub-workflow: expanded node %q not found in sub-graph", nodeID),
			}, nil
		}
		return dispatchSubWorkflowExpandedNode(ctx, r, subRunner, nodeID, n)
	}

	outcome, dispatchErr := workflow.DispatchSubWorkflow(ctx, r.run, expansion, subGraph, r.cycles, nodeRunner, r.ports.Emitter)
	if dispatchErr != nil {
		return core.Outcome{}, fmt.Errorf("sub-workflow node %q: dispatch: %w", spec.ParentNodeID, dispatchErr)
	}
	return outcome, nil
}

func dispatchSubWorkflowExpandedNode(
	ctx context.Context,
	r *dotSubWorkflowRunner,
	subRunner *dotSubWorkflowRunner,
	nodeID core.NodeID,
	n *dot.Node,
) (core.Outcome, error) {
	switch n.Type {
	case core.NodeTypeNonAgentic:
		if n.ToolCommand != "" && n.HandlerRef == "shell" {
			return dispatchDotToolNode(ctx, r.ports.Emitter, r.runID, r.runner, r.env.ProjectDir, r.wtPath, n, r.env.HandlerEnv)
		}
		return core.Outcome{Status: core.OutcomeStatusSuccess}, nil

	case core.NodeTypeAgentic:
		return dispatchDotAgenticNode(
			ctx,
			r.env,
			r.ports,
			r.handles,
			r.runID,
			r.beadID,
			r.beadRecord,
			r.beadTitle,
			r.beadDescription,
			r.activeRepo,
			r.wtPath,
			r.parentSHA,
			r.daemonSocket,
			n,
			false, // isReviewer: sub-workflow nodes are not reviewer nodes
			*r.iterationCount,
			r.claudeSessionID,
			r.resolvedModel,
			r.resolvedEffort,
			r.piProfile,
			r.extraContext,
			r.baseBranch,
			"",                  // reviewerHarnessOverride: none
			r.runner,            // remote-substrate: route sub-workflow node dispatch through the run's runner
			r.workerBinaryPath,  // hk-538l: worker harmonik path for the node hook command
			r.workerSessionName, // hk-538l: worker tmux session to ensure + spawn into
			r.workerSessionCwd,  // hk-538l: worker repo cwd for the worker tmux session
			false,               // isTerminalSpawn: sub-workflow expanded nodes use non-terminal path; consolidate detection requires de-namespacing (hk-x882o follow-up)
		)

	case core.NodeTypeGate:
		return dispatchDotGateNode(
			ctx, r.env, r.ports, r.handles, r.runID, r.run, r.wtPath, r.daemonSocket, n,
			*r.iterationCount, r.resolvedModel, r.resolvedEffort,
			r.beadID, r.beadRecord, // hk-01vs0: tier-1 harness label reaches the gate's harness resolution
			r.beadTitle, r.beadDescription, r.extraContext, r.baseBranch, r.runner,
			r.workerBinaryPath, r.workerSessionName, r.workerSessionCwd,
		)

	case core.NodeTypeSubWorkflow:
		subSpec := handler.SubWorkflowRunSpec{
			Run:                r.run,
			ParentNodeID:       nodeID,
			SubWorkflowRef:     core.SubWorkflowRef(n.SubWorkflowRef),
			SubWorkflowVersion: core.WorkflowVersion(n.WorkflowVersion),
		}
		return subRunner.Run(ctx, subSpec)

	default:
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow: node %q has unknown type %q", nodeID, n.Type),
		}, nil
	}
}

func resolveSubWorkflowGraph(subWorkflowRef, projectDir string) (*dot.Graph, string, error) {
	tier1Candidates := subWorkflowRefPaths(subWorkflowRef, projectDir)
	for _, candidate := range tier1Candidates {
		g, err := workflow.LoadDotWorkflow(candidate)
		if err == nil {
			return g, candidate, nil
		}
	}

	if subWorkflowRef != "workflow.dot" { // avoid double-trying the same file
		defaultPath := filepath.Join(projectDir, "workflow.dot")
		g, err := workflow.LoadDotWorkflow(defaultPath)
		if err == nil {
			return g, defaultPath, nil
		}
	}

	return nil, "", fmt.Errorf("sub-workflow %q: no registered artifact found (SW-004 tier 3: structural)", subWorkflowRef)
}

func subWorkflowRefPaths(ref, projectDir string) []string {
	var candidates []string
	if filepath.IsAbs(ref) {
		candidates = append(candidates, ref)
		if !strings.HasSuffix(ref, ".dot") {
			candidates = append(candidates, ref+".dot")
		}
	} else {
		p := filepath.Join(projectDir, ref)
		candidates = append(candidates, p)
		if !strings.HasSuffix(ref, ".dot") {
			candidates = append(candidates, p+".dot")
		}
	}
	return candidates
}

func checkSubWorkflowAcyclicity(parentWorkflowName, childWorkflowName string, parentGraph, childGraph *dot.Graph) error {
	refGraph := core.NewSubWorkflowRefGraph()

	if parentGraph != nil {
		for _, n := range parentGraph.Nodes {
			if n.Type == core.NodeTypeSubWorkflow && n.SubWorkflowRef != "" {
				refGraph.AddEdge(parentWorkflowName, n.SubWorkflowRef)
			}
		}
	}

	if childGraph != nil {
		for _, n := range childGraph.Nodes {
			if n.Type == core.NodeTypeSubWorkflow && n.SubWorkflowRef != "" {
				refGraph.AddEdge(childWorkflowName, n.SubWorkflowRef)
			}
		}
	}

	refGraph.AddEdge(parentWorkflowName, childWorkflowName)

	if refGraph.HasCycle() {
		return fmt.Errorf("sub-workflow reference graph is cyclic: %q → %q (EM-034b)", parentWorkflowName, childWorkflowName)
	}
	return nil
}
