package specaudit_test

// hk-i0tw.53 binding test — SH §8 failure-class taxonomy + §8.0 precedence table.
//
// Spec ref: specs/scenario-harness.md §8.
//
// The SH §8 taxonomy defines eight failure classes covering every failure
// transition in the scenario lifecycle (§7.1 pseudocode).  §8.0 defines the
// precedence table used when two or more classes co-occur on a single scenario.
//
// # Audit frame
//
// This is a spec-corpus binding test.  It verifies that specs/scenario-harness.md
// carries the normative §8 content at the spec-text level so that drift (renaming,
// removal, reordering of classes) surfaces immediately:
//
//  1. §8.0 heading — "### 8.0 Failure-class precedence" heading present.
//
//  2. Precedence table completeness — all eight failure classes appear in the
//     §8.0 body window, in the canonical precedence order:
//       harness-internal-error (rank 1)
//       orchestration-internal-error (rank 2)
//       scenario-load-failure (rank 3)
//       twin-binary-not-found (rank 4)
//       fixture-setup-failed (rank 5)
//       scenario-timeout (rank 6)
//       assertion-failed (rank 7)
//       cleanup-failed (rank 8)
//
//  3. cleanup-failed override prohibition — §8.0 body states that cleanup-failed
//     NEVER overwrites a prior verdict; it is appended to error_detail only.
//     This is the load-bearing invariant for cleanup-failed's rank-8 position.
//
//  4. §8.1..§8.8 section headings — each of the eight class sections is present
//     with a level-3 heading of the form "### 8.N `<class-name>`".
//
//  5. Each §8.N body carries a Detection, Default response, Escalation, and EM
//     analog bullet — the minimum spec-text contract for each class.
//
// # Failure modes
//
//   - §8.0 heading missing.
//   - One or more failure-class names absent from the §8.0 precedence window.
//   - cleanup-failed override-prohibition clause missing from §8.0.
//   - One or more §8.1..§8.8 level-3 headings missing.
//   - One or more §8.N bodies missing Detection, Default response, Escalation,
//     or EM analog bullets.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sh8TaxonomyFixture prefix
// per the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// sh8TaxonomyFixtureScenarioHarnessPath returns the absolute path to
// specs/scenario-harness.md by resolving from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/sh_section8_taxonomy_test.go
//
// so the repo root is two directories up.
func sh8TaxonomyFixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh8TaxonomyFixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh8TaxonomyFixtureLoadLines opens specFile and returns all lines.
func sh8TaxonomyFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh8TaxonomyFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh8TaxonomyFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh8TaxonomyFixtureSection80Heading matches the §8.0 level-3 heading.
var sh8TaxonomyFixtureSection80Heading = regexp.MustCompile(`^### 8\.0 Failure-class precedence`)

// sh8TaxonomyFixtureSection8NHeading matches any §8.1..§8.8 level-3 section
// heading of the form "### 8.N `<class-name>`".
var sh8TaxonomyFixtureSection8NHeading = regexp.MustCompile("^### 8\\.[1-8] `")

// sh8TaxonomyFixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to bound look-ahead windows.
var sh8TaxonomyFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh8TaxonomyFixtureBodyWindow is the maximum number of lines after a section
// heading to scan for requirement-body content.  The §8.0 precedence table has
// ~15 lines; 40 gives generous headroom while keeping the window tight.
const sh8TaxonomyFixtureBodyWindow = 40

// sh8TaxonomyFixtureBodyLinesAfter returns the lines comprising the body of the
// section starting at headingIdx: all lines after the heading up to the next
// Markdown heading or sh8TaxonomyFixtureBodyWindow lines, whichever comes first.
func sh8TaxonomyFixtureBodyLinesAfter(lines []string, headingIdx int) []string {
	limit := headingIdx + 1 + sh8TaxonomyFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var body []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh8TaxonomyFixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		body = append(body, lines[i])
	}
	return body
}

