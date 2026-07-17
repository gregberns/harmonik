package core

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

// pertypecompat_hqwn38_test.go — EV-029 per-type N-1 compatibility window tests.
//
// Spec ref: event-model.md §4.8 EV-029 — "Readers of events MUST accept the
// immediately prior schema version (N-1) for every event type AND for the
// envelope. Per-type independence means harmonik maintains up to 71+ independent
// compatibility contracts."
//
// Bead ref: hk-hqwn.38.

// TestEV029_AllRegisteredTypesHaveSchemaVersion verifies that every event type
// in the global registry has a non-zero schema version per EV-028 / EV-029.
//
// All registered types start at schema version 1 on RegisterEventType. If any
// type returns version 0 the registry implementation is broken.
func TestEV029_AllRegisteredTypesHaveSchemaVersion(t *testing.T) {
	t.Parallel()

	versions := AllPayloadSchemaVersions()
	if len(versions) == 0 {
		t.Fatal("EV-029: AllPayloadSchemaVersions() returned empty map; at least one type must be registered")
	}

	for typeName, version := range versions {
		typeName, version := typeName, version
		t.Run(typeName, func(t *testing.T) {
			t.Parallel()
			if version < 1 {
				t.Errorf("EV-029: registered type %q has schema version %d; all registered types MUST have version >= 1 per EV-028",
					typeName, version)
			}
		})
	}
}

// TestEV029_CompatTableCoversAllRegisteredTypes verifies that every event type
// present in the registry also has a PayloadCompatEntry in the per-type compat
// table, and vice versa.
//
// This is the EV-029 completeness sensor: every type the registry knows about
// must have an explicit N-1 compat declaration, and no stale entries may exist
// in the compat table for types that are no longer registered.
func TestEV029_CompatTableCoversAllRegisteredTypes(t *testing.T) {
	t.Parallel()

	registered := AllPayloadSchemaVersions()
	declared := AllPayloadCompatEntries()

	// Build lookup maps.
	declaredByName := make(map[string]PayloadCompatEntry, len(declared))
	for _, e := range declared {
		declaredByName[e.TypeName] = e
	}
	registeredNames := make(map[string]bool, len(registered))
	for name := range registered {
		registeredNames[name] = true
	}

	// Every registered type must have a compat entry.
	for typeName := range registered {
		typeName := typeName
		t.Run("registered/"+typeName, func(t *testing.T) {
			t.Parallel()
			if _, ok := declaredByName[typeName]; !ok {
				t.Errorf("EV-029: registered type %q has no PayloadCompatEntry in allPayloadCompatEntries; "+
					"add an entry to pertypecompat_hqwn38.go", typeName)
			}
		})
	}

	// Every declared compat entry must correspond to a registered type.
	for _, e := range declared {
		e := e
		t.Run("declared/"+e.TypeName, func(t *testing.T) {
			t.Parallel()
			if !registeredNames[e.TypeName] {
				t.Errorf("EV-029: PayloadCompatEntry for %q exists in allPayloadCompatEntries but the type is NOT registered; "+
					"remove the stale entry or register the type", e.TypeName)
			}
		})
	}
}

// TestEV029_InitialVersionIsOne verifies that all PayloadCompatEntries whose
// PreviousVersion is 0 (initial version) have CurrentVersion == 1.
//
// Initial-version entries are the baseline state. Any entry that advances past
// v1 must declare an explicit PreviousVersion and CompatWindowHolds.
func TestEV029_InitialVersionIsOne(t *testing.T) {
	t.Parallel()

	for _, e := range AllPayloadCompatEntries() {
		e := e
		if e.PreviousVersion != 0 {
			continue // not an initial-version entry; handled by TestEV029_NMinus1WindowHolds
		}
		t.Run(e.TypeName, func(t *testing.T) {
			t.Parallel()
			if e.CurrentVersion != 1 {
				t.Errorf("EV-029: type %q has PreviousVersion=0 but CurrentVersion=%d; "+
					"initial-version entries must have CurrentVersion=1 per EV-028",
					e.TypeName, e.CurrentVersion)
			}
			// At v1 the compat window is vacuously satisfied — no prior version exists.
			if !e.CompatWindowHolds {
				t.Errorf("EV-029: type %q at initial version has CompatWindowHolds=false; "+
					"the compat window is vacuously satisfied at v1 (no prior version exists)",
					e.TypeName)
			}
		})
	}
}

