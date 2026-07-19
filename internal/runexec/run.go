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

	// Single-mode path label + close summary, latched from the dispatch-terminal
	// event (RSM-033): the downstream merge/close strings are label-parameterized
	// and the branch is only known at event time. Empty for review-loop/DOT
	// (config-fallback behavior unchanged).
	PathLabel        string // "agent_completed" | "auto-close" | "noChange-subsumed" | "review-loop" | "dot"…
	PathCloseSummary string // event-carried close-success terminal summary
	// SingleShotLabel marks a PathLabel latched by a single-shot dispatch
	// terminal (EvAgentCompleted / EvCleanExit): those paths compose their
	// BrUnavailable transient from the label; mode-outcome labels (review-loop /
	// DOT, RT9) defer to the config override instead (RSM-020).
	SingleShotLabel bool

	// TransientSummary is the exact BrUnavailable close-transient string for the
	// close ladder in flight, latched at finalizeClose entry (RSM-020: the six
	// pre-change transient strings survive as data — cfg for the mode-level
	// default, path-label composition for the event-latched paths).
	TransientSummary string

	// AttentionClose marks the review-loop budget-exhausted needs-attention close
	// ladder (RSM-020 needs-attention flag): rejected outcome + close(attention),
	// terminal run_failed with the exhausted summary regardless of close result,
	// and a reopen ONLY on a hard (non-BrUnavailable) close error (hk-c1ah6 /
	// hk-hypbi — never re-dispatch past the operator-triage requirement).
	AttentionClose bool

	// Finalizing / Done detail.
	FinalizeMode FinalizeMode
	DoneOutcome  string // "closed" | "reopened" (set at Done)
	Success      bool   // terminal run success (read by the shell, RSM-022)

	// Drain (RSM-021: the shutdown-drain terminal edge).
	Draining bool
}

// The shutdown-drain terminal strings (RSM-021: background context, no gate, no
// pre-merge-sync, no outcome emission, direct run-completed emission, and the
// requeue-recovery reopen reason — preserved as data from the pre-RT9 drain
// block, workloop.go hk-dnrg / hk-ly0hg Fix-1).
const (
	drainCloseSummary     = "shutdown-drain: committed work merged"
	drainTransientSummary = "close-transient-merged (shutdown-drain)"
	drainRequeueReason    = "context_cancelled: daemon shutdown, requeue pending"
)

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
		// RSM-032: the interpolated guard/provisioning strings ride the event.
		return finalizeReopen(cfg, s, nil, ev.Reason, ev.Detail)
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
		return finalizeReopen(cfg, s, nil, ev.Reason, ev.Detail)
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
		// Guarding, mutually exclusive with the merge section (RSM-008). The path
		// label + event-carried close summary are latched here (RSM-033): the
		// downstream merge/close strings are parameterized by which terminal fired.
		// A payload-less event (legacy/RT6 feed) latches nothing, keeping the
		// config-fallback strings — the single-mode shell always sends the payload.
		if ev.Detail != "" {
			if ev.Kind == EvAgentCompleted {
				s.PathLabel = "agent_completed"
			} else {
				s.PathLabel = "auto-close"
			}
			s.SingleShotLabel = true
			s.PathCloseSummary = ev.Detail
		}
		s.Phase = RunGuarding
		return s, []Action{{Kind: ActCheckEscape}}
	case EvShutdownDrain:
		// RSM-021: the shutdown-drain terminal edge — background context, no gate,
		// no pre-merge-sync, direct submit. An empty WorktreeAheadSHA means no
		// commit landed (or the HEAD probe failed): reopen for re-dispatch with
		// the requeue-recovery reason and NO run terminal (QM-002a reverts the
		// item to pending at next startup; hk-ly0hg Fix-1).
		if ev.WorktreeAheadSHA == "" {
			return drainReopen(s)
		}
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
		// RT9 (RSM-020/033): a mode success latches its path label (the
		// merge-window failure strings are label-parameterized: "review-loop" /
		// "dot") and its event-carried close-success terminal summary (the
		// sub-driver's dynamic summary). Payload-less events (RT6 feeds) latch
		// nothing — the config-fallback strings are unchanged.
		if ev.PathLabel != "" {
			s.PathLabel = ev.PathLabel
		}
		if ev.Detail != "" {
			s.PathCloseSummary = ev.Detail
		}
		s.Phase = RunGating
		return s, []Action{{Kind: ActRunGate}}
	case ModeSubsumed, ModeNoChange:
		// RSM-035: a subsumed close with the emit-approved flag carries its close
		// summary on the event and latches a path label so a BrUnavailable close
		// reproduces the transient string — "noChange-subsumed" for the
		// single-mode path, event-overridden for DOT ("dot noChange-subsumed",
		// RT9). Without the flag (DOT subsumed / no-change legacy feed) the RT6
		// no-outcome close is unchanged.
		if ev.ModeOutcome == ModeSubsumed && ev.EmitOutcome {
			label := ev.PathLabel
			if label == "" {
				label = "noChange-subsumed"
			}
			s.PathLabel = label
			s.PathCloseSummary = ev.Detail
			return finalizeClose(cfg, s, ev.Detail, "close-transient-merged ("+label+")", true)
		}
		return finalizeClose(cfg, s, cfg.NoMergeCloseSummary, cfg.BrUnavailableSummary, false)
	case ModeBudget:
		// Budget policy (shell-computed): close-needs-attention vs reopen — the
		// CHOICE is the machine's (runexec-design §7 note 4). The needs-attention
		// ladder (RT9, parity with the pre-RT9 hk-c1ah6 block): rejected outcome
		// → close(needsAttention) → run_failed with the exhausted summary. It
		// deliberately does NOT ride the approved close ladder.
		if ev.NeedsAttention {
			s.Phase = RunFinalizing
			s.FinalizeMode = FinalizeClose
			s.AttentionClose = true
			s.PathCloseSummary = ev.Detail
			return s, []Action{
				{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "rejected"},
				{Kind: ActCloseBead, Summary: ev.Detail, NeedsAttention: true},
			}
		}
		return finalizeReopen(cfg, s, nil, "", "")
	default: // ModeFailure
		// RSM-031/032: the shell-classified failure strings ride the event.
		return finalizeReopen(cfg, s, nil, ev.Reason, ev.Detail)
	}
}

