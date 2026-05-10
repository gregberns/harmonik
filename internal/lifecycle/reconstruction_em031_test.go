package lifecycle

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/testhelpers"
)

// --- EM-031 sensor: state reconstruction uses git + Beads only ---
//
// The tests below assert the two complementary halves of EM-031:
//
//  1. Structural: DiscoverActiveRuns accepts only BeadsQuerier and BranchTipReader
//     parameters — there is no JSONL input path. A compile error in this file
//     would signal a path-leakage regression.
//
//  2. Behavioral: DiscoverActiveRuns returns a valid ActiveRunSet from a
//     fully-populated git + Beads context even when no JSONL file exists on disk.
//     This is the "JSONL-not-walked-for-state" sensor for EM-031.
//
// Spec ref: execution-model.md §4.7 EM-031 — "The JSONL event log MUST NOT be
// walked to reconstruct state."

// reconstructFixtureCompileTimeCheck is a compile-time assertion that
// DiscoverActiveRuns does not accept a JSONL reader parameter. If the function
// signature ever gains a JSONL source, this file will fail to compile because
// the test call sites below do not pass one.
//
// This is intentionally a documentation comment on a blank identifier — the
// actual enforcement is the set of test call sites using only (ctx, querier, reader).
var _ = DiscoverActiveRuns // function must remain (ctx, BeadsQuerier, BranchTipReader)

// TestEM031_ReconstructionUsesOnlyGitAndBeads verifies that DiscoverActiveRuns
// produces a correct ActiveRunSet when provided only git branch tips (via
// BranchTipReader) and Beads records (via BeadsQuerier), with no JSONL file
// on disk. This is the behavioral "JSONL-not-walked-for-state" sensor.
//
// Spec ref: execution-model.md §4.7 EM-031 — "The JSONL event log MUST NOT
// be walked to reconstruct state."
func TestEM031_ReconstructionUsesOnlyGitAndBeads(t *testing.T) {
	t.Parallel()

	// Two in-flight runs: one with a task branch (git source), one Beads-only.
	runID := reconstructFixtureRunID(1)
	const beadOnlyID = "hk-em031.1"

	// Querier: one non-terminal bead (the Beads-only run).
	querier := &activeRunDiscoveryFixtureFakeQuerier{
		statusMap: map[string][]core.BeadRecord{
			"open": {activeRunDiscoveryFixtureBeadRecord(beadOnlyID, core.CoarseStatusOpen)},
		},
	}

	// Reader: one task branch with a valid Harmonik-Run-ID trailer.
	reader := activeRunDiscoveryFixtureReaderWithTips(BranchTip{
		BranchName: "run/" + runID,
		RunID:      runID,
		BeadID:     "",
	})

	// DiscoverActiveRuns must succeed with only these two sources.
	// No JSONL path is available or needed.
	set, err := DiscoverActiveRuns(t.Context(), querier, reader)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	// Expect two entries: one from the branch scan, one from Beads.
	if set.Len() != 2 {
		t.Errorf("ActiveRunSet.Len() = %d; want 2 (one git-branch run + one Beads-only run)", set.Len())
	}
}

// TestEM031_ReconstructionEmptyGitAndBeads verifies that DiscoverActiveRuns
// returns an empty ActiveRunSet — not an error — when both sources are empty.
// An empty set means no in-flight runs; no JSONL consultation is needed or
// attempted.
//
// Spec ref: execution-model.md §4.7 EM-031.
func TestEM031_ReconstructionEmptyGitAndBeads(t *testing.T) {
	t.Parallel()

	set, err := DiscoverActiveRuns(
		t.Context(),
		activeRunDiscoveryFixtureEmptyQuerier(),
		activeRunDiscoveryFixtureEmptyReader(),
	)
	if err != nil {
		t.Fatalf("DiscoverActiveRuns: unexpected error: %v", err)
	}
	if set.Len() != 0 {
		t.Errorf("ActiveRunSet.Len() = %d; want 0", set.Len())
	}
}

// --- EM-031 sensor: JSONL torn-tail tolerance ---
//
// The tests below exercise ReadJSONLForDivergenceEvidence, the divergence-
// evidence JSONL reader, against each torn-tail variant defined in EM-031 and
// the testhelpers.JSONLFixtureTornTail fixture set.
//
// EM-031 specifies three cases:
//  1. Torn tail (unparseable final line, no terminating newline) → discard,
//     return valid preceding lines, NO Cat 6b signal.
//  2. Mid-file corruption → ErrJSONLMidFileCorruption (Cat 6b signal).
//  3. Terminated bad tail (unparseable final line WITH a newline) → Cat 6b.
//
// Spec ref: execution-model.md §4.7 EM-031 — torn-tail tolerance paragraph.

