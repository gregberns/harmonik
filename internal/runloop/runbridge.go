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
	PreMergeSync    func(context.Context) string
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
// ladder) onto the machine.
//
// CheckEscape runs NO check. It returns EvGuardsPassed unconditionally, so the
// machine always leaves Guarding for Gating. There is no escaped-worktree guard
// anywhere on the run path — the check that once filled this hook was deleted
// because it never ran for a graph workload, and the project decided not to
// rebuild it. Read the pass as "no guard exists", not as "a guard passed".
func (b *RunBridge) WireSpine(a SpineArgs) {
	b.sh.eff.CheckEscape = func(context.Context) []runexec.Event {
		return []runexec.Event{{Kind: runexec.EvGuardsPassed}}
	}
	b.sh.eff.RunGate = b.gateHook(a)
	b.sh.eff.PrepareMerge = b.mergeHook(a)
	b.sh.eff.SubmitMerge = b.drainMergeHook(a)
	b.sh.eff.CloseBead = b.closeHook(a)
}

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

func (b *RunBridge) mergeHook(a SpineArgs) func(context.Context) {
	attempt := 0
	lastReason := ""
	return func(c context.Context) {
		attempt++
		if attempt == 1 {
			if syncReason := a.PreMergeSync(c); syncReason != "" {
				b.rejectReason = syncReason
				b.sh.pending = append(b.sh.pending, runexec.Event{
					Kind: runexec.EvMergeResult, Merge: runexec.MergeFatal,
					MergeStage: runexec.MergeStageCodeSync, MergeReason: syncReason,
				})
				return
			}
		} else {
			fmt.Fprintf(os.Stderr, "daemon: workloop: merge-step retry %d/%d (bead %s): %s\n",
				attempt-1, b.m.State().MergeAttempt+1, b.beadID, lastReason)
		}
		if a.AmendTrailers != nil {
			a.AmendTrailers(c, attempt-1)
		}
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

func (b *RunBridge) drainMergeHook(a SpineArgs) func(context.Context, string) []runexec.Event {
	return func(c context.Context, _ string) []runexec.Event {
		mctx := context.WithoutCancel(c)
		if syncReason := a.PreMergeSync(mctx); syncReason != "" {
			fmt.Fprintf(os.Stderr, "daemon: workloop: shutdown-drain: sync failed for bead %s: %s; reopening for re-dispatch\n",
				b.beadID, syncReason)
			return []runexec.Event{{Kind: runexec.EvMergeResult, Merge: runexec.MergeFatal, MergeReason: syncReason}}
		}
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

func (b *RunBridge) closeHook(a SpineArgs) func(context.Context, string, bool) []runexec.Event {
	return func(c context.Context, _ string, needsAttention bool) []runexec.Event {
		cctx := c
		if b.draining {
			cctx = context.WithoutCancel(c)
		}
		if closeErr := b.rp.Ledger.CloseBead(cctx, b.runID, a.TransitionTID, b.beadID, needsAttention); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead %s: %v\n", b.beadID, closeErr)
			if b.draining {
				b.reopenBead(cctx, "context_cancelled: daemon shutdown, requeue pending")
				return []runexec.Event{{
					Kind: runexec.EvCloseResult, Close: runexec.CloseError,
					Detail: fmt.Sprintf("close-error (shutdown-drain): %v", closeErr),
				}}
			}
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
