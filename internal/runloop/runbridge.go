// runbridge.go — the RT7 beadRunOne composition root for the pure Run reactor
// (internal/runexec; RSM-007, RSM-031..035). runBridge owns the per-run Run
// machine + runShell pair and the shell-side event synthesis: beadRunOne feeds
// it shell-classified events (provisioning failures, the dispatch-terminal
// classes, the frozen-commit watchdog outcome) and the machine drives the
// reopen/close terminal spine through the effector hooks wired here.
//
// Spec: specs/run-state-machine.md §12 Amendment A1. Design:
// .kerf/works/2026-07-14-run-state-machine/04-design/rt7-single-mode-failure-mapping.md.

package runloop

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runmerge"
)

// RunBridge binds one bead run's pure Run machine to the live daemon effects.
// Single-goroutine-owned (the run's own goroutine), like the shell it wraps.
type RunBridge struct {
	m  *runexec.Run
	sh *RunShell

	env     RunEnv
	rp      RunPorts
	handles SharedHandles
	runID   core.RunID
	beadID  core.BeadID

	// rejectReason carries the classified failure reason for the
	// outcome_emitted=rejected prefix emission (RSM-034): the machine's ActEmit
	// carries only the "rejected" payload key; the shell resolves the reason.
	rejectReason string

	// draining marks the shutdown-drain batch (RSM-021): the terminal-emission
	// effector uses a background context and skips sessiondata for it, and the
	// close hook composes the drain close-error string.
	draining bool
}

// runBridgeConfig maps a workflow mode onto its RunConfig divergence parameters
// (RSM-020): single has no merge retry and composes its transient from the
// latched path label; DOT retries the merge step (1 + maxMergeStepRetries=2)
// with its success transient.
//
// DOT INHERITED the 3-attempt merge budget from the retired review-loop mode
// (hk-f9xzs), deliberately rather than by omission. The reasons the review-loop
// had it apply verbatim to DOT: the retryable reasons runmerge.IsRetryableReason
// classifies — rebase_conflict, non_ff_merge, merge_fmt_failed — are artifacts of
// CONCURRENT merge-to-main, not properties of a workflow shape. They get MORE
// likely as concurrency rises, not less, and DOT runs under the same
// --max-concurrent as review-loop did. Letting the budget lapse to 1 when
// review-loop was deleted would have made the surviving default mode strictly
// less robust at the merge step than the mode it replaced — a regression
// introduced by a deletion, which is the failure this port exists to avoid.
//
// single keeps its single attempt (A1 §3), unchanged and out of scope here.
func runBridgeConfig(mode core.WorkflowMode) runexec.RunConfig {
	cfg := runexec.RunConfig{
		Mode:             string(mode),
		MaxMergeAttempts: 1,
		EmitOutcome:      true,
	}
	switch mode {
	case core.WorkflowModeDot:
		cfg.MaxMergeAttempts = 3 // initial + maxMergeStepRetries (hk-f9xzs)
		cfg.BrUnavailableSummary = "close-transient-merged (dot success)"
	default: // single: A1 §3 — no retry, label-composed transient.
	}
	return cfg
}

// NewRunBridge constructs the Run machine + shell over the base effector hooks
// available at claim time (reopen, run-terminal, worktree-confirm, emissions).
// The terminal-spine hooks are wired later via WireSpine, once the merge-window
// context is in scope. emitRunTerminal is beadRunOne's terminal-emission
// effector (queue stamping + sessiondata policy; draining selects the RSM-021
// no-sessiondata batch policy).
func NewRunBridge(env RunEnv, rp RunPorts, handles SharedHandles, runID core.RunID, beadID core.BeadID, mode core.WorkflowMode, emitRunTerminal func(ctx context.Context, success bool, summary string, draining bool)) *RunBridge {
	b := &RunBridge{
		env:     env,
		rp:      rp,
		handles: handles,
		runID:   runID,
		beadID:  beadID,
		m:       runexec.NewRun(runBridgeConfig(mode)),
	}
	b.sh = NewRunShell(rp.Clock, RunEffectors{
		ReopenBead: b.reopenBead,
		EmitRunTerminal: func(c context.Context, success bool, summary string) {
			emitRunTerminal(c, success, summary, b.draining)
		},
		CreateWorktree: func(context.Context) []runexec.Event {
			// The worktree is provisioned imperatively in beadRunOne (shared
			// across all modes); by the time the machine starts it exists.
			return []runexec.Event{{Kind: runexec.EvProvisioned}}
		},
		Emit: b.emit,
	}, nil)
	return b
}

