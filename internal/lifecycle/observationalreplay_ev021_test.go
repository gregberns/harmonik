// Package lifecycle — EV-021 observational-replay-must-not-reconstruct-state sensor.
//
// EV-021 (event-model.md §4.5 EV-021): "Any tool that walks JSONL for
// debugging, pattern analysis, or dashboard purposes is performing
// observational replay. Output is advisory only. Authoritative
// state-reconstruction source is git plus Beads per [execution-model.md §4.7]."
//
// Observational replay is explicitly NOT state reconstruction. A reader that
// walks JSONL for divergence evidence (ReadJSONLForDivergenceEvidence) is
// performing observational replay; its result MUST be treated as advisory,
// never used to reconstitute durable state.
//
// This file is the documentation/discipline sensor for hk-hqwn.30. The
// runtime enforcement layer (a guard that rejects attempts to use JSONL
// results as authoritative state) is not yet built; that behavioral sensor
// will land on top of a future divergence-consumer layer in a later bead.
//
// Invariant locked by EV-021:
//
//	observational replay output is advisory only
//	→  ANY tool walking JSONL for debugging, pattern analysis, or dashboard is observational replay
//	→  MUST NOT reconstruct durable state from JSONL
//	→  authoritative state-reconstruction source is git plus Beads
//
// The tests below assert:
//  1. specs/event-model.md encodes EV-021 with the required canonical phrases.
//  2. The ReadJSONLForDivergenceEvidence godoc in jsonldivergence_em031.go
//     carries the EV-021 advisory constraint so it is visible at the call site.
//
// When a consumer-side enforcement layer is implemented, that bead SHOULD either:
//   - Delete the forward-doc marker (TestObservationalReplay_EV021_ForwardDocSensor)
//     and replace it with concrete assertions against the enforcement layer, OR
//   - Extend it with those assertions, retaining the EV-021 citation and
//     hk-hqwn.30 traceability.
//
// Requirement-traceable bead: hk-hqwn.30.
package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// replayNotReconstructFixtureSpecContent reads specs/event-model.md, locates
// the EV-021 anchor, and returns the paragraph that contains it. It fails the
// test if the file is unreadable or the anchor is missing.
func replayNotReconstructFixtureSpecContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repo root")
	}
	// Walk up: internal/lifecycle/<file> → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "event-model.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "EV-021 — Observational replay MUST NOT reconstruct state"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; EV-021 may have been removed or renamed", specPath, anchor)
	}

	// Return the paragraph from the anchor up to the next section boundary.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// replayNotReconstructFixtureDivergenceGoContent reads
// internal/lifecycle/jsonldivergence_em031.go and returns the godoc block for
// ReadJSONLForDivergenceEvidence. It fails the test if the file is unreadable
// or the function declaration is absent.
func replayNotReconstructFixtureDivergenceGoContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate jsonldivergence_em031.go")
	}
	srcPath := filepath.Join(filepath.Dir(thisFile), "jsonldivergence_em031.go")

	raw, err := os.ReadFile(srcPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", srcPath, err)
	}

	// Find the function declaration line.
	lines := strings.Split(string(raw), "\n")
	funcLineIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "func ReadJSONLForDivergenceEvidence(") {
			funcLineIdx = i
			break
		}
	}
	if funcLineIdx < 0 {
		t.Fatal("jsonldivergence_em031.go does not contain ReadJSONLForDivergenceEvidence declaration")
	}

	// Walk backwards from the function declaration to collect the comment block.
	var commentLines []string
	for i := funcLineIdx - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "//") {
			commentLines = append([]string{line}, commentLines...)
		} else {
			break
		}
	}
	return strings.Join(commentLines, "\n")
}

