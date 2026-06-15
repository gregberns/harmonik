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
//   - gate: the gate-decision SEMANTICS are resolved (CP-058 wins; a gate
//     deny/allow/escalate is status=SUCCESS, the cascade routes on the decision
//     surfaced via outcome.preferred_label; see handler.DispatchGateNode). The
//     daemon-side EVALUATOR seam is wired via dispatchDotGateNode (dot_gate.go,
//     hk-karlz): resolves gate_ref → ControlPoint, evaluates mechanism-tagged
//     gates via PolicyExprEvaluator (bool→GateAction per §6.4), dispatches
//     cognition-tagged gates as a fresh subprocess analogous to the reviewer
//     path, and reads gate-verdict.json. When cpRegistry is nil (no policy YAML
//     loaded) the node returns a structural eval-failure Outcome.
//   - sub-workflow: expanded in place within the parent run (SW-001..SW-010).
//     dotSubWorkflowRunner resolves the target graph (three-tier), checks
//     acyclicity (EM-034b), builds the namespaced SubWorkflowExpansion
//     (EM-034a), emits entered/exited events (EM-036), and returns the
//     terminal Outcome verbatim (EM-036a). Bead: hk-oe6.
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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

// resolveDotWorktreeHEAD resolves the worktree HEAD for a DOT-mode run, routing
// the git probe through the run's CommandRunner when one is present.
//
// NFR7 (local runs MUST stay byte-identical): when runner is nil — every LOCAL
// run, since rbc.sshRunner is nil unless a remote worker was selected — this
// calls the bare resolveWorktreeHEAD (exec.Command + cmd.Dir), unchanged. Only
// REMOTE runs (runner != nil, an SSHRunner) take the runner-routed path
// (resolveWorktreeHEADVia, `git -C <wtPath> rev-parse HEAD` over the transport),
// which is REQUIRED on a worker whose worktree lives on a separate filesystem
// that box A cannot chdir into.
func resolveDotWorktreeHEAD(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	if runner == nil {
		return resolveWorktreeHEAD(ctx, wtPath)
	}
	return resolveWorktreeHEADVia(ctx, runner, wtPath)
}

// errDotNoChangeSubsumed is returned by dispatchDotAgenticNode when the
// implementer exited without advancing HEAD and the bead is already subsumed
// in main (work landed via a prior run). driveDotWorkflow maps this to
// dotWorkflowResult{subsumed:true} so workloop.go can close-subsumed instead
// of reopening. Bead: hk-9v5yo.
var errDotNoChangeSubsumed = errors.New("dot: noChange-subsumed: work already in main")

