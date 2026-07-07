package workspace

// reviewverdict_parseonly_hk420yr4_test.go — B1a subsystem-proof:
// parseReviewVerdict happy-path acceptance.
//
// All tests call parseReviewVerdict directly with inline byte literals —
// no temp dirs, no file I/O, no daemon. Proves every enum value round-trips
// through the parser with all fields intact.
//
// Bead: hk-420yr.10 (subsystem-proofs B1a redo, re-land after false-close).
// Spec ref: workspace-model.md §4.7.WM-027a; event-model.md §8.1a.3.

import (
	"testing"
)

// parseVerdictFixtureJSON builds a minimal valid JSON blob with the given verdict string.
func parseVerdictFixtureJSON(verdict string) []byte {
	return []byte(`{"schema_version":1,"verdict":"` + verdict + `","flags":[],"notes":"Subsystem-proof fixture."}`)
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy-path: all three verdict enum values
// ─────────────────────────────────────────────────────────────────────────────

// TestParseReviewVerdict_B1a_Approve proves APPROVE round-trips via parseReviewVerdict.
func TestParseReviewVerdict_B1a_Approve(t *testing.T) {
	t.Parallel()
	v, err := parseReviewVerdict(parseVerdictFixtureJSON(ReviewVerdictApprove), "test-target")
	if err != nil {
		t.Fatalf("parseReviewVerdict(APPROVE) = %v, want nil", err)
	}
	if v.Verdict != ReviewVerdictApprove {
		t.Errorf("Verdict = %q; want %q", v.Verdict, ReviewVerdictApprove)
	}
	if v.SchemaVersion != ReviewVerdictSchemaVersion {
		t.Errorf("SchemaVersion = %d; want %d", v.SchemaVersion, ReviewVerdictSchemaVersion)
	}
	if v.Notes == "" {
		t.Error("Notes is empty; want non-empty")
	}
	if v.Flags == nil {
		t.Error("Flags is nil; want non-nil slice")
	}
}

// TestParseReviewVerdict_B1a_RequestChanges proves REQUEST_CHANGES round-trips.
func TestParseReviewVerdict_B1a_RequestChanges(t *testing.T) {
	t.Parallel()
	data := []byte(`{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["missing-tests"],"notes":"Add tests for the new path."}`)
	v, err := parseReviewVerdict(data, "test-target")
	if err != nil {
		t.Fatalf("parseReviewVerdict(REQUEST_CHANGES) = %v, want nil", err)
	}
	if v.Verdict != ReviewVerdictRequestChanges {
		t.Errorf("Verdict = %q; want %q", v.Verdict, ReviewVerdictRequestChanges)
	}
	if len(v.Flags) != 1 || v.Flags[0] != "missing-tests" {
		t.Errorf("Flags = %v; want [missing-tests]", v.Flags)
	}
}

// TestParseReviewVerdict_B1a_Block proves BLOCK round-trips.
func TestParseReviewVerdict_B1a_Block(t *testing.T) {
	t.Parallel()
	data := []byte(`{"schema_version":1,"verdict":"BLOCK","flags":["spec-violation"],"notes":"Changes violate the spec contract."}`)
	v, err := parseReviewVerdict(data, "test-target")
	if err != nil {
		t.Fatalf("parseReviewVerdict(BLOCK) = %v, want nil", err)
	}
	if v.Verdict != ReviewVerdictBlock {
		t.Errorf("Verdict = %q; want %q", v.Verdict, ReviewVerdictBlock)
	}
}

// TestParseReviewVerdict_B1a_AllVerdicts proves all three enum values round-trip.
func TestParseReviewVerdict_B1a_AllVerdicts(t *testing.T) {
	t.Parallel()
	cases := []string{
		ReviewVerdictApprove,
		ReviewVerdictRequestChanges,
		ReviewVerdictBlock,
	}
	for _, wantVerdict := range cases {
		wantVerdict := wantVerdict
		t.Run(wantVerdict, func(t *testing.T) {
			t.Parallel()
			v, err := parseReviewVerdict(parseVerdictFixtureJSON(wantVerdict), "test-target")
			if err != nil {
				t.Fatalf("parseReviewVerdict(%q) = %v, want nil", wantVerdict, err)
			}
			if v.Verdict != wantVerdict {
				t.Errorf("Verdict = %q; want %q", v.Verdict, wantVerdict)
			}
			if v.SchemaVersion != ReviewVerdictSchemaVersion {
				t.Errorf("SchemaVersion = %d; want %d", v.SchemaVersion, ReviewVerdictSchemaVersion)
			}
			if v.Notes == "" {
				t.Error("Notes is empty; want non-empty")
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy-path: flags variations
// ─────────────────────────────────────────────────────────────────────────────

// TestParseReviewVerdict_B1a_NullFlagsBecomesEmptySlice proves null flags JSON
// is coerced to a non-nil empty slice (not nil) per WM-027a.
func TestParseReviewVerdict_B1a_NullFlagsBecomesEmptySlice(t *testing.T) {
	t.Parallel()
	data := []byte(`{"schema_version":1,"verdict":"APPROVE","flags":null,"notes":"Null flags test."}`)
	v, err := parseReviewVerdict(data, "test-target")
	if err != nil {
		t.Fatalf("parseReviewVerdict(null flags) = %v, want nil", err)
	}
	if v.Flags == nil {
		t.Error("Flags is nil after null JSON value; want non-nil empty slice")
	}
	if len(v.Flags) != 0 {
		t.Errorf("Flags len = %d; want 0", len(v.Flags))
	}
}

// TestParseReviewVerdict_B1a_MultipleFlags proves a multi-element flags array
// round-trips without loss.
func TestParseReviewVerdict_B1a_MultipleFlags(t *testing.T) {
	t.Parallel()
	data := []byte(`{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":["correctness","simplification","test-coverage"],"notes":"Three flags."}`)
	v, err := parseReviewVerdict(data, "test-target")
	if err != nil {
		t.Fatalf("parseReviewVerdict(multiple flags) = %v, want nil", err)
	}
	if len(v.Flags) != 3 {
		t.Errorf("Flags = %v; want 3 entries", v.Flags)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Happy-path: field integrity
// ─────────────────────────────────────────────────────────────────────────────

// TestParseReviewVerdict_B1a_NotesPreserved proves the notes field is returned
// verbatim (no truncation or transformation).
func TestParseReviewVerdict_B1a_NotesPreserved(t *testing.T) {
	t.Parallel()
	wantNotes := "All changes are correct and tests pass. No spec drift detected. Approved."
	data := []byte(`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"` + wantNotes + `"}`)
	v, err := parseReviewVerdict(data, "test-target")
	if err != nil {
		t.Fatalf("parseReviewVerdict = %v, want nil", err)
	}
	if v.Notes != wantNotes {
		t.Errorf("Notes = %q; want %q", v.Notes, wantNotes)
	}
}

// TestParseReviewVerdict_B1a_SchemaVersionField proves the schema_version field
// is returned as ReviewVerdictSchemaVersion (1).
func TestParseReviewVerdict_B1a_SchemaVersionField(t *testing.T) {
	t.Parallel()
	data := []byte(`{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"Version check."}`)
	v, err := parseReviewVerdict(data, "test-target")
	if err != nil {
		t.Fatalf("parseReviewVerdict = %v, want nil", err)
	}
	if v.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d; want 1", v.SchemaVersion)
	}
	if v.SchemaVersion != ReviewVerdictSchemaVersion {
		t.Errorf("SchemaVersion constant mismatch: got %d, const = %d", v.SchemaVersion, ReviewVerdictSchemaVersion)
	}
}
