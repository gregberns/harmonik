package core

import (
	"testing"

	"github.com/google/uuid"
)

func outcomeValid(t *testing.T) Outcome {
	t.Helper()
	return Outcome{
		Status:           OutcomeStatusSuccess,
		PreferredLabel:   nil,
		SuggestedNextIDs: nil,
		ContextUpdates:   nil,
		Notes:            "",
		Kind:             OutcomeKindDefault,
		Payload:          nil,
	}
}

func outcomeValidPayload(t *testing.T) *VerdictEvent {
	t.Helper()
	ctx := "investigator context"
	return &VerdictEvent{
		Verdict:           VerdictResumeWithContext,
		InvestigatorRunID: uuid.Must(uuid.NewV7()),
		TargetRunID:       uuid.Must(uuid.NewV7()),
		Context:           &ctx,
		SchemaVersion:     1,
	}
}

func TestOutcomeValid_MinimalDefault(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	if !o.Valid() {
		t.Error("Valid() = false for minimal default Outcome, want true")
	}
}

func TestOutcomeValid_FullDefaultOptionals(t *testing.T) {
	t.Parallel()

	label := "success-path"
	o := Outcome{
		Status:           OutcomeStatusPartialSuccess,
		PreferredLabel:   &label,
		SuggestedNextIDs: []NodeID{"node-a", "node-b"},
		ContextUpdates:   map[string]any{"key": "value", "count": 42},
		Notes:            "freeform notes",
		Kind:             OutcomeKindDefault,
		Payload:          nil,
	}
	if !o.Valid() {
		t.Error("Valid() = false for fully-populated default Outcome, want true")
	}
}

func TestOutcomeValid_ReconciliationVerdictWithPayload(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindReconciliationVerdict,
		Payload: outcomeValidPayload(t),
	}
	if !o.Valid() {
		t.Error("Valid() = false for reconciliation_verdict Outcome with payload, want true")
	}
}

func TestOutcomeValid_InvalidStatus(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Status = OutcomeStatus("UNKNOWN")
	if o.Valid() {
		t.Error("Valid() = true with invalid Status, want false")
	}
}

func TestOutcomeValid_EmptyStatus(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Status = OutcomeStatus("")
	if o.Valid() {
		t.Error("Valid() = true with empty Status, want false")
	}
}

func TestOutcomeValid_AllStatusValues(t *testing.T) {
	t.Parallel()

	statuses := []OutcomeStatus{
		OutcomeStatusSuccess,
		OutcomeStatusFail,
		OutcomeStatusRetry,
		OutcomeStatusPartialSuccess,
	}
	for _, s := range statuses {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			o := outcomeValid(t)
			o.Status = s
			if !o.Valid() {
				t.Errorf("Valid() = false for status=%q, want true", s)
			}
		})
	}
}

func TestOutcomeValid_InvalidKind(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Kind = OutcomeKind("unknown_kind")
	if o.Valid() {
		t.Error("Valid() = true with invalid Kind, want false")
	}
}

func TestOutcomeValid_EmptyKind(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Kind = OutcomeKind("")
	if o.Valid() {
		t.Error("Valid() = true with empty Kind, want false")
	}
}

func TestOutcomeValid_DefaultKindWithPayload_Rejected(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Kind = OutcomeKindDefault
	o.Payload = outcomeValidPayload(t)
	if o.Valid() {
		t.Error("Valid() = true with Kind=default and non-nil Payload, want false (spec §6.1: payload absent when kind=default)")
	}
}

func TestOutcomeValid_ReconciliationVerdictKindWithoutPayload_Rejected(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Kind = OutcomeKindReconciliationVerdict
	o.Payload = nil
	if o.Valid() {
		t.Error("Valid() = true with Kind=reconciliation_verdict and nil Payload, want false (spec §6.1 RC-022a)")
	}
}

func TestOutcomeValid_EmptySuggestedNextIDs(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.SuggestedNextIDs = []NodeID{}
	if !o.Valid() {
		t.Error("Valid() = false for empty SuggestedNextIDs slice, want true")
	}
}

func TestOutcomeValid_NilContextUpdates(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.ContextUpdates = nil
	if !o.Valid() {
		t.Error("Valid() = false for nil ContextUpdates, want true")
	}
}

