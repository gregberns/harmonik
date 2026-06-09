package daemon

// dot_gate.go — Daemon-side gate evaluator seam for DOT workflow gate nodes (hk-karlz).
//
// Implements the three-part seam described in hk-karlz:
//  1. Resolve gate_ref → Gate ControlPoint via the daemon's cpRegistry.
//  2. For mechanism-tagged gates: evaluate the PolicyExpression against the run
//     context using PolicyExprEvaluator; bool result → GateAction per spec §6.4
//     (true → allow, false → deny).
//  3. For cognition-tagged gates: dispatch a fresh Claude subprocess, write a
//     gate-task.md brief, watch for .harmonik/gate-verdict.json, and read the
//     verdict (analogous to the reviewer path in dispatchDotAgenticNode).
//
// Constructs a handler.GateEvalFunc and calls handler.DispatchGateNode, feeding
// the outcome into DecideNextNode in driveDotWorkflow like any other node.
//
// Spec refs:
//   - specs/control-points.md §4.2 (Gate), §4.7.CP-034b (cost ceiling),
//     §6.4 (expression environment), §7.2 (cognition dispatch path).
//   - specs/execution-model.md §7.5 (DOT dispatch table).
//
// Bead: hk-karlz.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workspace"
	"github.com/gregberns/harmonik/internal/workflow/dot"
)

// gateExprEnv is the typed evaluation environment for mechanism-tagged Gate
// policy expressions, per specs/control-points.md §6.4.
//
// The spec declares: run, outcome, event, context, policy_meta.
// For gate pre-entry dispatch, outcome and event are nil (no outcome has been
// produced yet; gate nodes are not event-triggered). policy_meta is nil at MVH
// (no policy-document metadata is threaded to the daemon in the current
// implementation).
type gateExprEnv struct {
	Run        *core.Run      `expr:"run"`
	Outcome    *core.Outcome  `expr:"outcome"`
	Event      interface{}    `expr:"event"`
	Context    map[string]any `expr:"context"`
	PolicyMeta map[string]any `expr:"policy_meta"`
}

// gateVerdictSchemaVersion is the schema version expected in gate-verdict.json
// files written by cognition gate evaluators.
const gateVerdictSchemaVersion = 1

// gateVerdictRelPath is the worktree-relative path where a cognition gate
// evaluator writes its verdict. Analogous to .harmonik/review.json for reviewers.
const gateVerdictRelPath = ".harmonik/gate-verdict.json"

// gateTaskRelPath is the worktree-relative path of the brief file written for
// cognition gate evaluators. Analogous to .harmonik/review-target.md for reviewers.
const gateTaskRelPath = ".harmonik/gate-task.md"

// gateVerdictJSON is the on-disk format for gate-verdict.json written by
// cognition gate evaluator subprocesses.
type gateVerdictJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason,omitempty"`
}

// gateFileTimeout is the maximum time to wait for gate-verdict.json to appear.
// Override in tests via the var below.
var gateFileTimeout = 10 * time.Minute

// gateFilePollInterval is how often to poll for gate-verdict.json.
var gateFilePollInterval = 2 * time.Second

// dispatchDotGateNode resolves a gate node's ControlPoint, constructs a
// GateEvalFunc, and calls handler.DispatchGateNode. Returns the cascade-ready
// Outcome.
//
// When cpRegistry is nil (no registry loaded in daemon), returns a structural
// eval-failure Outcome (status=FAIL) with nil Go error so the cascade routes it.
// Infrastructure errors (unable to launch subprocess, etc.) are returned as Go
// errors.
func dispatchDotGateNode(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	run *core.Run,
	wtPath string,
	daemonSocket string,
	node *dot.Node,
	iterationCount int,
	resolvedModel string,
	resolvedEffort string,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	extraContext string,
	baseBranch string,
) (core.Outcome, error) {
	gateRef := core.GateRef(node.GateRef)

	// No registry → structural failure; no ControlPoint can be resolved.
	if deps.cpRegistry == nil {
		return gateEvalFailureOutcome("no ControlPoint registry loaded in daemon"), nil
	}

	// Step 1: resolve gate_ref → ControlPoint.
	cp, ok := deps.cpRegistry.LookupByName(string(gateRef))
	if !ok {
		return gateEvalFailureOutcome(fmt.Sprintf("gate_ref %q not found in ControlPoint registry", gateRef)), nil
	}
	if cp.Kind != core.KindGate {
		return gateEvalFailureOutcome(fmt.Sprintf("gate_ref %q resolves to kind=%s, expected Gate", gateRef, cp.Kind)), nil
	}

	// Step 2: construct GateEvalFunc based on evaluator ModeTag.
	var evalFn handler.GateEvalFunc
	switch cp.Evaluator.Mode {
	case core.ModeTagMechanism:
		evalFn = buildMechanismGateEval(cp)
	case core.ModeTagCognition:
		var cogErr error
		evalFn, cogErr = buildCognitionGateEval(deps, runID, cp, wtPath, daemonSocket, node, iterationCount, resolvedModel, resolvedEffort, beadID, beadTitle, beadDescription, extraContext, baseBranch)
		if cogErr != nil {
			return core.Outcome{}, fmt.Errorf("dot: gate node %q: build cognition eval: %w", node.ID, cogErr)
		}
	default:
		return gateEvalFailureOutcome(fmt.Sprintf("gate_ref %q has unknown evaluator mode %q", gateRef, cp.Evaluator.Mode)), nil
	}

	// Step 3: call handler.DispatchGateNode. It invokes evalFn, maps the result
	// to an Outcome, and emits gate_decision_recorded on success.
	result, err := handler.DispatchGateNode(ctx, run, core.NodeID(node.ID), gateRef, evalFn, deps.bus)
	if err != nil {
		return core.Outcome{}, fmt.Errorf("dot: gate node %q: DispatchGateNode: %w", node.ID, err)
	}
	return result.Outcome, nil
}

