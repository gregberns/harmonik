package daemon

// dot_cascade.go — DOT workflow-mode cascade driver (hk-9dnak).
//
// driveDotWorkflow walks an arbitrary validated DOT workflow graph node-by-node,
// dispatching each node according to its type and using the cascade engine
// (workflow.DecideNextNode) to resolve the next node after each outcome. It follows the
// graph's edges rather than a fixed implementer→reviewer cycle. It is the only
// execution engine; the driver this once generalized is deleted.
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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

// resolveDotWorktreeHEAD resolves the worktree HEAD for a DOT-mode run, routing
// the git probe through the run's CommandRunner when one is present.
//
// NFR7 (local runs MUST stay byte-identical): when runner is nil — every LOCAL
// run, since rbc.sshRunner is nil unless a remote worker was selected — this
// calls the bare gitprobe.ResolveWorktreeHEAD (exec.Command + cmd.Dir), unchanged. Only
// REMOTE runs (runner != nil, an SSHRunner) take the runner-routed path
// (gitprobe.ResolveWorktreeHEADVia, `git -C <wtPath> rev-parse HEAD` over the transport),
// which is REQUIRED on a worker whose worktree lives on a separate filesystem
// that box A cannot chdir into.
func resolveDotWorktreeHEAD(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	if runner == nil {
		return gitprobe.ResolveWorktreeHEAD(ctx, wtPath)
	}
	return gitprobe.ResolveWorktreeHEADVia(ctx, runner, wtPath)
}

// dotNodeTerminalFailure classifies what the agent REPORTED on its way out and
// returns the reason the node must fail with. ok is false when the exit is not a
// failure.
//
// It is the graph terminal decision. It keeps the three cases that the retired
// imperative tail previously duplicated:
//
//   - CHB-020 branch 1, the Stop hook reported WORK_COMPLETE or
//     REVIEWER_VERDICT. A pass, whatever the exit code was.
//   - Nothing reported AND exit 0 AND no watcher error. This is the auto-close
//     heuristic for twin-blind runs, and it is also a pass. A ProcessExit
//     harness reports nothing by design, so dropping this case would fail every
//     codex and pi node.
//   - Everything else is a failure: a FAILURE_SIGNAL, a non-zero exit with
//     nothing reported, or a watcher that could not read the progress stream.
//
// The graph decided node success on HEAD advance alone before this existed, so
// an agent that committed and then signalled failure was recorded as a SUCCESS
// node and its work was merged (hk-v4wer).
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-020.
func dotNodeTerminalFailure(
	sessionID string,
	exit runloop.ExitInfo,
	socketOutcome *handler.ExportedOutcomeEmittedPayload,
	watcherErr error,
) (string, bool) {
	term := handler.MapWaitReturnToTerminalEvent(sessionID, exit.ExitCode, exit.WaitErr, socketOutcome)
	watcherFailed := watcherErr != nil && !isWatcherErrCanceled(watcherErr)

	if term.Type == handlercontract.ProgressMsgTypeAgentCompleted {
		return "", false
	}
	if !outcomeIsAnAgentReport(socketOutcome) && exit.ExitCode == exitCodeClean && !watcherFailed {
		return "", false
	}

	switch {
	case watcherFailed:
		return fmt.Sprintf("watcher error: %v exit=%d", watcherErr, exit.ExitCode), true
	case term.SubReason != "":
		return fmt.Sprintf("agent_failed class=%s sub_reason=%s exit=%d", term.Class, term.SubReason, exit.ExitCode), true
	default:
		return fmt.Sprintf("exit=%d", exit.ExitCode), true
	}
}

// outcomeIsAnAgentReport reports whether an outcome_emitted payload actually
// says what the agent decided. Only the three CHB-020 kinds do.
//
// The bridge also emits an outcome_emitted carrying just an `error` field and no
// kind at all: hookrelay.go's Stop handler does this when the phase is reviewer
// and .harmonik/review.json is absent or malformed. That payload is the BRIDGE
// reporting that it could not find the file it was told to read. It is not the
// agent reporting anything, and reading it as one turns a clean exit into a
// failure.
//
// A cognition gate is the case that made this matter. It launches with
// ReviewLoopPhaseReviewer (dot_gate.go) but writes gate-verdict.json, so the
// bridge looks for review.json, never finds it, and emits missing_review_file on
// EVERY gate — including one that produced a perfectly good verdict. Treating a
// kindless payload as "nothing reported" leaves the exit code to decide, which
// is the same answer this function gives a harness that reports nothing by
// design.
//
// It does not soften the reviewer check. A reviewer that leaves no readable
// verdict is refused earlier, by the verdict == nil branch in
// dispatchDotAgenticNode, before this classifier is consulted.
func outcomeIsAnAgentReport(outcome *handler.ExportedOutcomeEmittedPayload) bool {
	if outcome == nil {
		return false
	}
	switch outcome.Kind {
	case "WORK_COMPLETE", "REVIEWER_VERDICT", "FAILURE_SIGNAL":
		return true
	default:
		return false
	}
}

// errDotNoChangeSubsumed is returned by dispatchDotAgenticNode when the
// implementer exited without advancing HEAD and a prior run already merged the
// bead's work on the branch this run lands on. driveDotWorkflow maps this to
// dotWorkflowResult{subsumed:true} so workloop.go can close-subsumed instead
// of reopening. Bead: hk-9v5yo, hk-1a7yb.
var errDotNoChangeSubsumed = errors.New("dot: noChange-subsumed: work already merged on the branch this run lands on")

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
	// bead's work was already merged on the branch this run lands on
	// (noChange-subsumed). The caller closes the bead rather than reopening it.
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
// like the finalize read, which uses ReadReviewVerdictLocalRetry.
// Without this, a local DOT run that observes review.json mid-flush gets a
// single no-retry read and false-fails the whole run on a transient
// ErrMalformed — the review-loop fix (hk-1hgjr) never applied to the DOT path.
func readDotReviewVerdictRetry(ctx context.Context, runner tmux.CommandRunner, wtPath string) (*workspace.ReviewVerdict, error) {
	if gitprobe.RunnerIsLocalFS(runner) {
		return workspace.ReadReviewVerdictLocalRetry(ctx, wtPath)
	}
	return workspace.ReadReviewVerdictVia(ctx, runner, wtPath)
}

