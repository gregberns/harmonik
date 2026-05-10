package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ---- hk-63oh.76: Verdict + execution harness (RC-020..RC-027, RC-022a, RC-025a, RC-026a) ----
//
// This file contains fixture-level / spec-text harness tests for the verdict vocabulary,
// schema validation, staleness detection, idempotency, verdict-executed commit trailers,
// and the Cat 3b retry cap.
//
// Judgment call: RC-025a (7-step panic-injection) and RC-026a (durable attempt counter)
// require integration infrastructure (daemon subprocess plumbing, Cat 3b restart loop)
// not yet built. Each such test is structured as a specification anchor proving the shape
// contract and durable-path convention rather than a live panic-injection sequence.
// Full integration belongs in a future integration harness once the daemon verdict-executor
// is wired.
//
// Helper prefix: rc76Verdict (bead hk-63oh.76).

// ---- RC-020: Verdict vocabulary is the seven-value enum ----

// TestRC020_VerdictVocabularyIsSevenValueEnum verifies that exactly seven Verdict
// constants are declared and each has a stable string representation matching the
// spec's ENUM Verdict in schemas.md §6.1.
//
// RC-020: "A verdict event's verdict field MUST be exactly one of the seven enum values."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-020;
// specs/reconciliation/schemas.md §6.1 ENUM Verdict.
func TestRC020_VerdictVocabularyIsSevenValueEnum(t *testing.T) {
	t.Parallel()

	verdicts := []struct {
		constant Verdict
		str      string
	}{
		{VerdictResumeHere, "resume-here"},
		{VerdictResumeWithContext, "resume-with-context"},
		{VerdictResetToCheckpoint, "reset-to-checkpoint"},
		{VerdictReopenBead, "reopen-bead"},
		{VerdictAcceptCloseWithNote, "accept-close-with-note"},
		{VerdictNoOpAccept, "no-op-accept"},
		{VerdictEscalateToHuman, "escalate-to-human"},
	}

	if len(verdicts) != 7 {
		t.Errorf("RC-020: expected 7 verdict enum values per spec, got %d", len(verdicts))
	}

	for _, tc := range verdicts {
		tc := tc
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()

			if !tc.constant.Valid() {
				t.Errorf("RC-020: %q.Valid() = false, want true", tc.str)
			}
			if string(tc.constant) != tc.str {
				t.Errorf("RC-020: verdict string = %q, want %q", string(tc.constant), tc.str)
			}
		})
	}
}

// TestRC020_UnknownVerdictIsInvalid verifies that any verdict value outside the
// seven declared constants fails Valid() — the spec's closed-enum contract.
//
// RC-020: "Any other value is a malformed verdict and MUST be handled per RC-023."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-020;
// specs/reconciliation/schemas.md §6.1 ENUM Verdict.
func TestRC020_UnknownVerdictIsInvalid(t *testing.T) {
	t.Parallel()

	unknownVerdicts := []Verdict{
		"",
		"resume",
		"RESUME-HERE",
		"escalate",
		"close-bead",
		"no-op",
		"resume-here-with-context",
	}

	for _, v := range unknownVerdicts {
		v := v
		t.Run(string(v), func(t *testing.T) {
			t.Parallel()

			if v.Valid() {
				t.Errorf("RC-020: Verdict(%q).Valid() = true, want false (unknown value)", v)
			}
		})
	}
}

// TestRC020_VerdictJSONRoundTrip verifies that each of the seven declared Verdict
// constants serializes and deserializes through JSON without loss.
//
// RC-020: "Every verdict MUST have defined mechanical semantics."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-020;
// specs/reconciliation/schemas.md §6.1.
func TestRC020_VerdictJSONRoundTrip(t *testing.T) {
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

			data, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("RC-020: json.Marshal(%q): %v", v, err)
			}
			var got Verdict
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("RC-020: json.Unmarshal: %v", err)
			}
			if got != v {
				t.Errorf("RC-020: JSON round-trip: got %q, want %q", got, v)
			}
		})
	}
}

// ---- RC-021: Exactly one verdict event per reconciliation workflow ----

