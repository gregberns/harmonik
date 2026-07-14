package runexec

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

// run.go — the PURE per-bead-run Run reactor (RSM-007/008/009, RSM-020..022;
// runexec-design §4). It is the terminal spine: the launch→gate→merge→close
// tail exists ONCE here (the four open-coded blocks + duplicated close ladder it
// replaces collapse onto `finalizeClose` / `finalizeReopen`). Every behavioral
// divergence among the former blocks survives as an explicit RunConfig parameter
// (RSM-020); all observable summary/reason strings are preserved as data.
//
// States (RSM-007):
//   Resolving → Provisioning → Dispatching → [Guarding] → Gating → Merging →
//     Finalizing → Done{closed | reopened}
// Guarding (RSM-008) is entered ONLY by the single-shot path (post-exit escape +
// no-commit guards); review-loop / DOT skip it. Every pre-launch failure routes
// to Finalizing(reopen) so the spine — not scattered returns — owns every
// reopen/emit pairing (RSM-009). stepRun is TOTAL and pure (RSM-001).

// RunPhase is the Run machine's state (RSM-007).
type RunPhase string

// The Run phases. Done is the sole terminal (RSM-003); its outcome is carried in
// RunState.DoneOutcome.
const (
	RunResolving    RunPhase = "resolving"
	RunProvisioning RunPhase = "provisioning"
	RunDispatching  RunPhase = "dispatching"
	RunGuarding     RunPhase = "guarding"
	RunGating       RunPhase = "gating"
	RunMerging      RunPhase = "merging"
	RunFinalizing   RunPhase = "finalizing"
	RunDone         RunPhase = "done"
)

// FinalizeMode records which ladder the run is finalizing on.
type FinalizeMode string

// The two finalize modes (RSM-020 close ladder vs the reopen spine).
const (
	FinalizeClose  FinalizeMode = "close"
	FinalizeReopen FinalizeMode = "reopen"
)

// RunConfig carries the per-run divergence parameters (RSM-020): each former
// close-ladder / terminal block preserved its behavior as one of these, so the
// single spine reproduces all of them.
type RunConfig struct {
	Mode             string // review_loop | dot | single (RSM-007 fork)
	MaxMergeAttempts int    // per-mode merge-retry cap (RSM-019)
	ReAmendTrailer   bool   // review-loop per-retry trailer re-amend (RF :3899)
	EmitOutcome      bool   // outcome-emission flag (RSM-020)

	// Close-ladder strings (RSM-020: observable strings preserved as data).
	CloseSummary         string // the approved-close SummaryRef
	BrUnavailableSummary string // the close success-transient string (RF §6)
	NoMergeCloseSummary  string // subsumed / no-change no-merge close (RF §6)
	ReopenReason         string // the reopen reason template
}

// RunState is the full reactor state (RSM). All timestamps are event-At-sourced.
type RunState struct {
	Phase        RunPhase
	Mode         string
	MergeAttempt int // 0-based count of merge attempts already made

	// Finalizing / Done detail.
	FinalizeMode FinalizeMode
	DoneOutcome  string // "closed" | "reopened" (set at Done)
	Success      bool   // terminal run success (read by the shell, RSM-022)

	// Drain (RSM-021: the shutdown-drain terminal edge).
	Draining bool
}

func (s RunState) clone() RunState { return s }

// Run is the pure per-run reactor. Not safe for concurrent use.
type Run struct {
	cfg   RunConfig
	state RunState
}

// NewRun constructs the reactor in Resolving.
func NewRun(cfg RunConfig) *Run {
	return &Run{cfg: cfg, state: RunState{Phase: RunResolving}}
}

// Step advances the machine.
func (m *Run) Step(ev Event) []Action {
	next, actions := stepRun(m.cfg, m.state, ev)
	m.state = next
	return actions
}

// State returns a copy of the current reactor state.
func (m *Run) State() RunState { return m.state.clone() }

// InFlight reports whether the run has not yet reached Done.
func (m *Run) InFlight() bool { return m.state.Phase != RunDone }

// Run drives the reactor from a substrate EventSource into an Effector.
func (m *Run) Run(ctx context.Context, src substrate.EventSource[Event], eff substrate.Effector[Action]) error {
	return substrate.Run(ctx, src, m.Step, eff)
}

