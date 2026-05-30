package lifecycle

// validTransitions is the authoritative edge table for the per-session FSM
// (HC-065). It mirrors the VALID_TRANSITIONS map from flywheel_gateway's
// agent-state.ts with PAUSED renamed to Suspended (disambiguates from
// handler-pause.md HandlerStatus.paused at the handler-type tier).
//
// Key invariants:
//   - Every non-terminal state can reach StateTerminating and StateFailed.
//   - StateReady ↔ StateExecuting is bidirectional.
//   - StateSuspended ↔ StateReady is bidirectional; no direct
//     Suspended→Executing edge.
//   - Terminal states (Terminated, Failed) have no outgoing edges.
var validTransitions [8][8]bool

func init() {
	allow := func(from, to LifecycleState) {
		validTransitions[from][to] = true
	}

	// Spawning
	allow(StateSpawning, StateInitializing)
	allow(StateSpawning, StateTerminating)
	allow(StateSpawning, StateFailed)

	// Initializing
	allow(StateInitializing, StateReady)
	allow(StateInitializing, StateTerminating)
	allow(StateInitializing, StateFailed)

	// Ready
	allow(StateReady, StateExecuting)
	allow(StateReady, StateSuspended)
	allow(StateReady, StateTerminating)
	allow(StateReady, StateFailed) // includes silent_hang (HC-026)

	// Executing
	allow(StateExecuting, StateReady)
	allow(StateExecuting, StateSuspended)
	allow(StateExecuting, StateTerminating)
	allow(StateExecuting, StateFailed)

	// Suspended
	allow(StateSuspended, StateReady)
	allow(StateSuspended, StateTerminating)
	allow(StateSuspended, StateFailed)

	// Terminating
	allow(StateTerminating, StateTerminated)
	allow(StateTerminating, StateFailed)

	// Terminated — terminal, no outgoing edges.
	// Failed — terminal, no outgoing edges.
}

// isValidTransition reports whether the from→to edge is in the table.
func isValidTransition(from, to LifecycleState) bool {
	if int(from) >= len(validTransitions) || int(to) >= len(validTransitions[0]) {
		return false
	}
	return validTransitions[from][to]
}
