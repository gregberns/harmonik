package runexec

import (
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// vocab.go — the shared flat, JSON-round-trippable Event / Action vocabulary
// for the two pure run-lifecycle reactors (RSM-001; runexec-design §1–§2). One
// vocabulary serves both the per-session Dispatch machine (dispatch.go) and the
// per-run Run machine (run.go); kinds are namespaced by consumer. The shell
// (internal/daemon/runshell.go, landed in RT7) samples I/O into Events and
// executes Actions via an effector — this package performs no I/O, reads no
// clock, and mints no identifiers: every timestamp in reactor state derives
// from an event's shell-stamped At (RSM-001), mirroring internal/keeper/step.go.

// SessionRef identifies one agent session (shell-minted; opaque here).
type SessionRef string

// InputID is the driver-internal monotonic input-sequence id used to correlate
// a delivered input with its Ack (RSM-027; agent-input AIS-003).
type InputID string

// TimerKind names the reactor timers the shell arms/cancels on the machines'
// behalf. Every run-path wall-clock select-deadline is expressed as one of
// these (RSM-014); the frozen commit watchdog (M3-D3) stays shell-side and
// feeds EvNoChangeTimeout / EvHeartbeatStale instead of a reactor timer.
type TimerKind string

// The reactor timer kinds (runexec-design §3, M3-D3).
const (
	TimerAgentReady    TimerKind = "agent_ready"
	TimerInputAck      TimerKind = "input_ack"
	TimerReadyKillReap TimerKind = "ready_kill_reap"
)

// EventKind discriminates the flat Event struct (runexec-design §1).
type EventKind string

// The shell→reactor event vocabulary. Dispatch-consumed and Run-consumed kinds
// share one struct; each machine ignores (explicit no-op, RSM-003) the kinds it
// does not name.
const (
	// Dispatch machine (per agent session).
	EvStartDispatch   EventKind = "start_dispatch" // shell entry (Idle → Launching)
	EvLaunched        EventKind = "launched"
	EvLaunchFailed    EventKind = "launch_failed"
	EvAgentReady      EventKind = "agent_ready"
	EvInputAck        EventKind = "input_ack"
	EvInputRejected   EventKind = "input_rejected"
	EvHeartbeat       EventKind = "heartbeat"
	EvCommitObserved  EventKind = "commit_observed"
	EvOutcomeReceived EventKind = "outcome_received"
	EvAgentExited     EventKind = "agent_exited"
	EvNoChangeTimeout EventKind = "no_change_timeout"
	EvHeartbeatStale  EventKind = "heartbeat_stale"
	EvAborted         EventKind = "aborted"

	// Run machine (per bead run).
	EvStartRun            EventKind = "start_run" // shell entry (guards passed)
	EvProvisioned         EventKind = "provisioned"
	EvProvisionFailed     EventKind = "provision_failed"
	EvModeOutcome         EventKind = "mode_outcome"
	EvAgentCompleted      EventKind = "agent_completed" // single-mode dispatch terminal
	EvCleanExit           EventKind = "clean_exit"      // exit-0 single-mode terminal
	EvEscapeDetected      EventKind = "escape_detected"
	EvNoCommitGuardReopen EventKind = "no_commit_guard_reopen"
	EvGuardsPassed        EventKind = "guards_passed"
	EvGatePassed          EventKind = "gate_passed"
	EvGateFailed          EventKind = "gate_failed"
	EvMergeResult         EventKind = "merge_result"
	EvCloseResult         EventKind = "close_result"
	EvShutdownDrain       EventKind = "shutdown_drain"

	// Shared.
	EvTimerFired EventKind = "timer_fired"
)

// ModeOutcomeClass classifies an EvModeOutcome from a review-loop / DOT
// sub-driver return (runexec-design §1 EvModeOutcome, RF P8).
type ModeOutcomeClass string

// The mode-outcome classes. Budget policy is computed shell-side (under
// BudgetPort) and carried as an event field; the machine owns only the CHOICE
// it drives (runexec-design §7 note 4).
const (
	ModeSuccess  ModeOutcomeClass = "success"
	ModeFailure  ModeOutcomeClass = "failure"
	ModeBudget   ModeOutcomeClass = "budget"    // review-loop-failure budget exhausted
	ModeSubsumed ModeOutcomeClass = "subsumed"  // no-merge close (RF §6)
	ModeNoChange ModeOutcomeClass = "no_change" // no-merge close
)

// MergeOutcomeClass is the merge-queue Submit result taxonomy (RSM-019;
// merge-queue-design §3). retryable carries a reason; the retry cap is a
// per-mode config scalar the Run machine compares MergeAttempt against.
type MergeOutcomeClass string

// The merge-outcome classes.
const (
	MergeSuccess   MergeOutcomeClass = "success"
	MergeNoChange  MergeOutcomeClass = "no_change"
	MergeRetryable MergeOutcomeClass = "retryable"
	MergeFatal     MergeOutcomeClass = "fatal"
)

// MergeStage discriminates WHERE in the merge window an EvMergeResult failure
// occurred (RSM-033): the pre-merge code-sync vs the merge itself. Empty means
// merge (back-compat with the RT6 review-loop/DOT feeds).
type MergeStage string

// The merge stages.
const (
	MergeStageCodeSync MergeStage = "code_sync"
	MergeStageMerge    MergeStage = "merge"
)

// CloseOutcomeClass is the LedgerPort close-return taxonomy (RSM-020 close
// ladder; RF §6). The success-transient string is preserved on br_unavailable.
type CloseOutcomeClass string

// The close-outcome classes.
const (
	CloseClosed        CloseOutcomeClass = "closed"
	CloseBrUnavailable CloseOutcomeClass = "br_unavailable"
	CloseError         CloseOutcomeClass = "error"
)

// InputKind is the payload class of an ActDeliverInput (runexec-design §2).
type InputKind string

// The input kinds delivered to a session.
const (
	InputBrief        InputKind = "brief"
	InputResumePrompt InputKind = "resume_prompt"
	InputQuit         InputKind = "quit"
)

// Event is the flat reactor input. Field population is by Kind; unset fields are
// zero. At is the shell-stamped event time — the sole time source for both
// machines (RSM-001).
type Event struct {
	Kind EventKind `json:"kind"`
	At   time.Time `json:"at"`

	// Session correlation (Dispatch events; some Run events).
	Session SessionRef `json:"session,omitempty"`
	InputID InputID    `json:"input_id,omitempty"`

	// Dispatch payloads.
	Reason   string `json:"reason,omitempty"`    // launch/abort/gate/guard reason
	ExitCode int    `json:"exit_code,omitempty"` // EvAgentExited
	WaitErr  string `json:"wait_err,omitempty"`  // EvAgentExited
	Outcome  string `json:"outcome,omitempty"`   // EvOutcomeReceived
	SHA      string `json:"sha,omitempty"`       // EvCommitObserved

	// Run payloads.
	Mode                  string            `json:"mode,omitempty"`                     // EvModeOutcome / EvStartRun
	ModeOutcome           ModeOutcomeClass  `json:"mode_outcome,omitempty"`             // EvModeOutcome
	NeedsAttention        bool              `json:"needs_attention,omitempty"`          // budget close-vs-reopen
	Detail                string            `json:"detail,omitempty"`                   // verdict/salvage SHA
	Merge                 MergeOutcomeClass `json:"merge,omitempty"`                    // EvMergeResult
	MergeReason           string            `json:"merge_reason,omitempty"`             // retryable/fatal reason
	MergeStage            MergeStage        `json:"merge_stage,omitempty"`              // code_sync | merge (RSM-033)
	EmitOutcome           bool              `json:"emit_outcome,omitempty"`             // subsumed approved-close flag (RSM-035)
	AlreadyApprovedOnMain bool              `json:"already_approved_on_main,omitempty"` // DOT carve-out (RF :4138)
	Close                 CloseOutcomeClass `json:"close,omitempty"`                    // EvCloseResult
	WorktreeAheadSHA      string            `json:"worktree_ahead_sha,omitempty"`       // EvShutdownDrain

	// Shared.
	Timer TimerKind `json:"timer,omitempty"` // EvTimerFired
}

// ActionKind discriminates the flat Action struct (runexec-design §2).
type ActionKind string

// The reactor→effector action vocabulary. Effector mapping + per-action failure
// policy is the shell's concern (runshell.go, RT7).
const (
	// Dispatch actions.
	ActLaunchAgent              ActionKind = "launch_agent"
	ActDeliverInput             ActionKind = "deliver_input"
	ActKillAgent                ActionKind = "kill_agent"
	ActDriveLifecycleTerminated ActionKind = "drive_lifecycle_terminated"

	// Run actions.
	ActCreateWorktree  ActionKind = "create_worktree"
	ActRunGate         ActionKind = "run_gate"
	ActCheckEscape     ActionKind = "check_escape"
	ActPrepareMerge    ActionKind = "prepare_merge"
	ActSubmitMerge     ActionKind = "submit_merge"
	ActReAmendTrailer  ActionKind = "reamend_trailer" // review-loop per-retry re-amend (RF :3899)
	ActCloseBead       ActionKind = "close_bead"
	ActReopenBead      ActionKind = "reopen_bead"
	ActEmitRunTerminal ActionKind = "emit_run_terminal"

	// Shared.
	ActEmit        ActionKind = "emit"
	ActArmTimer    ActionKind = "arm_timer"
	ActCancelTimer ActionKind = "cancel_timer"
)

// Action is the flat reactor output. Field population is by Kind.
type Action struct {
	Kind ActionKind `json:"kind"`

	// Session-targeted (Dispatch).
	Session   SessionRef `json:"session,omitempty"`
	InputID   InputID    `json:"input_id,omitempty"`
	InputKind InputKind  `json:"input_kind,omitempty"` // ActDeliverInput
	SpecRef   string     `json:"spec_ref,omitempty"`   // ActLaunchAgent
	ExitCode  int        `json:"exit_code,omitempty"`  // ActDriveLifecycleTerminated
	WaitErr   string     `json:"wait_err,omitempty"`   // ActDriveLifecycleTerminated

	// Emit (shared). Type is the core event type; Detail is a shell-resolved
	// payload key (the pure machine does not build run-specific JSON payloads —
	// those need shell-owned run data, runexec-design §7 note 4).
	Type   core.EventType `json:"type,omitempty"`
	Detail string         `json:"detail,omitempty"`

	// Run / close-ladder (RSM-020: every divergence survives as a parameter).
	Summary string `json:"summary,omitempty"` // ActCloseBead / ActEmitRunTerminal SummaryRef
	Reason  string `json:"reason,omitempty"`  // ActReopenBead reason template
	Success bool   `json:"success,omitempty"` // ActEmitRunTerminal
	Label   string `json:"label,omitempty"`   // ActSubmitMerge target label

	// Timers (shared).
	Timer TimerKind     `json:"timer,omitempty"` // ArmTimer / CancelTimer
	D     time.Duration `json:"d,omitempty"`     // ArmTimer
}
