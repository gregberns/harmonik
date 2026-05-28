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
//   - gate: the gate-decision SEMANTICS are now resolved (CP-058 wins; a gate
//     deny/allow/escalate is status=SUCCESS, the cascade routes on the decision
//     surfaced via outcome.preferred_label; see handler.DispatchGateNode). What is
//     still missing is the daemon-side EVALUATOR seam: there is no GateEvalFunc
//     provider, no ControlPoint-registry loading from the daemon, and no
//     mechanism/cognition policy resolution. Wiring a real evaluator (resolve
//     gate_ref → ControlPoint, run the policy expression for mechanism gates or
//     dispatch+read-verdict for cognition gates) is substantial infrastructure
//     that does not exist yet, tracked as bead hk-karlz. Rather than fabricate a
//     fake evaluator (which would make tests green without real semantics), the
//     driver returns a deterministic needs-attention failure for gate nodes,
//     citing hk-karlz. The handler-layer dispatch + cascade routing IS exercised
//     by handler.DispatchGateNode's tests and the DecideNextNode routing tests.
//   - sub-workflow: OUT OF SCOPE (separate bead). Same deterministic-failure
//     treatment.
//
// # Terminal handling
//
// The walk ends when DecideNextNode reports the current node is terminal (it is
// in graph.TerminalNodeIDs). The driver classifies the terminal node by its
// IDENTITY per WG-021/WG-022: reaching "close" (or any terminal that is NOT
// "close-needs-attention") is the success path; reaching "close-needs-attention"
// is the needs-attention path. This is the spec-mandated surface — consumers
// MUST NOT inspect inbound-edge topology to determine terminal disposition.
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
	"os"
	"os/exec"
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
			switch {
			case node.ToolCommand != "" && node.HandlerRef == "shell":
				// Path 1: shell tool node — execute tool_command via the built-in
				// in-process shell handler (WG-039 / HC-063). MAY run in-process;
				// no subprocess/socket/NDJSON/agent_ready/heartbeat required.
				toolOutcome, toolErr := dispatchDotToolNode(ctx, wtPath, node, deps.handlerEnv)
				if toolErr != nil {
					return dotWorkflowResult{
						success:        false,
						needsAttention: true,
						summary:        fmt.Sprintf("dot: tool node %q dispatch error: %v", currentNodeID, toolErr),
					}
				}
				outcome = toolOutcome

			case node.ToolCommand != "" && node.HandlerRef != "shell":
				// Path 3: non-agentic node bound to a non-shell handler — v1 stub.
				// The tool_command warning was already emitted at load/validate time
				// (WG-031). Non-shell non-agentic handlers are out of scope at v1;
				// the branch structure exists to avoid silent misrouting.
				// Fall through to a bare SUCCESS synth so the graph can still run.
				outcome = core.Outcome{Status: core.OutcomeStatusSuccess}

			default:
				// Path 2: no tool_command — preserve today's SUCCESS synth (noop
				// start/terminal pass-through). If the node is itself terminal the
				// cascade returns IsTerminal below.
				outcome = core.Outcome{Status: core.OutcomeStatusSuccess}
			}

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

		case core.NodeTypeGate:
			// Gate-decision SEMANTICS are resolved (CP-058: allow/deny/escalate are
			// all status=SUCCESS, routed on the decision via preferred_label; the
			// handler.DispatchGateNode + DecideNextNode path is implemented and
			// tested). The remaining gap is the daemon-side EVALUATOR seam: there is
			// no GateEvalFunc provider, no ControlPoint-registry loading here, and no
			// mechanism/cognition policy resolution. Wiring a real evaluator is
			// substantial infrastructure tracked as hk-karlz. We refuse rather than
			// fabricate a fake evaluator that would green tests without real gate
			// semantics. TODO(hk-karlz): construct a GateEvalFunc from the resolved
			// ControlPoint and call handler.DispatchGateNode, feeding the outcome
			// into DecideNextNode like any other node.
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary: fmt.Sprintf("dot: gate node %q cannot be dispatched yet — "+
					"the gate-decision semantics are resolved (CP-058) but the "+
					"daemon-side gate evaluator (GateEvalFunc provider) is not wired "+
					"(hk-karlz)", currentNodeID),
			}

		case core.NodeTypeSubWorkflow:
			// OUT OF SCOPE (separate bead): deterministic needs-attention failure.
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary: fmt.Sprintf("dot: sub-workflow node %q dispatch is out of "+
					"scope (separate bead)", currentNodeID),
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
			// Reached a terminal node. Classify by terminal node IDENTITY per
			// WG-021/WG-022: "close-needs-attention" → needs-attention; any other
			// terminal (including "close" and author-defined terminals) → success.
			// Inspecting inbound-edge topology is forbidden by WG-021.
			success := dotTerminalNodeIsSuccess(currentNodeID)
			return dotWorkflowResult{
				success:        success,
				terminalNodeID: currentNodeID,
				needsAttention: !success,
				summary:        fmt.Sprintf("dot: reached terminal node %q", currentNodeID),
			}

		case decision.Failed:
			// Cascade structural failure (no matching edge, WG-012) or traversal
			// cap hit (EM-043). Both terminate the run here by reopening the bead
			// (needs-attention) — SelectNextEdge returns Failed on cap-hit rather
			// than dropping the capped edge and re-selecting an unconditional
			// fallback, so cap-hit does NOT reach a terminal node; it ends as a
			// reopen, same as a genuine no-match structural failure.
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

	tap, tapCh := newPerRunEventTap(deps.bus, runID)
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

	// HC-056: waitAgentReady — the paste-inject below MUST run AFTER agent_ready
	// is observed, exactly as the single-mode (workloop.go step 6) and review-loop
	// (reviewloop.go) dispatch paths do. When paste-inject fires before the pane's
	// REPL input state is active, Claude Code's welcome splash consumes the
	// trailing Enter, the kick-off message sits typed-but-unsubmitted in the input
	// bar, claude never reads agent-task.md, and the run idles until the
	// stale-watcher fires (no commit). This gate was the missing step that left
	// DOT-mode dispatches hung at an unsent prompt (hk-3qjwl).
	//
	// Mirrors workloop.go:1496-1580 / reviewloop.go:339-399: derive a child context
	// that cancels when the watcher finishes (so a handler crash does not block for
	// the full timeout), wait, then handle the HC-056 timeout sentinel by killing +
	// erroring so the cascade reopens the bead rather than hanging.
	//
	// Substrate path: watcher is nil for tmux-hosted sessions; the watcher-done
	// goroutine is skipped and the wait relies on ctx / the timeout alone.
	//
	// Spec ref: specs/handler-contract.md §4.9 HC-056;
	//           specs/process-lifecycle.md §4.7 PL-021d.
	adapter, adapterErr := deps.adapterRegistry.ForAgent(core.AgentTypeClaudeCode)
	if adapterErr != nil {
		// No adapter for claude-code — non-fatal; skip ready-wait (matches the
		// other two dispatch paths).
		fmt.Fprintf(os.Stderr, "daemon: dot: ForAgent(claude-code) node %q: %v (skipping ready-wait)\n",
			node.ID, adapterErr)
	} else {
		readyCtx, readyCancel := context.WithCancel(ctx)
		if watcher != nil {
			go func() {
				select {
				case <-watcher.Done():
					readyCancel()
				case <-readyCtx.Done():
				}
			}()
		}

		eventSrc := newChanAgentEventSource(tapCh)
		readyErr := waitAgentReady(readyCtx, runID, eventSrc, adapter, deps.agentReadyTimeout)
		readyCancel() // always release the watcher-done goroutine above

		if readyErr == ErrAgentReadyTimeout {
			fmt.Fprintf(os.Stderr, "daemon: dot: waitAgentReady node %q run %s: %v (failing node)\n",
				node.ID, runID.String(), readyErr)
			_ = sess.Kill(ctx)
			if watcher != nil {
				select {
				case <-watcher.Done():
				case <-time.After(agentReadyKillReapTimeout):
				}
			}
			_ = sess.Wait(ctx)
			if deps.hookStore != nil {
				deps.hookStore.CloseHookSession(runID.String(), artifacts.claudeSessionID)
			}
			emitAgentReadyTimeout(ctx, deps.bus, runID, artifacts.claudeSessionID, deps.agentReadyTimeout)
			return core.Outcome{}, fmt.Errorf("node %q agent_ready_timeout", node.ID)
		}
		// readyErr == nil (agent_ready observed) OR context.Canceled (watcher
		// exited first / ctx cancelled). Fall through to paste-inject.
	}

	// Paste-inject + quit-on-commit / quit-on-review-file. These are no-ops when
	// the substrate does not implement the relevant interfaces (exec path / the
	// deterministic E2E /bin/sh handler), matching single-mode behavior.
	//
	// MUST run AFTER waitAgentReady above (hk-3qjwl): pasteInjectOnLaunch sends the
	// kick-off message and the submitting Enter via SendEnterToLastPane (hk-8cq23);
	// firing it before the REPL is input-ready leaves the prompt unsubmitted.
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

