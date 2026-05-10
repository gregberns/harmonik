package core

import (
	"reflect"
	"testing"
)

// expectedTrailerEntries enumerates all 8 registry entries (7 primary rows +
// Harmonik-Verdict-Executed known-extension) in the canonical declared order
// used by RegistryEntries().
var expectedTrailerEntries = []struct {
	key         string
	typ         TrailerValueType
	requirement TrailerRequirement
	ownerSpec   string
}{
	{"Harmonik-Bead-ID", TrailerTypeString, TrailerConditional, "execution-model"},
	{"Harmonik-Run-ID", TrailerTypeUUID, TrailerRequired, "execution-model"},
	{"Harmonik-Schema-Version", TrailerTypeInteger, TrailerRequired, "execution-model"},
	{"Harmonik-State-ID", TrailerTypeUUID, TrailerRequired, "execution-model"},
	{"Harmonik-Target-Run-ID", TrailerTypeUUID, TrailerConditional, "reconciliation"},
	{"Harmonik-Transition-ID", TrailerTypeUUID, TrailerRequired, "execution-model"},
	{"Harmonik-Workflow-Class", TrailerTypeEnum, TrailerConditional, "reconciliation"},
	{"Harmonik-Verdict-Executed", TrailerTypeEnum, TrailerKnownExtension, "reconciliation"},
}

// TestLookupTrailer_AllEntries verifies that each of the 8 registry entries can be
// looked up by exact key and that the returned spec has the correct type, requirement,
// and owning spec.
func TestLookupTrailer_AllEntries(t *testing.T) {
	t.Parallel()

	for _, want := range expectedTrailerEntries {
		t.Run(want.key, func(t *testing.T) {
			t.Parallel()

			spec, ok := LookupTrailer(want.key)
			if !ok {
				t.Fatalf("LookupTrailer(%q) returned ok=false, want true", want.key)
			}
			if spec.Key != want.key {
				t.Errorf("Key = %q, want %q", spec.Key, want.key)
			}
			if spec.Type != want.typ {
				t.Errorf("Type = %v, want %v", spec.Type, want.typ)
			}
			if spec.Requirement != want.requirement {
				t.Errorf("Requirement = %v, want %v", spec.Requirement, want.requirement)
			}
			if spec.OwnerSpec != want.ownerSpec {
				t.Errorf("OwnerSpec = %q, want %q", spec.OwnerSpec, want.ownerSpec)
			}
		})
	}
}

// TestLookupTrailer_UnknownKey verifies that an unregistered key returns ok=false.
func TestLookupTrailer_UnknownKey(t *testing.T) {
	t.Parallel()

	_, ok := LookupTrailer("Bogus-Trailer")
	if ok {
		t.Error("LookupTrailer(\"Bogus-Trailer\") returned ok=true, want false")
	}
}

// TestIsKnownTrailer_KnownKey verifies that a registered key returns true.
func TestIsKnownTrailer_KnownKey(t *testing.T) {
	t.Parallel()

	if !IsKnownTrailer("Harmonik-Run-ID") {
		t.Error("IsKnownTrailer(\"Harmonik-Run-ID\") = false, want true")
	}
}

// TestIsKnownTrailer_UnknownKey verifies that an unregistered key returns false.
func TestIsKnownTrailer_UnknownKey(t *testing.T) {
	t.Parallel()

	if IsKnownTrailer("Bogus-Trailer") {
		t.Error("IsKnownTrailer(\"Bogus-Trailer\") = true, want false")
	}
}

// TestRegistryEntries_Count verifies that exactly 8 entries are returned.
func TestRegistryEntries_Count(t *testing.T) {
	t.Parallel()

	entries := RegistryEntries()
	if len(entries) != 8 {
		t.Errorf("RegistryEntries() len = %d, want 8", len(entries))
	}
}

// TestRegistryEntries_StableOrder verifies that two successive calls to
// RegistryEntries() return entries in the same order.
func TestRegistryEntries_StableOrder(t *testing.T) {
	t.Parallel()

	first := RegistryEntries()
	second := RegistryEntries()

	if len(first) != len(second) {
		t.Fatalf("RegistryEntries() length mismatch: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Key != second[i].Key {
			t.Errorf("position %d: first=%q second=%q (order is not stable)", i, first[i].Key, second[i].Key)
		}
	}
}

