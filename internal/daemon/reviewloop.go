package daemon

// reviewloop.go — review-loop dispatch driver (T-WM-020).
//
// runReviewLoop executes the hardcoded implementer→reviewer cycle for a run
// with workflow_mode = review-loop per execution-model.md §4.3.EM-015d and
// §4.3.EM-015e.
//
// # Per-iteration sequence
//
// For each iteration (1..cap):
//  1. If iteration ≥ 2: emit implementer_resumed; dispatch implementer-resume.
//     If iteration = 1: dispatch implementer-initial.
//  2. Wait for implementer to complete; capture claude_session_id (iter 1 only,
//     via SessionIDInterceptor + persistClaudeSessionID + version_selected ACK).
//  3. Compute current diff hash.
//     If iteration ≥ 2 AND current_hash == last_diff_hash: emit no_progress_detected,
//     emit review_loop_cycle_complete (no_progress), terminate (needs-attention).
//  4. Store current hash as last_diff_hash.
//  5. [Iteration ≥ 2]: archive prior review.json to review.iter-{N-1}.json.
//  6. Emit reviewer_launched; dispatch reviewer.
//  7. Wait for reviewer to complete; read+validate .harmonik/review.json.
//  8. Emit reviewer_verdict; archive verdict to review.iter-N.json.
//  9. Route on verdict:
//       APPROVE               → emit cycle_complete (approved); success
//       BLOCK                 → emit cycle_complete (blocked);  fail + needs-attention
//       REQUEST_CHANGES, iter < cap → increment and loop
//       REQUEST_CHANGES, iter = cap → emit iteration_cap_hit, cycle_complete; fail + needs-attention
//
// # State threaded through iterations (Run.context keys per core/runcontextkeys.go)
//
//   - RunContextKeyIterationCount   ("iteration_count") — initialised to 1 before iter 1
//   - RunContextKeyClaudeSessionID  ("claude_session_id") — captured from handler_capabilities
//     via SessionIDInterceptor, persisted to git (CHB-023) before version_selected ACK
//   - RunContextKeyLastVerdict      ("last_verdict") — updated after each reviewer verdict
//   - RunContextKeyLastDiffHash     ("last_diff_hash") — updated before each reviewer launch
//
// Spec refs: specs/execution-model.md §4.3 EM-015d, §4.3 EM-015e.
// Related: T-WM-021 (cap enforcement), T-WM-022 (no-progress termination).
// Bead: hk-7om2q.20.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workspace"
)

// reviewLoopIterationCap is the hardcoded maximum number of iterations per
// execution-model.md §4.3.EM-015e. Not operator-tunable at MVH.
const reviewLoopIterationCap = 3

// priorVerdictSummaryMaxBytes is the maximum byte length of the
// prior_verdict_summary field in implementer_resumed events, per
// event-model.md §8.1a.1 (front-truncation to 256 UTF-8 bytes).
const priorVerdictSummaryMaxBytes = 256

// resumeReadyFallbackGrace is the fixed grace period after which the
// implementer-resume phase (iteration ≥ 2) under the tmux substrate synthesizes
// its own agent_ready when no relay-driven agent_ready has arrived.
//
// Root cause (hk-isq02): iteration 1 launches `claude --session-id <uuid>`, a
// FRESH session that fires a SessionStart hook; the hook-relay synthesizes
// agent_ready on first SessionStart receipt (hookrelay.go buildSessionStartMessage),
// which is the daemon's sole ready signal under the tmux substrate (implWatcher
// is nil there, so there is no progress-stream watcher to cancel the wait).
// Iteration ≥ 2 launches `claude --resume <uuid>`, which REATTACHES to an
// already-persisted session and does NOT re-fire a SessionStart hook the way a
// fresh `--session-id` launch does. The relay therefore never sends agent_ready,
// waitAgentReady never observes it, and the configured timeout fires →
// `implementer agent_ready_timeout at iteration 2`. The twin-claude handler
// masks this because it self-emits agent_ready directly on every phase.
//
// A `--resume` reattach still renders the welcome splash before the REPL is
// input-ready, so the fallback retains a splash-dismiss-class grace rather than
// synthesizing ready immediately; this preserves the hk-kunm4 "do not paste
// before the REPL accepts input" invariant. If a real relay agent_ready DOES
// arrive first (a newer claude that fires SessionStart on resume), waitAgentReady
// completes on that signal and the fallback emit is a harmless no-op (the tap
// channel is buffered and the wait has already returned).
//
// Bead: hk-isq02.
const resumeReadyFallbackGrace = 2 * time.Second

// reviewLoopResult is the terminal outcome produced by runReviewLoop.
// The caller (workloop.go) uses this to drive the bead close call and
// run_completed / run_failed event emission.
type reviewLoopResult struct {
	// success is true when the cycle terminated via APPROVE (run_completed).
	// false for all other termination paths (run_failed).
	success bool

	// completionReason is the completion_reason value for review_loop_cycle_complete.
	completionReason core.ReviewLoopCompletionReason

	// summary is a short human-readable explanation for run_completed/run_failed.
	summary string

	// needsAttention controls whether CloseBead applies the needs-attention label.
	needsAttention bool

	// approveVerdict carries the APPROVE verdict for merge commit trailer injection
	// (hk-dyim). Non-nil only when success=true; nil on all failure paths.
	approveVerdict *workspace.ReviewVerdict
}

// reviewLoopState carries mutable per-cycle context threaded through iterations.
type reviewLoopState struct {
	// iterationCount is the 1-based current iteration (initialised to 1).
	// Incremented before each implementer dispatch after the first.
	iterationCount int

	// claudeSessionID is the Claude Code session identifier captured from the
	// initial implementer launch per EM-015d.
	claudeSessionID string

	// lastVerdictNotes is the notes from the most recent reviewer verdict,
	// used as prior_verdict_summary (truncated) in implementer_resumed events.
	lastVerdictNotes string

	// lastDiffHash is the SHA-256 hex digest of `git diff <parent>..<head>`
	// computed after the most recent implementer run (just before the reviewer
	// launches). Empty before iteration 1's reviewer.
	//
	// hk-togxq: retained only for the no_progress_detected event payload
	// (diff_hash_current / diff_hash_prior, an observability surface). The
	// progress signal is now HEAD advancement (lastIterHeadSHA), NOT diff-hash
	// equality, so a new commit whose net parent..HEAD diff collides with a prior
	// commit is no longer false-flagged as no-progress.
	lastDiffHash string

	// lastIterHeadSHA is the worktree HEAD recorded just before the prior
	// iteration's reviewer launch. The no-progress check (iteration ≥ 2) fires
	// only when the current HEAD equals this — i.e. the implementer-resume made
	// no new commit in response to the prior REQUEST_CHANGES. (Reaching iteration
	// ≥ 2 in the review-loop is itself proof the prior verdict was
	// REQUEST_CHANGES; APPROVE/BLOCK return before incrementing.) Empty before
	// iteration 1's reviewer. hk-togxq.
	lastIterHeadSHA string

	// priorVerdicts accumulates per-iteration verdict summaries for the
	// `## Prior verdicts` section of review-target.md (EM-015d-RIA). Empty for
	// iteration 1's reviewer; appended after each verdict is read. Bead: hk-0xmwq.
	priorVerdicts []workspace.ReviewTargetPriorVerdict

	// lastVerdictFlags is the flags slice from the most recent REQUEST_CHANGES
	// verdict. Set in the REQUEST_CHANGES routing branch; read by the no-progress
	// check to populate review_fixup_stalled. Non-nil after any REQUEST_CHANGES
	// verdict (MAY be an empty slice when the reviewer emitted no flags).
	// Bead ref: hk-m1wqp.
	lastVerdictFlags []string
}

