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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// It is the graph's copy of the single-mode tail's terminal decision, and it
// keeps that decision's three cases in the same order:
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
	if socketOutcome == nil && exit.ExitCode == exitCodeClean && !watcherFailed {
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