// gateEvalFailureOutcome returns a FAIL Outcome with failure_class=structural
// and the given reason in Notes. Used for pre-eval failures (no registry, ref
// not found, etc.) that are not the result of the evaluator itself.
func gateEvalFailureOutcome(reason string) core.Outcome {
	fc := core.FailureClassStructural
	return core.Outcome{
		Status:       core.OutcomeStatusFail,
		Kind:         core.OutcomeKindDefault,
		FailureClass: &fc,
		Notes:        "gate dispatch: " + reason,
	}
}

// buildMechanismGateEval builds a GateEvalFunc for a mechanism-tagged Gate.
//
// Per specs/control-points.md §6.4 table:
//   - Gate mechanism expressions return Bool.
//   - true  → GateActionAllow
//   - false → GateActionDeny
//
// The expression is compiled and evaluated against gateExprEnv with a
// harmonik-level cost ceiling (PolicyExprEvaluator, CP-034b).
// DecisionActor is "mechanism" per GateDecisionPayload §3.
func buildMechanismGateEval(cp core.ControlPoint) handler.GateEvalFunc {
	policyEval := core.NewPolicyExprEvaluator(core.DefaultPolicyExprEvaluatorConfig())
	exprText := string(*cp.Evaluator.Expression)
	policyID := cp.Name

	return func(ctx context.Context, run *core.Run, _ core.NodeID, gateRef core.GateRef) (*core.GateDecisionPayload, error) {
		env := gateExprEnv{
			Run:        run,
			Outcome:    nil,
			Event:      nil,
			Context:    run.Context,
			PolicyMeta: nil,
		}

		prog, _, compileErr := policyEval.Compile(exprText, env)
		if compileErr != nil {
			return nil, fmt.Errorf("mechanism gate %q: compile expression: %w", gateRef, compileErr)
		}

		result, evalErr := policyEval.Evaluate(ctx, prog, env)
		if evalErr != nil {
			return nil, fmt.Errorf("mechanism gate %q: evaluate expression: %w", gateRef, evalErr)
		}

		boolVal, ok := result.Value.(bool)
		if !ok {
			return nil, fmt.Errorf("mechanism gate %q: expression returned non-bool %T (want bool per §6.4)", gateRef, result.Value)
		}

		decision := core.GateActionAllow
		if !boolVal {
			decision = core.GateActionDeny
		}

		return &core.GateDecisionPayload{
			PolicyID:      policyID,
			Decision:      decision,
			DecisionActor: "mechanism",
		}, nil
	}
}

