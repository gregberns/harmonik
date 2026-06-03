package core

// reconciliationevents_hqwn59_prop_test.go — property tests for the Valid()
// methods declared in reconciliationevents_hqwn59.go.
//
// Naming: TestProp_* per testing.md §Decisions #10.
// Approach: rapid generator builds a valid payload, flips exactly one required
// field to its zero/invalid value, asserts Valid()==false; all-valid -> true.
//
// Refs: hk-qgzso (property-test coverage uplift for hk-j3hrn core uplift).

import (
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"
)

// ============================================================
// ReconciliationTrigger
// ============================================================

func TestProp_ReconciliationTrigger_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allReconciliationTriggers {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if ReconciliationTrigger(s).Valid() {
			rt.Errorf("Valid() == true for unknown ReconciliationTrigger %q", s)
		}
	})
}

func TestProp_ReconciliationTrigger_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allReconciliationTriggers).Draw(rt, "trigger")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared ReconciliationTrigger %q", v)
		}
	})
}

// ============================================================
// ReconciliationStartedPayload
// ============================================================

func TestProp_ReconciliationStartedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationStartedPayload{
			ReconciliationRunID: RunID(drawNonNilUUID(rt, "run_id")),
			Trigger:             rapid.SampledFrom(allReconciliationTriggers).Draw(rt, "trigger"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ReconciliationStartedPayload")
		}
	})
}

func TestProp_ReconciliationStartedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationStartedPayload{
			ReconciliationRunID: RunID(uuid.Nil),
			Trigger:             rapid.SampledFrom(allReconciliationTriggers).Draw(rt, "trigger"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil ReconciliationRunID")
		}
	})
}

func TestProp_ReconciliationStartedPayload_InvalidTriggerRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allReconciliationTriggers {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "trigger_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := ReconciliationStartedPayload{
			ReconciliationRunID: RunID(drawNonNilUUID(rt, "run_id")),
			Trigger:             ReconciliationTrigger(s),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown trigger %q", s)
		}
	})
}

// ============================================================
// ReconciliationCategoryAssignedPayload
// ============================================================

func TestProp_ReconciliationCategoryAssignedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationCategoryAssignedPayload{
			ReconciliationRunID: RunID(drawNonNilUUID(rt, "run_id")),
			Category:            rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
			EvidenceRef:         drawNonEmptyString(rt, "evidence_ref"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ReconciliationCategoryAssignedPayload")
		}
	})
}

func TestProp_ReconciliationCategoryAssignedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationCategoryAssignedPayload{
			ReconciliationRunID: RunID(uuid.Nil),
			Category:            rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
			EvidenceRef:         drawNonEmptyString(rt, "evidence_ref"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil ReconciliationRunID")
		}
	})
}

func TestProp_ReconciliationCategoryAssignedPayload_NilTargetRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilRunID := RunID(uuid.Nil)
		p := ReconciliationCategoryAssignedPayload{
			ReconciliationRunID: RunID(drawNonNilUUID(rt, "run_id")),
			TargetRunID:         &nilRunID, // non-nil but uuid.Nil
			Category:            rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
			EvidenceRef:         drawNonEmptyString(rt, "evidence_ref"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when TargetRunID points to uuid.Nil")
		}
	})
}

func TestProp_ReconciliationCategoryAssignedPayload_InvalidCategoryRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allReconciliationCategories {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "cat_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := ReconciliationCategoryAssignedPayload{
			ReconciliationRunID: RunID(drawNonNilUUID(rt, "run_id")),
			Category:            ReconciliationCategory(s),
			EvidenceRef:         drawNonEmptyString(rt, "evidence_ref"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown category %q", s)
		}
	})
}

func TestProp_ReconciliationCategoryAssignedPayload_EmptyEvidenceRefRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationCategoryAssignedPayload{
			ReconciliationRunID: RunID(drawNonNilUUID(rt, "run_id")),
			Category:            rapid.SampledFrom(allReconciliationCategories).Draw(rt, "category"),
			EvidenceRef:         "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EvidenceRef")
		}
	})
}

// ============================================================
// ReconciliationVerdictEmittedPayload
// ============================================================

func TestProp_ReconciliationVerdictEmittedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationVerdictEmittedPayload{
			InvestigatorRunID: RunID(drawNonNilUUID(rt, "inv_run_id")),
			TargetRunID:       RunID(drawNonNilUUID(rt, "target_run_id")),
			Verdict:           rapid.SampledFrom(allVerdicts).Draw(rt, "verdict"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ReconciliationVerdictEmittedPayload")
		}
	})
}

