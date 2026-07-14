//go:build specaudit

package specaudit_test

// hk-hqwn.14 binding test — EV-011 asynchronous consumer class.
//
// Spec ref: specs/event-model.md §4.3 EV-011.
//
// EV-011 states: an asynchronous consumer runs off the critical path. Consumer failure
// MUST NOT block the producer. Failed deliveries are retried per a bounded policy
// (default 3 attempts, exponential backoff 1s base); exhausted retries enqueue to the
// dead-letter queue (§6.2) and emit dead_letter_enqueued (§8.8.3). Per-consumer dispatch
// queue depth MUST be bounded (default 1024; operator-configurable). Async consumers MAY
// be added at subscription time up to a per-type cap of 8.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The event bus implementation is pending; this
// sensor verifies that EV-011 is correctly declared in the spec so that:
//
//  1. EV-011 heading is present in specs/event-model.md.
//  2. "off the critical path" describes async consumer execution.
//  3. "MUST NOT block the producer" is declared for consumer failure.
//  4. "dead-letter queue" is named as the exhausted-retry destination.
//  5. "dead_letter_enqueued" event is named as the failure signal.
//  6. Bounded queue depth with default 1024 is declared.
//  7. Tags: mechanism is present in the EV-011 body window.
//
// # Failure modes
//
//   - EV-011 heading missing.
//   - off critical path absent.
//   - MUST NOT block producer absent.
//   - dead-letter queue absent.
//   - dead_letter_enqueued absent.
//   - bounded queue depth absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ev011Fixture prefix per
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

// ev011FixtureEventModelPath returns the absolute path to specs/event-model.md.
func ev011FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ev011FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// ev011FixtureEV011Heading matches the EV-011 level-4 requirement heading line.
var ev011FixtureEV011Heading = regexp.MustCompile(`^#### EV-011 —`)

// ev011FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var ev011FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ev011FixtureTagsMechanism matches a "Tags: mechanism" line.
var ev011FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// ev011FixtureBodyWindow is the maximum number of lines after the EV-011
// heading to scan for requirement-body content.
const ev011FixtureBodyWindow = 30

// ev011FixtureLoadLines opens specFile and returns all lines.
func ev011FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ev011FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ev011FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// ev011FixtureEV011BodyLines returns the lines comprising the EV-011 body.
func ev011FixtureEV011BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if ev011FixtureEV011Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-011 heading not found; expected '#### EV-011 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + ev011FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if ev011FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// ev011FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func ev011FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestEV011AsynchronousConsumerClass is the binding test for hk-hqwn.14.
func TestEV011AsynchronousConsumerClass(t *testing.T) {
	t.Parallel()

	specFile := ev011FixtureEventModelPath(t)
	lines := ev011FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := ev011FixtureEV011BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-011 check(1): %s", reason)
	}
	t.Logf("EV-011 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "off-critical-path",
			needle: "off the critical path",
			detail: "EV-011 body must declare 'off the critical path' for async consumer execution " +
				"(expected phrase 'off the critical path'); this is the defining property of the " +
				"asynchronous class — the consumer runs in a separate goroutine so the producer's " +
				"critical path is never blocked waiting for consumer completion",
		},
		{
			id:     "3",
			label:  "must-not-block-producer",
			needle: "MUST NOT block the producer",
			detail: "EV-011 body must declare 'MUST NOT block the producer' for consumer failure " +
				"(expected phrase 'MUST NOT block the producer'); this is the normative guarantee " +
				"that consumer failure (panic, error, timeout) MUST NOT propagate back to block the " +
				"producer — failure is isolated to the consumer's own goroutine",
		},
		{
			id:     "4",
			label:  "dead-letter-queue-exhausted-retry-destination",
			needle: "dead-letter queue",
			detail: "EV-011 body must name the 'dead-letter queue' as the exhausted-retry destination " +
				"(expected phrase 'dead-letter queue'); when the bounded retry policy (default 3 " +
				"attempts) is exhausted, the event MUST be enqueued to the dead-letter queue (§6.2) " +
				"for operator visibility and recovery",
		},
		{
			id:     "5",
			label:  "dead-letter-enqueued-event",
			needle: "dead_letter_enqueued",
			detail: "EV-011 body must name 'dead_letter_enqueued' as the failure signal event " +
				"(expected phrase 'dead_letter_enqueued'); emitting this event (§8.8.3) on dead-letter " +
				"enqueue ensures the operator can detect failed deliveries from the event stream " +
				"without polling the dead-letter queue directly",
		},
		{
			id:     "6",
			label:  "bounded-queue-depth-1024-default",
			needle: "1024",
			detail: "EV-011 body must declare the bounded queue depth with default '1024' " +
				"(expected phrase '1024'); the per-consumer dispatch queue MUST be bounded to prevent " +
				"unbounded memory growth; the default of 1024 events is the reference value that " +
				"operators tune per operator-nfr.md",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !ev011FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-011 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-011 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in EV-011 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if ev011FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-011 check(7) FAILED: Tags: mechanism not found in EV-011 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-011 body)\n"+
					"  detail: EV-011 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.14 audit complete — EV-011 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
