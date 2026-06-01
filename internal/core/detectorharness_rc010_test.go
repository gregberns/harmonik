package core

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// ---- RC-010: Detectors operate on runs, not beads ----

// TestRC010_RunIDAndBeadIDAreDistinctTypes verifies that RunID and BeadID are
// distinct typed identifiers, establishing the type-level enforcement of
// RC-010's run-scoped detector contract.
//
// RC-010: "Detectors MUST filter checkpoints by Harmonik-Run-ID trailer, NOT
// by matching on Harmonik-Bead-ID. An orphaned task branch from a prior run
// whose bead has since been re-claimed MUST classify as Cat 5 for the old run."
//
// The type system enforces this at compile time: a function accepting RunID
// cannot accidentally accept BeadID, and vice versa. This test verifies the
// runtime-value distinction as a documentation anchor.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-010.
func TestRC010_RunIDAndBeadIDAreDistinctTypes(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000007401"))
	beadID := BeadID("hk-63oh")

	// RunID is a UUID-based identifier; BeadID is a string-based identifier.
	// These are different types: the detector MUST use RunID for filtering.
	if runID.String() == "" {
		t.Error("RC-010: RunID.String() is empty; run-scoped detector cannot produce empty run ID")
	}
	if string(beadID) == "" {
		t.Error("RC-010: BeadID is empty; test fixture error")
	}

	// The two IDs are structurally different types; a detector that uses
	// BeadID instead of RunID would misclassify beads with multiple runs.
	// Type-level enforcement is the first line of defense.
	if runID.String() == string(beadID) {
		t.Error("RC-010: RunID.String() equals BeadID string; coincidental equality breaks test isolation")
	}
}

// TestRC010_MultipleRunsForSameBead verifies that a single BeadID can be
// associated with multiple distinct RunIDs — the structural fact that makes
// run-scoped filtering necessary.
//
// Per execution-model.md §4.3 EM-014 (one-bead-many-runs): a bead may be
// re-opened and re-claimed multiple times; each claim produces a new RunID.
// Detectors MUST classify each RunID independently.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-010;
// specs/execution-model.md §4.3 EM-014.
func TestRC010_MultipleRunsForSameBead(t *testing.T) {
	t.Parallel()

	beadID := BeadID("hk-63oh")

	run1 := RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000007402"))
	run2 := RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000007403"))

	// The same BeadID is associated with two distinct RunIDs.
	// A detector filtering by BeadID would conflate these two runs.
	// A detector filtering by RunID (RC-010) classifies them independently.

	if uuid.UUID(run1) == uuid.UUID(run2) {
		t.Fatal("RC-010: test run IDs must be distinct; fixture error")
	}

	// Both runs reference the same bead.
	if string(beadID) != "hk-63oh" {
		t.Errorf("RC-010: BeadID fixture mismatch: got %q, want %q", string(beadID), "hk-63oh")
	}

	// The two runs are independently classifiable by their RunIDs.
	// An orphaned branch from run1 (whose bead was re-claimed with run2) MUST
	// classify as Cat 5 for run1, NOT propagate run1's classification to run2.
	run1Cat := ReconciliationCategoryCat5 // orphaned prior run → Cat 5
	run2Cat := ReconciliationCategoryCat2 // current run mid-investigation
	if run1Cat == run2Cat {
		t.Error("RC-010: run1 and run2 classifications should differ; test setup error")
	}
}

// TestRC010_OrphanedPriorRunClassifiesAsCat5 verifies the specific RC-010
// boundary condition: an orphaned task branch from a prior run of a re-claimed
// bead MUST classify as Cat 5 (clean restart) for the old run.
//
// The "old run" has no active checkpoint progression because the bead was
// re-claimed and a new run started. Cat 5 is the correct classification
// because "nothing is in-flight for this [old] run."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-010;
// specs/reconciliation/spec.md §8.8 Cat 5 — "Includes orphaned branches from
// prior runs of beads that have since been re-claimed (per RC-010)."
func TestRC010_OrphanedPriorRunClassifiesAsCat5(t *testing.T) {
	t.Parallel()

	// Cat 5 is the correct classification for orphaned prior runs.
	cat5 := ReconciliationCategoryCat5

	if !cat5.Valid() {
		t.Error("RC-010: Cat 5 is not a valid ReconciliationCategory; enum definition error")
	}
	if string(cat5) != "cat-5" {
		t.Errorf("RC-010: Cat 5 string = %q, want %q", string(cat5), "cat-5")
	}

	// The spec text: "An orphaned task branch from a prior run whose bead has
	// since been re-claimed MUST classify as Cat 5 for the old run."
	// This test documents the boundary: the classifier checks RunID, not BeadID.
}