// runReviewLoop executes the review-loop dispatch cycle for a single bead run.
//
// Parameters:
//   - ctx             — caller context; cancellation propagates into all sub-calls.
//   - deps            — work-loop dependency bundle.
//   - runID           — the run's stable identifier (used for event scoping via EmitWithRunID).
//   - beadID          — the bead being executed (reserved for future logging; bead transitions
//     are owned by runWorkLoop after runReviewLoop returns).
//   - beadTitle       — bead title from the Beads ledger; threaded into CHB-028 agent-task.md.
//   - beadDescription — bead body from the Beads ledger; threaded into CHB-028 agent-task.md.
//   - wtPath          — absolute path of the git worktree created for this run.
//   - parentSHA       — the git commit SHA at which the worktree was created; used as the
//     <parent> argument for diff-hash computation per EM-015e.
//
// Returns a reviewLoopResult describing the terminal outcome. A context cancellation
// mid-cycle is absorbed into the result (error path) rather than returned as an error,
// unless cancellation occurs before any work begins.
func runReviewLoop(
	ctx context.Context,
	deps workLoopDeps,
	runID core.RunID,
	beadID core.BeadID,
	beadTitle string,
	beadDescription string,
	wtPath string,
	parentSHA string,
	resolvedModel string,
	resolvedEffort string,
	extraContext string, // hk-boiwe: per-item context injected into agent-task.md
	baseBranch string, // hk-mtm0w: resolved lands_on for pre-exit rebase step
	// runner is the per-run CommandRunner. For a REMOTE (SSH worker) run it is the
	// worker's SSHRunner; for a LOCAL run it is nil. When non-nil, the
	// implementer-worktree HEAD/diff probes route to the WORKER (where the
	// implementer's commits live), and the run branch is pushed worker→origin and
	// fetched origin→box-A before each reviewer launch so the reviewer's own
	// box-A worktree (CreateReviewerWorktree, always box-A-local) can materialise
	// at the implementer's committed SHA. nil ⇒ byte-identical local path (NFR7).
	runner tmux.CommandRunner,
) reviewLoopResult {
	// daemonSocket is the UNIX-domain socket path for the hook-relay per design §7.
	// Derived from projectDir so reviewloop.go does not need a separate field on deps.
	daemonSocket := filepath.Join(deps.projectDir, ".harmonik", "daemon.sock")

	state := reviewLoopState{iterationCount: 1}

	for {
		// ── Dispatch implementer ──────────────────────────────────────────
		if state.iterationCount >= 2 {
			// Iteration ≥ 2: emit implementer_resumed BEFORE dispatch per EM-015d.
			priorSummary := rlTruncateUTF8(state.lastVerdictNotes, priorVerdictSummaryMaxBytes)
			emitImplementerResumed(ctx, deps.bus, runID, state.claudeSessionID, state.iterationCount, priorSummary)
		}

		// Build the implementer LaunchSpec via the routed spec builder (T12, hk-xhawy).
		// deps.launchSpecBuilder is pre-built by beadRunOne (workloop.go) using the
		// harness registry + bead labels; it routes to the correct harness (claude or
		// codex) and populates artifacts.resolvedAgentType. Falls back to
		// buildClaudeLaunchSpec when nil (legacy test fixtures).
		//
		// Iteration 1: phase=implementer-initial, priorClaudeSessID=nil.
		// Iteration ≥ 2: phase=implementer-resume, priorClaudeSessID=&prior.
		//
		// The CHB-023 StdoutWrapper (SessionIDInterceptor) is preserved on
		// iteration 1 so the daemon can extract claude_session_id from
		// handler_capabilities and persist it to git before the version_selected ACK.
		var implPhase handlercontract.ReviewLoopPhase
		var implPrior *string
		if state.iterationCount == 1 {
			implPhase = handlercontract.ReviewLoopPhaseImplementerInitial
			// implPrior = nil — no prior session on first launch.
		} else {
			implPhase = handlercontract.ReviewLoopPhaseImplementerResume
			prior := state.claudeSessionID
			implPrior = &prior
		}

		implRC := claudeRunCtx{
			runID:             runID,
			beadID:            string(beadID),
			workspacePath:     wtPath,
			daemonSocket:      daemonSocket,
			workflowMode:      core.WorkflowModeReviewLoop,
			phase:             implPhase,
			iterationCount:    state.iterationCount,
			priorClaudeSessID: implPrior,
			handlerBinary:     deps.handlerBinary,
			daemonBinaryPath:  deps.daemonBinaryPath,
			baseEnv:           deps.handlerEnv,
			beadTitle:         beadTitle,
			beadDescription:   beadDescription,
			model:             resolvedModel,
			effort:            resolvedEffort,
			// worktreeRootPath enables --dangerously-skip-permissions for daemon-managed
			// worktrees per HC-055b.
			worktreeRootPath: workspace.WorktreeRootPath(deps.projectDir, workspace.NoWorktreeRootOverride()),
			// priorVerdictFile and priorVerdictSummary are populated below for
			// implementer-resume phases (iteration ≥ 2) once state.lastVerdictNotes is known.
			extraContext: extraContext, // hk-boiwe
			baseBranch:   baseBranch,   // hk-mtm0w: pre-exit rebase target
		}
		implSpecBuilder := deps.launchSpecBuilder
		if implSpecBuilder == nil {
			implSpecBuilder = buildClaudeLaunchSpec
		}
		implSpec, implArtifacts, implSpecErr := implSpecBuilder(ctx, implRC)
		if implSpecErr != nil {
			result := rlErrorResult(fmt.Sprintf("implementer spec error at iteration %d: %v", state.iterationCount, implSpecErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		// Attach the optional tmux substrate (nil at MVH; set from deps.substrate).
		// REQUIRED: without this, h.Launch takes the exec.CommandContext path and
		// SpawnWindow is never called; pasteInjectOnLaunch then fails with
		// "no window spawned yet". This is the root cause of the pane-race bug
		// (hk-2hb2y).
		//
		// hk-012af: wrap deps.substrate in a perRunSubstrate so this review-loop
		// iteration gets an isolated pane handle. Under MaxConcurrent>1, two
		// concurrent review-loop goroutines would otherwise race on shared
		// paste-inject state, sending paste-inject to the wrong pane.
		// (hk-jfh59: shared-state methods on tmuxSubstrate removed.)
		//
		// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
		implPRS := newPerRunSubstrate(deps.substrate, deps.handlerBinary, nil)
		var implSubstrate handler.Substrate = deps.substrate
		var implPasteTarget handler.Substrate = deps.substrate
		if implPRS != nil {
			implSubstrate = implPRS
			implPasteTarget = implPRS
		}
		implSpec.Substrate = implSubstrate

		// hk-mzgh: For SessionIDCaptured harnesses (codex): force the exec path so
		// that stdout is exposed as an io.Reader and StdoutWrapper can fire. The tmux
		// substrate returns Stdout()==nil (handler.launchViaSubstrate returns early),
		// so StdoutWrapper is never called and thread_id is never captured. Clearing
		// Substrate selects exec.CommandContext, which wires a real stdout pipe.
		// Paste injection and waitAgentReady are already gated on CompletionProcessExit
		// so they are safe to skip without a tmux pane.
		implIsSessionIDCaptured := false
		if deps.harnessRegistry != nil {
			if implH, implHErr := deps.harnessRegistry.ForAgent(artifactAgentType(implArtifacts)); implHErr == nil {
				implIsSessionIDCaptured = implH.SessionIDPolicy() == handlercontract.SessionIDCaptured
			}
		}
		if implIsSessionIDCaptured {
			implSpec.Substrate = nil
		}

		// Prepend deps.handlerArgs so test handlers (e.g. /bin/sh scriptPath) are invoked
		// correctly. For production (claude binary, empty handlerArgs) this is a no-op.
		// The session-id / resume flags from buildClaudeLaunchSpec follow the script path.
		if len(deps.handlerArgs) > 0 {
			implSpec.Args = append(deps.handlerArgs, implSpec.Args...)
		}

		// Register this phase's hook session so stop-hook outcomes are routed
		// correctly (CHB-025). Closed after waitWithSocketGrace returns so late
		// hooks from a completed implementer don't bleed into the reviewer.
		if deps.hookStore != nil {
			deps.hookStore.RegisterHookSession(runID.String(), implArtifacts.claudeSessionID)
		}

		// For the initial implementer launch (iteration 1): wire a
		// SessionIDInterceptor on the progress stream so the daemon can extract
		// claude_session_id from handler_capabilities, persist it to git (CHB-023),
		// and release the version_selected ACK before the handler execs Claude.
		//
		// sessionIDFromCapabilities is filled by the persist goroutine and read
		// after implWatcher.Done(). It is buffered (capacity 1) so the goroutine
		// never blocks even if no one reads the channel.
		sessionIDFromCapabilities := make(chan string, 1)

		// contextCommitSHACh carries the SHA of the CHB-023 checkpoint commit (or the
		// current HEAD when the persist was skipped due to idempotency). The no-commit
		// guard uses this to distinguish "only the context-persist commit exists" from
		// "the implementer made real code commits". Without it the guard compares against
		// parentSHA and misses the CHB-023 commit, falsely treating the run as having
		// made progress when the implementer exited without committing any code.
		contextCommitSHACh := make(chan string, 1)

		// Capture implSess so the ACK goroutine below can use it.
		// The variable is set after Launch; the goroutine uses it only after the
		// interceptor fires (which happens after Launch returns and the Watcher
		// starts reading), so the ordering is safe.
		var implSess handler.Session

		if state.iterationCount == 1 {
			if implIsSessionIDCaptured {
				// hk-mzgh: Codex path — capture thread_id from the first thread.started
				// JSONL event on the exec-path stdout pipe. No CHB-023 checkpoint commit
				// (codex uses no context persist) and no version_selected ACK (codex
				// communicates only via argv/JSONL, not the hook-bridge handshake).
				capturedSessionIDCh := sessionIDFromCapabilities
				implSpec.StdoutWrapper = func(r io.Reader) io.Reader {
					return newCodexThreadIDInterceptor(r, func(threadID string) {
						capturedSessionIDCh <- threadID
					})
				}
			} else {
				// Claude path: wire a SessionIDInterceptor on the progress stream so the
				// daemon can extract claude_session_id from handler_capabilities, persist
				// it to git (CHB-023), and release the version_selected ACK before the
				// handler execs Claude.
				//
				// Capture loop variables for the closure.
				capturedWtPath := wtPath
				capturedRunID := runID
				capturedBus := deps.bus
				capturedCtx := ctx
				capturedContextCommitSHACh := contextCommitSHACh

				implSpec.StdoutWrapper = func(r io.Reader) io.Reader {
					return newSessionIDInterceptor(r, func(id string) {
						// Fired on the Watcher's goroutine (inside Read).
						// Spawn a goroutine to persist + ACK so the Watcher is not blocked.
						//
						// CHB-023 ordering: git commit → transition_event → ACK.
						//
						// REMOTE-RUN NOTE (FLAGGED follow-up): persistClaudeSessionID
						// commits into capturedWtPath via a box-A-local git/os.WriteFile.
						// For a remote run capturedWtPath is on the worker, so the persist
						// fails and the goroutine signals empty → the loop falls back to
						// session-id synthesis (non-fatal; iteration 1 still proceeds, and
						// the version_selected ACK is still sent below). The CHB-023
						// context-commit baseline is not written on the worker; a
						// worker-routed persistClaudeSessionIDVia is part of the
						// multi-iteration remote follow-up bundle.
						go func() {
							res, persistErr := persistClaudeSessionID(capturedCtx, capturedWtPath, capturedRunID, id)
							if persistErr != nil {
								fmt.Fprintf(os.Stderr,
									"daemon: reviewloop: persist claude_session_id: %v (continuing without persistence)\n", persistErr)
								// Signal empty so the review loop falls back to synthesis.
								sessionIDFromCapabilities <- ""
								capturedContextCommitSHACh <- ""
								return
							}
							if !res.Skipped {
								// EM-025a: emit transition_event AFTER git commit.
								emitClaudeSessionIDPersisted(capturedCtx, capturedBus, capturedRunID, res.CommitSHA, id)
								capturedContextCommitSHACh <- res.CommitSHA
							} else {
								// Idempotent: CHB-023 commit already on the branch (daemon-restart +
								// resume). Resolve the current HEAD so the no-commit guard has the
								// correct post-context-commit baseline.
								sha, resolveErr := resolveWorktreeHEAD(capturedCtx, capturedWtPath)
								if resolveErr != nil {
									sha = ""
								}
								capturedContextCommitSHACh <- sha
							}
							// CHB-023: ACK (version_selected) AFTER the git commit.
							// implSess is read from the outer variable; this goroutine
							// runs only after Launch returns (the Watcher starts after
							// Launch), so implSess is already set.
							if ackErr := sendVersionSelectedACK(capturedCtx, implSess); ackErr != nil {
								fmt.Fprintf(os.Stderr,
									"daemon: reviewloop: sendVersionSelectedACK: %v (non-fatal)\n", ackErr)
							}
							sessionIDFromCapabilities <- id
						}()
					})
				}
			}
		}

		// hk-fra5l: emit pre-exec messages (handler_capabilities →
		// session_log_location → skills_provisioned) on the bus BEFORE Launch,
		// mirroring the single-mode path (workloop.go step 3).  Without this the
		// review-loop path emitted run_started but no pre-exec lifecycle events,
		// making the hk-j7o3i incident undetectable: operators saw only
		// run_started and run_stale with no intervening lifecycle events.
		//
		// hk-4l7zs: launch_initiated is held back and emitted AFTER Launch returns
		// so it signals a window actually spawned, not merely that the daemon is
		// about to try (which misleads operators when SpawnWindow is wedged on a
		// leaked spawn slot).
		implLaunchInitiatedMsg := emitPreExecBeforeLaunch(ctx, deps.bus, runID, implArtifacts.preExecMsgs)

		// Create a per-run tapping emitter so waitAgentReady can observe
		// watcher events from the implementer launch without a post-seal bus
		// subscription (EV-009). A new handler is constructed using the tap so
		// events flow through the channel — same pattern as the reviewer phase
		// (lines ~592-598) and single-mode beadRunOne (workloop.go lines 1173-1176).
		// Precondition: deps.adapterRegistry must be non-nil (enforced by
		// newWorkLoopDeps). NewHandler panics on a nil registry (hk-d8u1y).
		//
		// Bead ref: hk-kunm4.
		implTap, implTapCh := newPerRunEventTap(deps.bus, runID)
		implRunH := handler.NewHandler(implTap, handlercontract.NoopWatcherDeadLetter{}, deps.adapterRegistry)

		var implWatcher *handlercontract.Watcher
		var implLaunchErr error
		implLaunchedAt := time.Now()
		implSess, implWatcher, implLaunchErr = implRunH.Launch(ctx, implSpec)
		if implLaunchErr != nil {
			if deps.hookStore != nil {
				deps.hookStore.CloseHookSession(runID.String(), implArtifacts.claudeSessionID)
			}
			// hk-4l7zs: surface spawn-cap saturation (slot-leak signature) as a
			// dedicated spawn_cap_blocked event when the implementer launch is
			// wedged on the spawn semaphore.
			if errors.Is(implLaunchErr, ErrSpawnCapTimeout) {
				inUse, capSize := substrateSpawnStats(implSubstrate)
				emitSpawnCapBlocked(ctx, deps.bus, runID, time.Since(implLaunchedAt), inUse, capSize)
			}
			// hk-r1rup: surface a hung `tmux new-window` (the no-spawn wedge) as a
			// dedicated tmux_new_window_timeout event when the implementer launch is
			// wedged on the new-window call.
			if errors.Is(implLaunchErr, ErrTmuxNewWindowTimeout) {
				emitTmuxNewWindowTimeout(ctx, deps.bus, runID, time.Since(implLaunchedAt))
			}
			result := rlErrorResult(fmt.Sprintf("implementer launch error at iteration %d: %v", state.iterationCount, implLaunchErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		// hk-4l7zs: emit the held-back launch_initiated now that the implementer
		// window has actually spawned (Launch returned a live session). On the
		// wedged-spawn path Launch returns an error above and launch_initiated is
		// never emitted, so the event stays truthful.
		if implLaunchInitiatedMsg != nil {
			emitPreExecMessage(ctx, deps.bus, runID, implLaunchInitiatedMsg)
		}
		// hk-nvjk: start the CHB-019 heartbeat goroutine so the stale watcher
		// receives agent_heartbeat events (with run_id) after launch_initiated.
		// Without this, lastEventType stays frozen at "launch_initiated" for the
		// full run duration, causing false-positive run_stale on every review-loop
		// dispatch. Mirrors the single-mode path (workloop.go Step 5). The defer
		// accumulates per iteration (same pattern as implSessForTeardown below);
		// the goroutine also exits on ctx cancellation.
		implHBDone := make(chan struct{})
		go handler.RunHeartbeatLoop(ctx, implArtifacts.handlerSessionID,
			handler.HeartbeatInterval, implHBDone,
			newDaemonHeartbeatEmitter(implTap, runID))
		defer close(implHBDone)

		// hk-68pvl / hk-4l7zs: release this iteration's implementer session
		// PROMPTLY at the end of the iteration rather than only at runReviewLoop
		// return. The per-phase Kill (waitWithSocketGrace branch, ~line 533) is the
		// normal release point; this is a backstop for early-return paths. Kill is
		// idempotent, so registering it via a per-iteration LIFO defer (below) and
		// also calling it eagerly at iteration end (release-before-reacquire for the
		// next iteration's implementer) holds at most ONE spawn slot per phase.
		//
		// Why the defer alone over-holds (the original hk-4l7zs leak path): each
		// iteration's `defer forceTeardownSession` fires only when runReviewLoop
		// RETURNS, so a 3-iteration review loop accumulates up to 3 implementer +
		// 3 reviewer deferred teardowns. While the per-phase Kills already release
		// the slots on the tmux happy path, any error/early-return branch BETWEEN
		// the impl Kill (line ~533) and the reviewer Kill (line ~970) would hold
		// both this iteration's slots until function return. The explicit
		// per-iteration release below bounds the worst case to one live session.
		implSessForTeardown := implSess
		defer forceTeardownSession(implSessForTeardown)

		// Wire the implementer's agent-ready callback into implTap so that
		// relay-synthesized agent_ready envelopes from the hook-relay subprocess
		// reach implTapCh, which waitAgentReady (below) blocks on. Without this
		// call notifyAgentReady finds a nil callback and implTapCh stays empty,
		// causing HC-056 to fire every 30s.
		//
		// Same wiring as reviewer phase and single-mode beadRunOne
		// (workloop.go lines 1222-1242). Bead ref: hk-lj1p9.4, hk-kunm4.
		//
		// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-013; specs/handler-contract.md §4.9 HC-056.
		if deps.hookStore != nil {
			capturedImplTap := implTap
			deps.hookStore.SetAgentReadyCallback(runID.String(), implArtifacts.claudeSessionID, func() {
				_ = capturedImplTap.Emit(context.Background(), core.EventTypeAgentReady, nil)
			})
		}

		// hk-isq02: resume-phase agent_ready fallback.
		//
		// On iteration ≥ 2 the implementer is launched with `claude --resume <uuid>`
		// (see buildClaudeLaunchSpec / MintClaudeSessionID ResumeMode). Under the
		// tmux substrate (implWatcher == nil) the daemon's ONLY ready signal is the
		// relay-synthesized agent_ready, which the relay produces solely on a
		// SessionStart hook receipt. A `--resume` reattach does not reliably re-fire
		// SessionStart, so the relay never sends agent_ready and waitAgentReady below
		// would block until ErrAgentReadyTimeout — the hk-isq02 fix-up-cycle failure.
		//
		// Arm a fallback: after resumeReadyFallbackGrace (splash-dismiss-class), emit
		// agent_ready into implTap ourselves so waitAgentReady completes. The grace
		// preserves the hk-kunm4 invariant (do not paste before the REPL is input-
		// ready). The goroutine is bounded by resumeReadyFallbackCtx, cancelled right
		// after waitAgentReady returns, so a genuine relay agent_ready (newer claude)
		// still wins and the fallback emit never fires. Gating on implWatcher == nil
		// keeps the exec.CommandContext / single-mode path (watcher non-nil) untouched.
		resumeReadyFallbackCtx, resumeReadyFallbackCancel := context.WithCancel(ctx)
		// Defer guarantees the cancel runs on every return path (go vet lostcancel);
		// the eager cancel after waitAgentReady (below) stops the goroutine promptly.
		// The defer accumulates per iteration, bounded by reviewLoopIterationCap.
		defer resumeReadyFallbackCancel()
		if state.iterationCount >= 2 && implWatcher == nil {
			capturedImplTap := implTap
			go func() {
				select {
				case <-time.After(resumeReadyFallbackGrace):
					_ = capturedImplTap.Emit(context.Background(), core.EventTypeAgentReady, nil)
				case <-resumeReadyFallbackCtx.Done():
				}
			}()
		}

		// HC-056: waitAgentReady — implementer phase must observe agent_ready
		// within the configured timeout before paste-injecting the task.
		// Without this gate, ~60% of concurrent dispatches land the paste before
		// Claude's REPL is input-ready, resulting in empty panes (hk-kunm4).
		//
		// Pattern mirrors reviewer phase and single-mode beadRunOne
		// (workloop.go lines 1265-1337).
		// hk-a2okh: hang-detector state, set inside the else branch when
		// agent_ready is observed on the exec path.
		var implHangDetectedCh <-chan struct{}
		var implHangCancel context.CancelFunc = func() {}

		// hk-zlo8: resolve implCompletionMode before the adapter check so it is
		// accessible at the paste-inject gate below (pasteInjectOnLaunch +
		// pasteInjectQuitOnCommit must be skipped for ProcessExit harnesses —
		// same class as hk-f6g7).
		implCompletionMode := handlercontract.CompletionEventStreamThenQuit
		if deps.harnessRegistry != nil {
			if h, hErr := deps.harnessRegistry.ForAgent(artifactAgentType(implArtifacts)); hErr == nil {
				implCompletionMode = h.Completion()
			}
		}

		implAdapter, implAdapterErr := deps.adapterRegistry.ForAgent(artifactAgentType(implArtifacts))
		if implAdapterErr != nil {
			// No adapter for the resolved agent type — non-fatal; skip ready-wait.
			fmt.Fprintf(os.Stderr, "daemon: reviewloop: ForAgent(%s) implementer bead %s iter %d: %v (skipping ready-wait)\n",
				artifactAgentType(implArtifacts), beadID, state.iterationCount, implAdapterErr)
		} else {
			// hk-f6g7: skip waitAgentReady for ProcessExit harnesses (codex). These
			// self-terminate on turn completion and never emit agent_ready; calling
			// waitAgentReady unconditionally caused HC-056 timeout in all workflow modes.
			// Spec: specs/harness-contract.md §2 N5.
			// implCompletionMode was resolved above (hk-zlo8) and is used here directly.
			if implCompletionMode != handlercontract.CompletionProcessExit {
				// Derive a child context that cancels when the implementer watcher
				// finishes (handler exit), preventing a full-timeout block on crash.
				//
				// Substrate path: implWatcher is nil when deps.substrate != nil
				// (tmux-hosted sessions return nil from handler.launchViaSubstrate).
				// Skip the watcher-done goroutine in that case — readyCtx is still
				// valid and will be cancelled by the outer ctx or readyCancel below.
				// Bead ref: hk-yjduq.
				implReadyCtx, implReadyCancel := context.WithCancel(ctx)
				if implWatcher != nil {
					go func() {
						select {
						case <-implWatcher.Done():
							implReadyCancel()
						case <-implReadyCtx.Done():
						}
					}()
				}

				implEventSrc := newChanAgentEventSource(implTapCh)
				implReadyErr := waitAgentReady(implReadyCtx, runID, implEventSrc, implAdapter, deps.agentReadyTimeout)
				implReadyCancel() // always release the watcher-done goroutine above
				// hk-isq02: the ready-wait has returned — stop the resume-phase fallback
				// goroutine promptly so it does not emit a stale agent_ready after we have
				// moved on to paste-inject.
				resumeReadyFallbackCancel()

				if implReadyErr == ErrAgentReadyTimeout {
					// HC-056: implementer agent_ready_timeout — kill, reap, error result.
					fmt.Fprintf(os.Stderr, "daemon: reviewloop: waitAgentReady implementer bead %s iter %d run %s: %v (error)\n",
						beadID, state.iterationCount, runID.String(), implReadyErr)
					_ = implSess.Kill(ctx)
					if implWatcher != nil {
						select {
						case <-implWatcher.Done():
						case <-time.After(agentReadyKillReapTimeout):
							fmt.Fprintf(os.Stderr, "daemon: reviewloop: implWatcher.Done() reap timed out bead %s iter %d run %s after Kill — continuing\n",
								beadID, state.iterationCount, runID.String())
						}
					}
					_ = implSess.Wait(ctx)
					if deps.hookStore != nil {
						deps.hookStore.CloseHookSession(runID.String(), implArtifacts.claudeSessionID)
					}
					emitAgentReadyTimeout(ctx, deps.bus, runID, implArtifacts.claudeSessionID, deps.agentReadyTimeout)
					result := rlErrorResult(fmt.Sprintf("implementer agent_ready_timeout at iteration %d", state.iterationCount))
					emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
					return result
				}
				// implReadyErr == nil (agent_ready observed) OR context.Canceled
				// (watcher exited first or ctx cancelled). Fall through to paste-inject.

				// hk-a2okh: post-agent_ready hang detector — exec path only.
				// If agent_ready was observed (not just a context cancel) and we have a
				// watcher (exec path), subscribe to implTap AFTER agent_ready to watch
				// for the next event.  If none arrives within postAgentReadyHangTimeout
				// the session is declared hung and we fail fast.
				//
				// tmux path (implWatcher == nil) is intentionally excluded: the only
				// post-ready signal there would be unconditional daemon heartbeats which
				// cannot distinguish a hung agent from a working one.
				if implWatcher != nil && implReadyErr == nil {
					implHangCh := make(chan struct{})
					implHangDetectedCh = implHangCh
					hangCtx, cancelFn := context.WithCancel(ctx)
					implHangCancel = cancelFn
					postReadyCh := implTap.Subscribe()
					go func() {
						defer cancelFn()
						if err := waitPostAgentReadyProgress(hangCtx, postReadyCh, deps.postAgentReadyHangTimeout); errors.Is(err, ErrPostAgentReadyHang) {
							close(implHangCh)
							_ = implSess.Kill(hangCtx)
						}
					}()
				}
			} else {
				// CompletionProcessExit: task delivered via argv; no agent_ready handshake.
				// Cancel the resume-phase fallback goroutine — it is not needed here.
				resumeReadyFallbackCancel()
			}
		}

		// Paste-inject: only for interactive TUI harnesses (not ProcessExit).
		// hk-zlo8: CodexHarness (CompletionProcessExit) has no tmux pane; calling
		// pasteInjectOnLaunch causes "WriteLastPane: cant find pane" → no_commit in ~4s.
		// ProcessExit harnesses receive their task via argv (launch spec), not pane paste.
		// Mirrors the existing hk-f6g7 gate above for waitAgentReady.
		if implCompletionMode != handlercontract.CompletionProcessExit {
			// pasteInjectOnLaunch is a no-op when deps.substrate does not implement
			// pasteInjecter (exec.CommandContext path, test fixtures). Non-fatal.
			// Returns briefDelivered, passed to pasteInjectQuitOnCommit (hk-930o3).
			//
			// MUST run AFTER waitAgentReady returns (hk-kunm4): when paste-inject fires
			// before agent_ready, the trailing \n is consumed by Claude Code's welcome-splash
			// render before the REPL input state is active; the buffered text sits in the
			// input bar unsubmitted, claude never reads agent-task.md, and the run hangs.
			//
			// Spec ref: specs/process-lifecycle.md §4.7 PL-021d; specs/claude-hook-bridge.md §4.11 CHB-028.
			// Bead ref: hk-lj1p9.4, hk-zrj83, hk-930o3, hk-kunm4.
			implBriefDelivered := pasteInjectOnLaunch(ctx, implPasteTarget, implArtifacts.claudeSessionID,
				implPhase, state.iterationCount, wtPath, deps.bus, runID)

			// Quit-on-commit: after the implementer's task commit lands in the worktree,
			// send `/quit Enter` to trigger Stop hook → outcome_emitted → workloop unblocked.
			// The initial HEAD for this iteration is the current worktree HEAD at launch time.
			// Non-fatal: only fires when substrate implements quitSender (tmux path).
			//
			// hk-012af: use implPasteTarget (per-run wrapper) so /quit targets this
			// iteration's pane, not the shared "last pane".
			// hk-930o3: implBriefDelivered gates commit-polling until the brief paste lands.
			//
			// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
			// Beads: hk-cmybm, hk-930o3.
			if qs, ok := implPasteTarget.(quitSender); ok {
				// REMOTE: wtPath is on the worker; resolve its HEAD via the worker runner
				// (resolveWorktreeHEADVia delegates to the local probe when runner is nil).
				implInitialSHA, resolveErr := resolveWorktreeHEADVia(ctx, runner, wtPath)
				if resolveErr != nil {
					implInitialSHA = parentSHA // fallback to known-good parent SHA
				}
				// Pass implSess as the killer so commitPollTimeout forces an exit;
				// nil noChangeTimeoutCh — the reviewloop handles outcomes differently.
				// hk-x78n: subscribe to implTap BEFORE launching the goroutine so no
				// heartbeats are missed. Each tap.Subscribe() returns an independent
				// fan-out channel (no competing-consumer race with implTapCh or the
				// post-ready hang-detector). This makes the implementer-wait budget
				// heartbeat-extended (matching the reviewer-side hk-60t8 pattern):
				// each agent_heartbeat extends totalDeadline by commitPollTimeout;
				// only a genuine heartbeat-stall or commitHardCeiling kills.
				implHBCh := implTap.Subscribe()
				go pasteInjectQuitOnCommit(ctx, qs, implSess, wtPath, implInitialSHA, nil, implBriefDelivered, implHBCh, deps.bus, runID)
			}
		} // end hk-zlo8 ProcessExit gate

		// Wait for implementer using waitWithSocketGrace (OQ2 resolution: stop hook wins).
		// This replaces the bare <-watcher.Done() + sess.Wait() pattern.
		_, implEI := waitWithSocketGrace(ctx, deps.hookStore, implWatcher, implSess,
			runID.String(), implArtifacts.claudeSessionID)
		// implEI carries exit code + stderr tail; surfaced into the no-commit
		// failure summary below (hk-loga9, extends hk-ajhqw's single-mode fix).

		// hk-e6mtt: destroy the implementer tmux window after the session completes.
		// Mirrors the single-mode fix in workloop.go. Guarded by implWatcher==nil (tmux path).
		if implWatcher == nil {
			_ = implSess.Kill(context.Background())
		}

		// Emit implementer_phase_complete (hk-cd8yu) immediately after the
		// implementer session ends and before any reviewer phase begins.
		// commitLanded is true when the worktree HEAD has advanced past
		// parentSHA; HEAD resolution errors are treated as "not landed".
		{
			curHead, _ := resolveWorktreeHEADVia(ctx, runner, wtPath)
			commitLanded := curHead != "" && curHead != parentSHA
			emitImplementerPhaseComplete(ctx, deps.bus, runID, implEI.exitCode,
				implEI.stderrTail, commitLanded, time.Since(implLaunchedAt))
		}

		// Close this phase's hook session — late hooks from a completed implementer
		// must not bleed into the next phase (reviewer or implementer-resume).
		if deps.hookStore != nil {
			deps.hookStore.CloseHookSession(runID.String(), implArtifacts.claudeSessionID)
		}

		// hk-a2okh: stop the hang-detector goroutine promptly (idempotent).
		implHangCancel()

		// hk-a2okh: check whether the hang detector fired.
		if implHangDetectedCh != nil {
			select {
			case <-implHangDetectedCh:
				emitPostAgentReadyHang(ctx, deps.bus, runID, implArtifacts.claudeSessionID,
					deps.postAgentReadyHangTimeout, state.iterationCount, string(implPhase))
				result := rlErrorResult(fmt.Sprintf("post_agent_ready_hang: implementer made no observable progress within %v at iteration %d",
					deps.postAgentReadyHangTimeout, state.iterationCount))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			default:
			}
		}

		if ctx.Err() != nil {
			return reviewLoopResult{
				success:          false,
				completionReason: core.ReviewLoopCompletionReasonError,
				summary:          "context cancelled during implementer wait",
				needsAttention:   false,
			}
		}

		// ── No-commit guard (hk-9c1v4) ────────────────────────────────────
		//
		// If the implementer phase exits without advancing the worktree HEAD
		// past parentSHA, there is nothing for the reviewer to review.
		// Previously this case fell through to diff-hash + reviewer dispatch,
		// emitting reviewer_launched with a synthetic claude_session_id and
		// crashing the reviewer with "task file absent: review-target.md".
		//
		// Per EM-015d (implementer phase MUST advance HEAD before the daemon
		// launches the reviewer): short-circuit with a failed run when HEAD ==
		// parentSHA on iteration 1. On iteration ≥ 2 the existing
		// no_progress_detected check handles the analogous case (HEAD did not
		// advance from the prior iteration's HEAD).
		//
		// Bead: hk-9c1v4.
		//
		// CHB-023 baseline: the CHB-023 checkpoint commit advances HEAD past
		// parentSHA before the implementer runs. Without accounting for this,
		// an implementer that exits without committing code would pass the
		// headSHA==parentSHA check (HEAD is the CHB-023 commit, not parentSHA).
		// Drain contextCommitSHACh — the goroutine sends the CHB-023 commit SHA
		// (or current HEAD for the idempotent case) — and use it as the baseline
		// when non-empty (hk-mdwh4).
		var contextCommitSHA string
		if state.iterationCount == 1 {
			select {
			case sha := <-contextCommitSHACh:
				contextCommitSHA = sha
			default:
			}
		}

		if state.iterationCount == 1 {
			headSHA, headErr := resolveWorktreeHEADVia(ctx, runner, wtPath)
			if headErr != nil {
				result := rlErrorResult(fmt.Sprintf("resolve worktree HEAD after implementer at iteration %d: %v", state.iterationCount, headErr))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			// Use the CHB-023 commit SHA as the baseline if the context-persist
			// goroutine fired; fall back to parentSHA when the interceptor never fired
			// (tmux substrate, no handler_capabilities, or persist error).
			noCommitBaseline := parentSHA
			if contextCommitSHA != "" {
				noCommitBaseline = contextCommitSHA
			}
			if headSHA == noCommitBaseline {
				summary := fmt.Sprintf("no_commit_during_implementer: HEAD did not advance past parent %s at iteration %d exit=%d",
					noCommitBaseline, state.iterationCount, implEI.exitCode)
				// Surface stderr tail when available — helps diagnose silent
				// implementer crashes where the agent produced no NDJSON output.
				// Mirrors workloop.go:1428-1441 (hk-ajhqw single-mode fix).
				// Bead: hk-loga9.
				if len(implEI.stderrTail) > 0 {
					const maxTailInReason = 200
					tail := implEI.stderrTail
					truncated := ""
					if len(tail) > maxTailInReason {
						tail = tail[len(tail)-maxTailInReason:]
						truncated = " (truncated)"
					}
					fmt.Fprintf(os.Stderr, "daemon: review-loop: implementer exited without commit; bead %s run %s exit=%d stderr tail%s:\n%s\n",
						beadID, runID.String(), implEI.exitCode, truncated, tail)
					summary += fmt.Sprintf(" stderr_tail%s=%q", truncated, tail)
				}
				result := reviewLoopResult{
					success:          false,
					completionReason: core.ReviewLoopCompletionReasonError,
					summary:          summary,
					needsAttention:   true,
				}
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
		}

		// Capture claude_session_id for iteration 1.
		//
		// Resolution order (rlResolveIter1ClaudeSessionID):
		//   1. The value captured by the CHB-023 stdout interceptor (ideal; already
		//      persisted to git by the interceptor's persist goroutine).
		//   2. The real minted `--session-id <uuid>` value (implArtifacts.
		//      claudeSessionID). Under the tmux substrate the interceptor never fires
		//      (handler.launchViaSubstrate returns early when Stdout()==nil, so
		//      StdoutWrapper is never called — hk-za5mz), but the minted id is the
		//      one Claude is instructed to use as its session id via `claude
		//      --session-id <uuid>` (adoption is Claude Code's own --session-id CLI
		//      contract). The hook-relay's CHB-012 guard does not cause adoption — it
		//      only detects deviation and makes it a hard error — so the minted id is
		//      the correct --resume target for iteration 2; synthesising here was the
		//      root cause of no_progress_detected (hk-za5mz).
		//   3. Synthesis (twin-binary test paths launched without --session-id).
		if state.iterationCount == 1 {
			var interceptorID string
			select {
			case id := <-sessionIDFromCapabilities:
				interceptorID = id
			default:
				// Interceptor never fired (tmux substrate, or handler exited
				// without emitting handler_capabilities with claude_session_id).
			}
			state.claudeSessionID = rlResolveIter1ClaudeSessionID(interceptorID, implArtifacts.claudeSessionID)

			// hk-za5mz: when the interceptor never persisted the id (interceptorID
			// empty) but we fell back to the real minted id, the CHB-023 git
			// checkpoint was never written and claude_session_id_persisted never
			// fired. Persist it now so (a) a daemon restart can recover the id for
			// --resume via EM-031 state-reconstruction, and (b) the
			// claude_session_id_persisted event is observable for the resumable
			// session. Best-effort: a persist failure leaves the in-memory
			// state.claudeSessionID intact, so iteration 2 still resumes the real
			// session within this daemon process — only cross-restart recovery and
			// event observability are lost (matching the interceptor goroutine's
			// own non-fatal-on-error posture).
			//
			// REMOTE (runner != nil): persistClaudeSessionID does a box-A-local
			// os.WriteFile + git add/commit inside wtPath, which for a remote run is
			// the WORKER's worktree — the box-A-local write would fail. The persist
			// is purely for cross-restart --resume recovery + event observability;
			// iteration 2 already resumes the live session in-process regardless. So
			// we skip it on remote (avoids a guaranteed-error log every remote run).
			// A worker-routed persist (persistClaudeSessionIDVia) is FLAGGED as
			// multi-iteration follow-up.
			// hk-mzgh: skip CHB-023 fallback persist for SessionIDCaptured harnesses
			// (codex). Their session id is the captured thread_id, not a minted UUID;
			// persisting implArtifacts.claudeSessionID (a tracking UUID, not a real codex
			// thread_id) would write a useless value and misrepresent the state.
			if runner == nil && interceptorID == "" && state.claudeSessionID == implArtifacts.claudeSessionID && implArtifacts.claudeSessionID != "" && !implIsSessionIDCaptured {
				res, persistErr := persistClaudeSessionID(ctx, wtPath, runID, state.claudeSessionID)
				if persistErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: reviewloop: hk-za5mz persist minted claude_session_id: %v (continuing; iter-2 resume still targets the live session in-process)\n", persistErr)
				} else if !res.Skipped {
					emitClaudeSessionIDPersisted(ctx, deps.bus, runID, res.CommitSHA, state.claudeSessionID)
				}
			}
		}

		// ── Compute diff hash + HEAD BEFORE launching reviewer ────────────
		//
		// Per EM-015d: "Before launching the reviewer, the daemon MUST compute
		// last_diff_hash and write it into Run.context.last_diff_hash."
		// Per EM-015e (no-progress early-exit, T-WM-022): "Before launching
		// reviewer from iteration 2 onward" detect no-progress and terminate.
		//
		// hk-togxq: the progress signal is now COMMIT/HEAD ADVANCEMENT, not
		// diff-hash equality. The old `currentHash == state.lastDiffHash` test was
		// HEAD-blind: a real iter-N commit whose net parent..HEAD diff happened to
		// equal a prior iteration's diff (e.g. a revert+re-apply, or a commit that
		// nets to the same tree) was false-flagged as no-progress and the run
		// hard-failed, discarding the committed work. We now compare the current
		// worktree HEAD to the HEAD recorded before the prior iteration's reviewer
		// launch (state.lastIterHeadSHA): if HEAD advanced, the implementer-resume
		// committed real work in response to the prior REQUEST_CHANGES → progress
		// → do NOT fire. Reaching iteration ≥ 2 here is itself proof the prior
		// verdict was REQUEST_CHANGES (APPROVE/BLOCK return before incrementing),
		// so the verdict half of the rule is structurally guaranteed; the explicit
		// check is the HEAD-advance half. lastDiffHash is retained only for the
		// no_progress_detected event payload.
		currentHash, hashErr := rlComputeDiffHashVia(ctx, runner, wtPath, parentSHA)
		if hashErr != nil {
			result := rlErrorResult(fmt.Sprintf("diff-hash error before reviewer at iteration %d: %v", state.iterationCount, hashErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		currentHead, npHeadErr := resolveWorktreeHEADVia(ctx, runner, wtPath)
		if npHeadErr != nil {
			result := rlErrorResult(fmt.Sprintf("resolve HEAD before reviewer at iteration %d: %v", state.iterationCount, npHeadErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// No-progress check (iteration ≥ 2 only): fire only if HEAD did NOT
		// advance since the prior iteration's reviewer launch — i.e. the
		// implementer-resume produced no new commit in response to the prior
		// REQUEST_CHANGES. Iteration 1 has no prior HEAD, so the check is skipped.
		//
		// hk-m1wqp: reaching iteration ≥ 2 here is structural proof that the
		// prior verdict was REQUEST_CHANGES (APPROVE/BLOCK both return before
		// incrementing), so we emit review_fixup_stalled (carrying the reviewer
		// flags) instead of the generic no_progress_detected. This lets triage
		// see the specific flag the implementer failed to address.
		headAdvanced := state.lastIterHeadSHA == "" || currentHead != state.lastIterHeadSHA
		if state.iterationCount >= 2 && !headAdvanced {
			emitReviewFixupStalled(ctx, deps.bus, runID, core.WorkflowModeReviewLoop,
				state.iterationCount, state.lastVerdictFlags, currentHash, state.lastDiffHash)
			result := reviewLoopResult{
				success:          false,
				completionReason: core.ReviewLoopCompletionReasonFixupStalled,
				summary:          fmt.Sprintf("review fix-up stalled at iteration %d: HEAD unchanged after REQUEST_CHANGES", state.iterationCount),
				needsAttention:   true,
			}
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// Store diff hash + HEAD for next iteration's no-progress check.
		state.lastDiffHash = currentHash
		state.lastIterHeadSHA = currentHead

		// ── Archive prior verdict file (iteration ≥ 2) ───────────────────
		//
		// Per EM-015d: archive the prior review.json to review.iter-{N-1}.json
		// before launching the iteration-N reviewer. Non-fatal on failure.
		//
		// LOCAL only: this is an os.Rename inside the IMPLEMENTER worktree
		// (wtPath). For a REMOTE run wtPath is on the worker, so a box-A-local
		// rename targets a path that does not exist on box A. The canonical
		// verdict for each iteration already lives at revWtPath on box A
		// (ReadReviewVerdict below), so skipping the worker-side archive on remote
		// loses only a post-run inspection artifact in the worker worktree.
		// Routing this to the worker is FLAGGED as multi-iteration follow-up.
		if state.iterationCount >= 2 && runner == nil {
			if archErr := workspace.ArchiveVerdict(wtPath, state.iterationCount-1); archErr != nil {
				// F21: ErrNotFound is expected (reviewer didn't produce a verdict on
				// the prior iter) — suppress the log to keep the daemon log clean.
				if !errors.Is(archErr, workspace.ErrNotFound) {
					fmt.Fprintf(os.Stderr, "daemon: reviewloop: ArchiveVerdict prior iter %d: %v (non-fatal)\n",
						state.iterationCount-1, archErr)
				}
			}
		}

		// ── Write review-target.md BEFORE reviewer launch (hk-0xmwq) ─────
		//
		// Per EM-015d-RIA: the reviewer kick-off pasteinject expects
		// .harmonik/review-target.md to exist on disk before the reviewer pane
		// is launched. Without this write, pasteinject hits
		// "structural invariant violated: task file absent: review-target.md"
		// and the reviewer sits idle forever. The implementer counterpart is
		// WriteAgentTask inside buildClaudeLaunchSpec (CHB-028); the reviewer
		// brief is materialized here rather than there because the reviewer
		// requires diff-range SHAs that are only known to runReviewLoop.
		//
		// reviewHeadSHA is the implementer's committed HEAD that the reviewer
		// worktree checks out (detached) on BOX A.
		//
		// LOCAL run (runner == nil): wtPath is box-A-local, so the SHA is read
		// directly from the implementer's worktree (unchanged behaviour, NFR7).
		//
		// REMOTE run (runner != nil): the implementer's commit lives on the
		// WORKER. The reviewer must run on box A off the box-A-fetched run branch
		// (Phase-1 DD3), but box A does not yet have that commit — preMergeSync
		// (workloop.go) only fetches it AFTER the review loop returns. So we run
		// B8 steps (b)+(c) HERE, before each reviewer launch: push the run branch
		// worker→origin, fetch origin→box-A as refs/heads/run/<id>, then resolve
		// reviewHeadSHA from that box-A ref. Each REQUEST_CHANGES iteration
		// re-pushes the advanced (fast-forward) branch, so the reviewer always
		// sees the latest implementer commit. The later preMergeSync is then a
		// harmless no-op push + no-op fetch.
		var reviewHeadSHA string
		if runner != nil {
			if pushErr := pushRunBranchOnWorker(ctx, runner, wtPath, runID.String()); pushErr != nil {
				result := rlErrorResult(fmt.Sprintf("push run branch worker→origin before reviewer at iteration %d: %v", state.iterationCount, pushErr))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			if fetchErr := fetchRunBranchBoxA(ctx, nil, deps.projectDir, runID.String()); fetchErr != nil {
				result := rlErrorResult(fmt.Sprintf("fetch run branch origin→box-A before reviewer at iteration %d: %v", state.iterationCount, fetchErr))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			sha, branchErr := resolveBranchSHA(ctx, deps.projectDir, workspace.TaskBranchName(runID.String()))
			if branchErr != nil {
				result := rlErrorResult(fmt.Sprintf("resolve box-A run-branch SHA before reviewer at iteration %d: %v", state.iterationCount, branchErr))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			reviewHeadSHA = sha
		} else {
			sha, headErr := resolveWorktreeHEAD(ctx, wtPath)
			if headErr != nil {
				result := rlErrorResult(fmt.Sprintf("resolve worktree HEAD before reviewer at iteration %d: %v", state.iterationCount, headErr))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			reviewHeadSHA = sha
		}

		// ── Create isolated reviewer worktree (hk-dut6b) ─────────────────
		//
		// The reviewer runs in its own short-lived worktree checked out in
		// detached-HEAD mode at reviewHeadSHA.  A detached-HEAD worktree means
		// any `git checkout` the reviewer issues only affects the reviewer's
		// own worktree — it cannot switch the implementer's task branch to a
		// different commit and corrupt subsequent iterations.
		//
		// The worktree is NOT leased and NOT tracked by the workspace state
		// machine (analogous to the scratch merge-worktrees of WM-019a).
		// cleanup is deferred so the reviewer worktree is always removed even
		// on early-return paths (error, context cancellation).
		revWtCfg := workspace.NoWorktreeRootOverride()
		revWtPath, revWtCleanup, revWtErr := workspace.CreateReviewerWorktree(
			ctx, deps.projectDir, runID.String(), state.iterationCount, reviewHeadSHA, revWtCfg)
		if revWtErr != nil {
			result := rlErrorResult(fmt.Sprintf("create reviewer worktree at iteration %d: %v", state.iterationCount, revWtErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		defer revWtCleanup()

		reviewTargetPayload := workspace.ReviewTargetPayload{
			WorkspacePath: revWtPath,
			BeadID:        string(beadID),
			Iteration:     state.iterationCount,
			BeadTitle:     beadTitle,
			BeadBody:      beadDescription,
			BaseSHA:       parentSHA,
			HeadSHA:       reviewHeadSHA,
			PriorVerdicts: state.priorVerdicts,
		}
		if rtErr := workspace.WriteReviewTarget(reviewTargetPayload); rtErr != nil {
			result := rlErrorResult(fmt.Sprintf("WriteReviewTarget at iteration %d: %v", state.iterationCount, rtErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// ── Dispatch reviewer ─────────────────────────────────────────────
		//
		// CHB-009: reviewer ALWAYS mints a fresh claudeSessionID (priorClaudeSessID=nil).
		// Each reviewer is an independent fresh session; the prior implementer's
		// session ID is NEVER passed to the reviewer even when it is known.
		revRC := claudeRunCtx{
			runID:             runID,
			beadID:            string(beadID),
			workspacePath:     revWtPath,
			daemonSocket:      daemonSocket,
			workflowMode:      core.WorkflowModeReviewLoop,
			phase:             handlercontract.ReviewLoopPhaseReviewer,
			iterationCount:    state.iterationCount,
			priorClaudeSessID: nil, // CHB-009: reviewer always mints fresh
			handlerBinary:     deps.handlerBinary,
			daemonBinaryPath:  deps.daemonBinaryPath,
			baseEnv:           deps.handlerEnv,
			beadTitle:         beadTitle,
			beadDescription:   beadDescription,
			model:             resolvedModel,
			effort:            resolvedEffort,
			// worktreeRootPath enables --dangerously-skip-permissions for daemon-managed
			// worktrees per HC-055b.
			worktreeRootPath: workspace.WorktreeRootPath(deps.projectDir, workspace.NoWorktreeRootOverride()),
			extraContext:     extraContext, // hk-boiwe
			baseBranch:       baseBranch,   // hk-mtm0w: pre-exit rebase target
		}
		// T14 hk-iv748: reviewer harness resolution — DEFAULT path (review-loop mode).
		//
		// The reviewer uses the SAME resolved harness as the implementer for this run
		// (implArtifacts.resolvedAgentType). Build a reviewer-specific specBuilder with
		// nodeDefault (tier-3) pinned to the implementer's resolved agent type so the
		// reviewer is always wired to the same harness even when deps.launchSpecBuilder
		// was constructed without an explicit nodeDefault.
		//
		// For an all-claude run (resolvedAgentType = claude-code) this is byte-identical
		// to pre-T14 behaviour: reviewer = claude.
		//
		// DOT reviewer_harness override is NOT applicable in review-loop mode because
		// review-loop mode has no DOT node carrying the attribute; that override is
		// threaded in dot_cascade.go (dispatchDotAgenticNode).
		revSpecBuilder := deps.launchSpecBuilder
		if deps.harnessRegistry != nil && implArtifacts.resolvedAgentType.Valid() {
			revSpecBuilder = routedLaunchSpecBuilder(
				deps.harnessRegistry,
				core.BeadRecord{},               // tier-1 absent: bead labels already resolved into resolvedAgentType
				core.AgentType(""),              // queue default: hk-4x3rg
				implArtifacts.resolvedAgentType, // tier-3: implementer's resolved harness (DEFAULT)
				core.AgentType(""),              // global default: built-in fallback = claude-code
				deps.bus,
			)
		}
		if revSpecBuilder == nil {
			revSpecBuilder = buildClaudeLaunchSpec
		}
		revSpec, revArtifacts, revSpecErr := revSpecBuilder(ctx, revRC)
		if revSpecErr != nil {
			result := rlErrorResult(fmt.Sprintf("reviewer spec error at iteration %d: %v", state.iterationCount, revSpecErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		// Attach the optional tmux substrate (nil at MVH; set from deps.substrate).
		// Same requirement as implSpec.Substrate above (hk-2hb2y): without this
		// the reviewer launch takes the exec.CommandContext path, SpawnWindow is
		// never called, and pasteInjectOnLaunch fails with "no window spawned yet".
		//
		// hk-012af: wrap deps.substrate in a fresh perRunSubstrate for the reviewer
		// phase. This gives the reviewer its own isolated pane handle, preventing
		// cross-run pane misdirection under MaxConcurrent>1.
		//
		// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
		revPRS := newPerRunSubstrate(deps.substrate, deps.handlerBinary, nil)
		var revSubstrate handler.Substrate = deps.substrate
		var revPasteTarget handler.Substrate = deps.substrate
		if revPRS != nil {
			revSubstrate = revPRS
			revPasteTarget = revPRS
		}
		revSpec.Substrate = revSubstrate

		// Prepend deps.handlerArgs for test handlers; no-op in production.
		if len(deps.handlerArgs) > 0 {
			revSpec.Args = append(deps.handlerArgs, revSpec.Args...)
		}

		// Register reviewer's hook session (CHB-025); closed after wait completes
		// so late hooks from a closed reviewer don't bleed into the next iteration.
		if deps.hookStore != nil {
			deps.hookStore.RegisterHookSession(runID.String(), revArtifacts.claudeSessionID)
		}

		// hk-fra5l: emit reviewer pre-exec messages (handler_capabilities →
		// session_log_location → skills_provisioned) on the bus BEFORE Launch,
		// mirroring the implementer path above and the single-mode path.
		//
		// hk-4l7zs: launch_initiated is held back and emitted AFTER Launch returns
		// (see implementer phase) so it signals a live reviewer window.
		revLaunchInitiatedMsg := emitPreExecBeforeLaunch(ctx, deps.bus, runID, revArtifacts.preExecMsgs)

		// Create a per-phase tapping emitter so waitAgentReady can observe watcher
		// events from the reviewer launch without a post-seal bus subscription (EV-009).
		// A new handler is constructed using the tap so events flow through the channel.
		// Precondition: deps.adapterRegistry must be non-nil (enforced by
		// newWorkLoopDeps). NewHandler panics on a nil registry (hk-d8u1y).
		revTap, revTapCh := newPerRunEventTap(deps.bus, runID)
		revH := handler.NewHandler(revTap, handlercontract.NoopWatcherDeadLetter{}, deps.adapterRegistry)

		revSessionID := handlercontract.NewSessionID()
		emitReviewerLaunched(ctx, deps.bus, runID, revSessionID, state.claudeSessionID, state.iterationCount)

		revSess, revWatcher, revLaunchErr := revH.Launch(ctx, revSpec)
		if revLaunchErr != nil {
			if deps.hookStore != nil {
				deps.hookStore.CloseHookSession(runID.String(), revArtifacts.claudeSessionID)
			}
			// hk-4l7zs: spawn-cap saturation on the reviewer launch.
			if errors.Is(revLaunchErr, ErrSpawnCapTimeout) {
				inUse, capSize := substrateSpawnStats(revSubstrate)
				emitSpawnCapBlocked(ctx, deps.bus, runID, defaultSpawnAcquireTimeout, inUse, capSize)
			}
			// hk-r1rup: hung `tmux new-window` (no-spawn wedge) on the reviewer
			// launch. No launch-time var here (mirrors the spawn-cap branch), so
			// use defaultNewWindowTimeout as the proxy waited value.
			if errors.Is(revLaunchErr, ErrTmuxNewWindowTimeout) {
				emitTmuxNewWindowTimeout(ctx, deps.bus, runID, defaultNewWindowTimeout)
			}
			result := rlErrorResult(fmt.Sprintf("reviewer launch error at iteration %d: %v", state.iterationCount, revLaunchErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		// hk-4l7zs: emit the held-back reviewer launch_initiated now the window
		// is live.
		if revLaunchInitiatedMsg != nil {
			emitPreExecMessage(ctx, deps.bus, runID, revLaunchInitiatedMsg)
		}
		// hk-68pvl: backstop — force-tear-down this reviewer session before
		// runReviewLoop returns on ANY path, mirroring the implementer guard
		// above so the deferred wtCleanup never removes the worktree while a
		// substrate-hosted reviewer claude is still live in it. Idempotent.
		revSessForTeardown := revSess
		defer forceTeardownSession(revSessForTeardown)

		// Wire the reviewer's agent-ready callback into revTap so that relay-synthesized
		// agent_ready envelopes from the hook-relay subprocess reach revTapCh, which
		// waitAgentReady (below) blocks on. Without this call notifyAgentReady finds a
		// nil callback and revTapCh stays empty, causing HC-056 to fire every 30s.
		//
		// Same wiring gap as single-mode beadRunOne (bead hk-lj1p9.4); fixed in both paths.
		//
		// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-013; specs/handler-contract.md §4.9 HC-056.
		// Bead ref: hk-lj1p9.4.
		if deps.hookStore != nil {
			capturedRevTap := revTap
			deps.hookStore.SetAgentReadyCallback(runID.String(), revArtifacts.claudeSessionID, func() {
				_ = capturedRevTap.Emit(context.Background(), core.EventTypeAgentReady, nil)
			})
		}

		// HC-056: waitAgentReady — reviewer phase must observe agent_ready within
		// the configured timeout, same as the implementer phase.
		//
		// Precondition: deps.adapterRegistry is non-nil (enforced by newWorkLoopDeps;
		// hk-d8u1y deleted the nil-guard). When ErrAgentReadyTimeout fires: kill,
		// reap, emit rlErrorResult so the caller (workloop) can reopen the bead via
		// the same error envelope shape as the implementer phase.
		revAdapter, revAdapterErr := deps.adapterRegistry.ForAgent(artifactAgentType(revArtifacts))
		if revAdapterErr != nil {
			// No adapter for the resolved agent type — non-fatal; skip ready-wait.
			fmt.Fprintf(os.Stderr, "daemon: reviewloop: ForAgent(%s) bead %s iter %d: %v (skipping ready-wait)\n",
				artifactAgentType(revArtifacts), beadID, state.iterationCount, revAdapterErr)
		} else {
			// Derive a child context that cancels when the reviewer watcher finishes
			// (handler exit), preventing a full-timeout block on reviewer crash.
			//
			// Substrate path: revWatcher is nil when deps.substrate != nil
			// (tmux-hosted sessions return nil from handler.launchViaSubstrate
			// when subSess.Stdout() is nil — see internal/handler/handler.go:291).
			// In that case there is no progress-stream goroutine to await; the
			// watcher-done coordination goroutine is simply skipped and the
			// ready-wait below relies on context cancellation alone.
			// Bead ref: hk-yjduq (DOGFOOD-BLOCKER #2 — nil watcher in tmux path).
			revReadyCtx, revReadyCancel := context.WithCancel(ctx)
			if revWatcher != nil {
				go func() {
					select {
					case <-revWatcher.Done():
						revReadyCancel()
					case <-revReadyCtx.Done():
					}
				}()
			}

			revEventSrc := newChanAgentEventSource(revTapCh)
			revReadyErr := waitAgentReady(revReadyCtx, runID, revEventSrc, revAdapter, deps.agentReadyTimeout)
			revReadyCancel() // always release the watcher-done goroutine above

			if revReadyErr == ErrAgentReadyTimeout {
				// HC-056: reviewer agent_ready_timeout — kill, reap, error result.
				fmt.Fprintf(os.Stderr, "daemon: reviewloop: waitAgentReady reviewer bead %s iter %d run %s: %v (error)\n",
					beadID, state.iterationCount, runID.String(), revReadyErr)
				_ = revSess.Kill(ctx)
				// Wait for the reviewer watcher goroutine to exit with a
				// deadline — agentReadyKillReapTimeout prevents indefinite
				// blocking if the killed subprocess does not cooperate.
				// Substrate path: revWatcher is nil when tmux-hosted (no
				// stdout pipe — see handler.go:291); skip the watcher reap
				// and rely on sess.Wait below. Bead ref: hk-yjduq.
				if revWatcher != nil {
					select {
					case <-revWatcher.Done():
					case <-time.After(agentReadyKillReapTimeout):
						fmt.Fprintf(os.Stderr, "daemon: reviewloop: revWatcher.Done() reap timed out bead %s iter %d run %s after Kill — continuing\n",
							beadID, state.iterationCount, runID.String())
					}
				}
				_ = revSess.Wait(ctx)
				if deps.hookStore != nil {
					deps.hookStore.CloseHookSession(runID.String(), revArtifacts.claudeSessionID)
				}
				result := rlErrorResult(fmt.Sprintf("reviewer agent_ready_timeout at iteration %d", state.iterationCount))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			// revReadyErr == nil (agent_ready observed) OR context.Canceled (watcher
			// exited first or ctx cancelled). Fall through to waitWithSocketGrace.
		}

		// Paste-inject the reviewer kick-off message AFTER agent_ready (hk-zchbu).
		// Running before agent_ready races Claude's welcome splash, which
		// consumes the trailing \n and leaves the buffered text unsubmitted.
		// hk-012af: use revPasteTarget (per-run wrapper) so inject targets this
		// reviewer's pane rather than the shared "last pane".
		// pasteInjectOnLaunch is non-blocking (spawns an internal goroutine).
		// hk-zimkh: the returned briefDelivered channel gates
		// pasteInjectQuitOnReviewFile, which watches for .harmonik/review.json
		// and sends /quit once the verdict is written — without this the
		// reviewer claude hangs indefinitely at a prompt.
		// Spec ref: specs/process-lifecycle.md §4.7 PL-021d.
		revBriefDelivered := pasteInjectOnLaunch(ctx, revPasteTarget, revArtifacts.claudeSessionID,
			handlercontract.ReviewLoopPhaseReviewer, state.iterationCount, revWtPath,
			deps.bus, runID)
		if qs, ok := revPasteTarget.(quitSender); ok {
			// hk-7rgqs: pass the pasteInjecter + claude session id so the watchdog
			// can re-seed the reviewer brief once if the original submit Enter was
			// swallowed by a slow splash (revPasteTarget implements pasteInjecter
			// when it is a perRunSubstrate; a non-pasteInjecter target yields a nil
			// inj inside the watchdog → re-seed disabled).
			revInj, _ := revPasteTarget.(pasteInjecter)
			// hk-60t8: give the reviewer watchdog an independent heartbeat
			// subscription so it can extend the deadline when the reviewer is
			// actively reasoning (recent agent_heartbeat), not only when the OS
			// pane-liveness probe finds an active process.
			revHBCh := revTap.Subscribe()
			go pasteInjectQuitOnReviewFile(ctx, qs, revSess, revInj, revArtifacts.claudeSessionID, revWtPath, revBriefDelivered, revHBCh, 0)
		}

		// Wait for reviewer using waitWithSocketGrace (OQ2 resolution).
		_, revEI := waitWithSocketGrace(ctx, deps.hookStore, revWatcher, revSess,
			runID.String(), revArtifacts.claudeSessionID)
		_ = revEI

		// hk-e6mtt: destroy the reviewer tmux window after the session completes.
		// Mirrors the implementer fix above and the single-mode fix in workloop.go.
		if revWatcher == nil {
			_ = revSess.Kill(context.Background())
		}

		// Close reviewer's hook session — late hooks must not bleed into the
		// next iteration's implementer-resume (CHB-025 isolation).
		if deps.hookStore != nil {
			deps.hookStore.CloseHookSession(runID.String(), revArtifacts.claudeSessionID)
		}

		if ctx.Err() != nil {
			return reviewLoopResult{
				success:          false,
				completionReason: core.ReviewLoopCompletionReasonError,
				summary:          "context cancelled during reviewer wait",
				needsAttention:   false,
			}
		}

		// ── Read and validate verdict file ────────────────────────────────
		//
		// The reviewer wrote review.json to its isolated worktree (revWtPath).
		// Read from there, then copy to wtPath so the archive and implementer
		// worktree keep the verdict for post-run inspection (hk-dut6b).
		verdict, verdictErr := workspace.ReadReviewVerdict(revWtPath)
		if verdictErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: reviewloop: ReadReviewVerdict iter %d: %v\n", state.iterationCount, verdictErr)
			result := rlErrorResult(fmt.Sprintf("verdict malformed at iteration %d: %v", state.iterationCount, verdictErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		if verdict == nil {
			// hk-sah87: disambiguate a BUDGET kill (the reviewer was working but
			// ran out of its diff-scaled verdict budget on a heavy diff — see
			// pasteInjectQuitOnReviewFile) from a true no-verdict.  The marker is
			// written into the reviewer's worktree, so read from revWtPath.
			if sentinel, sErr := ReadReviewerBudgetSentinel(revWtPath); sErr == nil && sentinel != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: reviewloop: reviewer budget exceeded at iteration %d (reason=%s budget_ms=%d elapsed_ms=%d changed_lines=%d)\n",
					state.iterationCount, sentinel.Reason, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines)
				emitReviewerBudgetExceeded(ctx, deps.bus, runID, sentinel.BudgetMS, sentinel.ElapsedMS, sentinel.ChangedLines, sentinel.Reason)
				result := rlErrorResult(fmt.Sprintf(
					"reviewer budget exceeded at iteration %d (%s; budget=%dms, changed_lines=%d) — verdict absent",
					state.iterationCount, sentinel.Reason, sentinel.BudgetMS, sentinel.ChangedLines))
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			fmt.Fprintf(os.Stderr, "daemon: reviewloop: verdict absent at iteration %d\n", state.iterationCount)
			result := rlErrorResult(fmt.Sprintf("verdict absent at iteration %d", state.iterationCount))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// Copy review.json from the reviewer's isolated worktree to the
		// implementer's worktree so ArchiveVerdict (below) and post-run
		// inspection tools find it at the canonical wtPath location.
		//
		// LOCAL only: writes into the IMPLEMENTER worktree (wtPath). For a REMOTE
		// run wtPath is on the worker; this box-A-local copy would target a
		// nonexistent box-A path and create a stray directory tree. The verdict is
		// already read from revWtPath (box A) above and routed below, so the copy
		// is only an inspection convenience — skip it on remote.
		//
		// review.json and review.iter-*.json MUST be gitignored so they are never
		// committed onto the run branch. The .harmonik/.gitignore written by
		// `harmonik init` covers these paths; commitResidualDelta explicitly
		// excludes all of .harmonik/ as belt-and-suspenders (GH #7, hk-znou).
		if runner == nil {
			if cpErr := rlCopyReviewVerdict(revWtPath, wtPath); cpErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: reviewloop: copy review verdict iter %d: %v (non-fatal)\n",
					state.iterationCount, cpErr)
			}
		}

		// Emit reviewer_verdict (verbatim agent-reviewer schema v1 fields per EM-015d).
		emitReviewerVerdict(ctx, deps.bus, runID, revSessionID, state.claudeSessionID, state.iterationCount, verdict)

		// Archive this iteration's verdict to review.iter-N.json per EM-015d.
		//
		// LOCAL only (os.Rename inside wtPath) — see the prior-iter archive guard
		// above. Skipped on remote; FLAGGED as multi-iteration follow-up.
		if runner == nil {
			if archErr := workspace.ArchiveVerdict(wtPath, state.iterationCount); archErr != nil {
				// F21: ErrNotFound is expected when the reviewer session ended without
				// writing review.json — suppress to keep the daemon log clean.
				if !errors.Is(archErr, workspace.ErrNotFound) {
					fmt.Fprintf(os.Stderr, "daemon: reviewloop: ArchiveVerdict iter %d: %v (non-fatal)\n",
						state.iterationCount, archErr)
				}
			}
		}

		// Record verdict notes for use as prior_verdict_summary on next implementer resume.
		state.lastVerdictNotes = verdict.Notes

		// Append to priorVerdicts so the next iteration's review-target.md
		// renders a populated `## Prior verdicts` section (EM-015d-RIA). Notes
		// are truncated to the first 200 chars with an ellipsis per spec.
		// Bead: hk-0xmwq.
		const priorNotesMax = 200
		notesSummary := verdict.Notes
		if len(notesSummary) > priorNotesMax {
			notesSummary = rlTruncateUTF8(notesSummary, priorNotesMax) + "…"
		}
		state.priorVerdicts = append(state.priorVerdicts, workspace.ReviewTargetPriorVerdict{
			Iteration:    state.iterationCount,
			Verdict:      verdict.Verdict,
			Flags:        verdict.Flags,
			NotesSummary: notesSummary,
		})

		// ── Route on verdict ──────────────────────────────────────────────
		switch verdict.Verdict {
		case workspace.ReviewVerdictApprove:
			result := reviewLoopResult{
				success:          true,
				completionReason: core.ReviewLoopCompletionReasonApproved,
				summary:          fmt.Sprintf("APPROVE at iteration %d", state.iterationCount),
				needsAttention:   false,
				approveVerdict:   verdict, // hk-dyim: thread verdict for merge commit trailers
			}
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result

		case workspace.ReviewVerdictBlock:
			result := reviewLoopResult{
				success:          false,
				completionReason: core.ReviewLoopCompletionReasonBlocked,
				summary:          fmt.Sprintf("BLOCK at iteration %d", state.iterationCount),
				needsAttention:   true,
			}
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result

		case workspace.ReviewVerdictRequestChanges:
			if state.iterationCount >= reviewLoopIterationCap {
				// Cap hit: emit iteration_cap_hit BEFORE cycle_complete per §8.1a ordering.
				emitIterationCapHit(ctx, deps.bus, runID, state.iterationCount, reviewLoopIterationCap,
					core.ReviewerVerdictRequestChanges)
				result := reviewLoopResult{
					success:          false,
					completionReason: core.ReviewLoopCompletionReasonCapHit,
					summary:          fmt.Sprintf("REQUEST_CHANGES at iteration %d (cap=%d)", state.iterationCount, reviewLoopIterationCap),
					needsAttention:   true,
				}
				emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
				return result
			}
			// EM-015d-RFD: write reviewer-feedback.iter-N.md so that the iter-(N+1)
			// implementer-resume paste-inject can deliver the reviewer's notes.
			// Without this file, pasteInjectImplementerResume logs "task file absent"
			// and skips the feedback inject → implementer resumes blind → same diff →
			// no_progress_detected (hk-7x7ea root cause).
			rfPayload := workspace.ReviewerFeedbackPayload{
				WorkspacePath:  wtPath,
				PriorIteration: state.iterationCount,
				Verdict:        verdict.Verdict,
				Flags:          verdict.Flags,
				Notes:          verdict.Notes,
				DiffHash:       state.lastDiffHash,
			}
			// REMOTE-RUN LIMITATION (FLAGGED follow-up): the feedback file must land
			// in the IMPLEMENTER worktree (wtPath) so the iter-(N+1)
			// implementer-resume paste-inject — which runs on the WORKER — can read
			// it. WriteReviewerFeedback is a box-A-local os.WriteFile; for a remote
			// run wtPath is on the worker, so the write does not reach the worker.
			// There is no WriteReviewerFeedbackVia yet. We log loudly and continue;
			// the resume will skip the feedback inject (same effect as a missing
			// feedback file). Routing reviewer feedback to the worker is the one true
			// blocker for MULTI-ITERATION (REQUEST_CHANGES) remote runs — see the
			// return report's FLAGGED-follow-up section. First-pass APPROVE remote
			// runs never reach this branch.
			if runner != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: reviewloop: REMOTE run iter %d: reviewer feedback NOT routed to worker worktree (no WriteReviewerFeedbackVia); implementer-resume will run without feedback — multi-iteration remote runs are not yet supported (FLAGGED follow-up)\n",
					state.iterationCount)
			} else if rfErr := workspace.WriteReviewerFeedback(rfPayload); rfErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: reviewloop: WriteReviewerFeedback iter %d: %v (non-fatal)\n",
					state.iterationCount, rfErr)
				// Non-fatal: continue to next iteration. The implementer-resume will
				// skip the feedback paste-inject (same as the pre-fix behaviour for
				// this bead), but will not block the iteration from proceeding.
			}
			// hk-m1wqp: save the reviewer flags so the no-progress check at
			// the next iteration can emit review_fixup_stalled with the specific
			// flag the implementer failed to address.
			flags := verdict.Flags
			if flags == nil {
				flags = []string{}
			}
			state.lastVerdictFlags = flags
			// Iterations remaining: increment and continue to next iteration.
			state.iterationCount++

		default:
			// ReadReviewVerdict validates the verdict field; this branch is unreachable
			// under correct operation.
			result := rlErrorResult(fmt.Sprintf("unexpected verdict %q at iteration %d", verdict.Verdict, state.iterationCount))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
	}
}

// rlErrorResult constructs an error-path reviewLoopResult with needs-attention.
func rlErrorResult(summary string) reviewLoopResult {
	return reviewLoopResult{
		success:          false,
		completionReason: core.ReviewLoopCompletionReasonError,
		summary:          summary,
		needsAttention:   true,
	}
}

// rlComputeDiffHash resolves the current HEAD of the worktree and computes the
// diff hash against parentSHA per EM-015e.
func rlComputeDiffHash(ctx context.Context, wtPath, parentSHA string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon: reviewloop: git rev-parse HEAD in %q: %w", wtPath, err)
	}
	headSHA := string(out)
	for len(headSHA) > 0 && headSHA[len(headSHA)-1] == '\n' {
		headSHA = headSHA[:len(headSHA)-1]
	}
	if headSHA == "" {
		return "", fmt.Errorf("daemon: reviewloop: git rev-parse HEAD returned empty in %q", wtPath)
	}
	return workspace.ComputeDiffHash(ctx, wtPath, parentSHA, headSHA)
}

// rlComputeDiffHashVia is like rlComputeDiffHash but routes both the HEAD probe
// and the diff through runner. When runner is nil (every LOCAL run) it delegates
// to rlComputeDiffHash byte-identically (NFR7); only REMOTE DOT runs (runner is an
// SSHRunner) take the routed path, REQUIRED when the worktree is on a worker whose
// filesystem box A cannot read directly.
func rlComputeDiffHashVia(ctx context.Context, runner tmux.CommandRunner, wtPath, parentSHA string) (string, error) {
	if runner == nil {
		return rlComputeDiffHash(ctx, wtPath, parentSHA)
	}
	headSHA, err := resolveWorktreeHEADVia(ctx, runner, wtPath)
	if err != nil {
		return "", fmt.Errorf("daemon: reviewloop: %w", err)
	}
	return workspace.ComputeDiffHashVia(ctx, runner, wtPath, parentSHA, headSHA)
}

// resolveBranchSHA resolves a local branch ref to its commit SHA on BOX A via
// `git -C <projectDir> rev-parse refs/heads/<branch>`. Used by the REMOTE
// review-loop path to read the implementer's HEAD from the box-A-fetched run
// branch (refs/heads/run/<id>) so the reviewer worktree — created on box A —
// checks out a commit box A actually has. The lookup runs box-A-local (the
// reviewer always runs on box A regardless of where the implementer ran), so it
// uses a bare exec, mirroring resolveWorktreeHEAD.
func resolveBranchSHA(ctx context.Context, projectDir, branch string) (string, error) {
	ref := "refs/heads/" + branch
	cmd := exec.CommandContext(ctx, "git", "-C", projectDir, "rev-parse", ref)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon: reviewloop: resolveBranchSHA: git -C %q rev-parse %s: %w", projectDir, ref, err)
	}
	sha := string(out)
	for len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	if sha == "" {
		return "", fmt.Errorf("daemon: reviewloop: resolveBranchSHA: git rev-parse %s returned empty in %q", ref, projectDir)
	}
	return sha, nil
}

// rlSynthesiseClaudeSessionID produces a synthetic session ID for the MVH twin-
// binary case where the subprocess does not emit `--output-format json` stdout.
//
// The result must satisfy bufferNameRe ([a-z0-9-]+ after the "harmonik-" prefix
// and before the trailing "-<purpose>" slug).  The old format used
// "20060102T150405Z" whose uppercase 'T' and 'Z' violated the regex, causing
// WriteLastPane to return ErrStructural and the run to go run_stale on iter-2
// resume (hk-lckbv).  The new format uses only ASCII digits — no dashes in the
// ID body and no uppercase characters — so the buffer name always passes
// validation.
//
// Post-MVH: replace with handlercontract.ParseClaudeSessionID on the session's
// captured stdout buffer once the handler exposes it.
func rlSynthesiseClaudeSessionID() string {
	return "syntheticclaudesession" + time.Now().UTC().Format("20060102150405")
}

// rlResolveIter1ClaudeSessionID picks the claude_session_id to carry forward from
// iteration 1 to the iteration-2 implementer-resume launch, in priority order:
//
//  1. interceptorID — captured by the CHB-023 SessionIDInterceptor on the
//     handler's stdout. This is the ideal path: the value was already persisted
//     to git by the interceptor's persist goroutine.
//  2. realMintedID — the UUIDv7 the daemon minted and passed to the handler as
//     `claude --session-id <uuid>`. This is the id Claude is instructed to use as
//     its session id (adoption is Claude Code's own --session-id CLI contract).
//     The hook-relay's CHB-012 guard (bridge_session_id_mismatch) does not cause
//     adoption — it only detects deviation: any hook payload whose session_id
//     differs from realMintedID is a hard error (exit 1). So a session that ran
//     without erroring carries realMintedID, making it a valid `claude --resume`
//     target — unlike a synthetic id, which provably never existed.
//  3. synthesis — only when both ids are empty (a twin-binary test path launched
//     without `--session-id`, where no real Claude session exists to resume).
//
// # Why this fixes hk-za5mz
//
// Under the tmux substrate, handler.launchViaSubstrate returns early when
// subSess.Stdout() is nil (handler.go:303) and never calls StdoutWrapper, so the
// interceptor never fires and interceptorID is empty. The prior behavior fell
// straight through to synthesis, so iteration-2 launched
// `claude --resume <synthetic>` against a session that never existed → no commit
// → diff unchanged → no_progress_detected. Falling back to realMintedID (the live
// `--session-id` value) makes iteration-2's --resume target the real session.
func rlResolveIter1ClaudeSessionID(interceptorID, realMintedID string) string {
	if interceptorID != "" {
		return interceptorID
	}
	if realMintedID != "" {
		return realMintedID
	}
	return rlSynthesiseClaudeSessionID()
}

// rlCopyReviewVerdict copies ${srcWtPath}/.harmonik/review.json to
// ${dstWtPath}/.harmonik/review.json so the implementer's worktree retains the
// reviewer's verdict for ArchiveVerdict and post-run inspection after the
// reviewer's isolated worktree is removed (hk-dut6b).
//
// The destination directory is created if absent.  Errors are non-fatal to the
// caller; a failed copy is logged but does not reopen the bead.
//
// review.json and its per-iteration archives (review.iter-*.json) MUST be
// covered by .gitignore so they are never committed onto the run branch.
// The .harmonik/.gitignore written by `harmonik init` covers them, and
// commitResidualDelta explicitly excludes all of .harmonik/ as an additional
// safeguard (GH #7, hk-znou).
func rlCopyReviewVerdict(srcWtPath, dstWtPath string) error {
	src := workspace.ReviewVerdictPath(srcWtPath)
	dst := workspace.ReviewVerdictPath(dstWtPath)

	//nolint:gosec // G304: path constructed from workspace paths + known relative segments
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("rlCopyReviewVerdict: read %q: %w", src, err)
	}
	if mkErr := os.MkdirAll(filepath.Dir(dst), 0o755); mkErr != nil {
		return fmt.Errorf("rlCopyReviewVerdict: MkdirAll %q: %w", filepath.Dir(dst), mkErr)
	}
	//nolint:gosec // G306: 0644 is intentional for a review artifact
	if wErr := os.WriteFile(dst, data, 0o644); wErr != nil {
		return fmt.Errorf("rlCopyReviewVerdict: write %q: %w", dst, wErr)
	}
	return nil
}

// rlTruncateUTF8 returns the prefix of s that is at most maxBytes UTF-8 bytes,
// trimming any incomplete trailing code unit per event-model.md §6.3.
func rlTruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := []byte(s[:maxBytes])
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}


// ─────────────────────────────────────────────────────────────────────────────
// Event emission helpers (§8.1a review-loop events)
// ─────────────────────────────────────────────────────────────────────────────

// emitImplementerResumed emits implementer_resumed (§8.1a.1) before the
// implementer is dispatched on iterations ≥ 2.
func emitImplementerResumed(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	claudeSessionID string,
	iterationCount int,
	priorVerdictSummary string,
) {
	pl := core.ImplementerResumedPayload{
		RunID:               runID,
		WorkflowMode:        core.WorkflowModeReviewLoop,
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

// emitReviewerLaunched emits reviewer_launched (§8.1a.2) before each reviewer
// dispatch.
func emitReviewerLaunched(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	sessionID core.SessionID,
	claudeSessionID string,
	iterationCount int,
) {
	pl := core.ReviewerLaunchedPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeReviewLoop,
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

// emitReviewerVerdict emits reviewer_verdict (§8.1a.3) after reading and
// validating the verdict file. The agent-reviewer schema v1 fields are passed
// verbatim per the schema-reuse rule.
func emitReviewerVerdict(
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
		WorkflowMode:    core.WorkflowModeReviewLoop,
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

// emitIterationCapHit emits iteration_cap_hit (§8.1a.4) when the cap is reached.
// Emitted BEFORE review_loop_cycle_complete per §8.1a ordering rule.
func emitIterationCapHit(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	iterationCount int,
	capValue int,
	finalVerdict core.ReviewerVerdict,
) {
	pl := core.IterationCapHitPayload{
		RunID:          runID,
		WorkflowMode:   core.WorkflowModeReviewLoop,
		IterationCount: iterationCount,
		CapValue:       capValue,
		FinalVerdict:   finalVerdict,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeIterationCapHit, b)
}

// emitNoProgressDetected emits no_progress_detected (§8.1a.5) when the diff
// hash of the current iteration matches the prior. Emitted BEFORE
// review_loop_cycle_complete per §8.1a ordering rule.
func emitNoProgressDetected(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	iterationCount int,
	diffHashCurrent string,
	diffHashPrior string,
) {
	pl := core.NoProgressDetectedPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeReviewLoop,
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

// emitReviewFixupStalled emits review_fixup_stalled (§8.1a.7) when a REQUEST_CHANGES
// fix-up run advances HEAD by zero commits. Emitted BEFORE review_loop_cycle_complete
// in review-loop mode per §8.1a ordering rule; in DOT mode the cascade terminates
// directly without review_loop_cycle_complete. Bead ref: hk-m1wqp.
func emitReviewFixupStalled(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	workflowMode core.WorkflowMode,
	iterationCount int,
	reviewerFlags []string,
	diffHashCurrent string,
	diffHashPrior string,
) {
	flags := reviewerFlags
	if flags == nil {
		flags = []string{}
	}
	pl := core.ReviewFixupStalledPayload{
		RunID:           runID,
		WorkflowMode:    workflowMode,
		IterationCount:  iterationCount,
		ReviewerFlags:   flags,
		DiffHashCurrent: diffHashCurrent,
		DiffHashPrior:   diffHashPrior,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeReviewFixupStalled, b)
}

// emitReviewLoopCycleComplete emits review_loop_cycle_complete (§8.1a.6) exactly
// once per cycle before run_completed / run_failed per EM-015d ordering rule.
func emitReviewLoopCycleComplete(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	finalIterationCount int,
	completionReason core.ReviewLoopCompletionReason,
) {
	pl := core.ReviewLoopCycleCompletePayload{
		RunID:               runID,
		WorkflowMode:        core.WorkflowModeReviewLoop,
		FinalIterationCount: finalIterationCount,
		CompletionReason:    completionReason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeReviewLoopCycleComplete, b)
}

// emitReviewerBudgetExceeded emits a reviewer_budget_exceeded event (hk-da3rr)
// when a reviewer session is force-killed for exhausting its diff-scaled verdict
// budget. Non-fatal: a nil bus or marshal error is silently discarded.
func emitReviewerBudgetExceeded(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, budgetMS, elapsedMS int64, changedLines int, reason string) {
	if bus == nil {
		return
	}
	if reason == "" {
		reason = "reviewer-budget-exceeded"
	}
	pl := core.ReviewerBudgetExceededPayload{
		RunID:        runID.String(),
		BudgetMS:     budgetMS,
		ElapsedMS:    elapsedMS,
		ChangedLines: changedLines,
		Reason:       reason,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: reviewloop: emitReviewerBudgetExceeded: marshal: %v\n", err)
		return
	}
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerBudgetExceeded, b)
}
