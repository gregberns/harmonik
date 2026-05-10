package core

// DispatchDeferredReason is the typed alias for the `reason` field of the
// dispatch_deferred event (event-model.md §8.7.13).
//
// The set is semi-open: the canonical value is
// DispatchDeferredReasonMachineCeilingExhausted; other string values are
// accepted per event-model.md §8.7.13 ("machine_ceiling_exhausted / other").
// Consumers MUST treat unrecognised values as opaque strings and MUST NOT
// reject them.
//
// Spec ref: event-model.md §8.7.13; operator-nfr.md §8 exit code 18.
// Bead ref: hk-hqwn.71.
type DispatchDeferredReason string

const (
	// DispatchDeferredReasonMachineCeilingExhausted is the canonical reason value
	// when the machine-level agent-subprocess concurrency ceiling (per
	// operator-nfr.md §4.10 ON-041) blocks a dispatch.
	DispatchDeferredReasonMachineCeilingExhausted DispatchDeferredReason = "machine_ceiling_exhausted"
)
