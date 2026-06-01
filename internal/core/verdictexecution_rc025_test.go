package core

import (
	"testing"
)

// verdictexecution_rc025_test.go — tests for VerdictExecutionPlan and PlanForVerdict (RC-025).
//
// Covers the mechanical action dispatch layer:
//   - VerdictActionKind enum validity and completeness.
//   - VerdictExecutionPlan.Valid() for fully-populated and defective plans.
//   - PlanForVerdict returns a valid plan for all seven verdicts.
//   - Per-verdict idempotency and action-kind invariants per schemas.md §6.2.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025;
// specs/reconciliation/schemas.md §6.2 Verdict-execution table.

// ---- VerdictActionKind ----

// TestVerdictActionKind_SixValuesAreDeclared verifies that exactly six
// VerdictActionKind constants are declared and each is valid.
//
// Six action kinds cover the seven verdicts: resume-here and resume-with-context
// share VerdictActionKindDispatchCurrentNode; all other verdicts have distinct kinds.
func TestVerdictActionKind_SixValuesAreDeclared(t *testing.T) {
	t.Parallel()

	kinds := []struct {
		constant VerdictActionKind
		str      string
	}{
		{VerdictActionKindDispatchCurrentNode, "dispatch-current-node"},
		{VerdictActionKindResetToCheckpoint, "reset-to-checkpoint"},
		{VerdictActionKindReopenBead, "reopen-bead"},
		{VerdictActionKindAcceptCloseWithNote, "accept-close-with-note"},
		{VerdictActionKindNoOp, "no-op"},
		{VerdictActionKindEscalateToHuman, "escalate-to-human"},
	}

	const wantCount = 6
	if len(kinds) != wantCount {
		t.Errorf("VerdictActionKind: expected %d declared constants, got %d", wantCount, len(kinds))
	}

	for _, tc := range kinds {
		tc := tc
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()

			if !tc.constant.Valid() {
				t.Errorf("VerdictActionKind(%q).Valid() = false, want true", tc.str)
			}
			if string(tc.constant) != tc.str {
				t.Errorf("VerdictActionKind string = %q, want %q", string(tc.constant), tc.str)
			}
		})
	}
}

// TestVerdictActionKind_UnknownIsInvalid verifies that unknown VerdictActionKind
// values fail Valid().
func TestVerdictActionKind_UnknownIsInvalid(t *testing.T) {
	t.Parallel()

	unknown := []VerdictActionKind{
		"",
		"dispatch",
		"reopen",
		"noop",
		"escalate",
	}

	for _, k := range unknown {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()

			if k.Valid() {
				t.Errorf("VerdictActionKind(%q).Valid() = true, want false (unknown value)", k)
			}
		})
	}
}

// ---- VerdictExecutionPlan.Valid() ----

// verdictExecutionPlanFixture returns a fully-populated VerdictExecutionPlan with
// all required fields set. Tests mutate individual fields to probe Valid().
func verdictExecutionPlanFixture() VerdictExecutionPlan {
	return VerdictExecutionPlan{
		Verdict:              VerdictResumeHere,
		ActionKind:           VerdictActionKindDispatchCurrentNode,
		ActionSummary:        "re-dispatched outer run's current node",
		IdempotencyMechanism: "dispatch-check-sees-outer-run-already-running-no-re-dispatch",
	}
}

func TestVerdictExecutionPlanValid_AllFieldsSet(t *testing.T) {
	t.Parallel()

	p := verdictExecutionPlanFixture()
	if !p.Valid() {
		t.Error("Valid() = false for fully-populated VerdictExecutionPlan, want true")
	}
}

func TestVerdictExecutionPlanValid_EmptyVerdict(t *testing.T) {
	t.Parallel()

	p := verdictExecutionPlanFixture()
	p.Verdict = Verdict("")
	if p.Valid() {
		t.Error("Valid() = true with empty Verdict, want false")
	}
}