// TestEV029_NMinus1WindowHolds verifies that every PayloadCompatEntry with a
// non-zero PreviousVersion declares CompatWindowHolds = true.
//
// An entry with CompatWindowHolds = false represents a migration release (per
// ON-018/ON-019). Migration releases must be explicitly declared with operator
// pause semantics; they are NOT silently permitted here.
//
// Spec ref: event-model.md §4.8 EV-029; operator-nfr.md §4.5 ON-018, ON-019.
func TestEV029_NMinus1WindowHolds(t *testing.T) {
	t.Parallel()

	for _, e := range AllPayloadCompatEntries() {
		e := e
		if e.PreviousVersion == 0 {
			continue // initial version; vacuously satisfied, handled above
		}
		t.Run(e.TypeName, func(t *testing.T) {
			t.Parallel()
			if !e.CompatWindowHolds {
				t.Errorf("EV-029: type %q (v%d → v%d) has CompatWindowHolds=false; "+
					"this is a migration release — verify it was scheduled at an operator pause "+
					"per ON-019 and that a migration guide exists",
					e.TypeName, e.PreviousVersion, e.CurrentVersion)
			}
		})
	}
}

// TestEV029_AdditiveOnlyImpliesCompatWindowHolds verifies that every
// PayloadCompatEntry with AdditiveOnly = true also has CompatWindowHolds = true.
//
// Additive-only changes are non-breaking per §6.4. A compat window cannot fail
// for an additive-only change.
func TestEV029_AdditiveOnlyImpliesCompatWindowHolds(t *testing.T) {
	t.Parallel()

	for _, e := range AllPayloadCompatEntries() {
		e := e
		t.Run(e.TypeName, func(t *testing.T) {
			t.Parallel()
			if e.AdditiveOnly && !e.CompatWindowHolds {
				t.Errorf("EV-029: type %q has AdditiveOnly=true but CompatWindowHolds=false; "+
					"additive-only changes are non-breaking per §6.4 and MUST hold the compat window",
					e.TypeName)
			}
		})
	}
}

// TestEV029_CompatEntryVersionsMatchRegistry verifies that each PayloadCompatEntry's
// CurrentVersion matches what the global registry reports for that type.
//
// A mismatch means RegisterEventTypeAtVersion was called with a different version
// than allPayloadCompatEntries declares.
func TestEV029_CompatEntryVersionsMatchRegistry(t *testing.T) {
	t.Parallel()

	for _, e := range AllPayloadCompatEntries() {
		e := e
		t.Run(e.TypeName, func(t *testing.T) {
			t.Parallel()
			registryVersion, ok := CurrentPayloadSchemaVersion(e.TypeName)
			if !ok {
				// Type in compat table but not registered — caught by
				// TestEV029_CompatTableCoversAllRegisteredTypes; skip here.
				return
			}
			if registryVersion != e.CurrentVersion {
				t.Errorf("EV-029: type %q: compat table CurrentVersion=%d but registry version=%d; "+
					"update allPayloadCompatEntries in pertypecompat_hqwn38.go to match the registry, "+
					"or vice versa",
					e.TypeName, e.CurrentVersion, registryVersion)
			}
		})
	}
}

// TestEV029_NoDuplicateCompatEntries verifies that allPayloadCompatEntries has
// at most one entry per event type name.
func TestEV029_NoDuplicateCompatEntries(t *testing.T) {
	t.Parallel()

	seen := make(map[string]int)
	for _, e := range AllPayloadCompatEntries() {
		seen[e.TypeName]++
	}
	for typeName, count := range seen {
		typeName, count := typeName, count
		t.Run(typeName, func(t *testing.T) {
			t.Parallel()
			if count > 1 {
				t.Errorf("EV-029: type %q appears %d times in allPayloadCompatEntries; each type must appear exactly once",
					typeName, count)
			}
		})
	}
}

// TestEV029_CurrentVersionNeverLessThanOne verifies that no PayloadCompatEntry
// declares CurrentVersion < 1.
func TestEV029_CurrentVersionNeverLessThanOne(t *testing.T) {
	t.Parallel()

	for _, e := range AllPayloadCompatEntries() {
		e := e
		t.Run(e.TypeName, func(t *testing.T) {
			t.Parallel()
			if e.CurrentVersion < 1 {
				t.Errorf("EV-029: type %q has CurrentVersion=%d; schema versions must be >= 1 per EV-028",
					e.TypeName, e.CurrentVersion)
			}
		})
	}
}

// TestEV029_PreviousVersionLessThanCurrent verifies that when PreviousVersion is
// non-zero, it is strictly less than CurrentVersion.
func TestEV029_PreviousVersionLessThanCurrent(t *testing.T) {
	t.Parallel()

	for _, e := range AllPayloadCompatEntries() {
		e := e
		if e.PreviousVersion == 0 {
			continue
		}
		t.Run(e.TypeName, func(t *testing.T) {
			t.Parallel()
			if e.PreviousVersion >= e.CurrentVersion {
				t.Errorf("EV-029: type %q has PreviousVersion=%d >= CurrentVersion=%d; "+
					"PreviousVersion must be strictly less than CurrentVersion",
					e.TypeName, e.PreviousVersion, e.CurrentVersion)
			}
		})
	}
}

