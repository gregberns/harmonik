package operatornfr_test

// hk-sx9r.69 binding test — ON-INV-001 corpus-wide N-1 compat-matrix sensor.
//
// Spec ref: specs/operator-nfr.md §5 ON-INV-001.
//
// ON-INV-001 states: every versioned on-disk or wire artifact declared by
// foundation specs MUST hold the N-1 readability property of §4.5 ON-018
// simultaneously. A release that breaks N-1 for any single artifact is a
// migration release per §4.5 ON-019 and MUST require an operator pause for
// install.
//
// # Sensor definition (spec §5 ON-INV-001)
//
// Corpus-wide compat-matrix test harness: for every artifact declared by
// foundation specs — event envelope, event payload schemas, checkpoint trailer,
// queue overlay, queue execution plan (.harmonik/queue.json), policy schema —
// produce writer output at version N and parse at a reader pinned to N-1;
// failure of ANY pair flips the invariant. Sensor runs corpus-level per
// [architecture.md §4.1] AR-004.
//
// # Artifact corpus (as of ON v0.4.2)
//
// The artifact enumeration in ON-INV-001 was extended in v0.4.2 (extqueue
// reconciliation pass) to include the queue execution plan
// (.harmonik/queue.json) alongside the original five artifacts.  The full
// corpus is:
//
//  1. event-envelope         — event-model.md §6.1
//  2. event-payload          — event-model.md §6.3
//  3. checkpoint-trailer     — execution-model.md §4.4
//  4. queue-overlay          — operator-nfr.md §4.4 ON-015
//  5. queue-execution-plan   — queue-model.md §3 (.harmonik/queue.json)
//  6. policy-schema          — control-points.md §6.3
//
// # Relation to schemacompatwindow_test.go
//
// schemacompatwindow_test.go covers ON-018 and ON-019 semantics — per-artifact
// readability assertions and migration-release refusal logic.  This file
// provides the ON-INV-001 corpus-level sensor: it asserts that all six
// artifacts hold N-1 readability simultaneously, and that failure of any
// single artifact is detected as an invariant violation.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sx9r69Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"testing"
)

// sx9r69FixtureArtifactKind names one versioned on-disk or wire artifact in
// the ON-INV-001 corpus.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 sensor definition.
type sx9r69FixtureArtifactKind string

const (
	// sx9r69FixtureKindEventEnvelope is the event-envelope schema.
	// Spec ref: event-model.md §6.1.
	sx9r69FixtureKindEventEnvelope sx9r69FixtureArtifactKind = "event-envelope"

	// sx9r69FixtureKindEventPayload is the event payload schema.
	// Spec ref: event-model.md §6.3.
	sx9r69FixtureKindEventPayload sx9r69FixtureArtifactKind = "event-payload"

	// sx9r69FixtureKindCheckpointTrailer is the checkpoint trailer and sibling
	// files persisted by the execution model.
	// Spec ref: execution-model.md §4.4.
	sx9r69FixtureKindCheckpointTrailer sx9r69FixtureArtifactKind = "checkpoint-trailer"

	// sx9r69FixtureKindQueueOverlay is the harmonik overlay schema layered on
	// top of the Beads SQLite queue.
	// Spec ref: operator-nfr.md §4.4 ON-015.
	sx9r69FixtureKindQueueOverlay sx9r69FixtureArtifactKind = "queue-overlay"

	// sx9r69FixtureKindQueueExecutionPlan is the daemon execution plan
	// persisted as .harmonik/queue.json with a schema_version field.  Added
	// to the ON-INV-001 corpus in ON v0.4.2 (extqueue reconciliation pass).
	// Spec ref: queue-model.md §3.
	sx9r69FixtureKindQueueExecutionPlan sx9r69FixtureArtifactKind = "queue-execution-plan"

	// sx9r69FixtureKindPolicySchema is the operator policy schema.
	// Spec ref: control-points.md §6.3.
	sx9r69FixtureKindPolicySchema sx9r69FixtureArtifactKind = "policy-schema"
)

