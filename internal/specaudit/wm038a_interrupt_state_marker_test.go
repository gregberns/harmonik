package specaudit_test

// hk-8mwo.52 binding test — WM-038a workspace-local marker for operator/verdict-driven
// interrupt_state mutations.
//
// Spec ref: specs/workspace-model.md §4.10 WM-038a.
//
// WM-038a states: when the workspace manager mutates interrupt_state in response to an
// operator-control event (pause, stop, upgrade) OR a reconciliation verdict
// (crash-recovery disposition), the workspace manager MUST append a single workspace-scoped
// JSONL line to ${workspace_path}/.harmonik/events/workspace-<workspace_id>.jsonl and
// fsync that file. The marker event is "interrupt_state_changed" and includes workspace_id,
// run_id, prior_interrupt_state, new_interrupt_state, cause, cause_event_id, changed_at.
// The workspace-local marker is the authoritative durable record for reconciliation's
// consumer pass.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending; this
// sensor verifies that WM-038a is correctly declared in the spec so that:
//
//  1. WM-038a heading is present in specs/workspace-model.md.
//  2. "interrupt_state_changed" is named as the marker event type.
//  3. "fsync" is declared for the marker write.
//  4. "prior_interrupt_state" field is declared in the marker shape.
//  5. "cause" field is declared in the marker shape.
//  6. Marker is the "authoritative" durable record for reconciliation's consumer pass.
//  7. Tags: mechanism is present in the WM-038a body window.
//
// # Failure modes
//
//   - WM-038a heading missing.
//   - interrupt_state_changed event type absent.
//   - fsync absent.
//   - prior_interrupt_state field absent.
//   - cause field absent.
//   - authoritative durable record absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm038aFixture prefix per
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

// wm038aFixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm038aFixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm038aFixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm038aFixtureHeading matches the WM-038a level-4 requirement heading line.
var wm038aFixtureHeading = regexp.MustCompile(`^#### WM-038a —`)

// wm038aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm038aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm038aFixtureTagsMechanism matches a "Tags: mechanism" line.
var wm038aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm038aFixtureBodyWindow is the maximum number of lines to scan after the heading.
const wm038aFixtureBodyWindow = 40

// wm038aFixtureLoadLines opens specFile and returns all lines.
func wm038aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm038aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm038aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm038aFixtureBodyLines returns the lines comprising the WM-038a body.
func wm038aFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm038aFixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-038a heading not found; expected '#### WM-038a —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm038aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm038aFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm038aFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm038aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM038aInterruptStateWorkspaceLocalMarker is the binding test for hk-8mwo.52.
func TestWM038aInterruptStateWorkspaceLocalMarker(t *testing.T) {
	t.Parallel()

	specFile := wm038aFixtureWorkspaceModelPath(t)
	lines := wm038aFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm038aFixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-038a check(1): %s", reason)
	}
	t.Logf("WM-038a heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "interrupt-state-changed-event-type",
			needle: "interrupt_state_changed",
			detail: "WM-038a body must name 'interrupt_state_changed' as the marker event type " +
				"(expected phrase 'interrupt_state_changed'); this is the JSONL event appended to " +
				"the workspace-scoped event file on every interrupt_state mutation — it is the " +
				"durable record of the transition for reconciliation's consumer pass",
		},
		{
			id:     "3",
			label:  "fsync-declared-for-marker",
			needle: "fsync",
			detail: "WM-038a body must declare 'fsync' for the marker write " +
				"(expected phrase 'fsync'); fsync ensures the JSONL line is durable before " +
				"the interrupt_state mutation is considered complete — without fsync, a crash " +
				"between the write and flush could leave the marker incomplete",
		},
		{
			id:     "4",
			label:  "prior-interrupt-state-field",
			needle: "prior_interrupt_state",
			detail: "WM-038a body must declare 'prior_interrupt_state' field in the marker shape " +
				"(expected phrase 'prior_interrupt_state'); the prior state is needed so " +
				"reconciliation can verify the transition was expected — a transition from " +
				"'none' to 'operator-paused' is different from 'operator-paused' to 'operator-stopped-graceful'",
		},
		{
			id:     "5",
			label:  "cause-field-in-marker",
			needle: `"cause"`,
			detail: "WM-038a body must declare 'cause' field in the marker shape " +
				"(expected phrase '\"cause\"'); the cause field names the operator-event-type or " +
				"verdict-kind that triggered the interrupt_state mutation — it connects the durable " +
				"marker to the event or verdict that caused it",
		},
		{
			id:     "6",
			label:  "authoritative-durable-record",
			needle: "authoritative record",
			detail: "WM-038a body must declare the marker is the 'authoritative record' for reconciliation " +
				"(expected phrase 'authoritative record'); the local marker is more durable than the " +
				"in-process bus event (which is fire-and-forget) — reconciliation's consumer pass " +
				"uses the marker file, not the bus event, as its source of truth",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm038aFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-038a check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-038a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in WM-038a body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm038aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-038a check(7) FAILED: Tags: mechanism not found in WM-038a body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-038a body)\n"+
					"  detail: WM-038a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.52 audit complete — WM-038a heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
