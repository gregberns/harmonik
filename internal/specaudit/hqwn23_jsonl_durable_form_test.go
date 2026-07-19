//go:build specaudit

package specaudit_test

// hk-hqwn.23 binding test — EV-015 JSONL is the durable on-disk form.
//
// Spec ref: specs/event-model.md §4.4 EV-015; §6.2 On-disk JSONL format.
//
// EV-015 states: the bus MUST persist every emitted event (before any shed)
// to a JSONL file on the local filesystem at `.harmonik/events/events.jsonl`.
// Line format per §6.2.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The JSONL writer implementation is
// pending; this sensor verifies that the EV-015 requirement and the §6.2
// on-disk format section are correctly declared in the spec so that:
//
//  1. EV-015 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. "before any shed" is declared — persistence happens before shed, not
//     after; this ordering is the core durability contract.
//
//  3. The primary log path `.harmonik/events/events.jsonl` is declared in
//     EV-015 body — the canonical storage path is normatively named.
//
//  4. The §6.2 reference is present in EV-015 body — the line-format
//     normative reference is cited.
//
//  5. Tags: mechanism is present in the EV-015 body window.
//
//  6. §6.2 section heading is present in specs/event-model.md — the on-disk
//     format section exists as a normative reference target.
//
//  7. §6.2 primary log path `.harmonik/events/events.jsonl` is declared —
//     the section names the canonical path.
//
//  8. §6.2 names the `event_id` envelope field — the required UUIDv7 field.
//
//  9. §6.2 names the `schema_version` envelope field — the versioning field.
//
// 10. §6.2 names the `source_subsystem` envelope field — the emitter field.
//
// # Failure modes
//
//   - EV-015 heading missing: EV-015 heading not found in specs/event-model.md.
//   - "before any shed" absent: ordering constraint not declared in EV-015 body.
//   - Primary log path absent: `.harmonik/events/events.jsonl` not in EV-015 body.
//   - §6.2 reference absent: line-format reference not cited in EV-015 body.
//   - Tags: mechanism missing from EV-015 body window.
//   - §6.2 section heading missing: on-disk format section not present.
//   - §6.2 primary log path absent: path not declared in §6.2 section.
//   - §6.2 event_id field absent: UUIDv7 field not named in §6.2.
//   - §6.2 schema_version field absent: versioning field not named in §6.2.
//   - §6.2 source_subsystem field absent: emitter field not named in §6.2.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn23Fixture prefix per
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

// hqwn23FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn23_jsonl_durable_form_test.go
//
// so the repo root is two directories up.
func hqwn23FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn23FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn23FixtureEV015Heading matches the EV-015 level-4 requirement heading line.
var hqwn23FixtureEV015Heading = regexp.MustCompile(`^#### EV-015 —`)

// hqwn23FixtureSection62Heading matches the §6.2 section heading line.
var hqwn23FixtureSection62Heading = regexp.MustCompile(`^### 6\.2 `)

// hqwn23FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of a requirement body window.
var hqwn23FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn23FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn23FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn23FixtureBodyWindow is the maximum number of lines after a heading to
// scan for requirement-body content.
const hqwn23FixtureBodyWindow = 30

// hqwn23FixtureSection62Window is the maximum number of lines after the §6.2
// heading to scan for format declarations; wider than a single requirement.
const hqwn23FixtureSection62Window = 60

// hqwn23FixtureLoadLines opens specFile and returns all lines.
func hqwn23FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn23FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn23FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn23FixtureBodyLines returns requirement body lines: all lines after the
// matched heading up to (but not including) the next Markdown heading or
// windowSize lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func hqwn23FixtureBodyLines(lines []string, headingPattern *regexp.Regexp, reqID string, windowSize int) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if headingPattern.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, fmt.Sprintf("%s heading not found; expected '%s' pattern in specs/event-model.md", reqID, headingPattern.String())
	}

	limit := headingIdx + 1 + windowSize
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (a heading at the same level or higher).
		if hqwn23FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn23FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn23FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// hqwn23FixtureSection62BodyLines returns the §6.2 section body: all lines
