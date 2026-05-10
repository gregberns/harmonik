package specaudit_test

// hk-8mup.32 + hk-8mup.33 binding test — PL-020 composition root is internal/daemon;
// PL-020a cross-subsystem registries reside in the composition root.
// Coalesced per §2.3 (consecutive headings in the same spec section, related scope).
//
// Spec ref: specs/process-lifecycle.md §4.6 PL-020 and PL-020a.
//
// PL-020 states: the daemon's code organization MUST treat the `internal/daemon` Go
// package as the composition root. Only `internal/daemon` is allowed to import across
// subsystem boundaries. Subsystems MUST NOT import each other directly except through
// the interfaces each subsystem exposes.
//
// PL-020a states: all cross-subsystem registries — including the event bus, the
// control-point registry, the handler registry, the skill registry, and the policy
// registry — MUST be instantiated inside the composition root (`internal/daemon`) on
// startup per PL-005 step 0. No out-of-daemon registry is permitted for MVH per AR-INV-007.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that PL-020 and PL-020a are correctly declared in the spec so that:
//
// PL-020 checks:
//  1. PL-020 heading is present in specs/process-lifecycle.md.
//  2. "internal/daemon" is declared as the composition root package.
//  3. "MUST NOT import each other directly" is declared for subsystems.
//  4. Tags: mechanism is present in the PL-020 body window.
//
// PL-020a checks:
//  5. PL-020a heading is present in specs/process-lifecycle.md.
//  6. "skill registry" is named as a cross-subsystem registry in the composition root.
//  7. "No out-of-daemon registry" is declared for MVH.
//  8. Tags: mechanism is present in the PL-020a body window.
//
// # Failure modes
//
//   - PL-020 heading missing.
//   - internal/daemon absent.
//   - MUST NOT import each other directly absent.
//   - PL-020 Tags: mechanism missing.
//   - PL-020a heading missing.
//   - skill registry absent.
//   - No out-of-daemon registry absent.
//   - PL-020a Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the pl020Fixture prefix per
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

// pl020FixtureProcessLifecyclePath returns the absolute path to specs/process-lifecycle.md.
func pl020FixtureProcessLifecyclePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("pl020FixtureProcessLifecyclePath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "process-lifecycle.md")
}

// pl020FixturePL020Heading matches the PL-020 level-4 requirement heading line.
var pl020FixturePL020Heading = regexp.MustCompile(`^#### PL-020 —`)

// pl020FixturePL020aHeading matches the PL-020a level-4 requirement heading line.
var pl020FixturePL020aHeading = regexp.MustCompile(`^#### PL-020a —`)

// pl020FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var pl020FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// pl020FixtureTagsMechanism matches a "Tags: mechanism" line.
var pl020FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// pl020FixtureBodyWindow is the maximum number of lines to scan after each heading.
const pl020FixtureBodyWindow = 15

// pl020FixtureLoadLines opens specFile and returns all lines.
func pl020FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("pl020FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("pl020FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// pl020FixturePL020BodyLines returns the lines comprising the PL-020 body.
func pl020FixturePL020BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if pl020FixturePL020Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "PL-020 heading not found; expected '#### PL-020 —' in specs/process-lifecycle.md"
	}

	limit := headingIdx + 1 + pl020FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if pl020FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// pl020FixturePL020aBodyLines returns the lines comprising the PL-020a body.
func pl020FixturePL020aBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if pl020FixturePL020aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "PL-020a heading not found; expected '#### PL-020a —' in specs/process-lifecycle.md"
	}

	limit := headingIdx + 1 + pl020FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if pl020FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// pl020FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func pl020FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestPL020CompositionRootAndRegistries is the binding test for hk-8mup.32 (PL-020) and
