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
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/shared"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/policy"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

// The mechanism gate evaluation environment (GateExprEnv), the bool→GateAction
// mapping (MechanismDecision), the gate-verdict.json parse (ParseGateVerdict),
// and the pre-eval structural-failure Outcome (GateEvalFailureOutcome) are the
// pure DECISION predicates — they live in internal/policy (M5 slice 2 sub-slice
// C). This file is the daemon shell that threads every effect in.

// gateVerdictRelPath is the worktree-relative path where a cognition gate
// evaluator writes its verdict. Analogous to .harmonik/review.json for reviewers.
const gateVerdictRelPath = ".harmonik/gate-verdict.json"

// gateTaskRelPath is the worktree-relative path of the brief file written for
// cognition gate evaluators. Analogous to .harmonik/review-target.md for reviewers.
const gateTaskRelPath = ".harmonik/gate-task.md"

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
	// beadRecord carries the tier-1 harness:<agent-type> LABEL. hk-01vs0 needs it
	// to compute (quietly) the harness the cognition gate WOULD inherit, so a
	// reviewer-class gate never lands on a SessionIDCaptured harness.
	beadRecord core.BeadRecord,
	beadTitle string,
	beadDescription string,
	extraContext string,
	baseBranch string,
	runner ltmux.CommandRunner,
	// remote-substrate: worker-launch params threaded the same way as
	// dispatchDotAgenticNode (dot_cascade.go) — all empty for a LOCAL run,
	// byte-identical to the pre-hk-9fe2 box-A-only path (NFR7).
	workerBinaryPath string,
	workerSessionName string,
	workerSessionCwd string,
) (core.Outcome, error) {
	gateRef := core.GateRef(node.GateRef)

	// Step 1: resolve gate_ref → ControlPoint via the GatePort (RSM-010).
	cp, ok, registryLoaded := deps.runPorts().Gate.LookupGate(gateRef)
	// No registry → structural failure; no ControlPoint can be resolved.
	if !registryLoaded {
		return policy.GateEvalFailureOutcome("no ControlPoint registry loaded in daemon"), nil
	}
	if !ok {
		return policy.GateEvalFailureOutcome(fmt.Sprintf("gate_ref %q not found in ControlPoint registry", gateRef)), nil
	}
	if cp.Kind != core.KindGate {
		return policy.GateEvalFailureOutcome(fmt.Sprintf("gate_ref %q resolves to kind=%s, expected Gate", gateRef, cp.Kind)), nil
	}

	// Step 2: construct GateEvalFunc based on evaluator ModeTag.
	var evalFn handler.GateEvalFunc
	switch cp.Evaluator.Mode {
	case core.ModeTagMechanism:
		evalFn = buildMechanismGateEval(cp)
	case core.ModeTagCognition:
		var cogErr error
		evalFn, cogErr = buildCognitionGateEval(deps, runID, cp, wtPath, daemonSocket, node, iterationCount, resolvedModel, resolvedEffort, beadID, beadRecord, beadTitle, beadDescription, extraContext, baseBranch, runner, workerBinaryPath, workerSessionName, workerSessionCwd)
		if cogErr != nil {
			return core.Outcome{}, fmt.Errorf("dot: gate node %q: build cognition eval: %w", node.ID, cogErr)
		}
	default:
		return policy.GateEvalFailureOutcome(fmt.Sprintf("gate_ref %q has unknown evaluator mode %q", gateRef, cp.Evaluator.Mode)), nil
	}

	// Step 3: call handler.DispatchGateNode. It invokes evalFn, maps the result
	// to an Outcome, and emits gate_decision_recorded on success.
	result, err := handler.DispatchGateNode(ctx, run, core.NodeID(node.ID), gateRef, evalFn, deps.emitterPort())
	if err != nil {
		return core.Outcome{}, fmt.Errorf("dot: gate node %q: DispatchGateNode: %w", node.ID, err)
	}
	return result.Outcome, nil
}