// TestObservationalReplay_EV021_SpecContainsAdvisoryInvariant verifies that
// the EV-021 section of specs/event-model.md encodes the advisory invariant
// with the required canonical phrases.
//
// Phrases required by the invariant (EV-021, hk-hqwn.30):
//
//   - "advisory only"             — the output classification; must be explicit
//   - "observational replay"      — the named category of JSONL walk
//   - "git plus Beads"            — the authoritative state-reconstruction source
//   - "MUST NOT"                  — the normative prohibition; advisory language is not sufficient
//
// A future rename of any of these phrases in the spec is a breaking change and
// MUST be accompanied by a corresponding update to this test.
func TestObservationalReplay_EV021_SpecContainsAdvisoryInvariant(t *testing.T) {
	t.Parallel()

	para := replayNotReconstructFixtureSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "advisory only",
			hint:   "EV-021 must explicitly classify observational replay output as advisory only; removing this phrase weakens the EV-021 contract",
		},
		{
			phrase: "observational replay",
			hint:   "EV-021 must name 'observational replay' as the category of JSONL walk; this is the hk-hqwn.30 invariant boundary",
		},
		{
			phrase: "git plus Beads",
			hint:   "EV-021 must name git plus Beads as the authoritative state-reconstruction source; consumers need the redirect target",
		},
		{
			phrase: "MUST NOT",
			hint:   "EV-021 must use normative MUST NOT language; advisory language does not satisfy the hk-hqwn.30 requirement",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"EV-021 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestObservationalReplay_EV021_CodeGodocCarriesAdvisoryConstraint verifies
// that the ReadJSONLForDivergenceEvidence godoc in jsonldivergence_em031.go
// carries the EV-021 advisory constraint so the prohibition is visible at the
// call site without requiring a spec lookup.
//
// The godoc must mention:
//   - "MUST NOT"                  — the normative prohibition phrase
//   - "reconstruct run state"     — names the forbidden action explicitly
//   - "git"                       — the authoritative redirect target (git + Beads)
//   - "Beads"                     — the second authoritative source
func TestObservationalReplay_EV021_CodeGodocCarriesAdvisoryConstraint(t *testing.T) {
	t.Parallel()

	godoc := replayNotReconstructFixtureDivergenceGoContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "MUST NOT",
			hint:   "ReadJSONLForDivergenceEvidence godoc must carry the normative prohibition phrase 'MUST NOT'; advisory language does not satisfy EV-021",
		},
		{
			phrase: "reconstruct run state",
			hint:   "ReadJSONLForDivergenceEvidence godoc must name 'reconstruct run state' as the forbidden action; this anchors the EV-021 constraint at the function level",
		},
		{
			phrase: "git",
			hint:   "ReadJSONLForDivergenceEvidence godoc must redirect consumers to git as an authoritative source; git + Beads is the EV-021-mandated state-reconstruction path",
		},
		{
			phrase: "Beads",
			hint:   "ReadJSONLForDivergenceEvidence godoc must name Beads as the second authoritative source per EV-021; the redirect must be complete",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(godoc, tc.phrase) {
			t.Errorf(
				"ReadJSONLForDivergenceEvidence godoc does not contain %q — %s\nGodoc:\n%s",
				tc.phrase, tc.hint, godoc,
			)
		}
	}
}

// TestObservationalReplay_EV021_ForwardDocSensor is a documentation-marker
// test for event-model.md §4.5 EV-021 (hk-hqwn.30).
//
// EV-021 requires that observational replay output is advisory only. Any tool
// walking JSONL for debugging, pattern analysis, or dashboard purposes is
// performing observational replay and MUST NOT use the result to reconstruct
// durable state. Authoritative state-reconstruction source is git plus Beads.
//
// This test skips unconditionally because the consumer-side enforcement layer
// (a runtime guard that rejects JSONL-result-as-authoritative-state usage) is
// not yet implemented. It exists as a discoverable anchor in the test suite.
// When the enforcement layer lands, the implementer SHOULD either:
//
//  1. Replace this marker with concrete assertions against the enforcement layer, OR
//  2. Extend it with those assertions, retaining the EV-021 citation and
//     hk-hqwn.30 traceability.
//
// Requirement-traceable bead: hk-hqwn.30.
func TestObservationalReplay_EV021_ForwardDocSensor(t *testing.T) {
	t.Log("EV-021 (hk-hqwn.30): observational replay output is advisory only.")
	t.Log("ANY tool walking JSONL for debugging, pattern analysis, or dashboard is observational replay.")
	t.Log("MUST NOT reconstruct durable state from JSONL results.")
	t.Log("Authoritative state-reconstruction source is git plus Beads per execution-model.md §4.7.")
	t.Log("Spec reference: event-model.md §4.5 EV-021.")
	t.Log("")
	t.Log("Consumer-side enforcement layer not yet implemented.")
	t.Log("When that layer lands, the implementer SHOULD:")
	t.Log("  1. Delete this forward-doc marker, OR")
	t.Log("  2. Extend it with concrete assertions against the enforcement layer.")
	t.SkipNow()
}
