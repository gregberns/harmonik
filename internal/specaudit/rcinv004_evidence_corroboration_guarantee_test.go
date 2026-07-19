//go:build specaudit

package specaudit_test

// hk-63oh.45 binding test — RC-INV-004: evidence-corroboration guarantee across
// detector runs.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-004.
//
// RC-INV-004 states: Every `store_divergence_detected` emission traced through
// any detector dispatch (startup, on-demand, scheduled per RC-020a) MUST carry
// a non-`inconclusive` `corroboration` value per EV-023a. A single-source
// observation whose corroboration cannot be established MUST emit
// `divergence_inconclusive` instead. The invariant bounds false-positive
// investigator dispatches and is the reconciliation-side projection of
// [event-model.md §8.6.8] EV-023a.
//
// The sensor has two parts:
//
//  (a) Detector emission layer validates `corroboration ∈
//      {git-corroborated, beads-corroborated}` before the event is written to
//      JSONL; an inconclusive corroboration MUST route to
//      `divergence_inconclusive` instead.
//  (b) Audit-log sample at daemon startup checks that every
//      `store_divergence_detected` in the last N events carries a
//      non-`inconclusive` corroboration.
//
// The RC-INV-004 body carries a cross-spec cite to EV-023a in event-model.md,
// which defines the evidence-inconclusive classification contract that
// RC-INV-004 projects onto the reconciliation detector.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that RC-INV-004 is correctly
// declared in specs/reconciliation/spec.md AND that the upstream requirement
// EV-023a is correctly declared in specs/event-model.md so that:
//
//  1. RC-INV-004 heading is present — the invariant exists in the normative spec.
//
//  2. "store_divergence_detected" is declared — the event this invariant
//     constrains is named in the RC-INV-004 body.
//
//  3. "EV-023a" is cited — the upstream event-model requirement that defines
//     the corroboration contract is referenced.
//
//  4. "corroboration" is declared — the payload field name is named explicitly.
//
//  5. "git-corroborated" is declared — the first valid corroboration enum value
//     is named so implementations know the complete two-value domain.
//
//  6. "beads-corroborated" is declared — the second valid corroboration enum
//     value is named so the closed-enum contract is unambiguous.
//
//  7. "divergence_inconclusive" is declared — the mandatory alternative event
//     for inconclusive single-source observations is named.
//
//  8. "RC-020a" is cited — the detector dispatch-cadence requirement that
//     scopes the invariant across startup, on-demand, and scheduled dispatch
//     points is referenced.
//
//  9. Tags: mechanism is present in the RC-INV-004 body window.
//
// 10. EV-023a heading is present in specs/event-model.md — the upstream
//     requirement referenced by the cross-spec cite exists and is normative.
//
// 11. "inconclusive" is present in the EV-023a body — the upstream requirement
//     declares the three-way classification including the inconclusive case,
//     which is what RC-INV-004 projects onto the reconciliation detector.
//
// # Failure modes
//
//   - RC-INV-004 heading missing: invariant not found in specs/reconciliation/spec.md.
//   - "store_divergence_detected" absent: constrained event not declared.
//   - "EV-023a" absent: upstream corroboration contract not cited.
//   - "corroboration" absent: payload field name not declared.
//   - "git-corroborated" absent: first enum value not named.
//   - "beads-corroborated" absent: second enum value not named.
//   - "divergence_inconclusive" absent: alternative event for inconclusive cases not declared.
//   - "RC-020a" absent: detector dispatch-cadence scoping cite missing.
//   - Tags: mechanism missing from RC-INV-004 body window.
//   - EV-023a heading missing: upstream requirement not found in specs/event-model.md.
//   - "inconclusive" absent from EV-023a body: three-way classification not declared upstream.
//
// # Helper prefix
//
// All package-level identifiers in this file use the rcInv004Fixture prefix per
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

// rcInv004FixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at internal/specaudit/...; the repo root is
// two directories up.
func rcInv004FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rcInv004FixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// rcInv004FixtureSpecPath returns the absolute path to
// specs/reconciliation/spec.md.
func rcInv004FixtureSpecPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rcInv004FixtureRepoRoot(t), "specs", "reconciliation", "spec.md")
}