// readDotReviewerBudgetSentinel keeps remote transport failures distinct from
// confirmed marker absence. The DOT driver must see the wrapped transport
// classification instead of converting an unreachable worker into no-verdict.
func readDotReviewerBudgetSentinel(ctx context.Context, runner tmux.CommandRunner, wtPath, nodeID string) (*reviewerBudgetSentinel, error) {
	sentinel, err := ReadReviewerBudgetSentinelVia(ctx, runner, wtPath)
	if err != nil {
		return nil, fmt.Errorf("read reviewer budget sentinel for node %q: %w", nodeID, err)
	}
	return sentinel, nil
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
				killProcessGroup(cmd.Process, "auto-status inspection")
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
	marker := readAutoStatusMarkerOrReport(ctx, runner, wtPath)
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
//   - parent ctx cancel   → FAIL + failure_class=canceled
//   - outside signal kill → FAIL + failure_class=canceled (the gate reached no
//     verdict, so the run stops for triage and is never reported as a test
//     failure — hk-killed-gate-read-as-red-0hi5z)
//   - gate could not RUN  → FAIL + failure_class=structural (hk-2f3v4)
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
//
// hk-0kdr6: the worktree copy of the gate log dies with the worktree. The
// worktree is removed on the run's terminal transition, so by the time a cell or
// a run reports RED the path the daemon just logged does not exist and the only
// record of WHY the merge decision failed is gone. Every failed gate therefore
// ALSO appends to a run-scoped archive under the project's own .harmonik/, which
// nothing removes, and the daemon log points at THAT path rather than the
// worktree one.
func dispatchDotToolNode(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, runner tmux.CommandRunner, projectDir, wtPath string, node *dot.Node, env []string) (core.Outcome, error) {
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
			killProcessGroup(cmd.Process, "commit gate")
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
		writeGateLog(gateLogPath, combined)
	}

	// hk-0kdr6: the durable copy. Unlike the worktree copy this one outlives the
	// run, and unlike the worktree copy it is written for a REMOTE run too — the
	// bytes came back over the runner, and the archive lives on box A. Appending
	// (not truncating) keeps every attempt: a red gate drives the cascade back to
	// implement and the same node runs again, and what the EARLIER attempts failed
	// on is the whole question a red cell asks. Report the archive path, because
	// it is the one that still exists when somebody reads the log.
	if archived := appendGateLogArchive(projectDir, runID, node.ID, combined, err); archived != "" {
		gateLogPath = archived
	}

	outputTail := tailString(string(combined), dotGateOutputTailBytes)

	// Every reason a gate FAILs is decided by classifyDotToolNodeFailure, which
	// returns the class and the one-line description this path logs for it. An
	// empty description is a class that is logged nowhere.
	fc, logDesc := classifyDotToolNodeFailure(err, execCtx.Err(), ctx.Err(), combined, node.ID, timeoutSecs, runner != nil)
	if logDesc != "" {
		fmt.Fprintf(os.Stderr, "daemon: %s; gate log: %s\n", logDesc, gateLogPath)
	}
	return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc, Notes: outputTail}, nil
}