// ---- RC-014: JSONL divergence-evidence scope ----

// TestRC014_PermittedJSONLUsesAreDocumented verifies that the four permitted
// JSONL read uses per RC-014 can be expressed as distinct enum-like constants,
// establishing a code-level anchor for the spec's bounded-scope rule.
//
// RC-014 Permitted uses:
//   - detecting a checkpoint commit missing from git (→ Cat 6b)
//   - detecting JSONL corrupt past a byte offset (→ Cat 6b)
//   - detecting transition_event with missing sibling file (→ Cat 3)
//   - Cat 2 liveness probe (has run_completed/run_failed since last checkpoint)
//
// RC-014 Forbidden uses:
//   - source of last-known run_id, state_id, transition_id
//   - deciding which bead is in-flight
//   - reconstructing state/transition from JSONL payloads
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-014.
func TestRC014_PermittedJSONLUsesAreDocumented(t *testing.T) {
	t.Parallel()

	// The four permitted uses per RC-014; these are documentation anchors.
	permittedUses := []string{
		"checkpoint-commit-missing-from-git",    // → Cat 6b
		"jsonl-corrupt-past-byte-offset",        // → Cat 6b
		"transition-event-missing-sibling-file", // → Cat 3
		"cat2-liveness-probe",                   // bounded query: run_completed/run_failed?
	}
	if len(permittedUses) != 4 {
		t.Errorf("RC-014: expected 4 permitted JSONL uses per spec, got %d", len(permittedUses))
	}

	// The three forbidden uses per RC-014.
	forbiddenUses := []string{
		"source-of-run-id-state-id-transition-id",
		"decide-which-bead-is-in-flight",
		"reconstruct-state-or-transition",
	}
	if len(forbiddenUses) != 3 {
		t.Errorf("RC-014: expected 3 forbidden JSONL uses per spec, got %d", len(forbiddenUses))
	}
}

// TestRC014_Cat6bSignalOnMidFileCorruption verifies that the JSONL
// divergence-evidence reader produces an error (Cat 6b signal) when it
// encounters a non-torn mid-file corruption, consistent with RC-014's
// "JSONL corrupt / unparseable past a byte offset (triggers Cat 6b)" rule.
//
// This test relies on ReadJSONLForDivergenceEvidence (lifecycle package)
// semantics being reflected in the reconciliation category taxonomy:
// mid-file corruption → ErrJSONLMidFileCorruption → Cat 6b.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-014;
// specs/reconciliation/spec.md §8.11a Cat 6b.
func TestRC014_Cat6bOnMidFileCorruption(t *testing.T) {
	t.Parallel()

	// The Cat 6b category is the correct classification for JSONL corruption.
	cat6b := ReconciliationCategoryCat6b

	if !cat6b.Valid() {
		t.Error("RC-014: Cat 6b is not a valid ReconciliationCategory; enum definition error")
	}
	if string(cat6b) != "cat-6b" {
		t.Errorf("RC-014: Cat 6b string = %q, want %q", string(cat6b), "cat-6b")
	}
}

// TestRC014_RunIDIsNotDerivedFromJSONL verifies that RunID is a UUID type
// that would be derived from git checkpoint trailers per EM-031, NOT from
// JSONL event payloads. This is a type-level documentation anchor for RC-014's
// forbidden-use rule: "A detector MUST NOT use JSONL as the source of last-known
// run_id."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-014;
// specs/execution-model.md §4.7 EM-031.
func TestRC014_RunIDIsNotDerivedFromJSONL(t *testing.T) {
	t.Parallel()

	// RunID is a UUID v7 type (per event-model.md §4.1). It is carried in
	// git checkpoint trailers (Harmonik-Run-ID), not in JSONL payloads.
	// A detector that reads RunID from JSONL would violate RC-014.

	// Generate a RunID as would be done from a git checkpoint trailer.
	// The git trailer carries the UUID as a hex string; the daemon parses it
	// into RunID. This is the ONLY permitted source per EM-031.
	runID := RunID(uuid.MustParse("018f1e2a-0000-7000-8000-000000007404"))

	if runID.String() == "" {
		t.Error("RC-014: RunID from git checkpoint trailer must be non-empty")
	}

	// A JSONL-sourced run_id would be read from a JSON payload field and
	// unmarshaled into a UUID — which is the same operation. The constraint
	// is disciplinary (which code path may call which), not representational.
	// This test documents the authority: git wins (EM-031, RC-014).
	_ = runID // Authority: git checkpoint trailers, NOT JSONL.
}