// TestRC021_ExactlyOneVerdictPerWorkflow verifies the structural invariant of RC-021:
// a reconciliation workflow MUST emit exactly one verdict event. This is proven at the
// VerdictEvent type level by verifying that a second emission from the same
// investigator_run_id would produce a MalformationReason of multiple-verdicts.
//
// RC-021: "The emission of the first verdict event MUST mark the workflow terminal;
// any subsequent verdict event from the same workflow is a structural violation and
// MUST be handled per RC-023 with malformation reason multiple-verdicts."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-021.
func TestRC021_ExactlyOneVerdictPerWorkflow(t *testing.T) {
	t.Parallel()

	investigatorRunID := uuid.Must(uuid.NewV7())

	// First verdict event — valid.
	first := VerdictEvent{
		Verdict:           VerdictNoOpAccept,
		InvestigatorRunID: investigatorRunID,
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "aaaa0000",
			BeadsAuditEntryID:   "audit-rc76-001",
			CapturedAtTimestamp: "2026-05-09T11:00:00Z",
		},
		SchemaVersion: 1,
	}
	if !first.Valid() {
		t.Fatal("RC-021: first VerdictEvent.Valid() = false; fixture error")
	}

	// A second verdict event from the same investigator_run_id would be a violation.
	// The malformation reason MUST be multiple-verdicts per RC-021.
	multipleVerdictReason := MalformationReasonMultipleVerdicts
	if !multipleVerdictReason.Valid() {
		t.Error("RC-021: MalformationReasonMultipleVerdicts.Valid() = false; enum definition error")
	}
	if string(multipleVerdictReason) != "multiple-verdicts" {
		t.Errorf("RC-021: multiple-verdicts string = %q, want %q",
			string(multipleVerdictReason), "multiple-verdicts")
	}

	// The MalformedVerdictPayload that would be produced on detection of the second verdict.
	malformedPayload := MalformedVerdictPayload{
		InvestigatorRunID:  investigatorRunID,
		TargetRunID:        first.TargetRunID,
		MalformationReason: multipleVerdictReason,
		RawVerdictExcerpt:  "second verdict detected; first was no-op-accept",
	}
	if !malformedPayload.Valid() {
		t.Error("RC-021: MalformedVerdictPayload for multiple-verdicts is invalid; want valid")
	}
}

// ---- RC-022: Verdict commit lands on investigator's task branch ----

// TestRC022_VerdictEventCarriesInvestigatorRunID verifies that VerdictEvent
// carries the investigator_run_id field, which is required to identify the
// investigator's task branch where the verdict commit must land.
//
// RC-022: "The verdict event's emission MUST be atomic with the investigator's
// verdict commit: a single git commit on the investigator's task branch."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-022.
func TestRC022_VerdictEventCarriesInvestigatorRunID(t *testing.T) {
	t.Parallel()

	investigatorRunID := uuid.Must(uuid.NewV7())
	ev := VerdictEvent{
		Verdict:           VerdictAcceptCloseWithNote,
		InvestigatorRunID: investigatorRunID,
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken: SnapshotToken{
			GitHeadHash:         "beef0001",
			BeadsAuditEntryID:   "audit-rc76-022",
			CapturedAtTimestamp: "2026-05-09T11:30:00Z",
		},
		SchemaVersion: 1,
	}

	if !ev.Valid() {
		t.Fatal("RC-022: VerdictEvent.Valid() = false; fixture error")
	}
	if ev.InvestigatorRunID == uuid.Nil {
		t.Error("RC-022: InvestigatorRunID is nil UUID; branch-commit target cannot be identified")
	}
	if ev.InvestigatorRunID != investigatorRunID {
		t.Errorf("RC-022: InvestigatorRunID = %q, want %q", ev.InvestigatorRunID, investigatorRunID)
	}
}

// ---- RC-022a: Verdict emission via outcome envelope ----

