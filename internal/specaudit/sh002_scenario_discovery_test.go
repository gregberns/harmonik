package specaudit_test

// hk-i0tw.2 binding test — SH-002: scenario files live under `scenarios/` at the repo root.
//
// Spec ref: specs/scenario-harness.md §4.1 SH-002.
//
// SH-002 states: scenario files MUST be discovered under `scenarios/` at the
// repo root (via git rev-parse --show-toplevel).  Absolute or workspace-relative
// paths are forbidden.  Extension MUST be byte-exact `.yaml`; `.yml` / `.YAML`
// MUST be rejected at suite-load as `scenario-load-failure`.
//
// # Audit frame
//
// This is a spec-corpus binding test.  It verifies that specs/scenario-harness.md
// carries the SH-002 obligation at the spec-text layer so that any drift surfaces
// immediately:
//
//  1. Heading present — "#### SH-002 —" heading present.
//
//  2. `scenarios/` root declaration — body states scenario files are under
//     `scenarios/` at the repo root.
//
//  3. `git rev-parse --show-toplevel` citation — body cites the git command
//     used to determine the repo root.
//
//  4. Absolute-path prohibition — body forbids absolute or workspace-relative
//     scenario paths.
//
//  5. `.yaml` extension obligation — body states the cross-platform extension
//     is `.yaml` (lower-case, byte-exact).
//
//  6. `.yml` rejection as `scenario-load-failure` — body states `.yml` (and
//     other non-`.yaml` extensions) MUST be rejected at suite-load.
//
//  7. Tags: mechanism — body carries the mechanism tag.
//
// # Helper prefix
//
// All package-level identifiers use the sh002Fixture prefix.

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

// sh002FixtureScenarioHarnessPath returns the absolute path to specs/scenario-harness.md.
func sh002FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh002FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// sh002FixtureLoadLines opens specFile and returns all lines.
func sh002FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh002FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh002FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sh002FixtureSH002Heading matches the SH-002 level-4 heading.
var sh002FixtureSH002Heading = regexp.MustCompile(`^#### SH-002 —`)

// sh002FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var sh002FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sh002FixtureTagsMechanism matches a "Tags: mechanism" line in the body.
var sh002FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sh002FixtureBodyWindow is the maximum line look-ahead after the SH-002 heading.
const sh002FixtureBodyWindow = 30

// sh002FixtureBodyLines returns the SH-002 body lines (up to next heading or window).
// Returns nil and reason string if SH-002 heading is absent.
func sh002FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh002FixtureSH002Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-002 heading '#### SH-002 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh002FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh002FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

// sh002FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func sh002FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH002ScenarioDiscovery is the binding test for SH-002.
func TestSH002ScenarioDiscovery(t *testing.T) {
	t.Parallel()

	specFile := sh002FixtureScenarioHarnessPath(t)
	lines := sh002FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh002FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-002 check(a): %s", reason)
	}
	t.Logf("SH-002 heading found at specs/scenario-harness.md line %d; body = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "b",
			label:  "scenarios-root-declaration",
			needle: "scenarios/",
			detail: "SH-002 body must state scenario files are under the `scenarios/` directory; " +
				"this is the primary discovery root obligation",
		},
		{
			id:     "c",
			label:  "git-rev-parse-citation",
			needle: "git rev-parse --show-toplevel",
			detail: "SH-002 body must cite `git rev-parse --show-toplevel` as the repo-root determination mechanism; " +
				"absence means the body does not ground the repo-root claim to a concrete command",
		},
		{
			id:     "d",
			label:  "absolute-path-prohibition",
			needle: "absolute",
			detail: "SH-002 body must forbid absolute (and workspace-relative) scenario paths; " +
				"this prohibition is the reproducibility and security guard",
		},
		{
			id:     "e",
			label:  "yaml-extension-obligation",
			needle: ".yaml",
			detail: "SH-002 body must state the cross-platform extension is `.yaml` (lower-case, byte-exact); " +
				"this grounds the extension check to a concrete file-name predicate",
		},
		{
			id:     "f",
			label:  "yml-rejection-scenario-load-failure",
			needle: ".yml",
			detail: "SH-002 body must state `.yml` (and other non-`.yaml` extensions) MUST be rejected " +
				"at suite-load as `scenario-load-failure`; absence means the rejection rule is undocumented",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh002FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-002 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-002 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-g-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh002FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-002 check(g) FAILED: Tags: mechanism not found in SH-002 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-002 body)\n"+
					"  detail: SH-002 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-002 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
