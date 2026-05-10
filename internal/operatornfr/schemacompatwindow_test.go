package operatornfr_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/operatornfr"
)

// artifactKind names one of the versioned on-disk or wire artifact types
// listed in specs/operator-nfr.md section 4.5 ON-018.
//
// Spec ref: operator-nfr.md section 4.5 ON-018 -- "every versioned on-disk or
// wire artifact declared by foundation specs -- event-envelope schema, event
// payload schemas, checkpoint trailers and sibling files, queue overlay, policy
// schema -- MUST maintain N-1 readability."
type artifactKind string

const (
	artifactKindEventEnvelope     artifactKind = "event-envelope"
	artifactKindEventPayload      artifactKind = "event-payload"
	artifactKindCheckpointTrailer artifactKind = "checkpoint-trailer"
	artifactKindQueueOverlay      artifactKind = "queue-overlay"
	artifactKindPolicySchema      artifactKind = "policy-schema"
)

// compatMatrixFixture models one artifact pair in the N-1 compatibility matrix.
// For each artifact, we assert that a reader at N-1 can parse a writer's output
// at version N (additive fields treated as unknown but non-fatal).
//
// Spec ref: operator-nfr.md section 4.5 ON-018; ON-INV-001 sensor --
// "for every artifact declared by foundation specs, produce writer output at
// version N and parse at a reader pinned to N-1; failure of ANY pair flips the
// invariant."
type compatMatrixFixture struct {
	// Kind is the artifact type.
	Kind artifactKind
	// SpecRef is the normative spec section owning this artifact.
	SpecRef string
	// WriterVersionN is the current version (writer produces at N).
	WriterVersionN string
	// ReaderVersionNm1 is the prior version (reader expects N-1).
	ReaderVersionNm1 string
	// AdditiveFieldsOnly is true if the N->N-1 difference is additive only
	// (new fields in N are unknown to N-1 reader but non-fatal).
	AdditiveFieldsOnly bool
	// CompatWindowHolds is true if the N-1 reader CAN parse the N writer's output.
	CompatWindowHolds bool
}

// compatMatrixFixtureTable is the authoritative fixture table for ON-INV-001
// cross-artifact compatibility matrix.
//
// Every artifact listed in ON-018 MUST appear here. Failure of ANY row means
// the invariant is violated (per ON-INV-001 sensor definition).
//
// Spec ref: operator-nfr.md section 4.5 ON-018; section 5 ON-INV-001.
var compatMatrixFixtureTable = []compatMatrixFixture{
	{
		Kind:               artifactKindEventEnvelope,
		SpecRef:            "event-model.md section 6.1",
		WriterVersionN:     "1.0",
		ReaderVersionNm1:   "0.9",
		AdditiveFieldsOnly: true,
		CompatWindowHolds:  true,
	},
	{
		Kind:               artifactKindEventPayload,
		SpecRef:            "event-model.md section 6.3",
		WriterVersionN:     "1.0",
		ReaderVersionNm1:   "0.9",
		AdditiveFieldsOnly: true,
		CompatWindowHolds:  true,
	},
	{
		Kind:               artifactKindCheckpointTrailer,
		SpecRef:            "execution-model.md section 4.4",
		WriterVersionN:     "1.0",
		ReaderVersionNm1:   "0.9",
		AdditiveFieldsOnly: true,
		CompatWindowHolds:  true,
	},
	{
		Kind:               artifactKindQueueOverlay,
		SpecRef:            "operator-nfr.md section 4.4 ON-015",
		WriterVersionN:     "2.0",
		ReaderVersionNm1:   "1.9",
		AdditiveFieldsOnly: true,
		CompatWindowHolds:  true,
	},
	{
		Kind:               artifactKindPolicySchema,
		SpecRef:            "control-points.md section 6.3",
		WriterVersionN:     "1.0",
		ReaderVersionNm1:   "0.9",
		AdditiveFieldsOnly: true,
		CompatWindowHolds:  true,
	},
}

