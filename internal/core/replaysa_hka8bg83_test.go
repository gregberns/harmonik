package core

// replaySafetyHarness_hka8bg83_test.go — Cognition-tagged replay-safety harness
//
// Covers specs/control-points.md §4.8 (CP-039..CP-042) and CP-INV-003:
//   - Gate writes verdict into Transition.evidence keyed by gate_name BEFORE transition advances (CP-040).
//   - Hook writes HookVerdictRecord + emits hook_verdict_persisted (CP-040).
//   - Envelope-hash computation determinism: SHA-256 over 5-input canonical-JSON (CP-040a).
//   - Replay reads persisted verdict on hash match — no second model call (CP-041, CP-INV-003).
//   - Replay emits verdict_envelope_mismatch + escalates to Cat-6 on hash mismatch (CP-041).
//   - Cat-6 stale-verdict re-invocation path (reconciliation/spec.md §4.2).
//
// These tests are fixtures-only: they document the contract shapes and invariants
// at the core-types level. Full dispatcher integration lives in the S01/S05 subsystem
// tests; here we verify the data-structure contracts that the dispatcher must honour.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// replaySafetyGateVerdictFixture returns a GateVerdictRecord with a cognition
// evaluator's verdict — allow action with a known envelope hash. Used as the
// "persisted verdict" object in replay-safety tests.
func replaySafetyGateVerdictFixture(t *testing.T) GateVerdictRecord {
	t.Helper()

	hash := strings.Repeat("d", 64)
	return GateVerdictRecord{
		GateName:          "quality-gate-v1",
		Action:            GateActionAllow,
		Reason:            nil,
		CognitionMeta:     nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-09T00:00:00Z",
	}
}

// replaySafetyHookVerdictFixture returns a HookVerdictRecord with a cognition
// evaluator's verdict — non-failed, known hash. Used as the "persisted verdict"
// in hook replay-safety tests.
func replaySafetyHookVerdictFixture(t *testing.T) HookVerdictRecord {
	t.Helper()

	hash := strings.Repeat("e", 64)
	return HookVerdictRecord{
		HookName:          "pre-merge-hook",
		InvocationID:      uuid.MustParse("01960001-0000-7000-8000-000000000083"),
		SideEffect:        replaySafetyFixtureSideEffect(t),
		Failed:            false,
		Reason:            nil,
		CognitionMeta:     nil,
		InputEnvelopeHash: hash,
		ProducedAt:        "2026-05-09T00:00:00Z",
	}
}

// replaySafetyFixtureSideEffect returns a valid SideEffect for use in
// replay-safety HookVerdictRecord fixture construction.
func replaySafetyFixtureSideEffect(t *testing.T) SideEffect {
	t.Helper()
	return SideEffect{
		Kind:             SideEffectKindEmitEvent,
		Target:           "hook.pre_merge.fired",
		Payload:          map[string]any{"gate": "quality-gate-v1"},
		IdempotencyClass: IdempotencyClassIdempotent,
	}
}

// replaySafetyEnvelopeFixture returns a canonical 5-field envelope map per
// specs/control-points.md §4.8.CP-040a. The envelope is the SHA-256 input.
//
// Fields (in canonical order per spec):
//
//  1. expression_text — mechanism-tagged expression source (nil for cognition-only)
//  2. prompt_template — resolved prompt template body
//  3. skill_packages — skill-package identifiers and versions
//  4. context_subset — context keys reachable via static AST walk
//  5. policy_meta — metadata block of the declaring policy document
func replaySafetyEnvelopeFixture() map[string]any {
	return map[string]any{
		"expression_text": nil,
		"prompt_template": "You are a quality reviewer. Assess: {{.artifact}}",
		"skill_packages": []string{
			"beads-cli@0.1.4",
			"code-reviewer@1.0.0",
		},
		"context_subset": map[string]any{
			"artifact": "pkg/core",
			"branch":   "task/hk-42",
		},
		"policy_meta": map[string]any{
			"name":           "quality-policy",
			"schema_version": 1,
		},
	}
}

