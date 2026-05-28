package daemon

// dot_cascade.go — DOT workflow-mode cascade driver (hk-9dnak).
//
// driveDotWorkflow walks an arbitrary validated DOT workflow graph node-by-node,
// dispatching each node according to its type and using the cascade engine
// (workflow.DecideNextNode) to resolve the next node after each outcome. It is a
// GENERALIZATION of the hardcoded review-loop driver (reviewloop.go): instead of
// a fixed implementer→reviewer cycle, it follows the graph's edges.
//
// # Node-type dispatch table
//
//   - non-agentic (e.g. noop): no agent. A SUCCESS outcome is synthesized and the
//     single outbound edge is followed.
//   - agentic: the handler is dispatched into the substrate exactly like
//     single-mode / review-loop (worktree, paste-inject, commit detection). The
//     node's outcome is derived from the run result:
//       * reviewer-class nodes (a .harmonik/review.json verdict was produced):
//         outcome.preferred_label = the verdict (APPROVE / REQUEST_CHANGES / BLOCK).
//       * other agentic nodes (implementer): outcome = SUCCESS, no preferred_label
//         (the implementer→reviewer edge is unconditional). HEAD MUST have advanced.
//   - gate / sub-workflow: OUT OF SCOPE for this bead (hk-9dnak). Gate semantics
//     are under an unresolved spec contradiction (EM-005b deny-routing vs CP-058
//     gate_decision payload); sub-workflow dispatch is a separate bead. The driver
//     returns a deterministic failure that reopens the bead rather than attempting
//     dispatch.
//
// # Terminal handling
//
// The walk ends when DecideNextNode reports the current node is terminal (it is
// in graph.TerminalNodeIDs). The driver classifies the terminal node by its role
// in the graph's edge topology: a node reachable only via an APPROVE-class edge is
// the success terminal (close); a node reachable via BLOCK / cap-hit / no-progress
// is the needs-attention terminal (close-needs-attention). Rather than parse
// semantics, the driver records WHICH terminal was reached and the caller maps it:
// the success terminal → CloseBead; the needs-attention terminal → reopen with the
// needs-attention reason. The success-terminal id is provided by the caller
// (resolved from the canonical review-loop topology / convention).
//
// # Cap enforcement
//
// dotEdgeToCoreEdge bridges traversal_cap from the parsed dot.Edge UnknownAttrs
// map into core.Edge.TraversalCap (closing the hk-i7yq8 gap for the DOT→core
// edge conversion). core.SelectNextEdge then enforces the cap by consulting the
// CycleCounter; the driver Increments the counter after traversing a capped edge.
// As defense-in-depth the loop also enforces an absolute node-visit bound so a
// mis-authored graph (missing cap, accidental cycle) cannot spin forever.
//
// Spec refs:
//   - specs/execution-model.md §7.5 (dot-mode dispatcher: input contract,
//     dispatch equivalence, validator obligations, dispatch table).
//   - specs/execution-model.md §4.10 EM-041 / EM-043 (cascade + traversal cap).
//   - specs/workflow-graph.md §5 WG-010..WG-012 (five-step cascade).
//   - specs/examples/review-loop.dot (canonical fixture).
//
// Bead: hk-9dnak (cascade driver wiring); hk-bf85t (cascade engine library);
// hk-i7yq8 (traversal_cap bridge).

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

// dotMaxNodeVisits is the absolute upper bound on the number of node visits in a
// single DOT-mode run. It is a safety net independent of per-edge traversal_cap
// enforcement: a graph that omits a cap on a back-edge (which the validator
// SHOULD reject per WG-028, but defense-in-depth is cheap) cannot spin the daemon
// forever. The value is generous relative to the EM-015e iteration cap (3) so it
// never fires on a well-formed graph.
const dotMaxNodeVisits = 64

