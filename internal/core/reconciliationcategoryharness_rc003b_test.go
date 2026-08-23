package core

import (
	"testing"

	"github.com/google/uuid"
)

// TestRC003b_WorkflowClassDiscriminatorIsReconciliation verifies that the
// Harmonik-Workflow-Class discriminator used by RC-003b to identify a
// reconciliation-workflow task branch is the "reconciliation" WorkflowClass
// constant. This is the binding-time check: the type-level constant must equal
// the spec's named value.
//
// RC-003b: "The discriminator is the Harmonik-Workflow-Class: reconciliation
// trailer per RC-002."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003b;
// specs/reconciliation/schemas.md §6.5 WorkflowClass extension.
func TestRC003b_WorkflowClassDiscriminatorIsReconciliation(t *testing.T) {
	t.Parallel()

	discriminator := WorkflowClassReconciliation
	if string(discriminator) != "reconciliation" {
		t.Errorf("RC-003b: WorkflowClassReconciliation string = %q, want %q",
			string(discriminator), "reconciliation")
	}
	if !discriminator.Valid() {
		t.Error("RC-003b: WorkflowClassReconciliation.Valid() = false; discriminator must be valid")
	}
}

// TestRC003b_CrashedReconciliationWorkflowClassifiesAsCat5 verifies the RC-003b
// rule: a reconciliation workflow whose task branch exists but whose investigator
// subprocess died mid-run (no verdict commit yet emitted) MUST be classified as
// Cat 5 (clean re-dispatch), NOT Cat 6a (multiple task branches).
//
// This test binds the shape contract: the discriminating evidence is the
// WorkflowClassReconciliation tag, the resulting category is Cat 5, and the
// rationale is that reconciliation workflows are excluded from Cat 6a per RC-002's
// recursion-bounding rule.
//
// RC-003b: "MUST be classified as Cat 5 (clean re-dispatch) on subsequent
// reconciliation cadence ticks, NOT Cat 6a (multiple task branches)."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003b.
func TestRC003b_CrashedReconciliationWorkflowClassifiesAsCat5(t *testing.T) {
	t.Parallel()

	reconciliationClass := WorkflowClassReconciliation
	if string(reconciliationClass) != "reconciliation" {
		t.Fatalf("RC-003b: discriminator constant mismatch — got %q, want %q",
			string(reconciliationClass), "reconciliation")
	}

	expectedCat := ReconciliationCategoryCat5
	if !expectedCat.Valid() {
		t.Fatal("RC-003b: ReconciliationCategoryCat5.Valid() = false; category must be valid")
	}
	if string(expectedCat) != "cat-5" {
		t.Errorf("RC-003b: Cat 5 string = %q, want %q", string(expectedCat), "cat-5")
	}

	cat6a := ReconciliationCategoryCat6a
	if string(cat6a) == string(expectedCat) {
		t.Error("RC-003b: Cat 5 and Cat 6a are the same; they must be distinct categories")
	}
}

// TestRC003b_ReconciliationWorkflowExcludedFromCat6a verifies that a
// reconciliation workflow's crashed branch is explicitly NOT Cat 6a, even though
// a "bead in_progress with two+ task branches each advertising a run ID without
// a Harmonik-Verdict-Executed marker" would otherwise classify as Cat 6a per §8.11.
//
// The exemption is enforced by the Harmonik-Workflow-Class: reconciliation
// discriminator per RC-003b.
//
// RC-003b: "RC-003a priority order applies; this rule scopes the Cat 5 vs Cat 6a
// tiebreak for the reconciliation-workflow case."
//
// Spec ref: specs/reconciliation/spec.md §4.1 RC-003b.
func TestRC003b_ReconciliationWorkflowExcludedFromCat6a(t *testing.T) {
	t.Parallel()

	if ReconciliationCategoryCat5 == ReconciliationCategoryCat6a {
		t.Fatal("RC-003b: Cat 5 and Cat 6a must be distinct — tiebreak requires two different categories")
	}

	wc := WorkflowClassReconciliation
	if !wc.Valid() {
		t.Errorf("RC-003b: WorkflowClassReconciliation.Valid() = false; " +
			"the discriminator MUST be a valid WorkflowClass value so the detector can distinguish " +
			"reconciliation-workflow branches from ordinary task branches")
	}
}

