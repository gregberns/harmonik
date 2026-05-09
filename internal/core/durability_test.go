// Package core — named requirement-traceable sensors for the EM-023a durability
// decision procedure.
//
// Tests verify execution-model.md §4.5.EM-023a: a transition is durable iff
// transition_kind ∈ {forward, local-patchback, architectural-rollback,
// policy-rollback, context-restore} AND outcome.status ∈ {SUCCESS,
// PARTIAL_SUCCESS}. All other (kind, status) combinations are non-durable.
package core

import "testing"

// durabilityFixtureAllKinds returns the full set of TransitionKind values for
// use in durability truth-table tests.
func durabilityFixtureAllKinds() []TransitionKind {
	return []TransitionKind{
		TransitionKindForward,
		TransitionKindLocalPatchback,
		TransitionKindArchitecturalRollback,
		TransitionKindPolicyRollback,
		TransitionKindContextRestore,
	}
}

// durabilityFixtureAllStatuses returns the full set of OutcomeStatus values for
// use in durability truth-table tests.
func durabilityFixtureAllStatuses() []OutcomeStatus {
	return []OutcomeStatus{
		OutcomeStatusSuccess,
		OutcomeStatusPartialSuccess,
		OutcomeStatusFail,
		OutcomeStatusRetry,
	}
}

// TestIsDurable_EM023a_TruthTable verifies the full (kind × status) truth table
// from execution-model.md §4.5.EM-023a. Every durable cell must return true;
// every non-durable cell must return false.
func TestIsDurable_EM023a_TruthTable(t *testing.T) {
	t.Parallel()

	durableStatuses := map[OutcomeStatus]bool{
		OutcomeStatusSuccess:        true,
		OutcomeStatusPartialSuccess: true,
		OutcomeStatusFail:           false,
		OutcomeStatusRetry:          false,
	}

	for _, kind := range durabilityFixtureAllKinds() {
		for _, status := range durabilityFixtureAllStatuses() {
			kind, status := kind, status
			want := durableStatuses[status] // all five kinds are durable when status is durable
			t.Run(string(kind)+"/"+string(status), func(t *testing.T) {
				t.Parallel()
				got := IsDurable(kind, status)
				if got != want {
					t.Errorf("IsDurable(%q, %q) = %v, want %v", kind, status, got, want)
				}
			})
		}
	}
}

// TestIsDurable_EM023a_DurableKindsWithSuccess verifies that every durable
// transition_kind returns true when paired with SUCCESS.
func TestIsDurable_EM023a_DurableKindsWithSuccess(t *testing.T) {
	t.Parallel()

	for _, kind := range durabilityFixtureAllKinds() {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if !IsDurable(kind, OutcomeStatusSuccess) {
				t.Errorf("IsDurable(%q, SUCCESS) = false, want true (EM-023a: all five kinds are durable with SUCCESS)", kind)
			}
		})
	}
}

// TestIsDurable_EM023a_DurableKindsWithPartialSuccess verifies that every
// durable transition_kind returns true when paired with PARTIAL_SUCCESS.
func TestIsDurable_EM023a_DurableKindsWithPartialSuccess(t *testing.T) {
	t.Parallel()

	for _, kind := range durabilityFixtureAllKinds() {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if !IsDurable(kind, OutcomeStatusPartialSuccess) {
				t.Errorf("IsDurable(%q, PARTIAL_SUCCESS) = false, want true (EM-023a: PARTIAL_SUCCESS is durable)", kind)
			}
		})
	}
}

// TestIsDurable_EM023a_RetryIsNeverDurable verifies that RETRY is non-durable
// with every transition_kind per EM-023a (RETRY = intra-run loop, no checkpoint).
func TestIsDurable_EM023a_RetryIsNeverDurable(t *testing.T) {
	t.Parallel()

	for _, kind := range durabilityFixtureAllKinds() {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if IsDurable(kind, OutcomeStatusRetry) {
				t.Errorf("IsDurable(%q, RETRY) = true, want false (EM-023a: RETRY is not durable)", kind)
			}
		})
	}
}

// TestIsDurable_EM023a_FailIsNeverDurable verifies that FAIL is non-durable
// with every transition_kind per EM-023a (failure handling per EM-025).
func TestIsDurable_EM023a_FailIsNeverDurable(t *testing.T) {
	t.Parallel()

	for _, kind := range durabilityFixtureAllKinds() {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if IsDurable(kind, OutcomeStatusFail) {
				t.Errorf("IsDurable(%q, FAIL) = true, want false (EM-023a: FAIL is not durable)", kind)
			}
		})
	}
}