func TestProp_ReconciliationVerdictEmittedPayload_NilInvestigatorRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationVerdictEmittedPayload{
			InvestigatorRunID: RunID(uuid.Nil),
			TargetRunID:       RunID(drawNonNilUUID(rt, "target_run_id")),
			Verdict:           rapid.SampledFrom(allVerdicts).Draw(rt, "verdict"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil InvestigatorRunID")
		}
	})
}

func TestProp_ReconciliationVerdictEmittedPayload_NilTargetRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationVerdictEmittedPayload{
			InvestigatorRunID: RunID(drawNonNilUUID(rt, "inv_run_id")),
			TargetRunID:       RunID(uuid.Nil),
			Verdict:           rapid.SampledFrom(allVerdicts).Draw(rt, "verdict"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil TargetRunID")
		}
	})
}

func TestProp_ReconciliationVerdictEmittedPayload_InvalidVerdictRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allVerdicts {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "verdict_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := ReconciliationVerdictEmittedPayload{
			InvestigatorRunID: RunID(drawNonNilUUID(rt, "inv_run_id")),
			TargetRunID:       RunID(drawNonNilUUID(rt, "target_run_id")),
			Verdict:           Verdict(s),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown verdict %q", s)
		}
	})
}

func TestProp_ReconciliationVerdictEmittedPayload_EmptyRationaleRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		empty := ""
		p := ReconciliationVerdictEmittedPayload{
			InvestigatorRunID: RunID(drawNonNilUUID(rt, "inv_run_id")),
			TargetRunID:       RunID(drawNonNilUUID(rt, "target_run_id")),
			Verdict:           rapid.SampledFrom(allVerdicts).Draw(rt, "verdict"),
			Rationale:         &empty, // non-nil but empty
		}
		if p.Valid() {
			rt.Error("Valid() should be false when Rationale is non-nil but empty")
		}
	})
}

// ============================================================
// DivergenceKind
// ============================================================

func TestProp_DivergenceKind_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allDivergenceKinds {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if DivergenceKind(s).Valid() {
			rt.Errorf("Valid() == true for unknown DivergenceKind %q", s)
		}
	})
}

func TestProp_DivergenceKind_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allDivergenceKinds).Draw(rt, "kind")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared DivergenceKind %q", v)
		}
	})
}

// ============================================================
// StoreDivergenceDetectedPayload
// ============================================================

func TestProp_StoreDivergenceDetectedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StoreDivergenceDetectedPayload{
			DivergenceKind: rapid.SampledFrom(allDivergenceKinds).Draw(rt, "kind"),
			EvidenceRef:    drawNonEmptyString(rt, "evidence_ref"),
			Corroboration:  rapid.SampledFrom(allDivergenceCorroborations).Draw(rt, "corroboration"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed StoreDivergenceDetectedPayload")
		}
	})
}

func TestProp_StoreDivergenceDetectedPayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := StoreDivergenceDetectedPayload{
			RunID:          &nilID, // non-nil but uuid.Nil
			DivergenceKind: rapid.SampledFrom(allDivergenceKinds).Draw(rt, "kind"),
			EvidenceRef:    drawNonEmptyString(rt, "evidence_ref"),
			Corroboration:  rapid.SampledFrom(allDivergenceCorroborations).Draw(rt, "corroboration"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when RunID points to uuid.Nil")
		}
	})
}

func TestProp_StoreDivergenceDetectedPayload_EmptyBeadIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		emptyBeadID := BeadID("")
		p := StoreDivergenceDetectedPayload{
			BeadID:         &emptyBeadID, // non-nil but empty
			DivergenceKind: rapid.SampledFrom(allDivergenceKinds).Draw(rt, "kind"),
			EvidenceRef:    drawNonEmptyString(rt, "evidence_ref"),
			Corroboration:  rapid.SampledFrom(allDivergenceCorroborations).Draw(rt, "corroboration"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when BeadID is non-nil but empty")
		}
	})
}

func TestProp_StoreDivergenceDetectedPayload_InvalidKindRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allDivergenceKinds {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "kind_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := StoreDivergenceDetectedPayload{
			DivergenceKind: DivergenceKind(s),
			EvidenceRef:    drawNonEmptyString(rt, "evidence_ref"),
			Corroboration:  rapid.SampledFrom(allDivergenceCorroborations).Draw(rt, "corroboration"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown divergence kind %q", s)
		}
	})
}