// dotWorkflowResult is the terminal outcome of driveDotWorkflow. The caller
// (beadRunOne's WorkflowModeDot branch) uses it to drive the bead close/reopen
// decision and run lifecycle events.
type dotWorkflowResult struct {
	// success is true when the walk reached the success terminal node.
	success bool

	// terminalNodeID is the terminal node the walk reached (empty on a
	// non-terminal failure such as a cascade structural failure or a gate node).
	terminalNodeID string

	// needsAttention is true when the run terminated on a non-success path that
	// requires operator attention (BLOCK, cap-hit, no-progress, or a gate/
	// sub-workflow out-of-scope failure).
	needsAttention bool

	// summary is a short human-readable explanation for run_completed/run_failed.
	summary string
}

// driveDotWorkflow walks the validated DOT graph from its start node to a
// terminal node, dispatching each node by type and following edges via the
// cascade engine.
//
// Parameters mirror runReviewLoop plus the loaded graph. parentSHA is the
// worktree HEAD at creation time (used for HEAD-advanced / commit detection).
//
// The bead transition (close / reopen) and merge-to-main are owned by the caller
// after driveDotWorkflow returns, mirroring how runWorkLoop owns those steps for
// runReviewLoop.
func driveDotWorkflow(
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
	extraContext string,
	baseBranch string,
) dotWorkflowResult {
	daemonSocket := filepath.Join(deps.projectDir, ".harmonik", "daemon.sock")

	// Index nodes by ID for O(1) type lookup during the walk.
	nodesByID := make(map[string]*dot.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodesByID[n.ID] = n
	}

	// Synthesize a *core.Run for the cascade engine. The cascade only reads
	// RunID (for cycle-counter keying) and Context (for EM-041a context updates);
	// the remaining fields are set to valid placeholders so Run is well-formed.
	run := &core.Run{
		RunID:           runID,
		WorkflowID:      core.WorkflowID(uuid.New()),
		WorkflowVersion: core.WorkflowVersion(graphVersionOr(graph)),
		Input:           core.WorkspaceRef(wtPath),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.New()),
		Context:         map[string]any{},
		StartTime:       time.Now(),
	}
	if beadID != "" {
		b := beadID
		run.BeadID = &b
	}
	cycles := core.NewCycleCounter()

	currentNodeID := graph.StartNodeID
	if currentNodeID == "" {
		return dotWorkflowResult{
			success:        false,
			needsAttention: true,
			summary:        "dot: graph has no start_node",
		}
	}

	// iterationCount drives the implementer-initial vs implementer-resume phase
	// selection so a reviewer back-edge resumes the same Claude session (matching
	// the review-loop semantics). It is incremented each time we (re)enter an
	// implementer-class node.
	iterationCount := 0
	var claudeSessionID string

	for visits := 0; visits < dotMaxNodeVisits; visits++ {
		node := nodesByID[currentNodeID]
		if node == nil {
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary:        fmt.Sprintf("dot: edge points at undeclared node %q", currentNodeID),
			}
		}

		// Emit node_dispatch_requested (O-class observability) before handling the
		// node, per event-model.md §8.1.11.
		emitNodeDispatchRequested(ctx, deps.bus, runID, core.NodeID(currentNodeID))

		var outcome core.Outcome

		switch node.Type {
		case core.NodeTypeNonAgentic:
			// Non-agentic node (e.g. noop start/terminal): no agent is dispatched.
			// Synthesize a SUCCESS outcome and follow the single outbound edge.
			// If this node is itself terminal the cascade returns IsTerminal below.
			outcome = core.Outcome{Status: core.OutcomeStatusSuccess}

		case core.NodeTypeAgentic:
			// Agentic node: dispatch the handler into the substrate, then derive
			// the outcome from the run result (HEAD advanced + reviewer verdict).
			isReviewer := nodeIsReviewer(node)
			if !isReviewer {
				iterationCount++
			}
			nodeOutcome, nodeErr := dispatchDotAgenticNode(ctx, deps, runID, beadID,
				beadTitle, beadDescription, wtPath, parentSHA, daemonSocket, node,
				isReviewer, iterationCount, &claudeSessionID,
				resolvedModel, resolvedEffort, extraContext, baseBranch)
			if nodeErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: true,
					summary:        fmt.Sprintf("dot: agentic node %q failed: %v", currentNodeID, nodeErr),
				}
			}
			outcome = nodeOutcome

		case core.NodeTypeGate, core.NodeTypeSubWorkflow:
			// OUT OF SCOPE for hk-9dnak (deterministic failure, reopen the bead).
			//
			// Gate dispatch is blocked on an unresolved spec contradiction between
			// EM-005b (gate deny-routing semantics) and CP-058 (gate_decision
			// payload contract); sub-workflow dispatch is tracked as a separate
			// bead. Attempting either here would bake in a contract that may be
			// reversed, so the driver refuses rather than guesses.
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary: fmt.Sprintf("dot: node %q has unsupported type %q "+
					"(gate/sub-workflow dispatch is out of scope for hk-9dnak; "+
					"gate semantics blocked on EM-005b vs CP-058)", currentNodeID, node.Type),
			}

		default:
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary:        fmt.Sprintf("dot: node %q has unknown type %q", currentNodeID, node.Type),
			}
		}

		if ctx.Err() != nil {
			return dotWorkflowResult{
				success:        false,
				needsAttention: false,
				summary:        fmt.Sprintf("dot: context cancelled at node %q", currentNodeID),
			}
		}

		// Run the cascade to decide the next node (or detect terminal/failure).
		decision := workflow.DecideNextNode(graph, currentNodeID, outcome, run, cycles)
		emitNodeDispatchDecided(ctx, deps.bus, decision.Payload)

		switch {
		case decision.IsTerminal:
			// Reached a terminal node. The cascade reports terminal when the
			// node we just dispatched is in terminal_node_ids; classify success
			// vs needs-attention from the LAST agentic outcome's preferred_label
			// is not available here, so we classify by the terminal node's role
			// recorded during the walk (see terminalSuccess below).
			success := dotTerminalIsSuccess(graph, currentNodeID)
			return dotWorkflowResult{
				success:        success,
				terminalNodeID: currentNodeID,
				needsAttention: !success,
				summary:        fmt.Sprintf("dot: reached terminal node %q", currentNodeID),
			}

		case decision.Failed:
			// Cascade structural failure (no matching edge, WG-012) or traversal
			// cap hit (EM-043). Both terminate the run; cap-hit and BLOCK route to
			// the needs-attention terminal in a well-formed graph, but a genuine
			// no-match is a hard structural failure.
			needsAttention := true
			summary := fmt.Sprintf("dot: cascade failed at node %q: class=%s reason=%s",
				currentNodeID, decision.FailureClass, decision.FailureReason)
			if decision.CompletionReason == "cap_hit" {
				summary = fmt.Sprintf("dot: traversal cap hit at node %q (%s)",
					currentNodeID, decision.FailureReason)
			}
			return dotWorkflowResult{
				success:        false,
				needsAttention: needsAttention,
				summary:        summary,
			}

		case decision.Advance:
			// Increment the per-edge cycle counter so the traversal_cap is
			// enforced on subsequent traversals of this edge (EM-043a). Only
			// capped edges are tracked; uncapped edges Increment is harmless but
			// we restrict to capped edges to bound the counter map.
			incrementCapIfBounded(graph, cycles, runID, currentNodeID, decision.NextNodeID)
			currentNodeID = decision.NextNodeID

		default:
			// DecideNextNode guarantees exactly one of Advance/IsTerminal/Failed.
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary:        fmt.Sprintf("dot: cascade returned no decision at node %q", currentNodeID),
			}
		}
	}

	// Absolute visit bound exceeded — treat as a runaway graph.
	return dotWorkflowResult{
		success:        false,
		needsAttention: true,
		summary:        fmt.Sprintf("dot: exceeded max node visits (%d) — possible unbounded cycle", dotMaxNodeVisits),
	}
}

