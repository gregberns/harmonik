package specaudit_test

// hk-hqwn.22 binding test — EV-014d consumer-recovery replay contract (closes EV-INV-002 consumer side).
//
// Spec ref: specs/event-model.md §4.3 EV-014d.
//
// EV-014d states: on daemon startup, for every subscription whose `since` field
// is non-nil OR whose `offset_checkpoint_event_id` is non-nil, the bus MUST
// perform a JSONL-tail replay to the consumer before live-stream delivery
// resumes. Key obligations declared:
//
//   - Replay scans events.jsonl for lines whose event_id is strictly greater than
//     the consumer's effective checkpoint; dispatches in event_id order.
//   - Dead-letter log and spill files are NOT automatically replayed.
//   - Replay and live-stream are serialised per consumer.
//   - on_tail_truncation fires (if registered) after replay when tail lost data.
//   - Consumers with since=None and no offset_checkpoint_event_id start from live stream.
//   - Synchronous consumers do NOT participate in replay.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The bus implementation is pending; this
// sensor verifies that EV-014d is correctly declared in the spec so that:
//
//  1. EV-014d heading is present in specs/event-model.md.
//  2. JSONL-tail replay obligation is declared.
//  3. event_id strict-greater-than ordering is declared.
//  4. Dead-letter exclusion from auto-replay is declared.
//  5. Per-consumer serialisation of replay and live-stream is declared.
//  6. on_tail_truncation callback is declared.
//  7. Synchronous consumers do NOT participate in replay.
//  8. Tags: mechanism is present in the EV-014d body window.
//
// # Failure modes
//
//   - EV-014d heading missing.
//   - JSONL-tail replay obligation absent.
//   - event_id ordering absent.
//   - Dead-letter exclusion absent.
//   - Per-consumer serialisation absent.
//   - on_tail_truncation absent.
//   - Synchronous-consumer replay exclusion absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn22Fixture prefix per
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

// hqwn22FixtureEventModelPath returns the absolute path to specs/event-model.md.
func hqwn22FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn22FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn22FixtureEV014dHeading matches the EV-014d level-4 requirement heading line.
var hqwn22FixtureEV014dHeading = regexp.MustCompile(`^#### EV-014d —`)

// hqwn22FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hqwn22FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn22FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn22FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn22FixtureBodyWindow is the maximum number of lines after the EV-014d
// heading to scan for requirement-body content.
const hqwn22FixtureBodyWindow = 30

// hqwn22FixtureLoadLines opens specFile and returns all lines.
func hqwn22FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn22FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn22FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn22FixtureEV014dBodyLines returns the lines comprising the EV-014d body.
func hqwn22FixtureEV014dBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn22FixtureEV014dHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-014d heading not found; expected '#### EV-014d —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn22FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hqwn22FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn22FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hqwn22FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN22ConsumerRecoveryReplayContract is the binding test for hk-hqwn.22.
func TestHQWN22ConsumerRecoveryReplayContract(t *testing.T) {
	t.Parallel()

	specFile := hqwn22FixtureEventModelPath(t)
	lines := hqwn22FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn22FixtureEV014dBodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-014d check(1): %s", reason)
	}
	t.Logf("EV-014d heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "jsonl-tail-replay-obligation",
			needle: "JSONL-tail replay",
			detail: "EV-014d body must declare the JSONL-tail replay obligation " +
				"(expected phrase 'JSONL-tail replay'); this is the primary mechanism the " +
				"bus uses to close EV-INV-002's consumer side at daemon startup",
		},
		{
			id:     "3",
			label:  "event-id-strict-greater-than-ordering",
			needle: "strictly greater than",
			detail: "EV-014d body must declare that replay covers event_ids strictly greater " +
				"than the checkpoint (expected phrase 'strictly greater than'); this is the " +
				"boundary condition that makes replay idempotent-safe at the boundary event",
		},
		{
			id:     "4",
			label:  "dead-letter-excluded-from-auto-replay",
			needle: "NOT automatically replayed",
			detail: "EV-014d body must declare that dead-letter and spill files are NOT " +
				"automatically replayed (expected phrase 'NOT automatically replayed'); " +
				"operator-initiated replay requires the separate DeadLetterReplay call per EV-011",
		},
		{
			id:     "5",
			label:  "per-consumer-serialisation",
			needle: "serialized per consumer",
			detail: "EV-014d body must declare that replay and live-stream are serialised per " +
				"consumer (expected phrase 'serialized per consumer'); this prevents a consumer " +
				"from receiving live events before its replay is complete",
		},
		{
			id:     "6",
			label:  "on-tail-truncation-callback",
			needle: "on_tail_truncation",
			detail: "EV-014d body must declare the on_tail_truncation callback " +
				"(expected phrase 'on_tail_truncation'); this is the signal consumers register " +
				"to detect JSONL tail truncation after restart replay completes",
		},
		{
			id:     "7",
			label:  "synchronous-consumers-do-not-replay",
			needle: "Synchronous consumers do NOT participate",
			detail: "EV-014d body must declare that synchronous consumers do NOT participate in " +
				"replay (expected phrase 'Synchronous consumers do NOT participate'); re-invoking " +
				"a synchronous handler on restart would risk double side effects",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn22FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-014d check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-014d body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in EV-014d body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn22FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-014d check(8) FAILED: Tags: mechanism not found in EV-014d body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-014d body)\n"+
					"  detail: EV-014d carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.22 audit complete — EV-014d heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
