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

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// newDotSubWorkflowRunner constructs a dotSubWorkflowRunner from the current
// driveDotWorkflow call context. The parentGraph is the loaded dot.Graph for
// the enclosing workflow; its Name (or version, or a stable placeholder) is
// used as the "parent vertex" in the sub-workflow reference graph acyclicity
// check (SW-003).
func newDotSubWorkflowRunner(
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadRecord core.BeadRecord,
	beadTitle, beadDescription string,
	wtPath, parentSHA, daemonSocket string,
	iterationCount *int,
	claudeSessionID *string,
	resolvedModel, resolvedEffort string,
	extraContext, baseBranch string,
	run *core.Run,
	cycles *core.CycleCounter,
	parentGraph *dot.Graph,
	runner tmux.CommandRunner, // remote-substrate: SSHRunner for remote runs; nil for local (NFR7)
) *dotSubWorkflowRunner {
	parentName := parentGraphName(parentGraph)
	return &dotSubWorkflowRunner{
		deps:               deps,
		runID:              runID,
		beadID:             beadID,
		beadRecord:         beadRecord,
		beadTitle:          beadTitle,
		beadDescription:    beadDescription,
		wtPath:             wtPath,
		parentSHA:          parentSHA,
		daemonSocket:       daemonSocket,
		iterationCount:     iterationCount,
		claudeSessionID:    claudeSessionID,
		resolvedModel:      resolvedModel,
		resolvedEffort:     resolvedEffort,
		extraContext:       extraContext,
		baseBranch:         baseBranch,
		run:                run,
		cycles:             cycles,
		parentGraph:        parentGraph,
		parentWorkflowName: parentName,
		runner:             runner,
	}
}

// parentGraphName returns a stable name for a dot.Graph for use as the parent
// vertex in the acyclicity reference graph. Prefers graph.Name, falls back to
// graph.Version, and finally uses a constant placeholder.
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

