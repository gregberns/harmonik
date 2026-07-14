//go:build specaudit

package specaudit_test

// hk-sx9r.38 binding test — ON-027 step 5: memory layer flushes indexing.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-027.
//
// ON-027 step 5 states: memory layer flushes its indexing state to disk. Bounded by
// timeout.step_5 per ON-029. Per ON-027a, each step is durably marked before the next
// step begins.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The drain step-5 memory-flush implementation is
// pending; this sensor verifies that ON-027 step 5 is correctly declared in the spec
// so that:
//
//  1. ON-027 heading is present in specs/operator-nfr.md.
//  2. "memory layer flushes indexing" declares step 5.
//  3. Step ordering is declared — each step completes before the next.
//  4. The eight-step drain is the precondition for the pausing→paused transition.
//  5. workspace manager unlocks leased workspaces is declared as step 6 (confirming step 5 ordering).
//  6. "drain-timeout-escalated" names the exit code for step overruns.
//  7. Tags: mechanism is present in the ON-027 body window.
//
// # Failure modes
//
//   - ON-027 heading missing.
//   - memory layer flushes indexing absent.
//   - step ordering absent.
//   - pausing→paused precondition absent.
//   - workspace manager step 6 absent.
//   - drain-timeout-escalated exit code absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on027s5Fixture prefix per
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

// on027s5FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on027s5FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on027s5FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on027s5FixtureON027Heading matches the ON-027 level-4 requirement heading line.
var on027s5FixtureON027Heading = regexp.MustCompile(`^#### ON-027 —`)

// on027s5FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on027s5FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on027s5FixtureTagsMechanism matches a "Tags: mechanism" line.
var on027s5FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on027s5FixtureBodyWindow is the maximum number of lines after the ON-027
// heading to scan for requirement-body content.
const on027s5FixtureBodyWindow = 30

// on027s5FixtureLoadLines opens specFile and returns all lines.
func on027s5FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on027s5FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on027s5FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on027s5FixtureON027BodyLines returns the lines comprising the ON-027 body.
func on027s5FixtureON027BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on027s5FixtureON027Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-027 heading not found; expected '#### ON-027 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on027s5FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on027s5FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on027s5FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on027s5FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON027Step5MemoryLayerFlushesIndexing is the binding test for hk-sx9r.38.
func TestON027Step5MemoryLayerFlushesIndexing(t *testing.T) {
	t.Parallel()

	specFile := on027s5FixtureOperatorNFRPath(t)
	lines := on027s5FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on027s5FixtureON027BodyLines(lines)
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
			label:  "memory-layer-flushes-indexing",
			needle: "memory layer flushes indexing",
			detail: "ON-027 body must declare 'memory layer flushes indexing' as step 5 " +
				"(expected phrase 'memory layer flushes indexing'); step 5 is the S07 memory " +
				"subsystem's drain obligation: it MUST flush its in-memory indexing state to " +
				"disk before the daemon can safely enter paused or exit",
		},
		{
			id:     "3",
			label:  "step-ordering-each-before-next",
			needle: "each step completing before the next begins",
			detail: "ON-027 body must declare step ordering: 'each step completing before the " +
				"next begins' (expected phrase 'each step completing before the next begins'); " +
				"this is the sequential invariant that makes step 5 MUST precede step 6 — the " +
				"memory flush must complete before workspace unlocks",
		},
		{
			id:     "4",
			label:  "pausing-to-paused-all-steps-precondition",
			needle: "pausing → paused",
			detail: "ON-027 body must declare the 'pausing → paused' transition requires ALL " +
				"steps (expected phrase 'pausing → paused'); this makes step 5 a mandatory " +
				"gate: the daemon cannot enter paused until the memory layer flush in step 5 " +
				"has completed (or timed out per ON-029)",
		},
		{
			id:     "5",
			label:  "workspace-manager-step-6-confirms-ordering",
			needle: "workspace manager",
			detail: "ON-027 body must name 'workspace manager' as step 6 " +
				"(expected phrase 'workspace manager'); step 6 (workspace-manager unlock) " +
				"follows step 5 (memory-layer flush) — naming step 6 confirms the step-5 " +
				"ordering: memory flush precedes workspace unlock in the drain sequence",
		},
		{
			id:     "6",
			label:  "drain-timeout-escalated-exit-code",
			needle: "drain-timeout-escalated",
			detail: "ON-027 body must name 'drain-timeout-escalated' as the exit code for " +
				"step overruns (expected phrase 'drain-timeout-escalated'); when step 5 exceeds " +
				"timeout.step_5 per ON-029, step 7 exits with the drain-timeout-escalated code",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on027s5FixtureBodyContains(body, c.needle) {
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
			if on027s5FixtureTagsMechanism.MatchString(line) {
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

	t.Logf("hk-sx9r.38 audit complete — ON-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