func TestVerdictExecutionPlanValid_UnknownVerdict(t *testing.T) {
	t.Parallel()

	p := verdictExecutionPlanFixture()
	p.Verdict = Verdict("not-a-verdict")
	if p.Valid() {
		t.Error("Valid() = true with unknown Verdict, want false")
	}
}

func TestVerdictExecutionPlanValid_EmptyActionKind(t *testing.T) {
	t.Parallel()

	p := verdictExecutionPlanFixture()
	p.ActionKind = VerdictActionKind("")
	if p.Valid() {
		t.Error("Valid() = true with empty ActionKind, want false")
	}
}

func TestVerdictExecutionPlanValid_UnknownActionKind(t *testing.T) {
	t.Parallel()

	p := verdictExecutionPlanFixture()
	p.ActionKind = VerdictActionKind("unknown-action")
	if p.Valid() {
		t.Error("Valid() = true with unknown ActionKind, want false")
	}
}

func TestVerdictExecutionPlanValid_EmptyActionSummary(t *testing.T) {
	t.Parallel()

	p := verdictExecutionPlanFixture()
	p.ActionSummary = ""
	if p.Valid() {
		t.Error("Valid() = true with empty ActionSummary, want false")
	}
}

func TestVerdictExecutionPlanValid_EmptyIdempotencyMechanism(t *testing.T) {
	t.Parallel()

	p := verdictExecutionPlanFixture()
	p.IdempotencyMechanism = ""
	if p.Valid() {
		t.Error("Valid() = true with empty IdempotencyMechanism, want false")
	}
}

// ---- PlanForVerdict — coverage for all seven verdicts ----

// TestPlanForVerdict_AllVerdictsProduce_Valid verifies that PlanForVerdict returns
// a valid VerdictExecutionPlan for each of the seven declared Verdict constants.
//
// RC-025: "Each verdict's mechanical action MUST be idempotent."
// Verified here via: non-empty IdempotencyMechanism and valid plan for each verdict.
func TestPlanForVerdict_AllVerdictsProduce_Valid(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	for _, v := range verdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			plan := PlanForVerdict(v)
			if !plan.Valid() {
				t.Errorf("PlanForVerdict(%q).Valid() = false; want true", v)
			}
			if plan.Verdict != v {
				t.Errorf("PlanForVerdict(%q).Verdict = %q; want %q", v, plan.Verdict, v)
			}
		})
	}
}

