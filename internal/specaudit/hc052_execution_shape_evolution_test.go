package specaudit_test

// hk-8i31.62 binding test — HC-052 execution-shape evolution re-implements the
// adapter, not the watcher.
//
// Spec ref: specs/handler-contract.md §HC-052.
//
// HC-052 states: a future replacement of the execution shape (a custom tmux +
// agent-profile library, a cloud-execution shape, a remote-container shape)
// MUST re-implement the per-agent-type adapter of §4.3.HC-012 without altering
// the watcher of §4.3.HC-011. The concurrency pin is load-bearing: new shapes
// provide new adapters; they do not move the concurrency boundary.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The execution-shape adapter interface is
// pending; this sensor verifies that the HC-052 requirement is correctly
// declared in the spec so that:
//
//  1. HC-052 heading is present in specs/handler-contract.md — the requirement
//     exists in the normative spec.
//
//  2. Re-implement adapter not watcher is declared — shape evolution MUST target
//     the adapter (HC-012) and MUST NOT alter the watcher (HC-011).
//
//  3. HC-011 and HC-012 cross-references are declared — the requirement
//     normatively ties the constraint to the existing watcher and adapter specs.
//
//  4. Concurrency pin is load-bearing is declared — the language "load-bearing"
//     establishes that this is an architectural invariant, not guidance.
//
//  5. Tags: mechanism is present in the HC-052 body window.
//
// # Failure modes
//
//   - HC-052 heading missing: HC-052 heading not found in specs/handler-contract.md.
//   - Adapter re-implementation clause absent: adapter re-implementation for shape evolution not stated.
//   - HC-011/HC-012 cross-references absent: cross-references to watcher and adapter specs not stated.
//   - Concurrency-pin load-bearing absent: load-bearing concurrency pin not stated.
//   - Tags: mechanism missing from HC-052 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc052Fixture prefix per
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

// hc052FixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/hc052_execution_shape_evolution_test.go
//
// so the repo root is two directories up.
func hc052FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc052FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc052FixtureHC052Heading matches the HC-052 level-4 requirement heading line.
var hc052FixtureHC052Heading = regexp.MustCompile(`^#### HC-052 —`)

// hc052FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-052 requirement body window.
var hc052FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc052FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc052FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc052FixtureBodyWindow is the maximum number of lines after the HC-052
// heading to scan for requirement-body content.
const hc052FixtureBodyWindow = 30

// hc052FixtureLoadLines opens specFile and returns all lines.
func hc052FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc052FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc052FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc052FixtureHC052BodyLines returns the lines comprising the HC-052 requirement
// body: all lines after the HC-052 heading line up to (but not including) the
// next Markdown heading or hc052FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the HC-052 heading is not found.
func hc052FixtureHC052BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc052FixtureHC052Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-052 heading not found; expected '#### HC-052 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc052FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hc052FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc052FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hc052FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC052ExecutionShapeEvolution is the binding test for hk-8i31.62.
//
// It opens specs/handler-contract.md, locates the HC-052 heading, and validates:
//
//	(a) The HC-052 heading is present (the requirement exists in the spec).
//	(b) Re-implement adapter (not watcher) for shape evolution declared.
//	(c) HC-011 cross-reference (watcher) declared.
//	(d) HC-012 cross-reference (adapter) declared.
//	(e) Concurrency pin is load-bearing declared.
//	(f) Tags: mechanism is present in the body window.
func TestHC052ExecutionShapeEvolution(t *testing.T) {
	t.Parallel()

	specFile := hc052FixtureHandlerContractPath(t)
	lines := hc052FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc052FixtureHC052BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-052 check(a): %s", reason)
	}
	t.Logf("HC-052 heading found at specs/handler-contract.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "b",
			label:  "re-implement adapter not watcher for shape evolution",
			needle: "re-implement",
			detail: "HC-052 body must declare that shape evolution MUST re-implement the adapter " +
				"(expected phrase 're-implement'); this is the normative direction: new shapes " +
				"provide new adapters without touching the watcher boundary",
		},
		{
			id:     "c",
			label:  "HC-011 watcher cross-reference",
			needle: "HC-011",
			detail: "HC-052 body must cross-reference HC-011 for the watcher spec " +
				"(expected phrase 'HC-011'); this ties the prohibition on altering the watcher " +
				"to the normative watcher requirement",
		},
		{
			id:     "d",
			label:  "HC-012 adapter cross-reference",
			needle: "HC-012",
			detail: "HC-052 body must cross-reference HC-012 for the adapter spec " +
				"(expected phrase 'HC-012'); this identifies the per-agent-type adapter as the " +
				"correct re-implementation target for execution-shape changes",
		},
		{
			id:     "e",
			label:  "concurrency pin is load-bearing",
			needle: "load-bearing",
			detail: "HC-052 body must declare that the concurrency pin is load-bearing " +
				"(expected phrase 'load-bearing'); this language elevates the constraint from " +
				"guidance to an architectural invariant that MUST NOT be overridden by shape evolution",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc052FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-052 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-052 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (f): Tags: mechanism in HC-052 body.
	t.Run("check-f-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc052FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-052 check(f) FAILED: Tags: mechanism not found in HC-052 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-052 body)\n"+
					"  detail: HC-052 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.62 audit complete — HC-052 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