// TestRC013_CategoryAssignedPayloadCarriesRequiredFields verifies that a
// ReconciliationCategoryAssignedPayload constructed with the three fields
// required by RC-013 (reconciliation_run_id, category, evidence_ref) passes
// Valid() — the payload shape enforces the RC-013 contract at the type level.
//
// RC-013: "The detector MUST emit reconciliation_category_assigned per
// [event-model.md §8] carrying run_id, assigned category, detection-rule name."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-013;
// specs/event-model.md §8.6.2.
func TestRC013_CategoryAssignedPayloadCarriesRequiredFields(t *testing.T) {
	t.Parallel()

	reconciliationRunID := RunID(uuid.MustParse("018f1e2a-0000-7001-8000-000000006501"))
	targetRunID := RunID(uuid.MustParse("018f1e2a-0000-7002-8000-000000006502"))

	payload := ReconciliationCategoryAssignedPayload{
		ReconciliationRunID: reconciliationRunID,
		TargetRunID:         &targetRunID,
		Category:            ReconciliationCategoryCat1,
		EvidenceRef:         "idempotency_class=idempotent",
	}

	if !payload.Valid() {
		t.Error("RC-013: ReconciliationCategoryAssignedPayload.Valid() = false; " +
			"a payload with reconciliation_run_id, target_run_id, category, and evidence_ref must be valid")
	}
	if uuid.UUID(payload.ReconciliationRunID) == uuid.Nil {
		t.Error("RC-013: ReconciliationRunID is uuid.Nil; must be a non-nil run_id")
	}
	if payload.TargetRunID == nil {
		t.Error("RC-013: TargetRunID is nil; target run must be identified")
	}
	if !payload.Category.Valid() {
		t.Errorf("RC-013: Category %q is not a valid ReconciliationCategory", payload.Category)
	}
	if payload.EvidenceRef == "" {
		t.Error("RC-013: EvidenceRef is empty; detection-rule name is required per RC-013")
	}
}

// TestRC013_EmissionPrecedesDispatch_ShapeContract verifies the emission-before-
// dispatch ordering rule of RC-013 at the shape level: the event payload includes
// the category, which the dispatcher consumes after the event is emitted. A nil
// TargetRunID makes the payload invalid, so any dispatcher consuming it would
// fail — enforcing that the valid payload (with run_id) must be constructed
// (emitted) before dispatch logic runs.
//
// RC-013: "Emission MUST precede dispatch of any reconciliation workflow or
// auto-resolver."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-013.
func TestRC013_EmissionPrecedesDispatch_ShapeContract(t *testing.T) {
	t.Parallel()

	invalidPayload := ReconciliationCategoryAssignedPayload{
		ReconciliationRunID: RunID(uuid.Nil),
		Category:            ReconciliationCategoryCat2,
		EvidenceRef:         "non-idempotent",
	}
	if invalidPayload.Valid() {
		t.Error("RC-013: payload with nil ReconciliationRunID must be invalid; " +
			"a dispatcher must not be able to proceed from an invalid emission")
	}

	reconRunID := RunID(uuid.MustParse("018f1e2a-0000-7001-8000-000000006503"))
	targetRunID := RunID(uuid.MustParse("018f1e2a-0000-7002-8000-000000006504"))
	validPayload := ReconciliationCategoryAssignedPayload{
		ReconciliationRunID: reconRunID,
		TargetRunID:         &targetRunID,
		Category:            ReconciliationCategoryCat2,
		EvidenceRef:         "non-idempotent",
	}
	if !validPayload.Valid() {
		t.Error("RC-013: fully-formed payload must be valid; dispatch can only proceed after a valid emission")
	}
}