func TestOutcomeValid_EmptyContextUpdates(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.ContextUpdates = map[string]any{}
	if !o.Valid() {
		t.Error("Valid() = false for empty ContextUpdates map, want true")
	}
}

func TestOutcomeValid_EmptyNotes(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Notes = ""
	if !o.Valid() {
		t.Error("Valid() = false for empty Notes, want true")
	}
}

func TestOutcomeValid_NilPreferredLabel(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.PreferredLabel = nil
	if !o.Valid() {
		t.Error("Valid() = false for nil PreferredLabel, want true")
	}
}

func TestOutcomeValid_DefaultKindNilPayload(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status: OutcomeStatusFail,
		Kind:   OutcomeKindDefault,
	}
	if !o.Valid() {
		t.Error("Valid() = false for default kind with nil Payload, want true")
	}
}

func outcomeGDPValidPayload(t *testing.T) *GateDecisionPayload {
	t.Helper()
	return &GateDecisionPayload{
		PolicyID:      "review-gate",
		Decision:      GateActionAllow,
		DecisionActor: "mechanism",
	}
}

func outcomeGDPValidPayloadDeny(t *testing.T) *GateDecisionPayload {
	t.Helper()
	return &GateDecisionPayload{
		PolicyID:      "review-gate",
		Decision:      GateActionDeny,
		DecisionActor: "reviewer",
	}
}

func outcomeGDPValidPayloadEscalate(t *testing.T) *GateDecisionPayload {
	t.Helper()
	sig := "pending-human-review"
	return &GateDecisionPayload{
		PolicyID:           "review-gate",
		Decision:           GateActionEscalateToHuman,
		DecisionActor:      "reviewer",
		ResolutionSignalID: &sig,
	}
}

func TestOutcomeValid_FailureClassAbsentOnSuccess(t *testing.T) {
	t.Parallel()

	fc := FailureClassTransient
	o := outcomeValid(t)
	o.Status = OutcomeStatusSuccess
	o.FailureClass = &fc
	if o.Valid() {
		t.Error("Valid() = true with FailureClass set on SUCCESS outcome, want false (HC-058: absent on non-FAIL)")
	}
}

func TestOutcomeValid_FailureClassAbsentOnRetry(t *testing.T) {
	t.Parallel()

	fc := FailureClassTransient
	o := outcomeValid(t)
	o.Status = OutcomeStatusRetry
	o.FailureClass = &fc
	if o.Valid() {
		t.Error("Valid() = true with FailureClass set on RETRY outcome, want false (HC-058: absent on non-FAIL)")
	}
}

func TestOutcomeValid_FailureClassAbsentOnPartialSuccess(t *testing.T) {
	t.Parallel()

	fc := FailureClassStructural
	o := outcomeValid(t)
	o.Status = OutcomeStatusPartialSuccess
	o.FailureClass = &fc
	if o.Valid() {
		t.Error("Valid() = true with FailureClass set on PARTIAL_SUCCESS outcome, want false (HC-058: absent on non-FAIL)")
	}
}

func TestOutcomeValid_FailureClassPresentOnFail(t *testing.T) {
	t.Parallel()

	classes := []FailureClass{
		FailureClassTransient,
		FailureClassStructural,
		FailureClassDeterministic,
		FailureClassCanceled,
		FailureClassBudgetExhausted,
		FailureClassCompilationLoop,
	}
	for _, fc := range classes {
		t.Run(string(fc), func(t *testing.T) {
			t.Parallel()
			o := outcomeValid(t)
			o.Status = OutcomeStatusFail
			o.FailureClass = &fc
			if !o.Valid() {
				t.Errorf("Valid() = false with FailureClass=%q and Status=FAIL, want true", fc)
			}
		})
	}
}

func TestOutcomeValid_NilFailureClassOnFail(t *testing.T) {
	t.Parallel()

	o := outcomeValid(t)
	o.Status = OutcomeStatusFail
	o.FailureClass = nil
	if !o.Valid() {
		t.Error("Valid() = false with nil FailureClass on FAIL outcome, want true (handler omits; daemon back-fills)")
	}
}