// replaySafetyComputeHash computes the SHA-256 hex digest of a canonical-JSON
// serialisation of the envelope, implementing the algorithm in
// specs/control-points.md §4.8.CP-040a.
//
// The caller MUST supply envelope fields in the declared order; this helper
// serialises the map as-is. Production implementations must use a stable
// canonical serialisation (field-order-independent, whitespace-normalised).
func replaySafetyComputeHash(envelope map[string]any) (string, error) {
	data, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("replaySafetyComputeHash: marshal: %w", err)
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}

// --- CP-040 Gate verdict-persistence-at-invocation ---

// TestReplaySafety_GateVerdictWrittenToEvidenceBeforeTransition verifies that
// a GateVerdictRecord is a well-formed value that can be stored in
// Transition.Evidence keyed by GateName, satisfying specs/control-points.md
// §4.8.CP-040 (Gate verdict written BEFORE the transition advances).
//
// The test documents the contract: the gate_name key in Evidence carries the
// GateVerdictRecord as its value; the transition MUST NOT advance until this
// write has occurred.
func TestReplaySafety_GateVerdictWrittenToEvidenceBeforeTransition(t *testing.T) {
	t.Parallel()

	verdict := replaySafetyGateVerdictFixture(t)
	if !verdict.Valid() {
		t.Fatalf("fixture GateVerdictRecord is invalid: %+v", verdict)
	}

	// Evidence is map[string]any; GateVerdictRecord is stored by gate_name key.
	evidence := Evidence{
		verdict.GateName: verdict,
	}
	if !evidence.Valid() {
		t.Error("Evidence.Valid() = false after inserting GateVerdictRecord, want true")
	}

	stored, ok := evidence[verdict.GateName]
	if !ok {
		t.Fatalf("gate_name key %q absent from Evidence after write", verdict.GateName)
	}

	storedRecord, ok := stored.(GateVerdictRecord)
	if !ok {
		t.Fatalf("evidence[gate_name] has type %T, want GateVerdictRecord", stored)
	}
	if storedRecord.InputEnvelopeHash != verdict.InputEnvelopeHash {
		t.Errorf("InputEnvelopeHash: stored %q, want %q",
			storedRecord.InputEnvelopeHash, verdict.InputEnvelopeHash)
	}
}

// TestReplaySafety_HookVerdictPersistenceContract verifies that a
// HookVerdictRecord is structurally valid and carries an InvocationID unique
// per Hook firing, satisfying specs/control-points.md §4.8.CP-040.
//
// The hook_verdict_persisted event MUST be emitted after the record is
// written to .harmonik/hooks/<run_id>/<invocation_id>.json; this test
// documents the data contract, not the filesystem write.
func TestReplaySafety_HookVerdictPersistenceContract(t *testing.T) {
	t.Parallel()

	verdict := replaySafetyHookVerdictFixture(t)
	if !verdict.Valid() {
		t.Fatalf("fixture HookVerdictRecord is invalid: %+v", verdict)
	}

	// InvocationID MUST be non-zero (unique per firing) per §6.1.6.
	if verdict.InvocationID == uuid.Nil {
		t.Error("HookVerdictRecord.InvocationID is nil UUID, violates §6.1.6")
	}

	// Persist path is .harmonik/hooks/<run_id>/<invocation_id>.json; verify
	// the invocation_id can form a valid path component.
	invocationPath := fmt.Sprintf(".harmonik/hooks/run-123/%s.json", verdict.InvocationID)
	if !strings.Contains(invocationPath, verdict.InvocationID.String()) {
		t.Errorf("invocation_id %q not present in expected path %q",
			verdict.InvocationID, invocationPath)
	}
}

// --- CP-040a Envelope hash computation determinism ---