// classifyDotToolNodeFailure maps a commit-gate command that did not exit 0 to
// its failure class, and to the description dispatchDotToolNode logs for it. The
// description is the part of the log line between "daemon: " and the
// "; gate log: <path>" suffix the caller appends; "" means log nothing.
//
// execCtxErr is the per-node timeout context's error and runCtxErr is the parent
// run context's error, both read after the command returned. combined is the
// gate's combined stdout+stderr.
//
// remote says a CommandRunner ran the gate rather than the daemon running it as
// a direct child. That is all it says — the caller passes runner != nil, and the
// runner that production wires in is an SSHRunner. A different runner would take
// this branch and would not necessarily mean ssh at all.
//
// It is here because, for an ssh runner, it changes what one exit code MEANS:
// ssh exits 255 when the transport failed or when the remote command died from a
// signal it cannot report. No ssh runs on the local path, so a LOCAL 255 is an
// exit status the gate command itself returned, and the same number must not be
// read the same way. That is what the parameter buys — one reading that is only
// available on one path.
//
// It is a CONFLATION, and a knowing one: ssh also returns the remote command's
// OWN exit status, and 255 is a legal one, so a remote gate that genuinely chose
// to exit 255 is read here as a gate that reached no verdict. That is the same
// mis-reading in the other direction, and it is accepted because the cost is
// asymmetric — a gate wrongly read as "no verdict" stops the run for triage,
// while a gate wrongly read as a verdict sends an implementer to fix a fault
// nobody observed. Nothing on this path approves anything.
// (hk-gate-error-143-still-deterministic-rhske)
//
// THE ORDER OF THE CHECKS BELOW IS LOAD-BEARING. The two kills the daemon issues
// itself — the node's own timeout, and a run teardown / re-dispatch — must be
// caught before the outside-signal branch, because that branch means "something
// other than the daemon signalled the gate" and is only true once these two are
// ruled out.
func classifyDotToolNodeFailure(err, execCtxErr, runCtxErr error, combined []byte, nodeID string, timeoutSecs int, remote bool) (class core.FailureClass, logDesc string) {
	// Timeout-killed: parent deadline exceeded first.
	if errors.Is(execCtxErr, context.DeadlineExceeded) {
		return core.FailureClassTransient, fmt.Sprintf("dot tool node %q timed out after %ds", nodeID, timeoutSecs)
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
	if runCtxErr != nil {
		return core.FailureClassCanceled, ""
	}

	// Killed by a signal from OUTSIDE the daemon's own deadline. The two branches
	// above cover every kill the daemon itself issues — the node's own timeout and
	// a run teardown / re-dispatch — so reaching here means something else
	// signalled the gate mid-flight.
	//
	// A killed gate has said NOTHING about the code. It never reached a verdict,
	// so reading it as a deterministic test failure invents one: the run drives the
	// commit_gate→implement back-edge and the implementer is resumed to fix a fault
	// nobody observed, on a tree that may be perfectly healthy. That is a whole
	// extra agent pass spent for nothing, and whatever it changes is changed for no
	// reason.
	//
	// canceled is the class the contract already names for a gate that a signal
	// stopped (handler-contract.md §4.1 HC-063 exit-state table, and the
	// shell-handler failure_class note in §III.1). standard-bead.dot conditions its
	// commit_gate out-edges on SUCCESS, deterministic and transient only, so
	// canceled matches none of them and takes the unconditional fallback to
	// close-needs-attention: the run STOPS and says why, which is what a gate that
	// reached no verdict has earned. Retrying on the transient self-loop is the
	// wrong answer here — nobody has identified what killed the gate, so a retry
	// runs straight back into the same kill and spends another full `make full`
	// before stopping anyway. No path here approves anything.
	// (hk-killed-gate-read-as-red-0hi5z)
	//
	// Measured live 2026-08-11: `make full` ran ~19 minutes of a 3600s budget,
	// passed build, lint and the subprocess tier, and was SIGTERM'd in the scenario
	// tier ("make: *** [full] Terminated: 15"). It reported "the build/test gate
	// did not pass. Fix the failure and re-commit".
	if sigDesc, killed := gateKilledBySignal(err, combined); killed {
		return core.FailureClassCanceled, fmt.Sprintf("dot tool node %q was KILLED mid-flight (%s) — it reached no verdict, so this is NOT a test failure; canceled, routed to close-needs-attention for triage", nodeID, sigDesc)
	}

	// REMOTE only: ssh exits 255 for a transport it could not make or keep, and
	// for a remote command whose exit status it cannot report because a signal
	// ended it. Both mean the gate reached NO VERDICT, which is the same thing
	// the kill branch above says, so it takes the same class. Before this, a
	// dropped connection and a killed remote gate that wrote no make recipe line
	// both fell through to deterministic and sent the implementer to fix a fault
	// nobody observed. Locally the number means nothing of the sort, which is why
	// this reads `remote`.
	//
	// It also swallows a remote gate that genuinely exited 255 on its own, since
	// ssh passes the remote command's own status through. See the `remote`
	// paragraph on this function for why that trade is taken.
	// (hk-gate-error-143-still-deterministic-rhske)
	if remote && tmux.IsSSHConnectionFailure(err) {
		return core.FailureClassCanceled, fmt.Sprintf("dot tool node %q ran on a worker and ssh exited 255 — the transport failed or the remote gate died from a signal; it reached no verdict, so this is NOT a test failure; canceled, routed to close-needs-attention for triage", nodeID)
	}

	// Infra-signature check: go build-cache TOCTOU failures emit a distinctive
	// "package X is not in std" message when concurrent go clean -cache deletes
	// stdlib cache entries mid-build. These are not code defects — retry on the
	// same tree succeeds — so classify as transient (self-loop) rather than
	// deterministic (fix-loop back to implement). (hk-7xgu4 / hk-1veco FIX2)
	if isGateBuildCacheInfraError(combined) {
		return core.FailureClassTransient, fmt.Sprintf("dot tool node %q failed (%v) with build-cache infra signature (transient)", nodeID, err)
	}

	// The gate could not RUN — a command it names does not exist. No amount of
	// re-implementing fixes a missing tool, so this must NOT drive the
	// commit_gate→implement back-edge. Structural is the class that says "retry
	// only after an approach change"; standard-bead.dot conditions its back-edges
	// on deterministic and transient only, so its unconditional fallback carries
	// a structural gate FAIL straight to close-needs-attention. The run stops and
	// says why, which is the whole point. (hk-2f3v4)
	//
	// Measured 2026-08-10 on the codex:local cell: the scratch clone had no
	// pinned gofumpt, every gate died on it, and the run spent four implement
	// passes and about 60 minutes of real agent time on a fault no pass could
	// reach. It reported only "incomplete".
	if isGateCannotRunError(combined) {
		return core.FailureClassStructural, fmt.Sprintf("dot tool node %q could not RUN — a command it names does not exist (%v); structural, NOT routed back to the implementer", nodeID, err)
	}

	// Non-zero exit code (1..255) → deterministic failure. This is the gate-FAIL
	// case that drives the commit_gate→implement back-edge; surface the diagnostic.
	return core.FailureClassDeterministic, fmt.Sprintf("dot tool node %q failed (%v)", nodeID, err)
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

// gateOutputSignature is one content string a gate-output detector matches,
// paired with the rendering gateEvidenceQuote writes in its place.
//
// The pairing is the point, and it is why this is a record rather than two
// lists kept side by side. The detectors below scan the WHOLE gate log, with no
// position scoping to protect them, and every line this daemon writes about a
// gate lands in the NEXT gate's log — the gate runs the suite under `go test
// -v`, and the report tool replays a failing test's own output. A detector
// string that a diagnostic reproduces word for word therefore makes the
// detector a generator of its own trigger.
//
// Measured 2026-08-12: the kill diagnostic had already been taught to strip
// make's recipe anchor, but nothing stripped `] Error 127`, so a failure
// message that quoted a `... Error 127` line back at the reader turned the next
// genuinely RED gate structural, and the implementer was told "NOTHING is known
// to be wrong with your change" — the exact sentence this whole unit exists to
// keep off a human's screen.
//
// Holding both strings in ONE record makes the sanitizer's coverage DERIVED
// rather than maintained in parallel: a detector reads text and gateEvidenceQuote
// reads quoted, so adding a signature to a table extends both at once.
//
// That is a CONVENTION, and the language does not enforce it. gateUnscopedSignatures
// below is an ordinary expression: a new []gateOutputSignature table can be handed
// to gateOutputHasAnySignature and reach the classifier without ever being named in
// it, and then a string this daemon matches on is one it can also write unrewritten.
// What holds the convention is a test —
// TestEveryDetectorTableIsReachableFromTheSanitizer reads this file and requires
// every package-level table here to be reachable from the sanitizer or to carry a
// written exemption. It sees package-level tables only, so a detector that matches a
// bare string with no table, or builds one inside a function, is still outside it.
// (hk-gate-selftest-fakes-a-kill-0hj0i)
type gateOutputSignature struct {
	// text is what the detector matches, anywhere in the gate's output.
	text string
	// quoted is what gateEvidenceQuote writes in text's place. It MUST NOT
	// contain text — otherwise the rewrite buys nothing — and it MUST still
	// read as the same fact to a human, because these strings are the whole
	// content of a diagnostic somebody has to act on.
	quoted string
}

// gateBuildCacheInfraSignatures are the known go build-cache / toolchain
// infrastructure failure signatures. Infra failures are transient — retry on the
// same committed tree succeeds — and must not be misclassified as deterministic
// (which would bounce the bead back to the implementer with a false "fix the
// build" signal). (hk-y3frr / hk-guez / hk-7xgu4 TOCTOU lineage)
//
//   - "is not in std" — concurrent go clean -cache deleted stdlib entries while
//     "go build ./..." was running; observed as "package bufio is not in std".
//     Passes immediately on retry after the cache is warm again.
var gateBuildCacheInfraSignatures = []gateOutputSignature{
	{text: "is not in std", quoted: "is not part of std"},
}

// gateCannotRunSignatures are the signatures that say the gate could not RUN, as
// opposed to running and finding a fault in the change. The only cause in this
// class is a command the gate names that does not exist.
//
// Two signatures, both meaning exit 127 from a shell:
//
//   - "] Error 127" — make's recipe-failure line, e.g.
//     `make[2]: *** [fmt-check] Error 127`. make reports the recipe's own exit
//     status, so the daemon sees make's exit 2 and never sees the 127 itself;
//     the output line is the only place it appears. The bracket is part of
//     make's format and keeps the match off a bare "127" in test output.
//   - ": command not found" — the same failure when the missing name has no
//     slash in it, so the shell reports it by name rather than by path.
var gateCannotRunSignatures = []gateOutputSignature{
	{text: "] Error 127", quoted: "] Error code 127"},
	{text: ": command not found", quoted: ": command was not found"},
}

// gateUnscopedSignatures is every signature whose detector reads the whole gate
// log. It is exactly the set gateEvidenceQuote has to rewrite, and it is built
// FROM the detectors' own tables rather than restated, so adding a signature to
// either table above extends the sanitizer with it. Adding a whole new table does
// NOT extend it — an operand has to be added here too, and the test named on
// gateOutputSignature is what makes that a failure rather than a silent hole.
var gateUnscopedSignatures = slices.Concat(gateBuildCacheInfraSignatures, gateCannotRunSignatures)

// gateOutputHasAnySignature reports whether output contains any signature in sigs.
func gateOutputHasAnySignature(output string, sigs []gateOutputSignature) bool {
	for _, sig := range sigs {
		if strings.Contains(output, sig.text) {
			return true
		}
	}
	return false
}

// isGateBuildCacheInfraError reports whether gate output matches a known build-
// cache / toolchain infrastructure signature. See gateBuildCacheInfraSignatures.
func isGateBuildCacheInfraError(output []byte) bool {
	return gateOutputHasAnySignature(string(output), gateBuildCacheInfraSignatures)
}

// isGateCannotRunError reports whether the gate output shows the gate could not
// RUN. See gateCannotRunSignatures.
//
// A gate log that quotes one of these strings for some other reason is
// misclassified. That costs a run that stops and names the reason instead of
// looping, which is the safer direction and never approves anything — but it is
// still a wrong answer, so anything this daemon WRITES has the strings rewritten
// on the way out (gateEvidenceQuote).
func isGateCannotRunError(output []byte) bool {
	return gateOutputHasAnySignature(string(output), gateCannotRunSignatures)
}

// gateBackEdgeMessage builds the note delivered to an implementer that a failed
// commit gate has routed back to. It must never assert a failure the gate did not
// observe. (hk-killed-gate-read-as-red-0hi5z)
//
// Only a DETERMINISTIC gate FAIL means the gate ran and found a fault. Every other
// class means the gate stopped before it reached a verdict — it was killed, the
// run was torn down, or the toolchain glitched — and the tree may be perfectly
// healthy. The old message said "the build/test gate did not pass, fix the failure
// and re-commit" in all of those cases. Measured live 2026-08-11, an implementer
// received exactly that after a 19-minute gate was SIGTERM'd in the scenario tier
// with nothing failing, and was sent to fix a fault that did not exist.
//
// The classifier now routes a killed gate to canceled, which the graph's
// unconditional fallback carries to close-needs-attention, so this branch should
// no longer see one. This is the second line: whatever class arrives here, the
// wording matches it, and a future class needs no second fix.
//
// notes is REAL gate output, and it goes through gateEvidenceQuote before it is
// quoted back. This message is the daemon speaking about one gate, and it
// travels — into the reviewer-feedback file, into the prompt the next
// implementer reads, and into the diagnostics a test prints when it checks this
// path. Any of those can land in the NEXT gate's log, at column 0, where the
// classifier reads it as make's own report. That is the same defect the reading
// end was repaired for, arriving through the writing end, and no position rule
// can see it. (hk-e0yhw)
func gateBackEdgeMessage(class core.FailureClass, notes string) string {
	quoted := gateEvidenceQuote(notes)
	if class == core.FailureClassDeterministic {
		return "The commit gate failed — your commit was recorded but the build/test gate did not pass. " +
			"Fix the failure and re-commit:\n\n" + quoted
	}
	return "The commit gate did not finish — your commit was recorded, but the gate stopped before it " +
		"reached a verdict, so NOTHING is known to be wrong with your change. Do not invent a fix and do " +
		"not rewrite working code. Re-read .harmonik/agent-task.md, confirm your work is complete and " +
		"committed, and change something only if it is genuinely missing. The last output the gate " +
		"produced follows, for context only — it is a partial log, not a failure report:\n\n" + quoted
}

// gateKilledBySignal reports whether the gate was killed by a signal rather than
// exiting on its own, and returns a short description for the log. A killed gate
// produced NO verdict — it says nothing about the code — so it must never be
// classified deterministic, which is the class that sends the implementer back to
// fix a failure. It is classified canceled instead, and the graph stops the run
// for triage. (hk-killed-gate-read-as-red-0hi5z)
//
// Two independent detectors, because neither one covers both cases:
//
//   - Exit state. The LOCAL gate is a direct child of the daemon, so a signal that
//     reaches it lands in syscall.WaitStatus and this reading is exact. It is the
//     detector that catches the live 2026-08-11 case, where the process group was
//     signalled and make re-raised the signal to itself.
//   - Output signature. Two cases leave the exit state clean. A signal that reaches
//     only a DESCENDANT lets the top-level make report its recipe's death and then
//     exit 2 on its own. And on a REMOTE run the exit state belongs to the local
//     `ssh` client, not to the gate on the worker. In both, make's recipe-failure
//     line is the only evidence, and it is the shape the live run produced:
//     `make[1]: *** [test-scenario] Terminated: 15`.
//
// WHERE the output detector looks is as load-bearing as what it looks for, and
// two live findings are the same detector wrong in opposite directions:
//
//   - The gate log is a VERBOSE REPLAY of the whole suite, so it contains every
//     string this detector matches — make output that tests print as fixtures,
//     and the classifier's own diagnostic, which used to quote the matched line
//     word for word. Scanning the whole transcript found that text 13,000 lines
//     from the end of a gate that had GENUINELY FAILED and reported a kill; the
//     real failure was reported to the operator as "NOTHING is known to be wrong
//     with your change". (hk-gate-selftest-fakes-a-kill-0hj0i)
//   - A signal that reaches only a DESCENDANT is reported by the recipe shell as
//     exit 128+N, so make prints `*** [test-scenario] Error 143` and never names
//     the signal, and the top-level shell exits 2 on its own. This comment used
//     to claim the exit-state detector saw through that case "on every local
//     run". It does not, and cannot: the daemon's own child exited cleanly.
//     Error 137 is the OOM killer and Error 143 is a SIGTERM to a child, which
//     are the shapes a loaded box produces — and a loaded box is exactly when a
//     gate gets killed. (hk-gate-error-143-still-deterministic-rhske)
//
// THE PRECEDENCE RULE that reconciles them is REGIONAL, not "the last line
// wins": only make's TERMINAL recipe-failure cascade is evidence about how the
// GATE ended (gateTerminalRecipeFailures). Text outside that cascade is the
// suite's transcript, and it says nothing about the gate however exactly it
// matches — that is why a `Terminated: 15` in the transcript loses to an
// `Error 1` cascade.
//
// WHERE the cascade begins is not a clean line, and the comment used to imply it
// was. gateTerminalRecipeFailures walks backwards and tolerates
// gateCascadeGapLines of non-recipe text between members, so a stray anchored
// line that lands inside that window is pulled in as a cascade member — and go
// test's four-line failure trailer is exactly the tolerance. That window is why
// the scan ALSO rejects an INDENTED line: make writes its recipe failures at
// column 0 and go test indents everything a test wrote, so indentation says the
// line came from a test and not from make, whatever else is on it.
//
// INSIDE the cascade any kill signature wins, and the first one in make's own
// write order decides (gateSignalKillOutputLine). A kill is named two ways and
// either one is enough — a signal word, or an exit code in the 128+N range. So
// `make[1]: *** [test-scenario] Terminated: 15` followed by `make: *** [full]
// Error 2` IS a kill: the inner recipe died from the signal, and the outer make
// only reports that it gave up. A cascade that names neither a signal nor a
// 128+N code is a VERDICT: the gate ran and found a fault.
//
// A false positive here costs a run that stops at close-needs-attention instead
// of looping back to the implementer; it can never approve anything.
func gateKilledBySignal(err error, output []byte) (string, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ProcessState != nil {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return "killed by signal " + ws.Signal().String(), true
		}
	}
	if line, ok := gateSignalKillOutputLine(output); ok {
		// The line is already sanitized (gateSignalKillOutputLine quotes it as
		// it extracts it), but gateKillOutputPrefix ends in `: ` and can supply
		// the head of a signature the sanitizer just removed. Same seam as
		// gateFailureTail, same guard. This description becomes the logDesc
		// dispatchDotToolNode writes to stderr at column 0, where the NEXT
		// gate's classifier reads it with no indentation to rule it out.
		if gateJoinBuildsASignature(gateKillOutputPrefix, line) {
			line = "…" + line
		}
		return gateKillOutputPrefix + line, true
	}
	return "", false
}

// gateKillOutputPrefix labels the cascade line gateKilledBySignal quotes. It is
// text this DAEMON writes onto sanitized output, so it is a seam on the same
// terms as gateFailureTailPrefix.
//
// It is named rather than inlined so that the string handed to
// gateJoinBuildsASignature and the string actually pasted onto the line are ONE
// value and cannot drift apart. That is the whole benefit, and it is worth
// stating exactly: no test reads this identifier, and the seam sweep would probe
// the same bytes if the literal were inlined twice — right up to the day someone
// edited one of the two copies.
const gateKillOutputPrefix = "gate output reports a signal kill: "

// gateSignalKillOutputLine finds the line in make's TERMINAL recipe-failure
// cascade that says the gate died from a signal, and returns it for the log.
// Within the cascade the FIRST such line decides, because make unwinds from the
// recipe that died out to the top: the inner recipe carries the signal, and the
// outer make lines above it only report that it gave up.
// Returns the line rendered by gateEvidenceQuote, never the raw text.
func gateSignalKillOutputLine(output []byte) (string, bool) {
	for _, line := range gateTerminalRecipeFailures(output) {
		if gateRecipeLineNamesAKill(line) {
			return gateEvidenceQuote(line), true
		}
	}
	return "", false
}

// gateLineIsIndented reports whether a line begins with whitespace.
//
// It is the cheap, non-make-specific test for "a TEST wrote this line, not
// make". make writes its recipe-failure lines at column 0 —
// `make[1]: *** [test] Error 1` — and go test indents everything a test wrote by
// at least four spaces: the `file.go:NN: ` first line of a failure message and
// every continuation line under it alike. Requiring a cascade member to START
// with `make` would do the same job for a Go build and break every gate whose
// driver is not make, so indentation is the more portable of the two.
//
// The cost is not zero, and it falls on the kill detector. A gate whose output
// reaches this daemon through anything that indents — a log formatter that adds
// a prefix, a wrapper that pipes through `sed 's/^/  /'`, a harness that quotes
// the child's output — has EVERY line indented, including make's own. Then no
// line is admitted to the terminal cascade, gateSignalKillOutputLine finds
// nothing, and a killed gate reads as a plain failure again: the exact defect
// this file was opened to remove. Nothing in the daemon indents gate output
// today, which is why this is a stated limit rather than a bug. Anyone adding a
// step between the gate and this classifier has to read this paragraph first.
//
// It replaced a narrower rule that matched go test's `file.go:NN: ` prefix
// alone. Measured 2026-08-12: that rule left the multi-line case open, because
// only the FIRST line of a failure message carries the prefix. A sibling test in
// this package writes `…reads as a clean exit:\n%s` with a raw anchor on the
// continuation line, and a red gate whose log replayed it classified canceled —
// the same defect, one line further down.
//
// What it does NOT cover: a line written straight to stdout or stderr, which
// nothing indents. The daemon's own diagnostics are that shape, and what covers
// them is gateEvidenceQuote at the writing end, not this.
func gateLineIsIndented(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

// gateRecipeFailureAnchor is the prefix make writes when a recipe fails:
// `make[1]: *** [test-scenario] Error 1`. It is the shape make itself uses to
// report on a command it ran.
const gateRecipeFailureAnchor = "*** ["

// gateCascadeGapLines bounds how many non-recipe lines may sit between two
// members of make's terminal cascade. make unwinds a failed recipe through its
// recursive invocations back to back; the tolerance covers the few lines that
// legitimately interleave — `make[1]: Leaving directory …` under -w, `make: ***
// Waiting for unfinished jobs`, and ssh's "Connection to <host> closed." after a
// remote gate. It is deliberately small: every line of slack is a line of
// verbose test output that gets read as make's own report.
const gateCascadeGapLines = 4

// gateTerminalRecipeFailures returns make's recipe-failure lines from the END of
// the gate output, in the order make wrote them — the cascade it prints as one
// failed recipe unwinds through the recursive invocations and stops the build.
//
// What scoping to the tail buys: the gate runs `make full`, which runs the suite
// under `go test -v`, so the log replays every line every test printed. Any
// string this file looks for can appear in there — as a test fixture, or as the
// daemon's own diagnostic about some earlier gate. Those lines are
// indistinguishable from the real thing BY CONTENT. They are distinguishable by
// POSITION: only the lines make wrote as it gave up sit at the end of the log.
// (hk-gate-selftest-fakes-a-kill-0hj0i)
//
// Position alone is not quite enough, because the region has a soft edge: the
// scan tolerates gateCascadeGapLines of slack between members, and go test's
// failure trailer (`--- FAIL`, `FAIL`, the package line, `FAIL`) is exactly that
// many lines. So an anchored line a TEST wrote can sit inside the window, ahead
// of make's real cascade, and be admitted as its first member — which is the
// original bug with a four-line reach instead of a 13,000-line one. Hence the
// second exclusion below: gateLineIsIndented.
func gateTerminalRecipeFailures(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	// Drop trailing blank lines first. The cascade is the last thing MAKE
	// writes, but the captured bytes need not end there — a shell that echoes
	// after the gate, and a transport that flushes the stream, both leave empty
	// lines behind it. An empty line is not a recipe failure, so without this it
	// counts as gap, and enough of them push the whole cascade out of reach.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	// Walk backwards from the end, allowing gateCascadeGapLines of slack between
	// members, and stop as soon as the gap is exceeded. The scan is bounded by
	// the cascade itself, so the transcript above it is never examined — a
	// 17,000-line log costs one split and a few lines of comparison.
	var reversed []string
	gap := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], gateRecipeFailureAnchor) && !gateLineIsIndented(lines[i]) {
			reversed = append(reversed, strings.TrimSpace(lines[i]))
			gap = 0
			continue
		}
		gap++
		if gap > gateCascadeGapLines {
			break
		}
	}
	cascade := make([]string, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		cascade = append(cascade, reversed[i])
	}
	return cascade
}

