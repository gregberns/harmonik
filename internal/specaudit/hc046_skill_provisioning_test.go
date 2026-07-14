//go:build specaudit

package specaudit_test

// hk-8i31.55 binding test — HC-046 + HC-047 skill provisioning + resolution surface
// (coalesced per §2.3).
//
// Spec ref: specs/handler-contract.md §4.11 HC-046, HC-047.
//
// HC-046 states: a handler MUST ensure the agent subprocess has every skill named in
// LaunchSpec.required_skills[] available before the subprocess emits agent_ready.
// HC-047 states: skill resolution MUST be deterministic — resolve the name against
// LaunchSpec.skill_search_paths[] in order, take the first match. No cognition participates.
//
// # Audit frame
//
// This test is a spec-corpus sensor. Both HC-046 and HC-047 are verified together
// (coalesced per §2.3). The sensor verifies that both are correctly declared so that:
//
//  1. HC-046 heading is present in specs/handler-contract.md.
//  2. "required_skills[]" must be available before "agent_ready" is declared.
//  3. "HC-047" heading is present in specs/handler-contract.md.
//  4. "deterministic" skill resolution is declared.
//  5. "skill_search_paths[]" is named as the resolution source.
//  6. "No cognition participates" in skill resolution is declared.
//  7. Tags: mechanism is present in the HC-046 body window.
//
// # Failure modes
//
//   - HC-046 heading missing.
//   - required_skills[] before agent_ready absent.
//   - HC-047 heading missing.
//   - deterministic resolution absent.
//   - skill_search_paths[] absent.
//   - No cognition participates absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc046Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// hc046FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc046FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc046FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc046FixtureHC046Heading matches the HC-046 level-4 requirement heading line.
var hc046FixtureHC046Heading = regexp.MustCompile(`^#### HC-046 —`)

// hc046FixtureHC047Heading matches the HC-047 level-4 requirement heading line.
var hc046FixtureHC047Heading = regexp.MustCompile(`^#### HC-047 —`)

// hc046FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc046FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc046FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc046FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc046FixtureBodyWindow is the maximum number of lines to scan after a heading.
const hc046FixtureBodyWindow = 30

// hc046FixtureLoadLines opens specFile and returns all lines.
func hc046FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc046FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc046FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc046FixtureBodyLines returns the body lines for the given heading pattern.
func hc046FixtureBodyLines(lines []string, headingPattern *regexp.Regexp, reqID string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if headingPattern.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, reqID + " heading not found in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc046FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc046FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc046FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc046FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC046SkillProvisioningResolution is the binding test for hk-8i31.55.
func TestHC046SkillProvisioningResolution(t *testing.T) {
	t.Parallel()

	specFile := hc046FixtureHandlerContractPath(t)
	lines := hc046FixtureLoadLines(t, specFile)

	hc046Body, hc046LineNo, hc046Reason := hc046FixtureBodyLines(lines, hc046FixtureHC046Heading, "HC-046")
	if hc046Reason != "" {
		t.Fatalf("HC-046 check(1): %s", hc046Reason)
	}
	t.Logf("HC-046 heading found at specs/handler-contract.md line %d; body window = %d lines",
		hc046LineNo, len(hc046Body))

	hc047Body, hc047LineNo, hc047Reason := hc046FixtureBodyLines(lines, hc046FixtureHC047Heading, "HC-047")
	if hc047Reason != "" {
		t.Fatalf("HC-047 check(3): %s", hc047Reason)
	}
	t.Logf("HC-047 heading found at specs/handler-contract.md line %d; body window = %d lines",
		hc047LineNo, len(hc047Body))

	// HC-046 body checks.
	t.Run("check-2-required-skills-before-agent-ready", func(t *testing.T) {
		t.Parallel()
		if !hc046FixtureBodyContains(hc046Body, "required_skills") || !hc046FixtureBodyContains(hc046Body, "agent_ready") {
			t.Errorf("HC-046 check(2) FAILED: required_skills[] before agent_ready absent\n"+
				"  spec: specs/handler-contract.md line ~%d (HC-046 body)\n"+
				"  detail: HC-046 MUST declare required_skills[] must be available before agent_ready — "+
				"this ordering ensures the agent subprocess finds its skills already provisioned "+
				"when it begins work", hc046LineNo)
		}
	})

	// HC-047 body checks.
	t.Run("check-4-deterministic-resolution", func(t *testing.T) {
		t.Parallel()
		if !hc046FixtureBodyContains(hc047Body, "deterministic") {
			t.Errorf("HC-047 check(4) FAILED: deterministic resolution absent\n"+
				"  spec: specs/handler-contract.md line ~%d (HC-047 body)\n"+
				"  detail: HC-047 MUST declare skill resolution is 'deterministic' — no random or "+
				"cognitive selection; first match in skill_search_paths[] wins always", hc047LineNo)
		}
	})
	t.Run("check-5-skill-search-paths", func(t *testing.T) {
		t.Parallel()
		if !hc046FixtureBodyContains(hc047Body, "skill_search_paths") {
			t.Errorf("HC-047 check(5) FAILED: skill_search_paths[] absent\n"+
				"  spec: specs/handler-contract.md line ~%d (HC-047 body)\n"+
				"  detail: HC-047 MUST name 'skill_search_paths[]' as the resolution source — "+
				"the ordered list in LaunchSpec that the resolver walks to find each skill package", hc047LineNo)
		}
	})
	t.Run("check-6-no-cognition-participates", func(t *testing.T) {
		t.Parallel()
		if !hc046FixtureBodyContains(hc047Body, "No cognition participates") {
			t.Errorf("HC-047 check(6) FAILED: No cognition participates absent\n"+
				"  spec: specs/handler-contract.md line ~%d (HC-047 body)\n"+
				"  detail: HC-047 MUST declare 'No cognition participates' in skill resolution — "+
				"this is the mechanism-tag declaration; skill resolution is a deterministic "+
				"filesystem lookup, not an LLM decision", hc047LineNo)
		}
	})

	// Check (7): Tags: mechanism in HC-046 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range hc046Body {
			if hc046FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-046 check(7) FAILED: Tags: mechanism not found in HC-046 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-046 body)\n"+
					"  detail: HC-046 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				hc046LineNo,
			)
		}
	})

	t.Logf("hk-8i31.55 audit complete — HC-046 at line %d, HC-047 at line %d",
		hc046LineNo, hc047LineNo)
}
