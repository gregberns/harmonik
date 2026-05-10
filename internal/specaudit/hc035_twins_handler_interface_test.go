package specaudit_test

// hk-8i31.42 binding test — HC-035 twins (as real-handler substitutes) implement the
// same Handler interface.
//
// Spec ref: specs/handler-contract.md §4.8 HC-035.
//
// HC-035 states: a twin handler MUST implement the Handler interface of §6.1 with the
// SAME method signatures and error-class discipline as a real handler, AND MUST be
// launched as a separate subprocess per HC-045. Selection between real handler and twin
// is config-level per HC-003. The carve-out for unit-test fakes (in-process Handler
// implementations for targeted unit tests) is NOT subject to §4.8 parity constraints.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler implementation is pending; this sensor
// verifies that HC-035 is correctly declared in the spec so that:
//
//  1. HC-035 heading is present in specs/handler-contract.md.
//  2. "same Handler interface" is declared as the twin requirement.
//  3. "separate subprocess" is declared as the launch requirement.
//  4. "config-level" is declared as the real/twin selection mechanism.
//  5. "unit-test fakes" carve-out is declared.
//  6. Tags: mechanism is present in the HC-035 body window.
//
// # Failure modes
//
//   - HC-035 heading missing.
//   - same Handler interface absent.
//   - separate subprocess absent.
//   - config-level absent.
//   - unit-test fakes carve-out absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc035Fixture prefix per
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

// hc035FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc035FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc035FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc035FixtureHeading matches the HC-035 level-4 requirement heading line.
var hc035FixtureHeading = regexp.MustCompile(`^#### HC-035 —`)

// hc035FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc035FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc035FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc035FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc035FixtureBodyWindow is the maximum number of lines to scan after the heading.
const hc035FixtureBodyWindow = 15

// hc035FixtureLoadLines opens specFile and returns all lines.
func hc035FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc035FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc035FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc035FixtureBodyLines returns the lines comprising the HC-035 body.
func hc035FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc035FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-035 heading not found; expected '#### HC-035 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc035FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc035FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc035FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc035FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC035TwinsHandlerInterface is the binding test for hk-8i31.42.
func TestHC035TwinsHandlerInterface(t *testing.T) {
	t.Parallel()

	specFile := hc035FixtureHandlerContractPath(t)
	lines := hc035FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc035FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-035 check(1): %s", reason)
	}
	t.Logf("HC-035 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "same-handler-interface",
			needle: "Handler` interface",
			detail: "HC-035 body must declare twins implement the 'Handler` interface' of §6.1 " +
				"(expected phrase 'Handler` interface'); the interface definition is the normative " +
				"contract — twins must satisfy the same method signatures and error-class discipline " +
				"as real handlers to be drop-in substitutes in scenario tests",
		},
		{
			id:     "3",
			label:  "separate-subprocess-launch",
			needle: "separate subprocess",
			detail: "HC-035 body must declare twins 'separate subprocess' launch requirement " +
				"(expected phrase 'separate subprocess'); a twin is a subprocess, not an in-process " +
				"mock — it communicates via the same Unix domain socket wire protocol as a real handler; " +
				"this is what makes scenario tests exercise the actual handler<->watcher boundary",
		},
		{
			id:     "4",
			label:  "config-level-selection",
			needle: "config-level",
			detail: "HC-035 body must declare 'config-level' as the real/twin selection mechanism " +
				"(expected phrase 'config-level'); the selection between a real handler and its twin " +
				"is a handler-config override, not a runtime branch or environment variable — this " +
				"is HC-003's config-level discipline applied to twin substitution",
		},
		{
			id:     "5",
			label:  "unit-test-fakes-carve-out",
			needle: "unit-test fakes",
			detail: "HC-035 body must declare the 'unit-test fakes' carve-out " +
				"(expected phrase 'unit-test fakes'); in-process Handler implementations for " +
				"targeted Go unit tests are NOT twins and NOT subject to parity constraints — " +
				"they can skip subprocess launch and wire protocol; only the canonical twin binary " +
				"is subject to §4.8",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc035FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-035 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-035 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in HC-035 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc035FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-035 check(6) FAILED: Tags: mechanism not found in HC-035 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-035 body)\n"+
					"  detail: HC-035 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.42 audit complete — HC-035 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