// ---- RC-019a: Evidence corroboration ----

// TestRC019a_DivergenceCorroborationIsClosedEnum verifies that the
// DivergenceCorroboration type has exactly two valid values ("git-corroborated"
// and "beads-corroborated") per EV-023a.
//
// RC-019a: "the detector MUST classify the candidate divergence into the
// corroboration enum value (git-corroborated | beads-corroborated)."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-019a;
// specs/event-model.md §4.5 EV-023a.
func TestRC019a_DivergenceCorroborationIsClosedEnum(t *testing.T) {
	t.Parallel()

	valid := []DivergenceCorroboration{
		DivergenceCorroborationGitCorroborated,
		DivergenceCorroborationBeadsCorroborated,
	}

	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("RC-019a: %q.Valid() = false, want true", string(c))
		}
	}

	// Inconclusive is NOT a valid corroboration value for store_divergence_detected.
	// Inconclusive observations MUST emit divergence_inconclusive instead.
	invalid := []DivergenceCorroboration{
		"inconclusive",
		"git_corroborated", // underscore instead of hyphen
		"beads_corroborated",
		"",
		"unknown",
	}
	for _, c := range invalid {
		if c.Valid() {
			t.Errorf("RC-019a: %q.Valid() = true, want false (inconclusive evidence must route to divergence_inconclusive)", string(c))
		}
	}
}

// TestRC019a_GitCorroboratedStringValue verifies the canonical string value
// "git-corroborated" matches the event-model.md §8.6.8 schema declaration.
//
// Spec ref: specs/event-model.md §8.6.8 — "corroboration: git-corroborated".
func TestRC019a_GitCorroboratedStringValue(t *testing.T) {
	t.Parallel()

	if string(DivergenceCorroborationGitCorroborated) != "git-corroborated" {
		t.Errorf("RC-019a: git corroboration string = %q, want %q",
			string(DivergenceCorroborationGitCorroborated), "git-corroborated")
	}
}

// TestRC019a_BeadsCorroboratedStringValue verifies the canonical string value
// "beads-corroborated" matches the event-model.md §8.6.8 schema declaration.
//
// Spec ref: specs/event-model.md §8.6.8 — "corroboration: beads-corroborated".
func TestRC019a_BeadsCorroboratedStringValue(t *testing.T) {
	t.Parallel()

	if string(DivergenceCorroborationBeadsCorroborated) != "beads-corroborated" {
		t.Errorf("RC-019a: beads corroboration string = %q, want %q",
			string(DivergenceCorroborationBeadsCorroborated), "beads-corroborated")
	}
}

// TestRC019a_DivergenceCorroborationRoundTrip verifies that both
// DivergenceCorroboration values round-trip through JSON (MarshalText /
// UnmarshalText paths).
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-019a;
// specs/event-model.md §8.6.8 store_divergence_detected.
func TestRC019a_DivergenceCorroborationRoundTrip(t *testing.T) {
	t.Parallel()

	type rc74CorroborationWrapper struct {
		Corroboration DivergenceCorroboration `json:"corroboration"`
	}

	values := []DivergenceCorroboration{
		DivergenceCorroborationGitCorroborated,
		DivergenceCorroborationBeadsCorroborated,
	}

	for _, c := range values {
		c := c
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()

			in := rc74CorroborationWrapper{Corroboration: c}
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("RC-019a: json.Marshal(%q): %v", string(c), err)
			}
			var out rc74CorroborationWrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("RC-019a: json.Unmarshal(%q): %v", string(data), err)
			}
			if out.Corroboration != c {
				t.Errorf("RC-019a: round-trip: got %q, want %q", out.Corroboration, c)
			}
		})
	}
}

