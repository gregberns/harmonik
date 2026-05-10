package specaudit_test

// hk-8mup.30 binding test — PL-018a panic recovery barrier in the daemon main goroutine.
//
// Spec ref: specs/process-lifecycle.md §4.3 PL-018a.
//
// PL-018a states: the daemon MUST install a top-level recover() barrier in its main
// goroutine. An unrecovered panic MUST terminate the daemon with ON §8 code 19
// (runtime-panic) and emit daemon_startup_failed (if event bus initialized) or
// daemon_shutdown{mode=immediate} (if ready has been reached) on best-effort basis.
// Panics in HC-watcher goroutines are handled by HC-011a. A double-panic MAY bypass
// exit-code-19; recovery proceeds via PL-024 stale-pidfile detection + PL-025a pairing-
// tolerance.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that PL-018a is correctly declared in the spec so that:
//
//  1. PL-018a heading is present in specs/process-lifecycle.md.
//  2. "top-level recover() barrier" is declared in the main goroutine.
//  3. "code 19" (runtime-panic) is named as the exit code for unrecovered panics.
//  4. "daemon_startup_failed" is named for the pre-bus-init path.
//  5. "double panic" scenario is acknowledged.
//  6. Tags: mechanism is present in the PL-018a body window.
//
// # Failure modes
//
//   - PL-018a heading missing.
//   - top-level recover() barrier absent.
//   - code 19 absent.
//   - daemon_startup_failed absent.
//   - double panic absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the pl018aFixture prefix per
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

// pl018aFixtureProcessLifecyclePath returns the absolute path to specs/process-lifecycle.md.
func pl018aFixtureProcessLifecyclePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("pl018aFixtureProcessLifecyclePath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "process-lifecycle.md")
}

// pl018aFixturePL018aHeading matches the PL-018a level-4 requirement heading line.
var pl018aFixturePL018aHeading = regexp.MustCompile(`^#### PL-018a —`)

// pl018aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var pl018aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// pl018aFixtureTagsMechanism matches a "Tags: mechanism" line.
var pl018aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// pl018aFixtureBodyWindow is the maximum number of lines after the PL-018a
// heading to scan for requirement-body content.
const pl018aFixtureBodyWindow = 30

// pl018aFixtureLoadLines opens specFile and returns all lines.
func pl018aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("pl018aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("pl018aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// pl018aFixturePL018aBodyLines returns the lines comprising the PL-018a body.
func pl018aFixturePL018aBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if pl018aFixturePL018aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "PL-018a heading not found; expected '#### PL-018a —' in specs/process-lifecycle.md"
	}

	limit := headingIdx + 1 + pl018aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if pl018aFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// pl018aFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func pl018aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestPL018aPanicRecoveryBarrier is the binding test for hk-8mup.30.
func TestPL018aPanicRecoveryBarrier(t *testing.T) {
	t.Parallel()

	specFile := pl018aFixtureProcessLifecyclePath(t)
	lines := pl018aFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := pl018aFixturePL018aBodyLines(lines)
	if reason != "" {
		t.Fatalf("PL-018a check(1): %s", reason)
	}
	t.Logf("PL-018a heading found at specs/process-lifecycle.md line %d; body window = %d lines",
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
			label:  "top-level-recover-barrier-main-goroutine",
			needle: "top-level",
			detail: "PL-018a body must declare 'top-level' recover() barrier in its main goroutine " +
				"(expected phrase 'top-level'); the barrier must be at the outermost level of the " +
				"main goroutine — not in a nested goroutine or subroutine — to catch any unrecovered " +
				"panic that propagates up the call stack",
		},
		{
			id:     "3",
			label:  "code-19-runtime-panic-exit",
			needle: "code 19",
			detail: "PL-018a body must declare 'code 19' as the exit code for unrecovered panics " +
				"(expected phrase 'code 19'); ON §8 code 19 is the canonical runtime-panic exit code — " +
				"operators observing this code know to look for a panic stack trace in the daemon's " +
				"stderr or log output",
		},
		{
			id:     "4",
			label:  "daemon-startup-failed-pre-bus-init",
			needle: "daemon_startup_failed",
			detail: "PL-018a body must name 'daemon_startup_failed' for the pre-bus-init panic path " +
				"(expected phrase 'daemon_startup_failed'); if the event bus is initialized at panic " +
				"time, the barrier emits daemon_startup_failed before exiting — if the bus is NOT " +
				"initialized yet, no event can be emitted (best-effort basis)",
		},
		{
			id:     "5",
			label:  "double-panic-acknowledged",
			needle: "double panic",
			detail: "PL-018a body must acknowledge the 'double panic' scenario " +
				"(expected phrase 'double panic'); a panic inside the top-level recover() handler " +
				"cannot be caught by the same barrier — it will terminate with a non-19 exit code; " +
				"recovery falls back to PL-024 stale-pidfile detection and PL-025a event-pairing tolerance",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !pl018aFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"PL-018a check(%s) FAILED: %s\n"+
						"  spec:    specs/process-lifecycle.md line ~%d (PL-018a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in PL-018a body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if pl018aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"PL-018a check(6) FAILED: Tags: mechanism not found in PL-018a body window\n"+
					"  spec:   specs/process-lifecycle.md line ~%d (PL-018a body)\n"+
					"  detail: PL-018a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mup.30 audit complete — PL-018a heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