// Success reports the Run terminal's success outcome (RSM-022): false until the
// machine reaches Done{closed, success}. The goroutine wrapper and the
// worktree-retention defers read this instead of the removed out-param.
func (b *RunBridge) Success() bool { return b.m.State().Success }

// SetRejectReason supplies the outcome_emitted=rejected reason for a
// caller-classified terminal path.
func (b *RunBridge) SetRejectReason(reason string) { b.rejectReason = reason }

// reopenBead is the ActReopenBead hook. RSM-022: a terminal reopen must not
// silently no-op under a cancelled per-run ctx (stale-watcher abort, hk-e3fy)
// — fall back to Background.
func (b *RunBridge) reopenBead(c context.Context, reason string) {
	rctx := c
	if rctx.Err() != nil {
		rctx = context.WithoutCancel(c)
	}
	tid, tidErr := b.handles.TIDGen.Next()
	if tidErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: tidGen.Next (run reopen) bead %s: %v\n", b.beadID, tidErr)
	}
	if reopenErr := b.rp.Ledger.ReopenBead(rctx, b.runID, tid, b.beadID, reason); reopenErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: ReopenBead FAILED bead %s run %s: %v — bead is stuck in_progress; operator must reopen manually (hk-s20z)\n",
			b.beadID, b.runID.String(), reopenErr)
	}
}

// emit is the ActEmit hook. run_started and the escaped-worktree event are
// emitted imperatively in beadRunOne (the former pre-mode-switch for all
// modes, the latter with its full dirty-file payload at the guard), so both
// are no-ops here; outcome_emitted resolves the RSM-034 rejected reason.
func (b *RunBridge) emit(c context.Context, typ core.EventType, detail string) {
	if typ != core.EventTypeOutcomeEmitted {
		return
	}
	reason := ""
	if detail == "rejected" {
		reason = b.rejectReason
	}
	runmerge.EmitOutcomeEmitted(c, b.rp.Emitter, b.runID, b.beadID, detail, reason)
}

// Feed stamps + feeds one shell-classified event and drains the synchronous
// follow-ups; a terminal-class feed lands the machine in Done within the same
// call (the spine is fully synchronous port I/O).
func (b *RunBridge) Feed(ctx context.Context, ev runexec.Event) {
	if ev.At.IsZero() {
		ev.At = b.rp.Clock.Now()
	}
	b.sh.feed(ctx, b.m, ev)
	for b.m.InFlight() && b.sh.drainPending(ctx, b.m) {
	}
}

// Start enters the machine's Dispatching phase (Resolving → Provisioning →
// Dispatching; the createWorktree hook confirms the already-provisioned
// worktree). Called immediately before Launch so every dispatch-phase failure
// rides EvModeOutcome{failure} (RSM-031).
func (b *RunBridge) Start(ctx context.Context, mode core.WorkflowMode) {
	b.Feed(ctx, runexec.Event{Kind: runexec.EvStartRun, Mode: string(mode)})
}

// Fail routes a shell-classified run failure onto the machine's reopen spine:
// EvProvisionFailed before dispatch (RSM-032), EvModeOutcome{failure} after
// (RSM-031). reason is the ReopenBead string; summary the run terminal's.
func (b *RunBridge) Fail(ctx context.Context, reason, summary string) {
	kind := runexec.EvProvisionFailed
	if phase := b.m.State().Phase; phase != runexec.RunResolving && phase != runexec.RunProvisioning {
		kind = runexec.EvModeOutcome
	}
	b.Feed(ctx, runexec.Event{Kind: kind, ModeOutcome: runexec.ModeFailure, Reason: reason, Detail: summary})
}