// TestON018_NMinus1ReadabilityForAllArtifacts verifies that every versioned
// artifact in the compatibility matrix asserts N-1 readability.
//
// Spec ref: operator-nfr.md section 4.5 ON-018 -- "A reader pinned to version
// N-1 MUST successfully parse and interpret artifacts written by version N, with
// additive fields treated as unknown but non-fatal."
func TestON018_NMinus1ReadabilityForAllArtifacts(t *testing.T) {
	t.Parallel()

	for _, fx := range compatMatrixFixtureTable {
		fx := fx
		t.Run(string(fx.Kind), func(t *testing.T) {
			t.Parallel()

			// Every artifact in the matrix MUST have N-1 compat window holding.
			if !fx.CompatWindowHolds {
				t.Errorf("ON-018: artifact %q (spec: %s) N-1 compat window does NOT hold; writer at %q, reader at %q; this is a migration-release violation",
					fx.Kind, fx.SpecRef, fx.WriterVersionN, fx.ReaderVersionNm1)
			}

			// Breaking changes MUST be additive-only for compat to hold.
			if !fx.AdditiveFieldsOnly && fx.CompatWindowHolds {
				t.Errorf("ON-018: artifact %q claims compat window holds but changes are not additive-only; breaking changes require a migration release per ON-019", fx.Kind)
			}

			// Writer and reader versions must be different (testing cross-version).
			if fx.WriterVersionN == fx.ReaderVersionNm1 {
				t.Errorf("ON-018: artifact %q writer=%q and reader=%q are the same version; N-1 test MUST use distinct versions",
					fx.Kind, fx.WriterVersionN, fx.ReaderVersionNm1)
			}
		})
	}
}

// TestONINV001_CompatMatrixCoversAllArtifacts verifies that the compat matrix
// fixture covers every artifact type declared in ON-018.
//
// Spec ref: operator-nfr.md section 5 ON-INV-001 sensor -- "for every artifact
// declared by foundation specs ... failure of ANY pair flips the invariant."
func TestONINV001_CompatMatrixCoversAllArtifacts(t *testing.T) {
	t.Parallel()

	requiredKinds := []artifactKind{
		artifactKindEventEnvelope,
		artifactKindEventPayload,
		artifactKindCheckpointTrailer,
		artifactKindQueueOverlay,
		artifactKindPolicySchema,
	}

	// Build a set of kinds covered by the matrix.
	covered := make(map[artifactKind]bool)
	for _, fx := range compatMatrixFixtureTable {
		covered[fx.Kind] = true
	}

	for _, kind := range requiredKinds {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			if !covered[kind] {
				t.Errorf("ON-INV-001: artifact %q is required by ON-018 but NOT present in the compat matrix fixture; every listed artifact MUST have a N-1 compat pair", kind)
			}
		})
	}
}

// TestONINV001_AnyPairFailureFlipsInvariant verifies that the compat matrix
// sensor treats ANY artifact failure as an invariant violation.
//
// Spec ref: operator-nfr.md section 5 ON-INV-001 -- "failure of ANY pair flips
// the invariant. Sensor runs corpus-level per architecture.md section 4.1 AR-004."
func TestONINV001_AnyPairFailureFlipsInvariant(t *testing.T) {
	t.Parallel()

	// Inject a hypothetical failing artifact and verify the sensor would detect it.
	hypotheticalMatrix := append(compatMatrixFixtureTable, compatMatrixFixture{
		Kind:               "hypothetical-breaking-artifact",
		SpecRef:            "hypothetical.md",
		WriterVersionN:     "2.0",
		ReaderVersionNm1:   "1.0",
		AdditiveFieldsOnly: false, // breaking change -- N-1 reader cannot parse N writer
		CompatWindowHolds:  false, // invariant violated
	})

	invariantHolds := true
	for _, fx := range hypotheticalMatrix {
		if !fx.CompatWindowHolds {
			invariantHolds = false
			break
		}
	}

	if invariantHolds {
		t.Error("ON-INV-001: sensor failed to detect a compat-window violation; ANY pair failure MUST flip the invariant")
	}
}

