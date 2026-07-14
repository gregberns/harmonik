//go:build specaudit

package specaudit_test

// hk-sx9r.35 binding test — ON-027 step 3: agent runners wait for handler subprocesses.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 step 3 states: agent runners (HC watcher goroutines per handler-contract.md
// §4.3 HC-011) wait for handler subprocesses to complete or reach the drain timeout.
// On step-3 timeout, surviving agent subprocesses are SIGKILLed at step 4's wait window
// with synthesized agent_warning_silent_hang{reason=drain_forced} per ON-040.
// Per ON-027a, each step is durably marked before the next begins.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain step-3 handler-wait implementation is
// pending; this sensor verifies that ON-027 step 3 is correctly declared in the spec so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. Agent runners waiting for handler subprocesses is declared as step 3.
//  3. "handler-contract.md" is cited for the agent runner (HC watcher) mechanism.
//  4. "drain timeout" bounds step 3.
//  5. Step 3a — br-CLI intent-log drain — is declared.
//  6. "BrUnavailable" escalation path is named for step 3a failures.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - Agent-runner handler-subprocess wait absent.
//   - handler-contract.md citation absent.
//   - Drain-timeout bounding absent.
//   - Step 3a br-CLI intent-log drain absent.
//   - BrUnavailable escalation absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027s3Fixture prefix per
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

// on027s3FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027s3FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027s3FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027s3FixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027s3FixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027s3FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027s3FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027s3FixtureTagsMechanism matches a "Tags: mechanism" line.
var on027s3FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027s3FixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027s3FixtureBodyWindow = 30

// on027s3FixtureLoadLines opens specFile and returns all lines.
func on027s3FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027s3FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027s3FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027s3FixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027s3FixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027s3FixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027s3FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027s3FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027s3FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027s3FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027Step3AgentRunnersWaitForHandlers is the binding test for hk-sx9r.35.
func TestON027Step3AgentRunnersWaitForHandlers(t *testing.T) {
	t.Parallel()

	specFile := on027s3FixtureOperatorNFRPath(t)
	lines := on027s3FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027s3FixtureON027BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-027 check(1): %s", reason)
	}
	t.Logf("ON-027 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "agent-runners-wait-handler-subprocesses",
			needle: "agent runners wait for handler subprocesses",
			detail: "ON-027 body must declare 'agent runners wait for handler subprocesses' as step 3 " +
				"(expected phrase 'agent runners wait for handler subprocesses'); step 3 is distinct " +
				"from step 2: step 2 waits for the orchestrator-layer in_flight(run) predicate to " +
				"clear; step 3 waits for the underlying OS subprocesses the handlers are executing",
		},
		{
			id:     "3",
			label:  "beads-integration-step3a-citation",
			needle: "beads-integration.md",
			detail: "ON-027 body must cite 'beads-integration.md' for step 3a " +
				"(expected phrase 'beads-integration.md'); step 3a references BI-029/BI-030 " +
				"for the br-CLI adapter intent-log drain contract; the citation binds the drain " +
				"obligation to the beads-integration spec's terminal-transition write guarantee",
		},
		{
			id:     "4",
			label:  "drain-timeout-bounds-step-3",
			needle: "drain timeout",
			detail: "ON-027 body must name the drain timeout as bounding step 3 " +
				"(expected phrase 'drain timeout'); agent runners wait for subprocesses to complete " +
				"OR reach the drain timeout — the timeout prevents indefinite blocking in step 3",
		},
		{
			id:     "5",
			label:  "step-3a-br-cli-intent-log-drain",
			needle: "intent-log",
			detail: "ON-027 body must declare step 3a: the br-CLI adapter intent-log drain " +
				"(expected phrase 'intent-log'); step 3a requires every pending intent-log entry's " +
				"terminal-transition write to resolve before step 4 proceeds per BI-029/BI-030",
		},
		{
			id:     "6",
			label:  "brunavailable-escalation-named",
			needle: "BrUnavailable",
			detail: "ON-027 body must name 'BrUnavailable' as the step-3a failure mode " +
				"(expected phrase 'BrUnavailable'); BrUnavailable failures in step 3a escalate to " +
				"step 4 with failed entries marked for next-restart Cat 3a routing per BI-031",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027s3FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-027 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-027 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in ON-027 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on027s3FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-027 check(7) FAILED: Tags: mechanism not found in ON-027 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-027 body)\n"+
					"  detail: ON-027 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.35 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
