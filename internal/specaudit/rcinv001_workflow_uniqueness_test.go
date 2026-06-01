package specaudit_test

// hk-63oh.44 binding test — RC-INV-001: reconciliation-as-workflow uniqueness
// across daemon lifetime.
//
// Spec ref: specs/reconciliation/spec.md §5 RC-INV-001.
//
// RC-INV-001 states: Across every daemon lifetime, every reconciliation
// dispatch MUST run as a DOT-tagged workflow with `workflow_class =
// reconciliation`. No non-reconciliation workflow node may invoke RC-025
// verdict-execution mechanics.
//
// The sensor has three parts:
//
//  (a) Daemon tags Workflow records with workflow_class; startup audit log
//      asserts every reconciliation_verdict_* event traces back to a Workflow
//      whose workflow_class = reconciliation.
//  (b) JSONL query at audit time filters reconciliation_verdict_emitted events
//      and joins against Workflow registry; any event whose source workflow's
//      class is NOT reconciliation fails the audit.
//  (c) Per-daemon-lifecycle audit log sample: one tag check per N verdicts
//      (default N=10) at runtime, full scan at shutdown.
//
// The RC-INV-001 body also carries the sensor→sensor edge to EM-INV-005 per
// the F-refs-EV-6 v0.8 sensor→sensor explicit-ID-cite extension:
// [execution-model.md §5 EM-INV-005] — "git wins on completion" — is the
// upstream execution-model invariant this sensor enforces from the
// reconciliation side.
//
// # Audit frame
//
// This test is a spec-corpus sensor. It verifies that RC-INV-001 is correctly
// declared in specs/reconciliation/spec.md AND that the upstream invariant
// EM-INV-005 is correctly declared in specs/execution-model.md so that:
//
//  1. RC-INV-001 heading is present — the invariant exists in the normative spec.
//
//  2. "workflow_class = reconciliation" is declared — the invariant names the
//     exact tag value every reconciliation workflow MUST carry.
//
//  3. "reconciliation_verdict_*" event family is declared — the sensor names
//     the event types it audits against the Workflow registry.
//
//  4. "reconciliation_verdict_emitted" is declared — the primary event targeted
//     by the JSONL-join audit (sensor part b) is named explicitly.
//
//  5. Workflow registry tagging obligation is declared — "registry" or
//     "Workflow registry" language is present so the obligation to tag the
//     in-memory registry is unambiguous.
//
//  6. "N=10" per-verdict audit cadence is declared — the runtime sampling
//     rate is specified so implementations know the default interval.
//
//  7. "shutdown" full-scan obligation is declared — the end-of-lifecycle
//     full scan is named explicitly.
//
//  8. "EM-INV-005" cross-spec cite is present — the sensor→sensor edge to the
//     upstream execution-model invariant is declared in the RC-INV-001 body
//     per F-refs-EV-6 v0.8 sensor→sensor explicit-ID-cite extension.
//
//  9. Tags: mechanism is present in the RC-INV-001 body window.
//
// 10. EM-INV-005 heading is present in specs/execution-model.md — the upstream
//     invariant referenced by the sensor→sensor edge exists and is normative.
//
// 11. "Git wins on completion" language is present in the EM-INV-005 body —
//     the upstream invariant correctly states its scope (completion
//     disagreement resolution in git's favour), not just its name.
//
// # Failure modes
//
//   - RC-INV-001 heading missing: invariant not found in specs/reconciliation/spec.md.
//   - "workflow_class = reconciliation" absent: invariant tag-value not declared.
//   - "reconciliation_verdict_*" absent: audited event family not named.
//   - "reconciliation_verdict_emitted" absent: JSONL-join target event not named.
//   - "registry" absent: Workflow registry tagging obligation not declared.
//   - "N=10" absent: per-verdict runtime cadence not specified.
//   - "shutdown" absent: end-of-lifecycle full-scan obligation not named.
//   - "EM-INV-005" absent: sensor→sensor upstream cite missing from RC-INV-001 body.
//   - Tags: mechanism missing from RC-INV-001 body window.
//   - EM-INV-005 heading missing: upstream invariant not found in specs/execution-model.md.
//   - "Git wins on completion" absent: EM-INV-005 body does not state git authority.
//
// # Helper prefix
//
// All package-level identifiers in this file use the rcInv001Fixture prefix per
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