// dotSubWorkflowRunner is the concrete handler.SubWorkflowRunner for the DOT
// cascade dispatch loop. It captures all per-dispatch context from the enclosing
// driveDotWorkflow call so the Run method can dispatch expanded sub-workflow
// nodes using the same infrastructure as the parent cascade.
type dotSubWorkflowRunner struct {
	deps            workLoopDeps
	runID           core.RunID
	beadID          core.BeadID
	beadRecord      core.BeadRecord
	beadTitle       string
	beadDescription string
	wtPath          string
	parentSHA       string
	daemonSocket    string
	iterationCount  *int
	claudeSessionID *string
	resolvedModel   string
	resolvedEffort  string
	extraContext    string
	baseBranch      string
	run             *core.Run
	cycles          *core.CycleCounter
	// runner is the run's CommandRunner (SSHRunner for a remote-substrate worker,
	// nil for local). Threaded into nested dispatchDotAgenticNode calls so the
	// sub-workflow's worktree probes + spawn target the worker (NFR7: nil = local).
	runner tmux.CommandRunner
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

	// ── Step 1: Three-tier graph resolution (SW-004) ──────────────────────────
	subGraph, resolvedPath, resolveErr := resolveSubWorkflowGraph(
		string(spec.SubWorkflowRef),
		r.deps.projectDir,
	)
	if resolveErr != nil {
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: graph resolution failed: %v", spec.ParentNodeID, resolveErr),
		}, nil
	}

	// ── Step 2: Acyclicity check (SW-003 / EM-034b) ───────────────────────────
	// Build the reference graph from the parent workflow's sub-workflow refs
	// plus those in the resolved child graph. HasCycle() detects direct and
	// transitive cycles (A→A, A→B→A, etc.).
	if cycleErr := checkSubWorkflowAcyclicity(r.parentWorkflowName, string(spec.SubWorkflowRef), r.parentGraph, subGraph); cycleErr != nil {
		// SW-003: NO sub_workflow_entered event; fail closed with structural.
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: acyclicity violation: %v", spec.ParentNodeID, cycleErr),
		}, nil
	}

	// ── Step 3: SW-010 — reject review-loop sub-workflows ─────────────────────
	if strings.EqualFold(subGraph.WorkflowClass, "review-loop") {
		fc := core.FailureClassStructural
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			FailureClass: &fc,
			Notes:        fmt.Sprintf("sub-workflow node %q: references a review-loop sub-workflow (SW-010)", spec.ParentNodeID),
		}, nil
	}

	// ── Step 4: Build expansion pin and expand graph (SW-001/SW-002) ──────────
	// The resolved workflow UUID is derived deterministically from the resolved
	// filesystem path via UUID v5 (namespace=DNS), giving a stable identifier
	// per EM-034c without requiring a formal registry.
	resolvedWorkflowID := core.WorkflowID(uuid.NewSHA1(uuid.NameSpaceDNS, []byte(resolvedPath)))
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
		// ExpandSubWorkflowGraph returns *ErrSubWorkflowExpand for structural
		// failures; treat as infrastructure error → returned error triggers run_failed.
		return core.Outcome{}, fmt.Errorf("sub-workflow node %q: expand: %w", spec.ParentNodeID, expandErr)
	}

	// ── Step 5: Build the SubWorkflowNodeRunner closure ───────────────────────
	// Index sub-graph nodes by their NAMESPACED ID so the runner can look up
	// the original dot.Node (with HandlerRef, ToolCommand, etc.) by the
	// namespaced ID that DispatchSubWorkflow passes.
	namespacedNodes := make(map[core.NodeID]*dot.Node, len(subGraph.Nodes))
	for _, n := range subGraph.Nodes {
		ns := core.NamespaceNodeID(spec.ParentNodeID, core.NodeID(n.ID))
		nCopy := *n
		namespacedNodes[ns] = &nCopy
	}

	// subRunner is a nested dotSubWorkflowRunner for dispatching sub-workflow
	// nodes found inside the child graph (recursive expansion).
	subRunner := &dotSubWorkflowRunner{
		deps:               r.deps,
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

	// ── Step 6: Dispatch the expanded sub-workflow (SW-005/SW-006) ────────────
	// DispatchSubWorkflow emits sub_workflow_entered, walks the expanded graph
	// via nodeRunner, emits sub_workflow_exited, and returns the terminal Outcome.
	outcome, dispatchErr := workflow.DispatchSubWorkflow(ctx, r.run, expansion, subGraph, r.cycles, nodeRunner, r.deps.bus)
	if dispatchErr != nil {
		return core.Outcome{}, fmt.Errorf("sub-workflow node %q: dispatch: %w", spec.ParentNodeID, dispatchErr)
	}
	return outcome, nil
}

// dispatchSubWorkflowExpandedNode dispatches a single expanded node within a
// sub-workflow using the same infrastructure as the parent DOT cascade.
//
// Node types:
//   - non-agentic (shell tool): calls dispatchDotToolNode.
//   - non-agentic (other): synthesizes SUCCESS.
//   - agentic: calls dispatchDotAgenticNode.
//   - gate: calls dispatchDotGateNode.
//   - sub-workflow: calls subRunner.Run recursively.
//   - unknown: structural FAIL Outcome.
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
			return dispatchDotToolNode(ctx, r.wtPath, n, r.deps.handlerEnv)
		}
		// Non-shell non-agentic: synthesize SUCCESS.
		return core.Outcome{Status: core.OutcomeStatusSuccess}, nil

	case core.NodeTypeAgentic:
		return dispatchDotAgenticNode(
			ctx,
			r.deps,
			r.runID,
			r.beadID,
			r.beadRecord,
			r.beadTitle,
			r.beadDescription,
			r.wtPath,
			r.parentSHA,
			r.daemonSocket,
			n,
			false, // isReviewer: sub-workflow nodes are not reviewer nodes
			*r.iterationCount,
			r.claudeSessionID,
			r.resolvedModel,
			r.resolvedEffort,
			r.extraContext,
			r.baseBranch,
			"",       // reviewerHarnessOverride: none
			r.runner, // remote-substrate: route sub-workflow node dispatch through the run's runner
		)

	case core.NodeTypeGate:
		return dispatchDotGateNode(
			ctx, r.deps, r.runID, r.run, r.wtPath, r.daemonSocket, n,
			*r.iterationCount, r.resolvedModel, r.resolvedEffort,
			r.beadID, r.beadTitle, r.beadDescription, r.extraContext, r.baseBranch,
		)

	case core.NodeTypeSubWorkflow:
		// Recursive sub-workflow: construct a nested spec and dispatch.
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