// gateCascadeKillWords are the signal names make writes into a recipe-failure
// line when the process IT forked was killed by a signal.
//
// These are read ONLY inside make's terminal cascade
// (gateTerminalRecipeFailures), and a line that has lost make's recipe anchor
// can never be a cascade member. That position scoping is what protects them, so
// unlike gateUnscopedSignatures they are NOT rewritten by gateEvidenceQuote —
// and they must not be, because naming which signal ended the gate is the whole
// value of the diagnostic.
var gateCascadeKillWords = []string{"Terminated", "Killed", "Interrupt", "Hangup"}

// gateRecipeLineNamesAKill reports whether one recipe-failure line says the
// command died from a signal. Two spellings, because make reports a signalled
// child either way, and which one it writes depends on how far down the signal
// landed:
//
//   - by NAME (`Terminated: 15`) when make's OWN child took the signal, so make
//     read the signal out of the wait status itself;
//   - by CODE 128+N (`Error 143`) when the signal landed on a grandchild. The
//     recipe shell reports its dead child as an ordinary exit status of 128+N,
//     and make cannot tell that apart from a status the shell chose.
//
// make writes the recipe-failure line in both cases; the shell only supplies the
// number in the second.
func gateRecipeLineNamesAKill(line string) bool {
	for _, word := range gateCascadeKillWords {
		if strings.Contains(line, word) {
			return true
		}
	}
	return gateRecipeExitCodeIsSignalDeath(line)
}

