// Package lifecycle implements the per-session agent lifecycle state machine
// declared in specs/handler-contract.md §4.x (HC-064..HC-067).
//
// This is a leaf package with no internal/ dependencies. Event emission is
// wired in a later bead (B4); callers obtain a *Machine via New and drive
// transitions via Machine.Transition.
package lifecycle

import "time"

// LifecycleState represents the discrete phase of a single agent session
// (HC-064).
//
// StateTerminated and StateFailed are TERMINAL_STATES; no outgoing transitions
// exist from either.
type LifecycleState uint8

const (
	// StateSpawning means the process has started and the handshake is not yet
	// complete.
	StateSpawning LifecycleState = iota
	// StateInitializing means the handshake is done and skills provisioning is
	// in progress.
	StateInitializing
	// StateReady means agent_ready fired; the session is idle and accepting
	// input.
	StateReady
	// StateExecuting means a command is in flight (between input-send and
	// outcome).
	StateExecuting
	// StateSuspended is a per-session operator pause. Distinct from
	// handler-pause.md HandlerStatus.paused which operates at the handler-type
	// tier, not per-session.
	StateSuspended
	// StateTerminating means SIGTERM has been sent and Wait has not yet
	// returned.
	StateTerminating
	// StateTerminated means Wait returned with exit==0 or an expected code.
	StateTerminated
	// StateFailed means Wait returned with a classified error, or a protocol
	// violation occurred (e.g. silent-hang per HC-026).
	StateFailed
)

// IsTerminal reports whether s is a terminal state (Terminated or Failed).
// No outgoing transitions are valid from a terminal state.
func (s LifecycleState) IsTerminal() bool {
	return s == StateTerminated || s == StateFailed
}

// String returns a human-readable label for the state.
func (s LifecycleState) String() string {
	switch s {
	case StateSpawning:
		return "Spawning"
	case StateInitializing:
		return "Initializing"
	case StateReady:
		return "Ready"
	case StateExecuting:
		return "Executing"
	case StateSuspended:
		return "Suspended"
	case StateTerminating:
		return "Terminating"
	case StateTerminated:
		return "Terminated"
	case StateFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// TransitionReason is the labelled cause for a state transition (HC-065).
// Values mirror the TS TransitionReason string-union from flywheel_gateway,
// plus ReasonSilentHang for the HC-026 direct Ready→Failed edge.
type TransitionReason string

// The Reason* constants are the complete set of TransitionReason values
// accepted by [Machine.Transition]; each names the cause recorded on the
// resulting [Transition] and mirrors the wire string emitted on
// lifecycle_transition events (HC-065).
const (
	ReasonSpawnStarted       TransitionReason = "spawn_started"
	ReasonInitComplete       TransitionReason = "init_complete"
	ReasonUserAction         TransitionReason = "user_action"
	ReasonCommandStarted     TransitionReason = "command_started"
	ReasonCommandComplete    TransitionReason = "command_complete"
	ReasonPauseRequested     TransitionReason = "pause_requested"
	ReasonResumeRequested    TransitionReason = "resume_requested"
	ReasonTerminateRequested TransitionReason = "terminate_requested"
	ReasonTerminateComplete  TransitionReason = "terminate_complete"
	ReasonError              TransitionReason = "error"
	ReasonTimeout            TransitionReason = "timeout"
	ReasonHealthCheckFailed  TransitionReason = "health_check_failed"
	ReasonDriverError        TransitionReason = "driver_error"
	ReasonResourceLimit      TransitionReason = "resource_limit"
	// ReasonSilentHang covers the HC-026 direct Ready→Failed edge for
	// sessions that become unresponsive without a clean process exit.
	ReasonSilentHang TransitionReason = "silent_hang"
)

// Transition records a single state-change event in the session history
// (HC-067).
//
// ErrCode and ErrMsg are populated only when To==StateFailed.
type Transition struct {
	From    LifecycleState
	To      LifecycleState
	At      time.Time
	Reason  TransitionReason
	ErrCode string
	ErrMsg  string
}
