//go:build specaudit

package specaudit_test

// hk-hqwn.29 binding test — EV-020 JSONL writes MUST be append-only.
//
// Spec ref: specs/event-model.md §4.4 EV-020.
//
// EV-020 states: The JSONL writer MUST NOT rewrite, truncate, or reorder
// existing lines. Corruption (partial-line write on crash) is detected by
// readers per §6.2.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The JSONL writer implementation is
// pending; this sensor verifies that the EV-020 requirement is correctly
// declared in the spec so that:
//
//  1. EV-020 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. "MUST NOT rewrite" is declared in the EV-020 body — the prohibition on
//     rewriting existing lines is explicit.
//
//  3. "truncate" is declared in the EV-020 body — the prohibition on
//     truncating existing lines is explicit.
//
//  4. "reorder" is declared in the EV-020 body — the prohibition on
//     reordering existing lines is explicit.
//
//  5. "partial-line write on crash" is declared in the EV-020 body — the
//     corruption detection rationale (torn-tail on crash) is named.
//
//  6. "§6.2" is cited in the EV-020 body — the cross-reference to the
//     read-recovery rules that detect the corruption is present.
//
//  7. Tags: mechanism is present in the EV-020 body window.
//
// # Failure modes
//
//   - EV-020 heading missing: EV-020 heading not found in specs/event-model.md.
//   - "MUST NOT rewrite" absent: rewrite prohibition not declared in EV-020 body.
//   - "truncate" absent: truncation prohibition not declared in EV-020 body.
//   - "reorder" absent: reorder prohibition not declared in EV-020 body.
//   - "partial-line write on crash" absent: corruption rationale not named.
//   - "§6.2" absent: cross-reference to read-recovery rules not cited.
//   - Tags: mechanism missing from EV-020 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn29Fixture prefix per
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

// hqwn29FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn29_jsonl_append_only_test.go
//
// so the repo root is two directories up.
func hqwn29FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn29FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn29FixtureEV020Heading matches the EV-020 level-4 requirement heading line.
var hqwn29FixtureEV020Heading = regexp.MustCompile(`^#### EV-020 —`)

// hqwn29FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of a requirement body window.
var hqwn29FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn29FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn29FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn29FixtureBodyWindow is the maximum number of lines after a heading to
// scan for requirement-body content.
const hqwn29FixtureBodyWindow = 30

// hqwn29FixtureLoadLines opens specFile and returns all lines.
func hqwn29FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn29FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn29FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn29FixtureBodyLines returns requirement body lines: all lines after the
// matched heading up to (but not including) the next Markdown heading or
// hqwn29FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func hqwn29FixtureBodyLines(lines []string, headingPattern *regexp.Regexp, reqID string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if headingPattern.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, fmt.Sprintf("%s heading not found; expected '%s' pattern in specs/event-model.md", reqID, headingPattern.String())
	}

	limit := headingIdx + 1 + hqwn29FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement or section).
		if hqwn29FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn29FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn29FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN29JSONLWritesMustBeAppendOnly is the binding test for hk-hqwn.29.
//
// It opens specs/event-model.md, locates the EV-020 heading, and validates
// the seven audit checks listed in the file-level comment.
func TestHQWN29JSONLWritesMustBeAppendOnly(t *testing.T) {
	t.Parallel()

	specFile := hqwn29FixtureEventModelPath(t)
	lines := hqwn29FixtureLoadLines(t, specFile)

	// Check (1): EV-020 heading present.
	ev020Body, ev020LineNo, ev020Reason := hqwn29FixtureBodyLines(lines, hqwn29FixtureEV020Heading, "EV-020")
	if ev020Reason != "" {
		t.Fatalf("EV-020 check(1): %s", ev020Reason)
	}
	t.Logf("EV-020 heading found at specs/event-model.md line %d; body window = %d lines",
		ev020LineNo, len(ev020Body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	ev020Checks := []check{
		{
			id:     "2",
			label:  "MUST NOT rewrite prohibition",
			needle: "MUST NOT rewrite",
			detail: "EV-020 body must declare that the JSONL writer 'MUST NOT rewrite' existing lines; " +
				"this is the explicit prohibition that forbids in-place mutation of committed log content",
		},
		{
			id:     "3",
			label:  "truncate prohibition",
			needle: "truncate",
			detail: "EV-020 body must declare the prohibition on 'truncate'; " +
				"truncation of the JSONL file would destroy durable event history",
		},
		{
			id:     "4",
			label:  "reorder prohibition",
			needle: "reorder",
			detail: "EV-020 body must declare the prohibition on 'reorder'; " +
				"reordering existing lines would violate the partial-order contract of EV-008 " +
				"and break reader-recovery deduplication",
		},
		{
			id:     "5",
			label:  "partial-line write on crash corruption rationale",
			needle: "partial-line write on crash",
			detail: "EV-020 body must name 'partial-line write on crash' as the corruption scenario; " +
				"this is the torn-tail shape that readers must detect via §6.2 read-recovery rules",
		},
		{
			id:     "6",
			label:  "section 6.2 cross-reference for reader detection",
			needle: "§6.2",
			detail: "EV-020 body must cite §6.2 for the reader-side corruption detection; " +
				"this normative reference anchors the write-side invariant to the read-recovery rules " +
				"that handle partial-line corruption",
		},
	}

	for _, c := range ev020Checks {
		c := c
		t.Run(fmt.Sprintf("EV020-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn29FixtureBodyContains(ev020Body, c.needle) {
				t.Errorf(
					"EV-020 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-020 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, ev020LineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in EV-020 body.
	t.Run("EV020-check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range ev020Body {
			if hqwn29FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-020 check(7) FAILED: Tags: mechanism not found in EV-020 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-020 body)\n"+
					"  detail: EV-020 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				ev020LineNo,
			)
		}
	})

	t.Logf("hk-hqwn.29 audit complete — EV-020 heading at line %d (body %d lines)",
		ev020LineNo, len(ev020Body))
}
