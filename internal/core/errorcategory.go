package core

import "fmt"

// ErrorCategory is the narrow handler-origin sentinel-error type declared in
// handler-contract.md §4.5 and referenced in event-model.md §3 / §6.3.
//
// Tags: mechanism
//
// Values include the seven handler-contract sentinel names and two bus-internal
// categories added by event-model.md §8.8.2 and §6.1. ErrorCategory is carried
// as the error_category field on several event payloads (event-model.md §8.1.3,
// §8.2.2, §8.2.8, §8.3.5, §8.8.2) to identify the cause of a failure.
//
// # Relationship to FailureClass
//
// FailureClass (execution-model.md §8) is the coarse failure bucket. ErrorCategory
// is the narrower sentinel from handler-contract.md §4.5 that is present only when
// the failure originated from a handler; it is absent for orchestrator-originated
// failures such as compilation_loop or no_outgoing_edge_matches. Consumers SHOULD
// key on FailureClass for bucket-level decisions and on ErrorCategory for
// handler-origin detail.
//
// # Closed enum
//
// Nine values are closed at MVH: seven handler-contract sentinels (including two
// structural sub-sentinels) and two bus-internal categories ("overflow", "panic").
// Future additions follow the amendment protocol per [architecture.md §4.6].
type ErrorCategory string

// Declared ErrorCategory constants per event-model.md §3 / §6.3 and
// handler-contract.md §4.5, plus bus-internal values per event-model.md §8.8.2
// and §6.1.
const (
	// ErrorCategoryTransient is the ErrTransient sentinel class.
	// Detection per handler-contract.md §8.1; routed per execution-model.md §8.1.
	ErrorCategoryTransient ErrorCategory = "ErrTransient"

	// ErrorCategoryStructural is the ErrStructural sentinel class.
	// Detection per handler-contract.md §8.2; routed per execution-model.md §8.2.
	ErrorCategoryStructural ErrorCategory = "ErrStructural"

	// ErrorCategoryDeterministic is the ErrDeterministic sentinel class.
	// Detection per handler-contract.md §8.3.
	ErrorCategoryDeterministic ErrorCategory = "ErrDeterministic"

	// ErrorCategoryCanceled is the ErrCanceled sentinel class.
	// Detection per handler-contract.md §8.4; also used for daemon-observed
	// operator cancellation signals.
	ErrorCategoryCanceled ErrorCategory = "ErrCanceled"

	// ErrorCategoryBudget is the ErrBudget sentinel class.
	// Detection per handler-contract.md §8.5.
	ErrorCategoryBudget ErrorCategory = "ErrBudget"

	// ErrorCategorySkillProvisioningFailed is the ErrSkillProvisioningFailed
	// sub-sentinel. It wraps ErrStructural per handler-contract.md §4.5 HC-022.
	ErrorCategorySkillProvisioningFailed ErrorCategory = "ErrSkillProvisioningFailed"

	// ErrorCategoryProtocolMismatch is the ErrProtocolMismatch sub-sentinel.
	// It wraps ErrStructural per handler-contract.md §4.5 HC-021.
	ErrorCategoryProtocolMismatch ErrorCategory = "ErrProtocolMismatch"

	// ErrorCategoryOverflow is the bus-internal category for events shed from a
	// per-consumer queue when it is full (EV-011a). The bus emits consumer_failed
	// (event-model.md §8.8.2) with error_category="overflow" for every shed event.
	// This value is bus-internal and MUST NOT appear in handler return paths.
	ErrorCategoryOverflow ErrorCategory = "overflow"

	// ErrorCategoryPanic is the bus-internal category for consumer-goroutine panics
	// recovered by the bus per the on_panic policy (event-model.md §6.1, OQ-EV-007).
	// The bus emits consumer_failed (event-model.md §8.8.2) with
	// error_category="panic" after recovering the panic. This value is bus-internal
	// and MUST NOT appear in handler return paths.
	ErrorCategoryPanic ErrorCategory = "panic"
)

// Valid reports whether c is one of the nine declared ErrorCategory constants.
// Unknown values are not tolerated; consumers observing an unknown ErrorCategory
// MUST route to reconciliation Cat 6a per [reconciliation/spec.md §8.11].
func (c ErrorCategory) Valid() bool {
	switch c {
	case ErrorCategoryTransient,
		ErrorCategoryStructural,
		ErrorCategoryDeterministic,
		ErrorCategoryCanceled,
		ErrorCategoryBudget,
		ErrorCategorySkillProvisioningFailed,
		ErrorCategoryProtocolMismatch,
		ErrorCategoryOverflow,
		ErrorCategoryPanic:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so ErrorCategory serialises
// correctly in JSON and YAML.
func (c ErrorCategory) MarshalText() ([]byte, error) {
	if !c.Valid() {
		return nil, fmt.Errorf("errorcategory: unknown value %q", string(c))
	}
	return []byte(c), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value not in the nine declared constants.
func (c *ErrorCategory) UnmarshalText(text []byte) error {
	v := ErrorCategory(text)
	if !v.Valid() {
		return fmt.Errorf(
			"errorcategory: unknown value %q; must be one of ErrTransient, ErrStructural, ErrDeterministic, ErrCanceled, ErrBudget, ErrSkillProvisioningFailed, ErrProtocolMismatch, overflow, panic",
			string(text),
		)
	}
	*c = v
	return nil
}
