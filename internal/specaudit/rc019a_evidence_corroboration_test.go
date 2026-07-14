//go:build specaudit

package specaudit_test

// hk-63oh.20 binding test — RC-019a: every store_divergence_detected emission
// MUST carry a non-inconclusive corroboration value; single-source observations
// MUST emit divergence_inconclusive instead.
//
// Spec ref: specs/reconciliation/spec.md §4.3 RC-019a.
//
// RC-019a states: Every `store_divergence_detected` emission MUST adhere to
// EV-023a per [event-model.md §4.5]: the detector MUST classify the candidate
// divergence into the `corroboration` enum value (`git-corroborated |
// beads-corroborated`) per [event-model.md §8.6.8] payload. Single-source
// observations whose corroboration cannot be established MUST emit
// `divergence_inconclusive` per [event-model.md §8.6.10] instead, NOT
// `store_divergence_detected`. Cat 6b (post-emission corroboration impossible)
// is reachable when neither git nor Beads is readable; Cat 6b emissions are
// exempt from corroboration via the dedicated escalation path of §8.11.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that RC-019a is correctly
// declared in specs/reconciliation/spec.md so that:
//
//  1. RC-019a heading is present — the requirement exists in the normative spec.
//
//  2. "store_divergence_detected" is declared — the event this requirement
//     constrains is named in the RC-019a body.
//
//  3. "EV-023a" is cited — the upstream event-model requirement that defines
//     the corroboration contract is referenced.
//
//  4. "corroboration" is declared — the payload field name is named explicitly.
//
//  5. "git-corroborated" is declared — the enum value for git-side corroboration
//     is named in the body.
//
//  6. "beads-corroborated" is declared — the enum value for Beads-side
//     corroboration is named in the body.
//
//  7. "divergence_inconclusive" is declared — the alternative event for
//     single-source observations is named.
//
//  8. "Cat 6b" exemption is declared — the exemption path for mechanically
//     unrecoverable cases is named.
//
//  9. Tags: mechanism is present in the RC-019a body window.
//
// # Failure modes
//
//   - RC-019a heading missing: RC-019a heading not found in specs/reconciliation/spec.md.
//   - "store_divergence_detected" absent: constrained event not declared.
//   - "EV-023a" absent: upstream corroboration contract not cited.
//   - "corroboration" absent: payload field name not declared.
//   - "git-corroborated" absent: enum value not named.
//   - "beads-corroborated" absent: enum value not named.
//   - "divergence_inconclusive" absent: alternative event not declared.
//   - "Cat 6b" absent: exemption path not declared.
//   - Tags: mechanism missing from RC-019a body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the rc019aFixture prefix per
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

// rc019aFixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at:
//
//	internal/specaudit/rc019a_evidence_corroboration_test.go
//
// so the repo root is two directories up.
func rc019aFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rc019aFixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// rc019aFixtureSpecPath returns the absolute path to specs/reconciliation/spec.md.
func rc019aFixtureSpecPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rc019aFixtureRepoRoot(t), "specs", "reconciliation", "spec.md")
}

// rc019aFixtureLoadLines opens specFile and returns all lines.
func rc019aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("rc019aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("rc019aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// rc019aFixtureRC019aHeading matches the RC-019a level-4 requirement heading line.
var rc019aFixtureRC019aHeading = regexp.MustCompile(`^#### RC-019a —`)

// rc019aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of a requirement body window.
var rc019aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rc019aFixtureTagsMechanism matches a "Tags: mechanism" line.
var rc019aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rc019aFixtureBodyWindow is the maximum number of lines after a heading to
// scan for requirement-body content.
const rc019aFixtureBodyWindow = 30

// rc019aFixtureBodyLines returns the body lines of the RC-019a section: all lines
// after the matched heading up to (but not including) the next Markdown heading
// or rc019aFixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the heading is not found.
func rc019aFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if rc019aFixtureRC019aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "RC-019a heading not found; expected '#### RC-019a —' pattern in specs/reconciliation/spec.md"
	}

	limit := headingIdx + 1 + rc019aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if rc019aFixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