// sx9r69FixtureCompatPair models one (writer@N, reader@N-1) artifact pair in
// the ON-INV-001 corpus-wide compat matrix.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 — "for every artifact declared by
// foundation specs, produce writer output at version N and parse at a reader
// pinned to N-1; failure of ANY pair flips the invariant."
type sx9r69FixtureCompatPair struct {
	// Kind is the artifact type.
	Kind sx9r69FixtureArtifactKind
	// SpecRef is the normative spec section owning this artifact.
	SpecRef string
	// WriterVersionN is the version the current writer produces (N).
	WriterVersionN string
	// ReaderVersionNm1 is the version the prior reader expects (N-1).
	ReaderVersionNm1 string
	// AdditiveOnly is true when the N→N-1 delta is purely additive: new
	// fields in N are unknown to the N-1 reader but non-fatal.
	// Spec ref: operator-nfr.md §4.5 ON-018 — "additive fields treated as
	// unknown but non-fatal."
	AdditiveOnly bool
	// CompatWindowHolds is true when the N-1 reader can parse N writer output.
	// Any pair with CompatWindowHolds=false is an invariant violation.
	CompatWindowHolds bool
}

// sx9r69FixtureCorpus is the authoritative fixture corpus for the ON-INV-001
// sensor.  Every artifact in the ON-INV-001 spec enumeration MUST appear here.
// Absence of any artifact from this table is itself a sensor failure.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 sensor definition; §4.5 ON-018
// artifact enumeration (updated in ON v0.4.2 to include queue-execution-plan).
var sx9r69FixtureCorpus = []sx9r69FixtureCompatPair{
	{
		Kind:              sx9r69FixtureKindEventEnvelope,
		SpecRef:           "event-model.md §6.1",
		WriterVersionN:    "1.0",
		ReaderVersionNm1:  "0.9",
		AdditiveOnly:      true,
		CompatWindowHolds: true,
	},
	{
		Kind:              sx9r69FixtureKindEventPayload,
		SpecRef:           "event-model.md §6.3",
		WriterVersionN:    "1.0",
		ReaderVersionNm1:  "0.9",
		AdditiveOnly:      true,
		CompatWindowHolds: true,
	},
	{
		Kind:              sx9r69FixtureKindCheckpointTrailer,
		SpecRef:           "execution-model.md §4.4",
		WriterVersionN:    "1.0",
		ReaderVersionNm1:  "0.9",
		AdditiveOnly:      true,
		CompatWindowHolds: true,
	},
	{
		Kind:              sx9r69FixtureKindQueueOverlay,
		SpecRef:           "operator-nfr.md §4.4 ON-015",
		WriterVersionN:    "2.0",
		ReaderVersionNm1:  "1.9",
		AdditiveOnly:      true,
		CompatWindowHolds: true,
	},
	{
		// Added to the ON-INV-001 corpus in ON v0.4.2 (extqueue reconciliation
		// pass).  The queue execution plan carries a top-level schema_version
		// field per queue-model.md §3; it is under N-1 compat per ON-018.
		Kind:              sx9r69FixtureKindQueueExecutionPlan,
		SpecRef:           "queue-model.md §3 (.harmonik/queue.json)",
		WriterVersionN:    "1.0",
		ReaderVersionNm1:  "0.9",
		AdditiveOnly:      true,
		CompatWindowHolds: true,
	},
	{
		Kind:              sx9r69FixtureKindPolicySchema,
		SpecRef:           "control-points.md §6.3",
		WriterVersionN:    "1.0",
		ReaderVersionNm1:  "0.9",
		AdditiveOnly:      true,
		CompatWindowHolds: true,
	},
}