func TestOutcomeValid_InvalidFailureClassValue(t *testing.T) {
	t.Parallel()

	fc := FailureClass("not_a_real_class")
	o := outcomeValid(t)
	o.Status = OutcomeStatusFail
	o.FailureClass = &fc
	if o.Valid() {
		t.Error("Valid() = true with invalid FailureClass value, want false")
	}
}

func TestOutcomeValid_GateDecisionKindWithValidPayload(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindGateDecision,
		Payload: outcomeGDPValidPayload(t),
	}
	if !o.Valid() {
		t.Error("Valid() = false for gate_decision Outcome with valid payload, want true")
	}
}

func TestOutcomeValid_GateDecisionKindWithDenyPayload(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindGateDecision,
		Payload: outcomeGDPValidPayloadDeny(t),
	}
	if !o.Valid() {
		t.Error("Valid() = false for gate_decision Outcome with deny payload, want true")
	}
}

func TestOutcomeValid_GateDecisionKindWithEscalatePayload(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindGateDecision,
		Payload: outcomeGDPValidPayloadEscalate(t),
	}
	if !o.Valid() {
		t.Error("Valid() = false for gate_decision Outcome with escalate-to-human payload, want true")
	}
}

func TestOutcomeValid_GateDecisionKindWithNilPayload_Rejected(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindGateDecision,
		Payload: nil,
	}
	if o.Valid() {
		t.Error("Valid() = true for gate_decision Outcome with nil Payload, want false")
	}
}

func TestOutcomeValid_GateDecisionKindWithWrongPayloadType_Rejected(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindGateDecision,
		Payload: outcomeValidPayload(t),
	}
	if o.Valid() {
		t.Error("Valid() = true with kind=gate_decision and *VerdictEvent payload, want false (wrong type)")
	}
}

func TestOutcomeValid_GateDecisionKindWithInvalidPayload_Rejected(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status: OutcomeStatusSuccess,
		Kind:   OutcomeKindGateDecision,
		Payload: &GateDecisionPayload{
			PolicyID:      "",
			Decision:      GateActionAllow,
			DecisionActor: "mechanism",
		},
	}
	if o.Valid() {
		t.Error("Valid() = true with gate_decision kind and invalid GateDecisionPayload, want false")
	}
}

func TestOutcomeValid_DefaultKindWithGDPPayload_Rejected(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindDefault,
		Payload: outcomeGDPValidPayload(t),
	}
	if o.Valid() {
		t.Error("Valid() = true with kind=default and non-nil *GateDecisionPayload Payload, want false")
	}
}

func TestOutcomeValid_ReconciliationVerdictKindWithGDPPayload_Rejected(t *testing.T) {
	t.Parallel()

	o := Outcome{
		Status:  OutcomeStatusSuccess,
		Kind:    OutcomeKindReconciliationVerdict,
		Payload: outcomeGDPValidPayload(t),
	}
	if o.Valid() {
		t.Error("Valid() = true with kind=reconciliation_verdict and *GateDecisionPayload payload, want false")
	}
}

func TestOutcomeKindGateDecisionValid(t *testing.T) {
	t.Parallel()

	if !OutcomeKindGateDecision.Valid() {
		t.Error("OutcomeKindGateDecision.Valid() = false, want true")
	}
}

func TestOutcomeKindGateDecisionMarshalText(t *testing.T) {
	t.Parallel()

	got, err := OutcomeKindGateDecision.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "gate_decision" {
		t.Errorf("MarshalText = %q, want %q", string(got), "gate_decision")
	}
}

func TestOutcomeKindGateDecisionUnmarshalText(t *testing.T) {
	t.Parallel()

	var k OutcomeKind
	if err := k.UnmarshalText([]byte("gate_decision")); err != nil {
		t.Fatalf("UnmarshalText error: %v", err)
	}
	if k != OutcomeKindGateDecision {
		t.Errorf("UnmarshalText got %q, want %q", k, OutcomeKindGateDecision)
	}
}

func TestGateDecisionPayloadValid_Minimal(t *testing.T) {
	t.Parallel()

	p := outcomeGDPValidPayload(t)
	if !p.Valid() {
		t.Error("GateDecisionPayload.Valid() = false for minimal valid payload, want true")
	}
}

