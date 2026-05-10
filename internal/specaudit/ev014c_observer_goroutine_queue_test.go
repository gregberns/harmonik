package specaudit_test

// hk-hqwn.21 binding test — EV-014c observer dispatch uses per-observer goroutine + bounded queue.
//
// Spec ref: specs/event-model.md §4.3 EV-014c.
//
// EV-014c states: FANOUT_OBSERVERS dispatches each event to each registered observer via
// a per-observer dedicated goroutine draining a per-observer bounded queue (default depth
// 1024, operator-configurable; same default as async per EV-011). Observer queues are
// class lossy-tail-ok for shed semantics per EV-011a. A slow observer MUST NOT
// back-pressure the producer OR starve peer observers. fsync-boundary events that cannot
// queue follow the EV-011a spill-file path.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The observer dispatch implementation is pending;
// this sensor verifies that EV-014c is correctly declared in the spec so that:
//
//  1. EV-014c heading is present in specs/event-model.md.
//  2. "per-observer dedicated goroutine" is declared as the dispatch mechanism.
//  3. "per-observer bounded queue" is declared (default depth 1024).
//  4. Observer queues are class "lossy-tail-ok" for shed semantics.
//  5. Slow observer "MUST NOT back-pressure the producer" is declared.
//  6. Slow observer "MUST NOT... starve peer observers" is declared.
//  7. Tags: mechanism is present in the EV-014c body window.
//
// # Failure modes
//
//   - EV-014c heading missing.
//   - per-observer dedicated goroutine absent.
//   - per-observer bounded queue absent.
//   - lossy-tail-ok shed semantics absent.
//   - MUST NOT back-pressure producer absent.
//   - MUST NOT starve peers absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ev014cFixture prefix per
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

// ev014cFixtureEventModelPath returns the absolute path to specs/event-model.md.
func ev014cFixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ev014cFixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// ev014cFixtureEV014cHeading matches the EV-014c level-4 requirement heading line.
var ev014cFixtureEV014cHeading = regexp.MustCompile(`^#### EV-014c —`)

// ev014cFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var ev014cFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ev014cFixtureTagsMechanism matches a "Tags: mechanism" line.
var ev014cFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// ev014cFixtureBodyWindow is the maximum number of lines after the EV-014c
// heading to scan for requirement-body content.
const ev014cFixtureBodyWindow = 30

// ev014cFixtureLoadLines opens specFile and returns all lines.
func ev014cFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ev014cFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ev014cFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// ev014cFixtureEV014cBodyLines returns the lines comprising the EV-014c body.
func ev014cFixtureEV014cBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if ev014cFixtureEV014cHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-014c heading not found; expected '#### EV-014c —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + ev014cFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if ev014cFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// ev014cFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func ev014cFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestEV014cObserverDispatchPerGoroutineBoundedQueue is the binding test for hk-hqwn.21.
func TestEV014cObserverDispatchPerGoroutineBoundedQueue(t *testing.T) {
	t.Parallel()

	specFile := ev014cFixtureEventModelPath(t)
	lines := ev014cFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := ev014cFixtureEV014cBodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-014c check(1): %s", reason)
	}
	t.Logf("EV-014c heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "per-observer-dedicated-goroutine",
			needle: "per-observer dedicated goroutine",
			detail: "EV-014c body must declare 'per-observer dedicated goroutine' as the dispatch mechanism " +
				"(expected phrase 'per-observer dedicated goroutine'); each observer gets its own goroutine " +
				"draining its own queue — this is the isolation mechanism that prevents one slow observer " +
				"from blocking others or back-pressuring the producer",
		},
		{
			id:     "3",
			label:  "per-observer-bounded-queue",
			needle: "per-observer bounded queue",
			detail: "EV-014c body must declare 'per-observer bounded queue' " +
				"(expected phrase 'per-observer bounded queue'); the per-observer queue has a bounded " +
				"depth (default 1024, same as async per EV-011) to prevent unbounded memory growth " +
				"from slow observers accumulating events",
		},
		{
			id:     "4",
			label:  "lossy-tail-ok-shed-semantics",
			needle: "lossy-tail-ok",
			detail: "EV-014c body must declare observer queues are class 'lossy-tail-ok' for shed semantics " +
				"(expected phrase 'lossy-tail-ok'); when a per-observer queue is full, events are dropped " +
				"(shed) before the producer is blocked — the lossy-tail-ok class is the normative " +
				"justification for this drop-without-block behavior",
		},
		{
			id:     "5",
			label:  "must-not-backpressure-producer",
			needle: "MUST NOT back-pressure the producer",
			detail: "EV-014c body must declare 'MUST NOT back-pressure the producer' for slow observers " +
				"(expected phrase 'MUST NOT back-pressure the producer'); this is the non-negotiable " +
				"isolation guarantee: no observer, regardless of how slow it is, may cause the producer " +
				"to block waiting for observer delivery",
		},
		{
			id:     "6",
			label:  "must-not-starve-peer-observers",
			needle: "starve peer observers",
			detail: "EV-014c body must declare 'starve peer observers' is forbidden for slow observers " +
				"(expected phrase 'starve peer observers'); a slow observer with a full queue MUST NOT " +
				"prevent other observers from receiving events — each observer's goroutine+queue is " +
				"independent so peer starvation cannot occur by design",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !ev014cFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-014c check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-014c body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in EV-014c body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if ev014cFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-014c check(7) FAILED: Tags: mechanism not found in EV-014c body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-014c body)\n"+
					"  detail: EV-014c carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.21 audit complete — EV-014c heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