// gateRecipeExitCodeIsSignalDeath reports whether a recipe-failure line carries
// an exit code in the range a shell uses to report a child that died from a
// signal: 128+N, i.e. 129..255. `Error 137` is the OOM killer and `Error 143` is
// a SIGTERM.
//
// It is a RANGE, not a proof, and this comment used to read as one. Any program
// may `exit 200` of its own accord, and a gate command that does reads as killed
// here. The range is used anyway because it is the only evidence a grandchild
// kill leaves, and because being wrong this way stops the run for triage while
// being wrong the other way sends an implementer to fix a fault nobody observed.
//
// Below 129 is a real exit status a command chose, and the classifier must keep
// reading those as verdicts: `Error 1` is a test that failed, `Error 2` is make
// itself giving up, and `Error 127` is a missing command, which the structural
// branch owns.
func gateRecipeExitCodeIsSignalDeath(line string) bool {
	const marker = "] Error "
	i := strings.LastIndex(line, marker)
	if i < 0 {
		return false
	}
	rest := line[i+len(marker):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return false
	}
	code, err := strconv.Atoi(rest[:end])
	if err != nil {
		return false
	}
	return code >= 129 && code <= 255
}

// gateRecipeFailureAnchorQuoted is what gateEvidenceQuote writes in place of
// make's recipe anchor. It says the same thing to a reader and matches nothing.
const gateRecipeFailureAnchorQuoted = "recipe ["