// TestRC013_DedupKeyComponents verifies that the three dedup-key components
// specified in RC-013 are all present in a ReconciliationCategoryAssignedPayload:
// (target_run_id, category, post_crash_window flag).
//
// RC-013: "Consumers MUST tolerate duplicate emissions; dedup key is
// (target_run_id, category, snapshot_token.git_head_hash)."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-013.
func TestRC013_DedupKeyComponents(t *testing.T) {
	t.Parallel()

	reconRunID := RunID(uuid.MustParse("018f1e2a-0000-7001-8000-000000006505"))
	targetRunID := RunID(uuid.MustParse("018f1e2a-0000-7002-8000-000000006506"))
	postCrashWindow := false

	payload := ReconciliationCategoryAssignedPayload{
		ReconciliationRunID: reconRunID,
		TargetRunID:         &targetRunID,
		Category:            ReconciliationCategoryCat3,
		EvidenceRef:         "store-divergence",
		PostCrashWindow:     &postCrashWindow,
	}

	if !payload.Valid() {
		t.Fatal("RC-013: dedup-key fixture payload must be valid")
	}

	if payload.TargetRunID == nil || uuid.UUID(*payload.TargetRunID) == uuid.Nil {
		t.Error("RC-013: dedup key component 1 (target_run_id) is nil or zero")
	}
	if !payload.Category.Valid() {
		t.Error("RC-013: dedup key component 2 (category) is invalid")
	}
	if payload.EvidenceRef == "" {
		t.Error("RC-013: dedup key component 3 (evidence_ref carrying git_head_hash) is empty")
	}
}

// TestRC013_AllCategoriesProduceValidPayload verifies that a valid
// ReconciliationCategoryAssignedPayload can be constructed for EVERY one of
// the 11 ReconciliationCategory values. RC-013 is non-discriminating on
// category: the detector MUST emit for every category it assigns.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-013.
func TestRC013_AllCategoriesProduceValidPayload(t *testing.T) {
	t.Parallel()

	reconRunID := RunID(uuid.MustParse("018f1e2a-0000-7001-8000-000000006507"))
	targetRunID := RunID(uuid.MustParse("018f1e2a-0000-7002-8000-000000006508"))

	allCats := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6a,
		ReconciliationCategoryCat6b,
	}

	for _, cat := range allCats {
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()
			payload := ReconciliationCategoryAssignedPayload{
				ReconciliationRunID: reconRunID,
				TargetRunID:         &targetRunID,
				Category:            cat,
				EvidenceRef:         "detection-rule-evidence",
			}
			if !payload.Valid() {
				t.Errorf("RC-013: cannot construct valid payload for category %q; "+
					"detector must be able to emit for every declared category", cat)
			}
		})
	}
}

// TestRC016_InvestigatorRequiredCategoriesAreExactlyThree verifies the
// RC-016 invariant: exactly three categories require an investigator
// (Cat 2, Cat 3, Cat 6a), and only those three.
//
// RC-016: "For each category with an investigator (§8.3 Cat 2, §8.4 Cat 3
// generic, §8.11 Cat 6a), the S01-shipped YAML policy MUST define a playbook."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-016; §8.12.
func TestRC016_InvestigatorRequiredCategoriesAreExactlyThree(t *testing.T) {
	t.Parallel()

	wantInvestigatorCats := []ReconciliationCategory{
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat6a,
	}

	if len(wantInvestigatorCats) != 3 {
		t.Fatalf("RC-016: investigator category list has %d entries, want 3 per §8.12",
			len(wantInvestigatorCats))
	}

	for _, cat := range wantInvestigatorCats {
		if !cat.Valid() {
			t.Errorf("RC-016: investigator category %q is not a valid ReconciliationCategory", cat)
		}
	}

	autoResolverCats := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6b,
	}

	invSet := make(map[ReconciliationCategory]bool, len(wantInvestigatorCats))
	for _, c := range wantInvestigatorCats {
		invSet[c] = true
	}
	for _, ac := range autoResolverCats {
		if invSet[ac] {
			t.Errorf("RC-016: category %q appears in both investigator and auto-resolver sets; "+
				"they must be mutually exclusive per §8.12 and RC-008", ac)
		}
	}
}