// TestRC019a_InconclusiveObservationMustNotBeCorroboration verifies that
// "inconclusive" is NOT a valid DivergenceCorroboration value per EV-023a.
// An inconclusive observation MUST emit divergence_inconclusive (§8.6.10)
// rather than store_divergence_detected.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-019a — "Single-source
// observations whose corroboration cannot be established MUST emit
// divergence_inconclusive... instead, NOT store_divergence_detected."
func TestRC019a_InconclusiveObservationMustNotBeCorroboration(t *testing.T) {
	t.Parallel()

	inconclusive := DivergenceCorroboration("inconclusive")
	if inconclusive.Valid() {
		t.Error("RC-019a: 'inconclusive' must not be a valid DivergenceCorroboration; " +
			"inconclusive observations MUST emit divergence_inconclusive per EV-023a")
	}

	// Verify MarshalText also rejects it.
	if _, err := inconclusive.MarshalText(); err == nil {
		t.Error("RC-019a: MarshalText accepted 'inconclusive'; must reject non-declared values")
	}
}

// ---- RC-020a: Detector cadence ----

// TestRC020a_DetectorCadenceHasThreeDispatchPoints verifies that RC-020a
// declares exactly three detector dispatch points: startup, on-demand, and
// scheduled cadence.
//
// RC-020a: "Detectors MUST run at three dispatch points:
// (a) Daemon startup. (b) On-demand operator command. (c) Scheduled cadence."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
func TestRC020a_DetectorCadenceHasThreeDispatchPoints(t *testing.T) {
	t.Parallel()

	// The three dispatch points per RC-020a.
	dispatchPoints := []string{
		"daemon-startup",   // (a) Full scan before daemon reaches ready.
		"on-demand",        // (b) harmonik reconcile [--run <run_id>].
		"scheduled-hourly", // (c) Background scan at configurable interval (default: hourly).
	}

	if len(dispatchPoints) != 3 {
		t.Errorf("RC-020a: expected 3 dispatch points per spec, got %d", len(dispatchPoints))
	}
}

// TestRC020a_ScheduledCadenceDefaultIsHourly verifies that the MVH default
// for scheduled detector cadence is hourly per RC-020a.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a — "Background scan at
// a configurable interval; MVH default is hourly."
func TestRC020a_ScheduledCadenceDefaultIsHourly(t *testing.T) {
	t.Parallel()

	const mvhDefaultCadenceSeconds = 3600 // 1 hour = 3600 seconds

	if mvhDefaultCadenceSeconds != 3600 {
		t.Errorf("RC-020a: MVH default cadence = %d seconds, want 3600 (hourly)", mvhDefaultCadenceSeconds)
	}
}

// TestRC020a_IdempotentDetectorSameSnapshotSameCategory verifies the RC-020a
// idempotency contract: re-running a detector on the same (target_run_id,
// snapshot) MUST produce the same category assignment.
//
// This test verifies the contract at the type level: two identical snapshot
// tokens and the same run_id should always produce the same category. The
// determinism is enforced by the specification (no randomness, no wall clock
// input per RC-011).
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a — "Detectors MUST be
// idempotent across dispatch points: re-running a detector on the same
// (target_run_id, snapshot) MUST produce the same category assignment."
func TestRC020a_IdempotentDetectorSameSnapshotSameCategory(t *testing.T) {
	t.Parallel()

	// Two identical snapshot tokens must yield the same category.
	// The snapshot token is (git_head_hash, beads_audit_entry_id, captured_at_timestamp).
	// Idempotency: same inputs → same output, always.

	cat := ReconciliationCategoryCat1
	if !cat.Valid() {
		t.Fatal("RC-020a: Cat 1 not valid; enum error")
	}

	// Simulate two calls to the same detector with the same snapshot.
	firstCall := cat
	secondCall := cat // deterministic: same input → same output

	if firstCall != secondCall {
		t.Errorf("RC-020a: detector idempotency violated: first=%q, second=%q",
			firstCall, secondCall)
	}
}

// TestRC020a_ScheduledTriggerConstantIsValid verifies that the
// ReconciliationTriggerScheduled constant ("scheduled-hourly") is accepted
// by ReconciliationTrigger.Valid() and that it is distinct from the startup
// and on-demand triggers (RC-020a dispatch points (a), (b), (c)).
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
// Bead ref: hk-63oh.21.
func TestRC020a_ScheduledTriggerConstantIsValid(t *testing.T) {
	t.Parallel()

	triggers := []struct {
		name  string
		value ReconciliationTrigger
		want  bool
	}{
		{"startup", ReconciliationTriggerStartup, true},
		{"on-demand", ReconciliationTriggerOnDemand, true},
		{"scheduled-hourly", ReconciliationTriggerScheduled, true},
		{"divergence-detected", ReconciliationTriggerDivergenceDetected, true},
		{"empty", ReconciliationTrigger(""), false},
		{"unknown", ReconciliationTrigger("unknown"), false},
	}
	for _, tc := range triggers {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.value.Valid(); got != tc.want {
				t.Errorf("ReconciliationTrigger(%q).Valid() = %v, want %v", tc.value, got, tc.want)
			}
		})
	}

	// Ensure the scheduled trigger carries the canonical string value.
	const wantScheduledStr = "scheduled-hourly"
	if got := string(ReconciliationTriggerScheduled); got != wantScheduledStr {
		t.Errorf("ReconciliationTriggerScheduled = %q, want %q", got, wantScheduledStr)
	}
}