// buildMechanismGateEval builds a GateEvalFunc for a mechanism-tagged Gate.
//
// Per specs/control-points.md §6.4 table:
//   - Gate mechanism expressions return Bool.
//   - true  → GateActionAllow
//   - false → GateActionDeny
//
// The expression is compiled and evaluated against policy.GateExprEnv with a
// harmonik-level cost ceiling (PolicyExprEvaluator, CP-034b).
// DecisionActor is "mechanism" per GateDecisionPayload §3.
func buildMechanismGateEval(cp core.ControlPoint) handler.GateEvalFunc {
	policyEval := core.NewPolicyExprEvaluator(core.DefaultPolicyExprEvaluatorConfig())
	exprText := string(*cp.Evaluator.Expression)
	policyID := cp.Name

	return func(ctx context.Context, run *core.Run, _ core.NodeID, gateRef core.GateRef) (*core.GateDecisionPayload, error) {
		env := policy.GateExprEnv{
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

		decision := policy.MechanismDecision(boolVal)

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
	beadRecord core.BeadRecord, // hk-01vs0: tier-1 harness label source
	beadTitle string,
	beadDescription string,
	extraContext string,
	baseBranch string,
	runner ltmux.CommandRunner,
	workerBinaryPath string,
	workerSessionName string,
	workerSessionCwd string,
) (handler.GateEvalFunc, error) {
	dp := cp.Evaluator.DelegationPath
	if dp == nil {
		return nil, fmt.Errorf("cognition gate %q: DelegationPath is nil", cp.Name)
	}

	return func(ctx context.Context, run *core.Run, nodeID core.NodeID, gateRef core.GateRef) (*core.GateDecisionPayload, error) {
		return executeCognitionGate(ctx, deps, runID, run, cp, *dp, wtPath, daemonSocket, node, iterationCount, resolvedModel, resolvedEffort, beadID, beadRecord, beadTitle, beadDescription, extraContext, baseBranch, gateRef, runner, workerBinaryPath, workerSessionName, workerSessionCwd)
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
	beadRecord core.BeadRecord, // hk-01vs0: tier-1 harness label source
	beadTitle string,
	beadDescription string,
	extraContext string,
	baseBranch string,
	gateRef core.GateRef,
	runner ltmux.CommandRunner,
	workerBinaryPath string,
	workerSessionName string,
	workerSessionCwd string,
) (*core.GateDecisionPayload, error) {
	// RSM-013 / M3-D4: default the run-path clock port for struct-literal test
	// deps that predate the field; newWorkLoopDeps wires SystemClock in prod.
	// deps is by-value, so this default propagates to every downstream site.
	// RT14: this site needs it because the dispatch segment is ClockPort-timed,
	// where the open-coded ready wait it replaces used a raw time.After. The
	// three other segment consumers (reviewloop.go, dot_cascade.go ×2) already
	// carry the identical backstop; this is the last one.
	if deps.clock == nil {
		deps.clock = substrate.SystemClock{}
	}
	// RSM-010: the run's EmitterPort, bound once for this call. Deliberately the
	// NARROW emitterPort accessor (runports.go) and not the runPorts() bundle,
	// which also reads the clock port — the default set just above must not be
	// bypassed by an earlier bundle read.
	emit := deps.emitterPort()
	// Remove any stale verdict from a prior attempt. Routed through runner so a
	// REMOTE run (runner != nil) clears the verdict on the WORKER's filesystem,
	// not box A's (hk-9fe2).
	verdictPath := filepath.Join(wtPath, gateVerdictRelPath)
	_ = workspace.RemoveFileVia(ctx, runner, verdictPath)

	// Write the gate-task.md brief. Routed through runner so a REMOTE run
	// (runner != nil) writes the brief onto the WORKER's filesystem — a
	// box-A-local write would leave the worker's gate evaluator with no brief
	// to read (hk-9fe2).
	if err := writeCognitionGateTask(ctx, runner, wtPath, cp, dp, run, string(beadID), beadTitle, beadDescription, node.ID); err != nil {
		return nil, fmt.Errorf("cognition gate %q: write gate-task.md: %w", gateRef, err)
	}

	// Build launch spec. Use ReviewLoopPhaseReviewer for a fresh session with
	// no resume, mirroring how the reviewer is launched.
	rc := shared.LaunchCtx{
		RunID:         runID,
		BeadID:        string(beadID),
		WorkspacePath: wtPath,
		// remote-substrate (hk-9fe2): thread the run's CommandRunner + worker
		// harmonik path into the cognition-gate's shared.LaunchCtx the same way
		// dispatchDotAgenticNode does (dot_cascade.go), so the trust/settings/
		// agent-task materialization writes land on the WORKER for a REMOTE
		// DOT run and stay box-A-local for a LOCAL run (runner == nil, NFR7).
		Runner:            runner,
		WorkerBinaryPath:  workerBinaryPath,
		DaemonSocket:      daemonSocket,
		WorkflowMode:      core.WorkflowModeDot,
		Phase:             handlercontract.ReviewLoopPhaseReviewer,
		IterationCount:    iterationCount,
		PriorClaudeSessID: nil,
		HandlerBinary:     deps.handlerBinary,
		DaemonBinaryPath:  deps.daemonBinaryPath,
		BaseEnv:           deps.handlerEnv,
		BeadTitle:         beadTitle,
		BeadDescription:   beadDescription,
		NodePrompt:        "",
		Model:             resolvedModel,
		Effort:            resolvedEffort,
		WorktreeRootPath:  workspace.WorktreeRootPath(deps.projectDir, workspace.NoWorktreeRootOverride()),
		ExtraContext:      extraContext,
		BaseBranch:        baseBranch,
	}

	// hk-01vs0: the cognition gate is REVIEWER-CLASS — it launches with
	// ReviewLoopPhaseReviewer, is briefed with .harmonik/gate-task.md, and must
	// write .harmonik/gate-verdict.json. It therefore must never run on a
	// SessionIDCaptured harness (codex, pi), for exactly the two reasons
	// reviewerharness_hkiv748.go documents for reviewers:
	//   - codexlaunchspec.go emits ONLY an IMPLEMENTER seed prompt and never reads
	//     rc.phase, so a codex "gate evaluator" is told to implement the bead and
	//     never learns gate-task.md exists, let alone writes a verdict;
	//   - codex never emits agent_ready, but the waitAgentReady below blocks on it,
	//     so the gate dies at "cognition gate %q: agent_ready_timeout".
	//
	// Before this fix the gate took deps.launchSpecBuilder UNCONDITIONALLY. That
	// builder is routedLaunchSpecBuilder(reg, beadRecord, …) (workloop.go), whose
	// tier-1 leg returns a per-bead `harness:codex` LABEL immediately
	// (harnessresolve.go) — so a single labelled bead, not just a global codex
	// default, routed the gate onto codex. This is the third site of the
	// "reviewer silently inherits a harness that cannot review" class; hk-pkxju
	// closed reviewloop.go and dot_cascade.go and left this one out of scope.
	//
	// Reuse of dotReviewerInheritedHarnessOverride (the DOT-cascade adapter) rather
	// than raw reviewerDefaultHarness: the correction needs the harness the gate
	// WOULD have inherited, which is the same quiet tier-1/tier-4 walk the adapter
	// already performs; calling reviewerDefaultHarness directly would mean
	// duplicating that walk here. Both pin arguments are deliberately empty:
	//   - reviewer_harness= is an attribute of an IMPLEMENTER node naming its
	//     reviewer; no implementer node feeds a gate node, so it never applies.
	//   - node.Harness is read ONLY by dispatchDotAgenticNode (dot_cascade.go). The
	//     gate path has never consulted it, so there is no operator pin to protect
	//     here — passing it would merely re-open the inherit hole for any gate node
	//     that happens to carry harness=. Teaching gate nodes to honour a harness=
	//     pin is a separate feature, not this fix.
	// Non-empty return ⇒ pin via pinnedHarnessLaunchSpecBuilder, NOT
	// routedLaunchSpecBuilder: the latter re-runs resolveHarness and would let the
	// tier-1 `harness:codex` bead label override the correction (the hk-2jxqg
	// footgun). Empty return ⇒ deps.launchSpecBuilder stands untouched, so an
	// all-claude run is byte-identical to pre-hk-01vs0 behaviour.
	specBuilder := deps.launchSpecBuilder
	gateInheritedHarness := dotReviewerInheritedHarnessOverride(
		deps.harnessRegistry,
		true,               // a cognition gate is reviewer-class by construction
		core.AgentType(""), // reviewer_harness=: never applies to a gate node
		core.AgentType(""), // node.Harness: not a gate-path mechanism (see above)
		beadRecord,
		deps.defaultHarness,
		string(beadID),
	)
	if gateInheritedHarness.Valid() && deps.harnessRegistry != nil {
		specBuilder = pinnedHarnessLaunchSpecBuilder(
			deps.harnessRegistry,
			beadRecord,
			gateInheritedHarness,
			emit,
		)
	}
	if specBuilder == nil {
		specBuilder = claude.BuildLaunchSpec
	}
	spec, artifacts, specErr := specBuilder(ctx, rc)
	if specErr != nil {
		return nil, fmt.Errorf("cognition gate %q: build launch spec: %w", gateRef, specErr)
	}
	if len(deps.handlerArgs) > 0 {
		spec.Args = append(deps.handlerArgs, spec.Args...)
	}

	// remote-substrate (hk-9fe2): thread the run's runner (SSHRunner for remote,
	// nil for local) into the per-run substrate so a REMOTE cognition-gate node
	// spawns on the WORKER, mirroring dispatchDotAgenticNode (dot_cascade.go).
	// LATENT: the default workflow.dot uses a tool-command commit_gate, not a
	// cognition gate, so no live remote run exercises this path today.
	// hk-qxvc2: the cognition-gate node runs a claude (SessionIDMinted) evaluator;
	// route it onto the tmux/claude substrate, never the codexdriver app-server
	// substrate. runner (SSHRunner/remote) is preserved so a remote gate still
	// spawns on the worker (hk-9fe2).
	gateHarnessIsClaude := true
	if deps.harnessRegistry != nil {
		if h, hErr := deps.harnessRegistry.ForAgent(shared.ArtifactAgentType(artifacts)); hErr == nil {
			gateHarnessIsClaude = h.SessionIDPolicy() == handlercontract.SessionIDMinted
		}
	}
	gateBaseSubstrate := deps.substrate
	if gateHarnessIsClaude && deps.reviewerSubstrate != nil {
		gateBaseSubstrate = deps.reviewerSubstrate
	}
	prs := newPerRunSubstrate(gateBaseSubstrate, deps.handlerBinary, runner)
	runSubstrate := gateBaseSubstrate
	var pasteTarget handler.Substrate = gateBaseSubstrate
	if prs != nil {
		runSubstrate = prs
		pasteTarget = prs
		if runner != nil && workerSessionName != "" {
			prs.workerSessionName = workerSessionName
			prs.workerSessionCwd = workerSessionCwd
		}
	}
	spec.Substrate = runSubstrate

	if deps.hookStore != nil {
		deps.hookStore.RegisterHookSession(runID.String(), artifacts.ClaudeSessionID)
	}

	tap, tapCh := newPerRunEventTap(emit, runID)
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, deps.adapterRegistry)

	// hk-goczd: emit the CHB-018 pre-exec messages before Launch, holding back
	// launch_initiated for after the window is live — same false-positive
	// launch_stall_detected fix as the DOT cascade path (dot_cascade.go) and the
	// single-mode path (workloop.go:2098/2137). Without this the cognition-gate
	// node never emits launch_initiated and the stale watcher (stalewatch.go:296)
	// flags a phantom launch stall on every gate dispatch.
	gateLaunchInitiatedMsg := runlaunch.EmitPreExecBeforeLaunch(ctx, emit, runID, artifacts.PreExecMsgs)

	// RT14: predeclared so the dispatch segment's launch / onLaunched hooks can
	// assign them from inside their closures. Safe because RunDispatch drives
	// every effector inline on this goroutine (runshell.go RunDispatch).
	var sess handler.Session
	var watcher *handlercontract.Watcher
	var launchErr error
	var gateHBDone chan struct{}

	// HC-056: the adapter supplies DetectReady for the segment's ready pump.
	// hk-01vs0: the cognition gate is claude-pinned, so the agent type is
	// hardcoded and there is no completionMode to resolve — the gate never runs a
	// ProcessExit harness, hence cfg.SkipReadyHandshake stays false.
	adapter, adapterErr := deps.adapterRegistry.ForAgent(core.AgentTypeClaudeCode)
	if adapterErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot: gate: ForAgent(claude-code) node %q: %v (skipping ready-wait)\n",
			node.ID, adapterErr)
		adapter = nil
	}

	gateSeg := &dispatchSegment{
		clock: deps.clock,
		runID: runID,
		cfg: runexec.DispatchConfig{
			SkipReadyHandshake: false,
			IsResume:           false,
			MaxInputAttempts:   1,
			// hk-96d7w: runner != nil marks a REMOTE (SSH worker) run — longer window.
			ReadyTimeout:  runlaunch.EffectiveAgentReadyTimeout(deps.agentReadyTimeout, deps.remoteAgentReadyTimeout, runner != nil),
			InputAck:      dispatchSegmentInputAckWindow,
			ReadyKillReap: runlaunch.KillReapTimeout,
		},
		// nil adapter (no claude-code adapter registered) → the segment feeds a
		// synthetic ready so the gate brief is still delivered without a wait.
		adapter: adapter,
		// pre-RT14 parity: the gate always launches fresh, never `claude --resume`.
		probeResume: false,
		tap:         tap,
		tapCh:       tapCh,
		launch: func(lctx context.Context) (<-chan struct{}, error) {
			sess, watcher, launchErr = runH.Launch(lctx, spec)
			if launchErr != nil {
				return nil, launchErr
			}
			if watcher != nil {
				return watcher.Done(), nil
			}
			return nil, nil
		},
		onLaunchFailed: func(context.Context, error) {
			if deps.hookStore != nil {
				deps.hookStore.CloseHookSession(runID.String(), artifacts.ClaudeSessionID)
			}
		},
		onLaunched: func(lctx context.Context) {
			// hk-goczd: window is live — emit the held-back launch_initiated to clear the
			// false stall. Mirrors workloop.go's single-mode path.
			if gateLaunchInitiatedMsg != nil {
				runlaunch.EmitPreExecMessage(lctx, emit, runID, gateLaunchInitiatedMsg)
			}

			// hk-nvjk: start the CHB-019 heartbeat goroutine so the stale watcher
			// receives agent_heartbeat events (with run_id) after launch_initiated.
			// Without this, lastEventType stays frozen at "launch_initiated" for the
			// full run duration, causing false-positive run_stale on every gate dispatch.
			// Mirrors the single-mode path (workloop.go Step 5). Closed via the defer
			// registered after the segment returns.
			gateHBDone = make(chan struct{})
			go handler.RunHeartbeatLoop(ctx, artifacts.HandlerSessionID,
				handler.HeartbeatInterval, gateHBDone,
				newDaemonHeartbeatEmitter(tap, runID))

			if deps.hookStore != nil {
				capturedTap := tap
				deps.hookStore.SetAgentReadyCallback(runID.String(), artifacts.ClaudeSessionID, func() { //nolint:contextcheck // relay callback runs off any request ctx (pre-RT8 idiom)
					_ = capturedTap.Emit(context.Background(), core.EventTypeAgentReady, nil) //nolint:errcheck // best-effort emit (pre-RT8 idiom)
				})
			}
		},
		deliver: func(dctx context.Context) {
			// Deliver gate-evaluator kick-off message and watch for verdict file.
			briefDelivered := pasteInjectCognitionGate(dctx, deps.clock, pasteTarget, artifacts.ClaudeSessionID, wtPath, emit, runID)
			if qs, ok := pasteTarget.(quitSender); ok {
				go pasteInjectQuitOnGateFile(ctx, deps.clock, runner, qs, sess, wtPath, briefDelivered)
			}
		},
		killReady: func(kctx context.Context) {
			fmt.Fprintf(os.Stderr, "daemon: dot: gate: waitAgentReady node %q run %s: %v\n",
				node.ID, runID.String(), runlaunch.ErrAgentReadyTimeout)
			_ = sess.Kill(kctx) //nolint:errcheck // kill is best-effort; reap below bounds it (pre-RT8 idiom)
			if watcher != nil {
				select {
				case <-watcher.Done():
				case <-substrate.After(deps.clock, runlaunch.KillReapTimeout): //nolint:contextcheck // ClockPort reap deadline, deliberately not ctx-scoped (pre-RT8 idiom)
				}
			}
			// The gate's reap Wait is deliberately UNBOUNDED — it does not carry
			// workloop.go's hk-4hso5 bounded context; adding one would be a logic change.
			_ = sess.Wait(kctx) //nolint:errcheck // reap wait; error non-actionable (pre-RT8 idiom)
			if deps.hookStore != nil {
				deps.hookStore.CloseHookSession(runID.String(), artifacts.ClaudeSessionID)
			}
		},
		emitReadyTimeout: func(ectx context.Context) {
			runlaunch.EmitAgentReadyTimeout(ectx, emit, runID, artifacts.ClaudeSessionID, deps.agentReadyTimeout)
		},
		killAbort: func(context.Context) {
			// Ctx-cancel abort edge: Kill is idempotent (the runlaunch.ForceTeardownSession
			// backstop registered below rides behind it either way — unlike workloop.go's
			// single-mode path, this site's teardown is unconditional).
			if sess != nil {
				_ = sess.Kill(context.Background()) //nolint:errcheck,contextcheck // idempotent abort kill off the cancelled ctx; teardown backstop follows
			}
		},
	}
	gateDispatch := gateSeg.run(ctx)

	if launchErr != nil {
		return nil, fmt.Errorf("cognition gate %q: launch: %w", gateRef, launchErr)
	}

	// RT14: the two cleanup defers are registered here, in their pre-RT14 textual
	// order, and BEFORE the ready-timeout terminal check below so LIFO firing
	// order is preserved on the agent_ready_timeout path. The order matters and is
	// this site's OWN, not the dot_cascade template's: pre-RT14 close(gateHBDone)
	// was registered first and ForceTeardownSession second, so under LIFO the
	// session is torn down BEFORE the heartbeat loop is stopped. Registering them
	// the other way round would silently invert that.
	if gateHBDone != nil {
		gateHBDoneToClose := gateHBDone
		defer close(gateHBDoneToClose)
	}

	// hk-goczd / hk-68pvl: slot-reclaim backstop — guarantee the spawn-semaphore
	// slot (hk-xb5yi / hk-4l7zs) is released on EVERY return path. The success path
	// below kills the session only when watcher == nil; this defer covers the exec
	// path and any early return (agent_ready timeout, ctx-cancel, verdict-read
	// error). Kill is idempotent, so it is a no-op when the session was already
	// torn down.
	defer runlaunch.ForceTeardownSession(sess) //nolint:contextcheck // teardown backstop takes no ctx (pre-RT8 idiom); it deliberately reaps on context.Background() so the kill completes even after the run ctx is cancelled

	if gateDispatch.Phase == runexec.DispatchFailed && gateDispatch.Reason == "agent_ready_timeout" {
		return nil, fmt.Errorf("cognition gate %q: agent_ready_timeout", gateRef)
	}
	// Working / Exited / Aborted: fall through — the pre-RT14 posture for
	// agent_ready-observed, watcher-exit-first, and ctx-cancel.

	_, _ = waitWithSocketGrace(ctx, deps.clock, deps.hookStore, watcher, sess,
		runID.String(), artifacts.ClaudeSessionID)

	if watcher == nil {
		_ = sess.Kill(context.Background())
	}

	if deps.hookStore != nil {
		deps.hookStore.CloseHookSession(runID.String(), artifacts.ClaudeSessionID)
	}

	if ctx.Err() != nil {
		return nil, fmt.Errorf("cognition gate %q: context cancelled", gateRef)
	}

	// Read and parse the gate verdict via runner for remote runs (hk-hd2w6).
	decision, readErr := readGateVerdictVia(ctx, runner, verdictPath)
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
	ctx context.Context,
	runner ltmux.CommandRunner,
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

	ctxJSON, ctxErr := json.MarshalIndent(run.Context, "", "  ")
	if ctxErr != nil {
		// Non-fatal: the brief renders an empty Run Context block. Log so the
		// silent omission is diagnosable rather than swallowed.
		fmt.Fprintf(os.Stderr, "daemon: dot: gate: marshal run context for node %q: %v\n", nodeID, ctxErr)
	}

	content := fmt.Sprintf(`# Gate Evaluation Task

## Gate Evaluator Constraint (CRITICAL — read before acting)

You are a READ-ONLY gate evaluator. You MUST NOT run any git command that changes repository state.
Forbidden commands: `+"`git reset`, `git checkout`, `git cherry-pick`, `git merge`, `git branch -d`, `git push`, `git rebase`"+`, or any other state-mutating git operation.
You operate on a reviewer worktree. The only file you may write is `+"`%s`"+` (your verdict) and any analysis scratch files under .harmonik/.
Violating this constraint can corrupt the implementer's task branch and break the merge pipeline.

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
		gateVerdictRelPath,
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

	return workspace.WriteFileVia(ctx, runner, taskPath, []byte(content), 0o644)
}

// readGateVerdict reads gate-verdict.json off the local filesystem and parses it
// via policy.ParseGateVerdict (the pure validation predicate — M5 slice 2
// sub-slice C). Factored so both this local path and readGateVerdictVia (remote)
// share byte-identical validation (NFR7).
func readGateVerdict(verdictPath string) (core.GateAction, error) {
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", verdictPath, err)
	}
	return policy.ParseGateVerdict(data)
}

// readGateVerdictVia is like readGateVerdict but routes the file read through
// runner for remote runs (hk-hd2w6). When runner is nil or local-FS, delegates
// to readGateVerdict (NFR7: byte-identical local path). When runner is non-local
// (e.g. SSHRunner), reads the worker-side gate-verdict.json via cat and applies
// identical parsing via policy.ParseGateVerdict.
//
// Bead: hk-hd2w6.
func readGateVerdictVia(ctx context.Context, runner ltmux.CommandRunner, verdictPath string) (core.GateAction, error) {
	if runner == nil || gitprobe.RunnerIsLocalFS(runner) {
		return readGateVerdict(verdictPath)
	}
	out, err := runner.Command(ctx, "cat", verdictPath).Output()
	if err != nil {
		return "", fmt.Errorf("read %s via runner: %w", verdictPath, err)
	}
	return policy.ParseGateVerdict(out)
}

// gateVerdictExistsVia reports whether the gate-verdict.json file exists and is
// non-empty. Routes the stat check through runner on remote runs so the check
// lands on the worker's filesystem (hk-hd2w6). NFR7: nil/local runner falls back
// to os.Stat on box A. Both paths guard against an empty/truncated file
// (local: info.Size() > 0; remote: test -s which is POSIX "exists and non-empty").
//
// Bead: hk-hd2w6.
func gateVerdictExistsVia(ctx context.Context, runner ltmux.CommandRunner, path string) bool {
	if runner == nil || gitprobe.RunnerIsLocalFS(runner) {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}
	return runner.Command(ctx, "test", "-s", path).Run() == nil
}

// pasteInjectCognitionGate delivers the gate-evaluator kick-off message.
// Analogous to pasteInjectReviewer for the reviewer path.
// Returns a channel closed once the kick-off paste has been written.
func pasteInjectCognitionGate(
	ctx context.Context,
	clk substrate.ClockPort,
	subst handler.Substrate,
	claudeSessID string,
	wtPath string,
	bus handlercontract.EventEmitter,
	runID core.RunID,
) <-chan struct{} {
	if clk == nil {
		clk = substrate.SystemClock{}
	}
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		if subst == nil {
			return
		}
		inj, ok := subst.(pasteInjecter)
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
			splashDismissWait(ctx, clk)
		}

		bufName := bufferName(claudeSessID, "gate")
		msg := "Read " + gateTaskRelPath + " in this worktree." +
			" It contains the gate evaluation request, the run context, and your role." +
			" Evaluate the gate and write your decision to " + gateVerdictRelPath +
			" as JSON: {\"schema_version\":1,\"decision\":\"allow|deny|escalate-to-human\",\"reason\":\"...\"}." +
			" The decision field MUST be exactly one of: allow, deny, escalate-to-human." +
			// hk-ppw: explicit read-only constraint — gate evaluator MUST NOT run git state-changing commands.
			" READ-ONLY CONSTRAINT: you MUST NOT run git reset, git checkout, git cherry-pick, git merge," +
			" git push, git rebase, or any other state-mutating git command. You are on a reviewer" +
			" worktree; mutating git state can corrupt the implementer's task branch." +
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
//
// clk is the determinism port for the whole watchdog (P2 E5 RT19c). The verdict
// deadline and the poll ticker are read from the SAME clock so a FakeClock can
// drive the 10-minute gateFileTimeout branch instantly; mixing a fake deadline
// with a real ticker (or vice versa) would leave the loop unable to terminate.
// nil is backstopped to substrate.SystemClock{} for struct-literal test callers.
func pasteInjectQuitOnGateFile(
	ctx context.Context,
	clk substrate.ClockPort,
	runner ltmux.CommandRunner,
	qs quitSender,
	killer sessionKiller,
	wtPath string,
	briefDelivered <-chan struct{},
) {
	if clk == nil {
		clk = substrate.SystemClock{}
	}
	if briefDelivered != nil {
		select {
		case <-ctx.Done():
			return
		case <-briefDelivered:
		case <-substrate.After(clk, briefDeliveredTimeout): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
			fmt.Fprintf(os.Stderr,
				"daemon: pasteinject: quit-on-gate-file: brief_delivered timeout for %s; proceeding\n", wtPath)
		}
	}

	verdictPath := filepath.Join(wtPath, gateVerdictRelPath)
	deadline := clk.Now().Add(gateFileTimeout)
	ticker := clk.NewTicker(gateFilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			if clk.Now().After(deadline) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-gate-file: timeout waiting for %s; sending /quit\n", verdictPath)
				_ = qs.SendQuitToLastPane(ctx)
				select {
				case <-ctx.Done():
				case <-substrate.After(clk, noChangeKillDelay): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
				}
				if killer != nil {
					_ = killer.Kill(ctx)
				}
				return
			}
			if gateVerdictExistsVia(ctx, runner, verdictPath) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-gate-file: verdict detected at %s; sending /quit\n", verdictPath)
				_ = qs.SendQuitToLastPane(ctx)
				select {
				case <-ctx.Done():
				case <-substrate.After(clk, postQuitKillGrace): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
				}
				if killer != nil {
					_ = killer.Kill(ctx)
				}
				return
			}
		}
	}
}
