package core

// verdictexecution_rc025.go — VerdictExecutionPlan and PlanForVerdict (RC-025).
//
// RC-025 requires the daemon's verdict-executor to perform the mechanical action
// for each of the seven verdicts per the verdict-execution table at
// [reconciliation/schemas.md §6.2]. This file declares:
//
//   - VerdictActionKind — discriminator for the class of mechanical action.
//   - VerdictExecutionPlan — describes what the verdict-executor must do for a
//     given verdict: which action kind to perform, the idempotency mechanism per
//     schemas.md §6.2, and the ActionSummary to embed in VerdictExecutedPayload.
//   - PlanForVerdict — maps a VerdictEvent to its VerdictExecutionPlan.
//
// This is a pure, I/O-free layer. The actual adapter calls, git commits, and event
// emissions are performed by the daemon's verdict-executor (RC-025a), which consumes
// this plan. The separation mirrors the CheckVerdictStaleness / staleness-result
// split for RC-024: pure logic here, I/O in the daemon.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025;
// specs/reconciliation/schemas.md §6.2 Verdict-execution table.

// VerdictActionKind is the discriminator for the class of mechanical action the
// verdict-executor must perform (RC-025, schemas.md §6.2).
//
// Each value corresponds to one or more rows in the verdict-execution table.
type VerdictActionKind string

const (
	// VerdictActionKindDispatchCurrentNode means re-dispatch the outer run's current
	// node (with or without context injection). Idempotent at the dispatch layer.
	// Covers: resume-here, resume-with-context.
	VerdictActionKindDispatchCurrentNode VerdictActionKind = "dispatch-current-node"

	// VerdictActionKindResetToCheckpoint means perform an intra-run rollback to the
	// named checkpoint (checkpoint_ref in VerdictEvent). The worktree and run_id
	// are preserved. Idempotent: no-op when current state already matches target.
	// Covers: reset-to-checkpoint.
	VerdictActionKindResetToCheckpoint VerdictActionKind = "reset-to-checkpoint"

	// VerdictActionKindReopenBead means invoke the BI-CLI adapter's reopen path per
	// [beads-integration.md §4.4 BI-010] (BI-010a verdict→op binding). Idempotency
	// handled by the adapter's BI-031 status-check-before-reissue protocol.
	// Covers: reopen-bead.
	VerdictActionKindReopenBead VerdictActionKind = "reopen-bead"

	// VerdictActionKindAcceptCloseWithNote means append the investigator's annotation
	// to the reconciliation commit and, when the bead is not already closed, write a
	// Beads close via the adapter with idempotency key <target_run_id>:close.
	// Covers: accept-close-with-note.
	VerdictActionKindAcceptCloseWithNote VerdictActionKind = "accept-close-with-note"

	// VerdictActionKindNoOp means no mechanical action beyond emitting
	// reconciliation_verdict_executed and appending the verdict-executed commit.
	// Idempotent: repeated application produces identical emissions (dedup by
	// target_run_id on the execution event per schemas.md §6.2).
	// Covers: no-op-accept.
	VerdictActionKindNoOp VerdictActionKind = "no-op"

	// VerdictActionKindEscalateToHuman means emit operator_escalation_required and
	// leave the outer run in its current state. Deduplicated by target_run_id:
	// subsequent emissions for the same target_run_id are no-ops.
	// Covers: escalate-to-human.
	VerdictActionKindEscalateToHuman VerdictActionKind = "escalate-to-human"
)

// Valid reports whether k is one of the six declared VerdictActionKind constants.
func (k VerdictActionKind) Valid() bool {
	switch k {
	case VerdictActionKindDispatchCurrentNode,
		VerdictActionKindResetToCheckpoint,
		VerdictActionKindReopenBead,
		VerdictActionKindAcceptCloseWithNote,
		VerdictActionKindNoOp,
		VerdictActionKindEscalateToHuman:
		return true
	default:
		return false
	}
}

// VerdictExecutionPlan describes the mechanical action the daemon's verdict-executor
// must perform for a given verdict (RC-025, schemas.md §6.2).
//
// PlanForVerdict produces a VerdictExecutionPlan from a VerdictEvent. The plan is
// consumed by the daemon's verdict-executor (RC-025a), which performs the described
// I/O operations in order.
//
// # Structural invariants (enforced by Valid)
//
//   - Verdict is non-empty and one of the seven declared Verdict constants.
//   - ActionKind is non-empty and one of the six declared VerdictActionKind constants.
//   - ActionSummary is non-empty (used as VerdictExecutedPayload.ActionSummary).
//   - IdempotencyMechanism is non-empty (documents the idempotency rule per schemas.md §6.2).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025;
// specs/reconciliation/schemas.md §6.2 Verdict-execution table.
type VerdictExecutionPlan struct {
	// Verdict is the verdict this plan was produced for.
	// Must be one of the seven declared Verdict constants.
	Verdict Verdict

	// ActionKind is the class of mechanical action to perform.
	// Must be one of the six declared VerdictActionKind constants.
	ActionKind VerdictActionKind

	// ActionSummary is the short prose description of the mechanical action taken.
	// Embedded verbatim in VerdictExecutedPayload.ActionSummary (RC-025;
	// schemas.md §6.1 RECORD VerdictExecutedPayload). Required (non-empty).
	ActionSummary string

	// IdempotencyMechanism describes how idempotency is guaranteed per the
	// "Idempotency key / rule" column of schemas.md §6.2. Required (non-empty).
	IdempotencyMechanism string

	// InjectsContext is true when the verdict requires context injection into the
	// outer run's shared context before re-dispatch. True only for resume-with-context.
	// The context string is carried by VerdictEvent.Context (RC-022a).
	InjectsContext bool

	// RequiresBIClose is true when the verdict-executor MUST write a Beads close
	// via the adapter when the bead is not already closed (accept-close-with-note).
	// Idempotency key for the close write: <target_run_id>:close.
	RequiresBIClose bool

	// EmitsOperatorEscalation is true when the verdict-executor MUST emit
	// operator_escalation_required deduplicated by target_run_id (escalate-to-human).
	EmitsOperatorEscalation bool
}

