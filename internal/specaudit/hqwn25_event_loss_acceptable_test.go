package specaudit_test

// hk-hqwn.25 binding test — EV-017 event loss between fsyncs is acceptable
// because git is authoritative.
//
// Spec ref: specs/event-model.md §4.4 EV-017.
//
// EV-017 states: producers MUST assume that `ordinary` and `lossy-tail-ok`
// events emitted between two fsync boundaries MAY be lost on a hard crash.
// This is acceptable because git plus Beads is authoritative for state per
// [execution-model.md §4.7] and producers emit events idempotently per EV-018.
// Consumers MUST be coded against tail truncation (EV-014b).
//
// # Audit frame
//
// This test is a spec-corpus sensor. The bus and JSONL write implementation is
// pending; this sensor verifies that EV-017 is correctly declared in the spec
// so that:
//
//  1. EV-017 heading is present in specs/event-model.md.
//  2. `ordinary` durability class is declared as potentially lossy.
//  3. `lossy-tail-ok` durability class is declared as potentially lossy.
//  4. Hard-crash loss is declared as acceptable.
//  5. git plus Beads as authoritative state is cited as the justification.
//  6. EV-018 producer idempotency pairing is declared.
//  7. EV-014b consumer tail-truncation obligation is declared.
//  8. Tags: mechanism is present in the EV-017 body window.
//
// # Failure modes
//
//   - EV-017 heading missing.
//   - `ordinary` class absent.
//   - `lossy-tail-ok` class absent.
//   - Hard-crash loss acceptability absent.
//   - git/Beads authority justification absent.
//   - EV-018 pairing absent.
//   - EV-014b tail-truncation reference absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn25Fixture prefix per
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

// hqwn25FixtureEventModelPath returns the absolute path to specs/event-model.md.
func hqwn25FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn25FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn25FixtureEV017Heading matches the EV-017 level-4 requirement heading line.
var hqwn25FixtureEV017Heading = regexp.MustCompile(`^#### EV-017 —`)

// hqwn25FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hqwn25FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn25FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn25FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn25FixtureBodyWindow is the maximum number of lines after the EV-017
// heading to scan for requirement-body content.
const hqwn25FixtureBodyWindow = 15

// hqwn25FixtureLoadLines opens specFile and returns all lines.
func hqwn25FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn25FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn25FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn25FixtureEV017BodyLines returns the lines comprising the EV-017 body.
func hqwn25FixtureEV017BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn25FixtureEV017Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-017 heading not found; expected '#### EV-017 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn25FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hqwn25FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn25FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hqwn25FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN25EventLossBetweenFsyncsAcceptable is the binding test for hk-hqwn.25.
func TestHQWN25EventLossBetweenFsyncsAcceptable(t *testing.T) {
	t.Parallel()

	specFile := hqwn25FixtureEventModelPath(t)
	lines := hqwn25FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn25FixtureEV017BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-017 check(1): %s", reason)
	}
	t.Logf("EV-017 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "ordinary-class-potentially-lossy",
			needle: "ordinary",
			detail: "EV-017 body must declare the `ordinary` durability class as potentially lossy " +
				"between fsyncs (expected phrase 'ordinary'); this is one of the two lossy-window " +
				"classes whose events producers must assume may be lost on hard crash",
		},
		{
			id:     "3",
			label:  "lossy-tail-ok-class-potentially-lossy",
			needle: "lossy-tail-ok",
			detail: "EV-017 body must declare the `lossy-tail-ok` durability class as potentially " +
				"lossy (expected phrase 'lossy-tail-ok'); this is the second lossy-window class " +
				"that producers must code against",
		},
		{
			id:     "4",
			label:  "hard-crash-loss-acceptable",
			needle: "acceptable",
			detail: "EV-017 body must declare that hard-crash event loss is acceptable " +
				"(expected phrase 'acceptable'); this is the normative stance that justifies " +
				"the non-fsync design for ordinary and lossy-tail-ok events",
		},
		{
			id:     "5",
			label:  "git-beads-authority-justification",
			needle: "git plus Beads",
			detail: "EV-017 body must cite git plus Beads as authoritative for state " +
				"(expected phrase 'git plus Beads'); this is the justification that makes " +
				"event-loss acceptable: state is not reconstructed from JSONL",
		},
		{
			id:     "6",
			label:  "ev018-producer-idempotency-pairing",
			needle: "EV-018",
			detail: "EV-017 body must cite EV-018 producer idempotency (expected phrase 'EV-018'); " +
				"producer idempotency is what makes re-emission after recovery safe when events " +
				"were lost in the fsync window",
		},
		{
			id:     "7",
			label:  "ev014b-consumer-tail-truncation-reference",
			needle: "EV-014b",
			detail: "EV-017 body must reference EV-014b consumer tail-truncation obligation " +
				"(expected phrase 'EV-014b'); the consumer side of the two-sided covenant must " +
				"be named to close the full idempotency contract",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn25FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-017 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-017 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in EV-017 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn25FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-017 check(8) FAILED: Tags: mechanism not found in EV-017 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-017 body)\n"+
					"  detail: EV-017 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.25 audit complete — EV-017 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