// after the §6.2 heading up to (but not including) the next level-3 or higher
// heading, capped at hqwn23FixtureSection62Window.
//
// Unlike hqwn23FixtureBodyLines, this does NOT stop at level-4 headings because
// §6.2 is itself a level-3 section containing sub-content without sub-headings.
func hqwn23FixtureSection62BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	level3OrHigher := regexp.MustCompile(`^#{1,3} `)

	headingIdx := -1
	for i, line := range lines {
		if hqwn23FixtureSection62Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "§6.2 heading not found; expected '### 6.2 ' pattern in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn23FixtureSection62Window
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next level-3 or higher heading (a sibling or parent section).
		if level3OrHigher.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// TestHQWN23JSONLIsDurableOnDiskForm is the binding test for hk-hqwn.23.
//
// It opens specs/event-model.md, locates the EV-015 heading and the §6.2
// section, and validates the ten audit checks listed in the file-level comment.
func TestHQWN23JSONLIsDurableOnDiskForm(t *testing.T) {
	t.Parallel()

	specFile := hqwn23FixtureEventModelPath(t)
	lines := hqwn23FixtureLoadLines(t, specFile)

	// --- EV-015 checks ---

	ev015Body, ev015LineNo, ev015Reason := hqwn23FixtureBodyLines(lines, hqwn23FixtureEV015Heading, "EV-015", hqwn23FixtureBodyWindow)
	if ev015Reason != "" {
		t.Fatalf("EV-015 check(1): %s", ev015Reason)
	}
	t.Logf("EV-015 heading found at specs/event-model.md line %d; body window = %d lines",
		ev015LineNo, len(ev015Body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	ev015Checks := []check{
		{
			id:     "2",
			label:  "persist-before-shed ordering",
			needle: "before any shed",
			detail: "EV-015 body must declare persistence happens 'before any shed' " +
				"(expected phrase 'before any shed'); this ordering constraint is the " +
				"core durability invariant: an event is durable before any consumer-shed decision",
		},
		{
			id:     "3",
			label:  "primary log path .harmonik/events/events.jsonl",
			needle: ".harmonik/events/events.jsonl",
			detail: "EV-015 body must name the canonical log path '.harmonik/events/events.jsonl' " +
				"(expected phrase '.harmonik/events/events.jsonl'); this is the normative " +
				"storage path that all writers and readers must use",
		},
		{
			id:     "4",
			label:  "section 6.2 line-format reference",
			needle: "§6.2",
			detail: "EV-015 body must cite §6.2 for the line format " +
				"(expected phrase '§6.2'); this normative reference anchors the " +
				"on-disk format to the format section",
		},
	}

	for _, c := range ev015Checks {
		c := c
		t.Run(fmt.Sprintf("EV015-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn23FixtureBodyContains(ev015Body, c.needle) {
				t.Errorf(
					"EV-015 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-015 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, ev015LineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (5): Tags: mechanism in EV-015 body.
	t.Run("EV015-check-5-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range ev015Body {
			if hqwn23FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-015 check(5) FAILED: Tags: mechanism not found in EV-015 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-015 body)\n"+
					"  detail: EV-015 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				ev015LineNo,
			)
		}
	})

	// --- §6.2 checks ---

	sec62Body, sec62LineNo, sec62Reason := hqwn23FixtureSection62BodyLines(lines)
	if sec62Reason != "" {
		t.Fatalf("§6.2 check(6): %s", sec62Reason)
	}
	t.Logf("§6.2 heading found at specs/event-model.md line %d; body window = %d lines",
		sec62LineNo, len(sec62Body))

	sec62Checks := []check{
		{
			id:     "7",
			label:  "section-6.2 primary log path declaration",
			needle: ".harmonik/events/events.jsonl",
			detail: "§6.2 must declare the primary log path '.harmonik/events/events.jsonl' " +
				"(expected phrase '.harmonik/events/events.jsonl'); this is the normative path " +
				"that appears in the on-disk format section as the canonical reference location",
		},
		{
			id:     "8",
			label:  "section-6.2 event_id envelope field",
			needle: "event_id",
			detail: "§6.2 must name the 'event_id' envelope field (expected phrase 'event_id'); " +
				"this is the UUIDv7 primary key for every JSONL line and is required for " +
				"reader-recovery deduplication and replay ordering",
		},
		{
			id:     "9",
			label:  "section-6.2 schema_version envelope field",
			needle: "schema_version",
			detail: "§6.2 must name the 'schema_version' envelope field (expected phrase 'schema_version'); " +
				"this is the versioning field readers use to detect format changes",
		},
		{
			id:     "10",
			label:  "section-6.2 source_subsystem envelope field",
			needle: "source_subsystem",
			detail: "§6.2 must name the 'source_subsystem' envelope field (expected phrase 'source_subsystem'); " +
				"this is the emitter attribution field required for subsystem-origin tracing",
		},
	}

	for _, c := range sec62Checks {
		c := c
		t.Run(fmt.Sprintf("sec62-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn23FixtureBodyContains(sec62Body, c.needle) {
				t.Errorf(
					"§6.2 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (§6.2 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, sec62LineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Logf("hk-hqwn.23 audit complete — EV-015 heading at line %d (body %d lines), §6.2 heading at line %d (body %d lines)",
		ev015LineNo, len(ev015Body), sec62LineNo, len(sec62Body))
}