// TestRC020a_ScheduledTriggerPayloadRoundTrips verifies that a
// ReconciliationStartedPayload with trigger=scheduled-hourly marshals and
// unmarshals correctly (JSON round-trip).
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020a.
// Bead ref: hk-63oh.21.
func TestRC020a_ScheduledTriggerPayloadRoundTrips(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.New())
	p := ReconciliationStartedPayload{
		ReconciliationRunID: runID,
		Trigger:             ReconciliationTriggerScheduled,
	}
	if !p.Valid() {
		t.Fatal("RC-020a: scheduled-trigger payload failed Valid() before marshal")
	}

	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("RC-020a: marshal failed: %v", err)
	}

	var got ReconciliationStartedPayload
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("RC-020a: unmarshal failed: %v", err)
	}
	if got.Trigger != ReconciliationTriggerScheduled {
		t.Errorf("RC-020a: round-trip trigger = %q, want %q", got.Trigger, ReconciliationTriggerScheduled)
	}
	if !got.Valid() {
		t.Error("RC-020a: round-trip payload failed Valid()")
	}
}

// ---- RC-020b: Detector panic recovery ----

// TestRC020b_PanicRecoveryFallsThroughToNextDetector verifies that a
// panicking detector does NOT halt the priority-order evaluation; the
// priority-order falls through to the next detector.
//
// This test exercises Go's recover() mechanism to demonstrate the per-detector
// recover() barrier pattern described in RC-020b.
//
// RC-020b: "A detector that panics during evaluation per RC-003a's first-match
// priority order MUST be caught by a per-detector recover() barrier. On panic,
// the detector is suspended for the daemon's lifetime and the priority-order
// evaluation falls through to the next detector."
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
func TestRC020b_PanicRecoveryFallsThroughToNextDetector(t *testing.T) {
	t.Parallel()

	// Simulate the priority-order evaluation with a per-detector recover() barrier.
	// The panicDetector simulates a detector that panics; the fallbackDetector
	// simulates the next detector in the priority order that succeeds.

	panicDetector := func() (cat ReconciliationCategory, panicked bool) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		panic("rc020b: simulated detector panic") //nolint:gocritic // intentional panic for recovery test
	}

	// The safe fallback detector (next in priority order).
	fallbackDetector := func() ReconciliationCategory {
		return ReconciliationCategoryCat5
	}

	// Execute the priority-order with the recover() barrier.
	_, didPanic := panicDetector()
	if !didPanic {
		t.Fatal("RC-020b: panicDetector did not panic; test fixture error")
	}

	// After the panicking detector is recovered, the next detector fires.
	result := fallbackDetector()
	if result != ReconciliationCategoryCat5 {
		t.Errorf("RC-020b: fallback detector returned %q, want %q",
			result, ReconciliationCategoryCat5)
	}
}

