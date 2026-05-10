package handlercontract

import "time"

// CancelGoSideBound is the maximum interval within which a ctx cancellation
// MUST cause all in-flight Go-side operations to return context.Canceled
// (wrapped as ErrCanceled per §4.5).
//
// Normative value: 500ms (specs/handler-contract.md §4.4.HC-018).
// Exceeding this bound is a daemon defect.
const CancelGoSideBound = 500 * time.Millisecond

// CancelSubprocessBound is the maximum interval within which a ctx
// cancellation MUST cause subprocess cleanup to complete.
//
// Normative value: 5s (specs/handler-contract.md §4.4.HC-018).
// Exceeding this bound triggers escalation to hard termination per §4.6.
const CancelSubprocessBound = 5 * time.Second
