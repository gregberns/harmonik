package specaudit_test

// hk-8i31.40 binding test — HC-033 compile-time event-schema payload check.
//
// Spec ref: specs/handler-contract.md §4.7.HC-033.
//
// HC-033 states: the event-schema registry MUST verify at startup that no
// registered event type's payload schema declares a field whose name matches
// the regex of §4.7.HC-031. Any such field MUST be a startup-time error, not a
// runtime warning. This prevents schema drift that would silently ship
// unredacted secrets.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The startup-time registry scan
// implementation is pending; this sensor verifies that the HC-033 requirement
// is correctly declared in the spec so that:
//
//  1. HC-033 heading is present in specs/handler-contract.md — the requirement
//     exists in the normative spec.
//
//  2. Registry startup-time verification is declared — the event-schema registry
//     must perform its scan at startup, not lazily.
//
//  3. HC-031 regex reference is declared — the match condition is tied to the
//     normative secret-prefix regex of §4.7.HC-031.
//
//  4. Startup-time error (not runtime warning) is declared — any matched field
//     MUST cause a hard startup failure.
//
//  5. Tags: mechanism is present in the HC-033 body window.
//
// # Failure modes
//
//   - HC-033 heading missing: HC-033 heading not found in specs/handler-contract.md.
//   - Registry startup verification absent: event-schema registry at startup not stated.
//   - HC-031 regex reference absent: secret-prefix regex cross-reference not stated.
//   - Startup-time error clause absent: startup-time error vs. runtime warning not stated.
//   - Tags: mechanism missing from HC-033 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc033Fixture prefix per
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

// hc033FixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/hc033_event_schema_payload_check_test.go
//
// so the repo root is two directories up.
func hc033FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc033FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc033FixtureHC033Heading matches the HC-033 level-4 requirement heading line.
var hc033FixtureHC033Heading = regexp.MustCompile(`^#### HC-033 —`)

// hc033FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-033 requirement body window.
var hc033FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc033FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc033FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc033FixtureBodyWindow is the maximum number of lines after the HC-033
// heading to scan for requirement-body content.
const hc033FixtureBodyWindow = 30

// hc033FixtureLoadLines opens specFile and returns all lines.
func hc033FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc033FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc033FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc033FixtureHC033BodyLines returns the lines comprising the HC-033 requirement
// body: all lines after the HC-033 heading line up to (but not including) the
// next Markdown heading or hc033FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the HC-033 heading is not found.
func hc033FixtureHC033BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc033FixtureHC033Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-033 heading not found; expected '#### HC-033 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc033FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hc033FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc033FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hc033FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC033EventSchemaPayloadCheck is the binding test for hk-8i31.40.
//
// It opens specs/handler-contract.md, locates the HC-033 heading, and validates:
//
//	(a) The HC-033 heading is present (the requirement exists in the spec).
//	(b) Event-schema registry verifies at startup declared.
//	(c) HC-031 regex cross-reference declared.
//	(d) Startup-time error (not runtime warning) declared.
//	(e) Tags: mechanism is present in the body window.
func TestHC033EventSchemaPayloadCheck(t *testing.T) {
	t.Parallel()

	specFile := hc033FixtureHandlerContractPath(t)
	lines := hc033FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc033FixtureHC033BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-033 check(a): %s", reason)
	}
	t.Logf("HC-033 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "event-schema registry verifies at startup",
			needle: "event-schema registry",
			detail: "HC-033 body must declare that the event-schema registry performs its scan at " +
				"startup (expected phrase 'event-schema registry'); the scan MUST be startup-time " +
				"to catch secret-named payload fields before any event is emitted",
		},
		{
			id:     "c",
			label:  "HC-031 regex cross-reference",
			needle: "HC-031",
			detail: "HC-033 body must reference HC-031 as the normative regex that determines " +
				"which field names are secret-shaped (expected phrase 'HC-031'); this cross-reference " +
				"ties the schema-check to the same rule used by the redaction middleware, ensuring " +
				"the two enforcement surfaces stay in sync",
		},
		{
			id:     "d",
			label:  "startup-time error not runtime warning",
			needle: "startup-time error",
			detail: "HC-033 body must state that any matched field MUST be a startup-time error, " +
				"not a runtime warning (expected phrase 'startup-time error'); this distinguishes " +
				"hard startup failure from a log-only warning and makes the enforcement non-optional",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc033FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-033 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-033 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (e): Tags: mechanism in HC-033 body.
	t.Run("check-e-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc033FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-033 check(e) FAILED: Tags: mechanism not found in HC-033 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-033 body)\n"+
					"  detail: HC-033 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.40 audit complete — HC-033 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