// dispatchDotAgenticNode dispatches a single agentic node into the substrate,
// mirroring the single-mode / review-loop launch+wait machinery, and derives the
// node's Outcome from the run result.
//
// For reviewer-class nodes it writes review-target.md before launch and reads the
// produced .harmonik/review.json verdict afterward, setting
// outcome.preferred_label to the verdict (APPROVE / REQUEST_CHANGES / BLOCK).
// For implementer-class nodes it requires HEAD to have advanced and returns a
// bare SUCCESS outcome (the outbound edge is unconditional).
func dispatchDotAgenticNode(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	daemonSocket string,
	node *dot.Node,
	isReviewer bool,
	iterationCount int,
	claudeSessionID *string,
	resolvedModel string,
	resolvedEffort string,
	extraContext string,
	baseBranch string,
) (core.Outcome, error) {
	// Reviewer nodes need review-target.md on disk before the kick-off paste so
	// the reviewer has a brief to read (mirrors reviewloop.go WriteReviewTarget).
	if isReviewer {
		headSHA, headErr := resolveWorktreeHEAD(ctx, wtPath)
		if headErr != nil {
			return core.Outcome{}, fmt.Errorf("resolve HEAD before reviewer node %q: %w", node.ID, headErr)
		}
		rtErr := workspace.WriteReviewTarget(workspace.ReviewTargetPayload{
			WorkspacePath: wtPath,
			BeadID:        string(beadID),
			Iteration:     iterationCount,
			BeadTitle:     beadTitle,
			BeadBody:      beadDescription,
			BaseSHA:       parentSHA,
			HeadSHA:       headSHA,
		})
		if rtErr != nil {
			return core.Outcome{}, fmt.Errorf("write review-target for node %q: %w", node.ID, rtErr)
		}
	}

	// Phase selection: reviewer always fresh-session; implementer resumes the
	// prior session on iterations ≥ 2 (back-edge re-entry).
	var phase handlercontract.ReviewLoopPhase
	var priorSess *string
	switch {
	case isReviewer:
		phase = handlercontract.ReviewLoopPhaseReviewer
	case iterationCount <= 1:
		phase = handlercontract.ReviewLoopPhaseImplementerInitial
	default:
		phase = handlercontract.ReviewLoopPhaseImplementerResume
		if *claudeSessionID != "" {
			prior := *claudeSessionID
			priorSess = &prior
		}
	}

	rc := claudeRunCtx{
		runID:             runID,
		beadID:            string(beadID),
		workspacePath:     wtPath,
		daemonSocket:      daemonSocket,
		workflowMode:      core.WorkflowModeDot,
		phase:             phase,
		iterationCount:    iterationCount,
		priorClaudeSessID: priorSess,
		handlerBinary:     deps.handlerBinary,
		daemonBinaryPath:  deps.daemonBinaryPath,
		baseEnv:           deps.handlerEnv,
		beadTitle:         beadTitle,
		beadDescription:   beadDescription,
		model:             resolvedModel,
		effort:            resolvedEffort,
		worktreeRootPath:  workspace.WorktreeRootPath(deps.projectDir, workspace.NoWorktreeRootOverride()),
		extraContext:      extraContext,
		baseBranch:        baseBranch,
	}

	specBuilder := deps.launchSpecBuilder
	if specBuilder == nil {
		specBuilder = buildClaudeLaunchSpec
	}
	spec, artifacts, specErr := specBuilder(ctx, rc)
	if specErr != nil {
		return core.Outcome{}, fmt.Errorf("build launch spec for node %q: %w", node.ID, specErr)
	}
	if len(deps.handlerArgs) > 0 {
		spec.Args = append(deps.handlerArgs, spec.Args...)
	}

	// Attach the optional substrate (nil at MVH / in the deterministic E2E test).
	prs := newPerRunSubstrate(deps.substrate)
	var substrate handler.Substrate = deps.substrate
	var pasteTarget handler.Substrate = deps.substrate
	if prs != nil {
		substrate = prs
		pasteTarget = prs
	}
	spec.Substrate = substrate

	preHeadSHA, _ := resolveWorktreeHEAD(ctx, wtPath)

	if deps.hookStore != nil {
		deps.hookStore.RegisterHookSession(runID.String(), artifacts.claudeSessionID)
	}

	tap, _ := newPerRunEventTap(deps.bus, runID)
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, deps.adapterRegistry)

	sess, watcher, launchErr := runH.Launch(ctx, spec)
	if launchErr != nil {
		if deps.hookStore != nil {
			deps.hookStore.CloseHookSession(runID.String(), artifacts.claudeSessionID)
		}
		return core.Outcome{}, fmt.Errorf("launch node %q: %w", node.ID, launchErr)
	}

	if deps.hookStore != nil {
		capturedTap := tap
		deps.hookStore.SetAgentReadyCallback(runID.String(), artifacts.claudeSessionID, func() {
			_ = capturedTap.Emit(context.Background(), core.EventTypeAgentReady, nil)
		})
	}

	// Paste-inject + quit-on-commit / quit-on-review-file. These are no-ops when
	// the substrate does not implement the relevant interfaces (exec path / the
	// deterministic E2E /bin/sh handler), matching single-mode behavior.
	briefDelivered := pasteInjectOnLaunch(ctx, pasteTarget, artifacts.claudeSessionID,
		phase, iterationCount, wtPath)
	if qs, ok := pasteTarget.(quitSender); ok {
		if isReviewer {
			go pasteInjectQuitOnReviewFile(ctx, qs, sess, wtPath, briefDelivered)
		} else {
			go pasteInjectQuitOnCommit(ctx, qs, sess, wtPath, preHeadSHA, nil, briefDelivered, nil)
		}
	}

	_, _ = waitWithSocketGrace(ctx, deps.hookStore, watcher, sess,
		runID.String(), artifacts.claudeSessionID)

	if watcher == nil {
		_ = sess.Kill(context.Background())
	}
	if deps.hookStore != nil {
		deps.hookStore.CloseHookSession(runID.String(), artifacts.claudeSessionID)
	}

	if ctx.Err() != nil {
		return core.Outcome{}, fmt.Errorf("context cancelled during node %q", node.ID)
	}

	// Capture the claude_session_id for implementer-resume back-edges.
	if !isReviewer && *claudeSessionID == "" {
		*claudeSessionID = artifacts.claudeSessionID
	}

	if isReviewer {
		// Read the produced verdict; its value becomes the preferred_label that
		// drives the reviewer cascade (APPROVE / REQUEST_CHANGES / BLOCK).
		verdict, verdictErr := workspace.ReadReviewVerdict(wtPath)
		if verdictErr != nil {
			return core.Outcome{}, fmt.Errorf("read reviewer verdict for node %q: %w", node.ID, verdictErr)
		}
		if verdict == nil {
			return core.Outcome{}, fmt.Errorf("reviewer node %q produced no verdict", node.ID)
		}
		label := verdict.Verdict
		return core.Outcome{
			Status:         core.OutcomeStatusSuccess,
			PreferredLabel: &label,
		}, nil
	}

	// Implementer-class node: require HEAD to have advanced past its pre-launch
	// state (per EM-015d: the implementer MUST produce a commit). The outbound
	// edge is unconditional, so a bare SUCCESS outcome is returned.
	postHeadSHA, headErr := resolveWorktreeHEAD(ctx, wtPath)
	if headErr != nil {
		return core.Outcome{}, fmt.Errorf("resolve HEAD after node %q: %w", node.ID, headErr)
	}
	if postHeadSHA == preHeadSHA {
		return core.Outcome{}, fmt.Errorf("node %q (implementer) exited without advancing HEAD past %s", node.ID, preHeadSHA)
	}
	return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
}

