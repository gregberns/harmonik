// Package core — BI-018 traceability sensors for the Harmonik-Bead-ID trailer.
//
// This file provides named requirement-traceable sensors for the
// Harmonik-Bead-ID checkpoint-trailer registration per
// beads-integration.md §4 BI-018: checkpoint commits for bead-bound runs
// carry the bead-ID trailer; the trailer MUST be absent for non-bead-bound runs.
package core

import (
	"strings"
	"testing"
)

// TestTrailerBI018_HarmonikBeadIDIsRegistered verifies that the Harmonik-Bead-ID
// trailer is present in the registry and meets all structural requirements
// specified by beads-integration.md §4 BI-018.
//
// BI-018 contract: the trailer is conditional (present on bead-bound runs,
// absent on non-bead-bound runs), owned by the execution-model spec, and has
// a non-empty description.
func TestTrailerBI018_HarmonikBeadIDIsRegistered(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Bead-ID")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Bead-ID\") returned ok=false; BI-018 requires this trailer to be registered")
	}

	if spec.Key != "Harmonik-Bead-ID" {
		t.Errorf("Key = %q, want \"Harmonik-Bead-ID\"", spec.Key)
	}

	if spec.Type != TrailerTypeString {
		t.Errorf("Type = %v, want TrailerTypeString; BI-018 trailer value is an opaque bead identifier string", spec.Type)
	}

	// BI-018: trailer is absent for non-bead-bound runs, so it MUST NOT be
	// TrailerRequired. TrailerConditional is the correct classification.
	if spec.Requirement != TrailerConditional {
		t.Errorf("Requirement = %v, want TrailerConditional; BI-018 stipulates presence only on bead-bound runs", spec.Requirement)
	}

	// Trailer semantics are owned by the execution-model spec (EM-014).
	if spec.OwnerSpec != "execution-model" {
		t.Errorf("OwnerSpec = %q, want \"execution-model\"", spec.OwnerSpec)
	}

	if spec.Description == "" {
		t.Error("Description is empty; BI-018 registry entry must carry a human-readable description")
	}
}

// TestTrailerBI018_DescriptionMentionsConditionality verifies that the
// Harmonik-Bead-ID trailer description contains language reflecting the
// BI-018 / EM-014 conditionality contract: present when tied to a bead,
// absent otherwise.
//
// Cites: beads-integration.md §4 BI-018; execution-model.md EM-014.
func TestTrailerBI018_DescriptionMentionsConditionality(t *testing.T) {
	t.Parallel()

	spec, ok := LookupTrailer("Harmonik-Bead-ID")
	if !ok {
		t.Fatal("LookupTrailer(\"Harmonik-Bead-ID\") returned ok=false")
	}

	desc := strings.ToLower(spec.Description)

	// The description must contain conditionality language: "bead", "tied", or "absent"
	// reflects the BI-018 contract that the trailer is conditional on bead binding.
	hasConditionality := strings.Contains(desc, "bead") ||
		strings.Contains(desc, "tied") ||
		strings.Contains(desc, "absent")
	if !hasConditionality {
		t.Errorf("Description %q does not mention conditionality (expected \"bead\", \"tied\", or \"absent\")", spec.Description)
	}

	// The description must cite EM-014 or BI-018 to satisfy traceability requirements.
	hasCitation := strings.Contains(desc, "em-014") || strings.Contains(desc, "bi-018")
	if !hasCitation {
		t.Errorf("Description %q does not cite EM-014 or BI-018; BI-018 requires a spec citation", spec.Description)
	}
}

// TestTrailerBI018_KnownTrailer verifies that IsKnownTrailer returns true for
// "Harmonik-Bead-ID", confirming trailer-lint will not flag it as unknown.
//
// Cites: beads-integration.md §4 BI-018.
func TestTrailerBI018_KnownTrailer(t *testing.T) {
	t.Parallel()

	if !IsKnownTrailer("Harmonik-Bead-ID") {
		t.Error("IsKnownTrailer(\"Harmonik-Bead-ID\") = false, want true; BI-018 requires the trailer to be registered")
	}
}
