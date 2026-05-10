package specaudit_test

// hk-hqwn.20 binding test — EV-014b consumer idempotency on recovery and dead-letter replay.
//
// Spec ref: specs/event-model.md §4.3 EV-014b.
//
// EV-014b states: every consumer MUST be coded against a tail-truncated event
// stream on recovery AND against repeated delivery via DeadLetterReplay and
// ReplayFrom. This obligation pairs with the producer idempotency of EV-018 to
// make lossy-tail-plus-replay safe end-to-end.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The consumer idempotency enforcement is a
// coding-discipline and review concern (no runtime enforcement mechanism); this
// sensor verifies that the EV-014b requirement is correctly declared in the
// spec so that:
//
//  1. EV-014b heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. Tail-truncation recovery obligation is declared — consumers MUST be coded
//     against a tail-truncated event stream on recovery.
//
//  3. DeadLetterReplay idempotency is declared — consumers MUST tolerate
//     repeated delivery via DeadLetterReplay.
//
//  4. ReplayFrom idempotency is declared — consumers MUST tolerate repeated
//     delivery via ReplayFrom.
//
//  5. EV-018 pairing is declared — the obligation pairs with EV-018 producer
//     idempotency to make lossy-tail-plus-replay safe end-to-end.
//
//  6. Tags: mechanism is present in the EV-014b body window.
//
// # Failure modes
//
//   - EV-014b heading missing: EV-014b heading not found in specs/event-model.md.
//   - Tail-truncation obligation absent: tail-truncated recovery not declared.
//   - DeadLetterReplay obligation absent: DeadLetterReplay idempotency not declared.
//   - ReplayFrom obligation absent: ReplayFrom idempotency not declared.
//   - EV-018 pairing absent: EV-018 pairing not declared.
//   - Tags: mechanism missing from EV-014b body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn20Fixture prefix per
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

// hqwn20FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn20_consumer_idempotent_replay_test.go
//
// so the repo root is two directories up.
func hqwn20FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn20FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn20FixtureEV014bHeading matches the EV-014b level-4 requirement heading line.
var hqwn20FixtureEV014bHeading = regexp.MustCompile(`^#### EV-014b —`)

// hqwn20FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-014b requirement body window.
var hqwn20FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn20FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn20FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn20FixtureBodyWindow is the maximum number of lines after the EV-014b
// heading to scan for requirement-body content.
const hqwn20FixtureBodyWindow = 15

// hqwn20FixtureLoadLines opens specFile and returns all lines.
func hqwn20FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn20FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn20FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn20FixtureEV014bBodyLines returns the lines comprising the EV-014b
// requirement body: all lines after the EV-014b heading up to (but not
// including) the next Markdown heading or hqwn20FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-014b heading is not found.
func hqwn20FixtureEV014bBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn20FixtureEV014bHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-014b heading not found; expected '#### EV-014b —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn20FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn20FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn20FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn20FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN20ConsumerIdempotentOnRecoveryAndDeadLetterReplay is the binding
// test for hk-hqwn.20.
//
// It opens specs/event-model.md, locates the EV-014b heading, and validates:
//
//	(a) The EV-014b heading is present (the requirement exists in the spec).
//	(b) Tail-truncation recovery obligation is declared.
//	(c) DeadLetterReplay idempotency obligation is declared.
//	(d) ReplayFrom idempotency obligation is declared.
//	(e) EV-018 pairing with producer idempotency is declared.
//	(f) Tags: mechanism is present in the body window.
func TestHQWN20ConsumerIdempotentOnRecoveryAndDeadLetterReplay(t *testing.T) {
	t.Parallel()

	specFile := hqwn20FixtureEventModelPath(t)
	lines := hqwn20FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn20FixtureEV014bBodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-014b check(a): %s", reason)
	}
	t.Logf("EV-014b heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "tail-truncation-recovery-obligation",
			needle: "tail-truncated",
			detail: "EV-014b body must declare that consumers MUST be coded against a " +
				"tail-truncated event stream on recovery (expected phrase 'tail-truncated'); " +
				"this is the primary recovery-path obligation imposed on all non-synchronous consumers",
		},
		{
			id:     "c",
			label:  "deadletterreplay-idempotency-obligation",
			needle: "DeadLetterReplay",
			detail: "EV-014b body must declare that consumers MUST tolerate repeated delivery " +
				"via DeadLetterReplay (expected phrase 'DeadLetterReplay'); this names the " +
				"operator-initiated replay path that consumers must handle idempotently",
		},
		{
			id:     "d",
			label:  "replayfrom-idempotency-obligation",
			needle: "ReplayFrom",
			detail: "EV-014b body must declare that consumers MUST tolerate repeated delivery " +
				"via ReplayFrom (expected phrase 'ReplayFrom'); this names the consumer-driven " +
				"checkpoint-recovery path that consumers must handle idempotently",
		},
		{
			id:     "e",
			label:  "ev018-producer-idempotency-pairing",
			needle: "EV-018",
			detail: "EV-014b body must declare the pairing with EV-018 producer idempotency " +
				"(expected phrase 'EV-018'); this pairing is what makes lossy-tail-plus-replay " +
				"safe end-to-end per the two-sided operational covenant",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn20FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-014b check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-014b body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (f): Tags: mechanism in EV-014b body.
	t.Run("check-f-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn20FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-014b check(f) FAILED: Tags: mechanism not found in EV-014b body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-014b body)\n"+
					"  detail: EV-014b carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.20 audit complete — EV-014b heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
