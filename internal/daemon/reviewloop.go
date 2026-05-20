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
	"github.com/gregberns/harmonik/internal/workspace"
)

// reviewLoopIterationCap is the hardcoded maximum number of iterations per
// execution-model.md §4.3.EM-015e. Not operator-tunable at MVH.
const reviewLoopIterationCap = 3

// priorVerdictSummaryMaxBytes is the maximum byte length of the
// prior_verdict_summary field in implementer_resumed events, per
// event-model.md §8.1a.1 (front-truncation to 256 UTF-8 bytes).
const priorVerdictSummaryMaxBytes = 256

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
	lastDiffHash string
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

		// Build the implementer LaunchSpec via buildClaudeLaunchSpec (hk-gql20.15).
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
		}
		implSpec, implArtifacts, implSpecErr := buildClaudeLaunchSpec(ctx, implRC)
		if implSpecErr != nil {
			result := rlErrorResult(fmt.Sprintf("implementer spec error at iteration %d: %v", state.iterationCount, implSpecErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		// Attach the optional tmux substrate (nil at MVH; set from deps.substrate).
		// REQUIRED: without this, h.Launch takes the exec.CommandContext path and
		// SpawnWindow is never called, leaving tmuxSubstrate.lastHandle empty.
		// pasteInjectOnLaunch then fails with "no window spawned yet" because it
		// reads lastHandle from the substrate but no window was opened in this phase.
		// This is the root cause of the pane-race bug (hk-2hb2y).
		//
		// hk-012af: wrap deps.substrate in a perRunSubstrate so this review-loop
		// iteration gets an isolated pane handle. Under MaxConcurrent>1, two
		// concurrent review-loop goroutines would otherwise race on
		// tmuxSubstrate.lastPaneID, sending paste-inject to the wrong pane.
		//
		// Spec ref: specs/process-lifecycle.md §4.7 PL-021b.
		implPRS := newPerRunSubstrate(deps.substrate)
		var implSubstrate handler.Substrate = deps.substrate
		var implPasteTarget handler.Substrate = deps.substrate
		if implPRS != nil {
			implSubstrate = implPRS
			implPasteTarget = implPRS
		}
		implSpec.Substrate = implSubstrate

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

		// Capture implSess so the ACK goroutine below can use it.
		// The variable is set after Launch; the goroutine uses it only after the
		// interceptor fires (which happens after Launch returns and the Watcher
		// starts reading), so the ordering is safe.
		var implSess handler.Session

		if state.iterationCount == 1 {
			// Capture loop variables for the closure.
			capturedWtPath := wtPath
			capturedRunID := runID
			capturedBus := deps.bus
			capturedCtx := ctx

			implSpec.StdoutWrapper = func(r io.Reader) io.Reader {
				return newSessionIDInterceptor(r, func(id string) {
					// Fired on the Watcher's goroutine (inside Read).
					// Spawn a goroutine to persist + ACK so the Watcher is not blocked.
					//
					// CHB-023 ordering: git commit → transition_event → ACK.
					go func() {
						res, persistErr := persistClaudeSessionID(capturedCtx, capturedWtPath, capturedRunID, id)
						if persistErr != nil {
							fmt.Fprintf(os.Stderr,
								"daemon: reviewloop: persist claude_session_id: %v (continuing without persistence)\n", persistErr)
							// Signal empty so the review loop falls back to synthesis.
							sessionIDFromCapabilities <- ""
							return
						}
						if !res.Skipped {
							// EM-025a: emit transition_event AFTER git commit.
							emitClaudeSessionIDPersisted(capturedCtx, capturedBus, capturedRunID, res.CommitSHA, id)
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

		var implWatcher *handlercontract.Watcher
		var implLaunchErr error
		implSess, implWatcher, implLaunchErr = deps.h.Launch(ctx, implSpec)
		if implLaunchErr != nil {
			if deps.hookStore != nil {
				deps.hookStore.CloseHookSession(runID.String(), implArtifacts.claudeSessionID)
			}
			result := rlErrorResult(fmt.Sprintf("implementer launch error at iteration %d: %v", state.iterationCount, implLaunchErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// Paste-inject: deliver the kick-off message to the implementer pane (hk-zrj83).
		// pasteInjectOnLaunch is a no-op when deps.substrate does not implement
		// pasteInjecter (exec.CommandContext path, test fixtures). Non-fatal.
		//
		// NOTE: The review-loop implementer path does not currently call waitAgentReady
		// (it uses waitWithSocketGrace directly), so no SetAgentReadyCallback wire is
		// needed here. If a future pass adds waitAgentReady to the implementer phase,
		// a per-run tap must be constructed before Launch and wired via SetAgentReadyCallback.
		//
		// Spec ref: specs/process-lifecycle.md §4.7 PL-021d; specs/claude-hook-bridge.md §4.11 CHB-028.
		// Bead ref: hk-lj1p9.4, hk-zrj83.
		go pasteInjectOnLaunch(ctx, implPasteTarget, implArtifacts.claudeSessionID,
			implPhase, state.iterationCount, wtPath)

		// Quit-on-commit: after the implementer's task commit lands in the worktree,
		// send `/quit Enter` to trigger Stop hook → outcome_emitted → workloop unblocked.
		// The initial HEAD for this iteration is the current worktree HEAD at launch time.
		// Non-fatal: only fires when substrate implements quitSender (tmux path).
		//
		// hk-012af: use implPasteTarget (per-run wrapper) so /quit targets this
		// iteration's pane, not the shared "last pane".
		//
		// Spec ref: specs/claude-hook-bridge.md §4.11 CHB-028 (session-completion-instruction).
		// Bead: hk-cmybm.
		if qs, ok := implPasteTarget.(quitSender); ok {
			implInitialSHA, resolveErr := resolveWorktreeHEAD(ctx, wtPath)
			if resolveErr != nil {
				implInitialSHA = parentSHA // fallback to known-good parent SHA
			}
			go pasteInjectQuitOnCommit(ctx, qs, wtPath, implInitialSHA)
		}

		// Wait for implementer using waitWithSocketGrace (OQ2 resolution: stop hook wins).
		// This replaces the bare <-watcher.Done() + sess.Wait() pattern.
		_, implEI := waitWithSocketGrace(ctx, deps.hookStore, implWatcher, implSess,
			runID.String(), implArtifacts.claudeSessionID)
		_ = implEI // exit info available for diagnostics; iteration control uses verdict file

		// Close this phase's hook session — late hooks from a completed implementer
		// must not bleed into the next phase (reviewer or implementer-resume).
		if deps.hookStore != nil {
			deps.hookStore.CloseHookSession(runID.String(), implArtifacts.claudeSessionID)
		}

		if ctx.Err() != nil {
			return reviewLoopResult{
				success:          false,
				completionReason: core.ReviewLoopCompletionReasonError,
				summary:          "context cancelled during implementer wait",
				needsAttention:   false,
			}
		}

		// Capture claude_session_id for iteration 1.
		// Prefer the value persisted to git via the interceptor (CHB-023).
		// Fall back to synthesis when the handler did not emit claude_session_id in
		// handler_capabilities (pre-bridge twin binary; existing test paths).
		if state.iterationCount == 1 {
			select {
			case id := <-sessionIDFromCapabilities:
				if id != "" {
					state.claudeSessionID = id
				} else {
					state.claudeSessionID = rlSynthesiseClaudeSessionID()
				}
			default:
				// Interceptor never fired (handler exited without emitting
				// handler_capabilities with claude_session_id). Synthesise.
				state.claudeSessionID = rlSynthesiseClaudeSessionID()
			}
		}

		// ── Compute diff hash BEFORE launching reviewer ───────────────────
		//
		// Per EM-015d: "Before launching the reviewer, the daemon MUST compute
		// last_diff_hash and write it into Run.context.last_diff_hash."
		// Per EM-015e (no-progress early-exit, T-WM-022): "Before launching
		// reviewer from iteration 2 onward, compare current diff hash to
		// Run.context.last_diff_hash. If equal, emit no_progress_detected and
		// terminate."
		currentHash, hashErr := rlComputeDiffHash(ctx, wtPath, parentSHA)
		if hashErr != nil {
			result := rlErrorResult(fmt.Sprintf("diff-hash error before reviewer at iteration %d: %v", state.iterationCount, hashErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// No-progress check (iteration ≥ 2 only): compare to prior iteration's hash.
		// Iteration 1 has no prior hash, so the check is skipped.
		if state.iterationCount >= 2 && currentHash == state.lastDiffHash {
			emitNoProgressDetected(ctx, deps.bus, runID, state.iterationCount, currentHash, state.lastDiffHash)
			result := reviewLoopResult{
				success:          false,
				completionReason: core.ReviewLoopCompletionReasonNoProgress,
				summary:          fmt.Sprintf("no-progress detected at iteration %d: diff hash unchanged", state.iterationCount),
				needsAttention:   true,
			}
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// Store diff hash for next iteration's no-progress check.
		state.lastDiffHash = currentHash

		// ── Archive prior verdict file (iteration ≥ 2) ───────────────────
		//
		// Per EM-015d: archive the prior review.json to review.iter-{N-1}.json
		// before launching the iteration-N reviewer. Non-fatal on failure.
		if state.iterationCount >= 2 {
			if archErr := workspace.ArchiveVerdict(wtPath, state.iterationCount-1); archErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: reviewloop: ArchiveVerdict prior iter %d: %v (non-fatal)\n",
					state.iterationCount-1, archErr)
			}
		}

		// ── Dispatch reviewer ─────────────────────────────────────────────
		//
		// CHB-009: reviewer ALWAYS mints a fresh claudeSessionID (priorClaudeSessID=nil).
		// Each reviewer is an independent fresh session; the prior implementer's
		// session ID is NEVER passed to the reviewer even when it is known.
		revRC := claudeRunCtx{
			runID:             runID,
			beadID:            string(beadID),
			workspacePath:     wtPath,
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
		}
		revSpec, revArtifacts, revSpecErr := buildClaudeLaunchSpec(ctx, revRC)
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
		revPRS := newPerRunSubstrate(deps.substrate)
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
			result := rlErrorResult(fmt.Sprintf("reviewer launch error at iteration %d: %v", state.iterationCount, revLaunchErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

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
		// When adapterRegistry is nil (test mode with no adapters) skip the guard
		// and fall through to waitWithSocketGrace as before. When ErrAgentReadyTimeout
		// fires: kill, reap, emit rlErrorResult so the caller (workloop) can reopen
		// the bead via the same error envelope shape as the implementer phase.
		_ = revTapCh // suppress lint if adapterRegistry is nil and block is skipped
		if deps.adapterRegistry != nil {
			revAdapter, revAdapterErr := deps.adapterRegistry.ForAgent(core.AgentTypeClaudeCode)
			if revAdapterErr != nil {
				// No adapter for claude-code — non-fatal; skip ready-wait.
				fmt.Fprintf(os.Stderr, "daemon: reviewloop: ForAgent(claude-code) bead %s iter %d: %v (skipping ready-wait)\n",
					beadID, state.iterationCount, revAdapterErr)
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
		}

		// Paste-inject the reviewer kick-off message AFTER agent_ready (hk-zchbu).
		// Running before agent_ready races Claude's welcome splash, which
		// consumes the trailing \n and leaves the buffered text unsubmitted.
		// hk-012af: use revPasteTarget (per-run wrapper) so inject targets this
		// reviewer's pane rather than the shared "last pane".
		// Spec ref: specs/process-lifecycle.md §4.7 PL-021d.
		go pasteInjectOnLaunch(ctx, revPasteTarget, revArtifacts.claudeSessionID,
			handlercontract.ReviewLoopPhaseReviewer, state.iterationCount, wtPath)

		// Wait for reviewer using waitWithSocketGrace (OQ2 resolution).
		_, revEI := waitWithSocketGrace(ctx, deps.hookStore, revWatcher, revSess,
			runID.String(), revArtifacts.claudeSessionID)
		_ = revEI

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
		verdict, verdictErr := workspace.ReadReviewVerdict(wtPath)
		if verdictErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: reviewloop: ReadReviewVerdict iter %d: %v\n", state.iterationCount, verdictErr)
			result := rlErrorResult(fmt.Sprintf("verdict malformed at iteration %d: %v", state.iterationCount, verdictErr))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}
		if verdict == nil {
			fmt.Fprintf(os.Stderr, "daemon: reviewloop: verdict absent at iteration %d\n", state.iterationCount)
			result := rlErrorResult(fmt.Sprintf("verdict absent at iteration %d", state.iterationCount))
			emitReviewLoopCycleComplete(ctx, deps.bus, runID, state.iterationCount, result.completionReason)
			return result
		}

		// Emit reviewer_verdict (verbatim agent-reviewer schema v1 fields per EM-015d).
		emitReviewerVerdict(ctx, deps.bus, runID, revSessionID, state.claudeSessionID, state.iterationCount, verdict)

		// Archive this iteration's verdict to review.iter-N.json per EM-015d.
		if archErr := workspace.ArchiveVerdict(wtPath, state.iterationCount); archErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: reviewloop: ArchiveVerdict iter %d: %v (non-fatal)\n",
				state.iterationCount, archErr)
		}

		// Record verdict notes for use as prior_verdict_summary on next implementer resume.
		state.lastVerdictNotes = verdict.Notes

		// ── Route on verdict ──────────────────────────────────────────────
		switch verdict.Verdict {
		case workspace.ReviewVerdictApprove:
			result := reviewLoopResult{
				success:          true,
				completionReason: core.ReviewLoopCompletionReasonApproved,
				summary:          fmt.Sprintf("APPROVE at iteration %d", state.iterationCount),
				needsAttention:   false,
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

// rlSynthesiseClaudeSessionID produces a synthetic session ID for the MVH twin-
// binary case where the subprocess does not emit `--output-format json` stdout.
//
// Post-MVH: replace with handlercontract.ParseClaudeSessionID on the session's
// captured stdout buffer once the handler exposes it.
func rlSynthesiseClaudeSessionID() string {
	return "synthetic-claude-session-" + time.Now().UTC().Format("20060102T150405Z")
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