// rc019aFixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func rc019aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRC019aEvidenceCorroboration is the binding test for hk-63oh.20.
//
// It opens specs/reconciliation/spec.md, locates the RC-019a heading, and
// validates the nine audit checks listed in the file-level comment.
func TestRC019aEvidenceCorroboration(t *testing.T) {
	t.Parallel()

	specFile := rc019aFixtureSpecPath(t)
	lines := rc019aFixtureLoadLines(t, specFile)

	// Check (1): RC-019a heading present.
	rc019aBody, rc019aLineNo, rc019aReason := rc019aFixtureBodyLines(lines)
	if rc019aReason != "" {
		t.Fatalf("RC-019a check(1): %s", rc019aReason)
	}
	t.Logf("RC-019a heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
		rc019aLineNo, len(rc019aBody))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	rc019aChecks := []check{
		{
			id:     "2",
			label:  "store_divergence_detected event name",
			needle: "store_divergence_detected",
			detail: "RC-019a body must name 'store_divergence_detected' as the event this " +
				"requirement constrains; the corroboration obligation applies specifically " +
				"to every emission of this event type",
		},
		{
			id:     "3",
			label:  "EV-023a upstream contract citation",
			needle: "EV-023a",
			detail: "RC-019a body must cite 'EV-023a' from event-model.md §4.5; this is the " +
				"upstream event-model requirement that defines the corroboration contract " +
				"that RC-019a projects onto the reconciliation detector",
		},
		{
			id:     "4",
			label:  "corroboration payload field name",
			needle: "corroboration",
			detail: "RC-019a body must name 'corroboration' as the payload enum field; " +
				"this is the concrete field in the store_divergence_detected payload that " +
				"carries the corroboration classification per event-model.md §8.6.8",
		},
		{
			id:     "5",
			label:  "git-corroborated enum value",
			needle: "git-corroborated",
			detail: "RC-019a body must name 'git-corroborated' as one of the two valid " +
				"corroboration enum values; its presence confirms the two-valued enum " +
				"is declared (not a length-≥-2 array predicate per OQ-RC-008 fix)",
		},
		{
			id:     "6",
			label:  "beads-corroborated enum value",
			needle: "beads-corroborated",
			detail: "RC-019a body must name 'beads-corroborated' as the second valid " +
				"corroboration enum value; both enum values must be declared so implementations " +
				"know the complete domain",
		},
		{
			id:     "7",
			label:  "divergence_inconclusive alternative event",
			needle: "divergence_inconclusive",
			detail: "RC-019a body must name 'divergence_inconclusive' as the mandatory " +
				"alternative for single-source observations; this is the emit-path for " +
				"cases where corroboration cannot be established and emission of " +
				"store_divergence_detected would be premature",
		},
		{
			id:     "8",
			label:  "Cat 6b exemption path",
			needle: "Cat 6b",
			detail: "RC-019a body must declare the 'Cat 6b' exemption — cases where neither " +
				"git nor Beads is readable are mechanically unrecoverable and are exempt " +
				"from the corroboration requirement via the §8.11 escalation path",
		},
	}

	for _, c := range rc019aChecks {
		c := c
		t.Run(fmt.Sprintf("RC019a-check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !rc019aFixtureBodyContains(rc019aBody, c.needle) {
				t.Errorf(
					"RC-019a check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-019a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, rc019aLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in RC-019a body.
	t.Run("RC019a-check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range rc019aBody {
			if rc019aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-019a check(9) FAILED: Tags: mechanism not found in RC-019a body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-019a body)\n"+
					"  detail: RC-019a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				rc019aLineNo,
			)
		}
	})

	t.Logf("hk-63oh.20 audit complete — RC-019a heading at line %d (body %d lines)",
		rc019aLineNo, len(rc019aBody))
}