func TestProp_StoreDivergenceDetectedPayload_EmptyEvidenceRefRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := StoreDivergenceDetectedPayload{
			DivergenceKind: rapid.SampledFrom(allDivergenceKinds).Draw(rt, "kind"),
			EvidenceRef:    "",
			Corroboration:  rapid.SampledFrom(allDivergenceCorroborations).Draw(rt, "corroboration"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EvidenceRef")
		}
	})
}

func TestProp_StoreDivergenceDetectedPayload_InvalidCorroborationRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allDivergenceCorroborations {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "corr_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := StoreDivergenceDetectedPayload{
			DivergenceKind: rapid.SampledFrom(allDivergenceKinds).Draw(rt, "kind"),
			EvidenceRef:    drawNonEmptyString(rt, "evidence_ref"),
			Corroboration:  DivergenceCorroboration(s),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown corroboration %q", s)
		}
	})
}

// ============================================================
// OperatorEscalationReason
// ============================================================

func TestProp_OperatorEscalationReason_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allOperatorEscalationReasons {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if OperatorEscalationReason(s).Valid() {
			rt.Errorf("Valid() == true for unknown OperatorEscalationReason %q", s)
		}
	})
}

func TestProp_OperatorEscalationReason_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allOperatorEscalationReasons).Draw(rt, "reason")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared OperatorEscalationReason %q", v)
		}
	})
}

// ============================================================
// OperatorEscalationRequiredPayload
// ============================================================

func TestProp_OperatorEscalationRequiredPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := OperatorEscalationRequiredPayload{
			Reason: rapid.SampledFrom(allOperatorEscalationReasons).Draw(rt, "reason"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed OperatorEscalationRequiredPayload")
		}
	})
}

func TestProp_OperatorEscalationRequiredPayload_NilTargetRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := OperatorEscalationRequiredPayload{
			TargetRunID: &nilID,
			Reason:      rapid.SampledFrom(allOperatorEscalationReasons).Draw(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when TargetRunID points to uuid.Nil")
		}
	})
}

func TestProp_OperatorEscalationRequiredPayload_InvalidReasonRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allOperatorEscalationReasons {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "reason_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := OperatorEscalationRequiredPayload{
			Reason: OperatorEscalationReason(s),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown reason %q", s)
		}
	})
}

// ============================================================
// DivergenceInconclusiveReason
// ============================================================

func TestProp_DivergenceInconclusiveReason_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allDivergenceInconclusiveReasons {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if DivergenceInconclusiveReason(s).Valid() {
			rt.Errorf("Valid() == true for unknown DivergenceInconclusiveReason %q", s)
		}
	})
}

func TestProp_DivergenceInconclusiveReason_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allDivergenceInconclusiveReasons).Draw(rt, "reason")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared DivergenceInconclusiveReason %q", v)
		}
	})
}

// ============================================================
// DivergenceInconclusivePayload
// ============================================================

func TestProp_DivergenceInconclusivePayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DivergenceInconclusivePayload{
			EvidenceRef: drawNonEmptyString(rt, "evidence_ref"),
			Reason:      rapid.SampledFrom(allDivergenceInconclusiveReasons).Draw(rt, "reason"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed DivergenceInconclusivePayload")
		}
	})
}

func TestProp_DivergenceInconclusivePayload_NilRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := DivergenceInconclusivePayload{
			RunID:       &nilID,
			EvidenceRef: drawNonEmptyString(rt, "evidence_ref"),
			Reason:      rapid.SampledFrom(allDivergenceInconclusiveReasons).Draw(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when RunID points to uuid.Nil")
		}
	})
}

func TestProp_DivergenceInconclusivePayload_EmptyBeadIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		emptyBeadID := BeadID("")
		p := DivergenceInconclusivePayload{
			BeadID:      &emptyBeadID,
			EvidenceRef: drawNonEmptyString(rt, "evidence_ref"),
			Reason:      rapid.SampledFrom(allDivergenceInconclusiveReasons).Draw(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when BeadID is non-nil but empty")
		}
	})
}

func TestProp_DivergenceInconclusivePayload_EmptyEvidenceRefRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := DivergenceInconclusivePayload{
			EvidenceRef: "",
			Reason:      rapid.SampledFrom(allDivergenceInconclusiveReasons).Draw(rt, "reason"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty EvidenceRef")
		}
	})
}

func TestProp_DivergenceInconclusivePayload_InvalidReasonRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allDivergenceInconclusiveReasons {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "reason_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := DivergenceInconclusivePayload{
			EvidenceRef: drawNonEmptyString(rt, "evidence_ref"),
			Reason:      DivergenceInconclusiveReason(s),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown reason %q", s)
		}
	})
}

