package specaudit_test

// hk-8mwo.41 binding test — WM-029 session-log directory is consumed read-only by S08.
//
// Spec ref: specs/workspace-model.md §4.7 WM-029.
//
// WM-029 states: the session-log directory (including the metadata sidecar and handler-
// written logs) MUST be treated as read-only from the memory-layer subsystem's perspective.
// S08 indexes contents into CASS without mutating any file under
// ${workspace_path}/.harmonik/sessions/. MVH does not declare a mechanical sensor for
// this obligation; the read-only contract is enforced by reviewer discipline until a
// memory-layer spec names an operator-auditable permission or audit hook.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The memory-layer (S08) implementation is pending;
// this sensor verifies that WM-029 is correctly declared in the spec so that:
//
//  1. WM-029 heading is present in specs/workspace-model.md.
//  2. "MUST be treated as read-only" is declared.
//  3. "S08" is named as the consumer subject of the read-only obligation.
//  4. ".harmonik/sessions/" is named as the protected path.
//  5. "indexes contents into CASS" describes the permitted operation.
//  6. Tags: mechanism is present in the WM-029 body window.
//
// # Failure modes
//
//   - WM-029 heading missing.
//   - MUST be treated as read-only absent.
//   - S08 absent.
//   - .harmonik/sessions/ absent.
//   - indexes contents into CASS absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the wm029Fixture prefix per
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

// wm029FixtureWorkspaceModelPath returns the absolute path to specs/workspace-model.md.
func wm029FixtureWorkspaceModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("wm029FixtureWorkspaceModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "workspace-model.md")
}

// wm029FixtureWM029Heading matches the WM-029 level-4 requirement heading line.
var wm029FixtureWM029Heading = regexp.MustCompile(`^#### WM-029 —`)

// wm029FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var wm029FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// wm029FixtureTagsMechanism matches a "Tags: mechanism" line.
var wm029FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// wm029FixtureBodyWindow is the maximum number of lines after the WM-029
// heading to scan for requirement-body content.
const wm029FixtureBodyWindow = 30

// wm029FixtureLoadLines opens specFile and returns all lines.
func wm029FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("wm029FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("wm029FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// wm029FixtureWM029BodyLines returns the lines comprising the WM-029 body.
func wm029FixtureWM029BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if wm029FixtureWM029Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "WM-029 heading not found; expected '#### WM-029 —' in specs/workspace-model.md"
	}

	limit := headingIdx + 1 + wm029FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if wm029FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// wm029FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func wm029FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestWM029SessionLogDirectoryReadOnlyByS08 is the binding test for hk-8mwo.41.
func TestWM029SessionLogDirectoryReadOnlyByS08(t *testing.T) {
	t.Parallel()

	specFile := wm029FixtureWorkspaceModelPath(t)
	lines := wm029FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := wm029FixtureWM029BodyLines(lines)
	if reason != "" {
		t.Fatalf("WM-029 check(1): %s", reason)
	}
	t.Logf("WM-029 heading found at specs/workspace-model.md line %d; body window = %d lines",
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
			label:  "must-be-treated-as-read-only",
			needle: "MUST be treated as read-only",
			detail: "WM-029 body must declare 'MUST be treated as read-only' " +
				"(expected phrase 'MUST be treated as read-only'); this is the normative " +
				"obligation that prevents the memory-layer subsystem from mutating session logs — " +
				"session logs are write-once artifacts owned by the handler-contract subsystem",
		},
		{
			id:     "3",
			label:  "s08-named-as-consumer",
			needle: "S08",
			detail: "WM-029 body must name 'S08' as the consumer subject of the read-only obligation " +
				"(expected phrase 'S08'); S08 is the memory-layer subsystem that indexes session " +
				"log contents into CASS — naming it explicitly binds the read-only constraint to " +
				"the correct subsystem",
		},
		{
			id:     "4",
			label:  "harmonik-sessions-path-named",
			needle: ".harmonik/sessions/",
			detail: "WM-029 body must name '.harmonik/sessions/' as the protected path " +
				"(expected phrase '.harmonik/sessions/'); this is the canonical location of " +
				"session logs under the workspace path — naming the path makes the read-only " +
				"obligation machine-checkable by linting or audit hooks",
		},
		{
			id:     "5",
			label:  "indexes-into-cass",
			needle: "indexes",
			detail: "WM-029 body must describe S08 'indexes' session-log contents into CASS " +
				"(expected phrase 'indexes'); indexing (read-only access to build a searchable index) " +
				"is the permitted operation — mutation is forbidden; naming the permitted operation " +
				"makes the boundary precise",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !wm029FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"WM-029 check(%s) FAILED: %s\n"+
						"  spec:    specs/workspace-model.md line ~%d (WM-029 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in WM-029 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if wm029FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"WM-029 check(6) FAILED: Tags: mechanism not found in WM-029 body window\n"+
					"  spec:   specs/workspace-model.md line ~%d (WM-029 body)\n"+
					"  detail: WM-029 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mwo.41 audit complete — WM-029 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
