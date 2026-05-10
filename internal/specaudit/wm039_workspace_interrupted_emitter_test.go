package specaudit_test

// hk-8mwo.53 binding test — WM-039 WM observes interrupt transitions but does NOT emit
// workspace_interrupted.
//
// Spec ref: specs/workspace-model.md §4.10 WM-039.
//
// WM-039 states: on any transition of interrupt_state from none to a non-none value, the
// workspace manager MUST update the record atomically with the causing input such that the
// new state is durable and observable by the reconciliation detector. The workspace_interrupted
// event per event-model.md §8.5.5 is emitted by the reconciliation detector, NOT by the
// workspace manager. The workspace manager MUST NOT emit workspace_interrupted.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending; this
// sensor verifies that WM-039 is correctly declared in the spec so that:
//
//  1. WM-039 heading is present in specs/workspace-model.md.
//  2. "MUST update the record atomically" is declared.
//  3. "workspace_interrupted" event name is present in the body.
//  4. "emitted by the reconciliation detector" is declared for workspace_interrupted.
//  5. "MUST NOT emit workspace_interrupted" is declared for the workspace manager.
//  6. "split of emission authority" is named as the design rationale.
//  7. Tags: mechanism is present in the WM-039 body window.
//
// # Failure modes
//
//   - WM-039 heading missing.
//   - MUST update the record atomically absent.
//   - workspace_interrupted absent.
//   - emitted by the reconciliation detector absent.
//   - MUST NOT emit absent.
//   - split of emission authority absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm039Fixture prefix per
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

// wm039FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm039FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm039FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm039FixtureHeading matches the WM-039 level-4 requirement heading line.
var wm039FixtureHeading = regexp.MustCompile(`^#### WM-039 —`)

// wm039FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm039FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm039FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm039FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm039FixtureBodyWindow is the maximum number of lines to scan after the heading.
const wm039FixtureBodyWindow = 30

// wm039FixtureLoadLines opens specFile and returns all lines.
func wm039FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm039FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm039FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm039FixtureBodyLines returns the lines comprising the WM-039 body.
func wm039FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm039FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-039 heading not found; expected '#### WM-039 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm039FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm039FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm039FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm039FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM039WorkspaceInterruptedEmitterIsReconciliation is the binding test for hk-8mwo.53.
func TestWM039WorkspaceInterruptedEmitterIsReconciliation(t *testing.T) {
	t.Parallel()

	specFile := wm039FixtureWorkspaceModelPath(t)
	lines := wm039FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm039FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-039 check(1): %s", reason)
	}
	t.Logf("WM-039 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "must-update-record-atomically",
			needle: "MUST update the record atomically",
			detail: "WM-039 body must declare 'MUST update the record atomically' with causing input " +
				"(expected phrase 'MUST update the record atomically'); atomic update ensures the " +
				"reconciliation detector always sees a consistent pair of (interrupt_state, causing_input) — " +
				"a torn update could leave the record in a state where the cause is unknown",
		},
		{
			id:     "3",
			label:  "workspace-interrupted-event-named",
			needle: "workspace_interrupted",
			detail: "WM-039 body must name 'workspace_interrupted' as the relevant event " +
				"(expected phrase 'workspace_interrupted'); this is the bus event that signals " +
				"interrupt_state transitions to downstream consumers — WM-039 defines the split " +
				"of emission authority for this event",
		},
		{
			id:     "4",
			label:  "emitted-by-reconciliation-detector",
			needle: "emitted by the reconciliation detector",
			detail: "WM-039 body must declare workspace_interrupted is 'emitted by the reconciliation detector' " +
				"(expected phrase 'emitted by the reconciliation detector'); the reconciliation detector " +
				"is the sole emitter of workspace_interrupted on the bus — the workspace manager " +
				"owns the FIELD (interrupt_state) but NOT the wire EVENT",
		},
		{
			id:     "5",
			label:  "wm-must-not-emit-workspace-interrupted",
			needle: "MUST NOT emit",
			detail: "WM-039 body must declare workspace manager 'MUST NOT emit' workspace_interrupted " +
				"(expected phrase 'MUST NOT emit'); if WM also emitted workspace_interrupted, " +
				"downstream consumers would see duplicate events for operator-driven and reconciliation-driven " +
				"interrupts, breaking deduplication and count-based invariants",
		},
		{
			id:     "6",
			label:  "split-of-emission-authority",
			needle: "split of emission authority",
			detail: "WM-039 body must name 'split of emission authority' as the design rationale " +
				"(expected phrase 'split of emission authority'); WM owns the FIELD; reconciliation owns " +
				"the EVENT — this split is the architectural choice that prevents WM from coupling " +
				"to EV's event taxonomy",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm039FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-039 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-039 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in WM-039 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm039FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-039 check(7) FAILED: Tags: mechanism not found in WM-039 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-039 body)\n"+
					"  detail: WM-039 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.53 audit complete — WM-039 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