func TestGateDecisionPayloadValid_EmptyPolicyID_Rejected(t *testing.T) {
	t.Parallel()

	p := outcomeGDPValidPayload(t)
	p.PolicyID = ""
	if p.Valid() {
		t.Error("GateDecisionPayload.Valid() = true with empty PolicyID, want false")
	}
}

func TestGateDecisionPayloadValid_EmptyDecisionActor_Rejected(t *testing.T) {
	t.Parallel()

	p := outcomeGDPValidPayload(t)
	p.DecisionActor = ""
	if p.Valid() {
		t.Error("GateDecisionPayload.Valid() = true with empty DecisionActor, want false")
	}
}

func TestGateDecisionPayloadValid_InvalidDecision_Rejected(t *testing.T) {
	t.Parallel()

	p := outcomeGDPValidPayload(t)
	p.Decision = GateAction("bogus")
	if p.Valid() {
		t.Error("GateDecisionPayload.Valid() = true with invalid Decision value, want false")
	}
}

func TestGateDecisionPayloadValid_EscalateWithoutSignal_Rejected(t *testing.T) {
	t.Parallel()

	p := &GateDecisionPayload{
		PolicyID:           "gate",
		Decision:           GateActionEscalateToHuman,
		DecisionActor:      "reviewer",
		ResolutionSignalID: nil, // required when escalate-to-human
	}
	if p.Valid() {
		t.Error("GateDecisionPayload.Valid() = true with escalate-to-human and nil ResolutionSignalID, want false")
	}
}

func TestGateDecisionPayloadValid_EscalateWithEmptySignal_Rejected(t *testing.T) {
	t.Parallel()

	empty := ""
	p := &GateDecisionPayload{
		PolicyID:           "gate",
		Decision:           GateActionEscalateToHuman,
		DecisionActor:      "reviewer",
		ResolutionSignalID: &empty,
	}
	if p.Valid() {
		t.Error("GateDecisionPayload.Valid() = true with escalate-to-human and empty ResolutionSignalID, want false")
	}
}

func TestGateDecisionPayloadValid_AllowWithSignal_Rejected(t *testing.T) {
	t.Parallel()

	sig := "unexpected-signal"
	p := &GateDecisionPayload{
		PolicyID:           "gate",
		Decision:           GateActionAllow,
		DecisionActor:      "mechanism",
		ResolutionSignalID: &sig, // must be nil for allow/deny
	}
	if p.Valid() {
		t.Error("GateDecisionPayload.Valid() = true with allow and non-nil ResolutionSignalID, want false")
	}
}

func TestGateDecisionPayloadValid_DenyWithSignal_Rejected(t *testing.T) {
	t.Parallel()

	sig := "unexpected-signal"
	p := &GateDecisionPayload{
		PolicyID:           "gate",
		Decision:           GateActionDeny,
		DecisionActor:      "reviewer",
		ResolutionSignalID: &sig, // must be nil for deny
	}
	if p.Valid() {
		t.Error("GateDecisionPayload.Valid() = true with deny and non-nil ResolutionSignalID, want false")
	}
}

func TestGateDecisionPayloadValid_WithOptionalEvidenceRef(t *testing.T) {
	t.Parallel()

	evRef := "evidence/gate-audit-001"
	p := &GateDecisionPayload{
		PolicyID:            "review-gate",
		Decision:            GateActionAllow,
		DecisionActor:       "mechanism",
		DecisionEvidenceRef: &evRef,
	}
	if !p.Valid() {
		t.Error("GateDecisionPayload.Valid() = false with optional DecisionEvidenceRef set, want true")
	}
}

func TestGateDecisionPayloadValid_EscalateWithSignalAndEvidence(t *testing.T) {
	t.Parallel()

	sig := "review-pending"
	evRef := "evidence/gate-audit-002"
	p := &GateDecisionPayload{
		PolicyID:            "review-gate",
		Decision:            GateActionEscalateToHuman,
		DecisionActor:       "reviewer",
		ResolutionSignalID:  &sig,
		DecisionEvidenceRef: &evRef,
	}
	if !p.Valid() {
		t.Error("GateDecisionPayload.Valid() = false for escalate-to-human with all fields, want true")
	}
}