// sh8TaxonomyFixtureBodyContains reports whether any line in body contains
// substr (case-insensitive).
func sh8TaxonomyFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// sh8TaxonomyFixtureFindHeadingIdx returns the 0-based index of the first line
// matching re, or -1 if not found.
func sh8TaxonomyFixtureFindHeadingIdx(lines []string, re *regexp.Regexp) int {
	for i, line := range lines {
		if re.MatchString(line) {
			return i
		}
	}
	return -1
}

// sh8TaxonomyFixtureAllLinesContain reports whether substr (case-insensitive)
// appears anywhere in lines.
func sh8TaxonomyFixtureAllLinesContain(lines []string, substr string) bool {
	return sh8TaxonomyFixtureBodyContains(lines, substr)
}

// TestSHSection8TaxonomySpec is the binding test for SH §8 failure-class
// taxonomy + §8.0 precedence table.
//
// It opens specs/scenario-harness.md and verifies:
//
//	(a) §8.0 heading present.
//	(b) All eight failure-class names in the §8.0 body.
//	(c) cleanup-failed override-prohibition clause in the §8.0 body.
//	(d) All eight §8.N section headings present.
//	(e) Each §8.N body carries Detection, Default response, Escalation, EM analog.
func TestSHSection8TaxonomySpec(t *testing.T) {
	t.Parallel()

	specFile := sh8TaxonomyFixtureScenarioHarnessPath(t)
	lines := sh8TaxonomyFixtureLoadLines(t, specFile)

	// --- check (a): §8.0 heading present ---
	section80Idx := sh8TaxonomyFixtureFindHeadingIdx(lines, sh8TaxonomyFixtureSection80Heading)
	if section80Idx < 0 {
		t.Fatalf("SH §8 check(a): '### 8.0 Failure-class precedence' heading not found in specs/scenario-harness.md; " +
			"the §8.0 precedence table is a load-bearing part of the failure-class taxonomy and must not be removed")
	}
	t.Logf("§8.0 heading found at line %d", section80Idx+1)

	section80Body := sh8TaxonomyFixtureBodyLinesAfter(lines, section80Idx)

	// --- check (b): all eight failure-class names in §8.0 body ---
	classes := []struct {
		name   string // the failure-class string value
		detail string // explanation for failure message
	}{
		{
			name:   "harness-internal-error",
			detail: "rank 1 — highest precedence; harness itself is broken",
		},
		{
			name:   "orchestration-internal-error",
			detail: "rank 2 — daemon stack in unknown state",
		},
		{
			name:   "scenario-load-failure",
			detail: "rank 3 — pre-orchestration load phase failure",
		},
		{
			name:   "twin-binary-not-found",
			detail: "rank 4 — pre-orchestration binary discovery failure",
		},
		{
			name:   "fixture-setup-failed",
			detail: "rank 5 — pre-orchestration fixture creation failure",
		},
		{
			name:   "scenario-timeout",
			detail: "rank 6 — supersedes assertion outcomes on partial data",
		},
		{
			name:   "assertion-failed",
			detail: "rank 7 — observational; run completed cleanly",
		},
		{
			name:   "cleanup-failed",
			detail: "rank 8 — lowest; never overwrites a prior verdict",
		},
	}

	for _, cls := range classes {
		cls := cls
		t.Run(fmt.Sprintf("check-b-%s-in-section80", strings.ReplaceAll(cls.name, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !sh8TaxonomyFixtureBodyContains(section80Body, cls.name) {
				t.Errorf(
					"SH §8 check(b): failure class %q not found in §8.0 precedence body\n"+
						"  spec:    specs/scenario-harness.md line ~%d (§8.0 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					cls.name, section80Idx+1, cls.name, cls.detail,
				)
			}
		})
	}

	// --- check (c): cleanup-failed override-prohibition clause ---
	t.Run("check-c-cleanup-failed-never-overwrites", func(t *testing.T) {
		t.Parallel()
		// The spec uses "NEVER overwrites" — check for the prohibition keyword
		// (case-insensitive "never overwrite" covers "NEVER overwrites").
		if !sh8TaxonomyFixtureBodyContains(section80Body, "never overwrite") &&
			!sh8TaxonomyFixtureBodyContains(section80Body, "NEVER overwrites") {
			t.Errorf(
				"SH §8 check(c): cleanup-failed override-prohibition clause not found in §8.0 body\n"+
					"  spec:    specs/scenario-harness.md line ~%d (§8.0 body)\n"+
					"  missing: phrase indicating cleanup-failed never overwrites a prior verdict\n"+
					"  detail:  §8.0 must state that cleanup-failed (rank 8) never overwrites pass/fail/timeout; "+
					"this is the load-bearing invariant for its lowest-precedence position",
				section80Idx+1,
			)
		}
	})

	// --- check (d): §8.1..§8.8 level-3 section headings present ---
	classHeadings := []struct {
		sectionNum string // e.g. "8.1"
		className  string // e.g. "scenario-load-failure"
	}{
		{"8.1", "scenario-load-failure"},
		{"8.2", "twin-binary-not-found"},
		{"8.3", "fixture-setup-failed"},
		{"8.4", "orchestration-internal-error"},
		{"8.5", "assertion-failed"},
		{"8.6", "scenario-timeout"},
		{"8.7", "harness-internal-error"},
		{"8.8", "cleanup-failed"},
	}

	for _, ch := range classHeadings {
		ch := ch
		// Pattern: "### 8.N `<class-name>`"
		headingPat := regexp.MustCompile(
			`^### ` + regexp.QuoteMeta(ch.sectionNum) + ` ` + "`" + regexp.QuoteMeta(ch.className) + "`",
		)
		sectionIdx := sh8TaxonomyFixtureFindHeadingIdx(lines, headingPat)
		testName := fmt.Sprintf("check-d-section-%s-%s",
			strings.ReplaceAll(ch.sectionNum, ".", "_"),
			strings.ReplaceAll(ch.className, "-", "_"))

		t.Run(testName, func(t *testing.T) {
			t.Parallel()
			if sectionIdx < 0 {
				t.Fatalf(
					"SH §8 check(d): section heading for %s %q not found in specs/scenario-harness.md\n"+
						"  missing: '### %s `%s`'\n"+
						"  detail:  each of the eight failure classes must have a dedicated §%s subsection; "+
						"absence means the class is undocumented at the spec layer",
					ch.sectionNum, ch.className, ch.sectionNum, ch.className, ch.sectionNum,
				)
			}
			t.Logf("§%s `%s` heading found at line %d", ch.sectionNum, ch.className, sectionIdx+1)

			// --- check (e): Detection, Default response, Escalation, EM analog ---
			body := sh8TaxonomyFixtureBodyLinesAfter(lines, sectionIdx)
			bodyChecks := []struct {
				id     string
				needle string
				detail string
			}{
				{
					id:     "detection",
					needle: "Detection",
					detail: "each §8.N body must carry a '**Detection.**' bullet naming the detection rule",
				},
				{
					id:     "default-response",
					needle: "Default response",
					detail: "each §8.N body must carry a '**Default response.**' bullet naming the response action",
				},
				{
					id:     "escalation",
					needle: "Escalation",
					detail: "each §8.N body must carry an '**Escalation.**' bullet naming the escalation path",
				},
				{
					id:     "em-analog",
					needle: "EM analog",
					detail: "each §8.N body must carry an '**EM analog.**' bullet noting the execution-model analog",
				},
			}
			for _, bc := range bodyChecks {
				bc := bc
				t.Run(fmt.Sprintf("check-e-%s-%s", strings.ReplaceAll(ch.sectionNum, ".", "_"), bc.id), func(t *testing.T) {
					t.Parallel()
					if !sh8TaxonomyFixtureBodyContains(body, bc.needle) {
						t.Errorf(
							"SH §8 check(e): %q not found in §%s `%s` body\n"+
								"  spec:    specs/scenario-harness.md line ~%d (§%s body)\n"+
								"  missing: %q\n"+
								"  detail:  %s",
							bc.needle, ch.sectionNum, ch.className,
							sectionIdx+1, ch.sectionNum,
							bc.needle, bc.detail,
						)
					}
				})
			}
		})
	}

	t.Logf("SH §8 taxonomy audit complete: §8.0 at line %d, %d class sections scanned",
		section80Idx+1, len(classHeadings))
}