// TestPlanForVerdict_ResumeHere verifies the resume-here verdict plan.
//
// schemas.md §6.2: "Re-dispatch the outer run's current node (no context change).
// Idempotent at dispatch layer (dispatch check sees outer run already running;
// no re-dispatch)."
func TestPlanForVerdict_ResumeHere(t *testing.T) {
	t.Parallel()

	plan := PlanForVerdict(VerdictResumeHere)

	if plan.ActionKind != VerdictActionKindDispatchCurrentNode {
		t.Errorf("resume-here ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindDispatchCurrentNode)
	}
	if plan.InjectsContext {
		t.Error("resume-here InjectsContext = true, want false (no context change for resume-here)")
	}
	if plan.RequiresBIClose {
		t.Error("resume-here RequiresBIClose = true, want false")
	}
	if plan.EmitsOperatorEscalation {
		t.Error("resume-here EmitsOperatorEscalation = true, want false")
	}
}

// TestPlanForVerdict_ResumeWithContext verifies the resume-with-context verdict plan.
//
// schemas.md §6.2: "Re-dispatch the outer run's current node with investigator context
// injected into the run's shared context. Idempotent at dispatch layer; context
// injection is additive."
func TestPlanForVerdict_ResumeWithContext(t *testing.T) {
	t.Parallel()

	plan := PlanForVerdict(VerdictResumeWithContext)

	if plan.ActionKind != VerdictActionKindDispatchCurrentNode {
		t.Errorf("resume-with-context ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindDispatchCurrentNode)
	}
	if !plan.InjectsContext {
		t.Error("resume-with-context InjectsContext = false, want true")
	}
	if plan.RequiresBIClose {
		t.Error("resume-with-context RequiresBIClose = true, want false")
	}
	if plan.EmitsOperatorEscalation {
		t.Error("resume-with-context EmitsOperatorEscalation = true, want false")
	}
}

// TestPlanForVerdict_ResumeHereAndResumeWithContext_ShareActionKind verifies that
// resume-here and resume-with-context share VerdictActionKindDispatchCurrentNode,
// distinguished only by InjectsContext.
func TestPlanForVerdict_ResumeHereAndResumeWithContext_ShareActionKind(t *testing.T) {
	t.Parallel()

	here := PlanForVerdict(VerdictResumeHere)
	withCtx := PlanForVerdict(VerdictResumeWithContext)

	if here.ActionKind != withCtx.ActionKind {
		t.Errorf("resume-here and resume-with-context ActionKind differ: %q vs %q; "+
			"both should be %q per schemas.md §6.2",
			here.ActionKind, withCtx.ActionKind, VerdictActionKindDispatchCurrentNode)
	}
	if here.InjectsContext == withCtx.InjectsContext {
		t.Errorf("resume-here and resume-with-context must differ on InjectsContext: "+
			"resume-here=%v, resume-with-context=%v",
			here.InjectsContext, withCtx.InjectsContext)
	}
}

// TestPlanForVerdict_ResetToCheckpoint verifies the reset-to-checkpoint verdict plan.
//
// schemas.md §6.2: "Revert the outer run to the checkpoint named by checkpoint_ref.
// Idempotent: if the current run state already matches the target state_id, no-op."
func TestPlanForVerdict_ResetToCheckpoint(t *testing.T) {
	t.Parallel()

	plan := PlanForVerdict(VerdictResetToCheckpoint)

	if plan.ActionKind != VerdictActionKindResetToCheckpoint {
		t.Errorf("reset-to-checkpoint ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindResetToCheckpoint)
	}
	if plan.InjectsContext {
		t.Error("reset-to-checkpoint InjectsContext = true, want false")
	}
	if plan.RequiresBIClose {
		t.Error("reset-to-checkpoint RequiresBIClose = true, want false")
	}
	if plan.EmitsOperatorEscalation {
		t.Error("reset-to-checkpoint EmitsOperatorEscalation = true, want false")
	}
}

// TestPlanForVerdict_ReopenBead verifies the reopen-bead verdict plan.
//
// schemas.md §6.2: "The daemon's verdict-executor invokes the BI-CLI adapter's
// reopen path per [beads-integration.md §4.4 BI-010] (BI-010a verdict→op binding);
// the adapter's BI-031 status-check-before-reissue protocol handles idempotency.
// Clear in-flight tracking; a subsequent claim produces a new run."
func TestPlanForVerdict_ReopenBead(t *testing.T) {
	t.Parallel()

	plan := PlanForVerdict(VerdictReopenBead)

	if plan.ActionKind != VerdictActionKindReopenBead {
		t.Errorf("reopen-bead ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindReopenBead)
	}
	if plan.RequiresBIClose {
		t.Error("reopen-bead RequiresBIClose = true, want false (reopen is a reopen, not a close)")
	}
	if plan.InjectsContext {
		t.Error("reopen-bead InjectsContext = true, want false")
	}
	if plan.EmitsOperatorEscalation {
		t.Error("reopen-bead EmitsOperatorEscalation = true, want false")
	}
}

// TestPlanForVerdict_AcceptCloseWithNote verifies the accept-close-with-note verdict plan.
//
// schemas.md §6.2: "Append the investigator's annotation to the reconciliation commit.
// If the bead is not already closed, write close through the adapter with idempotency
// key <target_run_id>:close."
func TestPlanForVerdict_AcceptCloseWithNote(t *testing.T) {
	t.Parallel()

	plan := PlanForVerdict(VerdictAcceptCloseWithNote)

	if plan.ActionKind != VerdictActionKindAcceptCloseWithNote {
		t.Errorf("accept-close-with-note ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindAcceptCloseWithNote)
	}
	if !plan.RequiresBIClose {
		t.Error("accept-close-with-note RequiresBIClose = false, want true (must write Beads close when not already closed)")
	}
	if plan.InjectsContext {
		t.Error("accept-close-with-note InjectsContext = true, want false")
	}
	if plan.EmitsOperatorEscalation {
		t.Error("accept-close-with-note EmitsOperatorEscalation = true, want false")
	}
}

// TestPlanForVerdict_NoOpAccept verifies the no-op-accept verdict plan.
//
// schemas.md §6.2: "No mechanical action beyond emitting reconciliation_verdict_executed
// and appending the verdict-executed commit. The outer run is left untouched."
func TestPlanForVerdict_NoOpAccept(t *testing.T) {
	t.Parallel()

	plan := PlanForVerdict(VerdictNoOpAccept)

	if plan.ActionKind != VerdictActionKindNoOp {
		t.Errorf("no-op-accept ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindNoOp)
	}
	if plan.InjectsContext {
		t.Error("no-op-accept InjectsContext = true, want false")
	}
	if plan.RequiresBIClose {
		t.Error("no-op-accept RequiresBIClose = true, want false (no mechanical action)")
	}
	if plan.EmitsOperatorEscalation {
		t.Error("no-op-accept EmitsOperatorEscalation = true, want false (no mechanical action)")
	}
}

// TestPlanForVerdict_EscalateToHuman verifies the escalate-to-human verdict plan.
//
// schemas.md §6.2: "RC emits operator_escalation_required event; the outer run
// remains in its current state. Deduplicated by target_run_id."
func TestPlanForVerdict_EscalateToHuman(t *testing.T) {
	t.Parallel()

	plan := PlanForVerdict(VerdictEscalateToHuman)

	if plan.ActionKind != VerdictActionKindEscalateToHuman {
		t.Errorf("escalate-to-human ActionKind = %q, want %q",
			plan.ActionKind, VerdictActionKindEscalateToHuman)
	}
	if !plan.EmitsOperatorEscalation {
		t.Error("escalate-to-human EmitsOperatorEscalation = false, want true")
	}
	if plan.InjectsContext {
		t.Error("escalate-to-human InjectsContext = true, want false")
	}
	if plan.RequiresBIClose {
		t.Error("escalate-to-human RequiresBIClose = true, want false")
	}
}

// TestPlanForVerdict_SevenVerdictsMappedToSixActionKinds verifies the expected
// verdict→action-kind fanout: 7 verdicts map to 6 action kinds, with only
// resume-here and resume-with-context sharing an action kind.
//
// This is the structural encoding of the schemas.md §6.2 table.
func TestPlanForVerdict_SevenVerdictsMappedToSixActionKinds(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	// Collect distinct action kinds.
	kindSet := make(map[VerdictActionKind][]Verdict)
	for _, v := range verdicts {
		plan := PlanForVerdict(v)
		kindSet[plan.ActionKind] = append(kindSet[plan.ActionKind], v)
	}

	const wantDistinctKinds = 6
	if len(kindSet) != wantDistinctKinds {
		t.Errorf("7 verdicts produced %d distinct action kinds, want %d "+
			"(resume-here and resume-with-context share VerdictActionKindDispatchCurrentNode)",
			len(kindSet), wantDistinctKinds)
	}

	// Exactly resume-here and resume-with-context share dispatch-current-node.
	dispatch := kindSet[VerdictActionKindDispatchCurrentNode]
	if len(dispatch) != 2 {
		t.Errorf("VerdictActionKindDispatchCurrentNode covers %d verdicts, want 2 "+
			"(resume-here, resume-with-context); got: %v", len(dispatch), dispatch)
	}

	// All other action kinds cover exactly one verdict.
	for kind, vv := range kindSet {
		if kind == VerdictActionKindDispatchCurrentNode {
			continue
		}
		if len(vv) != 1 {
			t.Errorf("action kind %q covers %d verdicts, want 1; got: %v", kind, len(vv), vv)
		}
	}
}

// TestPlanForVerdict_IdempotencyMechanismIsNonEmptyForAllVerdicts verifies that
// each verdict has a non-empty idempotency mechanism documentation string.
//
// RC-025: "Each verdict's mechanical action MUST be idempotent."
func TestPlanForVerdict_IdempotencyMechanismIsNonEmptyForAllVerdicts(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	for _, v := range verdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			plan := PlanForVerdict(v)
			if plan.IdempotencyMechanism == "" {
				t.Errorf("RC-025: PlanForVerdict(%q).IdempotencyMechanism is empty; "+
					"every verdict must have a documented idempotency mechanism per schemas.md §6.2", v)
			}
		})
	}
}

// TestPlanForVerdict_ActionSummaryIsNonEmptyForAllVerdicts verifies that each
// verdict produces a non-empty ActionSummary for embedding in VerdictExecutedPayload.
func TestPlanForVerdict_ActionSummaryIsNonEmptyForAllVerdicts(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	for _, v := range verdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			plan := PlanForVerdict(v)
			if plan.ActionSummary == "" {
				t.Errorf("PlanForVerdict(%q).ActionSummary is empty; "+
					"ActionSummary must be embeddable in VerdictExecutedPayload per schemas.md §6.1", v)
			}
		})
	}
}