// buildCognitionGateEval builds a GateEvalFunc for a cognition-tagged Gate.
//
// The returned GateEvalFunc, when called, dispatches a fresh Claude subprocess
// analogous to the reviewer path:
//  1. Write gate-task.md with gate context and decision instructions.
//  2. Launch Claude with ReviewLoopPhaseReviewer (fresh session, no resume).
//  3. Deliver the gate-evaluator kick-off message via paste inject.
//  4. Watch for gate-verdict.json; send /quit when it appears.
//  5. Wait for session to exit.
//  6. Read and parse gate-verdict.json into a GateDecisionPayload.
//
// DecisionActor is the DelegationPath.Role per GateDecisionPayload §3.
func buildCognitionGateEval(
	deps workLoopDeps,
	runID core.RunID,
	cp core.ControlPoint,
	wtPath string,
	daemonSocket string,
	node *dot.Node,
	iterationCount int,
	resolvedModel string,
	resolvedEffort string,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	extraContext string,
	baseBranch string,
) (handler.GateEvalFunc, error) {
	dp := cp.Evaluator.DelegationPath
	if dp == nil {
		return nil, fmt.Errorf("cognition gate %q: DelegationPath is nil", cp.Name)
	}

	return func(ctx context.Context, run *core.Run, nodeID core.NodeID, gateRef core.GateRef) (*core.GateDecisionPayload, error) {
		return executeCognitionGate(ctx, deps, runID, run, cp, *dp, wtPath, daemonSocket, node, iterationCount, resolvedModel, resolvedEffort, beadID, beadTitle, beadDescription, extraContext, baseBranch, gateRef)
	}, nil
}

// executeCognitionGate performs the actual cognition gate dispatch: write brief,
// launch subprocess, wait, read verdict. Called from the GateEvalFunc closure.
func executeCognitionGate(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	run *core.Run,
	cp core.ControlPoint,
	dp core.DelegationPath,
	wtPath string,
	daemonSocket string,
	node *dot.Node,
	iterationCount int,
	resolvedModel string,
	resolvedEffort string,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	extraContext string,
	baseBranch string,
	gateRef core.GateRef,
) (*core.GateDecisionPayload, error) {
	// Remove any stale verdict from a prior attempt.
	verdictPath := filepath.Join(wtPath, gateVerdictRelPath)
	_ = os.Remove(verdictPath)

	// Write the gate-task.md brief.
	if err := writeCognitionGateTask(wtPath, cp, dp, run, string(beadID), beadTitle, beadDescription, node.ID); err != nil {
		return nil, fmt.Errorf("cognition gate %q: write gate-task.md: %w", gateRef, err)
	}

	// Build launch spec. Use ReviewLoopPhaseReviewer for a fresh session with
	// no resume, mirroring how the reviewer is launched.
	rc := claudeRunCtx{
		runID:             runID,
		beadID:            string(beadID),
		workspacePath:     wtPath,
		daemonSocket:      daemonSocket,
		workflowMode:      core.WorkflowModeDot,
		phase:             handlercontract.ReviewLoopPhaseReviewer,
		iterationCount:    iterationCount,
		priorClaudeSessID: nil,
		handlerBinary:     deps.handlerBinary,
		daemonBinaryPath:  deps.daemonBinaryPath,
		baseEnv:           deps.handlerEnv,
		beadTitle:         beadTitle,
		beadDescription:   beadDescription,
		nodePrompt:        "",
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
		return nil, fmt.Errorf("cognition gate %q: build launch spec: %w", gateRef, specErr)
	}
	if len(deps.handlerArgs) > 0 {
		spec.Args = append(deps.handlerArgs, spec.Args...)
	}

	prs := newPerRunSubstrate(deps.substrate, deps.handlerBinary)
	var substrate handler.Substrate = deps.substrate
	var pasteTarget handler.Substrate = deps.substrate
	if prs != nil {
		substrate = prs
		pasteTarget = prs
	}
	spec.Substrate = substrate

	if deps.hookStore != nil {
		deps.hookStore.RegisterHookSession(runID.String(), artifacts.claudeSessionID)
	}

	tap, tapCh := newPerRunEventTap(deps.bus, runID)
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, deps.adapterRegistry)

	// hk-goczd: emit the CHB-018 pre-exec messages before Launch, holding back
	// launch_initiated for after the window is live — same false-positive
	// launch_stall_detected fix as the DOT cascade path (dot_cascade.go) and the
	// single-mode path (workloop.go:2098/2137). Without this the cognition-gate
	// node never emits launch_initiated and the stale watcher (stalewatch.go:296)
	// flags a phantom launch stall on every gate dispatch.
	gateLaunchInitiatedMsg := emitPreExecBeforeLaunch(ctx, deps.bus, runID, artifacts.preExecMsgs)

	sess, watcher, launchErr := runH.Launch(ctx, spec)
	if launchErr != nil {
		if deps.hookStore != nil {
			deps.hookStore.CloseHookSession(runID.String(), artifacts.claudeSessionID)
		}
		return nil, fmt.Errorf("cognition gate %q: launch: %w", gateRef, launchErr)
	}

	// hk-goczd: window is live — emit the held-back launch_initiated to clear the
	// false stall. Mirrors workloop.go:2137-2139.
	if gateLaunchInitiatedMsg != nil {
		emitPreExecMessage(ctx, deps.bus, runID, gateLaunchInitiatedMsg)
	}

	// hk-goczd / hk-68pvl: slot-reclaim backstop — guarantee the spawn-semaphore
	// slot (hk-xb5yi / hk-4l7zs) is released on EVERY return path. The success path
	// below kills the session only when watcher == nil (dot_gate.go ~line 397);
	// this defer covers the exec path and any early return (agent_ready timeout,
	// ctx-cancel, verdict-read error). Kill is idempotent, so it is a no-op when
	// the session was already torn down.
	defer forceTeardownSession(sess)

	if deps.hookStore != nil {
		capturedTap := tap
		deps.hookStore.SetAgentReadyCallback(runID.String(), artifacts.claudeSessionID, func() {
			_ = capturedTap.Emit(context.Background(), core.EventTypeAgentReady, nil)
		})
	}

	// HC-056: wait for agent_ready before paste-inject.
	adapter, adapterErr := deps.adapterRegistry.ForAgent(core.AgentTypeClaudeCode)
	if adapterErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot: gate: ForAgent(claude-code) node %q: %v (skipping ready-wait)\n",
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
		readyCancel()

		if readyErr == ErrAgentReadyTimeout {
			fmt.Fprintf(os.Stderr, "daemon: dot: gate: waitAgentReady node %q run %s: %v\n",
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
			return nil, fmt.Errorf("cognition gate %q: agent_ready_timeout", gateRef)
		}
	}

	// Deliver gate-evaluator kick-off message and watch for verdict file.
	briefDelivered := pasteInjectCognitionGate(ctx, pasteTarget, artifacts.claudeSessionID, wtPath, deps.bus, runID)
	if qs, ok := pasteTarget.(quitSender); ok {
		go pasteInjectQuitOnGateFile(ctx, qs, sess, wtPath, briefDelivered)
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
		return nil, fmt.Errorf("cognition gate %q: context cancelled", gateRef)
	}

	// Read and parse the gate verdict.
	decision, readErr := readGateVerdict(verdictPath)
	if readErr != nil {
		return nil, fmt.Errorf("cognition gate %q: read verdict: %w", gateRef, readErr)
	}

	actor := dp.Role
	return &core.GateDecisionPayload{
		PolicyID:      cp.Name,
		Decision:      decision,
		DecisionActor: actor,
	}, nil
}