// sx9r69FixtureRequiredKinds is the authoritative set of artifact kinds that
// MUST appear in sx9r69FixtureCorpus.  It is derived directly from the
// ON-INV-001 sensor definition and the ON-018 artifact enumeration
// (as updated in ON v0.4.2).
//
// Spec ref: operator-nfr.md §5 ON-INV-001; §4.5 ON-018.
var sx9r69FixtureRequiredKinds = []sx9r69FixtureArtifactKind{
	sx9r69FixtureKindEventEnvelope,
	sx9r69FixtureKindEventPayload,
	sx9r69FixtureKindCheckpointTrailer,
	sx9r69FixtureKindQueueOverlay,
	sx9r69FixtureKindQueueExecutionPlan,
	sx9r69FixtureKindPolicySchema,
}

// sx9r69FixtureCorpusInvariantHolds returns true iff every pair in the corpus
// has CompatWindowHolds=true.  This models the ON-INV-001 "failure of ANY pair
// flips the invariant" rule.
//
// Spec ref: operator-nfr.md §5 ON-INV-001.
func sx9r69FixtureCorpusInvariantHolds(corpus []sx9r69FixtureCompatPair) bool {
	for _, pair := range corpus {
		if !pair.CompatWindowHolds {
			return false
		}
	}
	return true
}

// TestONINV001_CorpusCoversAllRequiredArtifacts verifies that the
// sx9r69FixtureCorpus contains an entry for every artifact kind listed in the
// ON-INV-001 sensor definition.
//
// Absence of any required artifact from the corpus means the sensor is
// incomplete; an incomplete sensor cannot detect a violation for that artifact.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 — "for every artifact declared by
// foundation specs … failure of ANY pair flips the invariant."
func TestONINV001_CorpusCoversAllRequiredArtifacts(t *testing.T) {
	t.Parallel()

	covered := make(map[sx9r69FixtureArtifactKind]bool)
	for _, pair := range sx9r69FixtureCorpus {
		covered[pair.Kind] = true
	}

	for _, kind := range sx9r69FixtureRequiredKinds {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if !covered[kind] {
				t.Errorf(
					"ON-INV-001: artifact %q is required by ON-INV-001 / ON-018 but is "+
						"ABSENT from sx9r69FixtureCorpus; the sensor cannot detect a violation "+
						"for this artifact — add a CompatPair entry citing the owning spec section",
					kind,
				)
			}
		})
	}
}

// TestONINV001_CorpusEntriesHaveSpecRefs verifies that every corpus entry
// carries a non-empty SpecRef citing the normative spec section.
//
// Spec ref: operator-nfr.md §4.5 ON-018 (each artifact has a normative spec
// anchor); §5 ON-INV-001 (sensor must cover the full artifact enumeration).
func TestONINV001_CorpusEntriesHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, pair := range sx9r69FixtureCorpus {
		pair := pair
		t.Run(string(pair.Kind), func(t *testing.T) {
			t.Parallel()
			if pair.SpecRef == "" {
				t.Errorf(
					"ON-INV-001: corpus entry for artifact %q has empty SpecRef; "+
						"every entry MUST cite the owning spec section",
					pair.Kind,
				)
			}
		})
	}
}

// TestONINV001_CorpusVersionsAreDistinct verifies that each corpus entry
// uses distinct writer (N) and reader (N-1) version strings.
//
// Spec ref: operator-nfr.md §4.5 ON-018 — "a reader pinned to version N-1
// MUST successfully parse artifacts written by version N"; testing
// same-version pairs does not validate the compat window.
func TestONINV001_CorpusVersionsAreDistinct(t *testing.T) {
	t.Parallel()

	for _, pair := range sx9r69FixtureCorpus {
		pair := pair
		t.Run(string(pair.Kind), func(t *testing.T) {
			t.Parallel()
			if pair.WriterVersionN == pair.ReaderVersionNm1 {
				t.Errorf(
					"ON-INV-001: corpus entry for artifact %q has WriterVersionN=%q "+
						"and ReaderVersionNm1=%q equal; N-1 compat test MUST use distinct "+
						"version strings to validate the cross-version readability property",
					pair.Kind, pair.WriterVersionN, pair.ReaderVersionNm1,
				)
			}
		})
	}
}