// rcInv001FixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at internal/specaudit/...; the repo root is
// two directories up.
func rcInv001FixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rcInv001FixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// rcInv001FixtureSpecPath returns the absolute path to
// specs/reconciliation/spec.md.
func rcInv001FixtureSpecPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rcInv001FixtureRepoRoot(t), "specs", "reconciliation", "spec.md")
}

// rcInv001FixtureExecModelPath returns the absolute path to
// specs/execution-model.md.
func rcInv001FixtureExecModelPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rcInv001FixtureRepoRoot(t), "specs", "execution-model.md")
}

// rcInv001FixtureHeading matches the RC-INV-001 level-4 requirement heading.
var rcInv001FixtureHeading = regexp.MustCompile(`^#### RC-INV-001 —`)

// rcInv001FixtureEMInv005Heading matches the EM-INV-005 level-4 requirement heading.
var rcInv001FixtureEMInv005Heading = regexp.MustCompile(`^#### EM-INV-005 —`)

// rcInv001FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var rcInv001FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// rcInv001FixtureTagsMechanism matches a "Tags: mechanism" line.
var rcInv001FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// rcInv001FixtureBodyWindow is the maximum number of lines after the heading
// to scan for requirement-body content.
const rcInv001FixtureBodyWindow = 30

