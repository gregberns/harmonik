//go:build specaudit

package specaudit_test

// hk-zs0.17 binding test — AR-016 subsystem is a Go package inside the daemon for MVH.
//
// Spec ref: specs/architecture.md §4.5 AR-016.
//
// AR-016 states: "For MVH, a subsystem MUST be realized as a Go package inside
// the daemon binary (see [docs/foundation/components.md §8] for the daemon's
// single-binary shape). The envelope's events-produced and events-consumed
// discipline is therefore inter-package discipline within a single process; the
// event bus is in-process. The consumer taxonomy's in-process-synchronous,
// in-process-asynchronous, and fan-out-observer classes all live inside the same
// daemon binary."
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that AR-016 is correctly declared
// in specs/architecture.md with the required structural text so that the rule
// cannot be silently eroded by spec edits:
//
//  1. AR-016 heading is present in specs/architecture.md.
//  2. "Go package inside the daemon binary" is declared in the body.
//  3. "in-process" event bus is declared in the body.
//  4. Tags: mechanism is present in the AR-016 body window.
//
// The test does NOT assert that every subsystem's Go package exists on disk —
// that is an implementation-time obligation enforced by the compiler and PL-020
// (composition root). This test guards the spec text itself.
//
// # Failure modes
//
//   - AR-016 heading missing from specs/architecture.md.
//   - "Go package inside the daemon binary" absent from AR-016 body.
//   - "in-process" absent from AR-016 body (event bus in-process discipline).
//   - Tags: mechanism absent from AR-016 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ar016Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// ar016FixtureArchitecturePath returns the absolute path to specs/architecture.md.
func ar016FixtureArchitecturePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar016FixtureArchitecturePath: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar016_subsystem_go_package_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "architecture.md")
}

// ar016FixtureAR016Heading matches the AR-016 level-4 requirement heading line.
var ar016FixtureAR016Heading = regexp.MustCompile(`^#### AR-016 —`)

// ar016FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var ar016FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ar016FixtureTagsMechanism matches a "Tags: mechanism" line.
var ar016FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// ar016FixtureBodyWindow is the maximum number of lines to scan after the heading.
const ar016FixtureBodyWindow = 15

// ar016FixtureLoadLines opens specFile and returns all lines.
func ar016FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar016FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar016FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// ar016FixtureAR016BodyLines returns the lines comprising the AR-016 body window
// (up to ar016FixtureBodyWindow lines after the heading, stopping at the next
// heading). Returns (nil, 0, reason) if the heading is not found.
func ar016FixtureAR016BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if ar016FixtureAR016Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "AR-016 heading not found; expected '#### AR-016 —' in specs/architecture.md"
	}

	limit := headingIdx + 1 + ar016FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if ar016FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// ar016FixtureBodyContains reports whether any line in body contains substr
// (case-sensitive substring match).
func ar016FixtureBodyContains(body []string, substr string) bool {
	for _, line := range body {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// TestAR016SubsystemGoPackage is the binding test for hk-zs0.17 (AR-016).
//
// It verifies that AR-016 is correctly declared in specs/architecture.md with
// the four required structural elements:
//
//  1. AR-016 heading present.
//  2. "Go package inside the daemon binary" in body.
//  3. "in-process" in body (event bus in-process discipline).
//  4. Tags: mechanism in body window.
func TestAR016SubsystemGoPackage(t *testing.T) {
	t.Parallel()

	specFile := ar016FixtureArchitecturePath(t)
	lines := ar016FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := ar016FixtureAR016BodyLines(lines)
	if reason != "" {
		t.Fatalf("AR-016 check(1): %s", reason)
	}
	t.Logf("AR-016 heading found at specs/architecture.md line %d; body window = %d lines",
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
			label:  "go-package-inside-daemon-binary",
			needle: "Go package inside the daemon binary",
			detail: "AR-016 body must declare 'Go package inside the daemon binary' " +
				"(expected phrase 'Go package inside the daemon binary'); this is the core " +
				"MVH realization rule — every subsystem is a Go package in the single daemon " +
				"binary, not a separate process. Its absence means the rule has been silently removed.",
		},
		{
			id:     "3",
			label:  "event-bus-in-process",
			needle: "in-process",
			detail: "AR-016 body must declare 'in-process' event bus discipline " +
				"(expected phrase 'in-process'); this is the architectural consequence of the " +
				"single-binary rule — events cross subsystem boundaries within the same process, " +
				"not via IPC. Its absence means the in-process constraint has been eroded.",
		},
	}

	for _, c := range checks {
		c := c
		t.Run("check-"+c.id+"-"+strings.ReplaceAll(c.label, "-", "_"), func(t *testing.T) {
			t.Parallel()
			if !ar016FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"AR-016 check(%s) FAILED: %s\n"+
						"  spec:    specs/architecture.md line ~%d (AR-016 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (4): Tags: mechanism in AR-016 body.
	t.Run("check-4-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if ar016FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"AR-016 check(4) FAILED: Tags: mechanism not found in AR-016 body window\n"+
					"  spec:   specs/architecture.md line ~%d (AR-016 body)\n"+
					"  detail: AR-016 carries tag 'mechanism' (deterministic, daemon-side realization rule); "+
					"its absence indicates the requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-zs0.17 audit complete — AR-016 at line %d, body window %d lines; all checks passed",
		headingLineNo, len(body))
}