// SpineArgs is the merge-window context the terminal spine needs; it exists
// only at the point beadRunOne has resolved the launch + merge coordinates,
// hence the two-stage wiring. The RT9 per-mode divergence parameters (RSM-020:
// gate presence, trailer amend/re-amend policy, retry classification, the DOT
// rebase-dropped carve-out) ride here as optional hooks — nil keeps the
// single-mode behavior.
type SpineArgs struct {
	RunRunner       tmuxpkg.CommandRunner // remote SSH runner; nil ⇒ box-A-local
	WTPath          string
	HeadSHA         string // parent SHA the run branched from
	PreMergeSync    func() string
	MPort           MergePort
	ActiveRepo      string
	ProtectBranches []string
	TransitionTID   core.TransitionID
	EmitBeadClosed  func(context.Context)

	// MergeTarget is the per-bead integration branch the run-branch must LAND
	// on (hk-lgykq): the resolved baseBranch (lands_on) carrying the three-tier
	// precedence (bead ## Branching > branching.yaml > default), equal to
	// env.TargetBranch when no per-bead override is present. Empty only when
	// resolveBranching errored; the merge call sites fall back to
	// env.TargetBranch in that case so the merge is never directed at an empty
	// ref (mergeRunBranchToMain fail-closes on empty target). Threaded into the
	// mergeRunBranchToMain calls in mergeHook / drainMergeHook, superseding the
	// daemon-wide env.TargetBranch the run was formerly merged into.
	MergeTarget string

	// SkipGate short-circuits the gate to a pass: the DOT cascade runs its gate
	// inside the graph (commit_gate tool node), not as a post-mode gate.
	SkipGate bool
	// AmendTrailers stamps the Reviewed-By/Review-Verdict trailers on HEAD
	// before a merge attempt (hk-dyim / hk-tnui); retry is the 0-based retry
	// count (0 = the initial attempt, >0 = the per-retry re-amend, RF :3899).
	// nil ⇒ no trailer amend (single mode).
	AmendTrailers func(ctx context.Context, retry int)
	// Retryable classifies a merge-failure reason as retryable under the
	// mode's merge-retry budget (review-loop: isRetryableMergeReason,
	// hk-f9xzs). nil ⇒ every failure is fatal.
	Retryable func(reason string) bool
	// CarveOut reports the DOT already-approved-on-main fall-through for a
	// merge-failure reason (hk-whru3 / hk-vbv3b). nil ⇒ never.
	CarveOut func(reason string) bool
}

// WireSpine binds the terminal-spine hooks (gate → code-sync → merge → close
// ladder) onto the machine. The single-shot escape + no-commit guards stay
// imperative in beadRunOne and run BEFORE the dispatch-terminal classification
// for EVERY class (the pre-RT7 order); by the time the machine traverses
// Guarding they are known-green, so checkEscape is a recorded pass.
func (b *RunBridge) WireSpine(a SpineArgs) {
	b.sh.eff.CheckEscape = func(context.Context) []runexec.Event {
		return []runexec.Event{{Kind: runexec.EvGuardsPassed}}
	}
	b.sh.eff.RunGate = b.gateHook(a)
	b.sh.eff.PrepareMerge = b.mergeHook(a)
	b.sh.eff.SubmitMerge = b.drainMergeHook(a)
	b.sh.eff.CloseBead = b.closeHook(a)
}