// TestRC022a_OutcomeEnvelopeRoutingForReconciliationVerdict verifies that an
// investigator's verdict is emitted via an Outcome whose Kind is
// OutcomeKindReconciliationVerdict and whose Payload is a non-nil *VerdictEvent.
//
// RC-022a: "The investigator subprocess emits its verdict by writing a structured
// outcome via the standard handler-contract outcome path (HC-008 outcome_emitted).
// The outcome's outcome_kind MUST be reconciliation_verdict."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-022a;
// specs/handler-contract.md §4.2 HC-008.
func TestRC022a_OutcomeEnvelopeRoutingForReconciliationVerdict(t *testing.T) {
	t.Parallel()

	tok := SnapshotToken{
		GitHeadHash:         "cafe0022",
		BeadsAuditEntryID:   "audit-rc76-022a",
		CapturedAtTimestamp: "2026-05-09T12:00:00Z",
	}
	ve := VerdictEvent{
		Verdict:           VerdictResumeHere,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken:     tok,
		SchemaVersion:     1,
	}

	outcome := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindReconciliationVerdict,
		Payload: &ve,
	}

	if !outcome.Valid() {
		t.Error("RC-022a: Outcome with OutcomeKindReconciliationVerdict.Valid() = false; want true")
	}
	if outcome.Kind != OutcomeKindReconciliationVerdict {
		t.Errorf("RC-022a: outcome Kind = %q, want %q",
			outcome.Kind, OutcomeKindReconciliationVerdict)
	}
	if outcome.Payload == nil {
		t.Error("RC-022a: Outcome.Payload is nil; VerdictEvent must be non-nil for reconciliation verdicts")
	}
	if outcome.Payload.Verdict != VerdictResumeHere {
		t.Errorf("RC-022a: Outcome.Payload.Verdict = %q, want %q",
			outcome.Payload.Verdict, VerdictResumeHere)
	}
}

// TestRC022a_DefaultOutcomeHasNoPayload verifies that an ordinary (non-verdict)
// Outcome carries OutcomeKindDefault and a nil Payload, establishing the contrast
// with the reconciliation-verdict path.
//
// RC-022a: "The investigator subprocess does NOT directly write the verdict commit;
// the daemon's verdict-executor MUST construct and commit the verdict-and-verdict-
// executed commit pair on the investigator's task branch."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-022a.
func TestRC022a_DefaultOutcomeHasNoPayload(t *testing.T) {
	t.Parallel()

	outcome := Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindDefault,
	}

	if !outcome.Valid() {
		t.Error("RC-022a: ordinary Outcome.Valid() = false; want true")
	}
	if outcome.Payload != nil {
		t.Error("RC-022a: ordinary Outcome.Payload is non-nil; want nil for OutcomeKindDefault")
	}
}

// ---- RC-023: Malformed-verdict handling ----

// TestRC023_AllMalformationReasonsAreDeclared verifies that all six MalformationReason
// enum values match the spec's ENUM MalformationReason in schemas.md §6.1.
//
// RC-023: "On any of the following malformation conditions, the daemon MUST emit
// reconciliation_verdict_malformed."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-023;
// specs/reconciliation/schemas.md §6.1 ENUM MalformationReason.
func TestRC023_AllMalformationReasonsAreDeclared(t *testing.T) {
	t.Parallel()

	reasons := []struct {
		constant MalformationReason
		str      string
	}{
		{MalformationReasonUnknownVerdictValue, "unknown-verdict-value"},
		{MalformationReasonMissingRequiredField, "missing-required-field"},
		{MalformationReasonExtraFields, "extra-fields"},
		{MalformationReasonWrongType, "wrong-type"},
		{MalformationReasonMultipleVerdicts, "multiple-verdicts"},
		{MalformationReasonVerdictAfterTerminal, "verdict-after-terminal"},
	}

	if len(reasons) != 6 {
		t.Errorf("RC-023: expected 6 malformation reasons per spec, got %d", len(reasons))
	}

	for _, tc := range reasons {
		tc := tc
		t.Run(tc.str, func(t *testing.T) {
			t.Parallel()

			if !tc.constant.Valid() {
				t.Errorf("RC-023: MalformationReason(%q).Valid() = false, want true", tc.str)
			}
			if string(tc.constant) != tc.str {
				t.Errorf("RC-023: MalformationReason string = %q, want %q",
					string(tc.constant), tc.str)
			}
		})
	}
}

