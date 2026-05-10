package specaudit_test

// hk-sx9r.14 binding test — ON-012 improvement-pause is a subtype of pause.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-012.
//
// ON-012 states: the S09 improvement cycle MUST NOT introduce a new operator-control
// state. An improvement-pause MUST transition running→pausing→paused via the same path
// as an operator pause, sharing the identical state table with the pause_reason
// discriminator. The paused→resuming transition is triggered automatically when the
// improvement loop completes (no operator action required). The earlier framing of
// separate improvement-pausing/improvement-paused states is retired.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The improvement-pause implementation is pending;
// this sensor verifies that ON-012 is correctly declared in the spec so that:
//
//  1. ON-012 heading is present in specs/operator-nfr.md.
//  2. "MUST NOT introduce a new operator-control state" is declared.
//  3. "pause_reason" discriminator is the mechanism for distinguishing pause origins.
//  4. "pause_reason=operator" is named as the operator-initiated discriminator value.
//  5. "pause_reason=improvement" is named as the improvement-initiated discriminator value.
//  6. Auto-resume on improvement-loop completion is declared (no operator action required).
//  7. "NOT a separate pair of state-machine states" is declared (retired framing).
//  8. Tags: mechanism is present in the ON-012 body window.
//
// # Failure modes
//
//   - ON-012 heading missing.
//   - MUST NOT introduce new state absent.
//   - pause_reason discriminator absent.
//   - pause_reason=operator absent.
//   - pause_reason=improvement absent.
//   - Auto-resume on improvement completion absent.
//   - Retired separate-states framing absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on012Fixture prefix per
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

// on012FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on012FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on012FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on012FixtureON012Heading matches the ON-012 level-4 requirement heading line.
var on012FixtureON012Heading = regexp.MustCompile(`^#### ON-012 —`)

// on012FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on012FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on012FixtureTagsMechanism matches a "Tags: mechanism" line.
var on012FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on012FixtureBodyWindow is the maximum number of lines after the ON-012
// heading to scan for requirement-body content.
const on012FixtureBodyWindow = 30

// on012FixtureLoadLines opens specFile and returns all lines.
func on012FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on012FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on012FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on012FixtureON012BodyLines returns the lines comprising the ON-012 body.
func on012FixtureON012BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on012FixtureON012Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-012 heading not found; expected '#### ON-012 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on012FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on012FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on012FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on012FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON012ImprovementPauseIsSubtypeOfPause is the binding test for hk-sx9r.14.
func TestON012ImprovementPauseIsSubtypeOfPause(t *testing.T) {
	t.Parallel()

	specFile := on012FixtureOperatorNFRPath(t)
	lines := on012FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on012FixtureON012BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-012 check(1): %s", reason)
	}
	t.Logf("ON-012 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "must-not-introduce-new-state",
			needle: "MUST NOT introduce a new operator-control state",
			detail: "ON-012 body must declare 'MUST NOT introduce a new operator-control state' " +
				"(expected phrase 'MUST NOT introduce a new operator-control state'); this is the " +
				"root constraint: the improvement cycle is forbidden from extending the §7.1 state " +
				"table with distinct improvement-pausing or improvement-paused states",
		},
		{
			id:     "3",
			label:  "pause-reason-discriminator",
			needle: "pause_reason",
			detail: "ON-012 body must name 'pause_reason' as the discriminator mechanism " +
				"(expected phrase 'pause_reason'); the pause_reason field on operator_pause_status " +
				"is the sole mechanism for distinguishing operator-initiated from improvement-initiated " +
				"pauses within the shared pausing/paused state pair",
		},
		{
			id:     "4",
			label:  "pause-reason-operator-value",
			needle: "pause_reason=operator",
			detail: "ON-012 body must name 'pause_reason=operator' as the operator-initiated value " +
				"(expected phrase 'pause_reason=operator'); naming both discriminator values is required " +
				"so implementers know the full domain of the pause_reason field",
		},
		{
			id:     "5",
			label:  "pause-reason-improvement-value",
			needle: "pause_reason=improvement",
			detail: "ON-012 body must name 'pause_reason=improvement' as the improvement-initiated value " +
				"(expected phrase 'pause_reason=improvement'); this is the value the S09 improvement " +
				"cycle injects when initiating a pause, distinguishing it from operator-pause",
		},
		{
			id:     "6",
			label:  "auto-resume-no-operator-action",
			needle: "no operator action required",
			detail: "ON-012 body must declare that the paused→resuming transition occurs automatically " +
				"when the improvement loop completes with 'no operator action required' " +
				"(expected phrase 'no operator action required'); this is the key behavioral difference " +
				"between an improvement-pause and an operator-pause: operators don't have to resume",
		},
		{
			id:     "7",
			label:  "not-a-separate-pair-of-states",
			needle: "NOT a separate pair of state-machine states",
			detail: "ON-012 body must declare 'NOT a separate pair of state-machine states' " +
				"(expected phrase 'NOT a separate pair of state-machine states'); this closes the " +
				"retired improvement-pausing/improvement-paused framing and prevents future drift " +
				"back to the earlier two-state design",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on012FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-012 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-012 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in ON-012 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on012FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-012 check(8) FAILED: Tags: mechanism not found in ON-012 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-012 body)\n"+
					"  detail: ON-012 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.14 audit complete — ON-012 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