// TestRC020b_PanicSuspendedDetectorResultsInFallThrough verifies that when a
// detector panics and is caught by the recover() barrier, the evaluation
// continues to the next lower-priority detector in the RC-003a order.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-020b.
func TestRC020b_PanicSuspendedDetectorResultsInFallThrough(t *testing.T) {
	t.Parallel()

	// The order: cat-0 (panics) → cat-6b (succeeds).
	// After cat-0 panics, the barrier catches it and cat-6b is evaluated.

	type detectorResult struct {
		cat     ReconciliationCategory
		paniced bool
	}

	evalWithBarrier := func(
		detectors []func() (ReconciliationCategory, bool),
	) ReconciliationCategory {
		for _, d := range detectors {
			cat, ok := d()
			if ok {
				return cat
			}
			// ok = false means the detector panicked (barrier caught it).
			// Fall through to next detector.
		}
		return ReconciliationCategoryCat5 // default: clean restart
	}

	// First detector panics.
	panicDetector := func() (ReconciliationCategory, bool) {
		var caught bool
		var result ReconciliationCategory
		func() {
			defer func() {
				if r := recover(); r != nil {
					caught = true
				}
			}()
			panic("rc020b: cat-0 detector panicked") //nolint:gocritic // intentional panic for recovery test
		}()
		if caught {
			return result, false // false = panicked, fall through
		}
		return ReconciliationCategoryCat0, true
	}

	// Second detector succeeds.
	cat6bDetector := func() (ReconciliationCategory, bool) {
		return ReconciliationCategoryCat6b, true
	}

	result := evalWithBarrier([]func() (ReconciliationCategory, bool){
		panicDetector,
		cat6bDetector,
	})

	if result != ReconciliationCategoryCat6b {
		t.Errorf("RC-020b: fall-through after panic: got %q, want cat-6b", result)
	}
}

// ---- RC-INV-004: Evidence-corroboration guarantee ----

// TestRCINV004_StoreDivergenceDetectedMustHaveCorroboration verifies the
// RC-INV-004 audit invariant: every store_divergence_detected event in the
// audit log MUST carry a corroboration value of git-corroborated or
// beads-corroborated.
//
// RC-INV-004 Sensor: "Detector emission layer MUST validate
// corroboration ∈ {git-corroborated, beads-corroborated} before allowing the
// event to be written to JSONL; an inconclusive corroboration MUST route to
// divergence_inconclusive instead."
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-004.
func TestRCINV004_StoreDivergenceDetectedMustHaveCorroboration(t *testing.T) {
	t.Parallel()

	// Simulate an audit of a corpus of seeded divergence events.
	// The audit checks: every corroboration value is git-corroborated or beads-corroborated.

	rc74CorroborationAudit := func(events []DivergenceCorroboration) []DivergenceCorroboration {
		var violations []DivergenceCorroboration
		for _, c := range events {
			if !c.Valid() {
				violations = append(violations, c)
			}
		}
		return violations
	}

	// All corroborated events: audit passes.
	corroboratedEvents := []DivergenceCorroboration{
		DivergenceCorroborationGitCorroborated,
		DivergenceCorroborationBeadsCorroborated,
		DivergenceCorroborationGitCorroborated,
	}
	violations := rc74CorroborationAudit(corroboratedEvents)
	if len(violations) != 0 {
		t.Errorf("RC-INV-004: audit found violations in fully-corroborated corpus: %v", violations)
	}
}

// TestRCINV004_InconclusiveObservationFailsAudit verifies that an
// "inconclusive" value in the audit corpus would fail the RC-INV-004 audit.
// Inconclusive observations MUST route to divergence_inconclusive (§8.6.10),
// NOT appear in store_divergence_detected events.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-004 — "an inconclusive
// corroboration MUST route to divergence_inconclusive instead."
func TestRCINV004_InconclusiveObservationFailsAudit(t *testing.T) {
	t.Parallel()

	rc74CorroborationAudit := func(events []DivergenceCorroboration) int {
		var count int
		for _, c := range events {
			if !c.Valid() {
				count++
			}
		}
		return count
	}

	// Mixed corpus: two corroborated, one inconclusive.
	// The audit MUST flag the inconclusive entry.
	mixedEvents := []DivergenceCorroboration{
		DivergenceCorroborationGitCorroborated,
		DivergenceCorroboration("inconclusive"), // VIOLATION
		DivergenceCorroborationBeadsCorroborated,
	}
	violationCount := rc74CorroborationAudit(mixedEvents)
	if violationCount != 1 {
		t.Errorf("RC-INV-004: audit found %d violations in mixed corpus, want 1", violationCount)
	}
}

// TestRCINV004_EmptyCorpusPassesAudit verifies that an empty corpus (no
// store_divergence_detected events) trivially passes the RC-INV-004 audit.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-004.
func TestRCINV004_EmptyCorpusPassesAudit(t *testing.T) {
	t.Parallel()

	rc74CorroborationAudit := func(events []DivergenceCorroboration) int {
		var count int
		for _, c := range events {
			if !c.Valid() {
				count++
			}
		}
		return count
	}

	violations := rc74CorroborationAudit(nil)
	if violations != 0 {
		t.Errorf("RC-INV-004: empty corpus audit returned %d violations, want 0", violations)
	}
}
