package specaudit_test

// hk-hqwn.47 binding test — EV-INV-001 events are observational, not authoritative.
//
// Spec ref: specs/event-model.md §5 EV-INV-001.
//
// EV-INV-001 states: the JSONL event log MUST NEVER be treated as authoritative state.
// Git plus Beads is authoritative per [execution-model.md §4.7 EM-INV-001].
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that EV-INV-001 is correctly declared
// in the spec so that:
//
//  1. EV-INV-001 heading is present in specs/event-model.md.
//  2. "MUST NEVER be treated as authoritative state" is declared.
//  3. "Git plus Beads is authoritative" is cited as the alternative.
//  4. "EM-INV-001" is cross-referenced as the execution-model pairing.
//  5. Tags: mechanism is present in the EV-INV-001 body window.
//
// # Failure modes
//
//   - EV-INV-001 heading missing.
//   - MUST NEVER be treated as authoritative state absent.
//   - Git plus Beads authoritative absent.
//   - EM-INV-001 cross-reference absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the evInv001Fixture prefix per
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

// evInv001FixtureEventModelPath returns the absolute path to specs/event-model.md.
func evInv001FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("evInv001FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// evInv001FixtureHeading matches the EV-INV-001 level-4 requirement heading line.
var evInv001FixtureHeading = regexp.MustCompile(`^#### EV-INV-001 —`)

// evInv001FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var evInv001FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// evInv001FixtureTagsMechanism matches a "Tags: mechanism" line.
var evInv001FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// evInv001FixtureBodyWindow is the maximum number of lines to scan after the heading.
const evInv001FixtureBodyWindow = 30

// evInv001FixtureLoadLines opens specFile and returns all lines.
func evInv001FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("evInv001FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("evInv001FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// evInv001FixtureBodyLines returns the lines comprising the EV-INV-001 body.
func evInv001FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if evInv001FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-INV-001 heading not found; expected '#### EV-INV-001 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + evInv001FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if evInv001FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// evInv001FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func evInv001FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestEVINV001EventsAreObservationalNotAuthoritative is the binding test for hk-hqwn.47.
func TestEVINV001EventsAreObservationalNotAuthoritative(t *testing.T) {
	t.Parallel()

	specFile := evInv001FixtureEventModelPath(t)
	lines := evInv001FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := evInv001FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-INV-001 check(1): %s", reason)
	}
	t.Logf("EV-INV-001 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "must-never-be-authoritative-state",
			needle: "MUST NEVER be treated as authoritative state",
			detail: "EV-INV-001 body must declare 'MUST NEVER be treated as authoritative state' " +
				"(expected phrase 'MUST NEVER be treated as authoritative state'); this is the " +
				"foundational invariant that separates harmonik's event system from event-sourcing: " +
				"the JSONL log is observational, not the source of truth for workflow state",
		},
		{
			id:     "3",
			label:  "git-plus-beads-is-authoritative",
			needle: "Git plus Beads is authoritative",
			detail: "EV-INV-001 body must declare 'Git plus Beads is authoritative' " +
				"(expected phrase 'Git plus Beads is authoritative'); naming the authoritative " +
				"stores closes the invariant: if JSONL is NOT authoritative, then git+Beads is — " +
				"this is locked decision #12 per STATUS.md",
		},
		{
			id:     "4",
			label:  "em-inv-001-cross-reference",
			needle: "EM-INV-001",
			detail: "EV-INV-001 body must cite 'EM-INV-001' as the execution-model pairing " +
				"(expected phrase 'EM-INV-001'); EM-INV-001 (execution-model.md §4.7) is the " +
				"peer invariant on the execution-model side — the two together form the " +
				"cross-subsystem no-event-sourcing contract",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !evInv001FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-INV-001 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-INV-001 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (5): Tags: mechanism in EV-INV-001 body.
	t.Run("check-5-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if evInv001FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-INV-001 check(5) FAILED: Tags: mechanism not found in EV-INV-001 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-INV-001 body)\n"+
					"  detail: EV-INV-001 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.47 audit complete — EV-INV-001 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