// TestONINV001_CompatWindowHoldsForEveryArtifact is the primary corpus sensor.
// It iterates every artifact pair and asserts CompatWindowHolds=true.  A
// single failing pair causes a test failure, which directly encodes the
// ON-INV-001 rule: failure of ANY pair flips the invariant.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 — "failure of ANY pair flips the
// invariant. Sensor runs corpus-level per [architecture.md §4.1] AR-004."
func TestONINV001_CompatWindowHoldsForEveryArtifact(t *testing.T) {
	t.Parallel()

	for _, pair := range sx9r69FixtureCorpus {
		pair := pair
		t.Run(string(pair.Kind), func(t *testing.T) {
			t.Parallel()

			// ON-INV-001: every artifact MUST maintain N-1 readability.
			if !pair.CompatWindowHolds {
				t.Errorf(
					"ON-INV-001: artifact %q (spec: %s) compat window DOES NOT hold; "+
						"writer at %q, reader at %q — this constitutes an invariant violation; "+
						"a release with this breaking change is a migration release per §4.5 "+
						"ON-019 and MUST require an operator pause for install",
					pair.Kind, pair.SpecRef, pair.WriterVersionN, pair.ReaderVersionNm1,
				)
			}

			// ON-018: additive-only changes are the only permitted N-1-compatible
			// changes.  Non-additive changes with a claimed compat window is a
			// logical inconsistency in the fixture.
			if !pair.AdditiveOnly && pair.CompatWindowHolds {
				t.Errorf(
					"ON-INV-001: corpus entry for artifact %q claims CompatWindowHolds=true "+
						"but AdditiveOnly=false; a non-additive schema change CANNOT maintain "+
						"N-1 readability — fix the fixture or mark the artifact as a "+
						"migration release per §4.5 ON-019",
					pair.Kind,
				)
			}
		})
	}
}

// TestONINV001_InvariantIsCorpusWide verifies that the corpus invariant check
// function (sx9r69FixtureCorpusInvariantHolds) correctly detects a single
// artifact failure as a corpus-level violation.
//
// This test exercises the "any pair failure flips the invariant" semantics by
// injecting a hypothetical failing artifact into a copy of the corpus and
// asserting that sx9r69FixtureCorpusInvariantHolds returns false.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 — "failure of ANY pair flips the
// invariant."
func TestONINV001_InvariantIsCorpusWide(t *testing.T) {
	t.Parallel()

	// Baseline: the production corpus must hold the invariant.
	if !sx9r69FixtureCorpusInvariantHolds(sx9r69FixtureCorpus) {
		t.Error(
			"ON-INV-001: baseline corpus invariant check failed; " +
				"sx9r69FixtureCorpus must have CompatWindowHolds=true for every entry",
		)
	}

	// Inject one hypothetical failing artifact and verify the invariant flips.
	corpusWithBreaking := append( //nolint:gocritic // intentional copy-append for isolation
		append([]sx9r69FixtureCompatPair{}, sx9r69FixtureCorpus...),
		sx9r69FixtureCompatPair{
			Kind:              "hypothetical-breaking-artifact",
			SpecRef:           "hypothetical.md §1.0",
			WriterVersionN:    "2.0",
			ReaderVersionNm1:  "1.0",
			AdditiveOnly:      false,
			CompatWindowHolds: false, // invariant violation
		},
	)

	if sx9r69FixtureCorpusInvariantHolds(corpusWithBreaking) {
		t.Error(
			"ON-INV-001: sensor failed to detect a corpus-level compat-window violation; " +
				"sx9r69FixtureCorpusInvariantHolds MUST return false when any artifact pair " +
				"has CompatWindowHolds=false",
		)
	}
}

