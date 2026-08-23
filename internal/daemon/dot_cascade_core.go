package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/runloop"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

func driveDotWorkflow(
	ctx context.Context,
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
	runID core.RunID,
	beadID core.BeadID,
	beadRecord core.BeadRecord,
	beadTitle string,
	beadDescription string,
	activeRepo string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	descriptor core.WorkflowDescriptor,
	resolvedModel string,
	resolvedEffort string,
	piProfile projectconfig.PiProfileConfig,
	extraContext string,
	baseBranch string,
	runner tmux.CommandRunner, // remote-substrate: SSHRunner for remote runs; nil for local (NFR7)
	workerBinaryPath string,
	workerHookSock string,
	workerSessionName string,
	workerSessionCwd string,
) dotWorkflowResult {
	emit := ports.Emitter
	boxADaemonSocket := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")
	daemonSocket := tunnelpkg.ResolveAgentDaemonSocket(workerHookSock, boxADaemonSocket)

	nodesByID := make(map[string]*dot.Node, len(graph.Nodes))
	for _, n := range graph.Nodes {
		nodesByID[n.ID] = n
	}

	run := &core.Run{
		RunID:           runID,
		WorkflowID:      descriptor.WorkflowID,
		WorkflowVersion: descriptor.WorkflowVersion,
		Input:           core.WorkspaceRef(wtPath),
		WorkflowMode:    core.WorkflowModeDot,
		State:           core.StateID(uuid.New()),
		Context:         map[string]any{},
		StartTime:       ports.Clock.Now(),
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

	iterationCount := 0
	var claudeSessionID string

	lastDiffHash := ""

	priorIterHeadSHA := ""

	priorVerdict := ""

	var priorVerdictFlags []string

	priorVerdictNotes := ""

	lastGatePassed := false
	lastGateNotes := ""
	lastGateClass := core.FailureClass("")
	lastGateNodeID := ""

	const dotMaxReviewerNoVerdictRetries = 1
	reviewerNoVerdictRetries := 0

	var lastImplementerReviewerHarness core.AgentType

	axisReviewerVerdicts := make(map[string]string)

	noProgressGuardOff := false
	noProgressGuardCap := 0
	switch {
	case graph.NoProgressGuard == "off":
		noProgressGuardOff = true
	case strings.HasPrefix(graph.NoProgressGuard, "capped:"):
		if parsedCap, atoiErr := strconv.Atoi(strings.TrimPrefix(graph.NoProgressGuard, "capped:")); atoiErr == nil {
			noProgressGuardCap = parsedCap
		}
	}
	consecutiveNoProgressCount := 0

	prevAgenticNodeWasReviewer := false

	prevNodeID := ""

	for visits := 0; visits < dotMaxNodeVisits; visits++ {
		node := nodesByID[currentNodeID]
		if node == nil {
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary:        fmt.Sprintf("dot: edge points at undeclared node %q", currentNodeID),
			}
		}

		emitNodeDispatchRequested(ctx, emit, ports.Clock, runID, core.NodeID(currentNodeID))

		var outcome core.Outcome

		switch node.Type {
		case core.NodeTypeNonAgentic:
			switch {
			case node.ToolCommand != "" && node.HandlerRef == "shell":
				gateEnv := env.HandlerEnv
				if parentSHA != "" {
					gateEnv = append(append(make([]string, 0, len(env.HandlerEnv)+1), env.HandlerEnv...), "HK_GATE_BASE_SHA="+parentSHA)
				}
				toolOutcome, toolErr := dispatchDotToolNode(ctx, emit, runID, runner, env.ProjectDir, wtPath, node, gateEnv)
				if toolErr != nil {
					return dotWorkflowResult{
						success:        false,
						needsAttention: true,
						summary:        fmt.Sprintf("dot: tool node %q dispatch error: %v", currentNodeID, toolErr),
					}
				}
				outcome = toolOutcome
				lastGatePassed = outcome.Status == core.OutcomeStatusSuccess
				lastGateNodeID = currentNodeID
				if !lastGatePassed {
					lastGateNotes = outcome.Notes
					lastGateClass = ""
					if outcome.FailureClass != nil {
						lastGateClass = *outcome.FailureClass
					}
				}

			case node.ToolCommand != "" && node.HandlerRef != "shell":
				outcome = core.Outcome{Status: core.OutcomeStatusSuccess}

			default:
				outcome = core.Outcome{Status: core.OutcomeStatusSuccess}
			}

		case core.NodeTypeAgentic:
			isReviewer := nodeIsReviewer(node)

			currentHead, headErr := resolveDotWorktreeHEAD(ctx, runner, wtPath)
			if headErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: false,
					summary:        fmt.Sprintf("dot: resolve HEAD before agentic node %q at iteration %d: %v", currentNodeID, iterationCount, headErr),
				}
			}
			currentHash, hashErr := computeDiffHashVia(ctx, runner, wtPath, parentSHA)
			if hashErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: false,
					summary:        fmt.Sprintf("dot: diff-hash error before agentic node %q at iteration %d: %v", currentNodeID, iterationCount, hashErr),
				}
			}
			headAdvanced := priorIterHeadSHA == "" || currentHead != priorIterHeadSHA
			committedResult := parentSHA == "" || currentHead != parentSHA
			firstReviewerEntryWithGreenGate := isReviewer && priorVerdict == "" && committedResult && lastGatePassed
			reviewerRetryInFlight := isReviewer && reviewerNoVerdictRetries > 0
			if iterationCount >= 2 && !headAdvanced && !reviewerRetryInFlight && !firstReviewerEntryWithGreenGate {
				if committedResult && priorVerdict == workspace.ReviewVerdictApprove && !prevAgenticNodeWasReviewer {
					// hk-tnui: read the verdict so the caller can stamp
					// Reviewed-By / Review-Verdict trailers before merge.
					// Non-fatal: a missing/unreadable file yields nil, which the
					// caller's trailer-stamp guard already skips.
					// hk-f3u6o: route through runner so a REMOTE run reads the
					// worker-side review.json (a box-A os.ReadFile would miss it and
					// silently drop the merge trailers); nil/local → bare-local (NFR7).
					// hk-vv10r: use the retrying reader (both local and remote) so a
					// verdict observed mid-flush doesn't silently drop the trailers.
					//nolint:errcheck // non-fatal: a missing/unreadable file yields nil, which the trailer-stamp guard skips
					dotApproveVerdict, _ := readDotReviewVerdictRetry(ctx, runner, wtPath)
					return dotWorkflowResult{
						success:        true,
						approveVerdict: dotApproveVerdict,
						summary:        fmt.Sprintf("dot: completed at iteration %d — reviewer APPROVED and committed work is final (hk-8ps7q: HEAD did not advance because nothing remained to do)", iterationCount),
					}
				}
				if committedResult && priorVerdict == workspace.ReviewVerdictRequestChanges && lastGatePassed {
					return dotWorkflowResult{
						success:    true,
						advisoryRC: true,
						summary:    fmt.Sprintf("dot: completed at iteration %d — REQUEST_CHANGES was advisory-only (commit gate green; HEAD final, nothing committable remained) (hk-w2ow)", iterationCount),
					}
				}
				if committedResult && priorVerdict == "" && lastGatePassed && !isReviewer {
					return dotWorkflowResult{
						success: true,
						summary: fmt.Sprintf("dot: completed at iteration %d — committed work is gate-green with no prior reviewer verdict; preserving committed tree (hk-du455)", iterationCount),
					}
				}
				if noProgressGuardOff {
				} else {
					shouldFire := true
					if noProgressGuardCap > 0 {
						if !isReviewer {
							consecutiveNoProgressCount++
						}
						shouldFire = consecutiveNoProgressCount > noProgressGuardCap
					}
					if shouldFire {
						strandedNote := strandedCommitNote(runID, committedResult, lastGatePassed,
							lastGateNodeID, prevNodeID, currentHead, lastGateNotes)
						if priorVerdict == workspace.ReviewVerdictRequestChanges {
							emitReviewFixupStalled(ctx, emit, runID, core.WorkflowModeDot,
								iterationCount, priorVerdictFlags, currentHash, lastDiffHash)
							return dotWorkflowResult{
								success:        false,
								needsAttention: true,
								summary:        fmt.Sprintf("dot: review fix-up stalled at iteration %d: HEAD did not advance after REQUEST_CHANGES%s", iterationCount, strandedNote),
							}
						}
						emitDotNoProgressDetected(ctx, emit, runID, iterationCount, currentHash, lastDiffHash)
						return dotWorkflowResult{
							success:        false,
							needsAttention: true,
							summary:        fmt.Sprintf("dot: no-progress detected at iteration %d: HEAD did not advance%s", iterationCount, strandedNote),
						}
					}
				}
			}
			if headAdvanced && !isReviewer {
				consecutiveNoProgressCount = 0
			}
			lastDiffHash = currentHash
			if !isReviewer {
				priorIterHeadSHA = currentHead
			}

			if !isReviewer {
				iterationCount++
				reviewerNoVerdictRetries = 0
				axisReviewerVerdicts = make(map[string]string)
				lastImplementerReviewerHarness = core.AgentType(node.ReviewerHarness)

				if iterationCount >= 2 {
					priorIter := iterationCount - 1
					var priorSummary string
					if runner != nil {
						fmt.Fprintf(os.Stderr,
							"daemon: dot: REMOTE run iter %d: implementer-resume feedback NOT routed to worker worktree (no WriteReviewerFeedbackVia); resume will run without feedback/commit-nudge — multi-iteration remote DOT runs are not yet supported (FLAGGED follow-up) [hk-wixms]\n",
							iterationCount)
					} else {
						var rfPayload workspace.ReviewerFeedbackPayload
						prevNode := nodesByID[prevNodeID]
						fromReviewerRC := prevNode != nil && nodeIsReviewer(prevNode) &&
							priorVerdict == workspace.ReviewVerdictRequestChanges
						if fromReviewerRC {
							rfPayload = workspace.ReviewerFeedbackPayload{
								WorkspacePath:  wtPath,
								PriorIteration: priorIter,
								Verdict:        priorVerdict,
								Flags:          priorVerdictFlags,
								Notes:          priorVerdictNotes,
							}
							priorSummary = truncateUTF8(priorVerdictNotes, priorVerdictSummaryMaxBytes)
						} else {
							fromGateFail := prevNode != nil && prevNode.ID == "commit_gate" && !lastGatePassed
							if fromGateFail && lastGateNotes != "" {
								gateFailMsg := gateBackEdgeMessage(lastGateClass, lastGateNotes)
								rfPayload = workspace.ReviewerFeedbackPayload{
									WorkspacePath:  wtPath,
									PriorIteration: priorIter,
									Verdict:        "GATE_FAIL",
									Notes:          gateFailMsg,
								}
								priorSummary = truncateUTF8(gateFailMsg, priorVerdictSummaryMaxBytes)
							} else {
								const commitNudge = "Your previous pass produced NO commit — the workflow bounced back to you because HEAD did not advance. " +
									"Re-read .harmonik/agent-task.md, make the required changes if you have not already, and you MUST commit your changes before exiting. " +
									"If your prior edits are still in the working tree, commit them now; an uncommitted change is invisible to the workflow and will loop forever."
								rfPayload = workspace.ReviewerFeedbackPayload{
									WorkspacePath:  wtPath,
									PriorIteration: priorIter,
									Verdict:        "NO_COMMIT",
									Notes:          commitNudge,
								}
								priorSummary = truncateUTF8(commitNudge, priorVerdictSummaryMaxBytes)
							}
						}
						if rfErr := workspace.WriteReviewerFeedback(rfPayload); rfErr != nil {
							fmt.Fprintf(os.Stderr,
								"daemon: dot: WriteReviewerFeedback iter %d: %v (non-fatal) [hk-wixms]\n",
								iterationCount, rfErr)
						}
					}
					emitDotImplementerResumed(ctx, emit, runID, claudeSessionID, iterationCount, priorSummary)
				}
			}
			_, isConsolidate := isConsolidateJoinNode(graph, nodesByID, currentNodeID)
			nodeOutcome, nodeErr := dispatchDotAgenticNode(ctx, env, ports, handles, runID, beadID, beadRecord,
				beadTitle, beadDescription, activeRepo, wtPath, parentSHA, daemonSocket, node,
				isReviewer, iterationCount, &claudeSessionID,
				resolvedModel, resolvedEffort, piProfile, extraContext, baseBranch,
				lastImplementerReviewerHarness, runner,
				workerBinaryPath, workerSessionName, workerSessionCwd,
				isConsolidate)
			if nodeErr != nil {
				if errors.Is(nodeErr, errDotNoChangeSubsumed) {
					return dotWorkflowResult{
						subsumed: true,
						summary:  "noChange-subsumed: the bead's work is already merged on the branch this run lands on",
					}
				}
				if errors.Is(nodeErr, errDotReviewerNoVerdict) && isReviewer {
					committedResult := parentSHA == "" || currentHead != parentSHA
					if committedResult && reviewerNoVerdictRetries < dotMaxReviewerNoVerdictRetries {
						reviewerNoVerdictRetries++
						fmt.Fprintf(os.Stderr,
							"daemon: dot: reviewer node %q produced no verdict; committed result present — retrying reviewer (attempt %d/%d) [hk-bqf1q]\n",
							currentNodeID, reviewerNoVerdictRetries+1, dotMaxReviewerNoVerdictRetries+1)
						continue
					}
				}
				return dotWorkflowResult{
					success:        false,
					needsAttention: true,
					summary:        fmt.Sprintf("dot: agentic node %q failed: %v", currentNodeID, nodeErr),
				}
			}
			outcome = nodeOutcome

			if isReviewer && outcome.PreferredLabel != nil {
				axisReviewerVerdicts[currentNodeID] = *outcome.PreferredLabel
				if upstream, isJoin := isConsolidateJoinNode(graph, nodesByID, currentNodeID); isJoin {
					allVerdicts := make([]string, 0, len(upstream)+1)
					allVerdicts = append(allVerdicts, *outcome.PreferredLabel) // self
					for id := range upstream {
						if v, ok := axisReviewerVerdicts[id]; ok {
							allVerdicts = append(allVerdicts, v)
						}
					}
					if joined := verdictSeverityMax(allVerdicts); joined != "" && joined != *outcome.PreferredLabel {
						fmt.Fprintf(os.Stderr,
							"daemon: dot: consolidate node %q self-reported %q; routing on deterministic severity-max %q of %d axes (upstream+self) %v [hk-cmry,hk-0gnt]\n",
							currentNodeID, *outcome.PreferredLabel, joined, len(allVerdicts), allVerdicts)
						joinedLabel := joined
						outcome.PreferredLabel = &joinedLabel
					}
				}
			}

			if isReviewer && outcome.PreferredLabel != nil {
				priorVerdict = *outcome.PreferredLabel
				flags := outcome.PreferredLabelFlags
				if flags == nil {
					flags = []string{}
				}
				priorVerdictFlags = flags
				priorVerdictNotes = outcome.Notes
				reviewerNoVerdictRetries = 0
			}

			prevAgenticNodeWasReviewer = isReviewer

		case core.NodeTypeGate:
			gateOutcome, gateErr := dispatchDotGateNode(
				ctx, env, ports, handles, runID, run, wtPath, daemonSocket, node,
				iterationCount, resolvedModel, resolvedEffort,
				beadID, beadRecord, // hk-01vs0: tier-1 harness label reaches the gate's harness resolution
				beadTitle, beadDescription, extraContext, baseBranch, runner,
				workerBinaryPath, workerSessionName, workerSessionCwd,
			)
			if gateErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: true,
					summary:        fmt.Sprintf("dot: gate node %q dispatch failed: %v", currentNodeID, gateErr),
				}
			}
			outcome = gateOutcome

		case core.NodeTypeSubWorkflow:
			swRunner := newDotSubWorkflowRunner(
				env, ports, handles, runID, beadID, beadRecord, beadTitle, beadDescription,
				activeRepo, wtPath, parentSHA, daemonSocket,
				&iterationCount, &claudeSessionID, resolvedModel, resolvedEffort,
				piProfile, extraContext, baseBranch, run, cycles, graph,
				runner,            // remote-substrate: thread the run's runner into nested dispatch
				workerBinaryPath,  // hk-538l: worker harmonik path for remote sub-workflow node hooks
				workerSessionName, // hk-538l: worker tmux session for remote sub-workflow spawn
				workerSessionCwd,  // hk-538l: worker repo cwd for the worker tmux session
			)
			swSpec := handler.SubWorkflowRunSpec{
				Run:                run,
				ParentNodeID:       core.NodeID(currentNodeID),
				SubWorkflowRef:     core.SubWorkflowRef(node.SubWorkflowRef),
				SubWorkflowVersion: core.WorkflowVersion(node.WorkflowVersion),
			}
			if !swSpec.Valid() {
				return dotWorkflowResult{
					success:        false,
					needsAttention: true,
					summary:        fmt.Sprintf("dot: sub-workflow node %q: invalid spec (missing sub_workflow_ref or workflow_version)", currentNodeID),
				}
			}
			swOutcome, swErr := swRunner.Run(ctx, swSpec)
			if swErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: false,
					summary:        fmt.Sprintf("dot: sub-workflow node %q: infrastructure error: %v", currentNodeID, swErr),
				}
			}
			outcome = swOutcome

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

		decision := workflow.DecideNextNode(graph, currentNodeID, outcome, run, cycles)
		emitNodeDispatchDecided(ctx, emit, decision.Payload)

		switch {
		case decision.IsTerminal:
			success, why := dotTerminalNodeIsSuccess(graph, currentNodeID)
			summary := fmt.Sprintf("dot: reached terminal node %q", currentNodeID)
			if why != "" {
				summary += ": " + why
			}
			return dotWorkflowResult{
				success:        success,
				terminalNodeID: currentNodeID,
				needsAttention: !success,
				summary:        summary,
			}

		case decision.Failed:
			if decision.CompletionReason == "cap_hit" && currentNodeID == "commit_gate" && !graphHasReviewerNode(nodesByID) {
				if salvageHead, salvageErr := resolveDotWorktreeHEAD(ctx, runner, wtPath); salvageErr == nil &&
					salvageHead != "" && salvageHead != parentSHA {
					return dotWorkflowResult{
						success: true,
						summary: "dot: commit_gate cap-hit salvaged — committed tip present; auto-advancing to merge (hk-1vlz F42)",
					}
				}
			}
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
			incrementCapIfBounded(graph, cycles, runID, currentNodeID, decision.NextNodeID)
			prevNodeID = currentNodeID
			currentNodeID = decision.NextNodeID

		default:
			return dotWorkflowResult{
				success:        false,
				needsAttention: true,
				summary:        fmt.Sprintf("dot: cascade returned no decision at node %q", currentNodeID),
			}
		}
	}

	return dotWorkflowResult{
		success:        false,
		needsAttention: true,
		summary:        fmt.Sprintf("dot: exceeded max node visits (%d) — possible unbounded cycle", dotMaxNodeVisits),
	}
}

