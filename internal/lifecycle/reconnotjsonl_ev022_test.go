// Package lifecycle — EV-022 state-reconstruction-must-not-walk-JSONL sensor.
//
// EV-022 (event-model.md §4.5 EV-022): "The daemon's startup
// state-reconstruction path MUST walk git plus query Beads; it MUST NOT read
// JSONL to reconstruct state."
//
// State reconstruction on daemon startup MUST use git (task-branch tip walk
// via Harmonik-Run-ID trailers) and Beads (non-terminal bead query) as its
// sole inputs. Walking the JSONL event log for reconstruction is prohibited;
// JSONL is observational (see EV-021).
//
// PRE-FLIGHT SIGNAL: reconstruction_em031_test.go already sensors the
// behavioral DiscoverActiveRuns-has-no-JSONL-param contract under EM-031
// (execution-model.md §4.7). Those tests are the behavioral enforcement layer.
// This file adds the EV-022 (event-model.md §4.5) spec-text sensor, which
// is distinct: it asserts that the event-model spec encodes the prohibition
// with the required canonical phrases, and that the production code's godoc
// carries the EV-022 citation. The behavioral contract itself is not
// re-implemented here.
//
// This file is the documentation/discipline sensor for hk-hqwn.31. The
// runtime enforcement is in place (DiscoverActiveRuns has no JSONL parameter);
// this file locks in the spec-text anchor and godoc traceability.
//
// Invariant locked by EV-022:
//
//	daemon startup state-reconstruction MUST NOT read JSONL
//	→  reconstruction MUST walk git (task-branch tips)
//	→  reconstruction MUST query Beads (non-terminal bead records)
//	→  JSONL is observational only (see EV-021)
//
// The tests below assert:
//  1. specs/event-model.md encodes EV-022 with the required canonical phrases.
//  2. The DiscoverActiveRuns godoc in activerun_em031a.go carries the EV-022
//     spec citation so the constraint is visible at the function declaration.
//
// Requirement-traceable bead: hk-hqwn.31.
package lifecycle

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// reconNotJSONLFixtureSpecContent reads specs/event-model.md, locates the
// EV-022 anchor, and returns the paragraph that contains it. It fails the
// test if the file is unreadable or the anchor is missing.
func reconNotJSONLFixtureSpecContent(t *testing.T) string {
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

	const anchor = "EV-022 — State reconstruction MUST NOT walk JSONL"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("spec %s does not contain %q; EV-022 may have been removed or renamed", specPath, anchor)
	}

	// Return the paragraph from the anchor up to the next section boundary.
	paragraph := content[idx:]
	if end := strings.Index(paragraph, "\n####"); end > 0 {
		paragraph = paragraph[:end]
	}
	return paragraph
}

// reconNotJSONLFixtureActiveRunGoContent reads internal/lifecycle/activerun_em031a.go
// and returns the godoc block for DiscoverActiveRuns. It fails the test if
// the file is unreadable or the function declaration is absent.
func reconNotJSONLFixtureActiveRunGoContent(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate activerun_em031a.go")
	}
	srcPath := filepath.Join(filepath.Dir(thisFile), "activerun_em031a.go")

	raw, err := os.ReadFile(srcPath) //nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	if err != nil {
		t.Fatalf("cannot read %s: %v", srcPath, err)
	}

	// Find the function declaration line.
	lines := strings.Split(string(raw), "\n")
	funcLineIdx := -1
	for i, line := range lines {
		if strings.Contains(line, "func DiscoverActiveRuns(") {
			funcLineIdx = i
			break
		}
	}
	if funcLineIdx < 0 {
		t.Fatal("activerun_em031a.go does not contain DiscoverActiveRuns declaration")
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

// TestReconNotJSONL_EV022_SpecContainsProhibitionInvariant verifies that
// the EV-022 section of specs/event-model.md encodes the reconstruction
// prohibition with the required canonical phrases.
//
// Phrases required by the invariant (EV-022, hk-hqwn.31):
//
//   - "MUST NOT"                  — the normative prohibition; advisory language is not sufficient
//   - "JSONL"                     — names the prohibited input explicitly
//   - "git"                       — names one authoritative reconstruction source
//   - "Beads"                     — names the second authoritative reconstruction source
//
// A future rename of any of these phrases in the spec is a breaking change and
// MUST be accompanied by a corresponding update to this test.
func TestReconNotJSONL_EV022_SpecContainsProhibitionInvariant(t *testing.T) {
	t.Parallel()

	para := reconNotJSONLFixtureSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "MUST NOT",
			hint:   "EV-022 must use normative MUST NOT language; advisory language does not satisfy the hk-hqwn.31 requirement",
		},
		{
			phrase: "JSONL",
			hint:   "EV-022 must name JSONL as the prohibited reconstruction input; vague phrasing weakens the prohibition",
		},
		{
			phrase: "git",
			hint:   "EV-022 must name git as an authoritative reconstruction source; the redirect must be explicit",
		},
		{
			phrase: "Beads",
			hint:   "EV-022 must name Beads as the second authoritative reconstruction source; both stores must be mentioned",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf(
				"EV-022 spec paragraph does not contain %q — %s\nParagraph:\n%s",
				tc.phrase, tc.hint, para,
			)
		}
	}
}

// TestReconNotJSONL_EV022_CodeGodocCarriesSpecCitation verifies that
// DiscoverActiveRuns godoc in activerun_em031a.go carries a spec citation
// referencing the no-JSONL-for-reconstruction prohibition so the constraint
// is visible at the function declaration.
//
// The godoc must mention:
//   - "EM-031"                    — the execution-model requirement (primary citation)
//   - "JSONL"                     — names the prohibited path so the constraint is unambiguous
//   - "git"                       — one authoritative source; confirms the redirect is complete
func TestReconNotJSONL_EV022_CodeGodocCarriesSpecCitation(t *testing.T) {
	t.Parallel()

	godoc := reconNotJSONLFixtureActiveRunGoContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "EM-031",
			hint:   "DiscoverActiveRuns godoc must cite EM-031 so spec traceability is visible at the declaration; execution-model.md §4.7 is the load-bearing citation for the no-JSONL rule",
		},
		{
			phrase: "JSONL",
			hint:   "DiscoverActiveRuns godoc must name JSONL as the prohibited reconstruction input; without naming it the constraint is ambiguous",
		},
		{
			phrase: "git",
			hint:   "DiscoverActiveRuns godoc must name git as an authoritative reconstruction source; the redirect must be explicit at the call site",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(godoc, tc.phrase) {
			t.Errorf(
				"DiscoverActiveRuns godoc does not contain %q — %s\nGodoc:\n%s",
				tc.phrase, tc.hint, godoc,
			)
		}
	}
}
