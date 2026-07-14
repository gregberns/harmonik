//go:build specaudit

package specaudit_test

// hk-i0tw.39 binding test — SH-INV-003 twin-only invariant.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-003.
//
// SH-INV-003 states: "Every handler binary launched during scenario execution
// MUST be a twin per [handler-contract.md §4.8.HC-035]; the harness MUST
// refuse to launch a real-model handler. The 'is a twin' predicate is: a
// binary is a twin iff its absolute resolved path is under the configured
// twin-binary search-path prefix per SH-009 (item (b)) OR the scenario's
// agent_overrides declared the binary AND the daemon's HC-043 commit-hash
// check passes against a registered twin entry."
//
// # Audit frame
//
// This test is a spec-corpus sensor. The harness implementation is pending;
// this sensor verifies that the SH-INV-003 requirement is correctly declared
// in the spec so that:
//
//  1. SH-INV-003 heading is present in specs/scenario-harness.md — the
//     invariant exists in the normative spec.
//
//  2. "is a twin" predicate phrasing is present — the spec defines the
//     predicate as path-prefix-based plus HC-043 hash verification, not
//     name-only heuristics.
//
//  3. Name-only heuristic prohibition: the spec explicitly states that
//     HasSuffix("-twin") is NOT sufficient and MUST NOT be used by the sensor.
//     This is the key safety invariant preventing weak identity checks.
//
//  4. Pre-launch check obligation: the spec declares a pre-launch check at
//     handler-config resolution (§4.3); not at orchestration time.
//
//  5. Failure class: a scenario whose resolved configuration would launch a
//     binary failing the predicate MUST fail with verdict=error and
//     failure_class=harness-internal-error.
//
//  6. Conformance test obligation: a scenario whose agent_overrides references
//     /usr/bin/claude (or any binary outside the search-path prefix) MUST
//     fail at the pre-launch check, NOT at orchestration time.
//
//  7. Tags: mechanism is present in the SH-INV-003 body window.
//
// # Failure modes
//
//   - Heading missing: SH-INV-003 heading not found in specs/scenario-harness.md.
//   - Predicate phrasing absent: "is a twin" predicate definition removed or renamed.
//   - HC-043 reference missing: commit-hash check requirement dropped.
//   - Suffix-heuristic prohibition absent: HasSuffix("-twin") prohibition missing.
//   - Pre-launch check absent: pre-launch at handler-config resolution not stated.
//   - Failure class absent: verdict=error / harness-internal-error not declared.
//   - Conformance test absent: /usr/bin/claude outside-prefix case not mentioned.
//   - Tags: mechanism missing from body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the shInv003Fixture prefix per
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

