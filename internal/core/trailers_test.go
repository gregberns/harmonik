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
	{"Harmonik-Verdict-Executed", TrailerTypeString, TrailerKnownExtension, "reconciliation"},
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
// classified as TrailerKnownExtension and owned by "reconciliation".
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