// TestPlanForVerdict_PlanVerdictFieldMatchesInput verifies that the Verdict field
// of the returned plan always matches the input verdict.
func TestPlanForVerdict_PlanVerdictFieldMatchesInput(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	for _, v := range verdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			plan := PlanForVerdict(v)
			if plan.Verdict != v {
				t.Errorf("PlanForVerdict(%q).Verdict = %q; plan must carry the input verdict", v, plan.Verdict)
			}
		})
	}
}

// TestPlanForVerdict_OnlyOneVerdictEmitsOperatorEscalation verifies that exactly
// one verdict (escalate-to-human) sets EmitsOperatorEscalation = true.
//
// schemas.md §6.2: operator_escalation_required is emitted only for escalate-to-human.
func TestPlanForVerdict_OnlyOneVerdictEmitsOperatorEscalation(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	var escalates []Verdict
	for _, v := range verdicts {
		plan := PlanForVerdict(v)
		if plan.EmitsOperatorEscalation {
			escalates = append(escalates, v)
		}
	}

	if len(escalates) != 1 || escalates[0] != VerdictEscalateToHuman {
		t.Errorf("EmitsOperatorEscalation=true for verdicts %v; want only [escalate-to-human]", escalates)
	}
}

// TestPlanForVerdict_OnlyOneVerdictRequiresBIClose verifies that exactly one
// verdict (accept-close-with-note) sets RequiresBIClose = true.
//
// schemas.md §6.2: accept-close-with-note is the only verdict that writes
// a Beads close through the adapter.
func TestPlanForVerdict_OnlyOneVerdictRequiresBIClose(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	var closes []Verdict
	for _, v := range verdicts {
		plan := PlanForVerdict(v)
		if plan.RequiresBIClose {
			closes = append(closes, v)
		}
	}

	if len(closes) != 1 || closes[0] != VerdictAcceptCloseWithNote {
		t.Errorf("RequiresBIClose=true for verdicts %v; want only [accept-close-with-note]", closes)
	}
}

// TestPlanForVerdict_OnlyResumeWithContextInjectsContext verifies that exactly one
// verdict (resume-with-context) sets InjectsContext = true.
func TestPlanForVerdict_OnlyResumeWithContextInjectsContext(t *testing.T) {
	t.Parallel()

	verdicts := []Verdict{
		VerdictResumeHere,
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
		VerdictAcceptCloseWithNote,
		VerdictNoOpAccept,
		VerdictEscalateToHuman,
	}

	var injects []Verdict
	for _, v := range verdicts {
		plan := PlanForVerdict(v)
		if plan.InjectsContext {
			injects = append(injects, v)
		}
	}

	if len(injects) != 1 || injects[0] != VerdictResumeWithContext {
		t.Errorf("InjectsContext=true for verdicts %v; want only [resume-with-context]", injects)
	}
}
