package core

// inputenvelope_cp040a_hka8bg42_test.go — conformance suite for CP-040a.
//
// Covers specs/control-points.md §4.8.CP-040a:
//
//	"Every persisted cognition-tagged verdict record MUST include an
//	input_envelope_hash field computed deterministically over the evaluator's
//	resolved input envelope."
//
// # Coverage
//
//  1. ComputeInputEnvelopeHash is deterministic: same inputs → same hash.
//  2. Hash is a 64-character lowercase hex string (SHA-256).
//  3. Changing expression_text changes the hash (item 1 coverage).
//  4. Changing prompt_template changes the hash (item 2 coverage).
//  5. Changing skill_packages (bumping version) changes the hash (item 3).
//  6. SkillPackages insertion order does not affect the hash.
//  7. Changing context_subset changes the hash (item 4 coverage).
//  8. Changing policy_meta changes the hash (item 5 coverage).
//  9. Nil expression_text (pure-cognition evaluator) yields a valid hash.
// 10. ContextSubsetMode constants are valid.
// 11. Changing ContextSubsetMode changes the hash (mode is covered in hash surface).
// 12. Invalid ContextSubsetMode returns an error (enforces MUST-declare rule).
// 13. Computed hash satisfies GateVerdictRecord.Valid() when co-stored.
// 14. Computed hash satisfies HookVerdictRecord.Valid() when co-stored.
//
// Tags: mechanism
// Refs: hk-a8bg.42

import (
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

func cp040aBaseEnvelope() InputEnvelope {
	expr := "outcome.status == \"SUCCESS\""
	return InputEnvelope{
		ExpressionText: &expr,
		PromptTemplate: "You are a quality reviewer. Assess: {{.artifact}}",
		SkillPackages: []string{
			"beads-cli@0.1.4",
			"code-reviewer@1.0.0",
		},
		ContextSubset: map[string]any{
			"artifact": "pkg/core",
			"branch":   "task/hk-42",
		},
		PolicyMeta: map[string]any{
			"name":           "quality-policy",
			"schema_version": 1,
		},
		ContextSubsetMode: ContextSubsetModeConservative,
	}
}

// ---------------------------------------------------------------------------
// (1) Determinism: same envelope → same hash.
// ---------------------------------------------------------------------------

// TestCP040a_Determinism verifies that ComputeInputEnvelopeHash returns the
// same 64-char hex digest when called twice with identical inputs.
func TestCP040a_Determinism(t *testing.T) {
	t.Parallel()

	e := cp040aBaseEnvelope()
	h1, err := ComputeInputEnvelopeHash(e)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	h2, err := ComputeInputEnvelopeHash(e)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if h1 != h2 {
		t.Errorf("non-deterministic: h1=%q h2=%q", h1, h2)
	}
}

// ---------------------------------------------------------------------------
// (2) Hash shape: 64-char lowercase hex.
// ---------------------------------------------------------------------------

// TestCP040a_HashShape verifies the output is a 64-character lowercase hex
// string (SHA-256 hex digest encoding).
func TestCP040a_HashShape(t *testing.T) {
	t.Parallel()

	h, err := ComputeInputEnvelopeHash(cp040aBaseEnvelope())
	if err != nil {
		t.Fatalf("ComputeInputEnvelopeHash: %v", err)
	}
	if len(h) != 64 {
		t.Errorf("hash length = %d, want 64", len(h))
	}
	for _, c := range h {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("hash %q contains non-lowercase-hex character %q", h, c)
			break
		}
	}
}

// ---------------------------------------------------------------------------
// (3) expression_text mutation changes the hash (item 1).
// ---------------------------------------------------------------------------

