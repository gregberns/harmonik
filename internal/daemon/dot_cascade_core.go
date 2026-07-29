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
// in graph.TerminalNodeIDs). dotTerminalNodeIsSuccess then asks the graph what
// reaching that terminal means: the WG-022 reserved pair ("close" /
// "close-needs-attention") is normative; any author-declared terminal supplies
// its own terminal_disposition; an undeclared terminal is unclassifiable and the
// run goes to needs-attention rather than merging on a guess. Consumers MUST NOT
// inspect inbound-edge topology to determine terminal disposition (WG-021).
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
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runlaunch"
	"github.com/gregberns/harmonik/internal/runloop"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

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
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
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
	// hk-538l worker-launch params: workerBinaryPath resolves each node's SessionStart
	// hook command to the WORKER's harmonik path; workerHookSock is the worker-side
	// reverse-tunnel TCP endpoint each node's claude dials for the hook relay;
	// workerSessionName/Cwd tell the per-run substrate which tmux session to ensure +
	// spawn into ON THE WORKER. All empty for a LOCAL run ⇒ byte-identical box-A path
	// (NFR7).
	workerBinaryPath string,
	workerHookSock string,
	workerSessionName string,
	workerSessionCwd string,
) dotWorkflowResult {
	// RSM-010: the run's EmitterPort, bound once for this call. Deliberately the
	// NARROW emitterPort accessor (runports.go) rather than the runPorts() bundle,
	// which would assemble every port for one read (RT18: the clock default it
	// once guarded now folds inside runPorts() via clockOrSystem).
	emit := ports.Emitter
	// hk-538l: for a REMOTE run rewrite the hook socket to the worker-side reverse-
	// tunnel TCP endpoint so the worker's claude can reach the relay; box A's local
	// unix daemon.sock is unreachable from the worker. Empty workerHookSock (LOCAL
	// run) ⇒ unchanged box-A unix socket (NFR7). Mirrors workloop.go single-mode
	// tunnel.ResolveAgentDaemonSocket; previously the box-A unix path flowed into every
	// node's rc.daemonSocket → HARMONIK_DAEMON_SOCKET → connect failure → no hook →
	// agent_ready_timeout.
	boxADaemonSocket := filepath.Join(env.ProjectDir, ".harmonik", "daemon.sock")
	daemonSocket := tunnelpkg.ResolveAgentDaemonSocket(workerHookSock, boxADaemonSocket)

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

	// priorVerdictNotes is the full notes text from the most recent reviewer
	// verdict, parallel to priorVerdict / priorVerdictFlags. It feeds the
	// reviewer-feedback.iter-<N-1>.md file written before an implementer-resume
	// back-edge (hk-wixms) so the resumed implementer receives the reviewer's
	// REQUEST_CHANGES notes — mirroring reviewloop.go's WriteReviewerFeedback
	// path. Empty before any reviewer has run.
	priorVerdictNotes := ""

	// lastGatePassed records whether the MOST RECENT shell commit-gate node
	// (build + vet + test-compile + scenario tests) produced a SUCCESS outcome.
	// hk-w2ow: the broadened completion exemption in the no-progress block
	// consults this to distinguish an ADVISORY-ONLY REQUEST_CHANGES re-entry
	// whose gate is GREEN (build + tests pass — nothing committable left, so
	// COMPLETION) from a genuinely-stalled rework re-entry whose gate is RED
	// (build/test failure still un-addressed — FAIL). False until a gate node
	// has run, so a gate-less graph can NEVER take the broadened exemption: it
	// cannot assert the gate passes, so it preserves the prior fail behavior.
	lastGatePassed := false
	// lastGateNotes holds Outcome.Notes from the most recent gate failure
	// (the actionable tail of build/test output). Captured alongside
	// lastGatePassed so the commit_gate→implement back-edge can deliver the
	// real failure reason instead of the misleading NO-commit nudge. Empty
	// until a gate has run and failed (hk-778x9).
	lastGateNotes := ""

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

	// axisReviewerVerdicts records the latest verdict produced by each
	// reviewer-class node, keyed by node ID, during the current review pass. A
	// consolidate-style join node (a reviewer with >= 2 upstream reviewer
	// predecessors) reads its upstream axes from this map and routes on the
	// DETERMINISTIC severity-max (BLOCK > REQUEST_CHANGES > APPROVE) of those
	// axes — OVERRIDING its own self-reported verdict. This closes the
	// review-integrity hole (hk-cmry) where a consolidate LLM that self-reports
	// APPROVE while an upstream axis said REQUEST_CHANGES would route to close
	// and merge unreviewed-rejected work. Reset whenever an implementer
	// (re-)enters, so each fresh implementation cycle re-collects all axis
	// verdicts before the next consolidate join.
	axisReviewerVerdicts := make(map[string]string)

	// hk-nvd3 — configurable no-progress guard.
	//
	// noProgressGuardOff: when true the guard never fires (graph sets
	// no_progress_guard="off"). Code workflows should always leave this false.
	//
	// noProgressGuardCap: when > 0 the graph sets no_progress_guard="capped:N".
	// The guard fires only after noProgressGuardCap+1 CONSECUTIVE no-progress
	// iterations (i.e. N allowed before the (N+1)th fires). When 0 and !off the
	// guard fires immediately at the first no-progress iteration (strict / default).
	//
	// consecutiveNoProgressCount counts how many consecutive agentic-node entries
	// have been reached with !headAdvanced (after the completion exemptions). Reset
	// to 0 whenever headAdvanced==true so a real commit always resets the count.
	noProgressGuardOff := false
	noProgressGuardCap := 0
	switch {
	case graph.NoProgressGuard == "off":
		noProgressGuardOff = true
	case strings.HasPrefix(graph.NoProgressGuard, "capped:"):
		// Already validated by the parser; Atoi cannot fail here.
		noProgressGuardCap, _ = strconv.Atoi(strings.TrimPrefix(graph.NoProgressGuard, "capped:"))
	}
	consecutiveNoProgressCount := 0

	// prevAgenticNodeWasReviewer tracks whether the immediately preceding agentic
	// node was a reviewer. The hk-8ps7q APPROVE-completion exemption uses this to
	// distinguish two structurally-identical HEAD-unchanged re-entries:
	//
	//   (a) Multi-reviewer fan-out: review_1 APPROVE → review_2 (prev=reviewer).
	//       review_2 must actually run — MUST NOT complete here.
	//   (b) Implement (no commit) → review (prev=implementer). An implementer ran
	//       after the APPROVE but produced no new commit (nothing left to do);
	//       this entry is APPROVED-AND-DONE and MUST COMPLETE.
	//
	// By gating the exemption on !prevAgenticNodeWasReviewer instead of !isReviewer
	// we allow case (b) to complete while preserving case (a).
	prevAgenticNodeWasReviewer := false

	// prevNodeID is the node ID processed in the IMMEDIATELY preceding loop
	// iteration (the edge source that routed into the current node). hk-wixms
	// uses it to pick the correct implementer-resume message: a re-entry whose
	// inbound edge came from a reviewer node delivers the reviewer's verdict,
	// while a re-entry from a commit_gate (or any non-reviewer) node delivers the
	// "no commit" nudge — distinguishing the two even in the production
	// review[RC]→implement[commits]→commit_gate[FAIL]→implement trace, where
	// priorVerdict alone would carry stale REQUEST_CHANGES state. Empty on the
	// first iteration. Updated at the end of each loop iteration.
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

		// Emit node_dispatch_requested (O-class observability) before handling the
		// node, per event-model.md §8.1.11.
		emitNodeDispatchRequested(ctx, emit, ports.Clock, runID, core.NodeID(currentNodeID))

		var outcome core.Outcome

		switch node.Type {
		case core.NodeTypeNonAgentic:
			switch {
			case node.ToolCommand != "" && node.HandlerRef == "shell":
				// Path 1: shell tool node — execute tool_command via the built-in
				// in-process shell handler (WG-039 / HC-063). MAY run in-process;
				// no subprocess/socket/NDJSON/agent_ready required. It DOES, however,
				// require a daemon-emitted heartbeat: a long-running gate (the default
				// commit_gate: go build/vet/test + scenario-gate, up to 900s) produces
				// no NDJSON stream, so without an explicit heartbeat the stale watcher
				// sees no event for the run for the gate's full duration and false-fires
				// run_stale, re-dispatching the gate without killing the prior shell
				// (hk-vjsv). dispatchDotToolNode now ticks agent_heartbeat for the run
				// while the gate command runs (both local and remote paths).
				//
				// hk-t1t00: augment the gate env with HK_GATE_BASE_SHA=parentSHA so
				// scripts/scenario-gate.sh uses the run's own branch-point as the diff
				// base rather than falling back to `git merge-base origin/main HEAD`.
				// On a remote worker origin/main lags real main, inflating the diff to
				// hundreds of files → the full test suite exceeds the 900s gate timeout
				// → transient self-loop → cap. parentSHA is the exact commit the
				// worktree was branched from, so the affected-set is bounded to what
				// this bead actually changed. LOCAL runs benefit too (correctness), but
				// the problem is acute for remote workers whose ref is stale.
				gateEnv := env.HandlerEnv
				if parentSHA != "" {
					gateEnv = append(append(make([]string, 0, len(env.HandlerEnv)+1), env.HandlerEnv...), "HK_GATE_BASE_SHA="+parentSHA)
				}
				toolOutcome, toolErr := dispatchDotToolNode(ctx, emit, runID, runner, wtPath, node, gateEnv)
				if toolErr != nil {
					return dotWorkflowResult{
						success:        false,
						needsAttention: true,
						summary:        fmt.Sprintf("dot: tool node %q dispatch error: %v", currentNodeID, toolErr),
					}
				}
				outcome = toolOutcome
				// hk-w2ow: record whether this build/test gate passed. The
				// broadened completion exemption (no-progress block below) treats an
				// advisory-only REQUEST_CHANGES re-entry with a GREEN gate as
				// COMPLETION; a RED gate (build/test failure) still fails as stalled
				// rework. Most-recent semantics are sound here: the no-progress check
				// only fires when HEAD is UNCHANGED, so the gate result reflects the
				// exact tree under review.
				lastGatePassed = outcome.Status == core.OutcomeStatusSuccess
				if !lastGatePassed {
					lastGateNotes = outcome.Notes
				}

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
			currentHash, hashErr := computeDiffHashVia(ctx, runner, wtPath, parentSHA)
			if hashErr != nil {
				return dotWorkflowResult{
					success:        false,
					needsAttention: false,
					summary:        fmt.Sprintf("dot: diff-hash error before agentic node %q at iteration %d: %v", currentNodeID, iterationCount, hashErr),
				}
			}
			headAdvanced := priorIterHeadSHA == "" || currentHead != priorIterHeadSHA
			// committedResult: is there a committed result on this run at all (HEAD
			// past the run baseline parentSHA)? Computed here (rather than only
			// inside the no-progress block) because hk-nwgj7 needs it to gate the
			// FIRST-reviewer-entry suppression below.
			committedResult := parentSHA == "" || currentHead != parentSHA
			// hk-nwgj7: suppress the no-progress check entirely when the upcoming
			// node is a reviewer that has NEVER produced a verdict on this run
			// (priorVerdict == "") and there is gate-green committed work to
			// review. Before this fix, the hk-du455 case-4 exemption (below) fired
			// HERE and returned success WITHOUT ever dispatching the reviewer —
			// merging committed code with no reviewer verdict at all (the review
			// gate was silently bypassed). Reviewers never advance HEAD by design,
			// so a HEAD-unchanged first entry into the reviewer is not evidence of
			// being "stuck" — it just means the reviewer has not run yet. Skipping
			// the whole guard here lets the walk fall through to the normal
			// dispatch path so the reviewer actually reviews the committed work.
			firstReviewerEntryWithGreenGate := isReviewer && priorVerdict == "" && committedResult && lastGatePassed
			// hk-ycxfa: suppress the no-progress check when retrying a stalled reviewer
			// (hk-bqf1q follow-up). A reviewer retry does not advance HEAD (reviewers
			// never commit), so the check would fire prematurely when iterationCount >= 2
			// and priorVerdict == REQUEST_CHANGES — exactly the scenario hk-bqf1q was
			// meant to rescue. The retry is already gated by reviewerNoVerdictRetries <
			// dotMaxReviewerNoVerdictRetries; if the retry also stalls, hard-fail fires
			// below via the exhausted-budget branch.
			if iterationCount >= 2 && !headAdvanced && !(isReviewer && reviewerNoVerdictRetries > 0) && !firstReviewerEntryWithGreenGate {
				// hk-8ps7q — approved-and-done is COMPLETION, not no-progress.
				//
				// The no-progress condition (iter ≥ 2 + HEAD unchanged) is met by
				// THREE structurally-distinct situations, disambiguated by the prior
				// reviewer verdict AND the most-recent build/test gate state:
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
				//
				// committedResult was computed above (before this block) so the
				// hk-nwgj7 first-reviewer-entry suppression could consult it too.
				// hk-2vpj / hk-8ps7q: gate the APPROVE-completion exemption on
				// !prevAgenticNodeWasReviewer (rather than !isReviewer).
				//
				// Two HEAD-unchanged re-entries at iter ≥ 2 look identical from inside the
				// no-progress block; the previous agentic node type disambiguates them:
				//
				//   (a) Multi-reviewer fan-out: review_1 APPROVE → review_2
				//       (prevAgenticNodeWasReviewer=true). review_2 must actually run —
				//       MUST NOT complete here. (Preserves the hk-2vpj invariant.)
				//
				//   (b) Implement (no commit) → review: after the APPROVE an implementer
				//       ran but produced no new commit (nothing left to do); the reviewer
				//       re-entry is APPROVED-AND-DONE (prevAgenticNodeWasReviewer=false)
				//       → MUST COMPLETE and merge the approved work. (Fixes the
				//       regression where !isReviewer only covered the case where the
				//       NEXT node is the implementer, not the case where the graph
				//       routes implement→review after the post-APPROVE no-commit run.)
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
				// hk-w2ow — advisory-only REQUEST_CHANGES + GREEN gate is COMPLETION.
				//
				//   (3) ADVISORY AND DONE: there IS a committed result AND the prior
				//       reviewer returned REQUEST_CHANGES (advisory severity — NOT a
				//       BLOCK, per the hk-cmry BLOCK>RC>APPROVE severity-join that
				//       sets priorVerdict) AND the most-recent build/test gate passed
				//       (lastGatePassed). A REQUEST_CHANGES carrying only advisory /
				//       nitpick feedback has nothing committable: the implementer
				//       correctly added no new commit, HEAD stays put, and the gate
				//       is still green. Firing the stalled-rework failure here would
				//       discard finished, tested, gate-green work. The run instead
				//       COMPLETES so the caller merges it.
				//
				// This must STILL fail genuinely-stalled rework. A BLOCK verdict
				// never reaches this branch (priorVerdict != REQUEST_CHANGES → it
				// falls through to no_progress_detected below). A REQUEST_CHANGES
				// whose gate is RED (build/test still failing — real, un-addressed
				// work) has lastGatePassed == false, so it skips this branch and
				// falls through to review_fixup_stalled below, exactly as before.
				if committedResult && priorVerdict == workspace.ReviewVerdictRequestChanges && lastGatePassed {
					return dotWorkflowResult{
						success:    true,
						advisoryRC: true,
						summary:    fmt.Sprintf("dot: completed at iteration %d — REQUEST_CHANGES was advisory-only (commit gate green; HEAD final, nothing committable remained) (hk-w2ow)", iterationCount),
					}
				}
				// hk-du455 — committed + gate-green, no reviewer verdict yet, and the
				// upcoming node is NOT a reviewer: COMPLETION.
				//
				//   (4) COMMITTED + GATE-GREEN + NO VERDICT YET + NO REVIEWER TO RUN:
				//       there IS a committed result (HEAD is past parentSHA) AND no
				//       reviewer has produced a verdict yet (priorVerdict == "") AND
				//       the most-recent build/test gate passed (lastGatePassed) AND the
				//       node about to be (re-)dispatched is NOT a reviewer. This covers
				//       graphs with no reviewer node downstream of the gate (or any
				//       other non-reviewer agentic re-entry with nothing left to do):
				//       firing no_progress here would discard valid, gate-green
				//       committed work that will never reach a review step anyway.
				//       The run instead COMPLETES so the caller preserves the work.
				//
				//       hk-nwgj7: when the upcoming node IS a reviewer that has not run
				//       yet, this case must NOT fire — completing here would merge
				//       committed code with no reviewer verdict at all (an unreviewed
				//       merge). That shape is instead handled by the
				//       firstReviewerEntryWithGreenGate suppression above, which skips
				//       this whole guard block so the reviewer actually dispatches.
				//       Defense-in-depth for hk-7xgu4; precedent: cap-hit salvage above.
				if committedResult && priorVerdict == "" && lastGatePassed && !isReviewer {
					return dotWorkflowResult{
						success: true,
						summary: fmt.Sprintf("dot: completed at iteration %d — committed work is gate-green with no prior reviewer verdict; preserving committed tree (hk-du455)", iterationCount),
					}
				}
				// hk-nvd3 — configurable no-progress guard.
				// The completion exemptions above (APPROVE + committed, advisory
				// RC + green gate) are evaluated BEFORE this knob and remain in
				// effect regardless of guard mode: they represent genuine COMPLETION,
				// not stalled rework.  The knob only controls genuinely-stuck cases.
				//
				//   "off"      — skip the guard entirely; continue the walk.
				//   "capped:N" — allow up to N consecutive IMPLEMENTER no-progress
				//                iterations; fire only after the (N+1)th. Reviewer
				//                entries do not count toward the cap (reviewers are
				//                never expected to advance HEAD).
				//   "" / "strict" — fire immediately (default, unchanged behavior).
				if noProgressGuardOff {
					// Guard disabled: fall through to continue the walk.
				} else {
					shouldFire := true
					if noProgressGuardCap > 0 {
						// Only implementer entries count toward the cap; reviewer
						// entries are expected to leave HEAD unchanged (they write
						// verdicts, not commits) and must not exhaust the budget.
						if !isReviewer {
							consecutiveNoProgressCount++
						}
						shouldFire = consecutiveNoProgressCount > noProgressGuardCap
					}
					if shouldFire {
						// hk-m1wqp: emit review_fixup_stalled (carrying the reviewer
						// flags) when the prior verdict was REQUEST_CHANGES and the
						// implementer made no new commit. Fall back to
						// no_progress_detected for the uncommon case where HEAD did not
						// advance without any prior reviewer verdict (e.g. a commit_gate
						// loop with no reviewer node).
						if priorVerdict == workspace.ReviewVerdictRequestChanges {
							emitReviewFixupStalled(ctx, emit, runID, core.WorkflowModeDot,
								iterationCount, priorVerdictFlags, currentHash, lastDiffHash)
							return dotWorkflowResult{
								success:        false,
								needsAttention: true,
								summary:        fmt.Sprintf("dot: review fix-up stalled at iteration %d: HEAD did not advance after REQUEST_CHANGES", iterationCount),
							}
						}
						emitDotNoProgressDetected(ctx, emit, runID, iterationCount, currentHash, lastDiffHash)
						return dotWorkflowResult{
							success:        false,
							needsAttention: true,
							summary:        fmt.Sprintf("dot: no-progress detected at iteration %d: HEAD did not advance", iterationCount),
						}
					}
				}
			}
			// hk-nvd3: reset consecutive no-progress counter when HEAD has
			// advanced AND this is an implementer entry (a new commit from the
			// implementer resets the streak; reviewer entries cannot advance HEAD).
			if headAdvanced && !isReviewer {
				consecutiveNoProgressCount = 0
			}
			lastDiffHash = currentHash
			// hk-2vpj: only advance priorIterHeadSHA for implementer nodes. Reviewers
			// never commit, so updating the baseline on every reviewer entry would make
			// headAdvanced=false for the NEXT reviewer (reviewer-to-reviewer transition
			// in a multi-reviewer fan-out) and wrongly trigger the no-progress guard.
			// By anchoring the baseline to the last IMPLEMENTER entry, all reviewers in
			// a fan-out that follows a committing implementer see headAdvanced=true and
			// correctly skip the guard.
			if !isReviewer {
				priorIterHeadSHA = currentHead
			}

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
				// hk-cmry: a fresh implementation cycle invalidates the prior
				// pass's per-axis reviewer verdicts; clear them so the next
				// consolidate join aggregates only the current cycle's axes.
				axisReviewerVerdicts = make(map[string]string)
				// T14 hk-iv748: capture the reviewer_harness override from this
				// implementer node. A new implementer dispatch resets the override so
				// stale values from prior implementer cycles do not bleed into the next.
				lastImplementerReviewerHarness = core.AgentType(node.ReviewerHarness)

				// hk-wixms: deliver an ACTIONABLE instruction to the resumed
				// implementer on a back-edge re-entry (iterationCount >= 2). The
				// implementer-resume paste-inject (pasteInjectImplementerResume) reads
				// .harmonik/reviewer-feedback.iter-<N-1>.md from the worktree; without
				// this file it degrades to a bare "read agent-task.md and begin" —
				// the resumed session (which already produced satisfying work in its
				// prior pass) then has nothing concrete to do, sits idle until the
				// budget watchdog kills it, and the run thrashes (no commit →
				// no_progress → re-dispatch). This mirrors reviewloop.go's
				// WriteReviewerFeedback path, which the builtin review loop already
				// does correctly.
				//
				// Two distinct re-entry causes need two distinct messages:
				// Disambiguated by the INBOUND EDGE SOURCE (prevNodeID), not by
				// priorVerdict alone — priorVerdict carries stale REQUEST_CHANGES
				// state across an intervening implementer commit + commit_gate bounce
				// in the production review[RC]→implement[commits]→commit_gate[FAIL]→
				// implement trace.
				//   (a) reviewer → implement: the inbound edge came from a reviewer
				//       node that returned REQUEST_CHANGES — deliver the prior
				//       reviewer's verdict, flags, and notes verbatim.
				//   (b) commit_gate (or any non-reviewer) → implement: a deterministic
				//       gate FAIL / no-commit bounce — deliver an explicit "your
				//       previous pass produced NO commit — you MUST commit" nudge.
				//
				// Written to PriorIteration = iterationCount - 1 because the resume's
				// paste-inject looks for reviewer-feedback.iter-<iterationCount-1>.md
				// (priorIter = iterCount - 1 in pasteInjectImplementerResume).
				//
				// LOCAL only: WriteReviewerFeedback is a box-A-local os.WriteFile. For
				// a REMOTE DOT run wtPath is on the worker, so the write would not
				// reach the worker's worktree; the resume would still degrade. There
				// is no WriteReviewerFeedbackVia yet, so we log loudly and continue —
				// symmetric with reviewloop.go's REMOTE limitation (FLAGGED follow-up).
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
							// (a) reviewer REQUEST_CHANGES back-edge: deliver the verdict.
							rfPayload = workspace.ReviewerFeedbackPayload{
								WorkspacePath:  wtPath,
								PriorIteration: priorIter,
								Verdict:        priorVerdict,
								Flags:          priorVerdictFlags,
								Notes:          priorVerdictNotes,
							}
							priorSummary = truncateUTF8(priorVerdictNotes, priorVerdictSummaryMaxBytes)
						} else {
							// (b) commit_gate (or any non-reviewer) → implement back-edge.
							// Disambiguate on the actual cause:
							//   - commit_gate FAIL (lastGatePassed==false && prevNode is
							//     commit_gate): the implementer DID commit cleanly but the
							//     build/test gate failed. Deliver the gate failure output so
							//     the resumed implementer knows what to fix (hk-778x9).
							//   - genuine no-commit (anything else): HEAD did not advance;
							//     deliver the original commit nudge.
							fromGateFail := prevNode != nil && prevNode.ID == "commit_gate" && !lastGatePassed
							if fromGateFail && lastGateNotes != "" {
								gateFailMsg := "The commit gate failed — your commit was recorded but the build/test gate did not pass. " +
									"Fix the failure and re-commit:\n\n" + lastGateNotes
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
					// Emit implementer_resumed (§8.1a.1) BEFORE dispatch, mirroring the
					// review-loop path, so the resume carries prior_verdict_summary for
					// observability. WorkflowMode is DOT.
					emitDotImplementerResumed(ctx, emit, runID, claudeSessionID, iterationCount, priorSummary)
				}
			}
			// hk-x882o: mark the consolidate (verdict-join) node as a terminal
			// spawn so the substrate allocates the reserved +1 slot for it,
			// preventing starvation when all non-terminal slots are occupied.
			// The check is graph-structural and pure — safe to evaluate before
			// dispatch. The result is also used post-dispatch (line 883), so
			// computing it here avoids a second call.
			_, isConsolidate := isConsolidateJoinNode(graph, nodesByID, currentNodeID)
			nodeOutcome, nodeErr := dispatchDotAgenticNode(ctx, env, ports, handles, runID, beadID, beadRecord,
				beadTitle, beadDescription, wtPath, parentSHA, daemonSocket, node,
				isReviewer, iterationCount, &claudeSessionID,
				resolvedModel, resolvedEffort, extraContext, baseBranch,
				lastImplementerReviewerHarness, runner,
				workerBinaryPath, workerSessionName, workerSessionCwd,
				isConsolidate)
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

			// hk-cmry — DETERMINISTIC multi-reviewer severity-join.
			//
			// Record this reviewer node's self-reported verdict, then — if this
			// node is a consolidate-style JOIN node (a reviewer with >= 2 upstream
			// reviewer predecessors on the spine) — OVERRIDE the routing
			// preferred_label with the severity-max (BLOCK > REQUEST_CHANGES >
			// APPROVE) of those upstream per-axis verdicts. Routing MUST be the
			// deterministic join, never the consolidate LLM's self-report, so a
			// single over-lenient consolidate APPROVE can never merge work that
			// any axis-reviewer rejected (review-integrity hole: an unreviewed
			// RED-only commit reached main and broke the build fleet-wide).
			//
			// The consolidate node still produces a human-readable summary in
			// .harmonik/review.json; only the ROUTING label is overridden here.
			// Implementer-class nodes carry no preferred_label and are skipped.
			if isReviewer && outcome.PreferredLabel != nil {
				axisReviewerVerdicts[currentNodeID] = *outcome.PreferredLabel
				if upstream, isJoin := isConsolidateJoinNode(graph, nodesByID, currentNodeID); isJoin {
					// hk-0gnt: include self in the severity-max so a consolidate
					// node's own BLOCK can ESCALATE the join (never de-escalate it).
					// Self-APPROVE still cannot override an upstream BLOCK — the max
					// of upstream+self preserves the hk-cmry severity-integrity property
					// while closing the gap where a consolidate-caught BLOCK was lost.
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

			// hk-8ps7q: remember the most recent reviewer verdict so the
			// no-progress check above can distinguish an approved-and-done re-entry
			// (complete-and-merge) from a genuinely-stuck REQUEST_CHANGES re-entry
			// (no_progress-fail). Only reviewer nodes carry a preferred_label.
			// hk-bqf1q: also reset the stall retry counter — a real verdict
			// means the reviewer is no longer stalled.
			// hk-m1wqp: also capture flags from the verdict so review_fixup_stalled
			// can carry the specific REQUEST_CHANGES flags to triage.
			// hk-cmry: priorVerdict reflects the (possibly join-overridden) ROUTING
			// label in `outcome`, so the no-progress / fix-loop logic sees the same
			// verdict the cascade routes on.
			if isReviewer && outcome.PreferredLabel != nil {
				priorVerdict = *outcome.PreferredLabel
				flags := outcome.PreferredLabelFlags
				if flags == nil {
					flags = []string{}
				}
				priorVerdictFlags = flags
				// hk-wixms: capture the verdict notes so the next implementer-resume
				// back-edge can deliver them via reviewer-feedback.iter-<N-1>.md.
				priorVerdictNotes = outcome.Notes
				reviewerNoVerdictRetries = 0
			}

			// Track whether the previous agentic node was a reviewer so the
			// APPROVE-completion exemption (hk-8ps7q / hk-2vpj) can distinguish
			// a multi-reviewer fan-out (prev=reviewer → don't complete) from an
			// implement-no-commit→review transition (prev=implementer → complete).
			prevAgenticNodeWasReviewer = isReviewer

		case core.NodeTypeGate:
			// Gate dispatch: resolve gate_ref → ControlPoint, build GateEvalFunc
			// (mechanism: PolicyExpression eval; cognition: subprocess dispatch),
			// call handler.DispatchGateNode. Wired by hk-karlz.
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
			// Sub-workflow dispatch: resolve graph, check acyclicity, expand in
			// place, and run the nested cascade within the parent run (SW-001..SW-010).
			// Per SW-007, we build a dotSubWorkflowRunner and call Run.
			swRunner := newDotSubWorkflowRunner(
				env, ports, handles, runID, beadID, beadRecord, beadTitle, beadDescription,
				wtPath, parentSHA, daemonSocket,
				&iterationCount, &claudeSessionID, resolvedModel, resolvedEffort,
				extraContext, baseBranch, run, cycles, graph,
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
		emitNodeDispatchDecided(ctx, emit, decision.Payload)

		switch {
		case decision.IsTerminal:
			// Reached a terminal node. Ask the GRAPH what reaching it means: the
			// WG-022 reserved pair is normative, any other terminal declares its
			// own terminal_disposition, and an undeclared terminal is reported as
			// unclassifiable rather than merged on a guess about its name.
			// Inspecting inbound-edge topology is forbidden by WG-021.
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
			// Cascade structural failure (no matching edge, WG-012) or traversal
			// cap hit (EM-043). Both terminate the run here by reopening the bead
			// (needs-attention) — SelectNextEdge returns Failed on cap-hit rather
			// than dropping the capped edge and re-selecting an unconditional
			// fallback, so cap-hit does NOT reach a terminal node; it ends as a
			// reopen, same as a genuine no-match structural failure.
			//
			// F42 (hk-1vlz) AUTO-SALVAGE: when the traversal cap fires at the
			// commit_gate node AND the implementer already committed (HEAD advanced
			// past parentSHA), the committed work must NOT be silently discarded.
			// Return success so the caller (workloop.go) merges the committed run
			// branch to main, mirroring the verdict-absent salvage (hk-bqf1q) and
			// the approved-and-done path (hk-8ps7q).
			//
			// This covers the live failure class: implementer commits N times, gate
			// keeps failing, cap fires — the most-recent commit is salvaged rather
			// than stranded on the run branch (hk-3js5m).
			// hk-a8xjg: only salvage when the graph has NO reviewer node. When
			// a reviewer node exists the cap-hit is a triage outcome (the graph
			// defines a review stage that was never visited), NOT an approval —
			// fall through to the needs-attention reopen path below.
			if decision.CompletionReason == "cap_hit" && currentNodeID == "commit_gate" && !graphHasReviewerNode(nodesByID) {
				if salvageHead, salvageErr := resolveDotWorktreeHEAD(ctx, runner, wtPath); salvageErr == nil &&
					salvageHead != "" && salvageHead != parentSHA {
					return dotWorkflowResult{
						success: true,
						summary: fmt.Sprintf("dot: commit_gate cap-hit salvaged — committed tip present; auto-advancing to merge (hk-1vlz F42)"),
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
			// Increment the per-edge cycle counter so the traversal_cap is
			// enforced on subsequent traversals of this edge (EM-043a). Only
			// capped edges are tracked; uncapped edges Increment is harmless but
			// we restrict to capped edges to bound the counter map.
			incrementCapIfBounded(graph, cycles, runID, currentNodeID, decision.NextNodeID)
			// hk-wixms: record the node we just finished as the predecessor of the
			// next node, so an implementer re-entry can tell whether its inbound edge
			// came from a reviewer (deliver verdict) or a commit_gate (deliver nudge).
			prevNodeID = currentNodeID
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
	env runloop.RunEnv,
	ports runloop.RunPorts,
	handles runloop.SharedHandles,
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
	// hk-538l: workerBinaryPath resolves the node's SessionStart hook command to the
	// WORKER's harmonik path; workerSessionName/Cwd identify the tmux session to
	// ensure + spawn into ON THE WORKER. All empty for a LOCAL run ⇒ box-A path (NFR7).
	workerBinaryPath string,
	workerSessionName string,
	workerSessionCwd string,
	// hk-x882o: isTerminalSpawn marks the consolidate/join node as terminal so the
	// substrate allocates the reserved +1 slot, preventing starvation when all
	// non-terminal slots are occupied.
	isTerminalSpawn bool,
) (core.Outcome, error) {
	// RSM-010: the run's EmitterPort, bound once for this call. Deliberately the
	// NARROW emitterPort accessor (runports.go) rather than the runPorts() bundle,
	// which would assemble every port for one read (RT18: the clock default it
	// once guarded now folds inside runPorts() via clockOrSystem).
	emit := ports.Emitter
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
		// On a remote run wtPath is the WORKER's path, so route both the stale-
		// verdict removal and the review-target write through the runner; a box-A
		// os.Remove / WriteReviewTarget would no-op / orphan on box A and the worker
		// reviewer would never see its brief (produces no verdict). runner == nil for
		// a local run, restoring the byte-identical box-A path (NFR7).
		_ = workspace.RemoveReviewVerdictVia(ctx, runner, wtPath)
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
	//
	// hk-lfrub (codename:pi-model-leak): the DOT per-node model= attribute names a
	// model for the node's HARNESS, so it is HARNESS-FAMILY SCOPED. A claude model
	// name (e.g. claude-sonnet-4-6, which every node of the sonnet-triple-review
	// workflow.dot pins) is meaningless to a non-claude harness — the pi/codex
	// provider serves a different model set. Apply the model= pin ONLY when this
	// node's effective harness is the claude-code family; for a pi/codex effective
	// harness leave rc.model = the run-level resolvedModel (empty for a pi run), so
	// effectiveModel() falls through to the pi config model (ornith) instead of
	// asking the DGX provider for a claude model and failing. effort= is
	// harness-agnostic and stays unconditional below. A legitimate future pi/codex
	// node model= pin is out of scope for this bead (minimal claude-scoped fix).
	//
	// Effective-harness precedence mirrors the specBuilder selection below
	// (reviewer override > node harness= pin > run-level resolved harness);
	// resolveHarnessAgentTypeQuiet is the same four-tier walk routedLaunchSpecBuilder
	// performs at launch, run quietly here (no duplicate harness_selected events).
	//
	// hk-pkxju: reviewerInheritedHarness is the DEFAULT/INHERITED-leg correction — a
	// reviewer never inherits a SessionIDCaptured harness. Computed ONCE here (it logs)
	// and consumed by BOTH the model scoping immediately below and the specBuilder
	// selection further down, so the two stay in agreement as this comment promises.
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
		WorktreeRootPath:  workspace.WorktreeRootPath(env.ProjectDir, workspace.NoWorktreeRootOverride()),
		ExtraContext:      nodeExtraContext,
		BaseBranch:        baseBranch,
	}

	// Resolve the per-node spec builder. The pre-built launch-spec builder
	// captures tier-1 (bead labels) + tier-4 (global default). When the DOT node
	// carries a harness= attribute (T5, hk-u67of), rebuild with the node's harness
	// as nodeDefault (tier-3) so the four-tier precedence is fully honored (T12).
	//
	// T14 hk-iv748: for reviewer nodes, prefer reviewerHarnessOverride (the
	// implementer node's reviewer_harness= attr) over the reviewer node's own
	// harness= attr. This implements the OPTIONAL OVERRIDE precedence:
	//   1. reviewerHarnessOverride (implementer's reviewer_harness= attr) — if valid
	//   2. node.Harness (reviewer node's own harness= attr) — if valid
	//   3. the pre-built launch-spec builder (DEFAULT: same resolved harness as the implementer)
	specBuilder := ports.LaunchBuilder
	var effectiveNodeHarness core.AgentType
	if isReviewer && reviewerHarnessOverride.Valid() {
		// Override: implementer declared a specific reviewer harness.
		effectiveNodeHarness = reviewerHarnessOverride
	} else {
		// Default or non-reviewer: use the node's own harness= attr.
		effectiveNodeHarness = core.AgentType(node.Harness)
	}
	// hk-pkxju: leg 3 (DEFAULT/INHERITED) only — swap a SessionIDCaptured inherited
	// harness for claude. Computed above so the model scoping and this selection agree.
	if !effectiveNodeHarness.Valid() && reviewerInheritedHarness.Valid() {
		effectiveNodeHarness = reviewerInheritedHarness
	}
	if effectiveNodeHarness.Valid() && handles.HarnessRegistry != nil {
		// hk-2jxqg: use pinnedHarnessLaunchSpecBuilder so the node-level pin wins
		// unconditionally. routedLaunchSpecBuilder calls resolveHarness which lets a
		// tier-1 bead label (e.g. harness:codex) override the pin, silently routing
		// the reviewer to the wrong harness and producing no verdict.
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

	// Attach the optional substrate (nil by default and in the deterministic E2E test).
	// remote-substrate: thread the run's runner (SSHRunner for remote, nil for
	// local) so the per-run substrate's liveness + worktree probes target the
	// WORKER, and the implementer/reviewer spawns on the worker (mirrors the
	// single-mode path, workloop.go ~2733). nil preserves local behaviour (NFR7).
	// hk-qxvc2: a claude (SessionIDMinted) reviewer must run on the tmux/claude
	// substrate, not the codexdriver app-server substrate (handles.Substrate under
	// HARMONIK_SUBSTRATE=codexdriver is protocol-locked to codex JSON-RPC; a claude
	// reviewer handed to it never emits agent_ready). A SessionIDCaptured (codex)
	// reviewer is out of scope — spec.Substrate is nil'd below regardless.
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
	preHeadSHA, _ := resolveDotWorktreeHEAD(ctx, runner, wtPath)

	// hk-c73fs: emit reviewer_launched (§8.1a.2) for reviewer nodes before
	// launch, matching the builtin review-loop path. After the 06-08 DOT-default
	// deploy, all reviews ran via this function but reviewer_launched was never
	// emitted, making verdict latency unmeasurable. Mint the session ID here so
	// it can be reused in the post-run emitDotReviewerVerdict call below; the
	// EMIT rides the launch's OnBeforeLaunch hook so it stays the last event
	// before the spawn, after the CHB-018 pre-exec messages. That ordering is
	// load-bearing: the stale watcher gives a reviewer node a longer launch floor
	// only while lastEventType is reviewer_launched, so letting the pre-exec
	// messages land after it would silently drop a spawn-cap-blocked reviewer
	// back to the default window.
	var reviewerSessionID core.SessionID
	if isReviewer {
		reviewerSessionID = handlercontract.NewSessionID()
	}
	emitReviewerLaunched := func(lctx context.Context) {
		if isReviewer {
			emitDotReviewerLaunched(lctx, emit, runID, reviewerSessionID, *claudeSessionID, iterationCount)
		}
	}

	// dotDeliver is the post-ready brief delivery: paste-inject + quit-on-commit
	// / quit-on-review-file. These are no-ops when the substrate does not
	// implement the relevant interfaces (exec path / the deterministic E2E
	// /bin/sh handler), matching single-mode behavior.
	//
	// It runs on the machine's post-ready deliver edge (hk-3qjwl):
	// pasteInjectOnLaunch sends the kick-off message and the submitting Enter via
	// SendEnterToLastPane (hk-8cq23); firing it before the REPL is input-ready
	// leaves the prompt unsubmitted. For a ProcessExit harness (readiness
	// handshake skipped) runAgentLaunch invokes it directly once the segment
	// settles into Working, preserving the pre-RT8 fall-through ("paste-inject is
	// a no-op for codex").
	dotDeliver := func(dctx context.Context, dc agentDeliverCtx) {
		briefDelivered := pasteInjectOnLaunch(dctx, ports.Clock, dc.PasteTarget, artifacts.ClaudeSessionID,
			phase, iterationCount, wtPath, emit, runID)
		qs, ok := dc.PasteTarget.(quitSender)
		if !ok {
			return
		}
		if isReviewer {
			// hk-7rgqs: pass the pasteInjecter + claude session id so the watchdog
			// can re-seed the reviewer brief once if the original submit Enter was
			// swallowed by a slow splash (a non-pasteInjecter target yields a nil
			// inj inside the watchdog → re-seed disabled).
			revInj, _ := dc.PasteTarget.(pasteInjecter) //nolint:errcheck // nil revInj disables re-seed by design (pre-RT8 idiom)
			// hk-60t8: a per-node reviewer hard-ceiling override from the DOT
			// timeout= attribute (integer seconds) lets opus/high reviewer nodes
			// declare a longer budget in the workflow graph.
			var reviewerCeiling time.Duration
			if node.Timeout != "" {
				if n, err := strconv.Atoi(node.Timeout); err == nil && n > 0 {
					reviewerCeiling = time.Duration(n) * time.Second
				}
			}
			// hk-60t8 / hk-37giq: the watchdog takes its OWN subscription so it can
			// track agent_heartbeat for the active-reasoning extension. Sharing one
			// channel with the ready pump lets the ready-side drain goroutine steal
			// every heartbeat under concurrent dispatch.
			reviewerHBCh := dc.Tap.Subscribe()
			go pasteInjectQuitOnReviewFile(ctx, ports.Clock, qs, dc.Session, revInj, artifacts.ClaudeSessionID, wtPath, briefDelivered, reviewerHBCh, reviewerCeiling)
			return
		}
		if dc.ProcessExit {
			// hk-o90sl (T13/C5): gate on Completion() policy (specs/harness-contract.md
			// §2 N5). ProcessExit harnesses (codex) self-terminate when the turn
			// completes; sess.Wait + commitHardCeiling detect completion without a
			// /quit injection.
			return
		}
		watchdogCh := dc.Tap.Subscribe()
		go pasteInjectQuitOnCommit(ctx, ports.Clock, qs, dc.Session, wtPath, preHeadSHA, nil, briefDelivered, watchdogCh, emit, runID)
	}

	// The launch itself is the ONE path in agentlaunch.go. This site keeps only
	// what to launch (above) and what the exit means (below).
	launch := runAgentLaunch(ctx, agentLaunchInput{
		Env:     env,
		Ports:   ports,
		Handles: handles,
		RunID:   runID,
		LogPrefix: fmt.Sprintf("daemon: dot: bead %s node %q run %s",
			beadID, node.ID, runID.String()),
		Spec:              spec,
		Artifacts:         artifacts,
		WorktreePath:      wtPath,
		DaemonSocket:      daemonSocket,
		Runner:            runner,
		Remote:            runner != nil,
		BaseSubstrate:     baseSubstrate,
		WorkerSessionName: workerSessionName,
		WorkerSessionCwd:  workerSessionCwd,
		// PRESERVED DIVERGENCE: the DOT cascade has only ever srt-wrapped the
		// captured-session-id (exec) branch. Widening it to the substrate branch
		// would start sandboxing graph nodes that have never been sandboxed.
		SandboxScope: sandboxScopeCapturedOnly,
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
		Deliver:         dotDeliver,
	})
	// The heartbeat must keep beating through everything below — the auto_status
	// `go build` in particular — or the stale watcher's dead-process reap cancels
	// the run mid-inspection.
	defer launch.Cleanup()

	switch launch.Fail {
	case agentLaunchPrelaunchFailed:
		return core.Outcome{}, fmt.Errorf("node %q: %w", node.ID, launch.FailErr)
	case agentLaunchErrored:
		return core.Outcome{}, fmt.Errorf("launch node %q: %w", node.ID, launch.FailErr)
	case agentLaunchReadyTimeout:
		return core.Outcome{}, fmt.Errorf("node %q agent_ready_timeout", node.ID)
	case agentLaunchOK:
		// Fall through: the session has exited and been torn down.
	}

	// Emit implementer_phase_complete (hk-cd8yu / hk-mvjs4) immediately after the
	// implementer session ends, mirroring the single-mode path. Skipped for
	// reviewer-class nodes (they produce reviewer_verdict instead).
	//
	// hk-368i4: nodePhaseDur is captured ONCE and reused by the no-work detector
	// further down, so the event's duration_seconds and the detector's verdict
	// come from the same measurement.
	nodePhaseDur := ports.Clock.Since(launch.LaunchedAt)
	if !isReviewer {
		curHead, _ := resolveDotWorktreeHEAD(ctx, runner, wtPath)
		commitLanded := curHead != "" && curHead != preHeadSHA
		runlaunch.EmitImplementerPhaseComplete(ctx, emit, runID, launch.Exit.ExitCode,
			launch.Exit.StderrTail, commitLanded, nodePhaseDur)
	}

	if ctx.Err() != nil {
		return core.Outcome{}, fmt.Errorf("context cancelled during node %q", node.ID)
	}

	// Capture the claude_session_id for implementer-resume back-edges.
	if !isReviewer && *claudeSessionID == "" {
		*claudeSessionID = artifacts.ClaudeSessionID
	}

	if isReviewer {
		// Read the produced verdict; its value becomes the preferred_label that
		// drives the reviewer cascade (APPROVE / REQUEST_CHANGES / BLOCK).
		// hk-f3u6o: route the verdict + budget-sentinel reads through the run's
		// runner. For a REMOTE run (runner == SSHRunner) the reviewer writes
		// review.json / the budget marker on the WORKER, so a box-A os.ReadFile
		// never finds it → the run false-failed as "verdict absent". The …Via
		// variants cat the file over the transport; nil/local runner → byte-identical
		// bare-local read (NFR7). runner is the same value already threaded to the
		// node launch (e.g. resolveDotWorktreeHEAD above).
		//
		// hk-vv10r: this is a finalize read (runs once, after the reviewer node has
		// already exited) — not a poller — so it should retry-until-valid on a
		// transient ErrMalformed the same way reviewloop.go's finalize read does via
		// ReadReviewVerdictLocalRetry, on BOTH the local and remote branch.
		// ReadReviewVerdictVia alone only retries its remote branch; the local
		// branch falls through to the bare no-retry ReadReviewVerdict, so a local
		// DOT run false-failed on a review.json observed mid-flush.
		verdict, verdictErr := readDotReviewVerdictRetry(ctx, runner, wtPath)
		if verdictErr != nil {
			return core.Outcome{}, fmt.Errorf("read reviewer verdict for node %q: %w", node.ID, verdictErr)
		}
		if verdict == nil {
			// hk-da3rr: distinguish a BUDGET kill from a true no-verdict, mirroring
			// the builtin review-loop path (reviewloop.go). The marker file is written
			// into the reviewer's worktree by writeReviewerBudgetSentinel.
			sentinel, sentinelErr := readDotReviewerBudgetSentinel(ctx, runner, wtPath, node.ID)
			if sentinelErr != nil {
				return core.Outcome{}, sentinelErr
			}
			if sentinel != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: dot: reviewer node %q budget exceeded (reason=%s budget_ms=%d elapsed_ms=%d changed_lines=%d)\n",
					node.ID, sentinel.Reason, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines)
				emitReviewerBudgetExceeded(ctx, emit, runID, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines, sentinel.Reason)
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

	// codex --sandbox workspace-write cannot commit inside a worktree (.git points
	// outside the sandbox root → self-commit fails 100%). After the process exits,
	// the daemon stages+commits any changes codex produced via codex.EnsureRefsTrailer
	// (internal/harness/codex/commit.go, hk-gd9r). Mirrors workloop.go:4007-4019. Must run before
	// resolveDotWorktreeHEAD so the no-commit guard below sees any commit we create.
	// runAgentLaunch already resolved the harness; reuse its answer rather than
	// re-walking the registry for the same question.
	if launch.Harness != nil && launch.Harness.Completion() == handlercontract.CompletionProcessExit {
		codexOutcome, ensureErr := codex.EnsureRefsTrailer(ctx, runner, wtPath, preHeadSHA, beadID)
		if ensureErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: dot: ensureCodexRefsTrailer bead %s: %v (falling through to no-commit guard)\n",
				beadID, ensureErr)
		} else {
			fmt.Fprintf(os.Stderr, "daemon: dot: ensureCodexRefsTrailer bead %s: %s\n",
				beadID, codexOutcome)
			// hk-368i4: same detector as the workloop path — a no-change
			// outcome from a node that finished in seconds is a no-work run.
			// Diagnostic only; the no-commit guard below still decides.
			if codex.NoWorkSuspected(codexOutcome, nodePhaseDur, env.CodexNoWorkDurationFloor) {
				floor := codex.NoWorkFloor(env.CodexNoWorkDurationFloor)
				fmt.Fprintf(os.Stderr,
					"daemon: dot: bead %s node %q: implementer produced NO commit and a clean worktree after only %v (floor %v) — suspected no-work run (hk-368i4)\n",
					beadID, node.ID, nodePhaseDur, floor)
				codex.EmitImplementerNoWorkSuspected(ctx, emit, runID, beadID, nodePhaseDur, floor)
			}
		}
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
		if shared.MainHistoryHasRefsTrailer(ctx, env.ProjectDir, beadID) {
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