// ============================================================
// ReconciliationDispatchDeduplicatedPayload
// ============================================================

func TestProp_ReconciliationDispatchDeduplicatedPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationDispatchDeduplicatedPayload{
			TargetRunID: RunID(drawNonNilUUID(rt, "target_run_id")),
			DedupAt:     drawNonEmptyString(rt, "dedup_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ReconciliationDispatchDeduplicatedPayload")
		}
	})
}

func TestProp_ReconciliationDispatchDeduplicatedPayload_NilTargetRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationDispatchDeduplicatedPayload{
			TargetRunID: RunID(uuid.Nil),
			DedupAt:     drawNonEmptyString(rt, "dedup_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil TargetRunID")
		}
	})
}

func TestProp_ReconciliationDispatchDeduplicatedPayload_NilExistingInvestigatorRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		nilID := RunID(uuid.Nil)
		p := ReconciliationDispatchDeduplicatedPayload{
			TargetRunID:               RunID(drawNonNilUUID(rt, "target_run_id")),
			ExistingInvestigatorRunID: &nilID,
			DedupAt:                   drawNonEmptyString(rt, "dedup_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false when ExistingInvestigatorRunID points to uuid.Nil")
		}
	})
}

func TestProp_ReconciliationDispatchDeduplicatedPayload_EmptyDedupAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationDispatchDeduplicatedPayload{
			TargetRunID: RunID(drawNonNilUUID(rt, "target_run_id")),
			DedupAt:     "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty DedupAt")
		}
	})
}

// ============================================================
// ReconciliationDetectorPanicPayload
// ============================================================

func TestProp_ReconciliationDetectorPanicPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationDetectorPanicPayload{
			DetectorClass: DetectorClass(drawNonEmptyString(rt, "detector_class")),
			ErrorClass:    rapid.SampledFrom(allErrorCategories).Draw(rt, "error_class"),
			PanickedAt:    drawNonEmptyString(rt, "panicked_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ReconciliationDetectorPanicPayload")
		}
	})
}

func TestProp_ReconciliationDetectorPanicPayload_EmptyDetectorClassRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationDetectorPanicPayload{
			DetectorClass: DetectorClass(""),
			ErrorClass:    rapid.SampledFrom(allErrorCategories).Draw(rt, "error_class"),
			PanickedAt:    drawNonEmptyString(rt, "panicked_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty DetectorClass")
		}
	})
}

func TestProp_ReconciliationDetectorPanicPayload_InvalidErrorClassRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allErrorCategories {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "err_class_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := ReconciliationDetectorPanicPayload{
			DetectorClass: DetectorClass(drawNonEmptyString(rt, "detector_class")),
			ErrorClass:    ErrorCategory(s),
			PanickedAt:    drawNonEmptyString(rt, "panicked_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown error class %q", s)
		}
	})
}

func TestProp_ReconciliationDetectorPanicPayload_EmptyPanickedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationDetectorPanicPayload{
			DetectorClass: DetectorClass(drawNonEmptyString(rt, "detector_class")),
			ErrorClass:    rapid.SampledFrom(allErrorCategories).Draw(rt, "error_class"),
			PanickedAt:    "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty PanickedAt")
		}
	})
}

// ============================================================
// ReconciliationVerdictExecutionRetryPayload
// ============================================================

func TestProp_ReconciliationVerdictExecutionRetryPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationVerdictExecutionRetryPayload{
			TargetRunID: RunID(drawNonNilUUID(rt, "target_run_id")),
			Attempt:     rapid.IntRange(1, 100).Draw(rt, "attempt"),
			RetriedAt:   drawNonEmptyString(rt, "retried_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed ReconciliationVerdictExecutionRetryPayload")
		}
	})
}

func TestProp_ReconciliationVerdictExecutionRetryPayload_NilTargetRunIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationVerdictExecutionRetryPayload{
			TargetRunID: RunID(uuid.Nil),
			Attempt:     rapid.IntRange(1, 100).Draw(rt, "attempt"),
			RetriedAt:   drawNonEmptyString(rt, "retried_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with nil TargetRunID")
		}
	})
}

func TestProp_ReconciliationVerdictExecutionRetryPayload_ZeroAttemptRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationVerdictExecutionRetryPayload{
			TargetRunID: RunID(drawNonNilUUID(rt, "target_run_id")),
			Attempt:     rapid.IntRange(-100, 0).Draw(rt, "attempt"),
			RetriedAt:   drawNonEmptyString(rt, "retried_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for Attempt=%d (must be >= 1)", p.Attempt)
		}
	})
}