// gateEvidenceQuote renders gate-log text for a message this daemon writes,
// WITHOUT reproducing any string this file's detectors key on. It takes one line
// or a whole run of them — every rewrite is a plain substring replacement, so a
// multi-line excerpt is covered the same way a single line is.
//
// This is not cosmetic. The classifier's diagnostic goes to stderr, the gate runs
// the suite under `go test -v`, and a test that drives this path therefore prints
// the diagnostic into the NEXT gate's log. Quoting the matched line word for word
// made the detector a generator of its own trigger: on 2026-08-12 it matched its
// own earlier message and relabelled a genuinely red gate as a kill. A diagnostic
// that describes the evidence cannot do that. (hk-gate-selftest-fakes-a-kill-0hj0i)
//
// Its coverage is DERIVED, not listed. Two rewrites, one per class of detector:
//
//   - make's recipe anchor. Removing it is what disarms every CASCADE-SCOPED
//     detector at once — the kill words and the 128+N exit code are read only
//     inside make's terminal cascade, and gateTerminalRecipeFailures admits a
//     line to that cascade only if the anchor is on it. So the signal words stay
//     legible in the message, which is what a reader needs.
//   - every entry of gateUnscopedSignatures. Those detectors scan the entire log
//     and nothing about position protects them, so each one is rewritten to a
//     rendering that reads the same and matches nothing. Because that set IS the
//     detectors' own tables, a signature added to an EXISTING table arrives here
//     with it. A whole new table does not: gateUnscopedSignatures is an ordinary
//     expression, so a table that is never made an operand of it reaches the
//     classifier with no rewrite here, and so does one appended to at run time.
//     The convention is held by a test, not by the language — see the comment on
//     gateOutputSignature, which names it and names what it cannot see.
//
// It was written for the anchor alone, and for a while it defended one detector
// while starving the other: a message that had lost the anchor still carried
// `] Error 127`, and a red gate whose log replayed one read as structural.
func gateEvidenceQuote(text string) string {
	out := strings.ReplaceAll(text, gateRecipeFailureAnchor, gateRecipeFailureAnchorQuoted)
	for _, sig := range gateUnscopedSignatures {
		out = strings.ReplaceAll(out, sig.text, sig.quoted)
	}
	return out
}

// gateJoinBuildsASignature reports whether left+right carries a detector
// signature that NEITHER side carries on its own — one made out of a suffix of
// left and a prefix of right, at the seam where the two are pasted together.
//
// The sanitizer cannot see this. It runs on gate output BEFORE that output is
// pasted into a sentence, and at that moment the text is clean; the signature
// appears when a constant this daemon writes supplies the first bytes of one.
// Measured on the clause gateFailureTail builds: its prefix ends in `: `, which
// is the first two bytes of the `: command not found` signature, so an excerpt
// that begins `command not found` rebuilt the very string the sanitizer had
// just removed. (hk-e0yhw)
//
// It reads gateUnscopedSignatures, so an unscoped signature added to that table
// tomorrow is checked at this seam with no edit here. It does NOT read
// gateRecipeFailureAnchor, the sanitizer's other rewrite class: a constant that
// ended in `**` could supply the head of make's anchor the same way. No constant
// does today, so this is a stated limit rather than a covered case.
//
// Do not narrow that limit to "the anchor only matters beside a signal word".
// gateRecipeLineNamesAKill falls through to gateRecipeExitCodeIsSignalDeath, so
// an anchored line carrying an exit code of 129..255 and NO signal word reads as
// a kill on its own. The exposure is wider than a signal-word reading suggests.
func gateJoinBuildsASignature(left, right string) bool {
	for _, sig := range gateUnscopedSignatures {
		for i := 1; i < len(sig.text); i++ {
			if strings.HasSuffix(left, sig.text[:i]) && strings.HasPrefix(right, sig.text[i:]) {
				return true
			}
		}
	}
	return false
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

// Terminal-node disposition attribute (node-level) and its value domain.
// Read out of dot.Node.UnknownAttrs — the same WG-031/WG-032 permissive-
// retention channel dotEdgeTraversalCap uses for traversal_cap. It is NOT a
// reserved attribute name in specs/workflow-graph.md §10 WG-031; promoting it
// to one is a spec amendment, not a code change.
const (
	dotTerminalDispositionAttr           = "terminal_disposition"
	dotTerminalDispositionSuccess        = "success"
	dotTerminalDispositionNeedsAttention = "needs_attention"
)

// dotTerminalNodeIsSuccess reports whether reaching terminalID means the run's
// work should be merged and its bead closed green. The second return value is
// empty when the graph classified the terminal, and otherwise carries the reason
// the run is being sent to needs-attention instead.
//
// The answer comes from the GRAPH, in this order:
//
//  1. The two terminal IDs reserved by WG-022 are normative and cannot be
//     redefined by a graph: "close" is normal completion, "close-needs-attention"
//     is the operator-attention close. Standard graphs therefore need no
//     annotation and behave exactly as they always have.
//  2. Any other terminal — WG-022 permits authors to declare them, and says
//     consumers route them per their own policy — must declare its polarity on
//     the node: terminal_disposition="success" or "needs_attention".
//  3. A terminal that declares nothing, or declares an unrecognized value, is
//     NOT classifiable. The run goes to needs-attention with a reason. This is
//     the one honest answer available: guessing from the node's spelling is what
//     merged failing eval runs in the first place (eval-bead.dot's failure
//     terminal is "close-fail"), and a broader name heuristic would be the same
//     defect with a wider blast radius.
//
// Inspecting inbound-edge topology or the last edge's preferred_label to infer
// disposition is forbidden by WG-021; this reads a declaration, not a topology.
func dotTerminalNodeIsSuccess(graph *dot.Graph, terminalID string) (success bool, why string) {
	switch terminalID {
	case "close":
		return true, ""
	case "close-needs-attention":
		return false, ""
	}

	var node *dot.Node
	for _, n := range graph.Nodes {
		if n.ID == terminalID {
			node = n
			break
		}
	}
	if node == nil {
		return false, fmt.Sprintf("terminal node %q is not declared in the graph", terminalID)
	}

	switch raw := node.UnknownAttrs[dotTerminalDispositionAttr]; raw {
	case dotTerminalDispositionSuccess:
		return true, ""
	case dotTerminalDispositionNeedsAttention:
		return false, ""
	case "":
		return false, fmt.Sprintf(
			"terminal node %q is not a reserved terminal (WG-022) and declares no %s=%q|%q; "+
				"the graph does not say whether reaching it is a success, so the run is not merged",
			terminalID, dotTerminalDispositionAttr,
			dotTerminalDispositionSuccess, dotTerminalDispositionNeedsAttention)
	default:
		return false, fmt.Sprintf(
			"terminal node %q declares %s=%q, which is not one of %q|%q",
			terminalID, dotTerminalDispositionAttr, raw,
			dotTerminalDispositionSuccess, dotTerminalDispositionNeedsAttention)
	}
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
			if _, incErr := cycles.Increment(runID, core.NodeID(fromID), core.NodeID(toID), cap); incErr != nil {
				// The edge count did not move, so SelectNextEdge cannot enforce the
				// traversal cap on this edge and the cascade can loop past it.
				fmt.Fprintf(os.Stderr, "daemon: dot cascade: increment traversal cap for edge %s→%s: %v (cap not enforced)\n", fromID, toID, incErr)
			}
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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeNoProgressDetected, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit no_progress_detected: %v\n", emitErr)
	}
}