// dispatchDotToolNode executes a non-agentic shell node's tool_command in-process
// via /bin/sh -c. It is the built-in shell handler per WG-039 / HC-063.
//
// Exit-state → Outcome mapping (HC-063 §III.1 / EM-057 item 7 / EM-058):
//   - exit 0              → SUCCESS (kind=default, no payload)
//   - exit 1..255         → FAIL + failure_class=deterministic
//   - timeout kill        → FAIL + failure_class=transient
//   - signal-kill / ctx   → FAIL + failure_class=canceled
//
// Default axis_tags for shell: io-determinism=non-deterministic, replay-safety=unsafe.
// No RETRY or PARTIAL outcomes are produced; the author routes on FAIL sub-classes
// via edge conditions if needed.
func dispatchDotToolNode(ctx context.Context, wtPath string, node *dot.Node, env []string) (core.Outcome, error) {
	timeoutSecs := 300
	if node.Timeout != "" {
		if n, err := strconv.Atoi(node.Timeout); err == nil && n > 0 {
			timeoutSecs = n
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "/bin/sh", "-c", node.ToolCommand)
	cmd.Dir = wtPath
	cmd.Env = env

	err := cmd.Run()
	if err == nil {
		return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
	}

	// Timeout-killed: parent deadline exceeded first.
	if execCtx.Err() == context.DeadlineExceeded {
		fc := core.FailureClassTransient
		return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc}, nil
	}

	// Parent context cancelled (operator stop / SIGKILL / ctx-cancel).
	if ctx.Err() != nil {
		fc := core.FailureClassCanceled
		return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc}, nil
	}

	// Non-zero exit code (1..255) → deterministic failure.
	fc := core.FailureClassDeterministic
	return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc}, nil
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

// dotTerminalNodeIsSuccess classifies a terminal node as the success terminal
// by its ID per WG-021/WG-022.
//
// Rule: "close-needs-attention" is the reserved needs-attention terminal.
// Any other terminal ID — including the reserved "close" and author-defined
// extensions per WG-022 — is treated as a success terminal. Inspecting
// inbound-edge topology to infer disposition is forbidden by WG-021.
func dotTerminalNodeIsSuccess(terminalID string) bool {
	return terminalID != "close-needs-attention"
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