// shInv003FixtureScenarioHarnessPath returns the absolute path to
// specs/scenario-harness.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/shinv003_twin_only_test.go
//
// so the repo root is two directories up.
func shInv003FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("shInv003FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

// shInv003FixtureSHInv003Heading matches the SH-INV-003 level-4 requirement
// heading line.
var shInv003FixtureSHInv003Heading = regexp.MustCompile(`^#### SH-INV-003 —`)

// shInv003FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the SH-INV-003 requirement body window.
var shInv003FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// shInv003FixtureTagsMechanism matches a "Tags: mechanism" line.
var shInv003FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// shInv003FixtureBodyWindow is the maximum number of lines after the
// SH-INV-003 heading to scan for requirement-body content.
const shInv003FixtureBodyWindow = 30

// shInv003FixtureLoadLines opens specFile and returns all lines.
func shInv003FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("shInv003FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("shInv003FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// shInv003FixtureBodyLines returns the lines comprising the SH-INV-003
// requirement body: all lines after the SH-INV-003 heading line up to (but
// not including) the next Markdown heading or shInv003FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the SH-INV-003 heading is not
// found.
func shInv003FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if shInv003FixtureSHInv003Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-INV-003 heading not found; expected '#### SH-INV-003 —' in specs/scenario-harness.md"
	}

	limit := headingIdx + 1 + shInv003FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if shInv003FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// shInv003FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func shInv003FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSHInv003TwinOnlyHarnessInvariant is the binding test for SH-INV-003.
//
// It opens specs/scenario-harness.md, locates the SH-INV-003 heading, and
// validates:
//
//	(a) The SH-INV-003 heading is present (the invariant exists in the spec).
//	(b) The "is a twin" predicate phrasing: path-prefix + HC-043 hash.
//	(c) HC-043 commit-hash check is referenced (hash verification component).
//	(d) Name-only heuristic prohibition: HasSuffix("-twin") MUST NOT be used.
//	(e) Pre-launch check at handler-config resolution (§4.3) is declared.
//	(f) Failure class: verdict=error / failure_class=harness-internal-error
//	    declared for binaries failing the predicate.
//	(g) Conformance test obligation: /usr/bin/claude (outside prefix) MUST
//	    fail at the pre-launch check, NOT at orchestration time.
//	(h) Tags: mechanism is present in the body window.
func TestSHInv003TwinOnlyHarnessInvariant(t *testing.T) {
	t.Parallel()

	specFile := shInv003FixtureScenarioHarnessPath(t)
	lines := shInv003FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := shInv003FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-INV-003 check(a): %s", reason)
	}
	t.Logf("SH-INV-003 heading found at specs/scenario-harness.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string // substring to search for (case-insensitive)
		detail string // shown on failure
	}

	checks := []check{
		{
			id:     "b",
			label:  "is-a-twin predicate phrasing",
			needle: "is a twin",
			detail: "SH-INV-003 body must define the 'is a twin' predicate " +
				"(expected phrase 'is a twin'); the predicate is the load-bearing " +
				"contract for the pre-launch check implementation",
		},
		{
			id:     "c",
			label:  "HC-043 hash verification reference",
			needle: "HC-043",
			detail: "SH-INV-003 body must reference HC-043 (commit-hash check) as part of the " +
				"twin-identity predicate (expected phrase 'HC-043'); " +
				"name-only heuristics are explicitly prohibited — the hash check is " +
				"the normative second leg of the predicate",
		},
		{
			id:     "d",
			label:  "name-only heuristic prohibition (HasSuffix)",
			needle: "HasSuffix",
			detail: "SH-INV-003 body must prohibit the HasSuffix(\"-twin\") name heuristic " +
				"(expected phrase 'HasSuffix'); its explicit prohibition prevents " +
				"weak identity checks from satisfying the invariant",
		},
		{
			id:     "e",
			label:  "pre-launch check obligation",
			needle: "pre-launch check",
			detail: "SH-INV-003 body must declare a pre-launch check at handler-config resolution " +
				"(expected phrase 'pre-launch check'); this timing requirement means the " +
				"harness rejects non-twin binaries before orchestration begins",
		},
		{
			id:     "f",
			label:  "failure class harness-internal-error",
			needle: "harness-internal-error",
			detail: "SH-INV-003 body must declare that a scenario whose resolved configuration " +
				"would launch a non-twin binary MUST fail with failure_class=harness-internal-error " +
				"(expected phrase 'harness-internal-error'); this failure class is the " +
				"normative error surface for pre-launch predicate failures",
		},
		{
			id:     "g",
			label:  "conformance test /usr/bin/claude outside-prefix case",
			needle: "/usr/bin/claude",
			detail: "SH-INV-003 body must include the conformance test obligation naming " +
				"/usr/bin/claude (expected phrase '/usr/bin/claude') as the canonical " +
				"example of a binary outside the search-path prefix; this anchors the " +
				"sensor conformance test to a concrete non-twin path",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !shInv003FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-INV-003 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-INV-003 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (h): Tags: mechanism
	t.Run("check-h-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if shInv003FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-INV-003 check(h) FAILED: Tags: mechanism not found in SH-INV-003 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-INV-003 body)\n"+
					"  detail: SH-INV-003 carries tag 'mechanism'; its absence indicates the"+
					" requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-INV-003 audit complete — all checks evaluated (heading at line %d, body = %d lines)",
		headingLineNo, len(body))
}
