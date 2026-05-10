package specaudit_test

// hk-sx9r.40 binding test — ON-027 step 7: orchestrator exits (or enters paused).
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 step 7 states: orchestrator exits with code 0 if clean, or §8 code 11
// (drain-timeout-escalated) if any prior step exceeded its bound. In the pause/upgrade
// path, step 7 is replaced by "enter paused" (no process exit) — the step sequence is
// identical. Completion of ALL eight steps is the precondition for pausing→paused per ON-008.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain step-7 orchestrator-exit implementation
// is pending; this sensor verifies that ON-027 step 7 is correctly declared in the spec
// so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. "orchestrator exits with code 0 if clean" declares the clean-exit path.
//  3. "drain-timeout-escalated" names the non-zero exit code for step overruns.
//  4. "enter `paused`" is named as the pause-path replacement for step-7 exit.
//  5. "the step sequence is identical" confirms the drain steps are the same for both paths.
//  6. "pausing → paused" precondition requires ALL eight steps.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - orchestrator exits with code 0 absent.
//   - drain-timeout-escalated exit code absent.
//   - enter paused pause-path replacement absent.
//   - step sequence identical declaration absent.
//   - pausing→paused precondition absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027s7Fixture prefix per
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

// on027s7FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027s7FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027s7FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027s7FixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027s7FixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027s7FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027s7FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027s7FixtureTagsMechanism matches a "Tags: mechanism" line.
var on027s7FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027s7FixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027s7FixtureBodyWindow = 30

// on027s7FixtureLoadLines opens specFile and returns all lines.
func on027s7FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027s7FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027s7FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027s7FixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027s7FixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027s7FixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027s7FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027s7FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027s7FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027s7FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027Step7OrchestratorExitsOrEntersPaused is the binding test for hk-sx9r.40.
func TestON027Step7OrchestratorExitsOrEntersPaused(t *testing.T) {
	t.Parallel()

	specFile := on027s7FixtureOperatorNFRPath(t)
	lines := on027s7FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027s7FixtureON027BodyLines(lines)
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
			label:  "orchestrator-exits-code-0-if-clean",
			needle: "orchestrator exits with code 0 if clean",
			detail: "ON-027 body must declare 'orchestrator exits with code 0 if clean' for step 7 " +
				"(expected phrase 'orchestrator exits with code 0 if clean'); step 7 is the terminal " +
				"step: if all prior steps completed without timeout, the orchestrator exits with 0 " +
				"(the normal success path for stop --graceful and SIGTERM-initiated drains)",
		},
		{
			id:     "3",
			label:  "drain-timeout-escalated-exit-code",
			needle: "drain-timeout-escalated",
			detail: "ON-027 body must name 'drain-timeout-escalated' as the exit code when a prior " +
				"step exceeded its bound (expected phrase 'drain-timeout-escalated'); this non-zero " +
				"code signals to process supervisors that the graceful drain did not fully complete " +
				"within its configured time budget",
		},
		{
			id:     "4",
			label:  "enter-paused-pause-path",
			needle: "enter `paused`",
			detail: "ON-027 body must declare 'enter paused' as the pause-path replacement for step-7 " +
				"exit (expected phrase 'enter `paused`'); in the pause/upgrade path, the process does " +
				"not exit — instead, step 7 transitions to the paused state, preserving the daemon " +
				"for subsequent resume or upgrade",
		},
		{
			id:     "5",
			label:  "step-sequence-identical-both-paths",
			needle: "the step sequence is identical",
			detail: "ON-027 body must declare 'the step sequence is identical' for both stop and " +
				"pause/upgrade paths (expected phrase 'the step sequence is identical'); this normative " +
				"statement prevents implementations from shortcutting the drain steps for the pause " +
				"path vs the stop path — the full sequence is mandatory in both cases",
		},
		{
			id:     "6",
			label:  "pausing-to-paused-all-steps-precondition",
			needle: "pausing → paused",
			detail: "ON-027 body must declare the 'pausing → paused' transition requires ALL " +
				"steps (expected phrase 'pausing → paused'); step 7 completion (either exit 0 or " +
				"enter paused) is the final gate: paused is unreachable until step 7 resolves",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027s7FixtureBodyContains(body, c.needle) {
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
			if on027s7FixtureTagsMechanism.MatchString(line) {
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

	t.Logf("hk-sx9r.40 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