// stepRunGuarding: the single-shot post-exit guards (RSM-008). Both guard
// failures route to the reopen spine; a pass proceeds to Gating.
func stepRunGuarding(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvEscapeDetected:
		return finalizeReopen(cfg, s, []Action{
			{Kind: ActEmit, Type: core.EventTypeImplementerEscapedWorktree, Detail: ev.Reason},
		}, ev.Reason, ev.Reason)
	case EvNoCommitGuardReopen:
		return finalizeReopen(cfg, s, nil, ev.Reason, ev.Reason)
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
		// RSM-034: an event-classified gate failure pairs an
		// outcome_emitted=rejected prefix with the reopen (the shell resolves the
		// classified reason into the emission payload). Empty-reason behavior is
		// the RT6 config fallback, unchanged.
		if ev.Reason != "" {
			prefix := []Action{{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "rejected"}}
			return finalizeReopen(cfg, s, prefix, ev.Reason, ev.Reason)
		}
		return finalizeReopen(cfg, s, nil, "", "")
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
	// Drain batch (RSM-021): merged → the drain close ladder (no outcome
	// emission, the drain summaries); any failure → the requeue-recovery reopen
	// with NO run terminal (parity with the pre-RT9 hk-dnrg drain block).
	if s.Draining {
		switch ev.Merge {
		case MergeSuccess, MergeNoChange:
			s.PathCloseSummary = drainCloseSummary
			return finalizeClose(cfg, s, drainCloseSummary, drainTransientSummary, false)
		default:
			return drainReopen(s)
		}
	}
	switch ev.Merge {
	case MergeSuccess, MergeNoChange:
		return finalizeClose(cfg, s, cfg.CloseSummary, mergeCloseTransient(cfg, s), cfg.EmitOutcome)
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
		// RT9 parity fix: the pre-RT9 DOT carve-out falls through to the SAME
		// approved close ladder as a successful merge (workloop.go pre-RT9
		// :4206–:4217 skips only the reopen; outcome_emitted=approved is still
		// emitted). cfg.EmitOutcome — not a hardcoded false — preserves that
		// stream (RSM-020: all observable strings preserved).
		return finalizeClose(cfg, s, cfg.CloseSummary, mergeCloseTransient(cfg, s), cfg.EmitOutcome)
	}
	prefix := []Action{{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "rejected"}}
	reason, summary := mergeFailureStrings(s, ev)
	return finalizeReopen(cfg, s, prefix, reason, summary)
}

// mergeFailureStrings composes the label-parameterized single-mode merge-window
// failure strings (RSM-033; design-note rows 5–8) — pure string composition of
// preserved templates (RSM-020). Without a latched path label (review-loop/DOT,
// not yet re-driven) it returns empty strings so finalizeReopen keeps the RT6
// config fallback.
func mergeFailureStrings(s RunState, ev Event) (reason, summary string) {
	if s.PathLabel == "" {
		return "", ""
	}
	if ev.MergeStage == MergeStageCodeSync {
		return "code-sync failed (" + s.PathLabel + "): " + ev.MergeReason,
			"code-sync-failed (" + s.PathLabel + "): " + ev.MergeReason
	}
	return "merge-to-main failed: " + ev.MergeReason,
		"merge-failed (" + s.PathLabel + "): " + ev.MergeReason
}