// writeCognitionGateTask writes the gate-task.md brief for a cognition gate
// evaluator subprocess. The brief includes the gate's identity, the delegation
// path role, the run context, and clear instructions for writing gate-verdict.json.
func writeCognitionGateTask(
	wtPath string,
	cp core.ControlPoint,
	dp core.DelegationPath,
	run *core.Run,
	beadID string,
	beadTitle string,
	beadDescription string,
	nodeID string,
) error {
	taskPath := filepath.Join(wtPath, gateTaskRelPath)
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	ctxJSON, _ := json.MarshalIndent(run.Context, "", "  ")

	content := fmt.Sprintf(`# Gate Evaluation Task

## Gate Identity

- **gate_name**: %s
- **node_id**: %s
- **role**: %s
- **model_class**: %s

## Bead Context

- **bead_id**: %s
- **title**: %s

%s

## Run Context

`+"```json\n%s\n```"+`

## Your Task

You are acting as a gate evaluator in the role of **%s**.

Evaluate whether the current state of the worktree satisfies the gate policy.
Examine the code, tests, and any relevant artifacts in this worktree.

Write your decision to **%s** as a JSON object with the following schema:

`+"```json\n"+`{
  "schema_version": 1,
  "decision": "allow",
  "reason": "Brief explanation of your decision."
}
`+"```"+`

The **decision** field MUST be exactly one of:
- **allow** — the gate condition is satisfied; the workflow may proceed.
- **deny** — the gate condition is NOT satisfied; the workflow should not proceed.
- **escalate-to-human** — you cannot determine the outcome; human review is required.

Write the JSON file now, then exit with /quit.
`,
		cp.Name,
		nodeID,
		dp.Role,
		dp.ModelClass,
		beadID,
		beadTitle,
		beadDescription,
		string(ctxJSON),
		dp.Role,
		gateVerdictRelPath,
	)

	return os.WriteFile(taskPath, []byte(content), 0o644)
}