// TestCP040a_ExpressionTextMutationChangesHash verifies that changing
// expression_text (item 1 of 5) produces a different hash.
func TestCP040a_ExpressionTextMutationChangesHash(t *testing.T) {
	t.Parallel()

	base := cp040aBaseEnvelope()
	baseHash, err := ComputeInputEnvelopeHash(base)
	if err != nil {
		t.Fatalf("base hash: %v", err)
	}

	mutated := cp040aBaseEnvelope()
	newExpr := "outcome.status == \"FAILURE\""
	mutated.ExpressionText = &newExpr
	mutatedHash, err := ComputeInputEnvelopeHash(mutated)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if baseHash == mutatedHash {
		t.Error("expression_text change did not change the hash — item 1 not covered")
	}
}

// ---------------------------------------------------------------------------
// (4) prompt_template mutation changes the hash (item 2).
// ---------------------------------------------------------------------------

// TestCP040a_PromptTemplateMutationChangesHash verifies that changing
// prompt_template (item 2 of 5) produces a different hash.
func TestCP040a_PromptTemplateMutationChangesHash(t *testing.T) {
	t.Parallel()

	base := cp040aBaseEnvelope()
	baseHash, err := ComputeInputEnvelopeHash(base)
	if err != nil {
		t.Fatalf("base hash: %v", err)
	}

	mutated := cp040aBaseEnvelope()
	mutated.PromptTemplate = "You are a STRICT quality reviewer. Assess: {{.artifact}}"
	mutatedHash, err := ComputeInputEnvelopeHash(mutated)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if baseHash == mutatedHash {
		t.Error("prompt_template change did not change the hash — item 2 not covered")
	}
}

// ---------------------------------------------------------------------------
// (5) skill_packages mutation changes the hash (item 3).
// ---------------------------------------------------------------------------

// TestCP040a_SkillPackagesMutationChangesHash verifies that bumping a skill
// version in skill_packages (item 3 of 5) produces a different hash.
func TestCP040a_SkillPackagesMutationChangesHash(t *testing.T) {
	t.Parallel()

	base := cp040aBaseEnvelope()
	baseHash, err := ComputeInputEnvelopeHash(base)
	if err != nil {
		t.Fatalf("base hash: %v", err)
	}

	mutated := cp040aBaseEnvelope()
	mutated.SkillPackages = []string{
		"beads-cli@0.1.5", // bumped version
		"code-reviewer@1.0.0",
	}
	mutatedHash, err := ComputeInputEnvelopeHash(mutated)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if baseHash == mutatedHash {
		t.Error("skill_packages version bump did not change the hash — item 3 not covered")
	}
}

// ---------------------------------------------------------------------------
// (6) SkillPackages insertion order does not affect the hash.
// ---------------------------------------------------------------------------

// TestCP040a_SkillPackagesOrderIndependence verifies that two InputEnvelope
// values differing only in skill_packages insertion order produce the same hash.
// ComputeInputEnvelopeHash sorts the slice internally for determinism.
func TestCP040a_SkillPackagesOrderIndependence(t *testing.T) {
	t.Parallel()

	e1 := cp040aBaseEnvelope()
	e1.SkillPackages = []string{"beads-cli@0.1.4", "code-reviewer@1.0.0"}
	h1, err := ComputeInputEnvelopeHash(e1)
	if err != nil {
		t.Fatalf("e1 hash: %v", err)
	}

	e2 := cp040aBaseEnvelope()
	e2.SkillPackages = []string{"code-reviewer@1.0.0", "beads-cli@0.1.4"}
	h2, err := ComputeInputEnvelopeHash(e2)
	if err != nil {
		t.Fatalf("e2 hash: %v", err)
	}

	if h1 != h2 {
		t.Errorf("skill_packages insertion order changed the hash (h1=%q h2=%q)", h1, h2)
	}
}

// ---------------------------------------------------------------------------
// (7) context_subset mutation changes the hash (item 4).
// ---------------------------------------------------------------------------

