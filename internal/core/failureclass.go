package core

import "fmt"

// FailureClass is the failure-class taxonomy for harmonik runs, defined in
// execution-model.md §8.  The six classes are emitted as the failure_class
// payload field on run_failed events per event-model.md §8.1.
//
// The enum is harmonik-owned and closed: unlike Beads-owned enums (e.g.
// CoarseStatus), unknown values are never tolerated.
//
// Disjointness invariant: structural and compilation_loop are DISJOINT at
// emission.  A handler returning ErrStructural MUST NOT produce a
// compilation_loop class; compilation_loop is daemon-observed only (per-edge
// traversal cap hit at cascade per EM-043).
type FailureClass string

// Failure-class constants per execution-model.md §8.
const (
	// FailureClassTransient corresponds to handler ErrTransient (handler-contract.md §4.5).
	// Bounded retry with exponential backoff; on attempt-cap exhaustion reclassifies as structural.
	FailureClassTransient FailureClass = "transient"

	// FailureClassStructural corresponds to handler ErrStructural (handler-contract.md §4.5).
	// Retry only after an approach change — typically via an edge that routes to a re-planning node.
	// DISJOINT from compilation_loop at emission.
	FailureClassStructural FailureClass = "structural"

	// FailureClassDeterministic corresponds to handler ErrDeterministic (handler-contract.md §4.5).
	// MUST NOT retry; fail the run and preserve state for post-mortem.
	FailureClassDeterministic FailureClass = "deterministic"

	// FailureClassCanceled corresponds to handler ErrCanceled or a daemon-observed
	// stop --immediate operator signal (operator-nfr.md §4.3).
	// Graceful cleanup of handler subprocess; preserve last durable checkpoint.
	FailureClassCanceled FailureClass = "canceled"

	// FailureClassBudgetExhausted corresponds to a budget-counter exceedance at dispatch
	// time per control-points.md §4.5 (budget_exhausted event emitted there), or to
	// handler ErrBudget.  Deny dispatch; do not launch the handler.
	FailureClassBudgetExhausted FailureClass = "budget_exhausted"

	// FailureClassCompilationLoop is daemon-observed: the per-edge traversal cap per
	// EM-043 has been reached at cascade evaluation (execution-model.md §4.10).
	// Cap further retries; fail the run.  DISJOINT from structural at emission.
	FailureClassCompilationLoop FailureClass = "compilation_loop"
)

// Valid reports whether f is one of the six declared FailureClass constants.
// The failure-class taxonomy is harmonik-owned and closed; unknown values are
// never valid.
func (f FailureClass) Valid() bool {
	switch f {
	case FailureClassTransient, FailureClassStructural, FailureClassDeterministic,
		FailureClassCanceled, FailureClassBudgetExhausted, FailureClassCompilationLoop:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so FailureClass serialises
// correctly in JSON and YAML.
func (f FailureClass) MarshalText() ([]byte, error) {
	if !f.Valid() {
		return nil, fmt.Errorf("failureclass: unknown value %q", string(f))
	}
	return []byte(f), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the six declared constants.
func (f *FailureClass) UnmarshalText(text []byte) error {
	v := FailureClass(text)
	if !v.Valid() {
		return fmt.Errorf(
			"failureclass: unknown value %q; must be one of transient, structural, deterministic, canceled, budget_exhausted, compilation_loop",
			string(text),
		)
	}
	*f = v
	return nil
}