// stepRunFinalizing: the close ladder's second half — the LedgerPort close
// return (RSM-020). BrUnavailable preserves the success-transient string
// (RF §6). Reopen never rests here (finalizeReopen goes straight to Done).
func stepRunFinalizing(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	if ev.Kind != EvCloseResult {
		return s, nil
	}
	// Needs-attention close ladder (RT9, hk-c1ah6): the run terminal is ALWAYS
	// run_failed with the exhausted summary; a transient BrUnavailable leaves the
	// bead in_progress for BI-031 recovery (no reopen, hk-hypbi); a hard close
	// error reopens with the exhausted summary before the terminal.
	if s.AttentionClose {
		switch ev.Close {
		case CloseClosed, CloseBrUnavailable:
			return doneClosed(s, s.PathCloseSummary, false)
		default: // CloseError (hard) — reopen, then the failed terminal.
			s.Phase = RunDone
			s.DoneOutcome = "reopened"
			s.FinalizeMode = FinalizeReopen
			s.Success = false
			return s, []Action{
				{Kind: ActReopenBead, Reason: s.PathCloseSummary},
				{Kind: ActEmitRunTerminal, Success: false, Summary: s.PathCloseSummary},
			}
		}
	}
	// Event/latch-first summaries (RSM-032/033); empty falls back to the RT6
	// config strings so the legacy feeds are byte-unchanged.
	switch ev.Close {
	case CloseClosed:
		if s.PathCloseSummary != "" {
			return doneClosed(s, s.PathCloseSummary, true)
		}
		return doneClosed(s, cfg.CloseSummary, true)
	case CloseBrUnavailable:
		if s.TransientSummary != "" {
			return doneClosed(s, s.TransientSummary, true)
		}
		return doneClosed(s, cfg.BrUnavailableSummary, true)
	default: // CloseError — the bead close itself failed; still emit the terminal
		if ev.Detail != "" {
			return doneClosed(s, ev.Detail, false)
		}
		return doneClosed(s, cfg.CloseSummary, false)
	}
}

// finalizeClose enters the close ladder: emit the approved outcome (when the
// mode emits one) then request the bead close, resting in Finalizing until the
// EvCloseResult lands. This is the ONE close-ladder tail (RSM-020). transient
// is the exact BrUnavailable close-transient string for this ladder (latched
// into state; RSM-020 string preservation).
func finalizeClose(_ RunConfig, s RunState, summary, transient string, emitOutcome bool) (RunState, []Action) {
	s.Phase = RunFinalizing
	s.FinalizeMode = FinalizeClose
	s.TransientSummary = transient
	var actions []Action
	if emitOutcome {
		actions = append(actions, Action{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "approved"})
	}
	actions = append(actions, Action{Kind: ActCloseBead, Summary: summary})
	return s, actions
}

// mergeCloseTransient resolves the BrUnavailable transient string for a
// merge-window close: the mode-level config override when set (review-loop
// "…(review-loop APPROVE)", DOT "…(dot success)"), else the single-mode
// path-label composition, else the RT6 config fallback.
func mergeCloseTransient(cfg RunConfig, s RunState) string {
	if s.SingleShotLabel && s.PathLabel != "" {
		return "close-transient-merged (" + s.PathLabel + ")"
	}
	return cfg.BrUnavailableSummary
}

// drainReopen is the shutdown-drain reopen edge (RSM-021): reopen with the
// requeue-recovery reason and NO run terminal — the bead re-enters the ready
// queue at next startup; a run_failed here would falsely record the outcome.
func drainReopen(s RunState) (RunState, []Action) {
	s.Phase = RunDone
	s.FinalizeMode = FinalizeReopen
	s.DoneOutcome = "reopened"
	s.Success = false
	s.Draining = true
	return s, []Action{{Kind: ActReopenBead, Reason: drainRequeueReason}}
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
// the reopen. `reason`/`summary` are the per-call event-sourced strings
// (RSM-032); empty falls back to cfg.ReopenReason for BOTH, preserving the RT6
// review-loop/DOT behavior.
func finalizeReopen(cfg RunConfig, s RunState, prefix []Action, reason, summary string) (RunState, []Action) {
	if reason == "" {
		reason = cfg.ReopenReason
	}
	if summary == "" {
		summary = cfg.ReopenReason
	}
	s.Phase = RunDone
	s.FinalizeMode = FinalizeReopen
	s.DoneOutcome = "reopened"
	s.Success = false
	actions := append([]Action{}, prefix...)
	actions = append(actions,
		Action{Kind: ActReopenBead, Reason: reason},
		Action{Kind: ActEmitRunTerminal, Success: false, Summary: summary},
	)
	return s, actions
}