// gateHook runs the scenario gate (hk-i2ie5). REMOTE: routed via RunRunner so
// the gate runs on the worker (nil ⇒ local). SkipGate (DOT) records a pass.
func (b *RunBridge) gateHook(a SpineArgs) func(context.Context) []runexec.Event {
	return func(c context.Context) []runexec.Event {
		if a.SkipGate {
			return []runexec.Event{{Kind: runexec.EvGatePassed}}
		}
		if sgr := runScenarioGateIfNeededVia(c, a.RunRunner, a.WTPath, a.HeadSHA); sgr.blocked {
			b.rejectReason = sgr.reason
			return []runexec.Event{{Kind: runexec.EvGateFailed, Reason: sgr.reason}}
		}
		return []runexec.Event{{Kind: runexec.EvGatePassed}}
	}
}

// mergeHook runs the DD1 code-sync (remote-substrate B8, first attempt only)
// then the §4.12.EM-052 merge; the staged result feeds the machine (RSM-033
// rows 5–8). It owns the per-attempt shell policy: the trailer amend before
// each attempt (hk-dyim; per-retry re-amend RF :3899), the retry-progress log
// line, and the Retryable / CarveOut classification the machine's
// merge-retry budget consumes (RSM-019).
func (b *RunBridge) mergeHook(a SpineArgs) func(context.Context) {
	attempt := 0
	lastReason := ""
	return func(c context.Context) {
		attempt++
		if attempt == 1 {
			if syncReason := a.PreMergeSync(); syncReason != "" {
				b.rejectReason = syncReason
				b.sh.pending = append(b.sh.pending, runexec.Event{
					Kind: runexec.EvMergeResult, Merge: runexec.MergeFatal,
					MergeStage: runexec.MergeStageCodeSync, MergeReason: syncReason,
				})
				return
			}
		} else {
			// §4.12 merge-step retry (hk-f9xzs): log progress with the prior
			// attempt's failure reason, then re-amend the trailers (the prior
			// inner rebase may have rewritten HEAD).
			fmt.Fprintf(os.Stderr, "daemon: workloop: merge-step retry %d/%d (bead %s): %s\n",
				attempt-1, b.m.State().MergeAttempt+1, b.beadID, lastReason)
		}
		if a.AmendTrailers != nil {
			a.AmendTrailers(c, attempt-1)
		}
		// hk-lgykq: land on the per-bead integration branch (MergeTarget =
		// resolved baseBranch), not the daemon-wide default; fall back to
		// env.TargetBranch when resolveBranching left MergeTarget empty.
		mergeInto := a.MergeTarget
		if mergeInto == "" {
			mergeInto = b.env.TargetBranch
		}
		mergeRes := runmerge.RunBranchToTarget(c, a.MPort.Submit(), a.ActiveRepo, b.runID, b.rp.Emitter, b.beadID, a.HeadSHA, mergeInto, a.ProtectBranches, b.env.BrPath)
		switch {
		case mergeRes.NoChange:
			b.sh.pending = append(b.sh.pending, runexec.Event{Kind: runexec.EvMergeResult, Merge: runexec.MergeNoChange})
		case mergeRes.Success:
			b.sh.pending = append(b.sh.pending, runexec.Event{Kind: runexec.EvMergeResult, Merge: runexec.MergeSuccess})
		default:
			// EM-053: non-FF or push failure → merge-failure classification.
			b.rejectReason = mergeRes.Reason
			lastReason = mergeRes.Reason
			ev := runexec.Event{
				Kind: runexec.EvMergeResult, Merge: runexec.MergeFatal,
				MergeStage: runexec.MergeStageMerge, MergeReason: mergeRes.Reason,
			}
			switch {
			case a.CarveOut != nil && a.CarveOut(mergeRes.Reason):
				ev.AlreadyApprovedOnMain = true // hk-whru3/hk-vbv3b fall-through
			case a.Retryable != nil && a.Retryable(mergeRes.Reason):
				ev.Merge = runexec.MergeRetryable
			}
			b.sh.pending = append(b.sh.pending, ev)
		}
	}
}

