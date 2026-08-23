package lifecycle

var validTransitions [8][8]bool

func init() {
	allow := func(from, to LifecycleState) {
		validTransitions[from][to] = true
	}

	allow(StateSpawning, StateInitializing)
	allow(StateSpawning, StateTerminating)
	allow(StateSpawning, StateFailed)

	allow(StateInitializing, StateReady)
	allow(StateInitializing, StateTerminating)
	allow(StateInitializing, StateFailed)

	allow(StateReady, StateExecuting)
	allow(StateReady, StateSuspended)
	allow(StateReady, StateTerminating)
	allow(StateReady, StateFailed) // includes silent_hang (HC-026)

	allow(StateExecuting, StateReady)
	allow(StateExecuting, StateSuspended)
	allow(StateExecuting, StateTerminating)
	allow(StateExecuting, StateFailed)

	allow(StateSuspended, StateReady)
	allow(StateSuspended, StateTerminating)
	allow(StateSuspended, StateFailed)

	allow(StateTerminating, StateTerminated)
	allow(StateTerminating, StateFailed)
}

func isValidTransition(from, to LifecycleState) bool {
	if int(from) >= len(validTransitions) || int(to) >= len(validTransitions[0]) {
		return false
	}
	return validTransitions[from][to]
}