// TestEM031_TornTail_MissingNewline verifies that a final line containing
// valid JSON but lacking a terminating newline is returned as a valid line.
//
// The EM-031 torn-tail discard rule applies only when the final line is
// UNPARSEABLE ("if the final line of a JSONL file is unparseable AND is not
// terminated by a newline"). A valid-JSON final line that happens to be
// unterminated is not unparseable — the reader returns it normally.
//
// This test verifies that the TornTailMissingNewline fixture (valid JSON, no
// final "\n") produces both lines (original valid line + unterminated valid
// final line), NOT just 1.
//
// Spec ref: execution-model.md §4.7 EM-031 — torn-tail tolerance condition
// is "unparseable AND not terminated by a newline" (not merely unterminated).
func TestEM031_TornTail_MissingNewline(t *testing.T) {
	t.Parallel()

	var fixture testhelpers.TornTailFixture
	for _, f := range testhelpers.JSONLFixtureTornTail() {
		if f.Kind == testhelpers.TornTailMissingNewline {
			fixture = f
			break
		}
	}
	if fixture.JSONL == nil {
		t.Fatal("TornTailMissingNewline fixture not found")
	}

	results, err := ReadJSONLForDivergenceEvidence(fixture.JSONL)
	if err != nil {
		t.Fatalf("ReadJSONLForDivergenceEvidence: unexpected error for missing-newline (parseable JSON): %v", err)
	}
	// The MissingNewline fixture has 1 valid preceding line + 1 valid unterminated final line.
	// Both should be returned because the final line IS parseable JSON (not a torn tail per EM-031).
	wantLines := fixture.ValidLineCount + 1 // valid preceding + valid unterminated final
	if len(results) != wantLines {
		t.Errorf("len(results) = %d; want %d (parseable unterminated final line is returned, not discarded)",
			len(results), wantLines)
	}
}

// TestEM031_TornTail_BadJSON verifies that a final line containing truncated
// (non-parseable) JSON without a terminating newline is silently discarded,
// and the preceding valid line is returned without error.
//
// Spec ref: execution-model.md §4.7 EM-031 — torn-tail discard rule.
func TestEM031_TornTail_BadJSON(t *testing.T) {
	t.Parallel()

	var fixture testhelpers.TornTailFixture
	for _, f := range testhelpers.JSONLFixtureTornTail() {
		if f.Kind == testhelpers.TornTailBadJSON {
			fixture = f
			break
		}
	}
	if fixture.JSONL == nil {
		t.Fatal("TornTailBadJSON fixture not found")
	}

	results, err := ReadJSONLForDivergenceEvidence(fixture.JSONL)
	if err != nil {
		t.Fatalf("ReadJSONLForDivergenceEvidence: unexpected error for torn tail (BadJSON): %v", err)
	}
	if len(results) != fixture.ValidLineCount {
		t.Errorf("len(results) = %d; want %d (torn bad-JSON line discarded)",
			len(results), fixture.ValidLineCount)
	}
}

// TestEM031_TornTail_TerminatedBadLine verifies that a final line containing
// invalid JSON WITH a terminating newline (the TornTailBadEnvelope fixture,
// whose final line is newline-terminated per the fixture implementation) returns
// ErrJSONLMidFileCorruption — a Cat 6b signal.
//
// Note: the testhelpers.TornTailBadEnvelope fixture has valid JSON but fails
// envelope schema validation; however, ReadJSONLForDivergenceEvidence performs
// only json.Valid() checks at the syntax level. The BadEnvelope fixture passes
// json.Valid() (it IS valid JSON), so we construct a terminated-bad-JSON line
// directly to exercise the Cat 6b path.
//
// Spec ref: execution-model.md §4.7 EM-031 — "An unparseable line anywhere
// but at the tail, or an unparseable tail line terminated by a newline, IS a
// Cat 6b signal."
func TestEM031_TornTail_TerminatedBadLine_IsCat6b(t *testing.T) {
	t.Parallel()

	// Build a JSONL with one valid line + a second invalid (truncated) line that
	// IS newline-terminated. This is NOT a torn tail — it is a terminated bad line.
	data := bytes.Join([][]byte{
		[]byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000001","schema_version":1}` + "\n"),
		[]byte(`{"event_id":"BAD` + "\n"), // truncated but has newline
	}, nil)

	_, err := ReadJSONLForDivergenceEvidence(data)
	if err == nil {
		t.Fatal("expected ErrJSONLMidFileCorruption for terminated bad tail, got nil")
	}
	if !errors.Is(err, ErrJSONLMidFileCorruption) {
		t.Errorf("errors.Is(err, ErrJSONLMidFileCorruption) = false; got: %v", err)
	}
}

