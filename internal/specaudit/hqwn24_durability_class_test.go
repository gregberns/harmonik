//go:build specaudit

package specaudit_test

// hk-hqwn.24 binding test — EV-016 + EV-016a durability class and fsync semantics.
//
// Spec ref: specs/event-model.md §4.4 EV-016, EV-016a.
//
// EV-016 states: every row in §8 MUST carry a `durability_class` attribute in
// `{fsync-boundary, ordinary, lossy-tail-ok}`. The JSONL writer MUST call
// `fsync(2)` after appending any event whose class is `fsync-boundary`. `Append`
// MUST return only after fsync completes for boundary-class events; `ordinary` /
// `lossy-tail-ok` returns without fsync.
//
// EV-016a states: `Emit` performs per-event fsync for `fsync-boundary`-class events
// (EV-016). The bus does NOT guarantee atomicity across multiple boundary events.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The JSONL writer implementation is pending;
// this sensor verifies that the EV-016 + EV-016a requirements are correctly
// declared in the spec so that:
//
//  1. EV-016 heading is present in specs/event-model.md — the requirement exists
//     in the normative spec.
//
//  2. The closed vocabulary `{fsync-boundary, ordinary, lossy-tail-ok}` is
//     declared in the EV-016 body — all three durability class names are present.
//
//  3. The `durability_class` attribute name is declared — the attribute name that
//     §8 rows must carry is specified.
//
//  4. The fsync-boundary write semantics are declared — `Append` MUST return only
//     after fsync completes for boundary-class events.
//
//  5. The ordinary / lossy-tail-ok non-blocking semantics are declared — events of
//     those classes return without fsync.
//
//  6. Tags: mechanism is present in the EV-016 body window.
//
//  7. EV-016a heading is present in specs/event-model.md — the per-event fsync
//     clarification exists in the normative spec.
//
//  8. Per-event fsync semantics are declared in EV-016a — `Emit` performs
//     per-event fsync for `fsync-boundary`-class events.
//
//  9. No multi-event atomicity guarantee declared in EV-016a — the bus does NOT
//     guarantee atomicity across multiple boundary events.
//
// 10. The O(N-fsync) cost acceptance is declared in EV-016a — MVH accepts the
//     O(N-fsync) cost.
//
// # Failure modes
//
//   - EV-016 heading missing: EV-016 heading not found in specs/event-model.md.
//   - Closed vocabulary absent: `{fsync-boundary, ordinary, lossy-tail-ok}` not
//     all present in EV-016 body.
//   - durability_class attribute name absent: `durability_class` not in EV-016 body.
//   - fsync-boundary write semantics absent: `Append` return-after-fsync not stated.
//   - ordinary/lossy non-blocking absent: return-without-fsync not stated.
//   - Tags: mechanism missing from EV-016 body window.
//   - EV-016a heading missing: EV-016a heading not found.
//   - Per-event fsync absent: `Emit` per-event fsync not stated in EV-016a.
//   - No-atomicity guarantee absent: bus non-atomicity not stated in EV-016a.
//   - O(N-fsync) cost absent: MVH cost acceptance not stated in EV-016a.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn24Fixture prefix per
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

// hqwn24FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn24_durability_class_test.go
//
// so the repo root is two directories up.
func hqwn24FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn24FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn24FixtureEV016Heading matches the EV-016 level-4 requirement heading line.
var hqwn24FixtureEV016Heading = regexp.MustCompile(`^#### EV-016 —`)

// hqwn24FixtureEV016aHeading matches the EV-016a level-4 requirement heading line.
var hqwn24FixtureEV016aHeading = regexp.MustCompile(`^#### EV-016a —`)

// hqwn24FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of a requirement body window.
var hqwn24FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn24FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn24FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn24FixtureBodyWindow is the maximum number of lines after a heading to
// scan for requirement-body content.
const hqwn24FixtureBodyWindow = 30

// hqwn24FixtureLoadLines opens specFile and returns all lines.
func hqwn24FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn24FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn24FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn24FixtureBodyLines returns the requirement body lines: all lines after
// the matched heading up to (but not including) the next Markdown heading or
// hqwn24FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func hqwn24FixtureBodyLines(lines []string, headingPattern *regexp.Regexp, reqID string) (body []string, headingLineNo int, reason string) {
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

	limit := headingIdx + 1 + hqwn24FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn24FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn24FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn24FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN24DurabilityClassAndFsyncSemantics is the binding test for hk-hqwn.24.
