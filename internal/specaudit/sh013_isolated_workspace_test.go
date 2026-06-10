package specaudit_test

// sh013_isolated_workspace_test.go — spec-corpus sensor for SH-013.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-013.
//
// SH-013 states: each scenario's workspace MUST be disjoint from every other
// scenario's workspace and from the operator's working tree. "Disjoint" means:
// no two scenarios' canonical workspace paths share a prefix and no symlink
// under one workspace resolves to a path under another. The harness MUST place
// each scenario's workspace at `<fixture-root>/<scenario-name>/workspace/`;
// workspaces MUST be created under a per-suite ephemeral root (SH-016) and
// MUST NOT reuse any path from a prior suite invocation. The harness MUST
// verify no path-collision at suite-load.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-013 is present and declares:
//
//  1. Heading present — "#### SH-013 —".
//  2. "Disjoint" definition: no shared prefix AND no symlink crosses.
//  3. Workspace path shape: <fixture-root>/<scenario-name>/workspace/.
//  4. Per-suite ephemeral root placement (MUST NOT reuse prior suite paths).
//  5. Suite-load path-collision lint (MUST verify no collision).
//  6. Tags: mechanism.
//
// # Helper prefix: sh013Isolated

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

func sh013IsolatedScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh013IsolatedScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh013IsolatedLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh013IsolatedLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh013IsolatedLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh013IsolatedSH013Heading      = regexp.MustCompile(`^#### SH-013 —`)
	sh013IsolatedAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh013IsolatedTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

const sh013IsolatedBodyWindow = 30

func sh013IsolatedBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh013IsolatedSH013Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-013 heading '#### SH-013 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh013IsolatedBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh013IsolatedAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh013IsolatedBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH013IsolatedWorkspace is the spec-corpus binding test for SH-013.
func TestSH013IsolatedWorkspace(t *testing.T) {
	t.Parallel()

	specFile := sh013IsolatedScenarioHarnessPath(t)
	lines := sh013IsolatedLoadLines(t, specFile)

	body, headingLineNo, reason := sh013IsolatedBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-013 check(a): %s", reason)
	}
	t.Logf("SH-013 heading found at specs/scenario-harness.md line %d; body = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:    "b",
			label: "disjoint-definition-no-shared-prefix",
			// SH-013: "no two scenarios' canonical workspace paths share a prefix"
			needle: "share a prefix",
			detail: "SH-013 body must define 'disjoint' to include 'no two scenarios\\' canonical workspace paths " +
				"share a prefix'; this is the structural invariant the cross-scenario isolation property rests on",
		},
		{
			id:    "c",
			label: "disjoint-definition-no-symlink-crosses",
			// SH-013: "no symlink under one workspace resolves to a path under another"
			needle: "symlink",
			detail: "SH-013 body must define 'disjoint' to include no-symlink-crossing; " +
				"absence means the spec is silent on symlink traversal attacks between sibling workspaces",
		},
		{
			id:    "d",
			label: "workspace-path-shape-fixture-root-scenario-workspace",
			// SH-013: "<fixture-root>/<scenario-name>/workspace/"
			needle: "workspace/",
			detail: "SH-013 body must declare the canonical workspace path shape " +
				"<fixture-root>/<scenario-name>/workspace/; " +
				"absence means path-shape compliance cannot be verified from the spec text alone",
		},
		{
			id:    "e",
			label: "per-suite-ephemeral-root-no-reuse",
			// SH-013: "MUST NOT reuse any path from a prior suite invocation"
			needle: "MUST NOT reuse",
			detail: "SH-013 body must forbid reusing any path from a prior suite invocation; " +
				"absence means the no-reuse property is not normatively bound to the workspace isolation contract",
		},
		{
			id:    "f",
			label: "suite-load-path-collision-lint",
			// SH-013: "The harness MUST verify no path-collision at suite-load"
			needle: "path-collision",
			detail: "SH-013 body must require the harness to verify no path-collision at suite-load; " +
				"this is the fail-fast lint that catches naming-convention violations before fixture-setup begins",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh013IsolatedBodyContains(body, c.needle) {
				t.Errorf(
					"SH-013 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-013 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-g-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh013IsolatedTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-013 check(g) FAILED: Tags: mechanism not found in SH-013 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-013 body)\n"+
					"  detail: SH-013 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-013 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
