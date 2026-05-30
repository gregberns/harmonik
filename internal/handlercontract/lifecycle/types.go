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
	// StateSpawning: process started, handshake not yet complete.
	StateSpawning LifecycleState = iota
	// StateInitializing: handshake done, skills provisioning in progress.
	StateInitializing
	// StateReady: agent_ready fired; idle, accepting input.
	StateReady
	// StateExecuting: command in flight (between input-send and outcome).
	StateExecuting
	// StateSuspended: per-session operator pause. Distinct from
	// handler-pause.md HandlerStatus.paused which operates at the handler-type
	// tier, not per-session.
	StateSuspended
	// StateTerminating: SIGTERM sent; Wait not yet returned.
	StateTerminating
	// StateTerminated: Wait returned with exit==0 or an expected code.
	StateTerminated
	// StateFailed: Wait returned with a classified error, or a protocol
	// violation (e.g. silent-hang per HC-026).
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