// resolveSubWorkflowGraph implements the three-tier resolution order (SW-004):
//  1. Explicit ref: look for <projectDir>/<subWorkflowRef> (and .dot variant).
//  2. Project-default graph: <projectDir>/workflow.dot.
//  3. Error: structural fail.
//
// Returns the loaded *dot.Graph and the resolved absolute path.
func resolveSubWorkflowGraph(subWorkflowRef, projectDir string) (*dot.Graph, string, error) {
	// Tier 1: explicit ref — try the ref as a path relative to projectDir.
	tier1Candidates := subWorkflowRefPaths(subWorkflowRef, projectDir)
	for _, candidate := range tier1Candidates {
		g, err := workflow.LoadDotWorkflow(candidate)
		if err == nil {
			return g, candidate, nil
		}
	}

	// Tier 2: project-default workflow.dot.
	if subWorkflowRef != "workflow.dot" { // avoid double-trying the same file
		defaultPath := filepath.Join(projectDir, "workflow.dot")
		g, err := workflow.LoadDotWorkflow(defaultPath)
		if err == nil {
			return g, defaultPath, nil
		}
	}

	// Tier 3: structural error.
	return nil, "", fmt.Errorf("sub-workflow %q: no registered artifact found (SW-004 tier 3: structural)", subWorkflowRef)
}

// subWorkflowRefPaths returns the ordered list of filesystem paths to probe for
// a given sub_workflow_ref. It tries the ref as-is (absolute or project-relative)
// and, if it lacks a .dot extension, also tries with .dot appended.
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

// checkSubWorkflowAcyclicity builds the sub-workflow reference graph for the
// parent→child edge (and all direct sub-workflow refs in both graphs) and
// reports whether a cycle exists per EM-034b / SW-003.
//
// This is a best-effort runtime check: it adds the direct sub-workflow edges
// from the parent and child graphs. A cycle in deeper nesting is caught when
// those sub-workflows are dispatched recursively.
func checkSubWorkflowAcyclicity(parentWorkflowName, childWorkflowName string, parentGraph, childGraph *dot.Graph) error {
	refGraph := core.NewSubWorkflowRefGraph()

	// Add all sub-workflow edges from the parent graph.
	if parentGraph != nil {
		for _, n := range parentGraph.Nodes {
			if n.Type == core.NodeTypeSubWorkflow && n.SubWorkflowRef != "" {
				refGraph.AddEdge(parentWorkflowName, n.SubWorkflowRef)
			}
		}
	}

	// Add all sub-workflow edges from the child graph (one level deep).
	if childGraph != nil {
		for _, n := range childGraph.Nodes {
			if n.Type == core.NodeTypeSubWorkflow && n.SubWorkflowRef != "" {
				refGraph.AddEdge(childWorkflowName, n.SubWorkflowRef)
			}
		}
	}

	// The critical edge: parent references child.
	refGraph.AddEdge(parentWorkflowName, childWorkflowName)

	if refGraph.HasCycle() {
		return fmt.Errorf("sub-workflow reference graph is cyclic: %q → %q (EM-034b)", parentWorkflowName, childWorkflowName)
	}
	return nil
}
