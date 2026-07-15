// runbridge.go — the RT7 beadRunOne composition root for the pure Run reactor
// (internal/runexec; RSM-007, RSM-031..035). runBridge owns the per-run Run
// machine + runShell pair and the shell-side event synthesis: beadRunOne feeds
// it shell-classified events (provisioning failures, the dispatch-terminal
// classes, the frozen-commit watchdog outcome) and the machine drives the
// reopen/close terminal spine through the effector hooks wired here.
//
// Spec: specs/run-state-machine.md §12 Amendment A1. Design:
// .kerf/works/2026-07-14-run-state-machine/04-design/rt7-single-mode-failure-mapping.md.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	tmuxpkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runexec"
)

// runBridge binds one bead run's pure Run machine to the live daemon effects.
// Single-goroutine-owned (the run's own goroutine), like the shell it wraps.
type runBridge struct {
	m  *runexec.Run
	sh *runShell

	deps   workLoopDeps
	rp     RunPorts
	runID  core.RunID
	beadID core.BeadID

	// rejectReason carries the classified failure reason for the
	// outcome_emitted=rejected prefix emission (RSM-034): the machine's ActEmit
	// carries only the "rejected" payload key; the shell resolves the reason.
	rejectReason string
}

// newRunBridge constructs the Run machine + shell over the base effector hooks
// available at claim time (reopen, run-terminal, worktree-confirm, emissions).
// The single-mode terminal-spine hooks are wired later via wireSpine, once the
// merge-window context is in scope. emitDone is beadRunOne's terminal-emission
// closure (queue stamping + runSucceeded bridge, removed in RT9).
func newRunBridge(deps workLoopDeps, rp RunPorts, runID core.RunID, beadID core.BeadID, mode core.WorkflowMode, emitDone func(success bool, summary string)) *runBridge {
	b := &runBridge{
		deps:   deps,
		rp:     rp,
		runID:  runID,
		beadID: beadID,
		m: runexec.NewRun(runexec.RunConfig{
			Mode:             string(mode),
			MaxMergeAttempts: 1, // single-mode has no merge retry loop (A1 §3)
			EmitOutcome:      true,
		}),
	}
	b.sh = newRunShell(deps.clock, runEffectors{
		reopenBead: b.reopenBead,
		emitRunTerminal: func(_ context.Context, success bool, summary string) {
			emitDone(success, summary)
		},
		createWorktree: func(context.Context) []runexec.Event {
			// The worktree is provisioned imperatively in beadRunOne (shared
			// across all modes); by the time the machine starts it exists.
			return []runexec.Event{{Kind: runexec.EvProvisioned}}
		},
		emit: b.emit,
	}, nil)
	return b
}

