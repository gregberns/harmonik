package testhelpers_test

// jsonlfixture sanity tests — verify that every JSONLFixture* function produces
// byte slices that satisfy their structural contracts (correct JSON, line count,
// newline discipline, key presence) WITHOUT invoking the production JSONL reader.
// The intent is to keep the fixtures honest: a fixture that produces malformed
// bytes in the "valid" case would corrupt the higher-level reader tests that
// depend on it.
//
// These tests are deliberately lightweight: they decode JSON with the stdlib
// decoder (not the production reader) and assert structural properties only.
//
// Spec refs: event-model.md §6.2 (on-disk JSONL format and read-recovery rules),
// §6.1 (envelope RECORD field presence), §10.2 (test-surface obligations for
// EV-001–EV-008 and EV-015–EV-020).

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/testhelpers"
)

// ---------------------------------------------------------------------------
// Helpers scoped to this file.
// ---------------------------------------------------------------------------

// countLines counts newline-terminated lines in b. A final byte sequence with
// no trailing newline is NOT counted (matches the JSONL torn-tail definition).
func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// decodeFirstLine decodes the first '\n'-terminated JSON object from b into dst.
// Returns the number of bytes consumed (including the '\n') and any error.
func decodeFirstLine(b []byte, dst *map[string]any) (int, error) {
	idx := bytes.IndexByte(b, '\n')
	if idx == -1 {
		return 0, nil // no complete line
	}
	line := b[:idx]
	if err := json.Unmarshal(line, dst); err != nil {
		return idx + 1, err
	}
	return idx + 1, nil
}

// requiredEnvelopeKeys lists the field names required by event-model.md §6.1 EV-001.
var requiredEnvelopeKeys = []string{
	"event_id",
	"schema_version",
	"type",
	"timestamp_wall",
	"source_subsystem",
	"payload",
}