// strandedCommitNote describes the commit a red gate is about to leave behind,
// for the no-progress terminals in the DOT walk (hk-2vx1n).
//
// A red gate routes the implementer back. When the implementer has nothing to
// add, HEAD does not advance and the run ends there — with a real commit
// sitting on the run branch that nothing will ever merge. Both no-progress
// terminals then report that HEAD did not advance, which reads as "the harness
// never landed anything". It landed something. The gate rejected the tree it
// was already sitting on.
//
// That wording is not a cosmetic problem. An operator who reads it goes and
// looks at the harness, and that misdirection is on record more than once. So
// when the node that routed the implementer back was the gate, and there IS a
// committed result, the reason names the stranded commit, its branch, and the
// tail of the gate output.
//
// This changes what a run SAYS, not what it does. The caller's disposition
// stays failure plus needs-attention either way, so no gate-red tree can merge
// through here. Whether the gate's failure was caused by this diff at all is
// the other half of hk-2vx1n and is not decided here.
//
// gateNodeID must match prevNodeID as well as gatePassed being false:
// gatePassed is also false when no gate has ever run, and that case is a
// genuine no-progress loop with nothing to preserve.
func strandedCommitNote(
	runID core.RunID,
	committed bool,
	gatePassed bool,
	gateNodeID string,
	prevNodeID string,
	headSHA string,
	gateNotes string,
) string {
	if !committed || gatePassed || gateNodeID == "" || prevNodeID != gateNodeID {
		return ""
	}
	// THE LEADING SPACE IS PUNCTUATION, NOT PROTECTION. This note is appended
	// to a sentence, and that is all the space is for. An earlier version of
	// this comment argued that the space also makes the line land INDENTED, so
	// gateLineIsIndented would rule it out of make's terminal cascade. That is
	// false at BOTH places this note is produced. dot_cascade_core.go glues it
	// onto "dot: no-progress detected at iteration N: HEAD did not advance" and
	// onto "dot: review fix-up stalled at iteration N: HEAD did not advance
	// after REQUEST_CHANGES". Both start at COLUMN 0, so the composed line is
	// not indented and the cascade-scoped detectors do see it.
	//
	// So BOTH rewrites in gateEvidenceQuote are load-bearing for this note, and
	// only the caller's shape shows it. Measured: with the recipe-anchor rewrite
	// disabled, this note goes red planted against the cascade IN ITS CALLER'S
	// SHAPE and stays green rendered bare — which is how the earlier reading
	// came to be recorded as a measurement. See
	// TestAMessageQuotingGateOutputCannotRelabelTheNextGate, which now renders
	// it both ways for exactly that reason.
	return fmt.Sprintf(
		" — the %s gate stayed red and the implementer added nothing; commit %s is preserved on %s and was NOT merged%s",
		gateNodeID, headSHA, workspace.TaskBranchPrefix+runID.String(), gateFailureTail(gateNotes))
}

// gateFailureTailMaxBytes bounds how much gate output the stranded-commit
// reason carries. The reason travels into run_failed, which an operator reads
// in one line, so it holds a hint and not a build log. The full output already
// reached the implementer as GATE_FAIL feedback.
const gateFailureTailMaxBytes = 200

// gateFailureTailPrefix labels the excerpt inside the stranded-commit reason.
// It is text this DAEMON writes, so it is part of the seam gateFailureTail has
// to keep signature-free; see the fourth step on gateFailureTail. Nothing
// follows the excerpt — TestGateFailureTail_KeepsTheEnd requires the clause to
// end where the gate output ends — so there is one seam here and not two.
const gateFailureTailPrefix = "; gate output: "

// gateFailureTail renders the last of a failed gate's notes for the
// stranded-commit reason (hk-2vx1n), prefixed and bounded, or "" when the gate
// recorded nothing. It keeps the TAIL rather than the head: a build or test
// gate names what failed at the end of its output.
//
// It carries REAL gate output into a line an operator reads, so it goes through
// gateEvidenceQuote for the reason given on gateBackEdgeMessage. The fold onto
// one line makes this the WORSE of the two: it puts make's recipe anchor in the
// middle of a single unindented line, where gateLineIsIndented cannot rule it
// out. (hk-e0yhw)
//
// THERE ARE FOUR STEPS AND THEY ARE IN THIS ORDER FOR A REASON. Fold, sanitize,
// bound, join. Three of them are ordered against each other; the fourth is the
// one the first version of this comment did not count, and it is the one that
// was broken:
//
//   - fold before sanitizing, because folding a newline to a space JOINS two
//     lines, and two lines that carry no signature apart can carry one together
//     — a line that ends in `:` above a line that reads `command not found`;
//   - sanitize before bounding, because gateEvidenceQuote makes text LONGER, so
//     a rewrite that happens after the cut pushes the excerpt back past
//     gateFailureTailMaxBytes;
//   - the cut re-arms nothing BY ITSELF: it only drops leading bytes, and any
//     substring of signature-free text is signature-free. That is an argument
//     about the cut, and it was written as though it were an argument about the
//     function;
//   - the JOIN is the fourth step and it is not safe by itself.
//     gateFailureTailPrefix ends in `: `, the first two bytes of the
//     `: command not found` signature, so an excerpt that begins `command not
//     found` rebuilds a signature the sanitizer removed — and this excerpt is
//     read by the classifier that scans a whole log, with no position rule to
//     save it. gateJoinBuildsASignature reads the seam, and an ellipsis at the
//     head of the excerpt breaks it: `…` begins no signature.
//
// Inserting that ellipsis cannot push the clause past the bound, because the
// two cases exclude each other. A cut excerpt ALREADY starts with `…`, so the
// seam is already broken and nothing is added; the marker only ever reaches an
// excerpt that was under gateFailureTailMaxBytes to begin with.
//
// It is inserted ONCE and not in a loop, and that is a limit rather than a
// proof. A marker that was itself the head of some future signature would sit
// against the excerpt exactly as the prefix does now, and a loop would prepend
// it forever instead of fixing it. `…` begins no signature today. What holds
// the invariant is a test that reads the RESULT and is derived over
// gateUnscopedSignatures — TestTheGateOutputClauseCannotBuildASignatureAtTheSeam
// — and the one shape it does not build a probe for is a signature that starts
// with the bytes of the marker itself. This paragraph is the only thing
// standing under that case; add such a signature and give it a probe.
func gateFailureTail(notes string) string {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return ""
	}
	oneLine := gateEvidenceQuote(strings.ReplaceAll(trimmed, "\n", " "))
	if len(oneLine) > gateFailureTailMaxBytes {
		b := []byte(oneLine[len(oneLine)-gateFailureTailMaxBytes:])
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[1:]
		}
		oneLine = "…" + string(b)
	}
	if gateJoinBuildsASignature(gateFailureTailPrefix, oneLine) {
		oneLine = "…" + oneLine
	}
	return gateFailureTailPrefix + oneLine
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
func emitNodeDispatchRequested(ctx context.Context, bus handlercontract.EventEmitter, clk substrate.ClockPort, runID core.RunID, nodeID core.NodeID) {
	pl := core.NodeDispatchRequestedPayload{
		RunID:       runID,
		NodeID:      nodeID,
		RequestedAt: clk.Now().UTC().Format(time.RFC3339),
		Origin:      core.NodeDispatchOriginWorkflow,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeNodeDispatchRequested, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit node_dispatch_requested: %v\n", emitErr)
	}
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
	if emitErr := bus.EmitWithRunID(ctx, payload.RunID, core.EventTypeNodeDispatchDecided, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit node_dispatch_decided: %v\n", emitErr)
	}
}

// emitDotReviewerLaunched emits reviewer_launched (§8.1a.2) for a DOT reviewer
// node.
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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerLaunched, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit reviewer_launched: %v\n", emitErr)
	}
}

// emitDotReviewerVerdict emits reviewer_verdict for a DOT reviewer node.
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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerVerdict, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit reviewer_verdict: %v\n", emitErr)
	}
}

// emitDotImplementerResumed emits implementer_resumed (§8.1a.1) before an
// implementer-resume back-edge dispatch (iterationCount >= 2), WorkflowMode
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
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerResumed, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit implementer_resumed: %v\n", emitErr)
	}
}

