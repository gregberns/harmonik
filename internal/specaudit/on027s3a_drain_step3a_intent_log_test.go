//go:build specaudit

package specaudit_test

// hk-sx9r.36 binding test — ON-027 step 3a: br-CLI adapter intent-log drain.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 step 3a states: the br-CLI adapter intent-log per [beads-integration.md §4.10
// BI-029/BI-030] MUST be drained to completion: every pending intent-log entry's
// terminal-transition write MUST resolve (success or BI-031 status-check classification)
// before step 4 proceeds. Drain failures (e.g., BrUnavailable) escalate to step 4 with
// failed entries marked for next-restart Cat 3a routing. Bounded by timeout.step_3a per
// ON-029. Per ON-027a, durably marked.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain step-3a intent-log implementation is
// pending; this sensor verifies that ON-027 step 3a is correctly declared in the spec
// so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. Step 3a is present in the body (the "(3a)" marker).
//  3. "drained to completion" declares the obligation.
//  4. "terminal-transition write MUST resolve" is declared.
//  5. "BI-029" is cited as the intent-log mechanism.
//  6. "Cat 3a routing" names the escalation destination for failures.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - Step 3a marker absent.
//   - drained to completion absent.
//   - terminal-transition write MUST resolve absent.
//   - BI-029 citation absent.
//   - Cat 3a routing absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027s3aFixture prefix per
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

// on027s3aFixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027s3aFixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027s3aFixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027s3aFixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027s3aFixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027s3aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027s3aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027s3aFixtureTagsMechanism matches a "Tags: mechanism" line.
var on027s3aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027s3aFixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027s3aFixtureBodyWindow = 30

// on027s3aFixtureLoadLines opens specFile and returns all lines.
func on027s3aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027s3aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027s3aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027s3aFixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027s3aFixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027s3aFixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027s3aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027s3aFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027s3aFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027s3aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027Step3aBrCLIIntentLogDrain is the binding test for hk-sx9r.36.
func TestON027Step3aBrCLIIntentLogDrain(t *testing.T) {
	t.Parallel()

	specFile := on027s3aFixtureOperatorNFRPath(t)
	lines := on027s3aFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027s3aFixtureON027BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-027 check(1): %s", reason)
	}
	t.Logf("ON-027 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "step-3a-marker-present",
			needle: "(3a)",
			detail: "ON-027 body must contain the '(3a)' step marker " +
				"(expected phrase '(3a)'); step 3a is the br-CLI adapter intent-log drain, " +
				"inserted after step 3 (handler-subprocess wait) and before step 4 (event-bus " +
				"flush); its explicit step label ensures implementations cannot skip or merge it",
		},
		{
			id:     "3",
			label:  "drained-to-completion",
			needle: "drained to completion",
			detail: "ON-027 body must declare 'drained to completion' for step 3a " +
				"(expected phrase 'drained to completion'); this is the normative obligation: " +
				"every pending intent-log entry MUST be resolved before step 4 proceeds, with " +
				"no partial drain permitted",
		},
		{
			id:     "4",
			label:  "terminal-transition-write-must-resolve",
			needle: "terminal-transition write MUST resolve",
			detail: "ON-027 body must declare 'terminal-transition write MUST resolve' for step 3a " +
				"(expected phrase 'terminal-transition write MUST resolve'); this binds step 3a " +
				"to the beads-integration spec's completion semantics: each entry resolves to " +
				"success or BI-031 status-check classification before step 4",
		},
		{
			id:     "5",
			label:  "bi-029-citation",
			needle: "BI-029",
			detail: "ON-027 body must cite 'BI-029' for the intent-log mechanism " +
				"(expected phrase 'BI-029'); BI-029 and BI-030 (beads-integration.md §4.10) " +
				"own the intent-log contract that step 3a drains — the citation anchors the " +
				"drain obligation to the authoritative spec",
		},
		{
			id:     "6",
			label:  "cat-3a-routing-named",
			needle: "Cat 3a routing",
			detail: "ON-027 body must name 'Cat 3a routing' as the escalation destination " +
				"(expected phrase 'Cat 3a routing'); BrUnavailable failures in step 3a mark " +
				"failed entries for next-restart Cat 3a routing per reconciliation/spec.md — " +
				"they are not lost, but deferred to the next startup's reconciliation pass",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027s3aFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-027 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-027 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in ON-027 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on027s3aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-027 check(7) FAILED: Tags: mechanism not found in ON-027 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-027 body)\n"+
					"  detail: ON-027 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.36 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
