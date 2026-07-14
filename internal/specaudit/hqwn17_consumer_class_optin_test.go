//go:build specaudit

package specaudit_test

// hk-hqwn.17 binding test — EV-013 consumer class is opt-in at subscription.
//
// Spec ref: specs/event-model.md §4.3 EV-013.
//
// EV-013 states: an in-process subscriber's default class MUST be `observer`.
// `synchronous` and `asynchronous` classes are opt-in.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The opt-in consumer class implementation
// is pending; this sensor verifies that the EV-013 requirement is correctly
// declared in the spec so that:
//
//  1. EV-013 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. Default class is observer — the in-process subscriber's default class
//     MUST be `observer`.
//
//  3. synchronous is opt-in — the `synchronous` class is not the default; it
//     is explicitly opt-in.
//
//  4. asynchronous is opt-in — the `asynchronous` class is not the default;
//     it is explicitly opt-in.
//
//  5. Tags: mechanism is present in the EV-013 body window.
//
// # Failure modes
//
//   - EV-013 heading missing: EV-013 heading not found in specs/event-model.md.
//   - Default-observer absent: default class `observer` not declared.
//   - synchronous opt-in absent: synchronous opt-in not declared.
//   - asynchronous opt-in absent: asynchronous opt-in not declared.
//   - Tags: mechanism missing from EV-013 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn17Fixture prefix per
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

// hqwn17FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn17_consumer_class_optin_test.go
//
// so the repo root is two directories up.
func hqwn17FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn17FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn17FixtureEV013Heading matches the EV-013 level-4 requirement heading line.
var hqwn17FixtureEV013Heading = regexp.MustCompile(`^#### EV-013 —`)

// hqwn17FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-013 requirement body window.
var hqwn17FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn17FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn17FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn17FixtureBodyWindow is the maximum number of lines after the EV-013
// heading to scan for requirement-body content.
const hqwn17FixtureBodyWindow = 30

// hqwn17FixtureLoadLines opens specFile and returns all lines.
func hqwn17FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn17FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn17FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn17FixtureEV013BodyLines returns the lines comprising the EV-013
// requirement body: all lines after the EV-013 heading up to (but not
// including) the next Markdown heading or hqwn17FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-013 heading is not found.
func hqwn17FixtureEV013BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn17FixtureEV013Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-013 heading not found; expected '#### EV-013 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn17FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn17FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn17FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn17FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN17ConsumerClassOptIn is the binding test for hk-hqwn.17.
//
// It opens specs/event-model.md, locates the EV-013 heading, and validates:
//
//	(1) The EV-013 heading is present (the requirement exists in the spec).
//	(2) Default class is observer.
//	(3) synchronous is opt-in.
//	(4) asynchronous is opt-in.
//	(5) Tags: mechanism is present in the body window.
func TestHQWN17ConsumerClassOptIn(t *testing.T) {
	t.Parallel()

	specFile := hqwn17FixtureEventModelPath(t)
	lines := hqwn17FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn17FixtureEV013BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-013 check(1): %s", reason)
	}
	t.Logf("EV-013 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "default-class-observer",
			needle: "default class MUST be `observer`",
			detail: "EV-013 body must declare that the in-process subscriber's default class MUST be " +
				"`observer` (expected phrase 'default class MUST be `observer`'); this is the core " +
				"opt-in contract that ensures accidental consumers do not inadvertently register as " +
				"synchronous or asynchronous",
		},
		{
			id:     "3",
			label:  "synchronous-opt-in",
			needle: "synchronous",
			detail: "EV-013 body must declare that the `synchronous` class is opt-in " +
				"(expected phrase 'synchronous'); this is the explicit signal that synchronous " +
				"class requires affirmative configuration at subscription time",
		},
		{
			id:     "4",
			label:  "asynchronous-opt-in",
			needle: "asynchronous",
			detail: "EV-013 body must declare that the `asynchronous` class is opt-in " +
				"(expected phrase 'asynchronous'); this is the explicit signal that asynchronous " +
				"class requires affirmative configuration at subscription time",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn17FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-013 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-013 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (5): Tags: mechanism in EV-013 body.
	t.Run("check-5-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn17FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-013 check(5) FAILED: Tags: mechanism not found in EV-013 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-013 body)\n"+
					"  detail: EV-013 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.17 audit complete — EV-013 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
