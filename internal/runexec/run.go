package runexec

import (
	"context"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/substrate"
)

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

func stepRunResolving(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvStartRun:
		s.Phase = RunProvisioning
		s.Mode = ev.Mode
		return s, []Action{{Kind: ActCreateWorktree}}
	case EvProvisionFailed:
		return finalizeReopen(cfg, s, nil, ev.Reason, ev.Detail)
	default:
		return s, nil
	}
}

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

func stepRunDispatching(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvModeOutcome:
		return stepRunModeOutcome(cfg, s, ev)
	case EvAgentCompleted, EvCleanExit:
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

func stepRunModeOutcome(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.ModeOutcome {
	case ModeSuccess:
		if ev.PathLabel != "" {
			s.PathLabel = ev.PathLabel
		}
		if ev.Detail != "" {
			s.PathCloseSummary = ev.Detail
		}
		s.Phase = RunGating
		return s, []Action{{Kind: ActRunGate}}
	case ModeSubsumed, ModeNoChange:
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
		return finalizeReopen(cfg, s, nil, ev.Reason, ev.Detail)
	}
}

func stepRunGuarding(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvNoCommitGuardReopen:
		return finalizeReopen(cfg, s, nil, ev.Reason, ev.Reason)
	case EvGuardsPassed:
		s.Phase = RunGating
		return s, []Action{{Kind: ActRunGate}}
	default:
		return s, nil
	}
}

func stepRunGating(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	switch ev.Kind {
	case EvGatePassed:
		s.Phase = RunMerging
		return s, []Action{{Kind: ActPrepareMerge}}
	case EvGateFailed:
		if ev.Reason != "" {
			prefix := []Action{{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "rejected"}}
			return finalizeReopen(cfg, s, prefix, ev.Reason, ev.Reason)
		}
		return finalizeReopen(cfg, s, nil, "", "")
	default:
		return s, nil
	}
}

func stepRunMerging(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	if ev.Kind != EvMergeResult {
		return s, nil
	}
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

func mergeExhaustedOrFatal(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	if ev.AlreadyApprovedOnMain {
		return finalizeClose(cfg, s, cfg.CloseSummary, mergeCloseTransient(cfg, s), cfg.EmitOutcome)
	}
	prefix := []Action{{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "rejected"}}
	reason, summary := mergeFailureStrings(s, ev)
	return finalizeReopen(cfg, s, prefix, reason, summary)
}

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

func stepRunFinalizing(cfg RunConfig, s RunState, ev Event) (RunState, []Action) {
	if ev.Kind != EvCloseResult {
		return s, nil
	}
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

func mergeCloseTransient(cfg RunConfig, s RunState) string {
	if s.SingleShotLabel && s.PathLabel != "" {
		return "close-transient-merged (" + s.PathLabel + ")"
	}
	return cfg.BrUnavailableSummary
}

func drainReopen(s RunState) (RunState, []Action) {
	s.Phase = RunDone
	s.FinalizeMode = FinalizeReopen
	s.DoneOutcome = "reopened"
	s.Success = false
	s.Draining = true
	return s, []Action{{Kind: ActReopenBead, Reason: drainRequeueReason}}
}

func doneClosed(s RunState, summary string, success bool) (RunState, []Action) {
	s.Phase = RunDone
	s.FinalizeMode = FinalizeClose
	s.DoneOutcome = "closed"
	s.Success = success
	return s, []Action{{Kind: ActEmitRunTerminal, Success: success, Summary: summary}}
}

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
