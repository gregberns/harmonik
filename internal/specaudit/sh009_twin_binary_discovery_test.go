package specaudit_test

// hk-i0tw.9 binding test — SH-009: twin-binary discovery uses scenario path or PATH prefix.
//
// Spec ref: specs/scenario-harness.md §4.3 SH-009.
//
// SH-009 states: `agent_overrides[role]` MUST resolve a twin binary by one of
// two mechanisms: (a) absolute path in the scenario, or (b) a name resolved
// against a configured twin-binary search-path prefix.  Unrestricted `$PATH`
// lookup is forbidden.  Search-path source precedence: CLI `--twin-search-path`,
// env `HARMONIK_TWIN_SEARCH_PATH`, default `<repo-root>/twins/`.  Hash-check
// failures (HC-043) classify as `twin-binary-not-found` even when the binary
// is present on disk.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-009 is present and declares:
//
//  1. Heading present — "#### SH-009 —".
//  2. Two resolution mechanisms: (a) absolute path, (b) search-path prefix.
//  3. Unrestricted $PATH lookup prohibition.
//  4. --twin-search-path CLI flag citation.
//  5. HARMONIK_TWIN_SEARCH_PATH env-var citation.
//  6. Default `<repo-root>/twins/` fallback.
//  7. HC-043 hash check citation.
//  8. Hash-check failure → twin-binary-not-found.
//  9. Tags: mechanism.
//
// # Helper prefix: sh009Fixture

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

func sh009FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh009FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh009FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh009FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh009FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var sh009FixtureSH009Heading = regexp.MustCompile(`^#### SH-009 —`)
var sh009FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
var sh009FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

const sh009FixtureBodyWindow = 30

func sh009FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh009FixtureSH009Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-009 heading '#### SH-009 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh009FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh009FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh009FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH009TwinBinaryDiscovery is the binding test for SH-009.
func TestSH009TwinBinaryDiscovery(t *testing.T) {
	t.Parallel()

	specFile := sh009FixtureScenarioHarnessPath(t)
	lines := sh009FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh009FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-009 check(a): %s", reason)
	}
	t.Logf("SH-009 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "absolute-path-resolution-mechanism",
			needle: "absolute path",
			detail: "SH-009 body must declare mechanism (a): resolution via absolute path declared in the scenario",
		},
		{
			id:     "c",
			label:  "search-path-prefix-mechanism",
			needle: "search-path",
			detail: "SH-009 body must declare mechanism (b): resolution against a configured twin-binary search-path prefix",
		},
		{
			id:     "d",
			label:  "unrestricted-path-lookup-prohibition",
			needle: "MUST NOT perform unrestricted",
			detail: "SH-009 body must forbid unrestricted $PATH lookup for twin binaries; " +
				"absence means the prohibition is undocumented at the spec layer",
		},
		{
			id:     "e",
			label:  "twin-search-path-cli-flag",
			needle: "--twin-search-path",
			detail: "SH-009 body must cite the `--twin-search-path` CLI flag as the highest-precedence search-path source",
		},
		{
			id:     "f",
			label:  "harmonik-twin-search-path-env-var",
			needle: "HARMONIK_TWIN_SEARCH_PATH",
			detail: "SH-009 body must cite the HARMONIK_TWIN_SEARCH_PATH env var as the second-precedence search-path source",
		},
		{
			id:     "g",
			label:  "default-twins-directory",
			needle: "twins/",
			detail: "SH-009 body must declare the default `<repo-root>/twins/` fallback; " +
				"absence means the default is undocumented",
		},
		{
			id:     "h",
			label:  "hc043-hash-check",
			needle: "HC-043",
			detail: "SH-009 body must cite HC-043 commit-hash verification as required for resolved twin paths",
		},
		{
			id:     "i",
			label:  "hash-check-failure-twin-binary-not-found",
			needle: "twin-binary-not-found",
			detail: "SH-009 body must state hash-check failure classifies as twin-binary-not-found " +
				"(binary present but version-mismatched is a discovery failure for the harness's purposes)",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh009FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-009 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-009 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-j-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh009FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-009 check(j) FAILED: Tags: mechanism not found in SH-009 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-009 body)\n"+
					"  detail: SH-009 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-009 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