// errDotReviewerNoVerdict is returned by dispatchDotAgenticNode when a
// reviewer node exits without writing a verdict file (stall, hang, or
// budget-kill without a budget sentinel). driveDotWorkflow uses this to
// distinguish a retriable reviewer stall (committed work exists) from a hard
// dispatch error. Bead: hk-bqf1q.
var errDotReviewerNoVerdict = errors.New("dot: reviewer node produced no verdict")

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

	// subsumed is true when the implementer exited without advancing HEAD and the
	// bead was already found in main (noChange-subsumed). The caller closes the
	// bead rather than reopening it.
	subsumed bool
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
	beadRecord core.BeadRecord,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	graph *dot.Graph,
	resolvedModel string,
	resolvedEffort string,
	extraContext string,
	baseBranch string,
	runner tmux.CommandRunner, // remote-substrate: SSHRunner for remote runs; nil for local (NFR7)
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

	// lastDiffHash is the SHA-256 hex digest of `git diff <parent>..<head>`
	// captured before each reviewer launch.  It is retained ONLY for the
	// no_progress_detected event payload (diff_hash_current / diff_hash_prior),
	// which is an observability surface; it is NO LONGER the progress signal.
	//
	// hk-togxq: the diff-hash equality test was VERDICT-BLIND and HEAD-BLIND. It
	// hard-failed any agentic re-entry at iteration ≥ 2 whose cumulative
	// parent..HEAD diff was unchanged, regardless of (a) whether a real commit
	// already landed (HEAD advanced past parentSHA / the prior iteration) and
	// (b) the prior reviewer verdict. That discarded good committed work:
	//   - a run that committed iter-1 work then re-entered with no NEW commit was
	//     failed instead of being allowed to flow to review/merge, and
	//   - a run whose iter-N commit produced the same NET diff as a prior commit
	//     (HEAD advanced, but `git diff parent..HEAD` collided) was false-flagged.
	// Progress is now measured by COMMIT/HEAD advancement across iterations
	// (priorIterHeadSHA) combined with the prior reviewer verdict (priorVerdict);
	// see the no-progress block below.
	lastDiffHash := ""

	// priorIterHeadSHA is the worktree HEAD recorded at the prior agentic-node
	// entry. The no-progress check compares the current HEAD to this value: if
	// HEAD advanced, the intervening implementer committed real work (progress),
	// so no_progress MUST NOT fire. Empty before the first agentic entry.
	priorIterHeadSHA := ""

	// priorVerdict is the preferred_label of the MOST RECENT reviewer node
	// (APPROVE / REQUEST_CHANGES / BLOCK), or "" before any reviewer has run.
	// hk-8ps7q: the no-progress check consults this to distinguish a
	// genuinely-stuck re-entry (prior verdict REQUEST_CHANGES — the implementer
	// was asked to make changes but produced none) from an approved-and-done
	// re-entry (prior verdict APPROVE — there is legitimately nothing left to do,
	// so HEAD does not advance). The latter must COMPLETE-and-merge the already
	// committed, reviewer-approved work, NOT no_progress-fail and strand it.
	priorVerdict := ""

	// priorVerdictFlags is the flags slice from the most recent reviewer verdict,
	// parallel to priorVerdict. Set alongside priorVerdict so the no-progress
	// check can emit review_fixup_stalled with the specific REQUEST_CHANGES flags
	// the implementer failed to address. Nil before any reviewer has run.
	// Bead ref: hk-m1wqp.
	var priorVerdictFlags []string

	// reviewerNoVerdictRetries counts how many times the current reviewer node
	// invocation was retried after producing no verdict (stall / hang).
	// hk-bqf1q: when committed work exists, the caller retries the reviewer
	// up to dotMaxReviewerNoVerdictRetries times before hard-failing.
	// Reset to 0 whenever a new implementation cycle begins (implementer runs)
	// or a reviewer produces a real verdict (stall resolved).
	const dotMaxReviewerNoVerdictRetries = 1
	reviewerNoVerdictRetries := 0

	// lastImplementerReviewerHarness carries the reviewer_harness attr from the most
	// recently dispatched implementer node (T14 hk-iv748). When non-empty it is passed
	// to dispatchDotAgenticNode as reviewerHarnessOverride so the reviewer's specBuilder
	// uses the implementer's declared reviewer harness rather than the reviewer node's
	// own harness= attr (which is typically absent for the standard reviewer node).
	// Reset on each new implementer dispatch so stale overrides do not bleed across
	// implementer→reviewer cycles when the graph revisits implementer nodes.
	var lastImplementerReviewerHarness core.AgentType

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
				toolOutcome, toolErr := dispatchDotToolNode(ctx, runner, wtPath, node, deps.handlerEnv)
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

			// ── No-progress check before ANY agentic dispatch (EM-015e / DOT) ──
			//
			// hk-togxq — HEAD-ADVANCEMENT no-progress detection. The progress signal
			// is COMMIT/HEAD ADVANCEMENT across agentic-node entries, NOT a stale
			// working-tree diff hash. At iteration ≥ 2 we fire no_progress ONLY when
			// HEAD did NOT advance since the prior agentic-node entry
			// (priorIterHeadSHA) — i.e. the intervening implementer produced no new
			// commit. This corrects the regression (dd7c3b57 / hk-pj4b6) where the
			// check was VERDICT-BLIND and HEAD-BLIND: it compared `git diff
			// parentSHA..HEAD` hashes and hard-failed whenever the *cumulative* diff
			// from parent was unchanged, regardless of whether HEAD itself advanced.
			// That false-flagged a run whose iter-N commit produced the same NET
			// parent..HEAD diff as a prior commit (HEAD advanced, but the diff hash
			// collided) — discarding good committed work stranded on the run branch.
			//
			// This satisfies:
			//   - REQUEST_CHANGES iter-1 + a REAL new iter-N commit (HEAD advances,
			//     MODE B): HEAD advanced → NO fire → flow on to re-review the new
			//     work, even when its net parent..HEAD diff collides with a prior
			//     commit (the old diff-hash test false-flagged exactly that);
			//   - REQUEST_CHANGES iter-1 + NO new commit at iter-N (HEAD unchanged,
			//     NEGATIVE GUARD): HEAD did not advance → fire → reject; un-addressed
			//     work is never merged;
			//   - implementer re-entry from a deterministic commit_gate FAIL with no
			//     new commit (hk-pj4b6 no-escape loop): HEAD unchanged → fire → clean
			//     no-progress failure BEFORE the traversal cap is hit.
			//
			// NOTE (hk-togxq scope): a run that committed VALID iter-1 work which the
			// commit_gate then WRONGLY bounced (no new commit on re-entry) is also
			// caught here — but that is a DIFFERENT bug (commit_gate bouncing a valid
			// commit; tracked separately) and is structurally indistinguishable at
			// this site from a genuinely-stuck gate loop. Salvaging that committed
			// work is out of scope for the no_progress signal.
			//
			// lastDiffHash is retained only to populate the no_progress_detected
			// event payload (diff_hash_current / diff_hash_prior — an observability
			// surface); it no longer gates the run.
			//
			// Unlike the review-loop, DOT mode does NOT emit
			// review_loop_cycle_complete after no_progress_detected — the DOT walk
			// terminates directly per the §8.1a ordering-rule DOT exemption.
			currentHead, headErr := resolveDotWorktreeHEAD(ctx, runner, wtPath)
			if headErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: false,
					summary:        fmt.Sprintf("dot: resolve HEAD before agentic node %q at iteration %d: %v", currentNodeID, iterationCount, headErr),
				}
			}
			currentHash, hashErr := rlComputeDiffHashVia(ctx, runner, wtPath, parentSHA)
			if hashErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: false,
					summary:        fmt.Sprintf("dot: diff-hash error before agentic node %q at iteration %d: %v", currentNodeID, iterationCount, hashErr),
				}
			}
			headAdvanced := priorIterHeadSHA == "" || currentHead != priorIterHeadSHA
			// hk-ycxfa: suppress the no-progress check when retrying a stalled reviewer
			// (hk-bqf1q follow-up). A reviewer retry does not advance HEAD (reviewers
			// never commit), so the check would fire prematurely when iterationCount >= 2
			// and priorVerdict == REQUEST_CHANGES — exactly the scenario hk-bqf1q was
			// meant to rescue. The retry is already gated by reviewerNoVerdictRetries <
			// dotMaxReviewerNoVerdictRetries; if the retry also stalls, hard-fail fires
			// below via the exhausted-budget branch.
			if iterationCount >= 2 && !headAdvanced && !(isReviewer && reviewerNoVerdictRetries > 0) {
				// hk-8ps7q — approved-and-done is COMPLETION, not no-progress.
				//
				// The no-progress condition (iter ≥ 2 + HEAD unchanged) is met by
				// TWO structurally-distinct situations, disambiguated ONLY by the
				// prior reviewer verdict:
				//
				//   (1) GENUINELY STUCK: the prior reviewer said REQUEST_CHANGES
				//       (or no reviewer has run yet) and the implementer re-entered
				//       WITHOUT a new commit — un-addressed feedback, nothing to
				//       merge. This MUST no_progress-fail (keeps the hk-togxq
				//       negative-guard + hk-5e9yj behavior intact).
				//
				//   (2) APPROVED AND DONE: there IS a committed result (HEAD is past
				//       the run baseline parentSHA) AND the prior reviewer APPROVED.
				//       HEAD legitimately does not advance because there is nothing
				//       left for the next iteration to do. Firing no_progress here
				//       false-fails the run and STRANDS the valid, reviewer-approved
				//       commit on the run branch (it is never merged). The run must
				//       instead COMPLETE so the caller merges the approved work.
				//
				// Note: a single APPROVE that routes straight to a terminal (e.g.
				// review→close in standard-bead.dot) never re-enters an agentic
				// node, so this branch only triggers in graphs whose post-APPROVE
				// path loops back through an agentic node (e.g. a commit_gate
				// fix-loop re-entry on an already-approved, already-committed bead —
				// the production T12/hk-xhawy shape).
				committedResult := parentSHA == "" || currentHead != parentSHA
				if committedResult && priorVerdict == workspace.ReviewVerdictApprove {
					return dotWorkflowResult{
						success: true,
						summary: fmt.Sprintf("dot: completed at iteration %d — reviewer APPROVED and committed work is final (hk-8ps7q: HEAD did not advance because nothing remained to do)", iterationCount),
					}
				}
				// hk-m1wqp: emit review_fixup_stalled (carrying the reviewer flags)
				// when the prior verdict was REQUEST_CHANGES and the implementer made
				// no new commit. Fall back to no_progress_detected for the uncommon
				// case where HEAD did not advance without any prior reviewer verdict
				// (e.g. a commit_gate loop with no reviewer node).
				if priorVerdict == workspace.ReviewVerdictRequestChanges {
					emitReviewFixupStalled(ctx, deps.bus, runID, core.WorkflowModeDot,
						iterationCount, priorVerdictFlags, currentHash, lastDiffHash)
					return dotWorkflowResult{
						success:        false,
						needsAttention: true,
						summary:        fmt.Sprintf("dot: review fix-up stalled at iteration %d: HEAD did not advance after REQUEST_CHANGES", iterationCount),
					}
				}
				emitDotNoProgressDetected(ctx, deps.bus, runID, iterationCount, currentHash, lastDiffHash)
				return dotWorkflowResult{
					success:        false,
					needsAttention: true,
					summary:        fmt.Sprintf("dot: no-progress detected at iteration %d: HEAD did not advance", iterationCount),
				}
			}
			lastDiffHash = currentHash
			priorIterHeadSHA = currentHead

			// Increment AFTER the no-progress check: an implementer (re-)entry
			// counts as a new iteration; reviewers reuse the implementer's count
			// (matching the review-loop semantics, where iterationCount tracks
			// implementer turns).
			// hk-bqf1q: each new implementer cycle resets the reviewer-stall
			// retry counter — a fresh impl commit warrants a full retry budget
			// for the subsequent reviewer invocation.
			if !isReviewer {
				iterationCount++
				reviewerNoVerdictRetries = 0
				// T14 hk-iv748: capture the reviewer_harness override from this
				// implementer node. A new implementer dispatch resets the override so
				// stale values from prior implementer cycles do not bleed into the next.
				lastImplementerReviewerHarness = core.AgentType(node.ReviewerHarness)
			}
			nodeOutcome, nodeErr := dispatchDotAgenticNode(ctx, deps, runID, beadID, beadRecord,
				beadTitle, beadDescription, wtPath, parentSHA, daemonSocket, node,
				isReviewer, iterationCount, &claudeSessionID,
				resolvedModel, resolvedEffort, extraContext, baseBranch,
				lastImplementerReviewerHarness, runner)
			if nodeErr != nil {
				if errors.Is(nodeErr, errDotNoChangeSubsumed) {
					return dotWorkflowResult{
						subsumed: true,
						summary:  "noChange-subsumed: bead found in main",
					}
				}
				// hk-bqf1q: reviewer produced no verdict (stall / hang / budget
				// kill). When committed work exists, retry the reviewer once rather
				// than hard-failing and stranding the valid impl commit.
				//
				// The retry re-enters the same reviewer node (currentNodeID
				// unchanged). reviewerNoVerdictRetries gates the total retry count
				// so a permanently-stalled reviewer does not loop indefinitely.
				//
				// NOTE: we do NOT update priorIterHeadSHA here — it was already set
				// to currentHead (the impl commit SHA) at line 381 above. The
				// no-progress check at the next agentic entry will see the same HEAD
				// and iterationCount=1, so iterationCount < 2 → no_progress does
				// NOT fire on the retry.
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

			// hk-8ps7q: remember the most recent reviewer verdict so the
			// no-progress check above can distinguish an approved-and-done re-entry
			// (complete-and-merge) from a genuinely-stuck REQUEST_CHANGES re-entry
			// (no_progress-fail). Only reviewer nodes carry a preferred_label.
			// hk-bqf1q: also reset the stall retry counter — a real verdict
			// means the reviewer is no longer stalled.
			// hk-m1wqp: also capture flags from the verdict so review_fixup_stalled
			// can carry the specific REQUEST_CHANGES flags to triage.
			if isReviewer && nodeOutcome.PreferredLabel != nil {
				priorVerdict = *nodeOutcome.PreferredLabel
				flags := nodeOutcome.PreferredLabelFlags
				if flags == nil {
					flags = []string{}
				}
				priorVerdictFlags = flags
				reviewerNoVerdictRetries = 0
			}

		case core.NodeTypeGate:
			// Gate dispatch: resolve gate_ref → ControlPoint, build GateEvalFunc
			// (mechanism: PolicyExpression eval; cognition: subprocess dispatch),
			// call handler.DispatchGateNode. Wired by hk-karlz.
			gateOutcome, gateErr := dispatchDotGateNode(
				ctx, deps, runID, run, wtPath, daemonSocket, node,
				iterationCount, resolvedModel, resolvedEffort,
				beadID, beadTitle, beadDescription, extraContext, baseBranch,
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
			// Sub-workflow dispatch: resolve graph, check acyclicity, expand in
			// place, and run the nested cascade within the parent run (SW-001..SW-010).
			// Per SW-007, we build a dotSubWorkflowRunner and call Run.
			swRunner := newDotSubWorkflowRunner(
				deps, runID, beadID, beadRecord, beadTitle, beadDescription,
				wtPath, parentSHA, daemonSocket,
				&iterationCount, &claudeSessionID, resolvedModel, resolvedEffort,
				extraContext, baseBranch, run, cycles, graph,
				runner, // remote-substrate: thread the run's runner into nested dispatch
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
				// Infrastructure failure → run_failed (not needs-attention).
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
	beadRecord core.BeadRecord,
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
	reviewerHarnessOverride core.AgentType, // T14 hk-iv748: reviewer_harness from implementer node; empty = DEFAULT (same as implementer)
	runner tmux.CommandRunner, // remote-substrate: SSHRunner for remote runs; nil for local (NFR7)
) (core.Outcome, error) {
	// Reviewer nodes need review-target.md on disk before the kick-off paste so
	// the reviewer has a brief to read (mirrors reviewloop.go WriteReviewTarget).
	if isReviewer {
		headSHA, headErr := resolveDotWorktreeHEAD(ctx, runner, wtPath)
		if headErr != nil {
			return core.Outcome{}, fmt.Errorf("resolve HEAD before reviewer node %q: %w", node.ID, headErr)
		}
		// hk-ycxfa: remove any prior review.json before launching the reviewer so a
		// stalled reviewer (exits without writing a verdict) correctly produces a nil
		// verdict. Without this, a stall at iter-2+ would pick up the stale verdict
		// from the prior iteration's reviewer, making ReadReviewVerdict return the old
		// verdict instead of nil — bypassing errDotReviewerNoVerdict and the retry
		// logic added by hk-bqf1q. Non-fatal: if the file doesn't exist, ignore.
		_ = os.Remove(workspace.ReviewVerdictPath(wtPath))
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

	// Surface node role= into the agent brief (hk-m5lmo). Prepend it to
	// extraContext so it appears in the ## Extra Context section of agent-task.md,
	// giving each node a distinct behavioural identity (e.g. per-axis reviewer).
	nodeExtraContext := extraContext
	if node.Role != "" {
		roleLine := "Role: " + node.Role
		if nodeExtraContext != "" {
			nodeExtraContext = roleLine + "\n\n" + nodeExtraContext
		} else {
			nodeExtraContext = roleLine
		}
	}

	// Start from the run-level resolved model/effort, then apply per-node
	// overrides (WG-042 §I.5, EM-012b-NODE). Independent: only-model inherits
	// run-level effort, vice versa. NOT a second resolution walk — static graph
	// data layered at dispatch.
	nodeModel := resolvedModel
	if node.Model != "" {
		nodeModel = node.Model
	}
	nodeEffort := resolvedEffort
	if node.Effort != "" {
		nodeEffort = node.Effort
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
		nodePrompt:        node.Prompt,
		model:             nodeModel,
		effort:            nodeEffort,
		worktreeRootPath:  workspace.WorktreeRootPath(deps.projectDir, workspace.NoWorktreeRootOverride()),
		extraContext:      nodeExtraContext,
		baseBranch:        baseBranch,
	}

	// Resolve the per-node spec builder. The pre-built deps.launchSpecBuilder
	// captures tier-1 (bead labels) + tier-4 (global default). When the DOT node
	// carries a harness= attribute (T5, hk-u67of), rebuild with the node's harness
	// as nodeDefault (tier-3) so the four-tier precedence is fully honored (T12).
	//
	// T14 hk-iv748: for reviewer nodes, prefer reviewerHarnessOverride (the
	// implementer node's reviewer_harness= attr) over the reviewer node's own
	// harness= attr. This implements the OPTIONAL OVERRIDE precedence:
	//   1. reviewerHarnessOverride (implementer's reviewer_harness= attr) — if valid
	//   2. node.Harness (reviewer node's own harness= attr) — if valid
	//   3. deps.launchSpecBuilder (DEFAULT: same resolved harness as the implementer)
	specBuilder := deps.launchSpecBuilder
	var effectiveNodeHarness core.AgentType
	if isReviewer && reviewerHarnessOverride.Valid() {
		// Override: implementer declared a specific reviewer harness.
		effectiveNodeHarness = reviewerHarnessOverride
	} else {
		// Default or non-reviewer: use the node's own harness= attr.
		effectiveNodeHarness = core.AgentType(node.Harness)
	}
	if effectiveNodeHarness.Valid() && deps.harnessRegistry != nil {
		specBuilder = routedLaunchSpecBuilder(
			deps.harnessRegistry,
			beadRecord,
			core.AgentType(""),   // queue default: hk-4x3rg
			effectiveNodeHarness, // tier-3: reviewer override or node harness attr (T5/T12/T14)
			core.AgentType(""),   // global default: built-in fallback = claude-code
			deps.bus,
		)
	}
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
	// remote-substrate: thread the run's runner (SSHRunner for remote, nil for
	// local) so the per-run substrate's liveness + worktree probes target the
	// WORKER, and the implementer/reviewer spawns on the worker (mirrors the
	// single-mode path, workloop.go ~2733). nil preserves local behaviour (NFR7).
	prs := newPerRunSubstrate(deps.substrate, deps.handlerBinary, runner)
	var substrate handler.Substrate = deps.substrate
	var pasteTarget handler.Substrate = deps.substrate
	if prs != nil {
		substrate = prs
		pasteTarget = prs
	}
	spec.Substrate = substrate

	preHeadSHA, _ := resolveDotWorktreeHEAD(ctx, runner, wtPath)

	if deps.hookStore != nil {
		deps.hookStore.RegisterHookSession(runID.String(), artifacts.claudeSessionID)
	}

	tap, tapCh := newPerRunEventTap(deps.bus, runID)
	runH := handler.NewHandler(tap, handlercontract.NoopWatcherDeadLetter{}, deps.adapterRegistry)

	// hk-goczd: emit the CHB-018 pre-exec progress messages (handler_capabilities,
	// session_log_location, skills_provisioned) BEFORE Launch, holding back
	// launch_initiated to emit AFTER the window is actually live. The DOT cascade
	// previously emitted NONE of these, so launch_initiated never fired for any
	// DOT-mode run — and the stale watcher's launch_stall_detected keys solely on
	// launch_initiated absence (stalewatch.go:296), firing a FALSE-POSITIVE stall
	// on every DOT dispatch even when the implementer spawned fine and the run
	// succeeded. Mirrors the single-mode path (workloop.go:2098/2137) and the
	// review-loop path (reviewloop.go:336). Holding launch_initiated until after a
	// successful Launch also keeps it truthful: when SpawnWindow is wedged on a
	// leaked slot, Launch returns an error below and launch_initiated never fires.
	nodeLaunchInitiatedMsg := emitPreExecBeforeLaunch(ctx, deps.bus, runID, artifacts.preExecMsgs)

	// hk-c73fs: emit reviewer_launched (§8.1a.2) for reviewer nodes before
	// launch, matching the builtin review-loop path (reviewloop.go:922-923).
	// After the 06-08 DOT-default deploy, all reviews ran via this function
	// but reviewer_launched was never emitted, making verdict latency
	// unmeasurable. Mint the session ID here so it can be reused in the
	// post-run emitDotReviewerVerdict call below.
	var reviewerSessionID core.SessionID
	if isReviewer {
		reviewerSessionID = handlercontract.NewSessionID()
		emitDotReviewerLaunched(ctx, deps.bus, runID, reviewerSessionID, *claudeSessionID, iterationCount)
	}

	nodeLaunchedAt := time.Now()
	sess, watcher, launchErr := runH.Launch(ctx, spec)
	if launchErr != nil {
		if deps.hookStore != nil {
			deps.hookStore.CloseHookSession(runID.String(), artifacts.claudeSessionID)
		}
		// hk-oihnf: surface structural launch-timeout failures as their dedicated
		// diagnostic events before returning — mirrors the single-mode path
		// (workloop.go beadRunOne, the errors.Is branches after Launch). Without
		// this the DOT path returned an opaque "launch node ...: %w" error and the
		// operator never saw WHY the launch failed (spawn-pool saturated vs. hung
		// tmux new-window). The returned launchErr already wraps handler.ErrStructural
		// (SpawnWindow stamps it), so the cascade's existing structural-error
		// handling reopens the bead — these branches only add the observability the
		// single-mode path already has.
		if errors.Is(launchErr, ErrSpawnCapTimeout) {
			inUse, capSize := substrateSpawnStats(deps.substrate)
			emitSpawnCapBlocked(ctx, deps.bus, runID, time.Since(nodeLaunchedAt), inUse, capSize)
		}
		if errors.Is(launchErr, ErrTmuxNewWindowTimeout) {
			emitTmuxNewWindowTimeout(ctx, deps.bus, runID, time.Since(nodeLaunchedAt))
		}
		return core.Outcome{}, fmt.Errorf("launch node %q: %w", node.ID, launchErr)
	}

	// hk-goczd: the tmux window has actually spawned (Launch returned a live
	// session) — emit the held-back launch_initiated now. This clears the
	// false-positive launch_stall_detected the DOT path otherwise triggered on
	// every run. Mirrors workloop.go:2137-2139.
	if nodeLaunchInitiatedMsg != nil {
		emitPreExecMessage(ctx, deps.bus, runID, nodeLaunchInitiatedMsg)
	}

	// hk-goczd / hk-68pvl: force-tear-down the session before this function
	// returns on EVERY exit path (success, ctx-cancel, verdict-read error, HEAD
	// resolution error, reviewer-success return). The success path below kills the
	// session only when watcher == nil (the substrate path, dot_cascade.go ~line
	// 674); this defer is the slot-reclaim backstop that guarantees the spawn
	// semaphore slot (hk-xb5yi / hk-4l7zs) is returned even on the exec path or any
	// early return between here and that conditional kill. Kill is idempotent
	// (killOnce), so this is a no-op when the session was already torn down.
	defer forceTeardownSession(sess)

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
	adapter, adapterErr := deps.adapterRegistry.ForAgent(artifactAgentType(artifacts))
	if adapterErr != nil {
		// No adapter for the resolved agent type — non-fatal; skip ready-wait.
		fmt.Fprintf(os.Stderr, "daemon: dot: ForAgent(%s) node %q: %v (skipping ready-wait)\n",
			artifactAgentType(artifacts), node.ID, adapterErr)
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
		phase, iterationCount, wtPath, deps.bus, runID)
	if qs, ok := pasteTarget.(quitSender); ok {
		if isReviewer {
			// hk-7rgqs: pass the pasteInjecter + claude session id so the watchdog
			// can re-seed the reviewer brief once if the original submit Enter was
			// swallowed by a slow splash (pasteTarget implements pasteInjecter when
			// it is a perRunSubstrate; a non-pasteInjecter target yields a nil inj
			// inside the watchdog → re-seed disabled).
			revInj, _ := pasteTarget.(pasteInjecter)
			go pasteInjectQuitOnReviewFile(ctx, qs, sess, revInj, artifacts.claudeSessionID, wtPath, briefDelivered)
		} else {
			// hk-o90sl (T13/C5): gate on Completion() policy (specs/harness-contract.md §2 N5).
			// ProcessExit harnesses (codex) self-terminate when the turn completes; sess.Wait +
			// commitHardCeiling detect completion without a /quit injection. Only launch the
			// watchdog for PasteInjectQuit harnesses (claude — the default when the registry is
			// absent or the agent type is unregistered).
			completionMode := handlercontract.CompletionEventStreamThenQuit
			if deps.harnessRegistry != nil {
				if h, hErr := deps.harnessRegistry.ForAgent(artifactAgentType(artifacts)); hErr == nil {
					completionMode = h.Completion()
				}
			}
			if completionMode != handlercontract.CompletionProcessExit {
				// hk-37giq: give the watchdog its OWN independent subscription
				// (tap.Subscribe()) rather than sharing tapCh with waitAgentReady.
				// Sharing one channel let waitAgentReady's drain goroutine steal every
				// heartbeat under concurrent dispatch, wedging this watchdog in the
				// launch-suppression branch forever. The fan-out tap delivers each
				// consumer its own copy of every event.
				watchdogCh := tap.Subscribe()
				go pasteInjectQuitOnCommit(ctx, qs, sess, wtPath, preHeadSHA, nil, briefDelivered, watchdogCh, deps.bus, runID)
			}
		}
	}

	_, nodeEI := waitWithSocketGrace(ctx, deps.hookStore, watcher, sess,
		runID.String(), artifacts.claudeSessionID)

	if watcher == nil {
		_ = sess.Kill(context.Background())
	}

	// Emit implementer_phase_complete (hk-cd8yu / hk-mvjs4) immediately after the
	// implementer session ends, mirroring workloop.go:1697 and reviewloop.go:526.
	// Skipped for reviewer-class nodes (they produce reviewer_verdict instead).
	if !isReviewer {
		curHead, _ := resolveDotWorktreeHEAD(ctx, runner, wtPath)
		commitLanded := curHead != "" && curHead != preHeadSHA
		emitImplementerPhaseComplete(ctx, deps.bus, runID, nodeEI.exitCode,
			nodeEI.stderrTail, commitLanded, time.Since(nodeLaunchedAt))
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
			// hk-da3rr: distinguish a BUDGET kill from a true no-verdict, mirroring
			// the builtin review-loop path (reviewloop.go). The marker file is written
			// into the reviewer's worktree by writeReviewerBudgetSentinel.
			if sentinel, sErr := ReadReviewerBudgetSentinel(wtPath); sErr == nil && sentinel != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: dot: reviewer node %q budget exceeded (reason=%s budget_ms=%d elapsed_ms=%d changed_lines=%d)\n",
					node.ID, sentinel.Reason, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines)
				emitReviewerBudgetExceeded(ctx, deps.bus, runID, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines, sentinel.Reason)
			}
			// hk-bqf1q: return the typed sentinel so driveDotWorkflow can detect
			// a reviewer stall and retry when committed work exists, rather than
			// hard-failing and stranding the valid impl commit.
			return core.Outcome{}, fmt.Errorf("%w (node %q)", errDotReviewerNoVerdict, node.ID)
		}
		// Emit reviewer_verdict matching the builtin review-loop path (reviewloop.go:932).
		// WorkflowMode is DOT; session_id reuses the reviewerSessionID minted before
		// launch (hk-c73fs: reviewer_launched uses the same ID so the two events
		// are correlated); claude_session_id is the reviewer node's Claude session.
		emitDotReviewerVerdict(ctx, deps.bus, runID, reviewerSessionID, artifacts.claudeSessionID, iterationCount, verdict)
		label := verdict.Verdict
		flags := verdict.Flags
		if flags == nil {
			flags = []string{}
		}
		return core.Outcome{
			Status:              core.OutcomeStatusSuccess,
			PreferredLabel:      &label,
			PreferredLabelFlags: flags, // hk-m1wqp: carries reviewer flags to driveDotWorkflow for review_fixup_stalled
		}, nil
	}

	// Implementer-class node: require HEAD to have advanced past its pre-launch
	// state (per EM-015d). Gate on node.NonCommitting per WG-041 §I.4 /
	// EM-058 non-committing sub-note (§II.8): when non_committing="true", a
	// clean exit yields SUCCESS without requiring HEAD advance; when false
	// (default), no HEAD advance is a node failure on iteration 1. In all modes
	// an unresolvable HEAD is a daemon-side error (broken worktree).
	//
	// Iteration ≥ 2 exception (EM-015e DOT-mode parity): when the implementer
	// exits without advancing HEAD on iteration ≥ 2, we return SUCCESS and allow
	// the diff-hash no-progress check in driveDotWorkflow to fire before the next
	// reviewer dispatch — exactly mirroring the review-loop path, which defers
	// the analogous "no new commit" case to the diff-hash check (reviewloop.go
	// defers to state.iterationCount >= 2 in its diff-hash block rather than the
	// no-commit guard which fires only on iteration 1).
	postHeadSHA, headErr := resolveDotWorktreeHEAD(ctx, runner, wtPath)
	if headErr != nil {
		return core.Outcome{}, fmt.Errorf("resolve HEAD after node %q: %w", node.ID, headErr)
	}
	if postHeadSHA == preHeadSHA && !node.NonCommitting {
		// Mirror the builtin noChange-subsumed check (workloop.go:1831-1848,
		// hk-trjef): if the bead's work already landed in main, close-subsumed
		// rather than hard-fail. Bead: hk-9v5yo.
		if beadAlreadySubsumedInMain(ctx, deps.projectDir, beadID) {
			return core.Outcome{}, errDotNoChangeSubsumed
		}
		if iterationCount < 2 {
			// First iteration: HEAD MUST advance. Hard-fail.
			return core.Outcome{}, fmt.Errorf("node %q (implementer) exited without advancing HEAD past %s", node.ID, preHeadSHA)
		}
		// Iteration ≥ 2: return SUCCESS; driveDotWorkflow's diff-hash check at
		// the next reviewer dispatch will detect no-progress and terminate.
		return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
	}
	// auto_status: when auto_status="true" on the implementer node, run a
	// deterministic work-product inspection before finalizing SUCCESS.
	// On pass: unchanged SUCCESS path. On fail: FAIL+deterministic.
	// AR-006-clean — see runAutoStatusInspection for the mechanism-tag guarantee.
	if node.AutoStatus {
		if outcome, pass := runAutoStatusInspection(ctx, runner, wtPath); !pass {
			return outcome, nil
		}
	}
	return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
}