// TestEM031_MidFileCorruption_IsCat6b verifies that a corrupt (unparseable)
// line appearing before the final line returns ErrJSONLMidFileCorruption.
//
// Spec ref: execution-model.md §4.7 EM-031 — "An unparseable line anywhere
// but at the tail … IS a Cat 6b signal."
func TestEM031_MidFileCorruption_IsCat6b(t *testing.T) {
	t.Parallel()

	// Three lines: valid, corrupt mid-file, valid trailing.
	data := bytes.Join([][]byte{
		[]byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000001","schema_version":1}` + "\n"),
		[]byte(`{CORRUPT` + "\n"),
		[]byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000003","schema_version":1}` + "\n"),
	}, nil)

	_, err := ReadJSONLForDivergenceEvidence(data)
	if err == nil {
		t.Fatal("expected ErrJSONLMidFileCorruption for mid-file corrupt line, got nil")
	}
	if !errors.Is(err, ErrJSONLMidFileCorruption) {
		t.Errorf("errors.Is(err, ErrJSONLMidFileCorruption) = false; got: %v", err)
	}
}

// TestEM031_EmptyJSONL verifies that ReadJSONLForDivergenceEvidence returns
// nil results and nil error for an empty input. Empty JSONL is not corruption.
//
// Spec ref: execution-model.md §4.7 EM-031.
func TestEM031_EmptyJSONL_ReturnsNoResults(t *testing.T) {
	t.Parallel()

	results, err := ReadJSONLForDivergenceEvidence(nil)
	if err != nil {
		t.Fatalf("unexpected error for nil input: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d; want 0 for nil input", len(results))
	}

	results, err = ReadJSONLForDivergenceEvidence([]byte{})
	if err != nil {
		t.Fatalf("unexpected error for empty input: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d; want 0 for empty input", len(results))
	}
}

// TestEM031_ValidJSONL_AllLinesReturned verifies that a well-formed multi-line
// JSONL input (all lines terminated by newline) returns all lines without error.
//
// Spec ref: execution-model.md §4.7 EM-031.
func TestEM031_ValidJSONL_AllLinesReturned(t *testing.T) {
	t.Parallel()

	data := bytes.Join([][]byte{
		[]byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000001","schema_version":1}` + "\n"),
		[]byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000002","schema_version":1}` + "\n"),
		[]byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000003","schema_version":1}` + "\n"),
	}, nil)

	results, err := ReadJSONLForDivergenceEvidence(data)
	if err != nil {
		t.Fatalf("ReadJSONLForDivergenceEvidence: unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("len(results) = %d; want 3", len(results))
	}
	// Verify line numbers are sequential.
	for i, r := range results {
		if r.LineNumber != i+1 {
			t.Errorf("results[%d].LineNumber = %d; want %d", i, r.LineNumber, i+1)
		}
	}
}

// TestEM031_SingleTornTail_OnlyValidLine verifies that a JSONL consisting of
// exactly one valid line followed by a torn tail returns the single valid line
// and no error.
//
// Spec ref: execution-model.md §4.7 EM-031 — torn-tail discard rule.
func TestEM031_SingleTornTail_OnlyValidLine(t *testing.T) {
	t.Parallel()

	data := bytes.Join([][]byte{
		[]byte(`{"event_id":"0196e3d1-3af8-7000-8000-000000000001","schema_version":1}` + "\n"),
		[]byte(`{"event_id":"TRUNC`), // no newline, bad JSON (torn tail)
	}, nil)

	results, err := ReadJSONLForDivergenceEvidence(data)
	if err != nil {
		t.Fatalf("ReadJSONLForDivergenceEvidence: unexpected error for single torn tail: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d; want 1 (torn tail discarded, valid line returned)", len(results))
	}
}

// --- Fixture helpers (reconstructFixture prefix per hk-b3f.39 discipline) ---

// reconstructFixtureRunID returns a deterministic UUIDv7-shaped string for use
// in EM-031 reconstruction tests. Uses the same format as sibling bead helpers.
func reconstructFixtureRunID(n int) string {
	return activeRunDiscoveryFixtureRunID(n + 100) // offset from EM-031a range
}