// drainMergeHook is the RSM-021 shutdown-drain merge effector: it merges the
// committed run branch under a cancellation-free context (the per-run ctx IS
// cancelled on this path — the pre-RT9 block used context.Background()) and
// feeds the staged result; a failure is logged and routed to the machine's
// drain requeue-reopen row.
func (b *RunBridge) drainMergeHook(a SpineArgs) func(context.Context, string) []runexec.Event {
	return func(c context.Context, _ string) []runexec.Event {
		mctx := c
		if mctx.Err() != nil {
			mctx = context.WithoutCancel(c)
		}
		// hk-lgykq: shutdown-drain merge also lands on the per-bead integration
		// branch (MergeTarget = resolved baseBranch); fall back to the
		// daemon-wide target when MergeTarget is empty.
		mergeInto := a.MergeTarget
		if mergeInto == "" {
			mergeInto = b.env.TargetBranch
		}
		mergeRes := runmerge.RunBranchToTarget(mctx, a.MPort.Submit(), a.ActiveRepo, b.runID, b.rp.Emitter, b.beadID, a.HeadSHA, mergeInto, a.ProtectBranches, b.env.BrPath)
		switch {
		case mergeRes.NoChange:
			return []runexec.Event{{Kind: runexec.EvMergeResult, Merge: runexec.MergeNoChange}}
		case mergeRes.Success:
			return []runexec.Event{{Kind: runexec.EvMergeResult, Merge: runexec.MergeSuccess}}
		default:
			fmt.Fprintf(os.Stderr, "daemon: workloop: shutdown-drain: merge failed for bead %s: %s; reopening for re-dispatch\n",
				b.beadID, mergeRes.Reason)
			return []runexec.Event{{Kind: runexec.EvMergeResult, Merge: runexec.MergeFatal, MergeReason: mergeRes.Reason}}
		}
	}
}

// closeHook performs the RSM-020 close-ladder bead close and classifies the
// return (hk-hypbi: transient BrUnavailable after a successful merge is a
// success; a hard error carries its shell-composed summary, row 12). The
// needs-attention flag rides the ActCloseBead action (the budget-exhausted
// ladder, hk-c1ah6). Drain closes run under a cancellation-free context and
// compose the drain close-error label (RSM-021 effector policy).
func (b *RunBridge) closeHook(a SpineArgs) func(context.Context, string, bool) []runexec.Event {
	return func(c context.Context, _ string, needsAttention bool) []runexec.Event {
		cctx := c
		// RSM-021 drain policy only: the pre-RT9 drain block closed under a
		// background context. Non-drain closes keep the caller ctx untouched.
		if b.draining && cctx.Err() != nil {
			cctx = context.WithoutCancel(c)
		}
		if closeErr := b.rp.Ledger.CloseBead(cctx, b.runID, a.TransitionTID, b.beadID, needsAttention); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead %s: %v\n", b.beadID, closeErr)
			if errors.Is(closeErr, brcli.BrUnavailable) {
				return []runexec.Event{{Kind: runexec.EvCloseResult, Close: runexec.CloseBrUnavailable}}
			}
			label := ""
			if b.draining {
				label = " (shutdown-drain)"
			}
			return []runexec.Event{{
				Kind: runexec.EvCloseResult, Close: runexec.CloseError,
				Detail: fmt.Sprintf("close-error%s: %v", label, closeErr),
			}}
		}
		if a.EmitBeadClosed != nil {
			a.EmitBeadClosed(cctx)
		}
		return []runexec.Event{{Kind: runexec.EvCloseResult, Close: runexec.CloseClosed}}
	}
}

// Drain routes the shutdown-drain terminal edge onto the machine (RSM-021):
// worktreeAheadSHA is the committed run-branch HEAD ("" = no commit / probe
// failed → the requeue-recovery reopen). Sets the drain batch policy consumed
// by the terminal-emission and close effectors.
func (b *RunBridge) Drain(ctx context.Context, worktreeAheadSHA string) {
	b.draining = true
	b.Feed(ctx, runexec.Event{Kind: runexec.EvShutdownDrain, WorktreeAheadSHA: worktreeAheadSHA})
}