func TestProp_ReconciliationVerdictExecutionRetryPayload_EmptyRetriedAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := ReconciliationVerdictExecutionRetryPayload{
			TargetRunID: RunID(drawNonNilUUID(rt, "target_run_id")),
			Attempt:     rapid.IntRange(1, 100).Draw(rt, "attempt"),
			RetriedAt:   "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty RetriedAt")
		}
	})
}

// ============================================================
// BeadTerminalTransitionOp
// ============================================================

func TestProp_BeadTerminalTransitionOp_UnknownValueRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allBeadTerminalTransitionOps {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 64, -1).Draw(rt, "s")
		if known[s] {
			rt.Skip("known constant")
		}
		if BeadTerminalTransitionOp(s).Valid() {
			rt.Errorf("Valid() == true for unknown BeadTerminalTransitionOp %q", s)
		}
	})
}

func TestProp_BeadTerminalTransitionOp_KnownConstantsAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		v := rapid.SampledFrom(allBeadTerminalTransitionOps).Draw(rt, "op")
		if !v.Valid() {
			rt.Errorf("Valid() == false for declared BeadTerminalTransitionOp %q", v)
		}
	})
}

// ============================================================
// BeadTerminalTransitionRecoveredPayload
// ============================================================

func TestProp_BeadTerminalTransitionRecoveredPayload_AllValidAccepted(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BeadTerminalTransitionRecoveredPayload{
			BeadID:         BeadID(drawNonEmptyString(rt, "bead_id")),
			Op:             rapid.SampledFrom(allBeadTerminalTransitionOps).Draw(rt, "op"),
			IdempotencyKey: drawNonEmptyString(rt, "idempotency_key"),
			RecoveredAt:    drawNonEmptyString(rt, "recovered_at"),
		}
		if !p.Valid() {
			rt.Error("Valid() == false for well-formed BeadTerminalTransitionRecoveredPayload")
		}
	})
}

func TestProp_BeadTerminalTransitionRecoveredPayload_EmptyBeadIDRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BeadTerminalTransitionRecoveredPayload{
			BeadID:         BeadID(""),
			Op:             rapid.SampledFrom(allBeadTerminalTransitionOps).Draw(rt, "op"),
			IdempotencyKey: drawNonEmptyString(rt, "idempotency_key"),
			RecoveredAt:    drawNonEmptyString(rt, "recovered_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty BeadID")
		}
	})
}

func TestProp_BeadTerminalTransitionRecoveredPayload_InvalidOpRejected(t *testing.T) {
	known := make(map[string]bool)
	for _, v := range allBeadTerminalTransitionOps {
		known[string(v)] = true
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.StringN(1, 32, -1).Draw(rt, "op_str")
		if known[s] {
			rt.Skip("known constant")
		}
		p := BeadTerminalTransitionRecoveredPayload{
			BeadID:         BeadID(drawNonEmptyString(rt, "bead_id")),
			Op:             BeadTerminalTransitionOp(s),
			IdempotencyKey: drawNonEmptyString(rt, "idempotency_key"),
			RecoveredAt:    drawNonEmptyString(rt, "recovered_at"),
		}
		if p.Valid() {
			rt.Errorf("Valid() should be false for unknown op %q", s)
		}
	})
}

func TestProp_BeadTerminalTransitionRecoveredPayload_EmptyIdempotencyKeyRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BeadTerminalTransitionRecoveredPayload{
			BeadID:         BeadID(drawNonEmptyString(rt, "bead_id")),
			Op:             rapid.SampledFrom(allBeadTerminalTransitionOps).Draw(rt, "op"),
			IdempotencyKey: "",
			RecoveredAt:    drawNonEmptyString(rt, "recovered_at"),
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty IdempotencyKey")
		}
	})
}

func TestProp_BeadTerminalTransitionRecoveredPayload_EmptyRecoveredAtRejected(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		p := BeadTerminalTransitionRecoveredPayload{
			BeadID:         BeadID(drawNonEmptyString(rt, "bead_id")),
			Op:             rapid.SampledFrom(allBeadTerminalTransitionOps).Draw(rt, "op"),
			IdempotencyKey: drawNonEmptyString(rt, "idempotency_key"),
			RecoveredAt:    "",
		}
		if p.Valid() {
			rt.Error("Valid() should be false with empty RecoveredAt")
		}
	})
}