// stepRun is the total pure transition. Done has no outgoing edges (RSM-003).
func stepRun(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch s.Phase {
	case RunResolving:
		return stepRunResolving(cfg, s, ev)
	case RunProvisioning:
		return stepRunProvisioning(cfg, s, ev)
	case RunDispatching:
		return stepRunDispatching(cfg, s, ev)
	case RunGuarding:
		return stepRunGuarding(cfg, s, ev)
	case RunGating:
		return stepRunGating(cfg, s, ev)
	case RunMerging:
		return stepRunMerging(cfg, s, ev)
	case RunFinalizing:
		return stepRunFinalizing(cfg, s, ev)
	default: // RunDone — terminal
		return s, nil
	}
}

// stepRunResolving: the shell runs prepareRun guards sequentially (RF P2–P4). A
// guard pass (EvStartRun) provisions the worktree; any guard failure feeds
// EvProvisionFailed and routes to the reopen spine (RSM-009).
func stepRunResolving(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvStartRun:
		s.Phase = RunProvisioning
		s.Mode = ev.Mode
		return s, []Action{{Kind: ActCreateWorktree}}
	case EvProvisionFailed:
		return finalizeReopen(cfg, s, nil)
	default:
		return s, nil
	}
}

// stepRunProvisioning: worktree creation resolves to run_started (RF :3742
// position preserved) or the reopen spine.
func stepRunProvisioning(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvProvisioned:
		s.Phase = RunDispatching
		return s, []Action{{Kind: ActEmit, Type: core.EventTypeRunStarted}}
	case EvProvisionFailed:
		return finalizeReopen(cfg, s, nil)
	default:
		return s, nil
	}
}

// stepRunDispatching: the workflow-mode fork drives one or more Dispatch
// instances shell-side; the Run machine consumes their terminal as a mode
// outcome (review-loop/DOT) or a single-mode dispatch terminal → Guarding.
func stepRunDispatching(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvModeOutcome:
		return stepRunModeOutcome(cfg, s, ev)
	case EvAgentCompleted, EvCleanExit:
		// Single-mode dispatch terminals → the escape + no-commit guards run in
		// Guarding, mutually exclusive with the merge section (RSM-008).
		s.Phase = RunGuarding
		return s, []Action{{Kind: ActCheckEscape}}
	case EvShutdownDrain:
		// RSM-021: the shutdown-drain terminal edge — background context, no gate,
		// no pre-merge-sync, direct submit.
		s.Phase = RunMerging
		s.Draining = true
		return s, []Action{{Kind: ActSubmitMerge, Label: ev.WorktreeAheadSHA}}
	default:
		return s, nil
	}
}

// stepRunModeOutcome routes a review-loop / DOT sub-driver return (RF §3 table).
func stepRunModeOutcome(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.ModeOutcome {
	case ModeSuccess:
		s.Phase = RunGating
		return s, []Action{{Kind: ActRunGate}}
	case ModeSubsumed, ModeNoChange:
		// The 2 no-merge close-ladder entries (RF §6): close without a merge.
		return finalizeClose(cfg, s, cfg.NoMergeCloseSummary, false)
	case ModeBudget:
		// Budget policy (shell-computed): close-needs-attention vs reopen — the
		// CHOICE is the machine's (runexec-design §7 note 4).
		if ev.NeedsAttention {
			return finalizeClose(cfg, s, cfg.CloseSummary, cfg.EmitOutcome)
		}
		return finalizeReopen(cfg, s, nil)
	default: // ModeFailure
		return finalizeReopen(cfg, s, nil)
	}
}

// stepRunGuarding: the single-shot post-exit guards (RSM-008). Both guard
// failures route to the reopen spine; a pass proceeds to Gating.
func stepRunGuarding(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvEscapeDetected:
		return finalizeReopen(cfg, s, []Action{
			{Kind: ActEmit, Type: core.EventTypeImplementerEscapedWorktree, Detail: ev.Reason},
		})
	case EvNoCommitGuardReopen:
		return finalizeReopen(cfg, s, nil)
	case EvGuardsPassed:
		s.Phase = RunGating
		return s, []Action{{Kind: ActRunGate}}
	default:
		return s, nil
	}
}