// reopenBead is the ActReopenBead hook. RSM-022: a terminal reopen must not
// silently no-op under a cancelled per-run ctx (stale-watcher abort, hk-e3fy)
// — fall back to Background.
func (b *runBridge) reopenBead(c context.Context, reason string) {
	rctx := c
	if rctx.Err() != nil {
		rctx = context.WithoutCancel(c)
	}
	tid, tidErr := b.deps.tidGen.Next()
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
func (b *runBridge) emit(c context.Context, typ core.EventType, detail string) {
	if typ != core.EventTypeOutcomeEmitted {
		return
	}
	reason := ""
	if detail == "rejected" {
		reason = b.rejectReason
	}
	emitOutcomeEmitted(c, b.deps.bus, b.runID, b.beadID, detail, reason)
}

// feed stamps + feeds one shell-classified event and drains the synchronous
// follow-ups; a terminal-class feed lands the machine in Done within the same
// call (the spine is fully synchronous port I/O).
func (b *runBridge) feed(ctx context.Context, ev runexec.Event) {
	if ev.At.IsZero() {
		ev.At = b.deps.clock.Now()
	}
	b.sh.feed(ctx, b.m, ev)
	for b.m.InFlight() && b.sh.drainPending(ctx, b.m) {
	}
}

// start enters the machine's Dispatching phase (Resolving → Provisioning →
// Dispatching; the createWorktree hook confirms the already-provisioned
// worktree). Called immediately before Launch so every dispatch-phase failure
// rides EvModeOutcome{failure} (RSM-031).
func (b *runBridge) start(ctx context.Context, mode core.WorkflowMode) {
	b.feed(ctx, runexec.Event{Kind: runexec.EvStartRun, Mode: string(mode)})
}

// fail routes a shell-classified run failure onto the machine's reopen spine:
// EvProvisionFailed before dispatch (RSM-032), EvModeOutcome{failure} after
// (RSM-031). reason is the ReopenBead string; summary the run terminal's.
func (b *runBridge) fail(ctx context.Context, reason, summary string) {
	kind := runexec.EvProvisionFailed
	if phase := b.m.State().Phase; phase != runexec.RunResolving && phase != runexec.RunProvisioning {
		kind = runexec.EvModeOutcome
	}
	b.feed(ctx, runexec.Event{Kind: kind, ModeOutcome: runexec.ModeFailure, Reason: reason, Detail: summary})
}

// spineArgs is the merge-window context the single-mode terminal spine needs;
// it exists only at the point beadRunOne has resolved the launch + merge
// coordinates, hence the two-stage wiring.
type spineArgs struct {
	runRunner       tmuxpkg.CommandRunner // remote SSH runner; nil ⇒ box-A-local
	wtPath          string
	headSHA         string // parent SHA the run branched from
	preMergeSync    func() string
	mport           MergePort
	activeRepo      string
	protectBranches []string
	transitionTID   core.TransitionID
}

// wireSpine binds the single-mode terminal-spine hooks (gate → code-sync →
// merge → close ladder) onto the machine. The escape + no-commit guards stay
// imperative in beadRunOne and run BEFORE the dispatch-terminal classification
// for EVERY class (the pre-RT7 order); by the time the machine traverses
// Guarding they are known-green, so checkEscape is a recorded pass.
func (b *runBridge) wireSpine(a spineArgs) {
	b.sh.eff.checkEscape = func(context.Context) []runexec.Event {
		return []runexec.Event{{Kind: runexec.EvGuardsPassed}}
	}
	b.sh.eff.runGate = b.gateHook(a)
	b.sh.eff.prepareMerge = b.mergeHook(a)
	b.sh.eff.closeBead = b.closeHook(a)
}

// gateHook runs the scenario gate (hk-i2ie5). REMOTE: routed via runRunner so
// the gate runs on the worker (nil ⇒ local).
func (b *runBridge) gateHook(a spineArgs) func(context.Context) []runexec.Event {
	return func(c context.Context) []runexec.Event {
		if sgr := runScenarioGateIfNeededVia(c, a.runRunner, a.wtPath, a.headSHA); sgr.blocked {
			b.rejectReason = sgr.reason
			return []runexec.Event{{Kind: runexec.EvGateFailed, Reason: sgr.reason}}
		}
		return []runexec.Event{{Kind: runexec.EvGatePassed}}
	}
}

// mergeHook runs the DD1 code-sync (remote-substrate B8) then the
// §4.12.EM-052 merge; the staged result feeds the machine (RSM-033 rows 5–8).
func (b *runBridge) mergeHook(a spineArgs) func(context.Context) {
	return func(c context.Context) {
		if syncReason := a.preMergeSync(); syncReason != "" {
			b.rejectReason = syncReason
			b.sh.pending = append(b.sh.pending, runexec.Event{
				Kind: runexec.EvMergeResult, Merge: runexec.MergeFatal,
				MergeStage: runexec.MergeStageCodeSync, MergeReason: syncReason,
			})
			return
		}
		mergeRes := mergeRunBranchToMain(c, a.mport.Submit(), a.activeRepo, b.runID, b.deps.bus, b.beadID, a.headSHA, b.deps.targetBranch, a.protectBranches, b.deps.brPath)
		switch {
		case mergeRes.noChange:
			b.sh.pending = append(b.sh.pending, runexec.Event{Kind: runexec.EvMergeResult, Merge: runexec.MergeNoChange})
		case mergeRes.success:
			b.sh.pending = append(b.sh.pending, runexec.Event{Kind: runexec.EvMergeResult, Merge: runexec.MergeSuccess})
		default:
			// EM-053: non-FF or push failure → terminal-class merge failure.
			b.rejectReason = mergeRes.reason
			b.sh.pending = append(b.sh.pending, runexec.Event{
				Kind: runexec.EvMergeResult, Merge: runexec.MergeFatal,
				MergeStage: runexec.MergeStageMerge, MergeReason: mergeRes.reason,
			})
		}
	}
}

// closeHook performs the RSM-020 close-ladder bead close and classifies the
// return (hk-hypbi: transient BrUnavailable after a successful merge is a
// success; a hard error carries its shell-composed summary, row 12).
func (b *runBridge) closeHook(a spineArgs) func(context.Context, string) []runexec.Event {
	return func(c context.Context, _ string) []runexec.Event {
		if closeErr := b.rp.Ledger.CloseBead(c, b.runID, a.transitionTID, b.beadID, false); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: CloseBead %s: %v\n", b.beadID, closeErr)
			if errors.Is(closeErr, brcli.BrUnavailable) {
				return []runexec.Event{{Kind: runexec.EvCloseResult, Close: runexec.CloseBrUnavailable}}
			}
			return []runexec.Event{{
				Kind: runexec.EvCloseResult, Close: runexec.CloseError,
				Detail: fmt.Sprintf("close-error: %v", closeErr),
			}}
		}
		emitBeadClosedAndMaybeEpic(c, b.deps, b.runID, b.beadID)
		return []runexec.Event{{Kind: runexec.EvCloseResult, Close: runexec.CloseClosed}}
	}
}
