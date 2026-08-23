package daemon

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

const gateVerdictRelPath = ".harmonik/gate-verdict.json"

const gateTaskRelPath = ".harmonik/gate-task.md"

var gateFileTimeout = 10 * time.Minute

var gateFilePollInterval = 2 * time.Second

func dispatchDotGateNode(
	ctx context.Context,
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
	runID core.RunID,
	run *core.Run,
	wtPath string,
	daemonSocket string,
	node *dot.Node,
	iterationCount int,
	resolvedModel string,
	resolvedEffort string,
	beadID core.BeadID,
	beadRecord core.BeadRecord,
	beadTitle string,
	beadDescription string,
	extraContext string,
	baseBranch string,
	runner ltmux.CommandRunner,
	workerBinaryPath string,
	workerSessionName string,
	workerSessionCwd string,
) (core.Outcome, error) {
	gateRef := core.GateRef(node.GateRef)

	cp, ok, registryLoaded := ports.Gate.LookupGate(gateRef)
	if !registryLoaded {
		return policy.GateEvalFailureOutcome("no ControlPoint registry loaded in daemon"), nil
	}
	if !ok {
		return policy.GateEvalFailureOutcome(fmt.Sprintf("gate_ref %q not found in ControlPoint registry", gateRef)), nil
	}
	if cp.Kind != core.KindGate {
		return policy.GateEvalFailureOutcome(fmt.Sprintf("gate_ref %q resolves to kind=%s, expected Gate", gateRef, cp.Kind)), nil
	}

	var evalFn handler.GateEvalFunc
	switch cp.Evaluator.Mode {
	case core.ModeTagMechanism:
		evalFn = buildMechanismGateEval(cp)
	case core.ModeTagCognition:
		var cogErr error
		evalFn, cogErr = buildCognitionGateEval(env, ports, handles, runID, cp, wtPath, daemonSocket, node, iterationCount, resolvedModel, resolvedEffort, beadID, beadRecord, beadTitle, beadDescription, extraContext, baseBranch, runner, workerBinaryPath, workerSessionName, workerSessionCwd)
		if cogErr != nil {
			return core.Outcome{}, fmt.Errorf("dot: gate node %q: build cognition eval: %w", node.ID, cogErr)
		}
	default:
		return policy.GateEvalFailureOutcome(fmt.Sprintf("gate_ref %q has unknown evaluator mode %q", gateRef, cp.Evaluator.Mode)), nil
	}

	result, err := handler.DispatchGateNode(ctx, run, core.NodeID(node.ID), gateRef, evalFn, ports.Emitter)
	if err != nil {
		return core.Outcome{}, fmt.Errorf("dot: gate node %q: DispatchGateNode: %w", node.ID, err)
	}
	return result.Outcome, nil
}

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

func buildCognitionGateEval(
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
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
		return executeCognitionGate(ctx, env, ports, handles, runID, run, cp, *dp, wtPath, daemonSocket, node, iterationCount, resolvedModel, resolvedEffort, beadID, beadRecord, beadTitle, beadDescription, extraContext, baseBranch, gateRef, runner, workerBinaryPath, workerSessionName, workerSessionCwd)
	}, nil
}