// TestRegistryEntries_DeclaredOrder verifies that RegistryEntries() returns entries
// in the canonical declared order documented in expectedTrailerEntries.
func TestRegistryEntries_DeclaredOrder(t *testing.T) {
	t.Parallel()

	entries := RegistryEntries()
	if len(entries) != len(expectedTrailerEntries) {
		t.Fatalf("RegistryEntries() len = %d, want %d", len(entries), len(expectedTrailerEntries))
	}
	for i, want := range expectedTrailerEntries {
		if entries[i].Key != want.key {
			t.Errorf("position %d: Key = %q, want %q", i, entries[i].Key, want.key)
		}
	}
}

// TestRegistryEntries_IsCopy verifies that mutating the returned slice does not
// affect subsequent calls (the returned slice is a defensive copy).
func TestRegistryEntries_IsCopy(t *testing.T) {
	t.Parallel()

	first := RegistryEntries()
	// Overwrite the first element's key.
	first[0].Key = "MUTATED"

	second := RegistryEntries()
	if second[0].Key == "MUTATED" {
		t.Error("RegistryEntries() returned a non-copy: mutating the result affected subsequent calls")
	}
}

// TestWorkflowClass_EnumValues verifies that Harmonik-Workflow-Class carries the
// MVH enum value set {reconciliation}.
func TestWorkflowClass_EnumValues(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Workflow-Class")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Workflow-Class\") returned ok=false")
	}
	want := []string{"reconciliation"}
	if !reflect.DeepEqual(spec.EnumValues, want) {
		t.Errorf("EnumValues = %v, want %v", spec.EnumValues, want)
	}
}

// TestConditionalRows verifies that the three conditional trailers carry
// TrailerConditional as their Requirement.
func TestConditionalRows(t *testing.T) {
	t.Parallel()

	conditionals := []string{
		"Harmonik-Bead-ID",
		"Harmonik-Workflow-Class",
		"Harmonik-Target-Run-ID",
	}
	for _, key := range conditionals {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			spec, ok := LookupTrailer(key)
			if !ok {
				t.Fatalf("LookupTrailer(%q) returned ok=false", key)
			}
			if spec.Requirement != TrailerConditional {
				t.Errorf("Requirement = %v, want TrailerConditional", spec.Requirement)
			}
		})
	}
}

// TestKnownExtension_VerdictExecuted verifies that Harmonik-Verdict-Executed is
// classified as TrailerKnownExtension, typed as TrailerTypeEnum, and owned by
// "reconciliation". Per schemas.md §6.4, the value is a fixed literal "true";
// we encode this as a 1-value enum so existing enum-validation machinery applies.
func TestKnownExtension_VerdictExecuted(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Verdict-Executed")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Verdict-Executed\") returned ok=false")
	}
	if spec.Requirement != TrailerKnownExtension {
		t.Errorf("Requirement = %v, want TrailerKnownExtension", spec.Requirement)
	}
	if spec.OwnerSpec != "reconciliation" {
		t.Errorf("OwnerSpec = %q, want \"reconciliation\"", spec.OwnerSpec)
	}
	if spec.Type != TrailerTypeEnum {
		t.Errorf("Type = %v, want TrailerTypeEnum", spec.Type)
	}
}

// verdictExecutedFixtureLookup is a test helper that looks up Harmonik-Verdict-Executed
// and fatals if the key is absent from the registry.
func verdictExecutedFixtureLookup(t *testing.T) TrailerSpec {
	t.Helper()
	spec, ok := LookupTrailer("Harmonik-Verdict-Executed")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Verdict-Executed\") returned ok=false")
	}
	return spec
}

// TestVerdictExecuted_EnumValues verifies that Harmonik-Verdict-Executed carries
// exactly {"true"} as its EnumValues, encoding the fixed-literal contract of
// schemas.md §6.4 (RC-023: any other value is malformed).
func TestVerdictExecuted_EnumValues(t *testing.T) {
	t.Parallel()

	spec := verdictExecutedFixtureLookup(t)
	want := []string{"true"}
	if !reflect.DeepEqual(spec.EnumValues, want) {
		t.Errorf("EnumValues = %v, want %v (schemas.md §6.4: value is fixed literal \"true\")", spec.EnumValues, want)
	}
}

