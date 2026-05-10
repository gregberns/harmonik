package specaudit_test

// hk-8i31.60 binding test — HC-050 skill declarations are read from LaunchSpec.
//
// Spec ref: specs/handler-contract.md §4.11 HC-050.
//
// HC-050 states: the handler MUST consult LaunchSpec.required_skills[] and
// LaunchSpec.skill_search_paths[] only; it MUST NOT read DOT node attributes or YAML
// policy documents directly. The workflow-load-time resolution per execution-model.md §4.9
// and control-points.md §4.11 is what populates LaunchSpec; the handler consumes the
// resolved set.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler skill implementation is pending; this
// sensor verifies that HC-050 is correctly declared in the spec so that:
//
//  1. HC-050 heading is present in specs/handler-contract.md.
//  2. "required_skills[]" and "skill_search_paths[]" are the only sources.
//  3. "MUST NOT read DOT node attributes" is declared.
//  4. "YAML policy documents" is named as another forbidden source.
//  5. Tags: mechanism is present in the HC-050 body window.
//
// # Failure modes
//
//   - HC-050 heading missing.
//   - required_skills[] and skill_search_paths[] as only sources absent.
//   - MUST NOT read DOT node attributes absent.
//   - YAML policy documents absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc050Fixture prefix per
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

// hc050FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc050FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc050FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc050FixtureHC050Heading matches the HC-050 level-4 requirement heading line.
var hc050FixtureHC050Heading = regexp.MustCompile(`^#### HC-050 —`)

// hc050FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc050FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc050FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc050FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc050FixtureBodyWindow is the maximum number of lines after the HC-050
// heading to scan for requirement-body content.
const hc050FixtureBodyWindow = 30

// hc050FixtureLoadLines opens specFile and returns all lines.
func hc050FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc050FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc050FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc050FixtureHC050BodyLines returns the lines comprising the HC-050 body.
func hc050FixtureHC050BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc050FixtureHC050Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-050 heading not found; expected '#### HC-050 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc050FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc050FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc050FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc050FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC050SkillDeclarationsFromLaunchSpec is the binding test for hk-8i31.60.
func TestHC050SkillDeclarationsFromLaunchSpec(t *testing.T) {
	t.Parallel()

	specFile := hc050FixtureHandlerContractPath(t)
	lines := hc050FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc050FixtureHC050BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-050 check(1): %s", reason)
	}
	t.Logf("HC-050 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "launchspec-required-skills-and-search-paths",
			needle: "required_skills[]",
			detail: "HC-050 body must name 'required_skills[]' as the only skill source " +
				"(expected phrase 'required_skills[]'); the handler reads skills exclusively from " +
				"LaunchSpec.required_skills[] and LaunchSpec.skill_search_paths[] — this seals the " +
				"handler against direct policy reads which would couple it to the control-plane",
		},
		{
			id:     "3",
			label:  "must-not-read-dot-node-attributes",
			needle: "MUST NOT read DOT node attributes",
			detail: "HC-050 body must declare 'MUST NOT read DOT node attributes' " +
				"(expected phrase 'MUST NOT read DOT node attributes'); DOT node attributes are " +
				"workflow-definition artifacts that the execution model resolves at load time — " +
				"the handler MUST NOT bypass this resolution layer by reading the DOT directly",
		},
		{
			id:     "4",
			label:  "yaml-policy-documents-forbidden",
			needle: "YAML policy documents",
			detail: "HC-050 body must name 'YAML policy documents' as a forbidden source " +
				"(expected phrase 'YAML policy documents'); YAML policies are resolved by " +
				"control-points.md §4.11 at load time and the results land in LaunchSpec — " +
				"a handler that reads YAML directly would bypass the resolution layer and " +
				"could observe different skills than those actually provisioned",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc050FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-050 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-050 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (5): Tags: mechanism in HC-050 body.
	t.Run("check-5-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc050FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-050 check(5) FAILED: Tags: mechanism not found in HC-050 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-050 body)\n"+
					"  detail: HC-050 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.60 audit complete — HC-050 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