// runAutoStatusInspection runs the deterministic work-product inspection for
// auto_status="true" implementer nodes. It mirrors the merge-build-gate
// (workloop.go:4127) against the implementer's worktree (wtPath) immediately
// after the implementer exits, before the SUCCESS outcome is finalized.
//
// AR-006 mechanism-tag: this function is wholly deterministic — it executes
// go build ./... and go vet ./... (exit-code evaluation only) with ZERO LLM
// calls. Any new signal added here MUST remain deterministic and LLM-free.
// This is a mechanism-tagged evaluation point per execution-model.md §4.2.
//
// Only active when a go.mod is present in wtPath (mirrors merge-build-gate;
// non-Go projects and bare-repo fixtures are unaffected).
//
// Remote runs (runner != nil): wtPath lives on the worker's filesystem, which
// box A cannot stat / chdir into. So the go.mod probe, the go build/vet gate, and
// the C2 marker read all route through runner so they run ON THE WORKER. Like the
// shell tool node, the build/vet gate is run under a login shell — `/bin/sh -lc
// 'cd <wt> && go build ./...'` — because SSHRunner does NOT forward cmd.Env, so the
// worker's own PATH (homebrew toolchain) must be sourced for `go` to resolve.
// NFR7: the LOCAL (runner == nil) branch is byte-identical to the prior code.
//
// Returns (outcome, false) on inspection failure — caller should return the
// outcome. Returns (zero, true) on pass — caller continues to SUCCESS.
func runAutoStatusInspection(ctx context.Context, runner tmux.CommandRunner, wtPath string) (core.Outcome, bool) {
	if !autoStatusHasGoMod(ctx, runner, wtPath) {
		return core.Outcome{}, true // no go.mod: pass through
	}
	fc := core.FailureClassDeterministic
	for _, buildArgs := range [][]string{
		{"build", "./..."},
		{"vet", "./..."},
	} {
		var cmd *exec.Cmd
		if runner == nil {
			// LOCAL run (NFR7: byte-identical to the pre-remote path).
			cmd = exec.CommandContext(ctx, "go", buildArgs...)
			cmd.Dir = wtPath
		} else {
			// REMOTE run: cd into the worker's worktree under a login shell so
			// the worker's PATH resolves `go`; SSHRunner does not forward cmd.Env.
			cmd = runner.Command(ctx, "/bin/sh", "-lc",
				fmt.Sprintf("cd %s && go %s", shellQuote(wtPath), strings.Join(buildArgs, " ")))
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return core.Outcome{
				Status:       core.OutcomeStatusFail,
				Kind:         core.OutcomeKindDefault,
				FailureClass: &fc,
				Notes:        fmt.Sprintf("auto_status inspection failed (go %s): %v\n%s", buildArgs[0], err, string(out)),
			}, false
		}
	}
	// C2: deny-side marker check per EM-068.
	// C1 already passed above; C2 fires only when C1 is clean (D3: C1 authoritative,
	// C1 FAIL short-circuits before reaching here).
	// D1: deny-side only — absent/non-FAIL markers are treated as absent by
	// ReadAutoStatusMarker, so C1-only pass-through is preserved.
	// D4: derived FAIL is terminal; no reviewer-loop re-entry.
	marker, _ := readAutoStatusMarkerVia(ctx, runner, wtPath)
	if marker != nil {
		c2fc := core.FailureClassDeterministic // HC-059 daemon back-fill when hint absent.
		if marker.FailureClass != "" {
			c2fc = core.FailureClass(marker.FailureClass)
		}
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			Kind:         core.OutcomeKindDefault,
			FailureClass: &c2fc,
			Notes:        marker.Notes,
		}, false
	}
	return core.Outcome{}, true
}