// TestVerdictExecuted_NonTrueValueFailsEnum verifies that a value other than "true"
// is malformed per RC-023. The registry encodes this as a 1-value enum; ValidateTrailerValue
// enforces it at runtime.
//
// Spec ref: schemas.md §6.4; RC-023.
func TestVerdictExecuted_NonTrueValueFailsEnum(t *testing.T) {
	t.Parallel()

	spec := verdictExecutedFixtureLookup(t)
	// Assert registry shape: exactly one permitted value.
	if len(spec.EnumValues) != 1 || spec.EnumValues[0] != "true" {
		t.Errorf("expected EnumValues=[\"true\"], got %v; registry shape is the enforcement hook for RC-023", spec.EnumValues)
	}
	// ValidateTrailerValue now exists; verify "false" is rejected.
	if err := ValidateTrailerValue(spec, "false"); err == nil {
		t.Error("ValidateTrailerValue(Harmonik-Verdict-Executed, \"false\") = nil, want error (RC-023: only \"true\" is valid)")
	}
	// And "true" is accepted.
	if err := ValidateTrailerValue(spec, "true"); err != nil {
		t.Errorf("ValidateTrailerValue(Harmonik-Verdict-Executed, \"true\") = %v, want nil", err)
	}
}

// TestRequiredRows verifies that the four unconditionally required trailers carry
// TrailerRequired as their Requirement.
func TestRequiredRows(t *testing.T) {
	t.Parallel()

	required := []string{
		"Harmonik-Run-ID",
		"Harmonik-State-ID",
		"Harmonik-Transition-ID",
		"Harmonik-Schema-Version",
	}
	for _, key := range required {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			spec, ok := LookupTrailer(key)
			if !ok {
				t.Fatalf("LookupTrailer(%q) returned ok=false", key)
			}
			if spec.Requirement != TrailerRequired {
				t.Errorf("Requirement = %v, want TrailerRequired", spec.Requirement)
			}
		})
	}
}

// TestEM017_RequiredSetConformance is a structural-conformance test that asserts
// the registry contains exactly the four unconditionally required trailers named
// by EM-017 — no more, no fewer — and exactly one EM-017 conditional trailer
// (Harmonik-Bead-ID).
//
// This test will fail if a future edit adds a new TrailerRequired row without
// a corresponding EM-017 amendment, preventing silent registry drift.
//
// Cites: execution-model.md §4.4.EM-017; §6.2.
func TestEM017_RequiredSetConformance(t *testing.T) {
	t.Parallel()

	// EM-017 names exactly these four Required trailers.
	wantRequired := map[string]bool{
		"Harmonik-Run-ID":         true,
		"Harmonik-State-ID":       true,
		"Harmonik-Transition-ID":  true,
		"Harmonik-Schema-Version": true,
	}
	// EM-017 names exactly one Conditional trailer from the execution-model set.
	wantEM017Conditional := map[string]bool{
		"Harmonik-Bead-ID": true,
	}

	var gotRequired []string
	var gotEM017Conditional []string

	for _, spec := range RegistryEntries() {
		switch spec.Requirement {
		case TrailerRequired:
			gotRequired = append(gotRequired, spec.Key)
			if !wantRequired[spec.Key] {
				t.Errorf("unexpected TrailerRequired entry %q: not listed in EM-017", spec.Key)
			}
		case TrailerConditional:
			if spec.OwnerSpec == "execution-model" {
				gotEM017Conditional = append(gotEM017Conditional, spec.Key)
				if !wantEM017Conditional[spec.Key] {
					t.Errorf("unexpected EM-017 TrailerConditional entry %q: not listed in EM-017", spec.Key)
				}
			}
		}
	}

	if len(gotRequired) != len(wantRequired) {
		t.Errorf("TrailerRequired count = %d, want %d (EM-017 names exactly 4 required trailers); got %v",
			len(gotRequired), len(wantRequired), gotRequired)
	}
	if len(gotEM017Conditional) != len(wantEM017Conditional) {
		t.Errorf("EM-017 TrailerConditional count = %d, want %d; got %v",
			len(gotEM017Conditional), len(wantEM017Conditional), gotEM017Conditional)
	}
}

// TestAllEntriesHaveNonEmptyDescription verifies that every registry entry
// carries a non-empty Description field.
func TestAllEntriesHaveNonEmptyDescription(t *testing.T) {
	t.Parallel()

	for _, spec := range RegistryEntries() {
		if spec.Description == "" {
			t.Errorf("entry %q has empty Description", spec.Key)
		}
	}
}

// ---- ValidateTrailerValue tests (hk-63oh.81) ----