// stepRunGating: the scenario/DOT gate result.
func stepRunGating(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvGatePassed:
		s.Phase = RunMerging
		return s, []Action{{Kind: ActPrepareMerge}}
	case EvGateFailed:
		return finalizeReopen(cfg, s, nil)
	default:
		return s, nil
	}
}

// stepRunMerging: the merge critical-section result, with the FIFO merge-retry
// budget (RSM-019) and the DOT already-approved-on-main carve-out (RF :4138).
func stepRunMerging(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	if ev.Kind != EvMergeResult {
		return s, nil
	}
	switch ev.Merge {
	case MergeSuccess, MergeNoChange:
		return finalizeClose(cfg, s, cfg.CloseSummary, cfg.EmitOutcome)
	case MergeRetryable:
		if s.MergeAttempt+1 < cfg.MaxMergeAttempts {
			s.MergeAttempt++
			actions := []Action{{Kind: ActPrepareMerge}}
			if cfg.ReAmendTrailer {
				actions = append(actions, Action{Kind: ActReAmendTrailer})
			}
			return s, actions
		}
		return mergeExhaustedOrFatal(cfg, s, ev)
	default: // MergeFatal
		return mergeExhaustedOrFatal(cfg, s, ev)
	}
}

// mergeExhaustedOrFatal handles an exhausted-retryable or fatal merge: the DOT
// already-approved-on-main carve-out closes (RF :4138–:4151); otherwise the run
// emits a rejected outcome, reopens, and fails (RSM-019).
func mergeExhaustedOrFatal(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	if ev.AlreadyApprovedOnMain {
		return finalizeClose(cfg, s, cfg.CloseSummary, false)
	}
	prefix := []Action{{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "rejected"}}
	return finalizeReopen(cfg, s, prefix)
}

// stepRunFinalizing: the close ladder's second half — the LedgerPort close
// return (RSM-020). BrUnavailable preserves the success-transient string
// (RF §6). Reopen never rests here (finalizeReopen goes straight to Done).
func stepRunFinalizing(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	if ev.Kind != EvCloseResult {
		return s, nil
	}
	switch ev.Close {
	case CloseClosed:
		return doneClosed(s, cfg.CloseSummary, true)
	case CloseBrUnavailable:
		return doneClosed(s, cfg.BrUnavailableSummary, true)
	default: // CloseError — the bead close itself failed; still emit the terminal
		return doneClosed(s, cfg.CloseSummary, false)
	}
}

// finalizeClose enters the close ladder: emit the approved outcome (when the
// mode emits one) then request the bead close, resting in Finalizing until the
// EvCloseResult lands. This is the ONE close-ladder tail (RSM-020).
func finalizeClose(_ RunConfig, s RunState, summary string, emitOutcome bool) (RunState, []Action) {
	s.Phase = RunFinalizing
	s.FinalizeMode = FinalizeClose
	var actions []Action
	if emitOutcome {
		actions = append(actions, Action{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "approved"})
	}
	actions = append(actions, Action{Kind: ActCloseBead, Summary: summary})
	return s, actions
}

// doneClosed lands the closed terminal with the run-completed emission (once,
// RSM-020/022).
func doneClosed(s RunState, summary string, success bool) (RunState, []Action) {
	s.Phase = RunDone
	s.FinalizeMode = FinalizeClose
	s.DoneOutcome = "closed"
	s.Success = success
	return s, []Action{{Kind: ActEmitRunTerminal, Success: success, Summary: summary}}
}

// finalizeReopen is the immediate reopen spine (RSM-009/022): the transition
// into it owns the reopen + failed-terminal pairing, then lands Done{reopened}
// in the same Step. Terminal reopens use a background context shell-side so a
// mid-merge-cancelled reopen does not silently no-op (RSM-022). `prefix` carries
// any path-specific emission (escaped-worktree, rejected outcome) that precedes
// the reopen.
func finalizeReopen(cfg RunConfig, s RunState, prefix []Action) (RunState, []Action) {
	s.Phase = RunDone
	s.FinalizeMode = FinalizeReopen
	s.DoneOutcome = "reopened"
	s.Success = false
	actions := append([]Action{}, prefix...)
	actions = append(actions,
		Action{Kind: ActReopenBead, Reason: cfg.ReopenReason},
		Action{Kind: ActEmitRunTerminal, Success: false, Summary: cfg.ReopenReason},
	)
	return s, actions
}