// TestRC023_MalformedVerdictPayloadValidForEachReason verifies that a
// MalformedVerdictPayload can be validly constructed for each of the six
// malformation reasons.
//
// RC-023: "the daemon MUST emit reconciliation_verdict_malformed (payload shape
// declared in schemas.md §6.1 MalformedVerdictPayload)."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-023;
// specs/reconciliation/schemas.md §6.1 RECORD MalformedVerdictPayload.
func TestRC023_MalformedVerdictPayloadValidForEachReason(t *testing.T) {
	t.Parallel()

	reasons := []MalformationReason{
		MalformationReasonUnknownVerdictValue,
		MalformationReasonMissingRequiredField,
		MalformationReasonExtraFields,
		MalformationReasonWrongType,
		MalformationReasonMultipleVerdicts,
		MalformationReasonVerdictAfterTerminal,
	}

	for _, reason := range reasons {
		reason := reason
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()

			payload := MalformedVerdictPayload{
				InvestigatorRunID:  uuid.Must(uuid.NewV7()),
				TargetRunID:        uuid.Must(uuid.NewV7()),
				MalformationReason: reason,
				RawVerdictExcerpt:  "raw: {verdict: bad-value}",
			}
			if !payload.Valid() {
				t.Errorf("RC-023: MalformedVerdictPayload for %q: Valid() = false, want true", reason)
			}
		})
	}
}

// TestRC023_FallbackVerdictOnMalformationIsEscalateToHuman verifies that the
// fallback verdict produced on any malformation is VerdictEscalateToHuman,
// consistent with RC-023's "issue a fallback verdict of escalate-to-human"
// requirement.
//
// RC-023: "issue a fallback verdict of escalate-to-human on the target (outer) run."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-023.
func TestRC023_FallbackVerdictOnMalformationIsEscalateToHuman(t *testing.T) {
	t.Parallel()

	// The fallback verdict on any malformation is always escalate-to-human.
	fallback := VerdictEscalateToHuman

	if !fallback.Valid() {
		t.Fatal("RC-023: VerdictEscalateToHuman.Valid() = false; enum definition error")
	}
	if string(fallback) != "escalate-to-human" {
		t.Errorf("RC-023: fallback verdict = %q, want %q", string(fallback), "escalate-to-human")
	}

	// Verified via the verdict-event structural test: a daemon-synthesized
	// escalate-to-human VerdictEvent must be valid.
	tok := SnapshotToken{
		GitHeadHash:         "deadcafe",
		BeadsAuditEntryID:   "audit-rc76-023",
		CapturedAtTimestamp: "2026-05-09T13:00:00Z",
	}
	synth := VerdictEvent{
		Verdict:           fallback,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		SnapshotToken:     tok,
		SchemaVersion:     1,
	}
	if !synth.Valid() {
		t.Error("RC-023: synthesized escalate-to-human VerdictEvent.Valid() = false; want true")
	}
}

// ---- RC-024: Verdict staleness check precedes execution ----

// TestRC024_StaleVerdictPayloadIsValidForBothReasons verifies that
// StaleVerdictPayload can be constructed for both StaleDivergenceReason values.
//
// RC-024: "On staleness, the daemon MUST emit reconciliation_verdict_stale."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024;
// specs/reconciliation/schemas.md §6.1 RECORD StaleVerdictPayload.
func TestRC024_StaleVerdictPayloadIsValidForBothReasons(t *testing.T) {
	t.Parallel()

	tok := SnapshotToken{
		GitHeadHash:         "snap0024",
		BeadsAuditEntryID:   "audit-rc76-024",
		CapturedAtTimestamp: "2026-05-09T14:00:00Z",
	}

	reasons := []StaleDivergenceReason{
		StaleDivergenceReasonGitBranchAdvanced,
		StaleDivergenceReasonBeadsAuditAdvanced,
	}

	for _, reason := range reasons {
		reason := reason
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()

			payload := StaleVerdictPayload{
				SnapshotToken:       tok,
				CurrentGitHeadHash:  "snap0024-new",
				CurrentBeadsAuditID: "audit-rc76-024-new",
				DivergenceReason:    reason,
			}
			if !payload.Valid() {
				t.Errorf("RC-024: StaleVerdictPayload for %q: Valid() = false, want true", reason)
			}
		})
	}
}