// nodeIsReviewer reports whether an agentic node is a reviewer-class node. The
// canonical review-loop.dot marks reviewers with agent_type="reviewer"; we also
// accept a handler_ref containing "reviewer" as a fallback.
func nodeIsReviewer(node *dot.Node) bool {
	if node.AgentType == "reviewer" {
		return true
	}
	return node.HandlerRef == "claude-reviewer"
}

// dotTerminalIsSuccess classifies a terminal node as the success terminal vs the
// needs-attention terminal.
//
// Classification rule (matches the canonical review-loop topology): a terminal
// node is the SUCCESS terminal iff at least one inbound edge carries an APPROVE
// preferred-label / condition AND it has no inbound edge from a BLOCK / cap-hit /
// fallback (unconditional) path. In the canonical graph `close` is reached only
// by the APPROVE conditional edge; `close-needs-attention` is reached by the
// BLOCK conditional edge and the unconditional fallback. So: a terminal reachable
// by an unconditional inbound edge is the needs-attention terminal; one reachable
// only by an APPROVE-conditioned edge is the success terminal.
func dotTerminalIsSuccess(graph *dot.Graph, terminalID string) bool {
	hasApproveEdge := false
	hasUnconditionalOrNonApprove := false
	for _, e := range graph.Edges {
		if e.ToNodeID != terminalID {
			continue
		}
		if e.Condition == nil {
			hasUnconditionalOrNonApprove = true
			continue
		}
		if edgeConditionMentionsApprove(e) {
			hasApproveEdge = true
		} else {
			hasUnconditionalOrNonApprove = true
		}
	}
	return hasApproveEdge && !hasUnconditionalOrNonApprove
}