// readGateVerdict reads and parses gate-verdict.json, returning the GateAction.
func readGateVerdict(verdictPath string) (core.GateAction, error) {
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", verdictPath, err)
	}

	var v gateVerdictJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("unmarshal gate-verdict.json: %w", err)
	}

	if v.SchemaVersion != gateVerdictSchemaVersion {
		return "", fmt.Errorf("gate-verdict.json: schema_version=%d, want %d", v.SchemaVersion, gateVerdictSchemaVersion)
	}

	action := core.GateAction(v.Decision)
	if !action.Valid() {
		return "", fmt.Errorf("gate-verdict.json: decision=%q is not a valid GateAction (must be allow, deny, or escalate-to-human)", v.Decision)
	}

	return action, nil
}

// pasteInjectCognitionGate delivers the gate-evaluator kick-off message.
// Analogous to pasteInjectReviewer for the reviewer path.
// Returns a channel closed once the kick-off paste has been written.
func pasteInjectCognitionGate(
	ctx context.Context,
	substrate handler.Substrate,
	claudeSessID string,
	wtPath string,
	bus handlercontract.EventEmitter,
	runID core.RunID,
) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		if substrate == nil {
			return
		}
		inj, ok := substrate.(pasteInjecter)
		if !ok {
			return
		}

		taskFile := filepath.Join(wtPath, gateTaskRelPath)
		if err := statTaskFile(taskFile); err != nil {
			reason := fmt.Sprintf("cognition-gate: %v", err)
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s (skipping inject)\n", reason)
			if bus != nil {
				emitPasteInjectFailed(ctx, bus, runID, "gate-evaluator", reason)
			}
			return
		}

		// Dismiss welcome splash before paste (mirrors reviewer path).
		if es, ok := inj.(enterSender); ok {
			if err := es.SendEnterToLastPane(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: pasteinject: cognition-gate SendEnterToLastPane: %v\n", err)
			}
			splashDismissWait(ctx)
		}

		bufName := bufferName(claudeSessID, "gate")
		msg := "Read " + gateTaskRelPath + " in this worktree." +
			" It contains the gate evaluation request, the run context, and your role." +
			" Evaluate the gate and write your decision to " + gateVerdictRelPath +
			" as JSON: {\"schema_version\":1,\"decision\":\"allow|deny|escalate-to-human\",\"reason\":\"...\"}." +
			" The decision field MUST be exactly one of: allow, deny, escalate-to-human." +
			" After writing the verdict file, exit with /quit.\n"

		if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
			reason := fmt.Sprintf("cognition-gate WriteLastPane: %v", err)
			fmt.Fprintf(os.Stderr, "daemon: pasteinject: %s\n", reason)
			if bus != nil {
				emitPasteInjectFailed(ctx, bus, runID, "gate-evaluator", reason)
			}
			return
		}
		if es, ok := inj.(enterSender); ok {
			if err := es.SendEnterToLastPane(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: pasteinject: cognition-gate post-paste SendEnterToLastPane: %v\n", err)
			}
		}
	}()
	return ch
}

// pasteInjectQuitOnGateFile watches for gate-verdict.json to appear, then
// sends /quit to terminate the gate evaluator session. Analogous to
// pasteInjectQuitOnReviewFile for the reviewer path.
func pasteInjectQuitOnGateFile(
	ctx context.Context,
	qs quitSender,
	killer sessionKiller,
	wtPath string,
	briefDelivered <-chan struct{},
) {
	if briefDelivered != nil {
		select {
		case <-ctx.Done():
			return
		case <-briefDelivered:
		case <-time.After(briefDeliveredTimeout):
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-gate-file: brief_delivered timeout for %s; proceeding\n", wtPath)
		}
	}

	verdictPath := filepath.Join(wtPath, gateVerdictRelPath)
	deadline := time.Now().Add(gateFileTimeout)
	ticker := time.NewTicker(gateFilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-gate-file: timeout waiting for %s; sending /quit\n", verdictPath)
				_ = qs.SendQuitToLastPane(ctx)
				select {
				case <-ctx.Done():
				case <-time.After(noChangeKillDelay):
				}
				if killer != nil {
					_ = killer.Kill(ctx)
				}
				return
			}
			if info, err := os.Stat(verdictPath); err == nil && info.Size() > 0 {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-gate-file: verdict detected at %s; sending /quit\n", verdictPath)
				_ = qs.SendQuitToLastPane(ctx)
				select {
				case <-ctx.Done():
				case <-time.After(postQuitKillGrace):
				}
				if killer != nil {
					_ = killer.Kill(ctx)
				}
				return
			}
		}
	}
}
