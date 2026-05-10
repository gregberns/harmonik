package specaudit_test

// hk-8i31.18 binding test — HC-015 mutex discipline.
//
// Spec ref: specs/handler-contract.md §4.3.HC-015.
//
// HC-015 states: state transitions MUST acquire a per-run write lock; event
// publication MUST NOT block the state lock; event consumers MUST read from a
// per-subscriber channel; no mutex MAY be held across a call into S04's adapter.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The watcher implementation is pending;
// this sensor verifies that the HC-015 requirement is correctly declared in the
// spec so that:
//
//  1. HC-015 heading is present in specs/handler-contract.md — the requirement
//     exists in the normative spec.
//
//  2. Per-run write lock for state transitions is declared — state transitions
//     MUST acquire a per-run (not global) write lock.
//
//  3. Event publication must not block state lock is declared — the publication
//     path MUST be lock-free with respect to the state lock.
//
//  4. Per-subscriber channel for event consumers is declared — consumers MUST
//     read from a per-subscriber channel.
//
//  5. No mutex held across adapter call is declared — the S04 adapter call
//     boundary must be mutex-free.
//
//  6. Tags: mechanism is present in the HC-015 body window.
//
// # Failure modes
//
//   - HC-015 heading missing: HC-015 heading not found in specs/handler-contract.md.
//   - Per-run write lock absent: per-run write lock for state transitions not stated.
//   - Event publication non-blocking absent: event publication must not block state lock not stated.
//   - Per-subscriber channel absent: per-subscriber channel for event consumers not stated.
//   - No mutex across adapter absent: no mutex held across adapter call not stated.
//   - Tags: mechanism missing from HC-015 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc015Fixture prefix per
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

// hc015FixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/hc015_mutex_discipline_test.go
//
// so the repo root is two directories up.
func hc015FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc015FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc015FixtureHC015Heading matches the HC-015 level-4 requirement heading line.
var hc015FixtureHC015Heading = regexp.MustCompile(`^#### HC-015 —`)

// hc015FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-015 requirement body window.
var hc015FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc015FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc015FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc015FixtureBodyWindow is the maximum number of lines after the HC-015
// heading to scan for requirement-body content.
const hc015FixtureBodyWindow = 30

// hc015FixtureLoadLines opens specFile and returns all lines.
func hc015FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc015FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc015FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc015FixtureHC015BodyLines returns the lines comprising the HC-015 requirement
// body: all lines after the HC-015 heading line up to (but not including) the
// next Markdown heading or hc015FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the HC-015 heading is not found.
func hc015FixtureHC015BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc015FixtureHC015Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-015 heading not found; expected '#### HC-015 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc015FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hc015FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc015FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hc015FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC015MutexDiscipline is the binding test for hk-8i31.18.
//
// It opens specs/handler-contract.md, locates the HC-015 heading, and validates:
//
//	(a) The HC-015 heading is present (the requirement exists in the spec).
//	(b) Per-run write lock for state transitions declared.
//	(c) Event publication must not block state lock declared.
//	(d) Per-subscriber channel for event consumers declared.
//	(e) No mutex held across adapter call declared.
//	(f) Tags: mechanism is present in the body window.
func TestHC015MutexDiscipline(t *testing.T) {
	t.Parallel()

	specFile := hc015FixtureHandlerContractPath(t)
	lines := hc015FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc015FixtureHC015BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-015 check(a): %s", reason)
	}
	t.Logf("HC-015 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "per-run write lock for state transitions",
			needle: "per-run write lock",
			detail: "HC-015 body must declare that state transitions MUST acquire a per-run write lock " +
				"(expected phrase 'per-run write lock'); the per-run scope is normative — a global " +
				"lock would serialize all sessions rather than isolating them",
		},
		{
			id:     "c",
			label:  "event publication must not block state lock",
			needle: "MUST NOT block the state lock",
			detail: "HC-015 body must declare that event publication MUST NOT block the state lock " +
				"(expected phrase 'MUST NOT block the state lock'); this prevents a deadlock where " +
				"a slow subscriber stalls state transitions across all sessions",
		},
		{
			id:     "d",
			label:  "per-subscriber channel for event consumers",
			needle: "per-subscriber channel",
			detail: "HC-015 body must declare that event consumers MUST read from a per-subscriber " +
				"channel (expected phrase 'per-subscriber channel'); this isolates slow consumers " +
				"from each other and from the state lock",
		},
		{
			id:     "e",
			label:  "no mutex held across adapter call",
			needle: "adapter",
			detail: "HC-015 body must declare that no mutex MAY be held across a call into S04's " +
				"adapter (expected phrase 'adapter'); the adapter call crosses a subsystem boundary " +
				"where lock-ordering is undefined",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc015FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-015 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-015 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (f): Tags: mechanism in HC-015 body.
	t.Run("check-f-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc015FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-015 check(f) FAILED: Tags: mechanism not found in HC-015 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-015 body)\n"+
					"  detail: HC-015 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.18 audit complete — HC-015 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
