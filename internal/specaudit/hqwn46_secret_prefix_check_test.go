//go:build specaudit

package specaudit_test

// hk-hqwn.46 binding test — EV-036 compile-time check: no payload field name
// matches the secret-prefix rule.
//
// Spec ref: specs/event-model.md §4.10 EV-036.
//
// EV-036 states: at daemon startup the payload-type registry MUST be scanned;
// any registered payload type whose struct field names match the secret-prefix
// rule MUST cause startup to fail with a typed configuration error.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The payload-registry scanner implementation
// is pending; this sensor verifies that the EV-036 requirement is correctly
// declared in the spec so that:
//
//  1. EV-036 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. Payload-type registry scan at daemon startup is declared — the scan
//     must happen at startup, not lazily.
//
//  3. Secret-prefix rule match causes startup failure is declared — a payload
//     type whose field names match the rule MUST cause startup to fail.
//
//  4. Typed configuration error is declared — the failure surface is a typed
//     error (not a panic or untyped error).
//
//  5. Tags: mechanism is present in the EV-036 body window.
//
// # Failure modes
//
//   - EV-036 heading missing: EV-036 heading not found in specs/event-model.md.
//   - Registry scan absent: payload-type registry scan at startup not stated.
//   - Secret-prefix failure absent: startup failure for secret-prefix fields not stated.
//   - Typed configuration error absent: typed error class not named.
//   - Tags: mechanism missing from EV-036 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn46Fixture prefix per
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

// hqwn46FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn46_secret_prefix_check_test.go
//
// so the repo root is two directories up.
func hqwn46FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn46FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn46FixtureEV036Heading matches the EV-036 level-4 requirement heading line.
var hqwn46FixtureEV036Heading = regexp.MustCompile(`^#### EV-036 —`)

// hqwn46FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-036 requirement body window.
var hqwn46FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn46FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn46FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn46FixtureBodyWindow is the maximum number of lines after the EV-036
// heading to scan for requirement-body content.
const hqwn46FixtureBodyWindow = 30

// hqwn46FixtureLoadLines opens specFile and returns all lines.
func hqwn46FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn46FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn46FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn46FixtureEV036BodyLines returns the lines comprising the EV-036
// requirement body: all lines after the EV-036 heading line up to (but not
// including) the next Markdown heading or hqwn46FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-036 heading is not found.
func hqwn46FixtureEV036BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn46FixtureEV036Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-036 heading not found; expected '#### EV-036 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn46FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn46FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn46FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn46FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN46SecretPrefixCompileTimeCheck is the binding test for hk-hqwn.46.
//
// It opens specs/event-model.md, locates the EV-036 heading, and validates:
//
//	(a) The EV-036 heading is present (the requirement exists in the spec).
//	(b) Payload-type registry scan at daemon startup declared.
//	(c) Secret-prefix field match causes startup to fail declared.
//	(d) Typed configuration error named as the failure surface.
//	(e) Tags: mechanism is present in the body window.
func TestHQWN46SecretPrefixCompileTimeCheck(t *testing.T) {
	t.Parallel()

	specFile := hqwn46FixtureEventModelPath(t)
	lines := hqwn46FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn46FixtureEV036BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-036 check(a): %s", reason)
	}
	t.Logf("EV-036 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "payload-type registry scanned at daemon startup",
			needle: "payload-type registry",
			detail: "EV-036 body must declare that the payload-type registry is scanned at daemon " +
				"startup (expected phrase 'payload-type registry'); the scan timing is normative — " +
				"a lazy or deferred scan does not satisfy the startup-time error requirement",
		},
		{
			id:     "c",
			label:  "secret-prefix match causes startup failure",
			needle: "secret-prefix rule",
			detail: "EV-036 body must state that struct field names matching the secret-prefix rule " +
				"MUST cause startup to fail (expected phrase 'secret-prefix rule'); this is the " +
				"structural guardrail that prevents secret-named fields from appearing in " +
				"registered payload types",
		},
		{
			id:     "d",
			label:  "typed configuration error as failure surface",
			needle: "typed configuration error",
			detail: "EV-036 body must name 'typed configuration error' as the failure surface " +
				"(expected phrase 'typed configuration error'); this distinguishes the error " +
				"class from a panic or untyped error and allows callers to handle it programmatically",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn46FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-036 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-036 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (e): Tags: mechanism in EV-036 body.
	t.Run("check-e-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn46FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-036 check(e) FAILED: Tags: mechanism not found in EV-036 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-036 body)\n"+
					"  detail: EV-036 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.46 audit complete — EV-036 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
