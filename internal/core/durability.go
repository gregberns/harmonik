package core

// IsDurable reports whether a transition with the given kind and outcome status
// is durable per execution-model.md §4.5.EM-023a.
//
// A transition is durable iff BOTH of the following hold:
//
//	transition_kind ∈ {forward, local-patchback, architectural-rollback,
//	                    policy-rollback, context-restore}
//	outcome.status  ∈ {SUCCESS, PARTIAL_SUCCESS}
//
// Transitions with status RETRY are not durable (intra-run loops per EM-015);
// transitions with status FAIL are not durable (failure handling per EM-025).
// Gate denial and validator rejection are also non-durable, but those are not
// representable as a (TransitionKind, OutcomeStatus) pair and are therefore
// outside this function's scope.
//
// The caller is responsible for ensuring kind and status are valid
// (kind.Valid() == true, status.Valid() == true) before calling IsDurable.
// No validation is performed here; the function is a pure decision primitive.
func IsDurable(kind TransitionKind, status OutcomeStatus) bool {
	return isDurableKind(kind) && isDurableStatus(status)
}

// isDurableKind reports whether kind is one of the five durable TransitionKind
// values per execution-model.md §4.5.EM-023a.
func isDurableKind(kind TransitionKind) bool {
	switch kind {
	case TransitionKindForward,
		TransitionKindLocalPatchback,
		TransitionKindArchitecturalRollback,
		TransitionKindPolicyRollback,
		TransitionKindContextRestore:
		return true
	default:
		return false
	}
}

// isDurableStatus reports whether status is one of the two durable
// OutcomeStatus values per execution-model.md §4.5.EM-023a.
func isDurableStatus(status OutcomeStatus) bool {
	switch status {
	case OutcomeStatusSuccess, OutcomeStatusPartialSuccess:
		return true
	default:
		return false
	}
}
