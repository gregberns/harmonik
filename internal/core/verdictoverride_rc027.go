package core

// OperatorVerdictOverridePolicy is the policy-document field set extracted from
// a reconciliation workflow's YAML policy that controls whether the daemon
// MUST pause verdict execution and await operator input.
//
// confirm_required defaults to false in the S01-shipped policies for Cat 2 and
// Cat 3. Cat 6a defaults to confirm_required: true because the default verdict
// (escalate-to-human) benefits from operator review before execution (per
// OQ-RC-012 resolution: require for Cat 6a, optional otherwise).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027; OQ-RC-012.
type OperatorVerdictOverridePolicy struct {
	// ConfirmRequired is true when the reconciliation workflow's YAML policy
	// declares confirm_required: true. When true, the daemon MUST pause the
	// verdict-execution step (RC-025a step 4) and emit
	// reconciliation_verdict_pending_confirmation before executing the verdict.
	// When false, execution proceeds immediately after the staleness check
	// (RC-024). Default: false.
	ConfirmRequired bool
}

// PolicyRequiresConfirmation returns true when the policy declares
// confirm_required: true. This is a pure convenience accessor so callers
// do not directly inspect policy struct fields in the verdict-execution hot path.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027.
func PolicyRequiresConfirmation(policy OperatorVerdictOverridePolicy) bool {
	return policy.ConfirmRequired
}

// VerdictOverrideDecision is the operator's decision for a pending verdict.
//
// Two values cover the full decision space:
//
//   - VerdictOverrideDecisionConfirm — the operator approves the verdict; the
//     daemon MUST proceed with the verdict's mechanical action per RC-025.
//
//   - VerdictOverrideDecisionVeto — the operator rejects the verdict; the daemon
//     MUST NOT execute the verdict. When VetoPromotion is
//     VetoPromotionEscalateToHuman, the daemon replaces the verdict with
//     escalate-to-human before executing. Otherwise the run is left in its
//     current state (equivalent to no-op-accept for the original verdict).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
// specs/operator-nfr.md §4.3 ON-014.
type VerdictOverrideDecision string

const (
	// VerdictOverrideDecisionConfirm signals that the operator has approved the
	// investigator's verdict; the daemon proceeds with verdict execution per
	// RC-025a.
	//
	// CLI surface: harmonik confirm-verdict <run_id>
	VerdictOverrideDecisionConfirm VerdictOverrideDecision = "confirm"

	// VerdictOverrideDecisionVeto signals that the operator has rejected the
	// investigator's verdict; the daemon MUST NOT execute the original verdict.
	// The optional VetoPromotion field in OperatorVerdictOverrideRequest controls
	// whether the daemon substitutes escalate-to-human.
	//
	// CLI surface: harmonik veto-verdict <run_id> [--promote-to escalate-to-human]
	VerdictOverrideDecisionVeto VerdictOverrideDecision = "veto"
)

// Valid reports whether d is one of the two declared VerdictOverrideDecision values.
func (d VerdictOverrideDecision) Valid() bool {
	return d == VerdictOverrideDecisionConfirm || d == VerdictOverrideDecisionVeto
}

// VetoPromotion is the optional target verdict that replaces the original
// verdict when the operator vetoes with --promote-to.
//
// Currently the only defined promotion target is escalate-to-human, which
// causes the daemon to substitute the original verdict with escalate-to-human
// before execution. VetoPromotionNone means the daemon abandons the verdict
// without substitution (the run remains in its current state).
//
// The full grammar is: harmonik veto-verdict <run_id> [--promote-to escalate-to-human]
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
// specs/operator-nfr.md §4.3 ON-014 (--promote-to flag).
type VetoPromotion string

const (
	// VetoPromotionNone means the veto is a plain abort: the original verdict is
	// discarded and no replacement verdict is executed. The run's state is
	// unchanged.
	VetoPromotionNone VetoPromotion = ""

	// VetoPromotionEscalateToHuman means the veto additionally promotes the
	// effective verdict to escalate-to-human. The daemon MUST substitute the
	// original verdict with VerdictEscalateToHuman and execute it per RC-025.
	//
	// Wire value: the literal string "escalate-to-human", matching the Verdict
	// enum value (schemas.md §6.1) so the daemon can pass it directly to
	// PlanForVerdict without additional mapping.
	VetoPromotionEscalateToHuman VetoPromotion = "escalate-to-human"
)

// Valid reports whether v is one of the two declared VetoPromotion values.
func (v VetoPromotion) Valid() bool {
	return v == VetoPromotionNone || v == VetoPromotionEscalateToHuman
}

// OperatorVerdictOverrideRequest is the operator's full decision as received
// from the CLI and forwarded to the daemon via the socket protocol.
//
// The daemon's verdict-executor (RC-025a) consumes this request when
// PolicyRequiresConfirmation returns true: it parks verdict execution, waits
// for an OperatorVerdictOverrideRequest, and then either proceeds (Confirm) or
// discards / promotes (Veto).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027;
// specs/operator-nfr.md §4.3 ON-014.
type OperatorVerdictOverrideRequest struct {
	// TargetRunID is the run_id of the reconciliation run whose pending verdict
	// the operator is acting on. The daemon MUST reject requests where TargetRunID
	// does not match a run currently parked in the verdict-pending-confirmation
	// state; the CLI exits 16 (operator-control-invalid-state) on that rejection.
	TargetRunID string

	// Decision is the operator's choice: confirm or veto.
	Decision VerdictOverrideDecision

	// VetoPromotion carries the optional --promote-to target for a veto decision.
	// MUST be VetoPromotionNone when Decision == VerdictOverrideDecisionConfirm.
	// MAY be VetoPromotionEscalateToHuman when Decision == VerdictOverrideDecisionVeto.
	VetoPromotion VetoPromotion
}

// Valid reports whether the request is well-formed:
//   - TargetRunID is non-empty
//   - Decision is one of the two valid values
//   - VetoPromotion is one of the two valid values
//   - VetoPromotion is VetoPromotionNone when Decision is Confirm
//   - VetoPromotion is valid when Decision is Veto
func (r OperatorVerdictOverrideRequest) Valid() bool {
	if r.TargetRunID == "" {
		return false
	}
	if !r.Decision.Valid() {
		return false
	}
	if !r.VetoPromotion.Valid() {
		return false
	}
	if r.Decision == VerdictOverrideDecisionConfirm && r.VetoPromotion != VetoPromotionNone {
		return false
	}
	return true
}

// ApplyVetoPromotion returns the effective Verdict that the daemon's
// verdict-executor MUST apply after a veto. When promotion is
// VetoPromotionEscalateToHuman, the returned verdict is VerdictEscalateToHuman.
// When promotion is VetoPromotionNone, the verdict is VerdictNoOpAccept (the
// veto silently accepts the current state without the original verdict's action).
//
// This function MUST only be called when request.Decision is
// VerdictOverrideDecisionVeto; callers MUST check this precondition.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027.
func ApplyVetoPromotion(promotion VetoPromotion) Verdict {
	if promotion == VetoPromotionEscalateToHuman {
		return VerdictEscalateToHuman
	}
	return VerdictNoOpAccept
}