//
// It opens specs/event-model.md and validates the EV-016 and EV-016a requirements
// against the nine audit checks listed in the file-level comment.
func TestHQWN24DurabilityClassAndFsyncSemantics(t *testing.T) {
	t.Parallel()

	specFile := hqwn24FixtureEventModelPath(t)
	lines := hqwn24FixtureLoadLines(t, specFile)

	// --- EV-016 checks ---

	ev016Body, ev016LineNo, ev016Reason := hqwn24FixtureBodyLines(lines, hqwn24FixtureEV016Heading, "EV-016")
	if ev016Reason != "" {
		t.Fatalf("EV-016 check(1): %s", ev016Reason)
	}
	t.Logf("EV-016 heading found at specs/event-model.md line %d; body window = %d lines",
		ev016LineNo, len(ev016Body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	ev016Checks := []check{
		{
			id:     "2",
			label:  "closed vocabulary fsync-boundary",
			needle: "fsync-boundary",
			detail: "EV-016 body must declare the durability class 'fsync-boundary' as part of " +
				"the closed vocabulary {fsync-boundary, ordinary, lossy-tail-ok}; its absence " +
				"means the fsync-requiring class is not normatively named in the spec",
		},
		{
			id:     "2b",
			label:  "closed vocabulary ordinary",
			needle: "ordinary",
			detail: "EV-016 body must declare the durability class 'ordinary' as part of " +
				"the closed vocabulary; its absence means the default non-fsync class is not named",
		},
		{
			id:     "2c",
			label:  "closed vocabulary lossy-tail-ok",
			needle: "lossy-tail-ok",
			detail: "EV-016 body must declare the durability class 'lossy-tail-ok' as part of " +
				"the closed vocabulary; its absence means the high-cardinality opportunistic class is not named",
		},
		{
			id:     "3",
			label:  "durability_class attribute name",
			needle: "durability_class",
			detail: "EV-016 body must name the `durability_class` attribute that every §8 row " +
				"must carry; this is the field name implementors and spec readers depend on",
		},
		{
			id:     "4",
			label:  "fsync-boundary Append-return-after-fsync semantics",
			needle: "fsync completes",
			detail: "EV-016 body must declare that `Append` MUST return only after fsync completes " +
				"for fsync-boundary events; this is the load-bearing synchronous durability contract",
		},
		{
			id:     "5",
			label:  "ordinary/lossy-tail-ok returns without fsync",
			needle: "returns without fsync",
			detail: "EV-016 body must declare that ordinary / lossy-tail-ok events return without fsync; " +
				"this establishes the non-blocking producer contract for non-boundary events",
		},
	}

	for _, c := range ev016Checks {
		c := c
		t.Run(fmt.Sprintf("EV016-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn24FixtureBodyContains(ev016Body, c.needle) {
				t.Errorf(
					"EV-016 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-016 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, ev016LineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in EV-016 body.
	t.Run("EV016-check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range ev016Body {
			if hqwn24FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-016 check(6) FAILED: Tags: mechanism not found in EV-016 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-016 body)\n"+
					"  detail: EV-016 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				ev016LineNo,
			)
		}
	})

	// --- EV-016a checks ---

	ev016aBody, ev016aLineNo, ev016aReason := hqwn24FixtureBodyLines(lines, hqwn24FixtureEV016aHeading, "EV-016a")
	if ev016aReason != "" {
		t.Fatalf("EV-016a check(7): %s", ev016aReason)
	}
	t.Logf("EV-016a heading found at specs/event-model.md line %d; body window = %d lines",
		ev016aLineNo, len(ev016aBody))

	ev016aChecks := []check{
		{
			id:     "8",
			label:  "per-event fsync for fsync-boundary events",
			needle: "per-event fsync",
			detail: "EV-016a body must declare that `Emit` performs per-event fsync for " +
				"fsync-boundary-class events; this is the clarification that each boundary " +
				"event gets its own fsync, not a shared batch fsync",
		},
		{
			id:     "9",
			label:  "no multi-event atomicity guarantee",
			needle: "NOT guarantee atomicity",
			detail: "EV-016a body must state that the bus does NOT guarantee atomicity across " +
				"multiple boundary events; producers must not assume crash-atomic pairs",
		},
		{
			id:     "10",
			label:  "O(N-fsync) cost acceptance",
			needle: "O(N-fsync)",
			detail: "EV-016a body must declare that MVH accepts the O(N-fsync) cost; this " +
				"documents the deliberate performance trade-off in exchange for simple per-event " +
				"durability semantics",
		},
	}

	for _, c := range ev016aChecks {
		c := c
		t.Run(fmt.Sprintf("EV016a-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn24FixtureBodyContains(ev016aBody, c.needle) {
				t.Errorf(
					"EV-016a check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-016a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, ev016aLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Logf("hk-hqwn.24 audit complete — EV-016 heading at line %d (body %d lines), EV-016a heading at line %d (body %d lines)",
		ev016LineNo, len(ev016Body), ev016aLineNo, len(ev016aBody))
}
