//go:build specaudit

package specaudit_test

// hk-8i31.30 binding test — HC-025 rate-limit events are distinct from failure events.
//
// Spec ref: specs/handler-contract.md §4.6 HC-025.
//
// HC-025 states: when the adapter's DetectRateLimit returns limited=true, the watcher
// MUST emit agent_rate_limited (NOT agent_failed) carrying retry_after. On the adapter's
// detection of rate-limit clearance, the watcher MUST emit agent_rate_limit_cleared.
// Rate-limited sessions are NOT failures; the daemon's policy is exponential backoff
// within wall-clock budget.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler implementation is pending; this sensor
// verifies that HC-025 is correctly declared in the spec so that:
//
//  1. HC-025 heading is present in specs/handler-contract.md.
//  2. "agent_rate_limited" is declared as the event to emit (not agent_failed).
//  3. "retry_after" is named as a field on the event.
//  4. "agent_rate_limit_cleared" is named for the clearance event.
//  5. "NOT failures" is declared for rate-limited sessions.
//  6. Tags: mechanism is present in the HC-025 body window.
//
// # Failure modes
//
//   - HC-025 heading missing.
//   - agent_rate_limited absent.
//   - retry_after absent.
//   - agent_rate_limit_cleared absent.
//   - NOT failures absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc025Fixture prefix per
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

// hc025FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc025FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc025FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc025FixtureHeading matches the HC-025 level-4 requirement heading line.
var hc025FixtureHeading = regexp.MustCompile(`^#### HC-025 —`)

// hc025FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc025FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc025FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc025FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc025FixtureBodyWindow is the maximum number of lines to scan after the heading.
const hc025FixtureBodyWindow = 15

// hc025FixtureLoadLines opens specFile and returns all lines.
func hc025FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc025FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc025FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc025FixtureBodyLines returns the lines comprising the HC-025 body.
func hc025FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc025FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-025 heading not found; expected '#### HC-025 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc025FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc025FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc025FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc025FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC025RateLimitDistinctFromFailure is the binding test for hk-8i31.30.
func TestHC025RateLimitDistinctFromFailure(t *testing.T) {
	t.Parallel()

	specFile := hc025FixtureHandlerContractPath(t)
	lines := hc025FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc025FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-025 check(1): %s", reason)
	}
	t.Logf("HC-025 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "agent-rate-limited-event",
			needle: "agent_rate_limited",
			detail: "HC-025 body must declare 'agent_rate_limited' as the event to emit " +
				"(expected phrase 'agent_rate_limited'); the watcher emits this event instead of " +
				"agent_failed when DetectRateLimit returns limited=true — distinguishing rate-limit " +
				"from failure is essential so the daemon applies backoff rather than terminating",
		},
		{
			id:     "3",
			label:  "retry-after-field",
			needle: "retry_after",
			detail: "HC-025 body must name 'retry_after' as a field on the rate-limit event " +
				"(expected phrase 'retry_after'); this field carries the adapter's suggested " +
				"wait time before the next attempt — consumers of agent_rate_limited use it to " +
				"schedule the exponential backoff correctly",
		},
		{
			id:     "4",
			label:  "agent-rate-limit-cleared-event",
			needle: "agent_rate_limit_cleared",
			detail: "HC-025 body must name 'agent_rate_limit_cleared' for the clearance event " +
				"(expected phrase 'agent_rate_limit_cleared'); this paired event closes the rate-limit " +
				"window; together with agent_rate_limited they form the agent_rate_limit_status " +
				"paired-phase lifecycle per event-model.md §8.9(h)",
		},
		{
			id:     "5",
			label:  "rate-limited-sessions-not-failures",
			needle: "NOT failures",
			detail: "HC-025 body must declare rate-limited sessions are 'NOT failures' " +
				"(expected phrase 'NOT failures'); this is the explicit statement that distinguishes " +
				"rate-limit semantics from failure semantics — operators and reconciliation detectors " +
				"must not treat agent_rate_limited as evidence of a failed run",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc025FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-025 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-025 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in HC-025 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc025FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-025 check(6) FAILED: Tags: mechanism not found in HC-025 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-025 body)\n"+
					"  detail: HC-025 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.30 audit complete — HC-025 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
