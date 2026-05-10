package specaudit_test

// hk-8i31.37 binding test — HC-030 redaction registry middleware in event-bus producer path.
//
// Spec ref: specs/handler-contract.md §4.7 HC-030.
//
// HC-030 states: the daemon MUST install a redaction-middleware function in the event-bus
// producer path that applies the rules of HC-031 and HC-032 to every event payload and
// every structured log line before emission. The middleware is mechanism-tagged; no cognition
// participates.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that HC-030 is correctly declared in the spec so that:
//
//  1. HC-030 heading is present in specs/handler-contract.md.
//  2. "redaction-middleware function" is declared in the event-bus producer path.
//  3. "HC-031" is named as a rule applied by the middleware.
//  4. "HC-032" is named as a rule applied by the middleware.
//  5. "no cognition participates" is declared.
//  6. Tags: mechanism is present in the HC-030 body window.
//
// # Failure modes
//
//   - HC-030 heading missing.
//   - redaction-middleware function absent.
//   - HC-031 absent.
//   - HC-032 absent.
//   - no cognition participates absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc030Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

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

// hc030FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc030FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc030FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc030FixtureHeading matches the HC-030 level-4 requirement heading line.
var hc030FixtureHeading = regexp.MustCompile(`^#### HC-030 —`)

// hc030FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc030FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc030FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc030FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc030FixtureBodyWindow is the maximum number of lines to scan after the heading.
const hc030FixtureBodyWindow = 15

// hc030FixtureLoadLines opens specFile and returns all lines.
func hc030FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc030FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc030FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc030FixtureBodyLines returns the lines comprising the HC-030 body.
func hc030FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc030FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-030 heading not found; expected '#### HC-030 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc030FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc030FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc030FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc030FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC030RedactionMiddleware is the binding test for hk-8i31.37.
func TestHC030RedactionMiddleware(t *testing.T) {
	t.Parallel()

	specFile := hc030FixtureHandlerContractPath(t)
	lines := hc030FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc030FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-030 check(1): %s", reason)
	}
	t.Logf("HC-030 heading found at specs/handler-contract.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "2",
			label:  "redaction-middleware-in-producer-path",
			needle: "redaction-middleware function",
			detail: "HC-030 body must declare 'redaction-middleware function' in the event-bus producer path " +
				"(expected phrase 'redaction-middleware function'); the middleware is installed at the " +
				"producer level — all events pass through it before reaching subscribers, ensuring " +
				"no secret value can bypass redaction",
		},
		{
			id:     "3",
			label:  "hc-031-rule-applied",
			needle: "HC-031",
			detail: "HC-030 body must name 'HC-031' as a rule applied by the middleware " +
				"(expected phrase 'HC-031'); HC-031 is the common-prefix redaction rule that " +
				"redacts all values whose keys match the HARMONIK_SECRET_* pattern — the " +
				"middleware applies it to every event payload and structured log line",
		},
		{
			id:     "4",
			label:  "hc-032-rule-applied",
			needle: "HC-032",
			detail: "HC-030 body must name 'HC-032' as a rule applied by the middleware " +
				"(expected phrase 'HC-032'); HC-032 is the per-handler redaction pattern rule — " +
				"handlers declare additional redaction patterns in their subsystem envelope, and " +
				"the middleware applies them alongside the common prefix rule",
		},
		{
			id:     "5",
			label:  "no-cognition-participates",
			needle: "no cognition participates",
			detail: "HC-030 body must declare 'no cognition participates' " +
				"(expected phrase 'no cognition participates'); redaction is a deterministic " +
				"pattern-matching operation — no LLM inference or heuristic determines what to " +
				"redact; this is the mechanism-tag discipline applied explicitly",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc030FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-030 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-030 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in HC-030 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc030FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-030 check(6) FAILED: Tags: mechanism not found in HC-030 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-030 body)\n"+
					"  detail: HC-030 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.37 audit complete — HC-030 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
