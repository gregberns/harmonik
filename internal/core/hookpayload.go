package core

// HookPayload is the per-attachment payload for a Hook control point
// (specs/control-points.md §6.1.2 RECORD HookPayload).
//
// The five fields map directly to the RECORD declaration:
//
//	RECORD HookPayload:
//	    trigger_event        : String                  -- event name from the registered lifecycle set
//	    subscription_filter  : PolicyExpression | None -- optional filter over event payload
//	    side_effect_kind     : SideEffectKind          -- one of {emit-event, state-mutation, external-action}
//	    halt_on_failure      : Bool                    -- default false; see §4.3.CP-015
//	    subsystem_priority   : Integer                 -- declared by subsystem envelope
//
// # Default: HaltOnFailure
//
// HaltOnFailure defaults to false per CP-015. Use [NewHookPayload] to obtain a
// zero-value HookPayload with this default applied. Callers constructing a
// HookPayload via struct literal get the correct default from the Go zero value
// (false), but MUST still set TriggerEvent and SideEffectKind explicitly before
// the payload is valid.
//
// # Typed-alias deferral: SubscriptionFilter
//
// SubscriptionFilter uses *string as a placeholder for PolicyExpression, which
// is not yet a Go type. A nil value means no filter applies (the hook fires on
// all occurrences of the trigger event). A non-nil value is a raw
// policy-expression string that the evaluator interprets per §6.1.2.
//
// TODO hk-a8bg.92: replace *string with PolicyExpression typed alias once the
// typed wrapper is implemented (specs/control-points.md §6.1.2
// subscription_filter : PolicyExpression | None).
type HookPayload struct {
	// TriggerEvent is the event name from the registered lifecycle set that
	// triggers this hook (specs/control-points.md §6.1.2, CP-013). Must be
	// non-empty.
	//
	// Wire field name: trigger_event.
	TriggerEvent string `json:"trigger_event"`

	// SubscriptionFilter is an optional filter over the event payload. When
	// nil (None), the hook fires on every occurrence of TriggerEvent. When
	// non-nil, the evaluator applies the expression and fires only when it
	// evaluates to true.
	//
	// TODO hk-a8bg.92: replace *string with PolicyExpression typed alias
	// (specs/control-points.md §6.1.2 subscription_filter : PolicyExpression | None).
	//
	// Wire field name: subscription_filter.
	SubscriptionFilter *string `json:"subscription_filter,omitempty"`

	// SideEffectKind discriminates how the hook evaluator's side effects are
	// dispatched by S05. Must be one of the three declared SideEffectKind
	// constants.
	//
	// Wire field name: side_effect_kind.
	SideEffectKind SideEffectKind `json:"side_effect_kind"`

	// HaltOnFailure controls chain behavior on evaluator error per CP-015
	// (specs/control-points.md §4.3.CP-015). When false (default), a per-hook
	// failure is recorded and the chain continues. When true, the failure
	// propagates and halts further hook evaluation.
	//
	// Wire field name: halt_on_failure.
	HaltOnFailure bool `json:"halt_on_failure"`

	// SubsystemPriority is the integer ordering weight declared by the
	// subsystem envelope per CP-014 (specs/control-points.md §4.3.CP-014).
	// Lower values run first. The value is set by the subsystem that registers
	// the hook; the S05 dispatcher uses it to establish evaluation order.
	//
	// Wire field name: subsystem_priority.
	SubsystemPriority int `json:"subsystem_priority"`
}

// NewHookPayload returns a HookPayload with the spec-mandated default applied:
// HaltOnFailure is false per CP-015 (specs/control-points.md §4.3.CP-015).
//
// The Go zero value for bool is already false, so NewHookPayload is provided
// for documentation clarity and parity with sibling constructors. Callers MUST
// still set TriggerEvent and SideEffectKind explicitly before the payload is
// valid.
func NewHookPayload() HookPayload {
	return HookPayload{
		HaltOnFailure: false,
	}
}

// Valid reports whether p satisfies the structural invariants declared in
// specs/control-points.md §6.1.2:
//
//   - TriggerEvent must be non-empty.
//   - SideEffectKind must be a recognised constant per [SideEffectKind.Valid].
//
// SubscriptionFilter contents are not validated here; the evaluator interprets
// the expression per §6.1.2. HaltOnFailure and SubsystemPriority are always
// structurally valid (no domain constraints at the type level).
func (p HookPayload) Valid() bool {
	if p.TriggerEvent == "" {
		return false
	}
	if !p.SideEffectKind.Valid() {
		return false
	}
	return true
}
