//go:build specaudit

package specaudit_test

// hk-hqwn.18 binding test — EV-014 subscription-time class conflict is a startup error.
//
// Spec ref: specs/event-model.md §4.3 EV-014.
//
// EV-014 states: if two `synchronous` consumers for the same event type
// register, the daemon MUST fail startup with a typed configuration error.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The class-conflict startup-error
// implementation is pending; this sensor verifies that the EV-014 requirement
// is correctly declared in the spec so that:
//
//  1. EV-014 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. Two synchronous consumers for same event type is declared — the conflict
//     condition is explicitly two `synchronous` consumers for the same event type.
//
//  3. Daemon MUST fail startup is declared — the failure mode is startup
//     failure, not runtime rejection.
//
//  4. Typed configuration error is declared — the failure is surfaced as a
//     typed configuration error, not a generic error.
//
//  5. Tags: mechanism is present in the EV-014 body window.
//
// # Failure modes
//
//   - EV-014 heading missing: EV-014 heading not found in specs/event-model.md.
//   - Two-synchronous-consumers condition absent: two synchronous consumers condition not declared.
//   - Daemon-must-fail-startup absent: daemon MUST fail startup not declared.
//   - Typed-configuration-error absent: typed configuration error not declared.
//   - Tags: mechanism missing from EV-014 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn18Fixture prefix per
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

// hqwn18FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn18_class_conflict_startup_error_test.go
//
// so the repo root is two directories up.
func hqwn18FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn18FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn18FixtureEV014Heading matches the EV-014 level-4 requirement heading line.
// Note: the pattern uses a word boundary after "014" to avoid matching EV-014a, EV-014b, etc.
var hqwn18FixtureEV014Heading = regexp.MustCompile(`^#### EV-014 —`)

// hqwn18FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-014 requirement body window.
var hqwn18FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn18FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn18FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn18FixtureBodyWindow is the maximum number of lines after the EV-014
// heading to scan for requirement-body content.
const hqwn18FixtureBodyWindow = 30

// hqwn18FixtureLoadLines opens specFile and returns all lines.
func hqwn18FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn18FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn18FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn18FixtureEV014BodyLines returns the lines comprising the EV-014
// requirement body: all lines after the EV-014 heading up to (but not
// including) the next Markdown heading or hqwn18FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-014 heading is not found.
func hqwn18FixtureEV014BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn18FixtureEV014Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-014 heading not found; expected '#### EV-014 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn18FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn18FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn18FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn18FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN18ClassConflictStartupError is the binding test for hk-hqwn.18.
//
// It opens specs/event-model.md, locates the EV-014 heading, and validates:
//
//	(1) The EV-014 heading is present (the requirement exists in the spec).
//	(2) Two synchronous consumers for same event type condition is declared.
//	(3) Daemon MUST fail startup is declared.
//	(4) Typed configuration error is declared.
//	(5) Tags: mechanism is present in the body window.
func TestHQWN18ClassConflictStartupError(t *testing.T) {
	t.Parallel()

	specFile := hqwn18FixtureEventModelPath(t)
	lines := hqwn18FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn18FixtureEV014BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-014 check(1): %s", reason)
	}
	t.Logf("EV-014 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "two-synchronous-consumers-condition",
			needle: "synchronous",
			detail: "EV-014 body must declare that the conflict condition is two `synchronous` " +
				"consumers for the same event type (expected phrase 'synchronous'); this is the " +
				"specific class that triggers the startup-failure gate, not asynchronous or observer",
		},
		{
			id:     "3",
			label:  "daemon-must-fail-startup",
			needle: "MUST fail startup",
			detail: "EV-014 body must declare that the daemon MUST fail startup on the class " +
				"conflict (expected phrase 'MUST fail startup'); this ensures the conflict is " +
				"caught at startup time before any event processing begins, not silently at " +
				"registration time or deferred to runtime",
		},
		{
			id:     "4",
			label:  "typed-configuration-error",
			needle: "typed configuration error",
			detail: "EV-014 body must declare that the startup failure is a typed configuration " +
				"error (expected phrase 'typed configuration error'); this is the mechanism by " +
				"which operators can distinguish a synchronous-conflict error from other startup " +
				"failures and write targeted error handlers",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn18FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-014 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-014 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (5): Tags: mechanism in EV-014 body.
	t.Run("check-5-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn18FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-014 check(5) FAILED: Tags: mechanism not found in EV-014 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-014 body)\n"+
					"  detail: EV-014 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.18 audit complete — EV-014 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
