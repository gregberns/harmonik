//go:build specaudit

package specaudit_test

// hk-sx9r.60 binding test — ON-044 distributed tracing across daemons is deferred post-MVH.
//
// Spec ref: specs/operator-nfr.md §4.10 ON-044.
//
// ON-044 states: distributed tracing across multiple harmonik instances is deferred
// post-MVH. Per-project daemon isolation means multi-instance tracing is an
// OS-process-isolation concern, not a harmonik-code concern — each daemon is a
// separate process with its own event log and own state. Cross-daemon correlation
// (if ever needed) is an external-tooling layer, not a foundation spec.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that ON-044 is correctly declared
// in the spec so that:
//
//  1. ON-044 heading is present in specs/operator-nfr.md.
//  2. "Distributed tracing" is declared as the deferred concern.
//  3. "Deferred" wording is present (deferral is explicit, not implicit).
//  4. Per-project daemon isolation is cited as the basis for the deferral.
//  5. "OS-process-isolation" framing is declared (multi-instance tracing is OS-level).
//  6. Cross-daemon correlation is named as an external-tooling concern.
//  7. "post-MVH" scoping is declared (deferral is bounded, not open-ended).
//  8. Tags: mechanism is present in the ON-044 body window.
//
// # Failure modes
//
//   - ON-044 heading missing.
//   - Distributed tracing wording absent.
//   - Deferral wording absent.
//   - Per-project daemon isolation basis absent.
//   - OS-process-isolation framing absent.
//   - Cross-daemon correlation external-tooling reference absent.
//   - Post-MVH scoping absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on044Fixture prefix per
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

// on044FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on044FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on044FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on044FixtureON044Heading matches the ON-044 level-4 requirement heading line.
var on044FixtureON044Heading = regexp.MustCompile(`^#### ON-044 —`)

// on044FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on044FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on044FixtureTagsMechanism matches a "Tags: mechanism" line.
var on044FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on044FixtureBodyWindow is the maximum number of lines after the ON-044
// heading to scan for requirement-body content.
const on044FixtureBodyWindow = 30

// on044FixtureLoadLines opens specFile and returns all lines.
func on044FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on044FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on044FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on044FixtureON044BodyLines returns the lines comprising the ON-044 body.
func on044FixtureON044BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on044FixtureON044Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-044 heading not found; expected '#### ON-044 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on044FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on044FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on044FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on044FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON044DistributedTracingDeferredPostMVH is the binding test for hk-sx9r.60.
func TestON044DistributedTracingDeferredPostMVH(t *testing.T) {
	t.Parallel()

	specFile := on044FixtureOperatorNFRPath(t)
	lines := on044FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on044FixtureON044BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-044 check(1): %s", reason)
	}
	t.Logf("ON-044 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "distributed-tracing-named",
			needle: "distributed tracing",
			detail: "ON-044 body must name 'distributed tracing' as the deferred concern " +
				"(expected phrase 'distributed tracing'); this scopes the deferral precisely — " +
				"single-daemon tracing (per-event-log) is in scope for MVH; cross-daemon " +
				"correlation is not",
		},
		{
			id:     "3",
			label:  "deferred-wording-present",
			needle: "deferred",
			detail: "ON-044 body must use the word 'deferred' (expected phrase 'deferred'); " +
				"the deferral wording is the normative signal that this concern is acknowledged " +
				"and bounded, not dismissed or silently omitted",
		},
		{
			id:     "4",
			label:  "per-project-daemon-isolation-basis",
			needle: "per-project daemon isolation",
			detail: "ON-044 body must cite per-project daemon isolation as the basis for the deferral " +
				"(expected phrase 'per-project daemon isolation'); the argument is that isolation-by-" +
				"separate-process is sufficient for MVH and cross-daemon tracing is therefore an " +
				"OS-level concern, not a foundation spec concern",
		},
		{
			id:     "5",
			label:  "os-process-isolation-framing",
			needle: "OS-process-isolation",
			detail: "ON-044 body must declare that multi-instance tracing is an OS-process-isolation " +
				"concern (expected phrase 'OS-process-isolation'); this framing is the normative " +
				"justification — harmonik is not responsible for tracing across OS-level process " +
				"boundaries in MVH",
		},
		{
			id:     "6",
			label:  "cross-daemon-correlation-external-tooling",
			needle: "external-tooling",
			detail: "ON-044 body must declare cross-daemon correlation as an external-tooling concern " +
				"(expected phrase 'external-tooling'); this is the bounded framing that prevents " +
				"future spec drift from re-opening the deferral as a foundation requirement",
		},
		{
			id:     "7",
			label:  "post-mvh-scoping",
			needle: "post-MVH",
			detail: "ON-044 body must carry 'post-MVH' scoping (expected phrase 'post-MVH'); " +
				"this bounds the deferral and signals that cross-daemon tracing may be re-evaluated " +
				"in a post-MVH foundation amendment if the need arises",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on044FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-044 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-044 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in ON-044 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on044FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-044 check(8) FAILED: Tags: mechanism not found in ON-044 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-044 body)\n"+
					"  detail: ON-044 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.60 audit complete — ON-044 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