// autoStatusHasGoMod reports whether a go.mod exists at the worktree root,
// routing the probe through runner so it targets the worker's filesystem on a
// remote run. NFR7: the LOCAL (runner == nil) branch is byte-identical to the
// prior os.Stat(filepath.Join(wtPath, "go.mod")) check.
func autoStatusHasGoMod(ctx context.Context, runner tmux.CommandRunner, wtPath string) bool {
	if runner == nil {
		_, err := os.Stat(filepath.Join(wtPath, "go.mod"))
		return err == nil
	}
	// REMOTE: `test -f <wt>/go.mod` on the worker; exit 0 = present.
	err := runner.Command(ctx, "test", "-f", filepath.Join(wtPath, "go.mod")).Run()
	return err == nil
}

// readAutoStatusMarkerVia reads + validates the C2 auto_status marker, routing
// the read through runner on a remote run so the marker is read from the worker's
// filesystem. NFR7: the LOCAL (runner == nil) branch calls the unchanged
// workspace.ReadAutoStatusMarker. The remote branch streams the file bytes over
// the runner (cat) and applies the identical validation via
// workspace.ParseAutoStatusMarker; a missing file (cat exits non-zero) yields a
// nil marker = C1-only pass-through, matching the local not-exist behavior.
func readAutoStatusMarkerVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (*workspace.AutoStatusMarker, error) {
	if runner == nil {
		return workspace.ReadAutoStatusMarker(wtPath)
	}
	out, err := runner.Command(ctx, "cat", workspace.AutoStatusMarkerPath(wtPath)).Output()
	if err != nil {
		// Absent marker (cat: no such file) or transport hiccup → treat as absent,
		// preserving C1-only pass-through (the local not-exist path returns nil,nil).
		return nil, nil //nolint:nilnil // absent/unreadable marker = C1-only gate per HC-068
	}
	return workspace.ParseAutoStatusMarker(out), nil
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
//
// Environment (hk-m5axg): for a LOCAL run the shell command inherits the daemon's
// full process environment (os.Environ()) with the handler-supplied env layered ON
// TOP so any operator overrides win on duplicate keys. The handler env alone is just
// HARMONIK_PROJECT_HASH (cfg.HandlerEnv is nil in production — see
// cmd/harmonik/main.go daemon.Config) and crucially carries NO PATH. Without the
// inherited environment, `/bin/sh -c "go build ..."` cannot find `go` (exit 127),
// so the standard-bead commit_gate node always returned a deterministic FAIL and
// the cascade looped commit_gate→implement forever, never reaching the review
// node — the run went run_stale. Unlike the claude implementer/reviewer launches
// (which inherit a full shell env via the tmux substrate), this shell node
// exec.CommandContext's directly and so must reconstruct the environment itself.
//
// Remote runs (runner != nil, an SSHRunner): the gate MUST run ON THE WORKER, not
// box A — the worktree path lives on the worker's filesystem, which box A cannot
// chdir into (cmd.Dir = wtPath → chdir/"no such file"). We route the command
// through runner.Command so it tunnels to the worker. SSHRunner.Command does NOT
// propagate cmd.Env to the remote shell (it only forwards argv), so the os.Environ()
// inheritance that gives the LOCAL path its PATH does not apply remotely. Instead we
// build a LOGIN shell — `/bin/sh -lc 'cd <wtPath> && <ToolCommand>'` — so the
// worker's own ~/.zprofile / ~/.profile PATH (which includes /opt/homebrew/bin,
// where go/git/claude resolve on the worker) is sourced before the gate runs. The
// cd anchors the gate at the worker's worktree the way cmd.Dir does locally.
// NFR7: the LOCAL (runner == nil) branch is byte-identical to the prior code.
func dispatchDotToolNode(ctx context.Context, runner tmux.CommandRunner, wtPath string, node *dot.Node, env []string) (core.Outcome, error) {
	timeoutSecs := 300
	if node.Timeout != "" {
		if n, err := strconv.Atoi(node.Timeout); err == nil && n > 0 {
			timeoutSecs = n
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runner == nil {
		// LOCAL run (NFR7: byte-identical to the pre-remote path).
		cmd = exec.CommandContext(execCtx, "/bin/sh", "-c", node.ToolCommand)
		cmd.Dir = wtPath
		// Inherit the daemon's process env (PATH, HOME, GOPATH, …) then layer the
		// handler-supplied entries last so they override on duplicate keys.
		cmd.Env = append(os.Environ(), env...)
	} else {
		// REMOTE run: route the gate through the worker's runner. cd into the
		// worker's worktree and run under a login shell so the worker's PATH
		// (homebrew toolchain) is sourced — SSHRunner does NOT forward cmd.Env.
		cmd = runner.Command(execCtx, "/bin/sh", "-lc",
			fmt.Sprintf("cd %s && %s", shellQuote(wtPath), node.ToolCommand))
	}

	// CAPTURE the combined stdout+stderr instead of discarding it (hk-pj4b6).
	// On a deterministic gate FAIL the cascade loops back to the implementer; the
	// diagnostic (which `go build`/`go vet`/test step failed and why) is the single
	// most useful thing to feed that re-entering implementer. Previously cmd.Run()
	// threw the output away, so a re-entering implementer had no signal about what
	// to fix and tended to re-commit nothing — the no-escape loop. We retain the
	// tail in Outcome.Notes (observability surface, opaque to the cascade) and write
	// the full output to a dedicated gate log file so the daemon log stays clean.
	combined, err := cmd.CombinedOutput()
	if err == nil {
		return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
	}

	// F30: write full gate output to a dedicated file; never tee go test / scenario
	// output into the daemon log. The daemon log gets a one-line pointer instead.
	// LOCAL only: for a REMOTE run wtPath is the worker's filesystem (absent on
	// box A), so this write would silently fail — skip it. The actionable tail is
	// still captured in Outcome.Notes from the combined output streamed back over
	// the runner, so the re-entering implementer keeps its diagnostic.
	gateLogPath := filepath.Join(wtPath, ".harmonik", "commit-gate.log")
	if runner == nil {
		_ = os.WriteFile(gateLogPath, combined, 0o644)
	}

	outputTail := tailString(string(combined), dotGateOutputTailBytes)

	// Timeout-killed: parent deadline exceeded first.
	if execCtx.Err() == context.DeadlineExceeded {
		fc := core.FailureClassTransient
		fmt.Fprintf(os.Stderr, "daemon: dot tool node %q timed out after %ds; gate log: %s\n", node.ID, timeoutSecs, gateLogPath)
		return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc, Notes: outputTail}, nil
	}

	// Parent context cancelled (operator stop / SIGKILL / ctx-cancel).
	if ctx.Err() != nil {
		fc := core.FailureClassCanceled
		return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc, Notes: outputTail}, nil
	}

	// Non-zero exit code (1..255) → deterministic failure. This is the gate-FAIL
	// case that drives the commit_gate→implement back-edge; surface the diagnostic.
	fc := core.FailureClassDeterministic
	fmt.Fprintf(os.Stderr, "daemon: dot tool node %q failed (%v); gate log: %s\n", node.ID, err, gateLogPath)
	return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc, Notes: outputTail}, nil
}

