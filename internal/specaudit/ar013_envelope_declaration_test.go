package specaudit_test

// hk-zs0.14 binding test — AR-013 subsystem envelope declaration (8 elements).
//
// Spec ref: specs/architecture.md §4.4 AR-013, §4.0 AR-052, §4.0 AR-053.
//
// AR-013 states: "Every `runtime-subsystem` spec MUST declare its envelope in
// the §4.a slot reserved by §4.0.AR-053. The envelope consists of: (a) events
// produced, (b) events consumed, (c) types introduced that appear in
// cross-subsystem event payloads or shared state, (d) handlers implemented,
// (e) state owned, (f) control points provided, (g) NFRs inherited and/or
// overridden, (h) boundary classification for each operation exposed.
// `foundation-cross-cutting` specs are exempt per §4.0.AR-052."
//
// AR-053 states: "Every `runtime-subsystem` spec MUST carry its Subsystem
// envelope as the FIRST subsection under §4, titled `§4.a Subsystem envelope`."
//
// AR-052 states: "`foundation-cross-cutting` specs ... are exempt from envelope
// declaration."
//
// # Audit frame
//
// This test is a spec-corpus sensor. It walks every .md file under specs/,
// identifies those with `spec-category: runtime-subsystem` in their front
// matter, and for each such spec asserts:
//
//  1. A `§4.a Subsystem envelope` heading is present.
//  2. The envelope body (within the body window after the heading) contains
//     all eight element markers: `(a)`, `(b)`, `(c)`, `(d)`, `(e)`, `(f)`,
//     `(g)`, `(h)`.
//  3. A `Tags: mechanism` line is present in the envelope body window.
//
// Specs with `spec-category: foundation-cross-cutting` are skipped.
// Supplement files (status: supplement) are skipped.
//
// The test does NOT assert the CONTENT of each element (e.g. that every
// event type name resolves) — those are implementation-time obligations
// enforced by other sensors. This test guards only that the structural
// frame (section heading + eight slots) cannot be silently removed.
//
// # Failure modes
//
//   - §4.a heading missing from a runtime-subsystem spec.
//   - One or more element markers ((a)–(h)) absent from the §4.a body.
//   - Tags: mechanism absent from the §4.a body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ar013Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// ar013FixtureSpecsDir returns the absolute path to the specs/ directory at the
// repository root.
func ar013FixtureSpecsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar013FixtureSpecsDir: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar013_envelope_declaration_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs")
}

// ar013FixtureEnvelopeHeading matches a §4.a Subsystem envelope heading line.
// The heading may appear at any Markdown level (e.g. "### 4.a Subsystem envelope").
var ar013FixtureEnvelopeHeading = regexp.MustCompile(`^#{1,4}\s+4\.a\s+Subsystem envelope`)

// ar013FixtureHeadingLevel extracts the number of leading '#' characters from a
// Markdown heading line. Returns 0 if the line is not a heading.
func ar013FixtureHeadingLevel(line string) int {
	level := 0
	for _, ch := range line {
		if ch == '#' {
			level++
		} else {
			break
		}
	}
	if level == 0 || level > 6 {
		return 0
	}
	// A valid heading requires '#' followed by a space.
	if len(line) <= level || line[level] != ' ' {
		return 0
	}
	return level
}

// ar013FixtureTagsMechanism matches a "Tags: mechanism" line in the body.
var ar013FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// ar013FixtureBodyWindow is the maximum number of lines to scan after the
// §4.a heading before concluding the body window is exhausted.
const ar013FixtureBodyWindow = 100

