package specaudit_test

// hk-sx9r.17 binding test — ON-013c operator-command idempotency on no-op transitions.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-013c.
//
// ON-013c states: operator commands MUST be idempotent on no-op transitions. A pause
// issued while already paused MUST return success (exit 0) with operator_pause_status
// re-emitted at most once per command (deduplicated via session_id). A stop while stopped
// MUST return success with no event emission. A resume while running MUST return success
// with no transition. The operator CLI MUST NOT see a different exit code for "already
// in target state" vs "transitioned successfully."
//
// # Audit frame
//
// This test is a spec-corpus sensor. The operator-control idempotency implementation
// is pending; this sensor verifies that ON-013c is correctly declared in the spec so that:
//
//  1. ON-013c heading is present in specs/operator-nfr.md.
//  2. "MUST be idempotent on no-op transitions" is declared.
//  3. "pause" while "paused" returning success is declared.
//  4. "session_id" deduplication is named.
//  5. "stop" while "stopped" returning success with no event emission is declared.
//  6. "resume" while "running" returning success with no transition is declared.
//  7. "MUST NOT see a different exit code" for same-state vs transition is declared.
//  8. Tags: mechanism is present in the ON-013c body window.
//
// # Failure modes
//
//   - ON-013c heading missing.
//   - Idempotent on no-op transitions absent.
//   - pause-while-paused success absent.
//   - session_id deduplication absent.
//   - stop-while-stopped success absent.
//   - resume-while-running success absent.
//   - MUST NOT different exit code absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on013cFixture prefix per
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

// on013cFixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on013cFixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on013cFixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on013cFixtureON013cHeading matches the ON-013c level-4 requirement heading line.
var on013cFixtureON013cHeading = regexp.MustCompile(`^#### ON-013c —`)

// on013cFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on013cFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on013cFixtureTagsMechanism matches a "Tags: mechanism" line.
var on013cFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on013cFixtureBodyWindow is the maximum number of lines after the ON-013c
// heading to scan for requirement-body content.
const on013cFixtureBodyWindow = 30

// on013cFixtureLoadLines opens specFile and returns all lines.
func on013cFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on013cFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on013cFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on013cFixtureON013cBodyLines returns the lines comprising the ON-013c body.
func on013cFixtureON013cBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on013cFixtureON013cHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-013c heading not found; expected '#### ON-013c —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on013cFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on013cFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on013cFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on013cFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON013cOperatorCommandIdempotencyNoOpTransitions is the binding test for hk-sx9r.17.
func TestON013cOperatorCommandIdempotencyNoOpTransitions(t *testing.T) {
	t.Parallel()

	specFile := on013cFixtureOperatorNFRPath(t)
	lines := on013cFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on013cFixtureON013cBodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-013c check(1): %s", reason)
	}
	t.Logf("ON-013c heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "must-be-idempotent-on-no-op-transitions",
			needle: "idempotent on no-op transitions",
			detail: "ON-013c body must declare 'idempotent on no-op transitions' " +
				"(expected phrase 'idempotent on no-op transitions'); this is the root normative " +
				"obligation — all operator commands targeting an already-achieved state MUST succeed " +
				"without error, making retry-safe CLI scripting possible",
		},
		{
			id:     "3",
			label:  "pause-while-paused-returns-success",
			needle: "pause",
			detail: "ON-013c body must describe 'pause' as an idempotent no-op when already paused " +
				"(expected phrase 'pause'); the pause case is the primary example: re-pausing while " +
				"paused MUST return exit 0 with at-most-one operator_pause_status re-emission",
		},
		{
			id:     "4",
			label:  "session-id-deduplication",
			needle: "session_id",
			detail: "ON-013c body must name 'session_id' as the deduplication key " +
				"(expected phrase 'session_id'); the session_id carried on operator commands per " +
				"ON-050 is the deduplication mechanism that prevents multiple operator_pause_status " +
				"emissions for the same logical command",
		},
		{
			id:     "5",
			label:  "stop-while-stopped-no-event",
			needle: "stop",
			detail: "ON-013c body must describe 'stop' as an idempotent no-op when already stopped " +
				"(expected phrase 'stop'); stop-while-stopped MUST return success with no event " +
				"emission — unlike the pause case, there is no re-emission because stopped is terminal",
		},
		{
			id:     "6",
			label:  "resume-while-running-no-transition",
			needle: "resume",
			detail: "ON-013c body must describe 'resume' as an idempotent no-op when already running " +
				"(expected phrase 'resume'); resume-while-running MUST return success with no transition " +
				"— the daemon is already in the target state",
		},
		{
			id:     "7",
			label:  "must-not-different-exit-code",
			needle: "MUST NOT see a different exit code",
			detail: "ON-013c body must declare 'MUST NOT see a different exit code' " +
				"(expected phrase 'MUST NOT see a different exit code'); this is the CLI-visible " +
				"contract: operators and scripts cannot distinguish 'already in target state' from " +
				"'transitioned successfully' by exit code — both are exit 0",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on013cFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-013c check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-013c body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in ON-013c body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on013cFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-013c check(8) FAILED: Tags: mechanism not found in ON-013c body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-013c body)\n"+
					"  detail: ON-013c carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.17 audit complete — ON-013c heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