// hk-8mup.33 (PL-020a), coalesced per §2.3.
func TestPL020CompositionRootAndRegistries(t *testing.T) {
	t.Parallel()

	specFile := pl020FixtureProcessLifecyclePath(t)
	lines := pl020FixtureLoadLines(t, specFile)

	// --- PL-020 checks ---

	pl020Body, pl020LineNo, pl020Reason := pl020FixturePL020BodyLines(lines)
	if pl020Reason != "" {
		t.Fatalf("PL-020 check(1): %s", pl020Reason)
	}
	t.Logf("PL-020 heading found at specs/process-lifecycle.md line %d; body window = %d lines",
		pl020LineNo, len(pl020Body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
		body   []string
		lineNo int
		req    string
	}

	checks := []check{
		{
			id:     "2",
			label:  "internal-daemon-composition-root",
			needle: "internal/daemon",
			detail: "PL-020 body must declare 'internal/daemon' as the composition root package " +
				"(expected phrase 'internal/daemon'); this is the Go package path that is the sole " +
				"permitted cross-boundary importer — all subsystem wiring happens here",
			body:   pl020Body,
			lineNo: pl020LineNo,
			req:    "PL-020",
		},
		{
			id:     "3",
			label:  "subsystems-must-not-import-each-other-directly",
			needle: "MUST NOT import each other directly",
			detail: "PL-020 body must declare subsystems 'MUST NOT import each other directly' " +
				"(expected phrase 'MUST NOT import each other directly'); direct cross-subsystem " +
				"imports bypass the composition root wiring discipline and create hidden coupling — " +
				"subsystems must expose interfaces and let the composition root wire them",
			body:   pl020Body,
			lineNo: pl020LineNo,
			req:    "PL-020",
		},
	}

	// --- PL-020a checks ---

	pl020aBody, pl020aLineNo, pl020aReason := pl020FixturePL020aBodyLines(lines)
	if pl020aReason != "" {
		t.Fatalf("PL-020a check(5): %s", pl020aReason)
	}
	t.Logf("PL-020a heading found at specs/process-lifecycle.md line %d; body window = %d lines",
		pl020aLineNo, len(pl020aBody))

	checks = append(checks,
		check{
			id:     "6",
			label:  "skill-registry-named",
			needle: "skill registry",
			detail: "PL-020a body must name 'skill registry' as a cross-subsystem registry " +
				"(expected phrase 'skill registry'); the skill registry (HC §4.11) is one of the " +
				"five named registries that MUST be instantiated in internal/daemon on startup — " +
				"its presence confirms the composition-root obligation covers the skill subsystem",
			body:   pl020aBody,
			lineNo: pl020aLineNo,
			req:    "PL-020a",
		},
		check{
			id:     "7",
			label:  "no-out-of-daemon-registry",
			needle: "No out-of-daemon registry",
			detail: "PL-020a body must declare 'No out-of-daemon registry' is permitted for MVH " +
				"(expected phrase 'No out-of-daemon registry'); this is the negative constraint that " +
				"forbids external-process registries in MVH — post-MVH process-geometry changes " +
				"are tracked via AR-019 and do not require a PL amendment",
			body:   pl020aBody,
			lineNo: pl020aLineNo,
			req:    "PL-020a",
		},
	)

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !pl020FixtureBodyContains(c.body, c.needle) {
				t.Errorf(
					"%s check(%s) FAILED: %s\n"+
						"  spec:    specs/process-lifecycle.md line ~%d (%s body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.req, c.id, c.label, c.lineNo, c.req, c.needle, c.detail,
				)
			}
		})
	}

	// Check (4): Tags: mechanism in PL-020 body.
	t.Run("check-4-pl020-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range pl020Body {
			if pl020FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"PL-020 check(4) FAILED: Tags: mechanism not found in PL-020 body window\n"+
					"  spec:   specs/process-lifecycle.md line ~%d (PL-020 body)\n"+
					"  detail: PL-020 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				pl020LineNo,
			)
		}
	})

	// Check (8): Tags: mechanism in PL-020a body.
	t.Run("check-8-pl020a-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range pl020aBody {
			if pl020FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"PL-020a check(8) FAILED: Tags: mechanism not found in PL-020a body window\n"+
					"  spec:   specs/process-lifecycle.md line ~%d (PL-020a body)\n"+
					"  detail: PL-020a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				pl020aLineNo,
			)
		}
	})

	t.Logf("hk-8mup.32/.33 audit complete — PL-020 at line %d, PL-020a at line %d",
		pl020LineNo, pl020aLineNo)
}