// validateTrailerValueFixtureSpec returns a TrailerSpec for testing purposes.
func validateTrailerValueFixtureSpec(typ TrailerValueType, enumValues []string) TrailerSpec {
	return TrailerSpec{
		Key:        "Test-Trailer",
		Type:       typ,
		EnumValues: enumValues,
	}
}

// TestValidateTrailerValue_UUID_Valid verifies that a well-formed UUIDv7 passes
// validation for TrailerTypeUUID.
//
// Spec ref: execution-model.md §4.4 EM-013 (UUIDv7 join-key invariant).
func TestValidateTrailerValue_UUID_Valid(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeUUID, nil)
	// UUIDv7: version nibble = 7 at position 14 of the hex string.
	uuidv7 := "018f1e2a-0000-7000-8000-000000000001"
	if err := ValidateTrailerValue(spec, uuidv7); err != nil {
		t.Errorf("ValidateTrailerValue(UUID, %q) = %v, want nil", uuidv7, err)
	}
}

// TestValidateTrailerValue_UUID_RejectsNonUUID verifies that a non-UUID string
// fails TrailerTypeUUID validation.
func TestValidateTrailerValue_UUID_RejectsNonUUID(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeUUID, nil)
	if err := ValidateTrailerValue(spec, "not-a-uuid"); err == nil {
		t.Error("ValidateTrailerValue(UUID, \"not-a-uuid\") = nil, want error")
	}
}

// TestValidateTrailerValue_UUID_RejectsUUIDv4 verifies that a valid UUIDv4
// fails TrailerTypeUUID validation because EM-013 requires UUIDv7.
func TestValidateTrailerValue_UUID_RejectsUUIDv4(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeUUID, nil)
	// UUIDv4: version nibble = 4.
	uuidv4 := "550e8400-e29b-41d4-a716-446655440000"
	if err := ValidateTrailerValue(spec, uuidv4); err == nil {
		t.Errorf("ValidateTrailerValue(UUID, %q) = nil, want error (must require UUIDv7)", uuidv4)
	}
}

// TestValidateTrailerValue_Integer_Valid verifies that a decimal integer string
// passes TrailerTypeInteger validation.
func TestValidateTrailerValue_Integer_Valid(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeInteger, nil)
	for _, v := range []string{"0", "1", "42", "100", "-1"} {
		v := v
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTrailerValue(spec, v); err != nil {
				t.Errorf("ValidateTrailerValue(Integer, %q) = %v, want nil", v, err)
			}
		})
	}
}

// TestValidateTrailerValue_Integer_RejectsNonInteger verifies that non-decimal
// strings fail TrailerTypeInteger validation.
func TestValidateTrailerValue_Integer_RejectsNonInteger(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeInteger, nil)
	for _, v := range []string{"1.5", "abc", "0x10", "1e3", " 1"} {
		v := v
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTrailerValue(spec, v); err == nil {
				t.Errorf("ValidateTrailerValue(Integer, %q) = nil, want error", v)
			}
		})
	}
}

// TestValidateTrailerValue_Enum_Valid verifies that a value in EnumValues
// passes TrailerTypeEnum validation.
func TestValidateTrailerValue_Enum_Valid(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeEnum, []string{"reconciliation", "other"})
	if err := ValidateTrailerValue(spec, "reconciliation"); err != nil {
		t.Errorf("ValidateTrailerValue(Enum, \"reconciliation\") = %v, want nil", err)
	}
	if err := ValidateTrailerValue(spec, "other"); err != nil {
		t.Errorf("ValidateTrailerValue(Enum, \"other\") = %v, want nil", err)
	}
}

// TestValidateTrailerValue_Enum_RejectsUnknownValue verifies that a value not
// in EnumValues fails TrailerTypeEnum validation.
func TestValidateTrailerValue_Enum_RejectsUnknownValue(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeEnum, []string{"reconciliation"})
	if err := ValidateTrailerValue(spec, "unknown"); err == nil {
		t.Error("ValidateTrailerValue(Enum, \"unknown\") = nil, want error")
	}
}

// TestValidateTrailerValue_String_Valid verifies that any non-empty string
// passes TrailerTypeString validation.
func TestValidateTrailerValue_String_Valid(t *testing.T) {
	t.Parallel()

	spec := validateTrailerValueFixtureSpec(TrailerTypeString, nil)
	for _, v := range []string{"abc", "hk-63oh", "any value", "123"} {
		v := v
		t.Run(v, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTrailerValue(spec, v); err != nil {
				t.Errorf("ValidateTrailerValue(String, %q) = %v, want nil", v, err)
			}
		})
	}
}