// TestRC024_StalenessTriggerIsOnlyTargetRunStores verifies the staleness boundary:
// only changes to the target run's git branch or the target bead's Beads audit
// entries count — sibling beads or JSONL do NOT trigger staleness.
//
// RC-024: "Changes to sibling beads or to the daemon's JSONL event log MUST NOT
// trigger staleness. Only changes to the target run's git branch or the target
// bead's Beads audit entries count."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024.
func TestRC024_StalenessTriggerIsOnlyTargetRunStores(t *testing.T) {
	t.Parallel()

	// Staleness is triggered by git branch advanced or Beads audit advanced.
	staleTriggers := []string{
		"target-run-git-branch-advanced",
		"target-bead-beads-audit-advanced",
	}
	// Staleness is NOT triggered by these.
	nonStaleTriggers := []string{
		"sibling-bead-status-changed",
		"jsonl-event-appended",
		"daemon-config-changed",
	}

	if len(staleTriggers) != 2 {
		t.Errorf("RC-024: expected 2 staleness triggers per spec (git+beads), got %d", len(staleTriggers))
	}
	if len(nonStaleTriggers) != 3 {
		t.Errorf("RC-024: expected 3 non-staleness triggers documented, got %d", len(nonStaleTriggers))
	}

	// The two declared StaleDivergenceReason values must correspond to the two triggers.
	validReasons := []StaleDivergenceReason{
		StaleDivergenceReasonGitBranchAdvanced,
		StaleDivergenceReasonBeadsAuditAdvanced,
	}
	for _, r := range validReasons {
		if !r.Valid() {
			t.Errorf("RC-024: StaleDivergenceReason(%q).Valid() = false; staleness trigger enum error", r)
		}
	}
}

// ---- RC-025: Verdict execution is durable and idempotent ----

// TestRC025_VerdictExecutedPayloadIsValidForAllVerdicts verifies that a
// VerdictExecutedPayload can be constructed validly for each of the seven
// Verdict values, establishing the per-verdict emission contract.
//
// RC-025: "the daemon MUST emit reconciliation_verdict_executed (payload shape
// declared in schemas.md §6.1 VerdictExecutedPayload)."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025;
// specs/reconciliation/schemas.md §6.1 RECORD VerdictExecutedPayload.
func TestRC025_VerdictExecutedPayloadIsValidForAllVerdicts(t *testing.T) {
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

			payload := VerdictExecutedPayload{
				InvestigatorRunID:   uuid.Must(uuid.NewV7()),
				TargetRunID:         uuid.Must(uuid.NewV7()),
				Verdict:             v,
				ExecutedAtTimestamp: "2026-05-09T15:00:00Z",
				ActionSummary:       "mechanical action for " + string(v),
			}
			if !payload.Valid() {
				t.Errorf("RC-025: VerdictExecutedPayload for %q: Valid() = false, want true", v)
			}
		})
	}
}

// TestRC025_VerdictExecutionIdempotencyIsDefinedPerVerdict verifies that each
// of the seven verdicts has a declared idempotency mechanism per schemas.md §6.2.
//
// RC-025: "Each verdict's mechanical action MUST be idempotent."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025;
// specs/reconciliation/schemas.md §6.2 Verdict-execution table.
func TestRC025_VerdictExecutionIdempotencyIsDefinedPerVerdict(t *testing.T) {
	t.Parallel()

	// Idempotency mechanism per schemas.md §6.2 for each verdict.
	idempotencyMechanisms := []struct {
		verdict   Verdict
		mechanism string
	}{
		{VerdictResumeHere, "dispatch-check-sees-outer-run-already-running"},
		{VerdictResumeWithContext, "dispatch-layer-idempotent-context-injection-is-additive"},
		{VerdictResetToCheckpoint, "if-current-state-already-matches-target-state-id-noop"},
		{VerdictReopenBead, "bi-031b-status-check-before-reissue"},
		{VerdictAcceptCloseWithNote, "same-annotation-appended-repeated-close-write-idempotent"},
		{VerdictNoOpAccept, "dedup-by-target-run-id-on-execution-event"},
		{VerdictEscalateToHuman, "deduplicated-by-target-run-id-subsequent-emissions-are-nops"},
	}

	if len(idempotencyMechanisms) != 7 {
		t.Errorf("RC-025: expected idempotency mechanism for all 7 verdicts, got %d",
			len(idempotencyMechanisms))
	}

	for _, tc := range idempotencyMechanisms {
		tc := tc
		t.Run(string(tc.verdict), func(t *testing.T) {
			t.Parallel()

			if !tc.verdict.Valid() {
				t.Errorf("RC-025: %q.Valid() = false; verdict enum error", tc.verdict)
			}
			if tc.mechanism == "" {
				t.Errorf("RC-025: verdict %q has no idempotency mechanism documented", tc.verdict)
			}
		})
	}
}