// ar013FixtureEightElements is the ordered list of element markers that MUST
// appear somewhere in the §4.a body, per AR-013.
var ar013FixtureEightElements = []struct {
	marker string
	label  string
	detail string
}{
	{
		marker: "(a)",
		label:  "events-produced",
		detail: "AR-013(a): events produced — event type names with emission rules and schema citations",
	},
	{
		marker: "(b)",
		label:  "events-consumed",
		detail: "AR-013(b): events consumed — event type names with consumption rules and schema citations",
	},
	{
		marker: "(c)",
		label:  "types-introduced",
		detail: "AR-013(c): types introduced in cross-subsystem event payloads or shared state " +
			"(each carrying four-axis + mechanism/cognition tags)",
	},
	{
		marker: "(d)",
		label:  "handlers-implemented",
		detail: "AR-013(d): handlers implemented (or 'none') cited from handler-contract",
	},
	{
		marker: "(e)",
		label:  "state-owned",
		detail: "AR-013(e): state owned (or 'none') cited from execution-model",
	},
	{
		marker: "(f)",
		label:  "control-points-provided",
		detail: "AR-013(f): control points provided (or 'none') cited from control-points",
	},
	{
		marker: "(g)",
		label:  "nfrs-inherited-overridden",
		detail: "AR-013(g): NFRs inherited and/or overridden (or 'none') cited from operator-nfr",
	},
	{
		marker: "(h)",
		label:  "boundary-classification",
		detail: "AR-013(h): boundary classification per operation — the four-axis + mechanism/cognition tags",
	},
}

// ar013FixtureFrontMatterFields opens specFile and extracts the YAML front-matter
// key-value pairs using the same fenced-YAML scanning approach as ar052Fixture.
// It returns a map of key → value for scalar fields (list items are ignored).
func ar013FixtureFrontMatterFields(t *testing.T, specFile string) map[string]string {
	t.Helper()

	//nolint:gosec // G304: path derived from ar013FixtureSpecsDir which resolves to repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar013FixtureFrontMatterFields: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	fields := make(map[string]string)

	const (
		stateOutside = iota
		stateInFence
		stateInYAML
		stateDone
	)

	state := stateOutside
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch state {
		case stateOutside:
			if trimmed == "```yaml" {
				state = stateInFence
			}
		case stateInFence:
			if trimmed == "---" {
				state = stateInYAML
			} else if trimmed == "```" {
				state = stateDone
			}
		case stateInYAML:
			if trimmed == "---" || trimmed == "```" {
				state = stateDone
			} else {
				if idx := strings.IndexByte(trimmed, ':'); idx > 0 {
					key := strings.TrimSpace(trimmed[:idx])
					val := strings.TrimSpace(trimmed[idx+1:])
					if _, exists := fields[key]; !exists {
						fields[key] = val
					}
				}
			}
		case stateDone:
			// nothing further to parse
		}

		if state == stateDone {
			break
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar013FixtureFrontMatterFields: scan %s: %v", specFile, scanErr)
	}

	return fields
}

// ar013FixtureLoadLines opens specFile and returns all lines.
func ar013FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()

	//nolint:gosec // G304: path derived from ar013FixtureSpecsDir which resolves to repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar013FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar013FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// ar013FixtureEnvelopeBody locates the §4.a Subsystem envelope heading in lines