// dotGateOutputTailBytes bounds how much of a failed tool node's combined output
// is retained in Outcome.Notes / logged. Gate output (full `go build`/`go vet`/
// test logs) can be large; the tail carries the actionable failure lines.
const dotGateOutputTailBytes = 4096

// shellQuote wraps s in single quotes for safe interpolation into a remote
// `/bin/sh -lc '<script>'` string, escaping any embedded single quotes via the
// standard '\” idiom. Used only for the worktree path on the REMOTE gate path,
// so a worker worktree path containing spaces (or other shell metacharacters)
// survives the cd intact. The local path never shell-interpolates wtPath.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tailString returns the last n bytes of s (rune-boundary-safe at the cut),
// prefixed with a truncation marker when s was longer than n. Returns s
// unchanged when it already fits.
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	// Advance to the next rune boundary so we never emit a half-rune.
	for i := 0; i < len(cut) && i < 4; i++ {
		if utf8.RuneStart(cut[i]) {
			cut = cut[i:]
			break
		}
	}
	return "…(truncated)…\n" + cut
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

// emitDotNoProgressDetected emits no_progress_detected for the DOT cascade
// path (event-model.md §8.1a.5).  WorkflowMode is WorkflowModeDot so consumers
// can distinguish DOT-path no-progress events from review-loop-path ones.
// Unlike the review-loop path, DOT mode does NOT follow this with
// review_loop_cycle_complete — the cascade terminates directly.
func emitDotNoProgressDetected(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	iterationCount int,
	diffHashCurrent string,
	diffHashPrior string,
) {
	pl := core.NoProgressDetectedPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeDot,
		IterationCount:  iterationCount,
		DiffHashCurrent: diffHashCurrent,
		DiffHashPrior:   diffHashPrior,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeNoProgressDetected, b)
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

