package specaudit_test

// hk-8i31.33 binding test — HC-026b drain-forced silent-hang synthesis is ON-classified,
// not HC-classified.
//
// Spec ref: specs/handler-contract.md §4.6 HC-026b.
//
// HC-026b states: when a handler subprocess is silent-hanging during an operator-initiated
// drain, the synthesis of agent_warning_silent_hang{reason=drain_forced} is governed by
// operator-nfr.md §4.9 ON-040, NOT by HC's silent-hang taxonomy. The watcher MUST cooperate
// by NOT also emitting an HC-classified silent-hang event for the same run/node when the
// operator-control subsystem has signalled a drain-forced synthesis path. Single-emitter
// (ON-side) construction keeps HC-INV-004 satisfied.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler implementation is pending; this sensor
// verifies that HC-026b is correctly declared in the spec so that:
//
//  1. HC-026b heading is present in specs/handler-contract.md.
//  2. "drain_forced" reason discriminator is named.
//  3. "ON-040" is named as the governing rule.
//  4. "NOT" emit HC-classified silent-hang is declared for the watcher.
//  5. "single-emitter" construction is declared.
//  6. Tags: mechanism is present in the HC-026b body window.
//
// # Failure modes
//
//   - HC-026b heading missing.
//   - drain_forced absent.
//   - ON-040 absent.
//   - NOT absent (watcher must not also emit).
//   - single-emitter absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc026bFixture prefix per
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

// hc026bFixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc026bFixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc026bFixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc026bFixtureHeading matches the HC-026b level-4 requirement heading line.
var hc026bFixtureHeading = regexp.MustCompile(`^#### HC-026b —`)

// hc026bFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc026bFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc026bFixtureTagsMechanism matches a "Tags: mechanism" line.
var hc026bFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc026bFixtureBodyWindow is the maximum number of lines to scan after the heading.
const hc026bFixtureBodyWindow = 15

// hc026bFixtureLoadLines opens specFile and returns all lines.
func hc026bFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc026bFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc026bFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc026bFixtureBodyLines returns the lines comprising the HC-026b body.
func hc026bFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc026bFixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-026b heading not found; expected '#### HC-026b —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc026bFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc026bFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc026bFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc026bFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC026bDrainForcedHangONClassified is the binding test for hk-8i31.33.
func TestHC026bDrainForcedHangONClassified(t *testing.T) {
	t.Parallel()

	specFile := hc026bFixtureHandlerContractPath(t)
	lines := hc026bFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc026bFixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-026b check(1): %s", reason)
	}
	t.Logf("HC-026b heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "drain-forced-reason-named",
			needle: "drain_forced",
			detail: "HC-026b body must name 'drain_forced' as the reason discriminator " +
				"(expected phrase 'drain_forced'); this is the reason field value in " +
				"agent_warning_silent_hang that identifies the drain-forced synthesis path — " +
				"it distinguishes operator-drain kills from genuine silence-triggered terminations",
		},
		{
			id:     "3",
			label:  "on-040-governing-rule",
			needle: "ON-040",
			detail: "HC-026b body must name 'ON-040' as the governing rule " +
				"(expected phrase 'ON-040'); ON-040 is the operator-nfr rule that defines the " +
				"synthesis of drain-forced silent-hang events — HC defers classification to ON " +
				"by accepting this cross-spec boundary",
		},
		{
			id:     "4",
			label:  "watcher-must-not-emit-hc-silent-hang",
			needle: "NOT also emitting",
			detail: "HC-026b body must declare the watcher 'NOT also emitting' an HC-classified silent-hang " +
				"(expected phrase 'NOT also emitting'); if the watcher also emitted an HC-classified " +
				"event, HC-INV-004 (exactly one terminal event per session) would be violated — " +
				"ON-040 is the sole emitter of the drain-forced synthesis event",
		},
		{
			id:     "5",
			label:  "single-emitter-construction",
			needle: "single-emitter",
			detail: "HC-026b body must declare 'single-emitter' construction for the synthesis " +
				"(expected phrase 'single-emitter'); this is the explicit statement that exactly one " +
				"subsystem (ON-side) is responsible for emitting the drain-forced silent-hang event — " +
				"it satisfies HC-INV-004 by construction",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc026bFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-026b check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-026b body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in HC-026b body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc026bFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-026b check(6) FAILED: Tags: mechanism not found in HC-026b body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-026b body)\n"+
					"  detail: HC-026b carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.33 audit complete — HC-026b heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