// edgeConditionMentionsApprove reports whether an edge's condition compares the
// preferred_label (or any RHS) to the APPROVE verdict.
func edgeConditionMentionsApprove(e *dot.Edge) bool {
	if e.Condition == nil {
		return false
	}
	for _, c := range e.Condition.Clauses {
		if c.RHS == workspace.ReviewVerdictApprove || c.RHS == "'"+workspace.ReviewVerdictApprove+"'" {
			return true
		}
	}
	return false
}

// incrementCapIfBounded increments the per-edge cycle counter for the traversed
// edge when that edge declares a positive traversal_cap, so subsequent traversals
// are bounded by core.SelectNextEdge's cap check (EM-043 / EM-043a).
func incrementCapIfBounded(graph *dot.Graph, cycles *core.CycleCounter, runID core.RunID, fromID, toID string) {
	for _, e := range graph.Edges {
		if e.FromNodeID != fromID || e.ToNodeID != toID {
			continue
		}
		if cap := dotEdgeTraversalCap(e); cap != nil && *cap > 0 {
			_, _ = cycles.Increment(runID, core.NodeID(fromID), core.NodeID(toID), cap)
		}
		return
	}
}

// dotEdgeTraversalCap parses the traversal_cap attribute (retained in the parsed
// edge's UnknownAttrs per parser.go) into a positive *int, or nil when absent /
// malformed / non-positive. This is the DOT→core traversal_cap bridge for the
// cascade-driver cap-enforcement path (hk-i7yq8).
func dotEdgeTraversalCap(e *dot.Edge) *int {
	raw, ok := e.UnknownAttrs["traversal_cap"]
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return nil
	}
	return &n
}

// graphVersionOr returns the graph's version field or a placeholder when empty
// (WorkflowVersion must be non-empty for a valid core.Run).
func graphVersionOr(graph *dot.Graph) string {
	if graph.Version != "" {
		return graph.Version
	}
	return "0"
}

// emitNodeDispatchRequested emits node_dispatch_requested (event-model.md §8.1.11,
// O-class observability) immediately before a node is handled.
func emitNodeDispatchRequested(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, nodeID core.NodeID) {
	pl := core.NodeDispatchRequestedPayload{
		RunID:       runID,
		NodeID:      nodeID,
		RequestedAt: time.Now().UTC().Format(time.RFC3339),
		Origin:      core.NodeDispatchOriginWorkflow,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeNodeDispatchRequested, b)
}

// emitNodeDispatchDecided emits node_dispatch_decided with the cascade-engine
// payload (event-model.md §8.1.11 / hk-bf85t). The payload is produced by
// workflow.DecideNextNode.
func emitNodeDispatchDecided(ctx context.Context, bus handlercontract.EventEmitter, payload *core.NodeDispatchDecidedPayload) {
	if payload == nil {
		return
	}
	b, err := json.Marshal(*payload)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, payload.RunID, core.EventTypeNodeDispatchDecided, b)
}