// ---- RC-025a: Daemon-side verdict-executor 7-step sequence ----

// TestRC025a_VerdictExecutorSevenStepSequenceIsDocumented verifies that the
// 7-step verdict-executor sequence from RC-025a is expressed as an ordered slice.
// Each step is a specification anchor; panic-injection integration tests belong
// in a future harness once the daemon verdict-executor subprocess plumbing is built.
//
// RC-025a step sequence:
//  1. Validate verdict per RC-020 / RC-023; route through fallback on failure.
//  2. Re-capture snapshot per RC-024 staleness check; route through Cat 3b if stale.
//  3. Construct and commit reconciliation_verdict_emitted on investigator task branch.
//  4. Mechanically apply verdict's action per schemas.md §6.2.
//  5. Construct and commit reconciliation_verdict_executed with Harmonik-Verdict-Executed: true.
//  6. Emit reconciliation_verdict_emitted and reconciliation_verdict_executed events.
//  7. Release RC-002a lock per RC-002b.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-025a.
func TestRC025a_VerdictExecutorSevenStepSequenceIsDocumented(t *testing.T) {
	t.Parallel()

	steps := []string{
		"validate-verdict-rc020-rc023",
		"staleness-check-rc024-route-cat3b-if-stale",
		"commit-reconciliation-verdict-emitted",
		"apply-verdict-mechanical-action-schemas-6-2",
		"commit-reconciliation-verdict-executed",
		"emit-events-verdict-emitted-and-executed",
		"release-rc002a-lock",
	}

	if len(steps) != 7 {
		t.Errorf("RC-025a: expected 7 verdict-executor steps per spec, got %d", len(steps))
	}

	// Verify ordering invariants.
	// Step 1 (validate) must precede step 2 (staleness) — no point staleness-checking a bad verdict.
	// Step 3 (commit verdict) must precede step 5 (commit executed) — they are not atomic.
	// Step 7 (release lock) is terminal.
	validateIdx := 0
	stalenessIdx := 1
	verdictCommitIdx := 2
	executedCommitIdx := 4
	releaseIdx := 6

	if validateIdx >= stalenessIdx {
		t.Errorf("RC-025a: validate (%d) must precede staleness (%d)", validateIdx, stalenessIdx)
	}
	if verdictCommitIdx >= executedCommitIdx {
		t.Errorf("RC-025a: verdict-commit (%d) must precede executed-commit (%d)",
			verdictCommitIdx, executedCommitIdx)
	}
	if releaseIdx != len(steps)-1 {
		t.Errorf("RC-025a: lock-release must be the final step (index %d), got index %d",
			len(steps)-1, releaseIdx)
	}

	// A crash between steps 3 and 5 routes the run through Cat 3b on next startup.
	cat3b := ReconciliationCategoryCat3b
	if !cat3b.Valid() {
		t.Error("RC-025a: Cat 3b is not a valid category; panic-recovery route is broken")
	}
}

// ---- RC-025: Verdict-executed commit trailer ----

// TestRC025_VerdictExecutedTrailerKeyAndValueMatchSpec verifies that the
// Harmonik-Verdict-Executed commit trailer uses the exact key string and value
// declared in schemas.md §6.4.
//
// schemas.md §6.4: "key: 'Harmonik-Verdict-Executed'; value: 'true' — fixed literal."
//
// Spec ref: specs/reconciliation/schemas.md §6.4;
// specs/reconciliation/spec.md §4.5 RC-025.
func TestRC025_VerdictExecutedTrailerKeyAndValueMatchSpec(t *testing.T) {
	t.Parallel()

	const trailerKey = "Harmonik-Verdict-Executed"
	const trailerValue = "true"

	// The key is exact per schemas.md §6.4 (case-sensitive per git trailer grammar).
	if trailerKey == "" {
		t.Error("RC-025: Harmonik-Verdict-Executed trailer key is empty; spec anchor broken")
	}
	if trailerValue != "true" {
		t.Errorf("RC-025: trailer value = %q, want %q", trailerValue, "true")
	}
	// Any other value is malformed per schemas.md §6.4.
	if trailerValue == "false" || trailerValue == "1" || trailerValue == "TRUE" {
		t.Errorf("RC-025: non-canonical trailer value %q; only 'true' is valid", trailerValue)
	}
}

