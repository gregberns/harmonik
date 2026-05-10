package specaudit_test

// hk-8mwo.42 binding test — WM-030 post-merge session-log retention default.
//
// Spec ref: specs/workspace-model.md §4.7 WM-030.
//
// WM-030 states: on successful merge (workspace_merge_status with status=merged), the
// workspace manager MUST preserve the sessions directory inside the merged branch by
// default. The MVH default requires that project .gitignore MUST NOT exclude
// .harmonik/sessions/; a gitignored sessions directory silently breaks the
// preserve-in-merged-branch contract.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The workspace manager implementation is pending;
// this sensor verifies that WM-030 is correctly declared in the spec so that:
//
//  1. WM-030 heading is present in specs/workspace-model.md.
//  2. "workspace_merge_status" with "status=merged" is the trigger event named.
//  3. "MUST preserve the sessions directory" is declared.
//  4. "preserve-in-merged-branch" is named as the MVH default behavior.
//  5. ".gitignore MUST NOT exclude .harmonik/sessions/" is declared.
//  6. Tags: mechanism is present in the WM-030 body window.
//
// # Failure modes
//
//   - WM-030 heading missing.
//   - workspace_merge_status absent.
//   - MUST preserve sessions directory absent.
//   - preserve-in-merged-branch absent.
//   - .gitignore MUST NOT exclude absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm030Fixture prefix per
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

// wm030FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm030FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm030FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm030FixtureWM030Heading matches the WM-030 level-4 requirement heading line.
var wm030FixtureWM030Heading = regexp.MustCompile(`^#### WM-030 —`)

// wm030FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm030FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm030FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm030FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm030FixtureBodyWindow is the maximum number of lines after the WM-030
// heading to scan for requirement-body content.
const wm030FixtureBodyWindow = 30

// wm030FixtureLoadLines opens specFile and returns all lines.
func wm030FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm030FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm030FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm030FixtureWM030BodyLines returns the lines comprising the WM-030 body.
func wm030FixtureWM030BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm030FixtureWM030Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-030 heading not found; expected '#### WM-030 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm030FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm030FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm030FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm030FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM030PostMergeSessionLogRetentionDefault is the binding test for hk-8mwo.42.
func TestWM030PostMergeSessionLogRetentionDefault(t *testing.T) {
	t.Parallel()

	specFile := wm030FixtureWorkspaceModelPath(t)
	lines := wm030FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm030FixtureWM030BodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-030 check(1): %s", reason)
	}
	t.Logf("WM-030 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "workspace-merge-status-trigger",
			needle: "workspace_merge_status",
			detail: "WM-030 body must name 'workspace_merge_status' as the trigger event " +
				"(expected phrase 'workspace_merge_status'); this is the event that signals a " +
				"successful merge — the workspace manager observes this event to know it MUST " +
				"preserve the sessions directory inside the merged branch",
		},
		{
			id:     "3",
			label:  "must-preserve-sessions-directory",
			needle: "MUST preserve the sessions directory",
			detail: "WM-030 body must declare 'MUST preserve the sessions directory' " +
				"(expected phrase 'MUST preserve the sessions directory'); this is the normative " +
				"obligation that ensures session logs survive the merge operation and remain " +
				"in the integration-branch commit tree for audit retention",
		},
		{
			id:     "4",
			label:  "preserve-in-merged-branch-default",
			needle: "preserve-in-merged-branch",
			detail: "WM-030 body must name 'preserve-in-merged-branch' as the MVH default behavior " +
				"(expected phrase 'preserve-in-merged-branch'); this is the canonical label for " +
				"the default retention strategy — it distinguishes the MVH behavior from the " +
				"post-MVH alternative of moving logs to an archive path",
		},
		{
			id:     "5",
			label:  "gitignore-must-not-exclude-sessions",
			needle: "MUST NOT exclude `.harmonik/sessions/`",
			detail: "WM-030 body must declare 'MUST NOT exclude `.harmonik/sessions/`' " +
				"(expected phrase 'MUST NOT exclude `.harmonik/sessions/`'); a gitignored sessions " +
				"directory silently breaks the preserve-in-merged-branch contract — naming this " +
				"constraint makes it operator-observable and testable via pre-merge hygiene checks",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm030FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-030 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-030 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in WM-030 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm030FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-030 check(6) FAILED: Tags: mechanism not found in WM-030 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-030 body)\n"+
					"  detail: WM-030 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.42 audit complete — WM-030 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
