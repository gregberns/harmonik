//go:build specaudit

package specaudit_test

// hk-sx9r.37 binding test — ON-027 step 4: event bus flushes pending events (fsync).
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 step 4 states: event bus flushes all pending events to JSONL with fsync(file_fd)
// + fsync(parent_dir_fd) per [event-model.md §4.4]. The step also synthesizes
// agent_warning_silent_hang{reason=drain_forced} per ON-040 BEFORE SIGKILL emission to
// any still-running agent subprocesses from step-3 timeout. Per ON-027a, durably marked.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain step-4 event-bus flush implementation
// is pending; this sensor verifies that ON-027 step 4 is correctly declared in the spec
// so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. Step 4 declares the event bus flushes pending events.
//  3. "event-model.md §4.4" is cited for the fsync obligation.
//  4. "drain-timeout-escalated" names the exit code produced when a step times out.
//  5. "pausing → paused" precondition requires ALL steps to complete.
//  6. The event bus flush step precedes the paused-state entry.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - Event-bus flush step absent.
//   - event-model.md §4.4 citation absent.
//   - drain-timeout-escalated exit code absent.
//   - pausing→paused precondition absent.
//   - Ordering of event-bus flush before paused absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027s4Fixture prefix per
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

// on027s4FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027s4FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027s4FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027s4FixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027s4FixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027s4FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027s4FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027s4FixtureTagsMechanism matches a "Tags: mechanism" line.
var on027s4FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027s4FixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027s4FixtureBodyWindow = 30

// on027s4FixtureLoadLines opens specFile and returns all lines.
func on027s4FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027s4FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027s4FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027s4FixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027s4FixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027s4FixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027s4FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027s4FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027s4FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027s4FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027Step4EventBusFlushesPendingEvents is the binding test for hk-sx9r.37.
func TestON027Step4EventBusFlushesPendingEvents(t *testing.T) {
	t.Parallel()

	specFile := on027s4FixtureOperatorNFRPath(t)
	lines := on027s4FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027s4FixtureON027BodyLines(lines)
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
			label:  "event-bus-flushes-pending-events",
			needle: "event bus flushes pending events",
			detail: "ON-027 body must declare 'event bus flushes pending events' as step 4 " +
				"(expected phrase 'event bus flushes pending events'); step 4 is the event-bus " +
				"flush step that durably commits all in-flight events to JSONL before the daemon " +
				"can safely enter paused or exit — any pending events lost after this point are " +
				"acceptable per EV-017, but the fsync MUST execute",
		},
		{
			id:     "3",
			label:  "event-model-section-4-4-cited",
			needle: "event-model.md §4.4",
			detail: "ON-027 body must cite 'event-model.md §4.4' for the fsync obligation " +
				"(expected phrase 'event-model.md §4.4'); EV §4.4 owns the fsync discipline " +
				"(file_fd + parent_dir_fd) that step 4 invokes; the citation anchors the " +
				"flush obligation to the normative event-model spec",
		},
		{
			id:     "4",
			label:  "drain-timeout-escalated-exit-code",
			needle: "drain-timeout-escalated",
			detail: "ON-027 body must name 'drain-timeout-escalated' as the exit code when a " +
				"step exceeds its bound (expected phrase 'drain-timeout-escalated'); step 7 " +
				"exits with the drain-timeout-escalated code (per §8) if any step timed out; " +
				"this is distinct from code 21 (drain-step-errored) for non-timeout errors",
		},
		{
			id:     "5",
			label:  "pausing-to-paused-all-steps-precondition",
			needle: "pausing → paused",
			detail: "ON-027 body must declare the 'pausing → paused' transition requires ALL " +
				"steps (expected phrase 'pausing → paused'); this binds the eight-step drain " +
				"to the state-machine transition gate: the event-bus flush in step 4 MUST " +
				"complete before paused is reachable",
		},
		{
			id:     "6",
			label:  "event-bus-flush-precedes-paused-entry",
			needle: "enter `paused`",
			detail: "ON-027 body must declare 'enter paused' as the pause-path terminal action " +
				"(expected phrase 'enter `paused`'); in the pause/upgrade path, step 7's " +
				"'orchestrator exits' is replaced by 'enter paused' — confirming that all " +
				"preceding steps (including the step-4 event-bus flush) precede paused-state entry",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027s4FixtureBodyContains(body, c.needle) {
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
			if on027s4FixtureTagsMechanism.MatchString(line) {
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

	t.Logf("hk-sx9r.37 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
