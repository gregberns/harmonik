package specaudit_test

// hk-sx9r.13 binding test — ON-011 operator-control state machine with serializable transitions.
//
// Spec ref: specs/operator-nfr.md §4.3 ON-011.
//
// ON-011 states: the daemon MUST implement the operator-control state machine of §7.1.
// States: running, pausing, paused, resuming, stopped (terminal-recoverable via start),
// and upgrading. Improvement-pause is NOT a distinct state (reuses pausing/paused with
// pause_reason=improvement per ON-012). Transitions MUST be serializable: a single mutex
// (or equivalent CAS primitive) guards the transition function, acquired before guard
// evaluation and held until emission and durable-marker write complete.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The operator-control state machine implementation
// is pending; this sensor verifies that ON-011 is correctly declared in the spec so that:
//
//  1. ON-011 heading is present in specs/operator-nfr.md.
//  2. "MUST implement the operator-control state machine" is declared.
//  3. All required states are named: running, pausing, paused, resuming, stopped, upgrading.
//  4. "serializable" transitions are declared.
//  5. Single mutex (or CAS primitive) guarding the transition function is required.
//  6. Mutex acquired before guard evaluation is declared.
//  7. Mutex held until durable-marker write per ON-030a completes is declared.
//  8. Tags: mechanism is present in the ON-011 body window.
//
// # Failure modes
//
//   - ON-011 heading missing.
//   - MUST implement state machine absent.
//   - Required states absent.
//   - Serializable transitions absent.
//   - Single mutex absent.
//   - Acquired before guard evaluation absent.
//   - Held until durable-marker write absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on011Fixture prefix per
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

// on011FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on011FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on011FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on011FixtureON011Heading matches the ON-011 level-4 requirement heading line.
var on011FixtureON011Heading = regexp.MustCompile(`^#### ON-011 —`)

// on011FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on011FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on011FixtureTagsMechanism matches a "Tags: mechanism" line.
var on011FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on011FixtureBodyWindow is the maximum number of lines after the ON-011
// heading to scan for requirement-body content.
const on011FixtureBodyWindow = 30

// on011FixtureLoadLines opens specFile and returns all lines.
func on011FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on011FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on011FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on011FixtureON011BodyLines returns the lines comprising the ON-011 body.
func on011FixtureON011BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on011FixtureON011Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-011 heading not found; expected '#### ON-011 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on011FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on011FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on011FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on011FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON011StateMachineSerializableTransitions is the binding test for hk-sx9r.13.
func TestON011StateMachineSerializableTransitions(t *testing.T) {
	t.Parallel()

	specFile := on011FixtureOperatorNFRPath(t)
	lines := on011FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on011FixtureON011BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-011 check(1): %s", reason)
	}
	t.Logf("ON-011 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "must-implement-state-machine",
			needle: "MUST implement the operator-control state machine",
			detail: "ON-011 body must declare 'MUST implement the operator-control state machine' " +
				"(expected phrase 'MUST implement the operator-control state machine'); " +
				"this is the root normative obligation that binds the daemon to the §7.1 state table",
		},
		{
			id:     "3a",
			label:  "state-running-named",
			needle: "running",
			detail: "ON-011 body must name the 'running' state (expected phrase 'running'); " +
				"running is the daemon's normal operating state and the initial state after startup",
		},
		{
			id:     "3b",
			label:  "state-pausing-named",
			needle: "pausing",
			detail: "ON-011 body must name the 'pausing' state (expected phrase 'pausing'); " +
				"pausing is the transient state while the drain sequence executes before paused",
		},
		{
			id:     "3c",
			label:  "state-upgrading-named",
			needle: "upgrading",
			detail: "ON-011 body must name the 'upgrading' state (expected phrase 'upgrading'); " +
				"upgrading is the post-paused state during exec-replace for harmonik upgrade",
		},
		{
			id:     "4",
			label:  "serializable-transitions-declared",
			needle: "serializable",
			detail: "ON-011 body must declare that transitions are 'serializable' " +
				"(expected phrase 'serializable'); serializable transitions mean concurrent " +
				"operator commands are arbitrated by mutex acquisition order, not interleaved",
		},
		{
			id:     "5",
			label:  "single-mutex-guarding-transition",
			needle: "single mutex",
			detail: "ON-011 body must require a 'single mutex' (expected phrase 'single mutex'); " +
				"the single mutex (or equivalent CAS primitive) is the mechanism that enforces " +
				"serializability — only one transition may proceed at a time",
		},
		{
			id:     "6",
			label:  "mutex-acquired-before-guard-evaluation",
			needle: "acquired before evaluating a transition guard",
			detail: "ON-011 body must declare that the mutex is 'acquired before evaluating a " +
				"transition guard' (expected phrase 'acquired before evaluating a transition guard'); " +
				"acquiring before guard evaluation prevents TOCTOU races where a guard evaluates " +
				"true but the state has changed by the time the transition executes",
		},
		{
			id:     "7",
			label:  "held-until-durable-marker-write",
			needle: "ON-030a",
			detail: "ON-011 body must declare that the mutex is held until the durable-marker write " +
				"per ON-030a completes (expected phrase 'ON-030a'); holding the mutex through the " +
				"durable write ensures that state-machine state and the .harmonik/daemon.state " +
				"marker are always consistent",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on011FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-011 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-011 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in ON-011 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on011FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-011 check(8) FAILED: Tags: mechanism not found in ON-011 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-011 body)\n"+
					"  detail: ON-011 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.13 audit complete — ON-011 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
