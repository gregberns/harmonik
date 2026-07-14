//go:build specaudit

package specaudit_test

// hk-sx9r.33 binding test — ON-027 step 1: daemon stops advancing the queue.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 states: on stop --graceful or SIGTERM (and on pause/upgrade per the
// drain-gate of ON-008), the daemon MUST execute the shutdown/drain sequence in
// strict step order. Step 1 (per extqueue v0.4.2): the daemon stops advancing
// the queue — no new dispatches are issued from the active group, and the
// queue's status field transitions to `paused-by-drain` per
// [queue-model.md §5]. (Reworded from the legacy "orchestrator stops pulling
// new tasks from the queue" phrasing.)
// Per ON-027a, each step's completion MUST be marked durably before the next step
// begins; on crash mid-drain, restart resumes from the next-uncompleted step.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain sequencer implementation is
// pending; this sensor verifies that ON-027 is correctly declared in the spec
// so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. "stop --graceful" is declared as a drain trigger.
//  3. "SIGTERM" is declared as a drain trigger.
//  4. "stops advancing the queue" is declared as drain step 1.
//  5. Step ordering is declared (each step completing before the next begins).
//  6. "drain-timeout" escalation is named as the out-of-bound handling path.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - stop --graceful absent.
//   - SIGTERM absent.
//   - Step-1 stop-advancing-queue wording absent.
//   - Step ordering declaration absent.
//   - drain-timeout escalation absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027Fixture prefix per
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

// on027FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027FixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027FixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027FixtureTagsMechanism matches a "Tags: mechanism" line.
var on027FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027FixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027FixtureBodyWindow = 30

// on027FixtureLoadLines opens specFile and returns all lines.
func on027FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027FixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027FixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027FixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027DrainStep1StopPullingQueue is the binding test for hk-sx9r.33.
func TestON027DrainStep1StopPullingQueue(t *testing.T) {
	t.Parallel()

	specFile := on027FixtureOperatorNFRPath(t)
	lines := on027FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027FixtureON027BodyLines(lines)
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
			label:  "stop-graceful-drain-trigger",
			needle: "stop --graceful",
			detail: "ON-027 body must declare 'stop --graceful' as a drain trigger " +
				"(expected phrase 'stop --graceful'); graceful stop is one of the two " +
				"operator-command triggers for the full eight-step drain sequence",
		},
		{
			id:     "3",
			label:  "sigterm-drain-trigger",
			needle: "SIGTERM",
			detail: "ON-027 body must declare 'SIGTERM' as a drain trigger " +
				"(expected phrase 'SIGTERM'); OS-signal-initiated stops follow the same " +
				"drain sequence as operator-command graceful stops",
		},
		{
			id:     "4",
			label:  "step1-stop-advancing-queue",
			needle: "stops advancing the queue",
			detail: "ON-027 body must declare 'stops advancing the queue' as step 1 " +
				"(expected phrase 'stops advancing the queue'); this is the first " +
				"drain step: per extqueue (v0.4.2), the daemon stops issuing new " +
				"dispatches from the active group, the queue's status field transitions " +
				"to 'paused-by-drain' per [queue-model.md §5], and no new run is " +
				"dispatched during drain. (Reworded v0.4.2 from the legacy " +
				"'orchestrator stops pulling new tasks from the queue' phrasing.)",
		},
		{
			id:     "5",
			label:  "step-ordering-each-before-next",
			needle: "each step completing before the next begins",
			detail: "ON-027 body must declare that steps complete before the next begins " +
				"(expected phrase 'each step completing before the next begins'); this is " +
				"the strict sequential ordering invariant of the drain sequence, enforced " +
				"by ON-027a on a single goroutine",
		},
		{
			id:     "6",
			label:  "drain-timeout-escalation-named",
			needle: "drain-timeout",
			detail: "ON-027 body must name 'drain-timeout' as the out-of-bound escalation path " +
				"(expected phrase 'drain-timeout'); the drain-timeout escalation is what happens " +
				"when a step exceeds its per-step bound (ON-029), producing a non-zero exit code " +
				"for 'drain-timeout-escalated' per §8",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027FixtureBodyContains(body, c.needle) {
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
			if on027FixtureTagsMechanism.MatchString(line) {
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

	t.Logf("hk-sx9r.33 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