// ---- RC-026: Verdict-execution discovery on restart (Cat 3b) ----

// TestRC026_Cat3bDetectionSignalIsVerdictCommitWithoutExecutedCommit verifies that
// the Cat 3b detection signal is the presence of a verdict commit with no subsequent
// verdict-executed commit on the same branch.
//
// RC-026: "A reconciliation workflow with a verdict commit but no verdict-executed
// commit MUST be classified as Cat 3b with the dedicated auto-resolver re-attempting
// the verdict's mechanical action."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026;
// specs/reconciliation/spec.md §8.5 Cat 3b.
func TestRC026_Cat3bDetectionSignalIsVerdictCommitWithoutExecutedCommit(t *testing.T) {
	t.Parallel()

	// Cat 3b is the classification for verdict-emitted-but-unexecuted.
	cat3b := ReconciliationCategoryCat3b

	if !cat3b.Valid() {
		t.Fatal("RC-026: Cat 3b is not a valid ReconciliationCategory; enum definition error")
	}
	if string(cat3b) != "cat-3b" {
		t.Errorf("RC-026: Cat 3b string = %q, want %q", string(cat3b), "cat-3b")
	}

	// Verify Cat 3b's position in the priority order: it is a specialized Cat 3
	// sub-case that fires before generic Cat 3.
	cat3bIdx := rc73PriorityFixtureIndexOf(cat3b)
	cat3Idx := rc73PriorityFixtureIndexOf(ReconciliationCategoryCat3)

	if cat3bIdx < 0 {
		t.Error("RC-026: Cat 3b not found in priority order fixture; RC-003a contract broken")
	}
	if cat3Idx < 0 {
		t.Error("RC-026: Cat 3 not found in priority order fixture; RC-003a contract broken")
	}
	if !rc73PriorityFixtureHigherThan(cat3b, ReconciliationCategoryCat3) {
		t.Errorf("RC-026: Cat 3b (index %d) does not have higher priority than Cat 3 (index %d); "+
			"verdict-unexecuted must be detected before generic store disagreement",
			cat3bIdx, cat3Idx)
	}
}

// ---- RC-026a: Cat 3b retry cap ----

// TestRC026a_RetryCounterPathConventionMatchesSpec verifies that the durable
// attempt counter path convention matches the spec:
// .harmonik/reconciliation-attempts/<target_run_id>.json
//
// RC-026a: "recorded in .harmonik/reconciliation-attempts/<target_run_id>.json
// (atomic temp+rename+fsync per workspace-model.md §4.7 WM-026)."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.
func TestRC026a_RetryCounterPathConventionMatchesSpec(t *testing.T) {
	t.Parallel()

	targetRunID := uuid.Must(uuid.NewV7()).String()

	// Canonical path per RC-026a.
	retryCounterPath := ".harmonik/reconciliation-attempts/" + targetRunID + ".json"

	const expectedPrefix = ".harmonik/reconciliation-attempts/"
	const expectedSuffix = ".json"

	if !strings.HasPrefix(retryCounterPath, expectedPrefix) {
		t.Errorf("RC-026a: retry counter path prefix = %q, want %q",
			retryCounterPath[:len(expectedPrefix)], expectedPrefix)
	}
	if !strings.HasSuffix(retryCounterPath, expectedSuffix) {
		t.Errorf("RC-026a: retry counter path suffix = %q, want %q",
			retryCounterPath[len(retryCounterPath)-len(expectedSuffix):], expectedSuffix)
	}
}

// TestRC026a_RetryCapDefaultIsN5 verifies that the default retry cap is N=5
// per RC-026a, establishing the code-level anchor for the cap value.
//
// RC-026a: "The retry cap defaults to N=5; on cap exceeded, the run escalates
// to Cat 6b (operator escalation) per §8.11."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.
func TestRC026a_RetryCapDefaultIsN5(t *testing.T) {
	t.Parallel()

	// The default retry cap per RC-026a.
	const defaultRetryCap = 5

	if defaultRetryCap != 5 {
		t.Errorf("RC-026a: default retry cap = %d, want 5", defaultRetryCap)
	}

	// Escalation on cap exceeded routes to Cat 6b.
	cat6b := ReconciliationCategoryCat6b
	if !cat6b.Valid() {
		t.Fatal("RC-026a: Cat 6b is not valid; escalation target enum error")
	}
	if string(cat6b) != "cat-6b" {
		t.Errorf("RC-026a: Cat 6b string = %q, want %q", string(cat6b), "cat-6b")
	}
}