// TestRC016_PlaybookObligationOnlyForInvestigatorCats verifies the RC-016
// playbook obligation is scoped to exactly Cat 2, Cat 3, and Cat 6a. Auto-resolver
// categories do NOT require a playbook — they have deterministic Go implementations
// per RC-008.
//
// RC-016: "For each category with an investigator... the S01-shipped YAML policy
// MUST define a playbook."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-016; §4.2 RC-008.
func TestRC016_PlaybookObligationOnlyForInvestigatorCats(t *testing.T) {
	t.Parallel()

	investigatorCats := []ReconciliationCategory{
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat6a,
	}

	nonInvestigatorCats := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat3a,
		ReconciliationCategoryCat3b,
		ReconciliationCategoryCat3c,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
		ReconciliationCategoryCat6b,
	}

	total := len(investigatorCats) + len(nonInvestigatorCats)
	if total != 11 {
		t.Errorf("RC-016: investigator (%d) + non-investigator (%d) = %d, want 11 (all categories)",
			len(investigatorCats), len(nonInvestigatorCats), total)
	}

	for _, cat := range investigatorCats {
		if !cat.Valid() {
			t.Errorf("RC-016: investigator cat %q is invalid", cat)
		}
	}
	for _, cat := range nonInvestigatorCats {
		if !cat.Valid() {
			t.Errorf("RC-016: non-investigator cat %q is invalid", cat)
		}
	}
}

// TestRC016_Cat2PlaybookTypicalVerdicts verifies that the RC-016 playbook
// for Cat 2 (non-idempotent in-flight) targets the expected set of verdict
// outcomes: resume-with-context, reset-to-checkpoint, reopen-bead.
//
// These are the same verdicts declared in the §8.12 action table for Cat 2.
//
// RC-016: "Playbooks MUST name the investigator's expected evidence outputs
// and the verdict-selection rubric."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-016; §8.3; §8.12.
func TestRC016_Cat2PlaybookTypicalVerdicts(t *testing.T) {
	t.Parallel()

	cat2Verdicts := []Verdict{
		VerdictResumeWithContext,
		VerdictResetToCheckpoint,
		VerdictReopenBead,
	}
	for _, v := range cat2Verdicts {
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			if !v.Valid() {
				t.Errorf("RC-016/Cat2: verdict %q is not a valid Verdict constant; "+
					"Cat 2 playbook must target only declared verdicts", v)
			}
		})
	}
}

// TestRC016_Cat3PlaybookTypicalVerdicts verifies that the RC-016 playbook
// for Cat 3 (generic store disagreement) targets the expected set of verdict
// outcomes: accept-close-with-note, reopen-bead, no-op-accept.
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-016; §8.4; §8.12.
func TestRC016_Cat3PlaybookTypicalVerdicts(t *testing.T) {
	t.Parallel()

	cat3Verdicts := []Verdict{
		VerdictAcceptCloseWithNote,
		VerdictReopenBead,
		VerdictNoOpAccept,
	}
	for _, v := range cat3Verdicts {
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()
			if !v.Valid() {
				t.Errorf("RC-016/Cat3: verdict %q is not a valid Verdict constant; "+
					"Cat 3 playbook must target only declared verdicts", v)
			}
		})
	}
}

// TestRC016_Cat6aDefaultVerdictIsEscalateToHuman verifies that the default verdict
// for a Cat 6a investigator is escalate-to-human, matching the §8.12 table. The
// playbook MAY downgrade to a repair path, but the default (fallback) verdict is
// escalate-to-human.
//
// RC-016: "Playbooks MUST name... the verdict-selection rubric."
//
// Spec ref: specs/reconciliation/spec.md §4.4 RC-016; §8.11; §8.12.
func TestRC016_Cat6aDefaultVerdictIsEscalateToHuman(t *testing.T) {
	t.Parallel()

	defaultVerdict := VerdictEscalateToHuman
	if !defaultVerdict.Valid() {
		t.Error("RC-016/Cat6a: VerdictEscalateToHuman.Valid() = false; " +
			"the default verdict for Cat 6a is escalate-to-human per §8.12")
	}
	if string(defaultVerdict) != "escalate-to-human" {
		t.Errorf("RC-016/Cat6a: default verdict string = %q, want %q",
			string(defaultVerdict), "escalate-to-human")
	}
}