// TestEV029_RegisterEventTypeAtVersionRejectsVersionLessThanOne verifies that
// RegisterEventTypeAtVersion returns an error for version < 1.
func TestEV029_RegisterEventTypeAtVersionRejectsVersionLessThanOne(t *testing.T) {
	t.Parallel()

	// schemaVersion=0 must be rejected since versions must be >= 1 per EV-028.
	err := RegisterEventTypeAtVersion("test_ev029_sentinel_type_bad_version", func() EventPayload { return nil }, 0)
	if err == nil {
		t.Error("EV-029: RegisterEventTypeAtVersion with schemaVersion=0 should return an error; versions must be >= 1 per EV-028")
	}
}

// TestEV029_LookupPayloadCompatEntryRoundTrip verifies that every entry in
// allPayloadCompatEntries can be found by LookupPayloadCompatEntry.
func TestEV029_LookupPayloadCompatEntryRoundTrip(t *testing.T) {
	t.Parallel()

	for _, e := range AllPayloadCompatEntries() {
		e := e
		t.Run(e.TypeName, func(t *testing.T) {
			t.Parallel()
			got, ok := LookupPayloadCompatEntry(e.TypeName)
			if !ok {
				t.Fatalf("EV-029: LookupPayloadCompatEntry(%q) = (_, false); entry should be findable", e.TypeName)
			}
			if got.TypeName != e.TypeName {
				t.Errorf("EV-029: LookupPayloadCompatEntry(%q).TypeName = %q, want %q", e.TypeName, got.TypeName, e.TypeName)
			}
			if got.CurrentVersion != e.CurrentVersion {
				t.Errorf("EV-029: LookupPayloadCompatEntry(%q).CurrentVersion = %d, want %d", e.TypeName, got.CurrentVersion, e.CurrentVersion)
			}
		})
	}
}

// TestEV029_LookupPayloadCompatEntryMissing verifies that LookupPayloadCompatEntry
// returns (_, false) for unknown type names.
func TestEV029_LookupPayloadCompatEntryMissing(t *testing.T) {
	t.Parallel()

	_, ok := LookupPayloadCompatEntry("not_a_real_event_type_hqwn38")
	if ok {
		t.Error("EV-029: LookupPayloadCompatEntry for unknown type should return (_, false)")
	}
}

// TestEV029_ValidateEnvelopeSchemaVersionMatchesRegistry verifies that
// ValidateEnvelopeSchemaVersion returns nil when envelope.SchemaVersion equals
// the registry's per-type version, and a non-nil error when it differs.
//
// Spec ref: event-model.md §4.7 EV-028.
func TestEV029_ValidateEnvelopeSchemaVersionMatchesRegistry(t *testing.T) {
	t.Parallel()

	// Use run_started as a representative type registered at v1.
	e := makeTestEvent("run_started", 1)
	if err := ValidateEnvelopeSchemaVersion(e); err != nil {
		t.Errorf("EV-029/EV-028: ValidateEnvelopeSchemaVersion for run_started at v1 = %v, want nil", err)
	}
}

// TestEV029_ValidateEnvelopeSchemaVersionDetectsMismatch verifies that
// ValidateEnvelopeSchemaVersion returns a non-nil error when the envelope
// carries a version that does not match the per-type registered version.
func TestEV029_ValidateEnvelopeSchemaVersionDetectsMismatch(t *testing.T) {
	t.Parallel()

	// Use run_started (registered at v1); present a wrong envelope version.
	e := makeTestEvent("run_started", 99)
	err := ValidateEnvelopeSchemaVersion(e)
	if err == nil {
		t.Error("EV-029/EV-028: ValidateEnvelopeSchemaVersion with wrong version should return error")
	}
}

// makeTestEvent constructs a minimal valid Event with the given type name and
// envelope schema version. Used for ValidateEnvelopeSchemaVersion tests.
func makeTestEvent(typeName string, schemaVersion int) Event {
	return Event{
		EventID:         EventID(uuid.Must(uuid.NewV7())),
		SchemaVersion:   schemaVersion,
		Type:            typeName,
		TimestampWall:   time.Now(),
		SourceSubsystem: "github.com/gregberns/harmonik/internal/core_test",
		Payload:         json.RawMessage(`{}`),
	}
}
