package operatornfr_test

// hk-sx9r.28 binding test — ON-023 compile-time payload-schema check.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-023.
//
// ON-023 states: event payload schemas declared per [event-model.md §6.3]
// MUST NOT declare any field typed as `Secret` or equivalent. A compile-time
// check (lint pass or generated-code assertion) MUST reject any payload schema
// that would carry a secret through the event bus. This closes the
// redaction-obligation loop: redaction cannot be forgotten at an emission site
// because no emission site is permitted to carry secret-typed fields.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The compile-time schema linter
// implementation is pending; this sensor verifies that the ON-023 requirement
// is correctly declared in the spec so that:
//
//  1. ON-023 heading is present in specs/operator-nfr.md — the requirement
//     exists in the normative spec.
//
//  2. No-Secret-field obligation is declared — payload schemas MUST NOT
//     declare any field typed as `Secret` or equivalent.
//
//  3. Compile-time check mechanism is declared — the enforcement mechanism is
//     named as a lint pass or generated-code assertion.
//
//  4. Reject-payload obligation is declared — the check MUST reject any
//     payload schema that would carry a secret through the event bus.
//
//  5. Tags: mechanism is present in the ON-023 body window.
//
// # Failure modes
//
//   - ON-023 heading missing: ON-023 heading not found in specs/operator-nfr.md.
//   - No-Secret-field obligation absent: the payload schema MUST NOT field
//     typed as Secret constraint not stated.
//   - Compile-time check mechanism absent: lint pass or generated-code
//     assertion mechanism not named.
//   - Reject-payload obligation absent: the reject-any-payload statement not
//     present.
//   - Tags: mechanism missing from ON-023 body window.
//
// # SUBSUMED scope note
//
// TestON023_SpecSectionExists in securitydrain_sx9r80_test.go already performs
// a thin two-string existence check ("ON-023" and "compile-time" anywhere in
// the file). This sensor supersedes that thin check with a body-window scan
// of the ON-023 section, verifying each normative obligation separately.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sx9r28Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sx9r28FixtureOperatorNFRPath returns the absolute path to
// specs/operator-nfr.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/operatornfr/payloadschemacheck_sx9r28_test.go
//
// so the repo root is two directories up.
func sx9r28FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	root := obligationsFixtureRepoRoot(t)
	return filepath.Join(root, "specs", "operator-nfr.md")
}

// sx9r28FixtureON023Heading matches the ON-023 level-4 requirement heading
// line in operator-nfr.md.
var sx9r28FixtureON023Heading = regexp.MustCompile(`^#### ON-023 —`)

// sx9r28FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the ON-023 requirement body window.
var sx9r28FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sx9r28FixtureTagsMechanism matches a "Tags: mechanism" line.
var sx9r28FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sx9r28FixtureBodyWindow is the maximum number of lines after the ON-023
// heading to scan for requirement-body content.
const sx9r28FixtureBodyWindow = 30

// sx9r28FixtureLoadLines opens specFile and returns all lines.
func sx9r28FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from obligationsFixtureRepoRoot (runtime.Caller) + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sx9r28FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sx9r28FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sx9r28FixtureON023BodyLines returns the lines comprising the ON-023
// requirement body: all lines after the ON-023 heading line up to (but not
// including) the next Markdown heading or sx9r28FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the ON-023 heading is not
// found.
func sx9r28FixtureON023BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sx9r28FixtureON023Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-023 heading not found; expected '#### ON-023 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + sx9r28FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if sx9r28FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sx9r28FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func sx9r28FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSX9R28_ON023PayloadSchemaCheck is the binding test for hk-sx9r.28.
//
// It opens specs/operator-nfr.md, locates the ON-023 heading, and validates:
//
//	(a) The ON-023 heading is present (the requirement exists in the spec).
//	(b) No-Secret-field obligation declared: payload schemas MUST NOT declare
//	    any field typed as `Secret` or equivalent.
//	(c) Compile-time check mechanism declared: lint pass or generated-code
//	    assertion is named as the enforcement mechanism.
//	(d) Reject-payload obligation declared: the check MUST reject any payload
//	    schema that would carry a secret through the event bus.
//	(e) Tags: mechanism is present in the body window.
func TestSX9R28_ON023PayloadSchemaCheck(t *testing.T) {
	t.Parallel()

	specFile := sx9r28FixtureOperatorNFRPath(t)
	lines := sx9r28FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sx9r28FixtureON023BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-023 check(a): %s", reason)
	}
	t.Logf("ON-023 heading found at specs/operator-nfr.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:    "b",
			label: "payload schemas MUST NOT declare field typed as Secret",
			// The spec body uses the phrase "MUST NOT declare any field typed as"
			// adjacent to the word "Secret"; match the typed-field constraint.
			needle: "MUST NOT declare",
			detail: "ON-023 body must state that event payload schemas MUST NOT declare any " +
				"field typed as `Secret` or equivalent (expected phrase 'MUST NOT declare'); " +
				"this is the source-level prohibition that closes the redaction-obligation loop",
		},
		{
			id:     "c",
			label:  "compile-time check mechanism declared",
			needle: "compile-time check",
			detail: "ON-023 body must name the enforcement mechanism as a 'compile-time check' " +
				"(expected phrase 'compile-time check'); naming the mechanism type (lint pass " +
				"or generated-code assertion) is normative — a runtime check does not satisfy " +
				"the structural-guardrail requirement",
		},
		{
			id:     "d",
			label:  "reject any payload schema that would carry a secret",
			needle: "reject any payload schema",
			detail: "ON-023 body must state that the check MUST reject any payload schema " +
				"that would carry a secret through the event bus (expected phrase 'reject any " +
				"payload schema'); this is the per-schema rejection obligation, distinct from " +
				"per-field typing — both are required to fully specify ON-023",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !sx9r28FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-023 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-023 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (e): Tags: mechanism in ON-023 body.
	t.Run("check-e-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sx9r28FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-023 check(e) FAILED: Tags: mechanism not found in ON-023 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-023 body)\n"+
					"  detail: ON-023 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.28 audit complete — ON-023 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