// assertEnvelopeKeys fails t if any required key is absent from obj.
func assertEnvelopeKeys(t *testing.T, label string, obj map[string]any) {
	t.Helper()
	for _, k := range requiredEnvelopeKeys {
		if _, ok := obj[k]; !ok {
			t.Errorf("%s: required envelope key %q is absent (EV-001)", label, k)
		}
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureMinimalEnvelope
// ---------------------------------------------------------------------------

// TestJSONLFixtureMinimalEnvelope_OneLine verifies that the minimal-envelope
// fixture produces exactly one newline-terminated JSONL line.
func TestJSONLFixtureMinimalEnvelope_OneLine(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureMinimalEnvelope()
	if n := countLines(got); n != 1 {
		t.Errorf("JSONLFixtureMinimalEnvelope: got %d newline-terminated lines, want 1", n)
	}
}

// TestJSONLFixtureMinimalEnvelope_ValidJSON verifies the fixture line is valid JSON.
func TestJSONLFixtureMinimalEnvelope_ValidJSON(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureMinimalEnvelope()
	var obj map[string]any
	if _, err := decodeFirstLine(got, &obj); err != nil {
		t.Fatalf("JSONLFixtureMinimalEnvelope: JSON decode error: %v", err)
	}
}

// TestJSONLFixtureMinimalEnvelope_RequiredKeys verifies all EV-001 required
// envelope keys are present.
func TestJSONLFixtureMinimalEnvelope_RequiredKeys(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureMinimalEnvelope()
	var obj map[string]any
	if _, err := decodeFirstLine(got, &obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertEnvelopeKeys(t, "JSONLFixtureMinimalEnvelope", obj)
}

// TestJSONLFixtureMinimalEnvelope_OptionalFieldsAbsent verifies that optional
// fields (run_id, state_id, timestamp_mono_nsec, trace_context) are absent from
// the minimal fixture — "minimal" means only required fields.
func TestJSONLFixtureMinimalEnvelope_OptionalFieldsAbsent(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureMinimalEnvelope()
	var obj map[string]any
	if _, err := decodeFirstLine(got, &obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"run_id", "state_id", "timestamp_mono_nsec", "trace_context"} {
		if _, present := obj[k]; present {
			t.Errorf("JSONLFixtureMinimalEnvelope: optional field %q is present; minimal fixture must omit it", k)
		}
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureFullEnvelope
// ---------------------------------------------------------------------------

// TestJSONLFixtureFullEnvelope_OneLine verifies the full-envelope fixture is
// one line.
func TestJSONLFixtureFullEnvelope_OneLine(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureFullEnvelope()
	if n := countLines(got); n != 1 {
		t.Errorf("JSONLFixtureFullEnvelope: got %d lines, want 1", n)
	}
}

// TestJSONLFixtureFullEnvelope_RequiredKeys checks required keys.
func TestJSONLFixtureFullEnvelope_RequiredKeys(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureFullEnvelope()
	var obj map[string]any
	if _, err := decodeFirstLine(got, &obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertEnvelopeKeys(t, "JSONLFixtureFullEnvelope", obj)
}

// TestJSONLFixtureFullEnvelope_OptionalFieldsPresent verifies that optional
// fields are present in the full-envelope fixture.
func TestJSONLFixtureFullEnvelope_OptionalFieldsPresent(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureFullEnvelope()
	var obj map[string]any
	if _, err := decodeFirstLine(got, &obj); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"run_id", "state_id", "timestamp_mono_nsec", "trace_context"} {
		if _, present := obj[k]; !present {
			t.Errorf("JSONLFixtureFullEnvelope: optional field %q is absent; full fixture must include it", k)
		}
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureMultipleValid
// ---------------------------------------------------------------------------

// TestJSONLFixtureMultipleValid_ThreeLines verifies exactly three lines.
func TestJSONLFixtureMultipleValid_ThreeLines(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureMultipleValid()
	if n := countLines(got); n != 3 {
		t.Errorf("JSONLFixtureMultipleValid: got %d lines, want 3", n)
	}
}

// TestJSONLFixtureMultipleValid_AllLinesValidJSON verifies all three lines decode.
func TestJSONLFixtureMultipleValid_AllLinesValidJSON(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureMultipleValid()
	scanner := bytes.NewReader(got)
	dec := json.NewDecoder(scanner)
	lineNum := 0
	for dec.More() {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			t.Fatalf("JSONLFixtureMultipleValid: line %d decode error: %v", lineNum, err)
		}
		assertEnvelopeKeys(t, "line "+string(rune('0'+lineNum)), obj)
		lineNum++
	}
	if lineNum != 3 {
		t.Errorf("JSONLFixtureMultipleValid: decoded %d objects, want 3", lineNum)
	}
}

// TestJSONLFixtureMultipleValid_StrictlyIncreasingEventIDs verifies that the
// three event_id values are strictly increasing (byte-lexicographic order
// suffices for UUIDv7; they share the same ms prefix and differ only in the
// trailing 6 bytes).
func TestJSONLFixtureMultipleValid_StrictlyIncreasingEventIDs(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureMultipleValid()
	var ids []string
	scanner := bytes.NewReader(got)
	dec := json.NewDecoder(scanner)
	for dec.More() {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			t.Fatalf("decode: %v", err)
		}
		id, _ := obj["event_id"].(string)
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i-1] >= ids[i] {
			t.Errorf("event_id[%d]=%q >= event_id[%d]=%q; must be strictly increasing (EV-002a)",
				i-1, ids[i-1], i, ids[i])
		}
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureDurabilityClasses
// ---------------------------------------------------------------------------

// TestJSONLFixtureDurabilityClasses_ThreeEntries verifies three entries are returned.
func TestJSONLFixtureDurabilityClasses_ThreeEntries(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureDurabilityClasses()
	if len(got) != 3 {
		t.Errorf("JSONLFixtureDurabilityClasses: got %d entries, want 3", len(got))
	}
}

// TestJSONLFixtureDurabilityClasses_AllClassesRepresented verifies that all
// three durability class constants appear exactly once.
func TestJSONLFixtureDurabilityClasses_AllClassesRepresented(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureDurabilityClasses()
	seen := make(map[testhelpers.DurabilityClass]int)
	for _, dcl := range got {
		seen[dcl.Class]++
	}
	for _, wantClass := range []testhelpers.DurabilityClass{
		testhelpers.DurabilityFsyncBoundary,
		testhelpers.DurabilityOrdinary,
		testhelpers.DurabilityLossyTailOk,
	} {
		if seen[wantClass] != 1 {
			t.Errorf("JSONLFixtureDurabilityClasses: class %q appears %d times, want 1",
				wantClass, seen[wantClass])
		}
	}
}

// TestJSONLFixtureDurabilityClasses_EachLineIsValidJSON verifies each line decodes.
func TestJSONLFixtureDurabilityClasses_EachLineIsValidJSON(t *testing.T) {
	t.Parallel()

	for _, dcl := range testhelpers.JSONLFixtureDurabilityClasses() {
		dcl := dcl
		t.Run(string(dcl.Class), func(t *testing.T) {
			t.Parallel()
			var obj map[string]any
			if _, err := decodeFirstLine(dcl.Line, &obj); err != nil {
				t.Fatalf("class %q: JSON decode error: %v", dcl.Class, err)
			}
			assertEnvelopeKeys(t, string(dcl.Class), obj)
		})
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureTornTail
// ---------------------------------------------------------------------------

// TestJSONLFixtureTornTail_ThreeVariants verifies three variants are returned.
func TestJSONLFixtureTornTail_ThreeVariants(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureTornTail()
	if len(got) != 3 {
		t.Errorf("JSONLFixtureTornTail: got %d variants, want 3", len(got))
	}
}

// TestJSONLFixtureTornTail_ValidLinesBefore verifies each variant has the
// declared number of valid preceding lines (ValidLineCount == 1).
func TestJSONLFixtureTornTail_ValidLinesBefore(t *testing.T) {
	t.Parallel()

	for _, fix := range testhelpers.JSONLFixtureTornTail() {
		fix := fix
		t.Run(string(fix.Kind), func(t *testing.T) {
			t.Parallel()
			if fix.ValidLineCount != 1 {
				t.Errorf("kind %q: ValidLineCount = %d, want 1", fix.Kind, fix.ValidLineCount)
			}
			// Verify first line is valid JSON.
			idx := bytes.IndexByte(fix.JSONL, '\n')
			if idx == -1 {
				t.Fatalf("kind %q: no newline found; fixture has no complete first line", fix.Kind)
			}
			var obj map[string]any
			if err := json.Unmarshal(fix.JSONL[:idx], &obj); err != nil {
				t.Fatalf("kind %q: first line JSON decode error: %v", fix.Kind, err)
			}
			assertEnvelopeKeys(t, string(fix.Kind)+" first-line", obj)
		})
	}
}

// TestJSONLFixtureTornTail_MissingNewline verifies the MissingNewline variant
// ends without a trailing '\n'.
func TestJSONLFixtureTornTail_MissingNewline(t *testing.T) {
	t.Parallel()

	for _, fix := range testhelpers.JSONLFixtureTornTail() {
		if fix.Kind != testhelpers.TornTailMissingNewline {
			continue
		}
		if len(fix.JSONL) == 0 {
			t.Fatal("TornTailMissingNewline: JSONL is empty")
		}
		if fix.JSONL[len(fix.JSONL)-1] == '\n' {
			t.Error("TornTailMissingNewline: JSONL ends with '\\n'; want no trailing newline on the final line")
		}
	}
}

// TestJSONLFixtureTornTail_BadJSON verifies the BadJSON variant's second
// portion (after the first newline) fails JSON parse.
func TestJSONLFixtureTornTail_BadJSON(t *testing.T) {
	t.Parallel()

	for _, fix := range testhelpers.JSONLFixtureTornTail() {
		if fix.Kind != testhelpers.TornTailBadJSON {
			continue
		}
		idx := bytes.IndexByte(fix.JSONL, '\n')
		if idx == -1 || idx+1 >= len(fix.JSONL) {
			t.Fatal("TornTailBadJSON: no bytes after first newline")
		}
		tail := fix.JSONL[idx+1:]
		// Remove trailing newline if any before checking.
		tail = bytes.TrimRight(tail, "\n")
		var obj map[string]any
		if err := json.Unmarshal(tail, &obj); err == nil {
			t.Error("TornTailBadJSON: final portion decoded as valid JSON; want parse error")
		}
	}
}

// TestJSONLFixtureTornTail_BadEnvelope verifies the BadEnvelope variant's final
// line is valid JSON but lacks "event_id" (required by EV-001).
func TestJSONLFixtureTornTail_BadEnvelope(t *testing.T) {
	t.Parallel()

	for _, fix := range testhelpers.JSONLFixtureTornTail() {
		if fix.Kind != testhelpers.TornTailBadEnvelope {
			continue
		}
		// Locate the second newline.
		first := bytes.IndexByte(fix.JSONL, '\n')
		if first == -1 {
			t.Fatal("TornTailBadEnvelope: no first newline")
		}
		rest := fix.JSONL[first+1:]
		second := bytes.IndexByte(rest, '\n')
		var finalLine []byte
		if second != -1 {
			finalLine = rest[:second]
		} else {
			finalLine = rest
		}
		finalLine = bytes.TrimRight(finalLine, "\n")
		var obj map[string]any
		if err := json.Unmarshal(finalLine, &obj); err != nil {
			t.Fatalf("TornTailBadEnvelope: final line is not valid JSON (want JSON with missing field): %v", err)
		}
		if _, ok := obj["event_id"]; ok {
			t.Error("TornTailBadEnvelope: final line contains \"event_id\"; want it absent (envelope schema failure per EV-001)")
		}
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureMidFileCorruption
// ---------------------------------------------------------------------------

// TestJSONLFixtureMidFileCorruption_Layout verifies the fixture has the correct
// line layout: valid, corrupt, valid.
func TestJSONLFixtureMidFileCorruption_Layout(t *testing.T) {
	t.Parallel()

	fix := testhelpers.JSONLFixtureMidFileCorruption()

	if fix.ValidLinesBefore != 1 {
		t.Errorf("ValidLinesBefore = %d, want 1", fix.ValidLinesBefore)
	}
	if fix.ValidLinesAfter != 1 {
		t.Errorf("ValidLinesAfter = %d, want 1", fix.ValidLinesAfter)
	}
	if fix.CorruptByteOffset <= 0 {
		t.Errorf("CorruptByteOffset = %d, want > 0", fix.CorruptByteOffset)
	}
	if len(fix.JSONL) == 0 {
		t.Fatal("JSONL is empty")
	}
}

// TestJSONLFixtureMidFileCorruption_CorruptLineFailsJSON verifies the line at
// CorruptByteOffset fails JSON parse.
func TestJSONLFixtureMidFileCorruption_CorruptLineFailsJSON(t *testing.T) {
	t.Parallel()

	fix := testhelpers.JSONLFixtureMidFileCorruption()
	tail := fix.JSONL[fix.CorruptByteOffset:]
	idx := bytes.IndexByte(tail, '\n')
	var corruptLine []byte
	if idx != -1 {
		corruptLine = tail[:idx]
	} else {
		corruptLine = tail
	}
	var obj map[string]any
	if err := json.Unmarshal(corruptLine, &obj); err == nil {
		t.Error("corrupt line decoded as valid JSON; want parse error")
	}
}

// TestJSONLFixtureMidFileCorruption_OffsetMatchesActual verifies that
// CorruptByteOffset points to the start of the second line (immediately after
// the first '\n').
func TestJSONLFixtureMidFileCorruption_OffsetMatchesActual(t *testing.T) {
	t.Parallel()

	fix := testhelpers.JSONLFixtureMidFileCorruption()
	firstNL := bytes.IndexByte(fix.JSONL, '\n')
	if firstNL == -1 {
		t.Fatal("no newline in JSONL fixture")
	}
	if fix.CorruptByteOffset != firstNL+1 {
		t.Errorf("CorruptByteOffset = %d, want %d (byte after first newline)",
			fix.CorruptByteOffset, firstNL+1)
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureEmptyLog
// ---------------------------------------------------------------------------

// TestJSONLFixtureEmptyLog_TwoVariants verifies two variants are returned.
func TestJSONLFixtureEmptyLog_TwoVariants(t *testing.T) {
	t.Parallel()

	got := testhelpers.JSONLFixtureEmptyLog()
	if len(got) != 2 {
		t.Errorf("JSONLFixtureEmptyLog: got %d variants, want 2", len(got))
	}
}

// TestJSONLFixtureEmptyLog_FreshProject verifies the fresh-project variant has
// nil JSONL and no prior-cycle evidence.
func TestJSONLFixtureEmptyLog_FreshProject(t *testing.T) {
	t.Parallel()

	for _, fix := range testhelpers.JSONLFixtureEmptyLog() {
		if fix.Kind != testhelpers.EmptyLogFreshProject {
			continue
		}
		if fix.JSONL != nil {
			t.Errorf("FreshProject: JSONL is non-nil (%d bytes), want nil (file absent)", len(fix.JSONL))
		}
		if fix.HasPriorCycleEvidence {
			t.Error("FreshProject: HasPriorCycleEvidence = true, want false")
		}
	}
}

// TestJSONLFixtureEmptyLog_PriorCycle verifies the prior-cycle variant has an
// empty (non-nil) byte slice and HasPriorCycleEvidence == true.
func TestJSONLFixtureEmptyLog_PriorCycle(t *testing.T) {
	t.Parallel()

	for _, fix := range testhelpers.JSONLFixtureEmptyLog() {
		if fix.Kind != testhelpers.EmptyLogWithPriorCycle {
			continue
		}
		if fix.JSONL == nil {
			t.Error("WithPriorCycle: JSONL is nil; want empty non-nil slice (file present, zero length)")
		}
		if len(fix.JSONL) != 0 {
			t.Errorf("WithPriorCycle: JSONL has %d bytes, want 0 (empty file)", len(fix.JSONL))
		}
		if !fix.HasPriorCycleEvidence {
			t.Error("WithPriorCycle: HasPriorCycleEvidence = false, want true")
		}
	}
}

// ---------------------------------------------------------------------------
// JSONLFixtureConcurrentTail
// ---------------------------------------------------------------------------

// TestJSONLFixtureConcurrentTail_TwoCompletedLines verifies two complete lines.
func TestJSONLFixtureConcurrentTail_TwoCompletedLines(t *testing.T) {
	t.Parallel()

	fix := testhelpers.JSONLFixtureConcurrentTail()
	if fix.CompletedLines != 2 {
		t.Errorf("CompletedLines = %d, want 2", fix.CompletedLines)
	}
	// Count actual newlines.
	if n := countLines(fix.JSONL); n != 2 {
		t.Errorf("JSONL contains %d newline-terminated lines, want 2", n)
	}
}

// TestJSONLFixtureConcurrentTail_PartialLineHasNoNewline verifies the in-progress
// bytes do not end with '\n' (they are non-authoritative per §6.2).
func TestJSONLFixtureConcurrentTail_PartialLineHasNoNewline(t *testing.T) {
	t.Parallel()

	fix := testhelpers.JSONLFixtureConcurrentTail()
	if len(fix.PartialLineBytes) == 0 {
		t.Fatal("PartialLineBytes is empty")
	}
	if fix.PartialLineBytes[len(fix.PartialLineBytes)-1] == '\n' {
		t.Error("PartialLineBytes ends with '\\n'; concurrent-tail partial must not be newline-terminated")
	}
}

// TestJSONLFixtureConcurrentTail_CompletedLinesDecodeOK verifies the two
// complete lines are valid JSON with required envelope keys.
func TestJSONLFixtureConcurrentTail_CompletedLinesDecodeOK(t *testing.T) {
	t.Parallel()

	fix := testhelpers.JSONLFixtureConcurrentTail()
	remaining := fix.JSONL
	for i := 0; i < fix.CompletedLines; i++ {
		var obj map[string]any
		n, err := decodeFirstLine(remaining, &obj)
		if err != nil {
			t.Fatalf("line %d: decode error: %v", i, err)
		}
		if n == 0 {
			t.Fatalf("line %d: no complete line found", i)
		}
		assertEnvelopeKeys(t, "concurrent-tail line "+string(rune('0'+i)), obj)
		remaining = remaining[n:]
	}
}