// rcInv001FixtureLoadLines opens specFile and returns all lines.
func rcInv001FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("rcInv001FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("rcInv001FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// rcInv001FixtureBodyLines returns the lines comprising the body of the
// requirement at headingRe in specLines, up to the next section heading or
// bodyWindow lines.
func rcInv001FixtureBodyLines(specLines []string, headingRe *regexp.Regexp, bodyWindow int) (body []string, headingLineNo int, reason string) {
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
		if rcInv001FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// rcInv001FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func rcInv001FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestRCINV001WorkflowClassUniqueness is the binding test for hk-63oh.44.
//
// It verifies that RC-INV-001 is correctly declared in
// specs/reconciliation/spec.md AND that the upstream invariant EM-INV-005 is
// correctly declared in specs/execution-model.md.
func TestRCINV001WorkflowClassUniqueness(t *testing.T) {
	t.Parallel()

	rcSpecFile := rcInv001FixtureSpecPath(t)
	rcLines := rcInv001FixtureLoadLines(t, rcSpecFile)

	// ── Part 1: RC-INV-001 body checks ────────────────────────────────────────

	rcBody, rcHeadingLineNo, reason := rcInv001FixtureBodyLines(rcLines, rcInv001FixtureHeading, rcInv001FixtureBodyWindow)
	if reason != "" {
		t.Fatalf("RC-INV-001 check(1): RC-INV-001 heading not found in specs/reconciliation/spec.md; "+
			"expected '#### RC-INV-001 —': %s", reason)
	}
	t.Logf("RC-INV-001 heading found at specs/reconciliation/spec.md line %d; body window = %d lines",
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
			label:  "workflow-class-reconciliation-declared",
			needle: "workflow_class = reconciliation",
			detail: "RC-INV-001 body must declare 'workflow_class = reconciliation' " +
				"(expected phrase 'workflow_class = reconciliation'); this is the exact tag " +
				"value every reconciliation workflow MUST carry — the sensor validates this " +
				"tag on every Workflow record it inspects",
		},
		{
			id:     "3",
			label:  "reconciliation-verdict-star-event-family",
			needle: "reconciliation_verdict_*",
			detail: "RC-INV-001 body must declare 'reconciliation_verdict_*' " +
				"(expected phrase 'reconciliation_verdict_*'); naming the event family " +
				"establishes the sensor's audit scope — every event matching this prefix " +
				"must trace back to a workflow_class = reconciliation Workflow",
		},
		{
			id:     "4",
			label:  "reconciliation-verdict-emitted-named",
			needle: "reconciliation_verdict_emitted",
			detail: "RC-INV-001 body must declare 'reconciliation_verdict_emitted' " +
				"(expected phrase 'reconciliation_verdict_emitted'); this is the specific " +
				"event targeted by the JSONL-join audit (sensor part b) — naming it " +
				"explicitly distinguishes the primary join key from the broader event family",
		},
		{
			id:     "5",
			label:  "workflow-registry-tagging-declared",
			needle: "registry",
			detail: "RC-INV-001 body must reference 'registry' " +
				"(expected phrase 'registry'); the obligation to tag the Workflow record " +
				"in the daemon's in-memory registry must be unambiguous — the sensor " +
				"joins JSONL events against this registry at audit time",
		},
		{
			id:     "6",
			label:  "per-n-verdicts-cadence-n10-declared",
			needle: "N=10",
			detail: "RC-INV-001 body must declare 'N=10' as the default per-verdict " +
				"audit cadence (expected phrase 'N=10'); specifying the default sampling " +
				"rate ensures implementations converge on the same interval — one tag " +
				"check per 10 verdicts at runtime",
		},
		{
			id:     "7",
			label:  "full-scan-at-shutdown-declared",
			needle: "shutdown",
			detail: "RC-INV-001 body must declare 'shutdown' as the full-scan trigger " +
				"(expected phrase 'shutdown'); the end-of-lifecycle full-JSONL scan is " +
				"the mandatory comprehensive audit pass — naming it explicitly ensures " +
				"implementations do not substitute the periodic sample for the final scan",
		},
		{
			id:     "8",
			label:  "em-inv-005-sensor-sensor-cite",
			needle: "EM-INV-005",
			detail: "RC-INV-001 body must cite 'EM-INV-005' " +
				"(expected phrase 'EM-INV-005'); per F-refs-EV-6 v0.8 sensor→sensor " +
				"explicit-ID-cite extension, the RC-INV-001 body must carry an explicit " +
				"cross-spec cite to [execution-model.md §5 EM-INV-005] to declare the " +
				"rc-inv-001 → em-inv-005 sensor→sensor edge",
		},
	}

	for _, c := range rcBodyChecks {
		c := c
		t.Run(fmt.Sprintf("rc-inv-001-check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !rcInv001FixtureBodyContains(rcBody, c.needle) {
				t.Errorf(
					"RC-INV-001 check(%s) FAILED: %s\n"+
						"  spec:    specs/reconciliation/spec.md line ~%d (RC-INV-001 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, rcHeadingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in RC-INV-001 body.
	t.Run("rc-inv-001-check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range rcBody {
			if rcInv001FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"RC-INV-001 check(9) FAILED: Tags: mechanism not found in RC-INV-001 body window\n"+
					"  spec:   specs/reconciliation/spec.md line ~%d (RC-INV-001 body)\n"+
					"  detail: RC-INV-001 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				rcHeadingLineNo,
			)
		}
	})

	// ── Part 2: EM-INV-005 upstream invariant checks ──────────────────────────
	//
	// The sensor→sensor edge rc-inv-001 → em-inv-005 requires EM-INV-005 to
	// exist and to carry the "git wins on completion" language so the
	// cross-spec cite is valid.

	emSpecFile := rcInv001FixtureExecModelPath(t)
	emLines := rcInv001FixtureLoadLines(t, emSpecFile)

	emBody, emHeadingLineNo, emReason := rcInv001FixtureBodyLines(emLines, rcInv001FixtureEMInv005Heading, rcInv001FixtureBodyWindow)
	if emReason != "" {
		t.Fatalf("RC-INV-001 check(10): EM-INV-005 heading not found in specs/execution-model.md; "+
			"expected '#### EM-INV-005 —': %s", emReason)
	}
	t.Logf("EM-INV-005 heading found at specs/execution-model.md line %d; body window = %d lines",
		emHeadingLineNo, len(emBody))

	emBodyChecks := []check{
		{
			id:     "11",
			label:  "em-inv-005-no-subsystem-prefers-jsonl-over-git",
			needle: "JSONL over git",
			detail: "EM-INV-005 body must declare 'JSONL over git' " +
				"(expected phrase 'JSONL over git', from 'No subsystem may silently prefer " +
				"Beads or JSONL over git'); the RC-INV-001 sensor→sensor cite to EM-INV-005 " +
				"is grounded in this commitment — no subsystem may treat JSONL or Beads as " +
				"authoritative over git; the RC-INV-001 sensor enforces this from the " +
				"reconciliation side by requiring workflow_class = reconciliation on every " +
				"reconciliation_verdict_* source workflow",
		},
	}

	for _, c := range emBodyChecks {
		c := c
		t.Run(fmt.Sprintf("em-inv-005-check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !rcInv001FixtureBodyContains(emBody, c.needle) {
				t.Errorf(
					"EM-INV-005 check(%s) FAILED: %s\n"+
						"  spec:    specs/execution-model.md line ~%d (EM-INV-005 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, emHeadingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Logf("hk-63oh.44 audit complete — RC-INV-001 at spec.md line %d, EM-INV-005 at execution-model.md line %d",
		rcHeadingLineNo, emHeadingLineNo)
}
