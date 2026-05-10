package specaudit_test

// hk-8mup.13 binding test — PL-007 orphan sweep is deterministic and complete before
// reconciliation classification.
//
// Spec ref: specs/process-lifecycle.md §4.2 PL-007.
//
// PL-007 states: the orphan sweep MUST be deterministic given the filesystem + process
// state AND the project-scoped provenance marker of PL-006a. After the sweep completes,
// no harmonik-owned process bearing this project's provenance marker from a prior daemon
// instance is alive and no harmonik-owned worktree is locked by a prior-instance lease.
// The sweep MUST NOT match on binary path alone and MUST NOT kill a process lacking a
// valid project-scoped marker.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that PL-007 is correctly declared in the spec so that:
//
//  1. PL-007 heading is present in specs/process-lifecycle.md.
//  2. "MUST be deterministic" is declared for the orphan sweep.
//  3. "provenance marker" is named as the scoping mechanism.
//  4. "MUST NOT match on binary path alone" is declared.
//  5. "MUST NOT kill a process lacking a valid" is declared.
//  6. Tags: mechanism is present in the PL-007 body window.
//
// # Failure modes
//
//   - PL-007 heading missing.
//   - MUST be deterministic absent.
//   - provenance marker absent.
//   - MUST NOT match on binary path alone absent.
//   - MUST NOT kill a process lacking a valid absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the pl007Fixture prefix per
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

// pl007FixtureProcessLifecyclePath returns the absolute path to specs/process-lifecycle.md.
func pl007FixtureProcessLifecyclePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("pl007FixtureProcessLifecyclePath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "process-lifecycle.md")
}

// pl007FixtureHeading matches the PL-007 level-4 requirement heading line.
var pl007FixtureHeading = regexp.MustCompile(`^#### PL-007 —`)

// pl007FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var pl007FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// pl007FixtureTagsMechanism matches a "Tags: mechanism" line.
var pl007FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// pl007FixtureBodyWindow is the maximum number of lines to scan after the heading.
const pl007FixtureBodyWindow = 30

// pl007FixtureLoadLines opens specFile and returns all lines.
func pl007FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("pl007FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("pl007FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// pl007FixtureBodyLines returns the lines comprising the PL-007 body.
func pl007FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if pl007FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "PL-007 heading not found; expected '#### PL-007 —' in specs/process-lifecycle.md"
	}

	limit := headingIdx + 1 + pl007FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if pl007FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// pl007FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func pl007FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestPL007OrphanSweepDeterministic is the binding test for hk-8mup.13.
func TestPL007OrphanSweepDeterministic(t *testing.T) {
	t.Parallel()

	specFile := pl007FixtureProcessLifecyclePath(t)
	lines := pl007FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := pl007FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("PL-007 check(1): %s", reason)
	}
	t.Logf("PL-007 heading found at specs/process-lifecycle.md line %d; body window = %d lines",
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
			label:  "must-be-deterministic",
			needle: "MUST be deterministic",
			detail: "PL-007 body must declare 'MUST be deterministic' for the orphan sweep " +
				"(expected phrase 'MUST be deterministic'); determinism means that given the same " +
				"filesystem + process state and the same provenance marker, the sweep always produces " +
				"the same set of processes to kill and worktrees to unlock — no random or time-varying behavior",
		},
		{
			id:     "3",
			label:  "provenance-marker-scoping",
			needle: "provenance marker",
			detail: "PL-007 body must name 'provenance marker' as the scoping mechanism " +
				"(expected phrase 'provenance marker'); the project-scoped provenance marker (PL-006a) " +
				"is what distinguishes harmonik-owned processes for THIS project from processes belonging " +
				"to other projects or other tools — it prevents the sweep from killing unrelated processes",
		},
		{
			id:     "4",
			label:  "must-not-match-binary-path-alone",
			needle: "MUST NOT match on binary path alone",
			detail: "PL-007 body must declare 'MUST NOT match on binary path alone' " +
				"(expected phrase 'MUST NOT match on binary path alone'); matching only on the binary " +
				"path would kill harmonik processes belonging to other projects sharing the same binary — " +
				"the provenance marker is required to narrow the scope correctly",
		},
		{
			id:     "5",
			label:  "must-not-kill-without-valid-marker",
			needle: "MUST NOT kill a process lacking a valid",
			detail: "PL-007 body must declare 'MUST NOT kill a process lacking a valid' project-scoped marker " +
				"(expected phrase 'MUST NOT kill a process lacking a valid'); this is the negative constraint " +
				"that pairs with check(4) — a process that looks like harmonik but has no valid provenance " +
				"marker MUST be left alone, even if its binary path matches",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !pl007FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"PL-007 check(%s) FAILED: %s\n"+
						"  spec:    specs/process-lifecycle.md line ~%d (PL-007 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in PL-007 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if pl007FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"PL-007 check(6) FAILED: Tags: mechanism not found in PL-007 body window\n"+
					"  spec:   specs/process-lifecycle.md line ~%d (PL-007 body)\n"+
					"  detail: PL-007 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mup.13 audit complete — PL-007 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
