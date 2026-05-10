package specaudit_test

// hk-8mwo.54 binding test — WM-040 interrupt_state reset to none requires reconciliation
// OR operator resume.
//
// Spec ref: specs/workspace-model.md §4.10 WM-040.
//
// WM-040 states: clearing interrupt_state back to none MUST be driven by either (a) an
// operator_resuming event per event-model.md §8.7 and operator-nfr.md §4.3 ON-013 for
// operator-initiated interrupts, or (b) a reconciliation verdict per reconciliation/spec.md
// §4.5 for daemon-crash or lost-lease interrupts. The workspace manager MUST NOT silently
// clear the field. Sensor: any mutation of interrupt_state to none not preceded by a
// matching causing event is a violation, detected by the reconciliation detector's
// transition-audit pass per reconciliation/spec.md §4.3 RC-010.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending; this
// sensor verifies that WM-040 is correctly declared in the spec so that:
//
//  1. WM-040 heading is present in specs/workspace-model.md.
//  2. "operator_resuming" event is named as the (a) clear pathway.
//  3. "reconciliation verdict" is named as the (b) clear pathway.
//  4. "MUST NOT silently clear" is declared.
//  5. "RC-010" is named as the sensor/detector for this rule.
//  6. Tags: mechanism is present in the WM-040 body window.
//
// # Failure modes
//
//   - WM-040 heading missing.
//   - operator_resuming pathway absent.
//   - reconciliation verdict pathway absent.
//   - MUST NOT silently clear absent.
//   - RC-010 sensor absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm040Fixture prefix per
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

// wm040FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm040FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm040FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm040FixtureHeading matches the WM-040 level-4 requirement heading line.
var wm040FixtureHeading = regexp.MustCompile(`^#### WM-040 —`)

// wm040FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm040FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm040FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm040FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm040FixtureBodyWindow is the maximum number of lines to scan after the heading.
const wm040FixtureBodyWindow = 30

// wm040FixtureLoadLines opens specFile and returns all lines.
func wm040FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm040FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm040FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm040FixtureBodyLines returns the lines comprising the WM-040 body.
func wm040FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm040FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-040 heading not found; expected '#### WM-040 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm040FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm040FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm040FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm040FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM040InterruptStateResetRequiresEventOrVerdict is the binding test for hk-8mwo.54.
func TestWM040InterruptStateResetRequiresEventOrVerdict(t *testing.T) {
	t.Parallel()

	specFile := wm040FixtureWorkspaceModelPath(t)
	lines := wm040FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm040FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-040 check(1): %s", reason)
	}
	t.Logf("WM-040 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "operator-resuming-pathway",
			needle: "operator_resuming",
			detail: "WM-040 body must name 'operator_resuming' as the (a) clear pathway " +
				"(expected phrase 'operator_resuming'); this is the event emitted by the operator " +
				"control subsystem when the operator resumes a paused or stopped run — observing it " +
				"is the workspace manager's trigger to clear interrupt_state to none",
		},
		{
			id:     "3",
			label:  "reconciliation-verdict-pathway",
			needle: "reconciliation verdict",
			detail: "WM-040 body must name 'reconciliation verdict' as the (b) clear pathway " +
				"(expected phrase 'reconciliation verdict'); a reconciliation verdict may clear " +
				"interrupt_state for daemon-crash-suspected (Cat 6a/6b resolution) or lost-lease " +
				"interrupts — the verdict is the authoritative cause for the clear",
		},
		{
			id:     "4",
			label:  "must-not-silently-clear",
			needle: "MUST NOT silently clear",
			detail: "WM-040 body must declare 'MUST NOT silently clear' the field " +
				"(expected phrase 'MUST NOT silently clear'); silent clearing would hide the cause " +
				"of the interrupt resolution from operators and reconciliation — all clears must " +
				"have a traceable causing event or verdict",
		},
		{
			id:     "5",
			label:  "rc-010-sensor",
			needle: "RC-010",
			detail: "WM-040 body must name 'RC-010' as the sensor/detector for this rule " +
				"(expected phrase 'RC-010'); RC-010 is the reconciliation detector that runs " +
				"the transition-audit pass — it detects interrupt_state-to-none mutations not " +
				"preceded by a matching causing event and routes to reconciliation Cat violations",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm040FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-040 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-040 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in WM-040 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm040FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-040 check(6) FAILED: Tags: mechanism not found in WM-040 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-040 body)\n"+
					"  detail: WM-040 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.54 audit complete — WM-040 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