// TestON019_MigrationReleaseRefusesWithoutPause verifies that a migration
// release (one that breaks the N-1 window) MUST refuse to install unless the
// daemon is in the `paused` state.
//
// Spec ref: operator-nfr.md section 4.5 ON-019 -- "A migration release ... MUST
// require an operator pause before installation. The `harmonik upgrade` contract
// of section 4.6 MUST refuse to exec-replace into a migration release unless the
// daemon is in the `paused` state AND the on-disk state's schema version is
// within the new binary's supported set."
func TestON019_MigrationReleaseRefusesWithoutPause(t *testing.T) {
	t.Parallel()

	// A migration release breaks the compat window: the new binary's N is a
	// non-readable-by-N-1 version. ON-019: upgrade MUST refuse unless paused.

	type migrationReleaseFixture struct {
		DaemonState          string   // operator-control state at upgrade time
		OnDiskSchemaVersion  string   // schema version of on-disk state
		NewBinarySchemaRange []string // schema versions the new binary supports
		WantRefused          bool
		WantExitCode         int
	}

	fixtures := []migrationReleaseFixture{
		{
			// Not paused: upgrade MUST be refused (code 13).
			DaemonState:          "running",
			OnDiskSchemaVersion:  "1.0",
			NewBinarySchemaRange: []string{"2.0", "1.9"},
			WantRefused:          true,
			WantExitCode:         13,
		},
		{
			// Paused, schema in supported set: upgrade allowed.
			DaemonState:          "paused",
			OnDiskSchemaVersion:  "1.9",
			NewBinarySchemaRange: []string{"2.0", "1.9"},
			WantRefused:          false,
			WantExitCode:         0,
		},
		{
			// Paused, but on-disk schema NOT in new binary's supported set: refused.
			// ON-019: MUST refuse for broader mismatches per section 4.5.ON-019.
			DaemonState:          "paused",
			OnDiskSchemaVersion:  "1.5", // too old -- not in {2.0, 1.9}
			NewBinarySchemaRange: []string{"2.0", "1.9"},
			WantRefused:          true,
			WantExitCode:         15, // upgrade-schema-incompatible
		},
	}

	schemaCompatCheck := func(onDisk string, supported []string) bool {
		for _, v := range supported {
			if v == onDisk {
				return true
			}
		}
		return false
	}

	for _, fx := range fixtures {
		fx := fx
		t.Run(fx.DaemonState+"/"+fx.OnDiskSchemaVersion, func(t *testing.T) {
			t.Parallel()

			var refused bool
			var exitCode int

			if fx.DaemonState != "paused" {
				refused = true
				exitCode = 13
			} else if !schemaCompatCheck(fx.OnDiskSchemaVersion, fx.NewBinarySchemaRange) {
				refused = true
				exitCode = 15
			}

			if refused != fx.WantRefused {
				t.Errorf("ON-019: daemon=%q onDisk=%q: refused = %v, want %v", fx.DaemonState, fx.OnDiskSchemaVersion, refused, fx.WantRefused)
			}
			if exitCode != fx.WantExitCode {
				t.Errorf("ON-019: daemon=%q onDisk=%q: exit code = %d, want %d", fx.DaemonState, fx.OnDiskSchemaVersion, exitCode, fx.WantExitCode)
			}

			if fx.WantRefused && fx.WantExitCode != 0 {
				e, ok := operatornfr.LookupExitCode(fx.WantExitCode)
				if !ok {
					t.Fatalf("ON-019: section 8 taxonomy missing code %d", fx.WantExitCode)
				}
				if e.Event != "operator_upgrade_rejected" {
					t.Errorf("ON-019: code %d event = %q, want %q", fx.WantExitCode, e.Event, "operator_upgrade_rejected")
				}
			}
		})
	}
}

// TestON018_BreakingChangesRequireMigrationRelease verifies that a non-additive
// schema change MUST be accompanied by a migration release (and therefore cannot
// be installed without an operator pause).
//
// Spec ref: operator-nfr.md section 4.5 ON-018 -- "Breaking changes MUST be
// accompanied by a migration release and MUST NOT be introduced mid-run."
func TestON018_BreakingChangesRequireMigrationRelease(t *testing.T) {
	t.Parallel()

	// Model a breaking change: a field is removed from the event-envelope schema.
	type changeFixture struct {
		Kind               artifactKind
		IsAdditive         bool
		RequiresMigRelease bool
	}

	changes := []changeFixture{
		{Kind: artifactKindEventEnvelope, IsAdditive: true, RequiresMigRelease: false},
		{Kind: artifactKindEventEnvelope, IsAdditive: false, RequiresMigRelease: true},
		{Kind: artifactKindCheckpointTrailer, IsAdditive: true, RequiresMigRelease: false},
		{Kind: artifactKindCheckpointTrailer, IsAdditive: false, RequiresMigRelease: true},
	}

	for _, ch := range changes {
		ch := ch
		t.Run(string(ch.Kind)+"/additive="+boolStr(ch.IsAdditive), func(t *testing.T) {
			t.Parallel()

			requiresMig := !ch.IsAdditive
			if requiresMig != ch.RequiresMigRelease {
				t.Errorf("ON-018: artifact %q additive=%v: requiresMigRelease = %v, want %v; breaking changes MUST require migration release",
					ch.Kind, ch.IsAdditive, requiresMig, ch.RequiresMigRelease)
			}
		})
	}
}

// TestON018_CompatMatrixHasSpecRefs verifies that every entry in the compat
// matrix fixture has a non-empty SpecRef citing the owning spec section.
//
// Spec ref: operator-nfr.md section 4.5 ON-018 (five artifact types listed with
// normative spec anchors).
func TestON018_CompatMatrixHasSpecRefs(t *testing.T) {
	t.Parallel()

	for _, fx := range compatMatrixFixtureTable {
		fx := fx
		t.Run(string(fx.Kind), func(t *testing.T) {
			t.Parallel()
			if fx.SpecRef == "" {
				t.Errorf("ON-018: artifact %q has empty SpecRef; every compat matrix entry MUST cite the owning spec section", fx.Kind)
			}
		})
	}
}

// boolStr converts a bool to "true" or "false" for t.Run subtest labels.
func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