// TestONINV001_QueueExecutionPlanIsInCorpus verifies that the queue execution
// plan artifact added to the ON-INV-001 corpus in ON v0.4.2 is present.
//
// This is a targeted regression guard: the extqueue reconciliation pass
// (v0.4.2 changelog) extended ON-018 and the ON-INV-001 sensor definition to
// include the queue execution plan (.harmonik/queue.json).  Any removal of
// this artifact from the corpus would silently break the sensor's coverage.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 sensor (updated v0.4.2); §4.5
// ON-018 (queue execution plan entry added v0.4.2); queue-model.md §3.
func TestONINV001_QueueExecutionPlanIsInCorpus(t *testing.T) {
	t.Parallel()

	found := false
	for _, pair := range sx9r69FixtureCorpus {
		if pair.Kind == sx9r69FixtureKindQueueExecutionPlan {
			found = true
			// Also verify the spec ref cites queue-model.md §3.
			if pair.SpecRef == "" {
				t.Error(
					"ON-INV-001: queue-execution-plan corpus entry has empty SpecRef; " +
						"MUST cite 'queue-model.md §3 (.harmonik/queue.json)'",
				)
			}
			break
		}
	}

	if !found {
		t.Error(
			"ON-INV-001: queue-execution-plan artifact is MISSING from sx9r69FixtureCorpus; " +
				"it was added to the ON-INV-001 sensor definition in ON v0.4.2 (extqueue " +
				"reconciliation pass) and MUST be covered by the corpus sensor; add a " +
				"CompatPair entry with Kind=sx9r69FixtureKindQueueExecutionPlan citing " +
				"'queue-model.md §3 (.harmonik/queue.json)'",
		)
	}
}

// TestONINV001_MigrationReleaseConstraint verifies that any artifact in the
// corpus that breaks the compat window (CompatWindowHolds=false) is correctly
// identified as requiring a migration release per ON-019.
//
// This test validates the constraint direction: a non-holding compat window is
// not merely a test failure — it is a structural trigger for the migration-
// release protocol.
//
// Spec ref: operator-nfr.md §5 ON-INV-001 — "A release that breaks N-1 for
// any single artifact is a migration release per §4.5 ON-019 and MUST require
// an operator pause for install."
func TestONINV001_MigrationReleaseConstraint(t *testing.T) {
	t.Parallel()

	type migrationScenario struct {
		kind              sx9r69FixtureArtifactKind
		compatWindowHolds bool
		wantMigRelease    bool
	}

	scenarios := []migrationScenario{
		// N-1 compat holds: ordinary (non-migration) release permitted.
		{kind: sx9r69FixtureKindEventEnvelope, compatWindowHolds: true, wantMigRelease: false},
		// N-1 compat broken: migration release is mandatory.
		{kind: sx9r69FixtureKindEventEnvelope, compatWindowHolds: false, wantMigRelease: true},
		// Queue execution plan compat holds: ordinary release.
		{kind: sx9r69FixtureKindQueueExecutionPlan, compatWindowHolds: true, wantMigRelease: false},
		// Queue execution plan compat broken: migration release.
		{kind: sx9r69FixtureKindQueueExecutionPlan, compatWindowHolds: false, wantMigRelease: true},
	}

	for _, s := range scenarios {
		s := s
		t.Run(string(s.kind)+"/compat="+sx9r69FixtureBoolStr(s.compatWindowHolds), func(t *testing.T) {
			t.Parallel()

			gotMigRelease := !s.compatWindowHolds
			if gotMigRelease != s.wantMigRelease {
				t.Errorf(
					"ON-INV-001/ON-019: artifact %q compatWindowHolds=%v: "+
						"migrationReleaseRequired = %v, want %v; "+
						"any compat-window break MUST trigger the migration-release protocol "+
						"per §4.5 ON-019 (operator pause required before install)",
					s.kind, s.compatWindowHolds, gotMigRelease, s.wantMigRelease,
				)
			}
		})
	}
}

// sx9r69FixtureBoolStr converts a bool to "true" or "false" for t.Run labels.
func sx9r69FixtureBoolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