// newCapturedSpawnProof returns the once-guarded emitter that a SessionIDCaptured
// harness (codex, pi) invokes when it captures its session id off its own stdout.
//
// Capturing a session id IS proof the child spawned and is producing output —
// the SessionIDCaptured analogue of the Claude SessionStart hook's agent_ready.
// Emitting it disarms the stale watcher's never-spawned reaper.
//
// Without this the reaper NEVER disarms for these harnesses: agent_ready is
// emitted only by the Claude hook path, so agentReadySeen stays false for the
// whole run and the launch-stall guard silently becomes an ABSOLUTE 30-minute
// wall-clock cap on TOTAL run time (hk-47u9z). It killed a demonstrably healthy
// codex run (commit_landed=true, exit_code=0) at 30m19s and mislabelled it
// "context cancelled at node commit_gate" — the wrong node AND the wrong cause,
// which is why the defect read as "codex is unreliable" for so long.
//
// A launch that truly never produces output never reaches the capture callback,
// so the never-spawned reaper still fires for it. This guard answers "did it ever
// spawn?", not "is it still making progress?".
//
// It does NOT follow that a wedged child is covered. run_stale cannot catch one:
// RunHeartbeatLoop refreshes lastEventAt every 300s against a 600s StaleAfter,
// regardless of child liveness, and commitHardCeiling is unreachable on this path
// because pasteTarget is nil for SessionIDCaptured. A hung child therefore holds
// its slot indefinitely. That hole is PRE-EXISTING and symmetric with the claude
// path — this change makes codex no worse than claude, and closing it is hk-spqhh.
//
// sync.Once is defensive only. Both current SessionIDCaptured interceptors
// already fire their callback exactly once (codexharness.go:192,
// piharness.go:258), so removing it breaks no test today. It guards against a
// future interceptor that does not.
//
// EmitWithRunID (not Emit) is load-bearing: the stale watcher's observe() SKIPS
// any envelope whose RunID is nil, so emitting without the run id would look
// correct and do nothing — precisely the hk-wths failure shape this bug rhymes
// with. Extracted as a named function so tests exercise this exact closure
// rather than a restatement of it.
// The context is detached with WithoutCancel rather than derived directly: this
// proof fires from a spawn callback that can outlive the run context (the whole
// point is to record that the agent DID spawn, which stays true even once the
// run is cancelled), but it should still carry the caller's values instead of an
// empty root.
func newCapturedSpawnProof(ctx context.Context, tap *runloop.PerRunEventTap, runID core.RunID) func() {
	emitCtx := context.WithoutCancel(ctx)
	var once sync.Once
	return func() {
		once.Do(func() {
			//nolint:errcheck // best-effort emit; the reaper disarm is a guard, not a correctness gate (pre-RT8 idiom)
			_ = tap.EmitWithRunID(emitCtx, runID, core.EventTypeAgentReady, nil)
		})
	}
}

// killProcessGroup SIGKILLs the whole process group of proc. A negative PID
// delivers the signal to the group, not just the leader. ESRCH means the group
// already exited, which is the normal case; anything else leaves live processes
// behind, so report it. The caller's Cancel still reports success.
func killProcessGroup(proc *os.Process, label string) {
	if proc == nil {
		return
	}
	if killErr := syscall.Kill(-proc.Pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: kill %s process group %d: %v\n", label, proc.Pid, killErr)
	}
}

// writeGateLog writes the full gate output to path. The daemon log only gets a
// pointer to it, so a failed write costs the operator the whole diagnostic.
func writeGateLog(path string, combined []byte) {
	// 0600: the daemon writes it and the operator reads it, both as the same
	// user. Nothing else needs the file.
	if writeErr := os.WriteFile(path, combined, 0o600); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: write gate log %q: %v\n", path, writeErr)
	}
}

// gateLogArchiveDir is the project-scoped root under which every run's failed
// gate output is kept. It is a sibling of .harmonik/runs and .harmonik/lt-runs,
// it is gitignored with the rest of .harmonik, and NOTHING removes it — that is
// the point (hk-0kdr6).
const gateLogArchiveDir = "gate-logs"

// appendGateLogArchive appends one failed gate attempt to
// <projectDir>/.harmonik/gate-logs/<runID>/<nodeID>.log and returns the path it
// wrote. It returns "" when it wrote nothing, so the caller keeps reporting the
// worktree path in that case.
//
// Append, not truncate: one run makes several attempts at the same gate node
// (the deterministic-FAIL back-edge to implement), and a red run is diagnosed by
// comparing them. Each attempt gets a header line so the boundaries are findable.
func appendGateLogArchive(projectDir string, runID core.RunID, nodeID string, combined []byte, gateErr error) string {
	if projectDir == "" {
		return ""
	}
	dir := filepath.Join(projectDir, ".harmonik", gateLogArchiveDir, runID.String())
	if mkErr := os.MkdirAll(dir, core.HarmonikDirMode); mkErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: mkdir gate-log archive %q: %v\n", dir, mkErr)
		return ""
	}
	path := filepath.Join(dir, sanitizeGateLogName(nodeID)+".log")

	//nolint:gosec // G304: path is projectDir (harmonik config) + runID (a UUID) + a sanitized node ID.
	f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if openErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: open gate-log archive %q: %v\n", path, openErr)
		return ""
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: dot cascade: close gate-log archive %q: %v\n", path, closeErr)
		}
	}()

	header := fmt.Sprintf("\n===== gate attempt: node=%s run=%s at=%s exit=%v =====\n",
		nodeID, runID.String(), time.Now().UTC().Format(time.RFC3339), gateErr)
	if _, writeErr := f.WriteString(header); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: write gate-log archive %q: %v\n", path, writeErr)
		return ""
	}
	if _, writeErr := f.Write(combined); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: write gate-log archive %q: %v\n", path, writeErr)
		return ""
	}
	return path
}

// sanitizeGateLogName reduces a DOT node ID to a safe single filename component.
// Node IDs are author-supplied, so a name with a path separator in it would
// otherwise decide where the daemon writes.
func sanitizeGateLogName(nodeID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, nodeID)
	if safe == "" || strings.Trim(safe, ".") == "" {
		return "node"
	}
	return safe
}

// dotResolveResumeSessionID picks the session identifier that an
// implementer-resume back-edge must target, in priority order:
//
//  1. capturedID — what the harness itself reported on its stdout (a codex
//     thread_id from thread.started, a pi session id). Always preferred: for a
//     SessionIDCaptured harness it is the ONLY value that harness accepts on a
//     resume.
//  2. mintedID — shared.LaunchArtifacts.ClaudeSessionID. For claude this is the
//     live `--session-id` value, so it is a valid `--resume` target. For codex
//     and pi it is a harmonik-internal TRACKING uuid the harness never saw.
//  3. "" — a SessionIDCaptured harness that reported no id. The caller then
//     leaves the prior-session pointer nil and the back-edge launches a FRESH
//     turn, which the harness accepts.
//
// # Why rule 3 exists (hk-codex-resume-wrong-threadid-5rmtc)
//
// The cascade used to carry mintedID forward unconditionally. On a codex run
// that meant the second pass ran `codex exec resume <tracking-uuid>`, codex
// answered "no rollout found for thread id <uuid> (code -32600)", and the run
// died in about 3.5 seconds. This is the mainline path, not an edge case: a bead
// with no workflow label gets the project default graph, which has a commit
// gate, so a second pass is normal. A fresh turn re-reads agent-task.md and the
// reviewer feedback, so it loses the harness-side conversation but keeps the
// work.
func dotResolveResumeSessionID(capturedID, mintedID string, sessionIDCaptured bool) string {
	if capturedID != "" {
		return capturedID
	}
	if sessionIDCaptured {
		return ""
	}
	return mintedID
}

// readAutoStatusMarkerOrReport reads the deny-side auto-status marker. An
// unreadable marker reads as "absent", so a deny-side FAIL would pass the C2
// check unseen — report the read failure instead of treating it as clean.
func readAutoStatusMarkerOrReport(ctx context.Context, runner tmux.CommandRunner, wtPath string) *workspace.AutoStatusMarker {
	marker, markerErr := readAutoStatusMarkerVia(ctx, runner, wtPath)
	if markerErr != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: dot cascade: read auto-status marker in %q: %v (C2 deny-side check treated as absent)\n",
			wtPath, markerErr)
	}
	return marker
}
