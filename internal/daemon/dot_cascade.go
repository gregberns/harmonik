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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

	// advisoryRC is true when the cascade completed via the advisory-RC exemption
	// (hk-w2ow): REQUEST_CHANGES was advisory-only, committed work exists, gate
	// green, HEAD final. The caller uses this to reconcile-close on
	// rebase_dropped_commits instead of re-queuing — preventing the infinite
	// re-dispatch loop where work already merged in a prior run is re-identified
	// as advisory-RC and re-queued (hk-whru3).
	advisoryRC bool

	// approveVerdict carries the APPROVE verdict when the cascade succeeded via
	// the explicit reviewer-APPROVE path (hk-8ps7q). Nil when success was via a
	// non-reviewer terminal node, advisory-RC advisory-only, or cap-hit salvage.
	// The caller (workloop.go) uses this to stamp Reviewed-By / Review-Verdict
	// trailers on the HEAD commit before merging, mirroring the review-loop path
	// (hk-tnui).
	approveVerdict *workspace.ReviewVerdict
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
	// hk-538l: for a REMOTE run rewrite the hook socket to the worker-side reverse-
	// tunnel TCP endpoint so the worker's claude can reach the relay; box A's local
	// unix daemon.sock is unreachable from the worker. Empty workerHookSock (LOCAL
	// run) ⇒ unchanged box-A unix socket (NFR7). Mirrors workloop.go single-mode
	// resolveAgentDaemonSocket; previously the box-A unix path flowed into every
	// node's rc.daemonSocket → HARMONIK_DAEMON_SOCKET → connect failure → no hook →
	// agent_ready_timeout.
	boxADaemonSocket := filepath.Join(deps.projectDir, ".harmonik", "daemon.sock")
	daemonSocket := resolveAgentDaemonSocket(workerHookSock, boxADaemonSocket)

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
		emitNodeDispatchRequested(ctx, deps.bus, runID, core.NodeID(currentNodeID))

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
				gateEnv := deps.handlerEnv
				if parentSHA != "" {
					gateEnv = append(append(make([]string, 0, len(deps.handlerEnv)+1), deps.handlerEnv...), "HK_GATE_BASE_SHA="+parentSHA)
				}
				toolOutcome, toolErr := dispatchDotToolNode(ctx, deps.bus, runID, runner, wtPath, node, gateEnv)
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
			currentHash, hashErr := rlComputeDiffHashVia(ctx, runner, wtPath, parentSHA)
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
							priorSummary = rlTruncateUTF8(priorVerdictNotes, priorVerdictSummaryMaxBytes)
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
								priorSummary = rlTruncateUTF8(gateFailMsg, priorVerdictSummaryMaxBytes)
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
								priorSummary = rlTruncateUTF8(commitNudge, priorVerdictSummaryMaxBytes)
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
					emitDotImplementerResumed(ctx, deps.bus, runID, claudeSessionID, iterationCount, priorSummary)
				}
			}
			// hk-x882o: mark the consolidate (verdict-join) node as a terminal
			// spawn so the substrate allocates the reserved +1 slot for it,
			// preventing starvation when all non-terminal slots are occupied.
			// The check is graph-structural and pure — safe to evaluate before
			// dispatch. The result is also used post-dispatch (line 883), so
			// computing it here avoids a second call.
			_, isConsolidate := isConsolidateJoinNode(graph, nodesByID, currentNodeID)
			nodeOutcome, nodeErr := dispatchDotAgenticNode(ctx, deps, runID, beadID, beadRecord,
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
				ctx, deps, runID, run, wtPath, daemonSocket, node,
				iterationCount, resolvedModel, resolvedEffort,
				beadID, beadTitle, beadDescription, extraContext, baseBranch, runner,
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
				deps, runID, beadID, beadRecord, beadTitle, beadDescription,
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

// nodeModelForHarness applies a DOT per-node model= attribute with harness-family
// scoping (hk-lfrub, codename:pi-model-leak). A node model= pin names a model for
// the node's HARNESS; a claude model name (e.g. claude-sonnet-4-6) is meaningless
// to a non-claude harness whose provider serves a different model set. The pin is
// therefore honored ONLY when the node's effective harness is the claude-code
// family; otherwise the run-level resolvedModel is returned unchanged (empty for a
// pi run → effectiveModel() falls through to the pi config model, ornith). effort=
// is harness-agnostic and is handled by the caller, not here.
func nodeModelForHarness(resolvedModel, nodeModelAttr string, effHarness core.AgentType) string {
	if nodeModelAttr != "" && effHarness == core.AgentTypeClaudeCode {
		return nodeModelAttr
	}
	return resolvedModel
}

// readDotReviewVerdictRetry reads a reviewer's .harmonik/review.json with
// retry-until-valid-on-ErrMalformed semantics regardless of whether runner is
// local or remote (hk-vv10r).
//
// ReadReviewVerdictVia only retries on its REMOTE (SSH cat) branch; its
// local/nil-runner branch intentionally falls through to the bare, no-retry
// ReadReviewVerdict (NFR7 — pollers like the quit-watchdog gate need a fast
// absent/malformed return). The DOT cascade's finalize verdict reads are NOT
// pollers: they run once, after the reviewer node has already exited, exactly
// like reviewloop.go's finalize read (which uses ReadReviewVerdictLocalRetry).
// Without this, a local DOT run that observes review.json mid-flush gets a
// single no-retry read and false-fails the whole run on a transient
// ErrMalformed — the review-loop fix (hk-1hgjr) never applied to the DOT path.
func readDotReviewVerdictRetry(ctx context.Context, runner tmux.CommandRunner, wtPath string) (*workspace.ReviewVerdict, error) {
	if runnerIsLocalFS(runner) {
		return workspace.ReadReviewVerdictLocalRetry(ctx, wtPath)
	}
	return workspace.ReadReviewVerdictVia(ctx, runner, wtPath)
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
	nodeModelHarness := core.AgentType(node.Harness)
	if isReviewer && reviewerHarnessOverride.Valid() {
		nodeModelHarness = reviewerHarnessOverride
	}
	if !nodeModelHarness.Valid() {
		nodeModelHarness = resolveHarnessAgentTypeQuiet(
			beadRecord,
			core.AgentType(""), // queue default (hk-4x3rg not landed)
			core.AgentType(""), // node default (already folded into node.Harness above)
			deps.defaultHarness,
		)
	}
	nodeModel := nodeModelForHarness(resolvedModel, node.Model, nodeModelHarness)
	nodeEffort := resolvedEffort
	if node.Effort != "" {
		nodeEffort = node.Effort
	}

	rc := claudeRunCtx{
		runID:         runID,
		beadID:        string(beadID),
		workspacePath: wtPath,
		// runner threads the per-run CommandRunner into buildClaudeLaunchSpec so the
		// worktree-trust / settings / agent-task writes land on the WORKER for a
		// REMOTE DOT run (runner == dotRunner == rbc.sshRunner) and stay box-A-local
		// for a LOCAL run (runner == nil, NFR7). Without this the trust upsert ran
		// box-A-local → worker worktree untrusted → trust modal → no_commit
		// (hk-3sus; symmetric with how settings/agent-task get the worker).
		runner: runner,
		// hk-538l: workerBinaryPath resolves the SessionStart hook command to the
		// WORKER's harmonik path for a REMOTE DOT run; empty for LOCAL falls back box-A-
		// local in claudelaunchspec. Without this the worker's settings.json pointed at
		// box-A's daemonBinaryPath → hook never exec'd → agent_ready_timeout (hk-538l).
		workerBinaryPath:  workerBinaryPath,
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
		// hk-2jxqg: use pinnedHarnessLaunchSpecBuilder so the node-level pin wins
		// unconditionally. routedLaunchSpecBuilder calls resolveHarness which lets a
		// tier-1 bead label (e.g. harness:codex) override the pin, silently routing
		// the reviewer to the wrong harness and producing no verdict.
		specBuilder = pinnedHarnessLaunchSpecBuilder(
			deps.harnessRegistry,
			beadRecord,
			effectiveNodeHarness,
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
		// hk-538l: for a REMOTE run tell the per-run substrate which tmux session to
		// ENSURE + spawn into ON THE WORKER and the cwd to create it with (the worker's
		// repo_path). Without this the worker spawn falls back to the box-A project-hash
		// session name with an empty cwd. runner != nil gates the remote case symmetric
		// with workloop.go single-mode.
		if runner != nil && workerSessionName != "" {
			prs.workerSessionName = workerSessionName
			prs.workerSessionCwd = workerSessionCwd
		}
		substrate = prs
		pasteTarget = prs
	}
	spec.Substrate = substrate

	// PI-014 DOT analog: predeclare sess so agentEndCb (inside the
	// SessionIDCaptured block below) can capture it by reference. Go's `:=`
	// redeclaration at Launch assigns to this same variable since watcher and
	// launchErr are new in that scope; the closure is safe because agent_end
	// can only arrive after Launch returns and sets sess.
	var sess handler.Session

	// hk-z4nif: SessionIDCaptured harnesses (Pi, Codex) deliver their task via
	// argv, not via tmux pane paste. Attempting paste injection yields "seed
	// marker absent" (the terminal shows NDJSON, not the seed text) →
	// pasteinject_failed → run_failed when no review.json appears. Nil out
	// pasteTarget so pasteInjectOnLaunch is a no-op for these harnesses.
	// hk-ybuts: also port the single-mode exec-path wiring (workloop.go:4237-4308):
	// force spec.Substrate=nil so the handler uses exec (not tmux SpawnWindow) to
	// wire a real stdout pipe; apply srt argv-wrap; capture pi-stdout.log; set
	// StdoutWrapper for session-id capture + PI-014 agent_end teardown.
	if deps.harnessRegistry != nil {
		if h, hErr := deps.harnessRegistry.ForAgent(artifactAgentType(artifacts)); hErr == nil {
			if h.SessionIDPolicy() == handlercontract.SessionIDCaptured {
				pasteTarget = nil
				spec.Substrate = nil
				sandboxSpawn := sandboxSpawnForRun(deps.sandboxCfg, resolveGateAgentType(h, artifactAgentType(artifacts)), SandboxProfileInput{
					WorktreePath:   wtPath,
					GitDir:         filepath.Join(deps.projectDir, ".git"),
					RunID:          runID.String(),
					DaemonSockPath: daemonSocket,
					AllowedDomains: deps.sandboxCfg.Network.AllowedDomains,
					// hk-ybuts/hk-u69my: the DOT cascade is the LIVE canary's launch path — it MUST
					// mirror single-mode's egress wiring (workloop.go), else a config-permitted local
					// binding never reaches the sandboxed Pi and the model connect is Seatbelt-denied.
					AllowLocalBinding:      deps.sandboxCfg.Network.AllowLocalBinding,
					WeakerNetworkIsolation: deps.sandboxCfg.Network.WeakerNetworkIsolation,
					TmpDirs:                sandboxOSTmpDirs(),
					SharedReadCacheDirs:    deps.sandboxCfg.Cache.WarmRead,
					PrivateWriteCacheDirs:  deps.sandboxCfg.Cache.PrivateWrite,
				})
				// hk-5wdon: prove the srt sandbox actually engages under this
				// profile before trusting it to isolate the run. Mirrors the
				// single-mode exec-path check in workloop.go — srt's own exit
				// code alone is not sufficient evidence (hk-tch4t).
				if sandboxSpawn != nil {
					canaryPath := srtEngagementCanaryPath(deps.projectDir, runID.String())
					if engageErr := verifySandboxEngaged(ctx, sandboxSpawn, canaryPath, func(format string, args ...any) {
						fmt.Fprintf(os.Stderr, "daemon: dot: bead %s run %s: "+format+"\n",
							append([]any{beadID, runID.String()}, args...)...)
					}); engageErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: dot: srt sandbox engagement verification bead %s run %s: %v\n",
							beadID, runID.String(), engageErr)
						return core.Outcome{}, fmt.Errorf("srt sandbox engagement verification failed: %w", engageErr)
					}
				}
				wrapBin, wrapArgs, wrapErr := sandboxWrapExecArgv(sandboxSpawn, spec.Binary, spec.Args)
				if wrapErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: dot: srt argv-wrap bead %s run %s: %v (reopening)\n",
						beadID, runID.String(), wrapErr)
					return core.Outcome{}, fmt.Errorf("srt argv-wrap error: %w", wrapErr)
				}
				spec.Binary = wrapBin
				spec.Args = wrapArgs
				var piStdoutFile *os.File
				if artifactAgentType(artifacts) == core.AgentTypePi {
					piCaptureDir := filepath.Join(wtPath, ".harmonik", "pi-agent")
					if mkErr := os.MkdirAll(piCaptureDir, 0o755); mkErr != nil {
						fmt.Fprintf(os.Stderr, "daemon: dot: hk-j6wm7: create pi capture dir %q: %v (stdout capture disabled)\n", piCaptureDir, mkErr)
					} else if f, ferr := os.Create(filepath.Join(piCaptureDir, "pi-stdout.log")); ferr != nil {
						fmt.Fprintf(os.Stderr, "daemon: dot: hk-j6wm7: create pi-stdout.log: %v (stdout capture disabled)\n", ferr)
					} else {
						piStdoutFile = f
						defer func() { _ = piStdoutFile.Close() }()
					}
				}
				capturedH := h
				capturedSessionIDCh := make(chan string, 1) // buffered; DOT cascade has no resume reader
				agentEndCb := func() {
					if sess != nil {
						_ = sess.Kill(context.Background())
					}
				}
				spec.StdoutWrapper = func(r io.Reader) io.Reader {
					src := r
					if piStdoutFile != nil {
						src = io.TeeReader(r, piStdoutFile)
					}
					return capturedH.NewSessionIDInterceptor(src, func(id string) {
						capturedSessionIDCh <- id
					}, agentEndCb)
				}
			}
		}
	}

	spec.Terminal = isTerminalSpawn // hk-x882o: terminal/consolidate nodes draw from the reserved +1 slot

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

	// hk-nvjk: start the CHB-019 heartbeat goroutine so the stale watcher
	// receives agent_heartbeat events (with run_id) after launch_initiated.
	// Without this, lastEventType stays frozen at "launch_initiated" for the
	// full run duration, causing false-positive run_stale on every DOT dispatch.
	// Mirrors the single-mode path (workloop.go Step 5).
	//
	// hk-sj6a: reviewers emit to deps.bus directly (NOT through tap). Routing
	// daemon heartbeats through tap fans them to reviewerHBCh (tap.Subscribe()),
	// which pasteInjectQuitOnReviewFile interprets as evidence the reviewer is
	// still reasoning — keeping recentHB=true indefinitely after the claude
	// process dies and preventing Kill until the hard ceiling (60 min).
	//
	// hk-e7n76: implementers emit through tap (parity with workloop.go:3721) so
	// watchdogCh (tap.Subscribe() in pasteInjectQuitOnCommit) receives heartbeats
	// and can extend totalDeadline. Emitting to deps.bus only bypasses tap entirely,
	// starving the implementer budget watchdog of progress signals. Tap is
	// per-node (reviewer XOR implementer) so this scoping is safe.
	hbTarget := handlercontract.EventEmitter(deps.bus)
	if !isReviewer {
		hbTarget = tap
	}
	nodeHBDone := make(chan struct{})
	go handler.RunHeartbeatLoop(ctx, artifacts.handlerSessionID,
		handler.HeartbeatInterval, nodeHBDone,
		newDaemonHeartbeatEmitter(hbTarget, runID))
	defer close(nodeHBDone)

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
		capturedRunID := runID // hk-wths: copy runID so EmitWithRunID stamps the bus envelope
		deps.hookStore.SetAgentReadyCallback(runID.String(), artifacts.claudeSessionID, func() {
			// hk-wths: use EmitWithRunID so the bus envelope carries run_id. Without
			// this, the stale watcher's observe() skips the event (evt.RunID == nil),
			// agentReadySeen stays false, and the never-spawned reaper fires after
			// neverSpawnedReaperDefaultTimeout (30 min) — cancelling the per-run
			// context mid-session on any DOT-mode run whose implement node takes longer
			// than 30 min (e.g. opus+max on complex beads).
			_ = capturedTap.EmitWithRunID(context.Background(), capturedRunID, core.EventTypeAgentReady, nil)
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
		// hk-f6g7: skip waitAgentReady for ProcessExit harnesses (codex). These
		// self-terminate on turn completion and never emit agent_ready; calling
		// waitAgentReady unconditionally caused HC-056 timeout in all workflow modes.
		// Mirrors the completionMode gate already applied to the /quit watchdog below
		// (dot_cascade.go:1213-1233). Spec: specs/harness-contract.md §2 N5.
		dotCompletionMode := handlercontract.CompletionEventStreamThenQuit
		if deps.harnessRegistry != nil {
			if h, hErr := deps.harnessRegistry.ForAgent(artifactAgentType(artifacts)); hErr == nil {
				dotCompletionMode = h.Completion()
			}
		}
		if dotCompletionMode != handlercontract.CompletionProcessExit {
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
			// hk-96d7w: runner != nil marks a REMOTE (SSH worker) run — longer window.
			readyTimeout := effectiveAgentReadyTimeout(deps.agentReadyTimeout, deps.remoteAgentReadyTimeout, runner != nil)
			readyErr := waitAgentReady(readyCtx, runID, eventSrc, adapter, readyTimeout)
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
		// CompletionProcessExit: process self-terminates; fall through directly to
		// paste-inject (which is a no-op for codex) and waitWithSocketGrace.
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
			// hk-60t8: parse per-node reviewer hard-ceiling override from the DOT
			// timeout= attribute (integer seconds).  A non-zero value overrides
			// reviewFileHardCeiling for this node only, allowing opus/high reviewer
			// nodes to declare a longer budget in the workflow graph.
			var reviewerCeiling time.Duration
			if node.Timeout != "" {
				if n, err := strconv.Atoi(node.Timeout); err == nil && n > 0 {
					reviewerCeiling = time.Duration(n) * time.Second
				}
			}
			// hk-60t8: give the reviewer watchdog its OWN independent subscription
			// so it can track agent_heartbeat events for the active-reasoning
			// extension — independent of the tapCh used by waitAgentReady.
			reviewerHBCh := tap.Subscribe()
			go pasteInjectQuitOnReviewFile(ctx, qs, sess, revInj, artifacts.claudeSessionID, wtPath, briefDelivered, reviewerHBCh, reviewerCeiling)
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
			if sentinel, sErr := ReadReviewerBudgetSentinelVia(ctx, runner, wtPath); sErr == nil && sentinel != nil {
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
			PreferredLabelFlags: flags,         // hk-m1wqp: carries reviewer flags to driveDotWorkflow for review_fixup_stalled
			Notes:               verdict.Notes, // hk-wixms: carry verdict notes so the next implementer-resume back-edge can deliver them via reviewer-feedback.iter-<N-1>.md
		}, nil
	}

	// codex --sandbox workspace-write cannot commit inside a worktree (.git points
	// outside the sandbox root → self-commit fails 100%). After the process exits,
	// the daemon stages+commits any changes codex produced via ensureCodexRefsTrailer
	// (codexcommit.go, hk-gd9r). Mirrors workloop.go:4007-4019. Must run before
	// resolveDotWorktreeHEAD so the no-commit guard below sees any commit we create.
	if deps.harnessRegistry != nil {
		if h, hErr := deps.harnessRegistry.ForAgent(artifactAgentType(artifacts)); hErr == nil &&
			h.Completion() == handlercontract.CompletionProcessExit {
			codexOutcome, ensureErr := ensureCodexRefsTrailer(ctx, runner, wtPath, preHeadSHA, beadID)
			if ensureErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: dot: ensureCodexRefsTrailer bead %s: %v (falling through to no-commit guard)\n",
					beadID, ensureErr)
			} else {
				fmt.Fprintf(os.Stderr, "daemon: dot: ensureCodexRefsTrailer bead %s: %s\n",
					beadID, codexOutcome)
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
			// Same process-group treatment as the sh -c gate (hk-me8ru): a ctx
			// timeout/cancel must reap the whole tree, not just the `go` PID, so
			// CombinedOutput below can't block on a lingering grandchild holding
			// the stdout pipe open. Lower risk here (the go toolchain reaps its
			// own children more reliably than sh -c fork) but same failure class.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error {
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return nil
			}
			cmd.WaitDelay = 5 * time.Second
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
// filesystem. Delegates to workspace.ReadAutoStatusMarkerVia which is the single
// source of truth for this logic (hk-hd2w6). NFR7: nil/local runner → local read,
// byte-identical to the pre-remote-substrate path.
func readAutoStatusMarkerVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (*workspace.AutoStatusMarker, error) {
	return workspace.ReadAutoStatusMarkerVia(ctx, runner, wtPath)
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
// build a LOGIN shell — `/bin/sh -lc 'export K=V; … cd <wtPath> && <ToolCommand>'`
// — so (a) the worker's own ~/.zprofile / ~/.profile PATH (which includes
// /opt/homebrew/bin, where go/git/claude resolve on the worker) is sourced, and
// (b) handler-supplied env vars (e.g. HARMONIK_PROJECT_HASH) are inlined as export
// statements before the gate, making them accessible to env-dependent tool_commands
// on the worker (hk-230h). The cd anchors the gate at the worker's worktree the way
// cmd.Dir does locally.
// NFR7: the LOCAL (runner == nil) branch is byte-identical to the prior code.
//
// hk-vjsv: while the gate command runs, a daemon heartbeat goroutine emits
// agent_heartbeat (run-scoped, via newDaemonHeartbeatEmitter) on HeartbeatInterval
// so the stale watcher keeps seeing events for this run. A non-agentic shell gate
// has no Claude handler session / NDJSON stream, so without this the run would
// appear silent for the full gate duration (default commit_gate ~build+vet+test+
// scenario, node timeout 900s, common to exceed the ~10-min stale window on a slow
// or cold-cache worker), and the watcher would false-fire run_stale and re-dispatch
// the gate without killing the prior shell. The goroutine ticks for BOTH the local
// and remote command paths and is stopped via close(hbDone) the moment the command
// returns, so it cannot leak. bus may be nil in unit tests that exercise the gate
// in isolation; the heartbeat is simply skipped in that case.
func dispatchDotToolNode(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, runner tmux.CommandRunner, wtPath string, node *dot.Node, env []string) (core.Outcome, error) {
	timeoutSecs := 300
	if node.Timeout != "" {
		if n, err := strconv.Atoi(node.Timeout); err == nil && n > 0 {
			timeoutSecs = n
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	// hk-vjsv: heartbeat the run for the lifetime of the gate command. Synthesize a
	// session ID (no handler session exists for a shell node). RunHeartbeatLoop emits
	// the first heartbeat immediately, then every HeartbeatInterval, until hbDone is
	// closed (deferred below) or execCtx is cancelled. Bind the loop to execCtx so it
	// also stops if the gate times out / the parent context is cancelled.
	if bus != nil {
		hbDone := make(chan struct{})
		defer close(hbDone)
		gateSessionID := string(handlercontract.NewSessionID())
		go handler.RunHeartbeatLoop(execCtx, gateSessionID,
			dotGateHeartbeatInterval, hbDone,
			newDaemonHeartbeatEmitter(bus, runID))
	}

	var cmd *exec.Cmd
	if runner == nil {
		// LOCAL run (NFR7: byte-identical to the pre-remote path).
		cmd = exec.CommandContext(execCtx, "/bin/sh", "-c", node.ToolCommand)
		cmd.Dir = wtPath
		// Inherit the daemon's process env (PATH, HOME, GOPATH, …) then layer the
		// handler-supplied entries last so they override on duplicate keys.
		cmd.Env = append(os.Environ(), env...)
		// Run the gate shell in its own process group so a per-node timeout/cancel
		// reaps the ENTIRE tree, not just the /bin/sh parent. On Linux, `sh -c "…"`
		// may fork its command (dash running e.g. `sleep 60`); CommandContext's
		// default kill SIGKILLs only the sh PID, leaving that child alive holding
		// the stdout pipe open — so cmd.CombinedOutput() below blocks until the
		// child exits on its own (observed on CI: a 1s-timeout gate returning after
		// the full 60s sleep, not ~1s). Setpgid + a Cancel that signals the negative
		// PGID kills the whole group; WaitDelay guarantees CombinedOutput unblocks
		// even if a grandchild lingers on the pipe. (hk-me8ru)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				// Negative PID → deliver to the whole process group.
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		cmd.WaitDelay = 5 * time.Second
	} else {
		// REMOTE run: route the gate through the worker's runner. cd into the
		// worker's worktree and run under a login shell so the worker's PATH
		// (homebrew toolchain) is sourced — SSHRunner does NOT forward cmd.Env.
		// Inline handler-supplied env vars (e.g. HARMONIK_PROJECT_HASH) as
		// shell export statements prepended to the gate command so env-dependent
		// tool_commands can reference them on the worker (hk-230h).
		var sb strings.Builder
		for _, kv := range env {
			if idx := strings.IndexByte(kv, '='); idx >= 0 {
				sb.WriteString("export ")
				sb.WriteString(kv[:idx])
				sb.WriteByte('=')
				sb.WriteString(shellQuote(kv[idx+1:]))
				sb.WriteString("; ")
			}
		}
		sb.WriteString("cd ")
		sb.WriteString(shellQuote(wtPath))
		sb.WriteString(" && ")
		sb.WriteString(node.ToolCommand)
		cmd = runner.Command(execCtx, "/bin/sh", "-lc", sb.String())
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
	//
	// hk-vjsv Fix (B) — gate-shell teardown on run re-dispatch / teardown:
	//   LOCAL (runner == nil): the gate runs under exec.CommandContext(execCtx, …);
	//   when the per-run context is cancelled (the run is torn down or re-dispatched
	//   after run_stale), Go kills the local shell process automatically, so no
	//   local gate shell leaks. Confirmed by the canceled-path test
	//   (dot_cascade_tool_hkcucz6_test.go).
	//
	//   REMOTE (runner != nil, SSHRunner): exec.CommandContext kills only the LOCAL
	//   `ssh` client on ctx-cancel; the remote `/bin/sh -lc '… go build/test …'`
	//   process tree on the WORKER is orphaned (classic SSH no-PTY orphan), which is
	//   the pile-up of concurrent full build+scenario runs the bug observed. Fix (A)
	//   removes the ROOT trigger (run_stale no longer false-fires for a healthy
	//   gate, so the run is not re-dispatched out from under a live gate), so this
	//   residual only bites on a GENUINE teardown of a still-running remote gate.
	//   TODO(hk-vjsv): make the remote gate killable — allocate a PTY (`ssh -tt`) so
	//   the worker shell dies with the client, or run the gate inside a wrapper that
	//   records its remote PID/PGID and have SSHRunner-aware teardown `ssh <host>
	//   kill -TERM -<pgid>` on ctx-cancel. Threading this needs the same
	//   runner/worker-session plumbing dispatchDotGateNode's cognition path is
	//   already missing (see TODO(hk-538l) in dot_gate.go); do it once for both.
	if ctx.Err() != nil {
		fc := core.FailureClassCanceled
		return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc, Notes: outputTail}, nil
	}

	// Infra-signature check: go build-cache TOCTOU failures emit a distinctive
	// "package X is not in std" message when concurrent go clean -cache deletes
	// stdlib cache entries mid-build. These are not code defects — retry on the
	// same tree succeeds — so classify as transient (self-loop) rather than
	// deterministic (fix-loop back to implement). (hk-7xgu4 / hk-1veco FIX2)
	if isGateBuildCacheInfraError(combined) {
		fc := core.FailureClassTransient
		fmt.Fprintf(os.Stderr, "daemon: dot tool node %q failed (%v) with build-cache infra signature (transient); gate log: %s\n", node.ID, err, gateLogPath)
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

// dotGateHeartbeatInterval is the cadence at which dispatchDotToolNode emits
// agent_heartbeat for the run while a non-agentic shell gate command executes
// (hk-vjsv). Defaults to handler.HeartbeatInterval (300s), matching the agentic
// DOT path. Declared as a var (not a const) so tests can shrink it to assert the
// PERIODIC tick fires for a gate that outlives one interval — without waiting the
// full 5 minutes. Production code never reassigns it.
var dotGateHeartbeatInterval = handler.HeartbeatInterval

// shellQuote wraps s in single quotes for safe interpolation into a remote
// `/bin/sh -lc '<script>'` string. It delegates to the canonical, single-source
// workflow.ShellQuote (WG-045 security primitive) so there is exactly one quoting
// implementation to audit: the same helper neutralises substituted tool_command
// param values at load time and worktree-path/env values on this remote gate path.
func shellQuote(s string) string {
	return workflow.ShellQuote(s)
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

// isGateBuildCacheInfraError reports whether gate output matches a known go
// build-cache / toolchain infrastructure failure signature. Infra failures are
// transient — retry on the same committed tree succeeds — and must not be
// misclassified as deterministic (which would bounce the bead back to the
// implementer with a false "fix the build" signal).
//
// Known signatures (hk-y3frr / hk-guez / hk-7xgu4 TOCTOU lineage):
//   - "is not in std" — concurrent go clean -cache deleted stdlib entries
//     while "go build ./..." was running; observed as "package bufio is not
//     in std". Passes immediately on retry after the cache is warm again.
func isGateBuildCacheInfraError(output []byte) bool {
	return strings.Contains(string(output), "is not in std")
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

// graphHasReviewerNode reports whether any node in the graph is a reviewer-class
// node. Used by the F42 cap-hit salvage gate (hk-a8xjg): when a reviewer exists
// the cap-hit is a triage outcome, not an approval, and must NOT auto-merge.
func graphHasReviewerNode(nodesByID map[string]*dot.Node) bool {
	for _, n := range nodesByID {
		if nodeIsReviewer(n) {
			return true
		}
	}
	return false
}

// verdictSeverity ranks a reviewer verdict on the BLOCK > REQUEST_CHANGES >
// APPROVE severity ladder. Higher is more severe. An unrecognized verdict ranks
// as REQUEST_CHANGES (1) — fail-toward-rejection, never silently APPROVE.
func verdictSeverity(verdict string) int {
	switch verdict {
	case workspace.ReviewVerdictApprove:
		return 0
	case workspace.ReviewVerdictRequestChanges:
		return 1
	case workspace.ReviewVerdictBlock:
		return 2
	default:
		// Unknown / empty: treat as REQUEST_CHANGES so a malformed value can
		// never approve work. (Empty never reaches here in practice — the map
		// only ever holds validated verdicts — but the guard is fail-closed.)
		return 1
	}
}

// verdictSeverityMax returns the most-severe verdict among the inputs on the
// BLOCK > REQUEST_CHANGES > APPROVE ladder. Returns "" for an empty input.
// This is the DETERMINISTIC severity-join the consolidate node's prose role=
// string describes but cannot itself enforce — a single over-lenient consolidate
// self-report can never approve work that any axis-reviewer rejected (hk-cmry).
func verdictSeverityMax(verdicts []string) string {
	best := ""
	bestRank := -1
	for _, v := range verdicts {
		if r := verdictSeverity(v); r > bestRank {
			bestRank = r
			best = v
		}
	}
	return best
}

// upstreamReviewerNodeIDs returns the set of reviewer-class node IDs that can
// reach targetID through the graph's forward edges (i.e. the reviewer nodes that
// run BEFORE targetID on the spine), excluding targetID itself. The
// keeper-redesign graph wires
// review_correctness → review_design → review_tests → consolidate, so all three
// axis reviewers are transitive predecessors of consolidate.
func upstreamReviewerNodeIDs(graph *dot.Graph, nodesByID map[string]*dot.Node, targetID string) map[string]bool {
	// Build backward adjacency (ToNodeID -> []FromNodeID).
	backward := make(map[string][]string, len(graph.Nodes))
	for _, e := range graph.Edges {
		backward[e.ToNodeID] = append(backward[e.ToNodeID], e.FromNodeID)
	}
	// BFS backward from targetID over all predecessors.
	reviewers := make(map[string]bool)
	seen := map[string]bool{targetID: true}
	queue := append([]string{}, backward[targetID]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if n := nodesByID[id]; n != nil && nodeIsReviewer(n) {
			reviewers[id] = true
		}
		queue = append(queue, backward[id]...)
	}
	return reviewers
}

// nodeRoutesOnPreferredLabel reports whether nodeID has at least one outgoing
// edge whose condition references outcome.preferred_label. This is the structural
// signature that distinguishes a verdict-JOIN node (e.g. consolidate, which
// branches close / implement / close-needs-attention on the consolidated verdict)
// from a per-axis reviewer (which routes UNCONDITIONALLY to the next reviewer).
func nodeRoutesOnPreferredLabel(graph *dot.Graph, nodeID string) bool {
	for _, e := range graph.Edges {
		if e.FromNodeID != nodeID || e.Condition == nil {
			continue
		}
		for _, c := range e.Condition.Clauses {
			if c.LHS == "outcome.preferred_label" {
				return true
			}
		}
	}
	return false
}

// isConsolidateJoinNode reports whether nodeID is a consolidate-style verdict
// JOIN node: a reviewer node that (a) ROUTES on outcome.preferred_label AND (b)
// has >= 2 upstream reviewer predecessors. Both conditions together uniquely
// select the consolidate node and never a per-axis reviewer — axis reviewers
// route unconditionally (fail condition a) even though, on a linear axis chain,
// later axis reviewers do have >= 2 reviewer predecessors. Name-free so it holds
// for any graph that follows the multi-reviewer-then-join shape (hk-cmry).
func isConsolidateJoinNode(graph *dot.Graph, nodesByID map[string]*dot.Node, nodeID string) (upstream map[string]bool, ok bool) {
	if !nodeRoutesOnPreferredLabel(graph, nodeID) {
		return nil, false
	}
	upstream = upstreamReviewerNodeIDs(graph, nodesByID, nodeID)
	return upstream, len(upstream) >= 2
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

// emitDotImplementerResumed emits implementer_resumed (§8.1a.1) before an
// implementer-resume back-edge dispatch (iterationCount >= 2), matching the
// builtin review-loop path (reviewloop.go emitImplementerResumed). WorkflowMode
// is WorkflowModeDot so consumers filtering on workflow_mode=dot see the resume
// event with prior_verdict_summary populated from the prior reviewer notes or
// the commit-nudge (hk-wixms).
func emitDotImplementerResumed(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	claudeSessionID string,
	iterationCount int,
	priorVerdictSummary string,
) {
	pl := core.ImplementerResumedPayload{
		RunID:               runID,
		WorkflowMode:        core.WorkflowModeDot,
		SessionID:           handlercontract.NewSessionID(),
		ClaudeSessionID:     claudeSessionID,
		IterationCount:      iterationCount,
		PriorVerdictSummary: priorVerdictSummary,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerResumed, b)
}