// and returns the body window (up to ar013FixtureBodyWindow lines, stopping at
// the next Markdown heading of the same or higher level — i.e. a sibling or
// ancestor section). Child headings (deeper level, e.g. #### ENV-001 inside
// ### 4.a) are included in the body. Returns (nil, 0, reason) if the heading
// is absent.
func ar013FixtureEnvelopeBody(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	envelopeLevel := 0
	for i, line := range lines {
		if ar013FixtureEnvelopeHeading.MatchString(line) {
			headingIdx = i
			envelopeLevel = ar013FixtureHeadingLevel(line)
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "§4.a Subsystem envelope heading not found; " +
			"expected a line matching '### 4.a Subsystem envelope' " +
			"(or any heading level 1–4) — " +
			"AR-053 requires this as the FIRST subsection under §4"
	}

	limit := headingIdx + 1 + ar013FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		lvl := ar013FixtureHeadingLevel(line)
		// Stop when we reach a sibling or ancestor heading (same or shallower level).
		if lvl > 0 && lvl <= envelopeLevel {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// ar013FixtureBodyContains reports whether any line in body contains substr
// (case-sensitive substring match).
func ar013FixtureBodyContains(body []string, substr string) bool {
	for _, line := range body {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// TestAR013EnvelopeDeclaration is the binding test for hk-zs0.14 (AR-013).
//
// It walks every .md file under specs/, skips foundation-cross-cutting and
// supplement files, and for each runtime-subsystem spec asserts that the
// §4.a Subsystem envelope section is present and contains all eight element
// markers (a)–(h) plus a Tags: mechanism line.
func TestAR013EnvelopeDeclaration(t *testing.T) {
	t.Parallel()

	specsDir := ar013FixtureSpecsDir(t)

	var specFiles []string
	walkErr := filepath.WalkDir(specsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			specFiles = append(specFiles, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("TestAR013EnvelopeDeclaration: walk %s: %v", specsDir, walkErr)
	}
	if len(specFiles) == 0 {
		t.Fatalf("TestAR013EnvelopeDeclaration: no .md files found under %s", specsDir)
	}

	t.Logf("AR-013 sensor: found %d spec file(s) under %s", len(specFiles), specsDir)

	for _, specFile := range specFiles {
		specFile := specFile
		relPath, relErr := filepath.Rel(specsDir, specFile)
		if relErr != nil {
			relPath = specFile
		}

		t.Run(relPath, func(t *testing.T) {
			t.Parallel()

			fields := ar013FixtureFrontMatterFields(t, specFile)

			// Supplement files are exempt: they are sibling components of a
			// multi-file spec and inherit their category from the primary file.
			if status := fields["status"]; status == "supplement" {
				t.Logf("AR-013 supplement-skip: %s has status=supplement; exempt", relPath)
				return
			}

			category := fields["spec-category"]
			if category != "runtime-subsystem" {
				// foundation-cross-cutting specs are exempt per AR-052.
				if category == "foundation-cross-cutting" {
					t.Logf("AR-013 exempt: %s is foundation-cross-cutting; skipping envelope check", relPath)
				} else {
					t.Logf("AR-013 unknown-category: %s has spec-category=%q; skipping envelope check "+
						"(AR-052 validity enforced by TestAR052SpecCategoryFrontMatter)",
						relPath, category)
				}
				return
			}

			// This spec is runtime-subsystem — apply the full AR-013 envelope check.
			lines := ar013FixtureLoadLines(t, specFile)
			body, headingLineNo, reason := ar013FixtureEnvelopeBody(lines)

			if reason != "" {
				t.Errorf(
					"AR-013 check(1) FAILED: §4.a envelope heading absent in %s\n"+
						"  detail: %s\n"+
						"  action: add '### 4.a Subsystem envelope' as the first subsection "+
						"under §4 and populate all eight envelope elements per AR-013",
					relPath, reason,
				)
				return
			}

			t.Logf("AR-013: %s §4.a heading at line %d; body window = %d lines",
				relPath, headingLineNo, len(body))

			// Checks (2a–2h): each of the eight element markers must appear.
			for _, elem := range ar013FixtureEightElements {
				elem := elem
				t.Run("element-"+elem.label, func(t *testing.T) {
					t.Parallel()
					if !ar013FixtureBodyContains(body, elem.marker) {
						t.Errorf(
							"AR-013 check(%s) FAILED: element marker %q absent in §4.a of %s\n"+
								"  spec:    %s line ~%d (§4.a body)\n"+
								"  missing: %q\n"+
								"  detail:  %s\n"+
								"  action:  add element %s to the §4.a envelope; "+
								"write 'none' explicitly if the element has no content",
							elem.label, elem.marker, relPath,
							relPath, headingLineNo,
							elem.marker, elem.detail,
							elem.marker,
						)
					}
				})
			}

			// Check (3): Tags: mechanism in §4.a body.
			t.Run("tags-mechanism", func(t *testing.T) {
				t.Parallel()
				found := false
				for _, line := range body {
					if ar013FixtureTagsMechanism.MatchString(line) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf(
						"AR-013 check(tags-mechanism) FAILED: 'Tags: mechanism' not found in §4.a body of %s\n"+
							"  spec:   %s line ~%d (§4.a body)\n"+
							"  detail: the §4.a envelope block must end with a 'Tags: mechanism' line "+
							"per the §A.1 template; its absence indicates the envelope block has been "+
							"truncated or the Tags: line was removed during editing",
						relPath, relPath, headingLineNo,
					)
				}
			})
		})
	}
}
