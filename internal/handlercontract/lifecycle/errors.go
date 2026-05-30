package lifecycle

import (
	"errors"
	"fmt"
)

// ErrInvalidStateTransition is the sentinel returned (wrapped) by
// Machine.Transition when the requested from→to edge is absent from the
// valid-transitions table (HC-066).
//
// Callers in the handler contract layer should additionally check against
// handlercontract.ErrDeterministic — an invalid state transition is a program
// bug, not a transient condition, and is never retryable (HC §4.5).
var ErrInvalidStateTransition = errors.New("lifecycle: invalid state transition")

// InvalidStateTransitionError carries the rejected from/to pair (HC-066).
// It wraps ErrInvalidStateTransition so errors.Is checks work on the sentinel.
type InvalidStateTransitionError struct {
	From LifecycleState
	To   LifecycleState
}

func (e *InvalidStateTransitionError) Error() string {
	return fmt.Sprintf("lifecycle: invalid state transition %s → %s", e.From, e.To)
}

func (e *InvalidStateTransitionError) Unwrap() error {
	return ErrInvalidStateTransition
}