func dispatchDotAgenticNode(
	ctx context.Context,
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
	runID core.RunID,
	beadID core.BeadID,
	beadRecord core.BeadRecord,
	beadTitle string,
	beadDescription string,
	activeRepo string,
	wtPath string,
	parentSHA string,
	daemonSocket string,
	node *dot.Node,
	isReviewer bool,
	iterationCount int,
	claudeSessionID *string,
	resolvedModel string,
	resolvedEffort string,
	piProfile projectconfig.PiProfileConfig,
	extraContext string,
	baseBranch string,
	reviewerHarnessOverride core.AgentType, // T14 hk-iv748: reviewer_harness from implementer node; empty = DEFAULT (same as implementer)
	runner tmux.CommandRunner, // remote-substrate: SSHRunner for remote runs; nil for local (NFR7)
	workerBinaryPath string,
	workerSessionName string,
	workerSessionCwd string,
	isTerminalSpawn bool,
) (core.Outcome, error) {
	emit := ports.Emitter
	if isReviewer {
		headSHA, headErr := resolveDotWorktreeHEAD(ctx, runner, wtPath)
		if headErr != nil {
			return core.Outcome{}, fmt.Errorf("resolve HEAD before reviewer node %q: %w", node.ID, headErr)
		}
		if rmErr := workspace.RemoveReviewVerdictVia(ctx, runner, wtPath); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr,
				"daemon: dot cascade: remove stale review verdict in %q: %v (a stalled reviewer may read the prior verdict)\n",
				wtPath, rmErr)
		}
		if rmErr := workspace.RemoveFileVia(ctx, runner, reviewerBudgetSentinelPath(wtPath)); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr,
				"daemon: dot cascade: remove stale reviewer budget marker in %q: %v (this reviewer may be excused from the terminal check)\n",
				wtPath, rmErr)
		}
		rtErr := workspace.WriteReviewTargetVia(ctx, runner, workspace.ReviewTargetPayload{
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

	nodeExtraContext := extraContext
	if node.Role != "" {
		roleLine := "Role: " + node.Role
		if nodeExtraContext != "" {
			nodeExtraContext = roleLine + "\n\n" + nodeExtraContext
		} else {
			nodeExtraContext = roleLine
		}
	}

	reviewerInheritedHarness := runloop.DotReviewerInheritedHarnessOverride(
		handles.HarnessRegistry,
		resolveHarnessAgentTypeQuiet,
		isReviewer,
		reviewerHarnessOverride,
		core.AgentType(node.Harness),
		beadRecord,
		env.QueueDefaultHarness,
		env.DefaultHarness,
		string(beadID),
	)
	nodeModelHarness := core.AgentType(node.Harness)
	if isReviewer && reviewerHarnessOverride.Valid() {
		nodeModelHarness = reviewerHarnessOverride
	}
	if !nodeModelHarness.Valid() {
		if reviewerInheritedHarness.Valid() {
			nodeModelHarness = reviewerInheritedHarness // hk-pkxju
		} else {
			nodeModelHarness = resolveHarnessAgentTypeQuiet(
				beadRecord,
				env.QueueDefaultHarness,
				core.AgentType(""), // node default (already folded into node.Harness above)
				env.DefaultHarness,
			)
		}
	}
	nodeModel := nodeModelForHarness(resolvedModel, node.Model, nodeModelHarness)
	nodeEffort := resolvedEffort
	if node.Effort != "" {
		nodeEffort = node.Effort
	}

	rc := shared.LaunchCtx{
		RunID:         runID,
		BeadID:        string(beadID),
		WorkspacePath: wtPath,
		// runner threads the per-run CommandRunner into buildClaudeLaunchSpec so the
		// worktree-trust / settings / agent-task writes land on the WORKER for a
		// REMOTE DOT run (runner == dotRunner == rbc.sshRunner) and stay box-A-local
		// for a LOCAL run (runner == nil, NFR7). Without this the trust upsert ran
		// box-A-local → worker worktree untrusted → trust modal → no_commit
		// (hk-3sus; symmetric with how settings/agent-task get the worker).
		Runner: runner,
		// hk-538l: workerBinaryPath resolves the SessionStart hook command to the
		// WORKER's harmonik path for a REMOTE DOT run; empty for LOCAL falls back box-A-
		// local in claudelaunchspec. Without this the worker's settings.json pointed at
		// box-A's daemonBinaryPath → hook never exec'd → agent_ready_timeout (hk-538l).
		WorkerBinaryPath:  workerBinaryPath,
		DaemonSocket:      daemonSocket,
		WorkflowMode:      core.WorkflowModeDot,
		Phase:             phase,
		IterationCount:    iterationCount,
		PriorClaudeSessID: priorSess,
		HandlerBinary:     env.HandlerBinary,
		DaemonBinaryPath:  env.DaemonBinaryPath,
		BaseEnv:           env.HandlerEnv,
		BeadTitle:         beadTitle,
		BeadDescription:   beadDescription,
		NodePrompt:        node.Prompt,
		Model:             nodeModel,
		Effort:            nodeEffort,
		// The per-bead Pi provider tuple. It is read only on the pi path, and a
		// non-Pi bead resolves the zero tuple, so setting it unconditionally is
		// the same shape the single-mode launch context uses. The graph set none
		// of these and nothing downstream supplied them, so a graph run fell back
		// to the harness-global provider and the bead's profile never applied
		// (hk-yo9g6).
		Provider:         piProfile.Provider,
		APIKeyEnv:        piProfile.APIKeyEnv,
		APIKeyFile:       piProfile.APIKeyFile,
		BaseURL:          piProfile.BaseURL,
		API:              piProfile.API,
		WorktreeRootPath: workspace.WorktreeRootPath(env.ProjectDir, workspace.NoWorktreeRootOverride()),
		ExtraContext:     nodeExtraContext,
		BaseBranch:       baseBranch,
	}

	specBuilder := ports.LaunchBuilder
	var effectiveNodeHarness core.AgentType
	if isReviewer && reviewerHarnessOverride.Valid() {
		effectiveNodeHarness = reviewerHarnessOverride
	} else {
		effectiveNodeHarness = core.AgentType(node.Harness)
	}
	if !effectiveNodeHarness.Valid() && reviewerInheritedHarness.Valid() {
		effectiveNodeHarness = reviewerInheritedHarness
	}
	if effectiveNodeHarness.Valid() && handles.HarnessRegistry != nil {
		specBuilder = pinnedHarnessLaunchSpecBuilder(
			handles.HarnessRegistry,
			beadRecord,
			effectiveNodeHarness,
			emit,
		)
	}
	if specBuilder == nil {
		specBuilder = claude.BuildLaunchSpec
	}
	spec, artifacts, specErr := specBuilder(ctx, rc)
	if specErr != nil {
		return core.Outcome{}, fmt.Errorf("build launch spec for node %q: %w", node.ID, specErr)
	}
	if len(env.HandlerArgs) > 0 {
		spec.Args = append(env.HandlerArgs, spec.Args...)
	}

	reviewerHarnessIsClaude := false
	if isReviewer && handles.HarnessRegistry != nil {
		if h, hErr := handles.HarnessRegistry.ForAgent(shared.ArtifactAgentType(artifacts)); hErr == nil {
			reviewerHarnessIsClaude = h.SessionIDPolicy() == handlercontract.SessionIDMinted
		}
	}
	baseSubstrate := handles.Substrate
	if reviewerHarnessIsClaude && handles.ReviewerSubstrate != nil {
		baseSubstrate = handles.ReviewerSubstrate
	}
	preHeadSHA, preHeadErr := resolveDotWorktreeHEAD(ctx, runner, wtPath)
	if preHeadErr != nil {
		return core.Outcome{}, fmt.Errorf("resolve HEAD before node %q: %w", node.ID, preHeadErr)
	}

	var reviewerSessionID core.SessionID
	if isReviewer {
		reviewerSessionID = handlercontract.NewSessionID()
	}
	emitReviewerLaunched := func(lctx context.Context) {
		if isReviewer {
			emitDotReviewerLaunched(lctx, emit, runID, reviewerSessionID, *claudeSessionID, iterationCount)
		}
	}

	dotDeliver := func(dctx context.Context, dc agentDeliverCtx) {
		briefDelivered := pasteInjectOnLaunch(dctx, ports.Clock, dc.PasteTarget, artifacts.ClaudeSessionID,
			phase, iterationCount, wtPath, emit, runID)
		qs, ok := dc.PasteTarget.(quitSender)
		if !ok {
			return
		}
		if isReviewer {
			revInj, _ := dc.PasteTarget.(pasteInjecter) //nolint:errcheck // nil revInj disables re-seed by design (pre-RT8 idiom)
			var reviewerCeiling time.Duration
			if node.Timeout != "" {
				if n, err := strconv.Atoi(node.Timeout); err == nil && n > 0 {
					reviewerCeiling = time.Duration(n) * time.Second
				}
			}
			reviewerHBCh := dc.Tap.Subscribe()
			go pasteInjectQuitOnReviewFile(ctx, ports.Clock, qs, dc.Session, revInj, artifacts.ClaudeSessionID, wtPath, briefDelivered, reviewerHBCh, reviewerCeiling)
			return
		}
		if dc.ProcessExit {
			return
		}
		watchdogCh := dc.Tap.Subscribe()
		go pasteInjectQuitOnCommit(ctx, ports.Clock, qs, dc.Session, wtPath, preHeadSHA, nil, briefDelivered, watchdogCh, emit, runID)
	}

	logPrefix := fmt.Sprintf("daemon: dot: bead %s node %q run %s", beadID, node.ID, runID.String())
	launch := runAgentLaunch(ctx, agentLaunchInput{
		Env:               env,
		Ports:             ports,
		Handles:           handles,
		RunID:             runID,
		LogPrefix:         logPrefix,
		Spec:              spec,
		Artifacts:         artifacts,
		WorktreePath:      wtPath,
		DaemonSocket:      daemonSocket,
		Runner:            runner,
		Remote:            runner != nil,
		BaseSubstrate:     baseSubstrate,
		WorkerSessionName: workerSessionName,
		WorkerSessionCwd:  workerSessionCwd,
		// hk-x882o: terminal/consolidate nodes draw from the reserved +1 slot.
		Terminal: isTerminalSpawn,
		// M3-D7: a DOT back-edge resume gets the segment's transitional
		// run_id-stamped readiness probe, because a tmux `--resume` reattach does
		// not reliably re-fire a SessionStart hook (hk-isq02).
		IsResume:    phase == handlercontract.ReviewLoopPhaseImplementerResume,
		ProbeResume: phase == handlercontract.ReviewLoopPhaseImplementerResume,
		// hk-sj6a / hk-e7n76: reviewers emit heartbeats straight to the bus.
		// Routing them through the tap fans them to the reviewer watchdog's
		// subscription, which reads them as "still reasoning" — keeping the
		// reviewer alive until the 60-minute hard ceiling after claude has died.
		// Implementers DO go through the tap so the budget watchdog sees progress.
		HeartbeatViaTap: !isReviewer,
		OnBeforeLaunch:  emitReviewerLaunched,
		// hk-b4xf2: the stale watcher's silent-hang drive and stategather's
		// dashboard read both reach the lifecycle machine through the RunHandle.
		// A graph run never set it, so both were inert on the path that carries
		// the traffic. A graph run holds one session per node, so the handle
		// carries the machine of the node running now — which is the one a stale
		// watcher firing right now needs to see.
		OnLaunchedExtra: func(_ context.Context, sess handler.Session) {
			if handle, ok := handles.RunRegistry.Get(runID); ok {
				handle.SetMachine(sess.Machine())
			}
		},
		Deliver: dotDeliver,
	})
	defer launch.Cleanup()

	switch launch.Fail {
	case agentLaunchPrelaunchFailed:
		return core.Outcome{}, fmt.Errorf("node %q: %w", node.ID, launch.FailErr)
	case agentLaunchErrored:
		return core.Outcome{}, fmt.Errorf("launch node %q: %w", node.ID, launch.FailErr)
	case agentLaunchReadyTimeout:
		return core.Outcome{}, fmt.Errorf("node %q agent_ready_timeout", node.ID)
	case agentLaunchOK:
	}

	postExit := runAgentPostExit(ctx, agentPostExitInput{
		Env:          env,
		Ports:        ports,
		RunID:        runID,
		BeadID:       beadID,
		LogPrefix:    logPrefix,
		Launch:       launch,
		Runner:       runner,
		WorktreePath: wtPath,
		// A cascade needs a PER-NODE baseline, because node N's baseline is node
		// N−1's tip. This is the HEAD probed immediately before this node
		// launched, not the run's parent SHA.
		BaselineSHA: preHeadSHA,
		AgentType:   shared.ArtifactAgentType(artifacts),
		// A reviewer node produces reviewer_verdict rather than
		// implementer_phase_complete, and it has no commit to fall back on.
		Implementer: !isReviewer,
		// A graph node always stops on a cancelled context: its caller,
		// driveDotWorkflow, turns the error into the run's failure.
		CancelReason: func() string {
			return fmt.Sprintf("context cancelled during node %q", node.ID)
		},
	})
	if postExit.CancelReason != "" {
		return core.Outcome{}, errors.New(postExit.CancelReason)
	}

	if !isReviewer && *claudeSessionID == "" {
		*claudeSessionID = dotResolveResumeSessionID(
			launch.CapturedSessionID,
			artifacts.ClaudeSessionID,
			launch.Harness != nil && launch.Harness.SessionIDPolicy() == handlercontract.SessionIDCaptured,
		)
	}

	if isReviewer {
		verdict, verdictErr := readDotReviewVerdictRetry(ctx, runner, wtPath)
		if verdictErr != nil {
			return core.Outcome{}, fmt.Errorf("read reviewer verdict for node %q: %w", node.ID, verdictErr)
		}
		sentinel, sentinelErr := readDotReviewerBudgetSentinel(ctx, runner, wtPath, node.ID)
		if sentinelErr != nil {
			if verdict == nil {
				return core.Outcome{}, sentinelErr
			}
			fmt.Fprintf(os.Stderr,
				"daemon: dot: reviewer node %q: read budget marker: %v (a verdict is present; judging the reviewer on its exit)\n",
				node.ID, sentinelErr)
			sentinel = nil
		}
		if sentinel != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: dot: reviewer node %q budget exceeded (reason=%s budget_ms=%d elapsed_ms=%d changed_lines=%d)\n",
				node.ID, sentinel.Reason, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines)
			emitReviewerBudgetExceeded(ctx, emit, runID, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines, sentinel.Reason)
		}
		if verdict == nil {
			return core.Outcome{}, fmt.Errorf("%w (node %q)", errDotReviewerNoVerdict, node.ID)
		}
		if sentinel == nil {
			var reviewerWatcherErr error
			if launch.Watcher != nil {
				reviewerWatcherErr = launch.Watcher.Err()
			}
			if reason, failed := dotNodeTerminalFailure(artifacts.HandlerSessionID, launch.Exit, launch.SocketOutcome, reviewerWatcherErr); failed {
				return core.Outcome{}, fmt.Errorf("node %q (reviewer) %s", node.ID, reason)
			}
		}
		emitDotReviewerVerdict(ctx, emit, runID, reviewerSessionID, artifacts.ClaudeSessionID, iterationCount, verdict)
		label := verdict.Verdict
		flags := verdict.Flags
		if flags == nil {
			flags = []string{}
		}
		return core.Outcome{
			Status:              core.OutcomeStatusSuccess,
			PreferredLabel:      &label,
			PreferredLabelFlags: flags,         // hk-m1wqp: carries reviewer flags to driveDotWorkflow for review_fixup_stalled
			Notes:               verdict.Notes, // hk-wixms: carry verdict notes so the next implementer-resume back-edge can deliver them via reviewer-feedback.iter-<N-1>.md
		}, nil
	}

	var nodeWatcherErr error
	if launch.Watcher != nil {
		nodeWatcherErr = launch.Watcher.Err()
	}
	if reason, failed := dotNodeTerminalFailure(artifacts.HandlerSessionID, launch.Exit, launch.SocketOutcome, nodeWatcherErr); failed {
		return core.Outcome{}, fmt.Errorf("node %q (implementer) %s", node.ID, reason)
	}

	postHeadSHA, headErr := resolveDotWorktreeHEAD(ctx, runner, wtPath)
	if headErr != nil {
		return core.Outcome{}, fmt.Errorf("resolve HEAD after node %q: %w", node.ID, headErr)
	}
	if postHeadSHA == preHeadSHA && !node.NonCommitting {
		if beadWorkLandedOn(ctx, activeRepo, baseBranch, beadID) {
			return core.Outcome{}, errDotNoChangeSubsumed
		}
		if iterationCount < 2 {
			return core.Outcome{}, fmt.Errorf("node %q (implementer) %s", node.ID, dotNoHeadAdvanceReason(launch.Exit, launch.PiCaptureDir, preHeadSHA))
		}
		return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
	}
	if node.AutoStatus {
		if outcome, pass := runAutoStatusInspection(ctx, runner, wtPath); !pass {
			return outcome, nil
		}
	}
	return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
}