// Valid reports whether all structural invariants of VerdictExecutionPlan are satisfied.
//
// Rules:
//   - Verdict is non-empty and a declared constant.
//   - ActionKind is non-empty and a declared constant.
//   - ActionSummary is non-empty.
//   - IdempotencyMechanism is non-empty.
func (p VerdictExecutionPlan) Valid() bool {
	if !p.Verdict.Valid() {
		return false
	}
	if !p.ActionKind.Valid() {
		return false
	}
	if p.ActionSummary == "" {
		return false
	}
	if p.IdempotencyMechanism == "" {
		return false
	}
	return true
}

// PlanForVerdict maps a VerdictEvent to its VerdictExecutionPlan.
//
// The returned plan describes the mechanical action the daemon's verdict-executor
// (RC-025a) must perform for the given verdict. The caller is responsible for
// validating the VerdictEvent (RC-020 / RC-023) and performing the RC-024 staleness
// check before calling this function; PlanForVerdict does NOT perform either check.
//
// For an invalid or unknown Verdict, PlanForVerdict panics. Callers MUST validate
// the VerdictEvent before calling this function. In normal operation the daemon's
// verdict-executor validates at RC-025a step 1 and routes through RC-023 fallback
// before reaching this function.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025;
// specs/reconciliation/schemas.md §6.2 Verdict-execution table.
func PlanForVerdict(v Verdict) VerdictExecutionPlan {
	switch v {
	case VerdictResumeHere:
		return VerdictExecutionPlan{
			Verdict:              v,
			ActionKind:           VerdictActionKindDispatchCurrentNode,
			ActionSummary:        "re-dispatched outer run's current node with no context change",
			IdempotencyMechanism: "dispatch-check-sees-outer-run-already-running-no-re-dispatch",
		}
	case VerdictResumeWithContext:
		return VerdictExecutionPlan{
			Verdict:              v,
			ActionKind:           VerdictActionKindDispatchCurrentNode,
			ActionSummary:        "re-dispatched outer run's current node with investigator context injected",
			IdempotencyMechanism: "dispatch-layer-idempotent-context-injection-is-additive",
			InjectsContext:       true,
		}
	case VerdictResetToCheckpoint:
		return VerdictExecutionPlan{
			Verdict:              v,
			ActionKind:           VerdictActionKindResetToCheckpoint,
			ActionSummary:        "reverted outer run to named checkpoint; will re-run from reverted state",
			IdempotencyMechanism: "no-op-when-current-state-already-matches-target-state-id",
		}
	case VerdictReopenBead:
		return VerdictExecutionPlan{
			Verdict:              v,
			ActionKind:           VerdictActionKindReopenBead,
			ActionSummary:        "invoked BI adapter reopen path; cleared in-flight tracking; subsequent claim produces new run",
			IdempotencyMechanism: "bi-031b-status-check-before-reissue-noop-when-bead-already-open",
		}
	case VerdictAcceptCloseWithNote:
		return VerdictExecutionPlan{
			Verdict:              v,
			ActionKind:           VerdictActionKindAcceptCloseWithNote,
			ActionSummary:        "appended investigator annotation to reconciliation commit; wrote Beads close if not already closed",
			IdempotencyMechanism: "same-annotation-appended-repeated-close-write-idempotent-key-target-run-id-colon-close",
			RequiresBIClose:      true,
		}
	case VerdictNoOpAccept:
		return VerdictExecutionPlan{
			Verdict:              v,
			ActionKind:           VerdictActionKindNoOp,
			ActionSummary:        "no mechanical action; outer run left untouched; subsequent dispatch cycles treat run as ordinary",
			IdempotencyMechanism: "no-mechanical-action-dedup-by-target-run-id-on-execution-event",
		}
	case VerdictEscalateToHuman:
		return VerdictExecutionPlan{
			Verdict:                 v,
			ActionKind:              VerdictActionKindEscalateToHuman,
			ActionSummary:           "emitted operator_escalation_required; outer run left in current state pending operator action",
			IdempotencyMechanism:    "deduplicated-by-target-run-id-subsequent-emissions-are-nops",
			EmitsOperatorEscalation: true,
		}
	default:
		panic("PlanForVerdict: unknown verdict " + string(v) + "; caller must validate VerdictEvent before calling")
	}
}
