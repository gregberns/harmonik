//go:build specaudit

package specaudit_test

// hk-8mwo.51 binding test — WM-038 interrupt_state is set by operator and
// reconciliation pathways; WM is sole writer.
//
// Spec ref: specs/workspace-model.md §4.10 WM-038.
//
// WM-038 states: transitions of interrupt_state are driven by (a) the operator control
// subsystem emitting operator-control events that the workspace manager observes and
// applies; (b) reconciliation emitting verdicts that the workspace manager applies. The
// workspace manager MUST be the SOLE writer of the interrupt_state field on the Workspace
// record. The workspace manager MUST NOT mutate interrupt_state except in response to (a)
// an observed operator-control event or (b) an observed reconciliation verdict execution.
// No other subsystem may mutate the field; cross-subsystem requests to change
// interrupt_state MUST route through the event bus.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending;
// this sensor verifies that WM-038 is correctly declared in the spec so that:
//
//  1. WM-038 heading is present in specs/workspace-model.md.
//  2. "operator control subsystem" is named as the (a) pathway.
//  3. "reconciliation" is named as the (b) pathway emitting verdicts.
//  4. "SOLE writer" of the interrupt_state field is declared.
//  5. "MUST NOT mutate interrupt_state" except in response to (a) or (b) is declared.
//  6. "MUST route through the event bus" for cross-subsystem requests is declared.
//  7. Tags: mechanism is present in the WM-038 body window.
//
// # Failure modes
//
//   - WM-038 heading missing.
//   - operator control subsystem absent.
//   - reconciliation pathway absent.
//   - SOLE writer absent.
//   - MUST NOT mutate except in response absent.
//   - MUST route through event bus absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm038Fixture prefix per
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

// wm038FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm038FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm038FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm038FixtureWM038Heading matches the WM-038 level-4 requirement heading line.
var wm038FixtureWM038Heading = regexp.MustCompile(`^#### WM-038 —`)

// wm038FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm038FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm038FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm038FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm038FixtureBodyWindow is the maximum number of lines after the WM-038
// heading to scan for requirement-body content.
const wm038FixtureBodyWindow = 30

// wm038FixtureLoadLines opens specFile and returns all lines.
func wm038FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm038FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm038FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm038FixtureWM038BodyLines returns the lines comprising the WM-038 body.
func wm038FixtureWM038BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm038FixtureWM038Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-038 heading not found; expected '#### WM-038 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm038FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm038FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm038FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm038FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM038InterruptStateSoleWriter is the binding test for hk-8mwo.51.
func TestWM038InterruptStateSoleWriter(t *testing.T) {
	t.Parallel()

	specFile := wm038FixtureWorkspaceModelPath(t)
	lines := wm038FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm038FixtureWM038BodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-038 check(1): %s", reason)
	}
	t.Logf("WM-038 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "operator-control-subsystem-pathway",
			needle: "operator control subsystem",
			detail: "WM-038 body must name 'operator control subsystem' as the (a) interrupt_state pathway " +
				"(expected phrase 'operator control subsystem'); the operator control subsystem " +
				"emits operator-control events (pause, stop, upgrade) that the workspace manager " +
				"observes and translates into interrupt_state mutations",
		},
		{
			id:     "3",
			label:  "reconciliation-verdict-pathway",
			needle: "reconciliation",
			detail: "WM-038 body must name 'reconciliation' as the (b) pathway emitting verdicts " +
				"(expected phrase 'reconciliation'); reconciliation emits verdict events that the " +
				"workspace manager observes and executes — crash-recovery verdicts may also drive " +
				"interrupt_state transitions",
		},
		{
			id:     "4",
			label:  "sole-writer-declared",
			needle: "SOLE writer",
			detail: "WM-038 body must declare 'SOLE writer' of the interrupt_state field " +
				"(expected phrase 'SOLE writer'); the workspace manager is the exclusive writer — " +
				"no other subsystem may directly mutate interrupt_state, preventing concurrent " +
				"mutation races from operator and reconciliation events arriving simultaneously",
		},
		{
			id:     "5",
			label:  "must-not-mutate-except-in-response",
			needle: "MUST NOT mutate",
			detail: "WM-038 body must declare 'MUST NOT mutate' interrupt_state except in response to (a)/(b) " +
				"(expected phrase 'MUST NOT mutate'); this is the defensive complement to SOLE writer — " +
				"the workspace manager must not speculatively or eagerly mutate interrupt_state; " +
				"only event/verdict receipt is a valid trigger",
		},
		{
			id:     "6",
			label:  "must-route-through-event-bus",
			needle: "MUST route through the event bus",
			detail: "WM-038 body must declare cross-subsystem requests 'MUST route through the event bus' " +
				"(expected phrase 'MUST route through the event bus'); this is the subsystem boundary " +
				"enforcement mechanism — direct field mutations from other subsystems are forbidden; " +
				"all changes flow through the bus so the workspace manager can apply them atomically",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm038FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-038 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-038 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in WM-038 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm038FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-038 check(7) FAILED: Tags: mechanism not found in WM-038 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-038 body)\n"+
					"  detail: WM-038 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.51 audit complete — WM-038 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