// TestValidateTrailerValue_EmptyAlwaysErrors verifies that an empty value is
// rejected for every trailer type.
func TestValidateTrailerValue_EmptyAlwaysErrors(t *testing.T) {
	t.Parallel()

	types := []TrailerValueType{
		TrailerTypeUUID,
		TrailerTypeInteger,
		TrailerTypeEnum,
		TrailerTypeString,
	}
	for _, typ := range types {
		typ := typ
		t.Run("", func(t *testing.T) {
			t.Parallel()
			spec := validateTrailerValueFixtureSpec(typ, []string{"true"})
			if err := ValidateTrailerValue(spec, ""); err == nil {
				t.Errorf("ValidateTrailerValue(type=%d, \"\") = nil, want error (empty value must be rejected)", typ)
			}
		})
	}
}

// TestValidateTrailerValue_HarmonikRunID verifies that the Harmonik-Run-ID
// registry entry passes validation against a well-formed UUIDv7.
//
// Spec ref: execution-model.md §6.2; EM-013 (UUIDv7 join-key invariant).
func TestValidateTrailerValue_HarmonikRunID(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Run-ID")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Run-ID\") returned ok=false")
	}
	uuidv7 := "018f1e2a-0000-7000-8000-000000000099"
	if err := ValidateTrailerValue(spec, uuidv7); err != nil {
		t.Errorf("ValidateTrailerValue(Harmonik-Run-ID, UUIDv7) = %v, want nil", err)
	}
}

// TestValidateTrailerValue_HarmonikSchemaVersion verifies that the
// Harmonik-Schema-Version registry entry accepts decimal integers.
//
// Spec ref: execution-model.md §6.2; EM-022 (N-1 readable).
func TestValidateTrailerValue_HarmonikSchemaVersion(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Schema-Version")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Schema-Version\") returned ok=false")
	}
	if err := ValidateTrailerValue(spec, "1"); err != nil {
		t.Errorf("ValidateTrailerValue(Harmonik-Schema-Version, \"1\") = %v, want nil", err)
	}
	if err := ValidateTrailerValue(spec, "3.14"); err == nil {
		t.Error("ValidateTrailerValue(Harmonik-Schema-Version, \"3.14\") = nil, want error")
	}
}

// TestValidateTrailerValue_HarmonikWorkflowClass verifies that the
// Harmonik-Workflow-Class registry entry accepts "reconciliation" and rejects
// unknown values.
//
// Spec ref: reconciliation/schemas.md §6.5; RC-001.
func TestValidateTrailerValue_HarmonikWorkflowClass(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Workflow-Class")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Workflow-Class\") returned ok=false")
	}
	if err := ValidateTrailerValue(spec, "reconciliation"); err != nil {
		t.Errorf("ValidateTrailerValue(Harmonik-Workflow-Class, \"reconciliation\") = %v, want nil", err)
	}
	if err := ValidateTrailerValue(spec, "ordinary"); err == nil {
		t.Error("ValidateTrailerValue(Harmonik-Workflow-Class, \"ordinary\") = nil, want error")
	}
}

// TestValidateTrailerValue_VerdictExecuted_RC023 verifies that ValidateTrailerValue
// enforces the RC-023 malformed-value rule for Harmonik-Verdict-Executed:
// only "true" is valid; any other value is malformed.
//
// Spec ref: reconciliation/schemas.md §6.4; RC-023.
func TestValidateTrailerValue_VerdictExecuted_RC023(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Verdict-Executed")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Verdict-Executed\") returned ok=false")
	}
	// "true" is the only valid value (RC-023, schemas.md §6.4 fixed literal).
	if err := ValidateTrailerValue(spec, "true"); err != nil {
		t.Errorf("ValidateTrailerValue(Harmonik-Verdict-Executed, \"true\") = %v, want nil (RC-023)", err)
	}
	// Any other value is malformed per RC-023.
	for _, bad := range []string{"false", "1", "yes", "TRUE", "True"} {
		bad := bad
		t.Run(bad, func(t *testing.T) {
			t.Parallel()
			if err := ValidateTrailerValue(spec, bad); err == nil {
				t.Errorf("ValidateTrailerValue(Harmonik-Verdict-Executed, %q) = nil, want error (RC-023: only \"true\" is valid)", bad)
			}
		})
	}
}