// emitDotReviewerLaunched emits reviewer_launched (§8.1a.2) for a DOT reviewer
// node, matching the builtin review-loop path (reviewloop.go emitReviewerLaunched).
// WorkflowMode is WorkflowModeDot so consumers filtering on workflow_mode=dot
// see a consistent launched/verdict pair (hk-c73fs).
func emitDotReviewerLaunched(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	sessionID core.SessionID,
	claudeSessionID string,
	iterationCount int,
) {
	pl := core.ReviewerLaunchedPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeDot,
		SessionID:       sessionID,
		ClaudeSessionID: claudeSessionID,
		IterationCount:  iterationCount,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerLaunched, b)
}

// emitDotReviewerVerdict emits reviewer_verdict for a DOT reviewer node,
// matching the builtin review-loop path (reviewloop.go emitReviewerVerdict).
// WorkflowMode is set to WorkflowModeDot to distinguish DOT-path verdicts.
func emitDotReviewerVerdict(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	sessionID core.SessionID,
	claudeSessionID string,
	iterationCount int,
	verdict *workspace.ReviewVerdict,
) {
	flags := verdict.Flags
	if flags == nil {
		flags = []string{}
	}
	pl := core.ReviewerVerdictPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeDot,
		SessionID:       sessionID,
		ClaudeSessionID: claudeSessionID,
		IterationCount:  iterationCount,
		SchemaVersion:   verdict.SchemaVersion,
		Verdict:         core.ReviewerVerdict(verdict.Verdict),
		Flags:           flags,
		Notes:           verdict.Notes,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerVerdict, b)
}