// TestCP040a_ContextSubsetMutationChangesHash verifies that changing a value in
// context_subset (item 4 of 5) produces a different hash. This is the core
// replay-safety property: any context change MUST bust the hash per CP-041.
func TestCP040a_ContextSubsetMutationChangesHash(t *testing.T) {
	t.Parallel()

	base := cp040aBaseEnvelope()
	baseHash, err := ComputeInputEnvelopeHash(base)
	if err != nil {
		t.Fatalf("base hash: %v", err)
	}

	mutated := cp040aBaseEnvelope()
	mutated.ContextSubset = map[string]any{
		"artifact": "pkg/core",
		"branch":   "task/hk-drifted", // changed
	}
	mutatedHash, err := ComputeInputEnvelopeHash(mutated)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if baseHash == mutatedHash {
		t.Error("context_subset change did not change hash — replay-safety compromised (item 4 not covered)")
	}
}

// ---------------------------------------------------------------------------
// (8) policy_meta mutation changes the hash (item 5).
// ---------------------------------------------------------------------------

// TestCP040a_PolicyMetaMutationChangesHash verifies that changing policy_meta
// (item 5 of 5) produces a different hash.
func TestCP040a_PolicyMetaMutationChangesHash(t *testing.T) {
	t.Parallel()

	base := cp040aBaseEnvelope()
	baseHash, err := ComputeInputEnvelopeHash(base)
	if err != nil {
		t.Fatalf("base hash: %v", err)
	}

	mutated := cp040aBaseEnvelope()
	mutated.PolicyMeta = map[string]any{
		"name":           "quality-policy",
		"schema_version": 2, // bumped
	}
	mutatedHash, err := ComputeInputEnvelopeHash(mutated)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if baseHash == mutatedHash {
		t.Error("policy_meta change did not change the hash — item 5 not covered")
	}
}

// ---------------------------------------------------------------------------
// (9) Nil expression_text (pure-cognition evaluator) yields a valid hash.
// ---------------------------------------------------------------------------

// TestCP040a_NilExpressionTextIsValid verifies that a pure-cognition evaluator
// (nil ExpressionText) produces a valid 64-char hash that differs from the
// non-nil case, confirming both states are distinct in the hash surface.
func TestCP040a_NilExpressionTextIsValid(t *testing.T) {
	t.Parallel()

	eNil := cp040aBaseEnvelope()
	eNil.ExpressionText = nil

	hNil, err := ComputeInputEnvelopeHash(eNil)
	if err != nil {
		t.Fatalf("nil ExpressionText: %v", err)
	}
	if len(hNil) != 64 {
		t.Errorf("hash length = %d, want 64", len(hNil))
	}

	eNonNil := cp040aBaseEnvelope()
	hNonNil, err := ComputeInputEnvelopeHash(eNonNil)
	if err != nil {
		t.Fatalf("non-nil ExpressionText: %v", err)
	}
	if hNil == hNonNil {
		t.Error("nil vs non-nil expression_text produced the same hash — expression_text not covered")
	}
}

// ---------------------------------------------------------------------------
// (10) ContextSubsetMode constants.
// ---------------------------------------------------------------------------

// TestCP040a_ContextSubsetModeConstants verifies that both declared
// ContextSubsetMode constants pass Valid() and are distinct.
func TestCP040a_ContextSubsetModeConstants(t *testing.T) {
	t.Parallel()

	if !ContextSubsetModeASTWalk.Valid() {
		t.Error("ContextSubsetModeASTWalk.Valid() = false, want true")
	}
	if !ContextSubsetModeConservative.Valid() {
		t.Error("ContextSubsetModeConservative.Valid() = false, want true")
	}
	if ContextSubsetModeASTWalk == ContextSubsetModeConservative {
		t.Error("ContextSubsetModeASTWalk == ContextSubsetModeConservative; modes must be distinct")
	}

	var unknown ContextSubsetMode = "unknown"
	if unknown.Valid() {
		t.Errorf("unknown ContextSubsetMode %q.Valid() = true, want false", unknown)
	}
}

// ---------------------------------------------------------------------------
// (11) ContextSubsetMode is included in the hash: changing it changes hash.
// ---------------------------------------------------------------------------