// rcInv004FixtureEventModelPath returns the absolute path to
// specs/event-model.md.
func rcInv004FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rcInv004FixtureRepoRoot(t), "specs", "event-model.md")
}

// rcInv004FixtureHeading matches the RC-INV-004 level-4 requirement heading.
var rcInv004FixtureHeading = regexp.MustCompile(`^#### RC-INV-004 —`)

// rcInv004FixtureEV023aHeading matches the EV-023a level-4 requirement heading.
var rcInv004FixtureEV023aHeading = regexp.MustCompile(`^#### EV-023a —`)

// rcInv004FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var rcInv004FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rcInv004FixtureTagsMechanism matches a "Tags: mechanism" line.
var rcInv004FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rcInv004FixtureBodyWindow is the maximum number of lines after the heading
// to scan for requirement-body content.
const rcInv004FixtureBodyWindow = 30

// rcInv004FixtureLoadLines opens specFile and returns all lines.
func rcInv004FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("rcInv004FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("rcInv004FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// rcInv004FixtureBodyLines returns the lines comprising the body of the
// requirement at headingRe in specLines, up to the next section heading or
// bodyWindow lines.
func rcInv004FixtureBodyLines(specLines []string, headingRe *regexp.Regexp, bodyWindow int) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range specLines {
		if headingRe.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, fmt.Sprintf("heading matching %s not found", headingRe.String())
	}

	limit := headingIdx + 1 + bodyWindow
	if limit > len(specLines) {
		limit = len(specLines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := specLines[i]
		if rcInv004FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// rcInv004FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func rcInv004FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRCINV004EvidenceCorroborationGuarantee is the binding test for hk-63oh.45.
//
// It verifies that RC-INV-004 is correctly declared in
// specs/reconciliation/spec.md AND that the upstream requirement EV-023a is
// correctly declared in specs/event-model.md.
func TestRCINV004EvidenceCorroborationGuarantee(t *testing.T) {
	t.Parallel()

	rcSpecFile := rcInv004FixtureSpecPath(t)
	rcLines := rcInv004FixtureLoadLines(t, rcSpecFile)

	// ── Part 1: RC-INV-004 body checks ────────────────────────────────────────

	rcBody, rcHeadingLineNo, reason := rcInv004FixtureBodyLines(rcLines, rcInv004FixtureHeading, rcInv004FixtureBodyWindow)
	if reason != "" {
		t.Fatalf("RC-INV-004 check(1): RC-INV-004 heading not found in specs/reconciliation/spec.md; "+
			"expected '#### RC-INV-004 —': %s", reason)
	}
	t.Logf("RC-INV-004 heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
		rcHeadingLineNo, len(rcBody))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	rcBodyChecks := []check{
		{
			id:     "2",
			label:  "store-divergence-detected-declared",
			needle: "store_divergence_detected",
			detail: "RC-INV-004 body must declare 'store_divergence_detected' " +
				"(expected phrase 'store_divergence_detected'); this is the event " +
				"type whose emissions the invariant constrains — every emission of " +
				"this event traced through any detector dispatch must carry a " +
				"non-inconclusive corroboration value",
		},
		{
			id:     "3",
			label:  "ev-023a-upstream-contract-cited",
			needle: "EV-023a",
			detail: "RC-INV-004 body must cite 'EV-023a' " +
				"(expected phrase 'EV-023a'); this is the upstream event-model " +
				"requirement that defines the evidence-corroboration classification " +
				"contract — RC-INV-004 is the reconciliation-side projection of this " +
				"upstream contract onto every detector dispatch point",
		},
		{
			id:     "4",
			label:  "corroboration-payload-field-declared",
			needle: "corroboration",
			detail: "RC-INV-004 body must declare 'corroboration' " +
				"(expected phrase 'corroboration'); this is the concrete payload " +
				"field on store_divergence_detected whose value the invariant " +
				"constrains — the emission-layer validator must check this field " +
				"before writing the event to JSONL",
		},
		{
			id:     "5",
			label:  "git-corroborated-enum-value-declared",
			needle: "git-corroborated",
			detail: "RC-INV-004 body must declare 'git-corroborated' " +
				"(expected phrase 'git-corroborated'); naming the first valid " +
				"corroboration enum value confirms the closed two-value domain — " +
				"evidence corroborated against the git DAG is the git-authority branch",
		},
		{
			id:     "6",
			label:  "beads-corroborated-enum-value-declared",
			needle: "beads-corroborated",
			detail: "RC-INV-004 body must declare 'beads-corroborated' " +
				"(expected phrase 'beads-corroborated'); naming the second valid " +
				"corroboration enum value closes the domain — evidence corroborated " +
				"against Beads is the beads-authority branch",
		},
		{
			id:     "7",
			label:  "divergence-inconclusive-alternative-declared",
			needle: "divergence_inconclusive",
			detail: "RC-INV-004 body must declare 'divergence_inconclusive' " +
				"(expected phrase 'divergence_inconclusive'); this is the mandatory " +
				"alternative event for single-source observations whose corroboration " +
				"cannot be established — the emission layer MUST route inconclusive " +
				"observations to this event instead of store_divergence_detected",
		},
		{
			id:     "8",
			label:  "rc-020a-dispatch-cadence-cited",
			needle: "RC-020a",
			detail: "RC-INV-004 body must cite 'RC-020a' " +
				"(expected phrase 'RC-020a'); this is the detector dispatch-cadence " +
				"requirement that defines the three dispatch points (startup, " +
				"on-demand, scheduled) over which the corroboration invariant applies — " +
				"without this cite the invariant scope is ambiguous",
		},
	}

	for _, c := range rcBodyChecks {
		c := c
		t.Run(fmt.Sprintf("rc-inv-004-check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !rcInv004FixtureBodyContains(rcBody, c.needle) {
				t.Errorf(
					"RC-INV-004 check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-INV-004 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, rcHeadingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in RC-INV-004 body.
	t.Run("rc-inv-004-check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range rcBody {
			if rcInv004FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-INV-004 check(9) FAILED: Tags: mechanism not found in RC-INV-004 body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-INV-004 body)\n"+
					"  detail: RC-INV-004 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				rcHeadingLineNo,
			)
		}
	})

	// ── Part 2: EV-023a upstream requirement checks ───────────────────────────
	//
	// The cross-spec cite rc-inv-004 → ev-023a requires EV-023a to exist in
	// event-model.md and to carry the evidence-inconclusive classification
	// language that RC-INV-004 projects onto the reconciliation detector.

	evSpecFile := rcInv004FixtureEventModelPath(t)
	evLines := rcInv004FixtureLoadLines(t, evSpecFile)

	evBody, evHeadingLineNo, evReason := rcInv004FixtureBodyLines(evLines, rcInv004FixtureEV023aHeading, rcInv004FixtureBodyWindow)
	if evReason != "" {
		t.Fatalf("RC-INV-004 check(10): EV-023a heading not found in specs/event-model.md; "+
			"expected '#### EV-023a —': %s", evReason)
	}
	t.Logf("EV-023a heading found at specs/event-model.md line %d; body window = %d lines",
		evHeadingLineNo, len(evBody))

	evBodyChecks := []check{
		{
			id:     "11",
			label:  "ev-023a-inconclusive-classification-declared",
			needle: "inconclusive",
			detail: "EV-023a body must declare 'inconclusive' " +
				"(expected phrase 'inconclusive'); EV-023a defines the three-way " +
				"classification — git-corroborated, beads-corroborated, or inconclusive " +
				"— that RC-INV-004 projects onto the reconciliation detector; without " +
				"the inconclusive case the upstream contract does not bound false-positive " +
				"investigator dispatches",
		},
	}

	for _, c := range evBodyChecks {
		c := c
		t.Run(fmt.Sprintf("ev-023a-check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !rcInv004FixtureBodyContains(evBody, c.needle) {
				t.Errorf(
					"EV-023a check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-023a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, evHeadingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Logf("hk-63oh.45 audit complete — RC-INV-004 at spec.md line %d, EV-023a at event-model.md line %d",
		rcHeadingLineNo, evHeadingLineNo)
}