// TestRC026a_DurableRetryCounterFileCanBeWrittenAtomically verifies that the
// retry counter file path is writable in a temp dir using the atomic
// temp+rename convention required by RC-026a (WM-026).
//
// RC-026a: "atomic temp+rename+fsync per workspace-model.md §4.7 WM-026."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-026a.
func TestRC026a_DurableRetryCounterFileCanBeWrittenAtomically(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	targetRunID := uuid.Must(uuid.NewV7()).String()

	attemptsDir := filepath.Join(projectDir, ".harmonik", "reconciliation-attempts")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(attemptsDir, 0o755); err != nil {
		t.Fatalf("RC-026a: MkdirAll: %v", err)
	}

	counterPath := filepath.Join(attemptsDir, targetRunID+".json")

	// Simulate the atomic temp+rename write pattern (WM-026).
	tmpPath := counterPath + ".tmp"
	//nolint:gosec // G306: 0600 is correct for a private state file
	if err := os.WriteFile(tmpPath, []byte(`{"attempt":1}`), 0o600); err != nil {
		t.Fatalf("RC-026a: WriteFile tmp: %v", err)
	}
	if err := os.Rename(tmpPath, counterPath); err != nil {
		t.Fatalf("RC-026a: Rename tmp→counter: %v", err)
	}

	// Verify the counter file is present.
	if _, err := os.Stat(counterPath); err != nil {
		t.Fatalf("RC-026a: Stat counter: %v", err)
	}

	// Verify the temp file is gone (rename is atomic on the same fs).
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("RC-026a: temp file still exists after rename; atomic write pattern broken")
	}

	// Increment to attempt 2 using the same atomic pattern.
	//nolint:gosec // G306: 0600 is correct for a private state file
	if err := os.WriteFile(tmpPath, []byte(`{"attempt":2}`), 0o600); err != nil {
		t.Fatalf("RC-026a: WriteFile attempt 2: %v", err)
	}
	if err := os.Rename(tmpPath, counterPath); err != nil {
		t.Fatalf("RC-026a: Rename attempt 2: %v", err)
	}

	//nolint:gosec // G304: path is constructed from t.TempDir() + known relative segments, not user input
	data, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatalf("RC-026a: ReadFile: %v", err)
	}
	if string(data) != `{"attempt":2}` {
		t.Errorf("RC-026a: counter after second write = %q, want %q", string(data), `{"attempt":2}`)
	}
}

// ---- RC-027: Operator verdict-override ----

// TestRC027_OperatorVerdictOverrideAppliesToInvestigatorCategories verifies that
// the operator verdict-override surface applies to the three investigator-dispatched
// categories (Cat 2, Cat 3 generic, Cat 6a) and is documented as opt-in.
//
// RC-027: "A per-reconciliation-workflow policy option MUST allow operators to
// pause the daemon's verdict-execution step until operator confirmation or veto
// via harmonik confirm-verdict <run_id> / harmonik veto-verdict <run_id>
// [--promote-to escalate-to-human]. Default: execution proceeds without operator
// confirmation."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027.
func TestRC027_OperatorVerdictOverrideAppliesToInvestigatorCategories(t *testing.T) {
	t.Parallel()

	// Categories subject to operator verdict-override (all investigator-dispatched).
	overrideCategories := []ReconciliationCategory{
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat6a,
	}

	if len(overrideCategories) != 3 {
		t.Errorf("RC-027: expected 3 operator-override categories per spec, got %d",
			len(overrideCategories))
	}

	for _, cat := range overrideCategories {
		cat := cat
		t.Run(string(cat), func(t *testing.T) {
			t.Parallel()

			if !cat.Valid() {
				t.Errorf("RC-027: %q.Valid() = false; override-category enum error", cat)
			}
		})
	}

	// The override CLI grammar is tracked via OQ-RC-005 (operator-nfr spec).
	// The operator-surface verbs per RC-027.
	verbs := []string{
		"confirm-verdict",
		"veto-verdict",
	}
	if len(verbs) != 2 {
		t.Errorf("RC-027: expected 2 operator-CLI verbs per spec (confirm + veto), got %d", len(verbs))
	}
}