func executeCognitionGate(
	ctx context.Context,
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
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
	emit := ports.Emitter
	verdictPath := filepath.Join(wtPath, gateVerdictRelPath)
	if rmErr := workspace.RemoveFileVia(ctx, runner, verdictPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr,
			"daemon: cognition gate: remove stale verdict %q: %v (the gate may read the prior attempt)\n",
			verdictPath, rmErr)
	}

	if err := writeCognitionGateTask(ctx, runner, wtPath, cp, dp, run, string(beadID), beadTitle, beadDescription, node.ID); err != nil {
		return nil, fmt.Errorf("cognition gate %q: write gate-task.md: %w", gateRef, err)
	}

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
		HandlerBinary:     env.HandlerBinary,
		DaemonBinaryPath:  env.DaemonBinaryPath,
		BaseEnv:           env.HandlerEnv,
		BeadTitle:         beadTitle,
		BeadDescription:   beadDescription,
		NodePrompt:        "",
		Model:             resolvedModel,
		Effort:            resolvedEffort,
		WorktreeRootPath:  workspace.WorktreeRootPath(env.ProjectDir, workspace.NoWorktreeRootOverride()),
		ExtraContext:      extraContext,
		BaseBranch:        baseBranch,
	}

	specBuilder := ports.LaunchBuilder
	gateInheritedHarness := runloop.DotReviewerInheritedHarnessOverride(
		handles.HarnessRegistry,
		resolveHarnessAgentTypeQuiet,
		true,               // a cognition gate is reviewer-class by construction
		core.AgentType(""), // reviewer_harness=: never applies to a gate node
		core.AgentType(""), // node.Harness: not a gate-path mechanism (see above)
		beadRecord,
		env.QueueDefaultHarness,
		env.DefaultHarness,
		string(beadID),
	)
	if gateInheritedHarness.Valid() && handles.HarnessRegistry != nil {
		specBuilder = pinnedHarnessLaunchSpecBuilder(
			handles.HarnessRegistry,
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
	if len(env.HandlerArgs) > 0 {
		spec.Args = append(env.HandlerArgs, spec.Args...)
	}

	gateHarnessIsClaude := true
	if handles.HarnessRegistry != nil {
		if h, hErr := handles.HarnessRegistry.ForAgent(shared.ArtifactAgentType(artifacts)); hErr == nil {
			gateHarnessIsClaude = h.SessionIDPolicy() == handlercontract.SessionIDMinted
		}
	}
	gateBaseSubstrate := handles.Substrate
	if gateHarnessIsClaude && handles.ReviewerSubstrate != nil {
		gateBaseSubstrate = handles.ReviewerSubstrate
	}

	launch := runAgentLaunch(ctx, agentLaunchInput{
		Env:     env,
		Ports:   ports,
		Handles: handles,
		RunID:   runID,
		LogPrefix: fmt.Sprintf("daemon: dot: gate: node %q run %s",
			node.ID, runID.String()),
		Spec:              spec,
		Artifacts:         artifacts,
		WorktreePath:      wtPath,
		DaemonSocket:      daemonSocket,
		Runner:            runner,
		Remote:            runner != nil,
		BaseSubstrate:     gateBaseSubstrate,
		WorkerSessionName: workerSessionName,
		WorkerSessionCwd:  workerSessionCwd,
		// The gate is never the terminal/merge spawn and always launches fresh —
		// never `claude --resume`.
		Terminal:        false,
		IsResume:        false,
		ProbeResume:     false,
		HeartbeatViaTap: true,
		Deliver: func(dctx context.Context, dc agentDeliverCtx) {
			briefDelivered := pasteInjectCognitionGate(dctx, ports.Clock, dc.PasteTarget, artifacts.ClaudeSessionID, wtPath, emit, runID)
			if qs, ok := dc.PasteTarget.(quitSender); ok {
				go pasteInjectQuitOnGateFile(ctx, ports.Clock, runner, qs, dc.Session, wtPath, briefDelivered)
			}
		},
	})
	defer launch.Cleanup()

	switch launch.Fail {
	case agentLaunchPrelaunchFailed:
		return nil, fmt.Errorf("cognition gate %q: %w", gateRef, launch.FailErr)
	case agentLaunchErrored:
		return nil, fmt.Errorf("cognition gate %q: launch: %w", gateRef, launch.FailErr)
	case agentLaunchReadyTimeout:
		return nil, fmt.Errorf("cognition gate %q: agent_ready_timeout", gateRef)
	case agentLaunchOK:
	}

	if ctx.Err() != nil {
		return nil, fmt.Errorf("cognition gate %q: context cancelled", gateRef)
	}

	var gateWatcherErr error
	if launch.Watcher != nil {
		gateWatcherErr = launch.Watcher.Err()
	}
	if reason, failed := dotNodeTerminalFailure(artifacts.HandlerSessionID, launch.Exit, launch.SocketOutcome, gateWatcherErr); failed {
		return nil, fmt.Errorf("cognition gate %q: %s", gateRef, reason)
	}

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

func readGateVerdict(verdictPath string) (core.GateAction, error) {
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", verdictPath, err)
	}
	return policy.ParseGateVerdict(data)
}

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

func gateVerdictExistsVia(ctx context.Context, runner ltmux.CommandRunner, path string) bool {
	if runner == nil || gitprobe.RunnerIsLocalFS(runner) {
		info, err := os.Stat(path)
		return err == nil && info.Size() > 0
	}
	return runner.Command(ctx, "test", "-s", path).Run() == nil
}

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
				if quitErr := qs.SendQuitToLastPane(ctx); quitErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-gate-file: send /quit failed: %v\n", quitErr)
				}
				select {
				case <-ctx.Done():
				case <-substrate.After(clk, noChangeKillDelay): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
				}
				if killer != nil {
					if killErr := killer.Kill(ctx); killErr != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-gate-file: kill session failed: %v (the pane may still be alive)\n", killErr)
					}
				}
				return
			}
			if gateVerdictExistsVia(ctx, runner, verdictPath) {
				fmt.Fprintf(os.Stderr,
					"daemon: pasteinject: quit-on-gate-file: verdict detected at %s; sending /quit\n", verdictPath)
				if quitErr := qs.SendQuitToLastPane(ctx); quitErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: pasteinject: quit-on-gate-file: send /quit failed: %v\n", quitErr)
				}
				select {
				case <-ctx.Done():
				case <-substrate.After(clk, postQuitKillGrace): //nolint:contextcheck // substrate.After is ctx-free by contract (internal/substrate/clock.go After); this select's ctx.Done() case carries cancellation
				}
				if killer != nil {
					if killErr := killer.Kill(ctx); killErr != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: pasteinject: quit-on-gate-file: kill session failed: %v (the pane may still be alive)\n", killErr)
					}
				}
				return
			}
		}
	}
}