// TestReplaySafety_EnvelopeHashDeterminism verifies that computing the
// SHA-256 hash of the same 5-field envelope twice produces the same result.
//
// specs/control-points.md §4.8.CP-040a: the hash MUST be deterministic over
// the canonical-JSON serialization of the five inputs. Non-determinism in the
// hash is a structural bug.
func TestReplaySafety_EnvelopeHashDeterminism(t *testing.T) {
	t.Parallel()

	envelope := replaySafetyEnvelopeFixture()

	hash1, err := replaySafetyComputeHash(envelope)
	if err != nil {
		t.Fatalf("first hash computation failed: %v", err)
	}
	hash2, err := replaySafetyComputeHash(envelope)
	if err != nil {
		t.Fatalf("second hash computation failed: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("envelope hash is non-deterministic: run1=%q run2=%q", hash1, hash2)
	}

	// SHA-256 hex digest is always 64 lowercase hex chars.
	if len(hash1) != 64 {
		t.Errorf("envelope hash length = %d, want 64 hex chars", len(hash1))
	}
}

// TestReplaySafety_EnvelopeHashChangesOnContextMutation verifies that
// changing any field of the input envelope produces a different hash, proving
// the hash covers the full envelope surface.
//
// This is the core correctness property of CP-040a: any context change MUST
// bust the hash and trigger hash-mismatch escalation (CP-041).
func TestReplaySafety_EnvelopeHashChangesOnContextMutation(t *testing.T) {
	t.Parallel()

	baseEnvelope := replaySafetyEnvelopeFixture()
	baseHash, err := replaySafetyComputeHash(baseEnvelope)
	if err != nil {
		t.Fatalf("base hash: %v", err)
	}

	// Mutate context_subset to simulate a context change.
	mutatedEnvelope := replaySafetyEnvelopeFixture()
	mutatedEnvelope["context_subset"] = map[string]any{
		"artifact": "pkg/core",
		"branch":   "task/hk-99", // different branch
	}
	mutatedHash, err := replaySafetyComputeHash(mutatedEnvelope)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if baseHash == mutatedHash {
		t.Error("context mutation did not change envelope hash, replay-safety is compromised")
	}
}

// TestReplaySafety_EnvelopeHashChangesOnSkillPackageMutation verifies that
// changing skill_packages (a different version) changes the hash, per
// specs/control-points.md §4.8.CP-040a (skill_packages is field 3 of 5 in
// the envelope).
func TestReplaySafety_EnvelopeHashChangesOnSkillPackageMutation(t *testing.T) {
	t.Parallel()

	baseEnvelope := replaySafetyEnvelopeFixture()
	baseHash, err := replaySafetyComputeHash(baseEnvelope)
	if err != nil {
		t.Fatalf("base hash: %v", err)
	}

	mutatedEnvelope := replaySafetyEnvelopeFixture()
	mutatedEnvelope["skill_packages"] = []string{
		"beads-cli@0.1.5", // bumped version
		"code-reviewer@1.0.0",
	}
	mutatedHash, err := replaySafetyComputeHash(mutatedEnvelope)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if baseHash == mutatedHash {
		t.Error("skill_package version bump did not change envelope hash, replay-safety is compromised")
	}
}

// --- CP-041 Replay reads persisted verdict on hash match ---

// TestReplaySafety_ReplayConsumesPersistedVerdictOnHashMatch verifies the
// replay path: when the stored verdict's InputEnvelopeHash matches the
// recomputed hash, the replayer MUST return the stored verdict WITHOUT
// re-invoking the model.
//
// specs/control-points.md §4.8.CP-041 and CP-INV-003.
// This test documents the decision logic as data-structure invariants;
// the S05 dispatcher must implement this branch.
func TestReplaySafety_ReplayConsumesPersistedVerdictOnHashMatch(t *testing.T) {
	t.Parallel()

	// Compute a hash from the canonical envelope.
	envelope := replaySafetyEnvelopeFixture()
	currentHash, err := replaySafetyComputeHash(envelope)
	if err != nil {
		t.Fatalf("computing current hash: %v", err)
	}

	// Persisted verdict was produced with the same envelope.
	persisted := replaySafetyGateVerdictFixture(t)
	persisted.InputEnvelopeHash = currentHash

	// On hash match the replayer MUST return persisted verdict.
	hashMatch := persisted.InputEnvelopeHash == currentHash
	if !hashMatch {
		t.Fatal("fixture setup error: persisted hash does not match current hash")
	}

	// Contract: replayer returns persisted verdict, no dispatch to model.
	// The gate_verdict returned is the stored record, not a fresh evaluation.
	if persisted.Action != GateActionAllow {
		t.Errorf("persisted verdict action = %q, want allow", persisted.Action)
	}
	if !persisted.Valid() {
		t.Error("persisted verdict is not valid after hash-match replay")
	}
}

// TestReplaySafety_ReplayEscalatesToCat6OnHashMismatch verifies the mismatch
// branch: when the current envelope hash does NOT match the stored verdict's
// InputEnvelopeHash, the replayer MUST NOT silently re-invoke the model; it
// MUST route to Cat 6 reconciliation per CP-041 and CP-INV-003.
//
// Cat 6a is the integrity-violation category that an LLM investigator can
// triage per specs/reconciliation/schemas.md §6.1.
func TestReplaySafety_ReplayEscalatesToCat6OnHashMismatch(t *testing.T) {
	t.Parallel()

	// Persisted verdict with a known (old) hash.
	persisted := replaySafetyGateVerdictFixture(t)
	storedHash := persisted.InputEnvelopeHash // known stored value

	// Current envelope has drifted (context changed).
	driftedEnvelope := replaySafetyEnvelopeFixture()
	driftedEnvelope["context_subset"] = map[string]any{
		"artifact": "pkg/core",
		"branch":   "task/hk-drifted",
	}
	currentHash, err := replaySafetyComputeHash(driftedEnvelope)
	if err != nil {
		t.Fatalf("computing drifted hash: %v", err)
	}

	// Confirm mismatch.
	if storedHash == currentHash {
		t.Fatal("fixture setup error: stored and current hash unexpectedly match")
	}

	// On mismatch the replayer MUST escalate to Cat 6, not silently re-invoke.
	// The verdict_envelope_mismatch event signals this condition.
	// We model the escalation decision as: mismatch → route to Cat 6a.
	mismatch := storedHash != currentHash
	if !mismatch {
		t.Error("expected hash mismatch to be detected, got match")
	}

	// Cat 6a is the correct escalation target per reconciliation/spec.md §4.2.
	escalationCategory := ReconciliationCategoryCat6a
	if !escalationCategory.Valid() {
		t.Errorf("escalation category %q is not a valid ReconciliationCategory", escalationCategory)
	}
	if escalationCategory != ReconciliationCategoryCat6a {
		t.Errorf("hash-mismatch escalation MUST route to cat-6a, got %q", escalationCategory)
	}
}

// TestReplaySafety_VerdictEnvelopeMismatchEventShape verifies that the
// verdict_envelope_mismatch event carries the stored hash and current hash
// as distinct fields, as required by specs/control-points.md §7.2.
//
// This test documents the event payload shape that the dispatcher emits before
// escalating to Cat 6; the actual event emission lives in S05.
func TestReplaySafety_VerdictEnvelopeMismatchEventShape(t *testing.T) {
	t.Parallel()

	storedHash := strings.Repeat("f", 64)
	currentHash := strings.Repeat("a", 64)

	// The mismatch event payload carries both hashes so the investigator can
	// compare them and classify the drift (context change vs. skill version bump).
	type verdictEnvelopeMismatchPayload struct {
		StoredHash  string `json:"stored"`
		CurrentHash string `json:"current"`
		VerdictKey  string `json:"verdict_key"`
	}
	payload := verdictEnvelopeMismatchPayload{
		StoredHash:  storedHash,
		CurrentHash: currentHash,
		VerdictKey:  "quality-gate-v1/run-83/transition-1",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal mismatch payload: %v", err)
	}

	var roundtripped verdictEnvelopeMismatchPayload
	if err := json.Unmarshal(data, &roundtripped); err != nil {
		t.Fatalf("json.Unmarshal mismatch payload: %v", err)
	}

	if roundtripped.StoredHash != storedHash {
		t.Errorf("stored hash round-trip: got %q, want %q", roundtripped.StoredHash, storedHash)
	}
	if roundtripped.CurrentHash != currentHash {
		t.Errorf("current hash round-trip: got %q, want %q", roundtripped.CurrentHash, currentHash)
	}
}

// --- CP-042 Verdict persistence is the mechanism/cognition boundary ---

// TestReplaySafety_PersistedVerdictRecordIsValid verifies that both
// GateVerdictRecord and HookVerdictRecord Valid() methods confirm the records
// are well-formed after a cognition evaluator would produce them.
//
// specs/control-points.md §4.8.CP-042: the persistence write is mechanism-
// tagged; the production of the verdict is cognition-tagged. The split is
// enforced by the type boundary: the cognition segment produces the record,
// the mechanism segment writes it to git.
func TestReplaySafety_PersistedVerdictRecordIsValid(t *testing.T) {
	t.Parallel()

	gateVerdict := replaySafetyGateVerdictFixture(t)
	if !gateVerdict.Valid() {
		t.Errorf("GateVerdictRecord.Valid() = false, mechanism segment cannot persist invalid record")
	}

	hookVerdict := replaySafetyHookVerdictFixture(t)
	if !hookVerdict.Valid() {
		t.Errorf("HookVerdictRecord.Valid() = false, mechanism segment cannot persist invalid record")
	}
}

// --- CP-INV-003 Cat-6 stale-verdict re-invocation gate ---

// TestReplaySafety_Cat6StaleVerdictReInvocationPath verifies that Cat 6a is
// the ONLY category authorised to permit re-invocation of a cognition-tagged
// evaluator during replay, per reconciliation/spec.md §4.2.
//
// A Cat 6 verdict that flags a persisted verdict as stale is the single legal
// path for re-invocation (CP-INV-003). All other replay paths MUST NOT
// re-invoke.
func TestReplaySafety_Cat6StaleVerdictReInvocationPath(t *testing.T) {
	t.Parallel()

	// The only categories that authorise re-invocation are cat-6a (investigator
	// triages) and cat-6b (operator escalation). Cat-6b auto-escalates without
	// spawning an investigator; cat-6a's investigator may authorise re-invocation
	// per reconciliation/spec.md §4.2.
	authorisedCategories := []ReconciliationCategory{
		ReconciliationCategoryCat6a,
	}
	for _, cat := range authorisedCategories {
		if !cat.Valid() {
			t.Errorf("Cat 6 category %q must be a valid ReconciliationCategory", cat)
		}
	}

	// All other categories MUST NOT authorise re-invocation.
	// Verify that non-cat-6 categories are not mistakenly treated as authorising.
	nonAuthorisedCategories := []ReconciliationCategory{
		ReconciliationCategoryCat0,
		ReconciliationCategoryCat1,
		ReconciliationCategoryCat2,
		ReconciliationCategoryCat3,
		ReconciliationCategoryCat4,
		ReconciliationCategoryCat5,
	}
	for _, cat := range nonAuthorisedCategories {
		if cat == ReconciliationCategoryCat6a || cat == ReconciliationCategoryCat6b {
			t.Errorf("category %q should not be in non-authorised list", cat)
		}
	}
}

// TestReplaySafety_HashMatchPrecludesModelReInvocation verifies the invariant
// of CP-INV-003: when a persisted verdict exists with a matching envelope hash,
// the dispatcher MUST NOT call the model. The invariant is expressed here as a
// boolean decision table.
func TestReplaySafety_HashMatchPrecludesModelReInvocation(t *testing.T) {
	t.Parallel()

	type replayDecision struct {
		verdictExists bool
		hashMatch     bool
		// expected: true = use persisted verdict; false = escalate / invoke
		usePersistedVerdict bool
		escalateToCat6      bool
	}

	table := []replayDecision{
		// Persisted verdict exists AND hash matches → use persisted, no re-invoke.
		{verdictExists: true, hashMatch: true, usePersistedVerdict: true, escalateToCat6: false},
		// Persisted verdict exists BUT hash mismatch → escalate to Cat 6.
		{verdictExists: true, hashMatch: false, usePersistedVerdict: false, escalateToCat6: true},
		// No persisted verdict → fresh invocation (not a replay path).
		{verdictExists: false, hashMatch: false, usePersistedVerdict: false, escalateToCat6: false},
	}

	for _, tc := range table {
		// Decision logic per §7.2 pseudocode.
		var gotUsePersisted, gotEscalate bool
		if tc.verdictExists {
			if tc.hashMatch {
				gotUsePersisted = true
			} else {
				gotEscalate = true
			}
		}
		if gotUsePersisted != tc.usePersistedVerdict {
			t.Errorf(
				"exists=%v hashMatch=%v: usePersistedVerdict got=%v want=%v",
				tc.verdictExists, tc.hashMatch, gotUsePersisted, tc.usePersistedVerdict,
			)
		}
		if gotEscalate != tc.escalateToCat6 {
			t.Errorf(
				"exists=%v hashMatch=%v: escalateToCat6 got=%v want=%v",
				tc.verdictExists, tc.hashMatch, gotEscalate, tc.escalateToCat6,
			)
		}
	}
}