// TestCP040a_ContextSubsetModeChangesHash verifies that switching from
// ContextSubsetModeConservative to ContextSubsetModeASTWalk (with otherwise
// identical inputs) produces a different hash. This confirms the mode is
// machine-readable in the stored envelope per CP-040a.
func TestCP040a_ContextSubsetModeChangesHash(t *testing.T) {
	t.Parallel()

	conservative := cp040aBaseEnvelope()
	conservative.ContextSubsetMode = ContextSubsetModeConservative
	hC, err := ComputeInputEnvelopeHash(conservative)
	if err != nil {
		t.Fatalf("conservative hash: %v", err)
	}

	astWalk := cp040aBaseEnvelope()
	astWalk.ContextSubsetMode = ContextSubsetModeASTWalk
	hA, err := ComputeInputEnvelopeHash(astWalk)
	if err != nil {
		t.Fatalf("ast_walk hash: %v", err)
	}

	if hC == hA {
		t.Error("changing ContextSubsetMode did not change the hash — mode not covered in hash surface")
	}
}

// TestCP040a_InvalidContextSubsetModeReturnsError verifies that
// ComputeInputEnvelopeHash returns an error when ContextSubsetMode is not a
// valid value, enforcing the "MUST declare which mode" requirement.
func TestCP040a_InvalidContextSubsetModeReturnsError(t *testing.T) {
	t.Parallel()

	e := cp040aBaseEnvelope()
	e.ContextSubsetMode = "" // zero value — not valid

	_, err := ComputeInputEnvelopeHash(e)
	if err == nil {
		t.Error("expected error for invalid ContextSubsetMode, got nil")
	}
}

// ---------------------------------------------------------------------------
// (13) Hash satisfies GateVerdictRecord.Valid() when co-stored.
// ---------------------------------------------------------------------------

// TestCP040a_HashCoStoredInGateVerdictRecord verifies that a GateVerdictRecord
// built with a ComputeInputEnvelopeHash result is Valid(), confirming the 64-
// char hex output satisfies the InputEnvelopeHash field invariant.
func TestCP040a_HashCoStoredInGateVerdictRecord(t *testing.T) {
	t.Parallel()

	h, err := ComputeInputEnvelopeHash(cp040aBaseEnvelope())
	if err != nil {
		t.Fatalf("ComputeInputEnvelopeHash: %v", err)
	}

	r := GateVerdictRecord{
		GateName:          "quality-gate-v1",
		Action:            GateActionAllow,
		InputEnvelopeHash: h,
		ProducedAt:        "2026-05-31T00:00:00Z",
	}
	if !r.Valid() {
		t.Errorf("GateVerdictRecord with computed hash is not Valid(): %+v", r)
	}
}

// ---------------------------------------------------------------------------
// (14) Hash satisfies HookVerdictRecord.Valid() when co-stored.
// ---------------------------------------------------------------------------

// TestCP040a_HashCoStoredInHookVerdictRecord verifies that a HookVerdictRecord
// built with a ComputeInputEnvelopeHash result is Valid().
func TestCP040a_HashCoStoredInHookVerdictRecord(t *testing.T) {
	t.Parallel()

	h, err := ComputeInputEnvelopeHash(cp040aBaseEnvelope())
	if err != nil {
		t.Fatalf("ComputeInputEnvelopeHash: %v", err)
	}

	invID := uuid.MustParse("019e7309-1648-7412-9e67-00000000aa42")
	se := SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "hook.pre_merge.fired",
		Payload:          map[string]any{"run": "test"},
		IdempotencyClass: IdempotencyClassIdempotent,
	}
	r := HookVerdictRecord{
		HookName:          "pre-merge-hook",
		InvocationID:      invID,
		SideEffect:        se,
		InputEnvelopeHash: h,
		ProducedAt:        "2026-05-31T00:00:00Z",
	}
	if !r.Valid() {
		t.Errorf("HookVerdictRecord with computed hash is not Valid(): %+v", r)
	}
}
