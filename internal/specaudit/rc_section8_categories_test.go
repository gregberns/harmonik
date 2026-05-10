package specaudit_test

// Reconciliation §8 category binding tests — spec-corpus sensors.
//
// Bead refs:
//   hk-63oh.63  Cat 1 — Idempotent rerun          (§8.2)
//   hk-63oh.64  Cat 2 — Non-idempotent in-flight   (§8.3)
//   hk-63oh.65  Cat 3 — Store disagreement generic  (§8.4)
//   hk-63oh.67  Cat 3b — Verdict-unexecuted          (§8.5)
//   hk-63oh.68  Cat 3c — Inverse premature-close     (§8.6)
//   hk-63oh.69  Cat 4 — Recoverable known state      (§8.7)
//   hk-63oh.70  Cat 5 — Clean restart                (§8.8)
//   hk-63oh.71  Cat 6a — Integrity violation, LLM-triageable (§8.11)
//   hk-63oh.72  Cat 6b — Integrity violation, mechanically unrecoverable (§8.11a)
//
// Spec ref: specs/reconciliation/spec.md §8.
//
// Each §8.N section MUST contain:
//
//  (a) "Detection rule" — the detection criterion for the category.
//  (b) "Default response" — the default daemon action.
//  (c) "Escalation path" — the escalation route.
//  (d) "Emitted event" — the event(s) emitted when this category is assigned.
//  (e) "Investigator?" — investigator-spawned indicator.
//  (f) "Auto-resolver?" — auto-resolver indicator.
//
// Additionally, each category section MUST name its key spec-level identifiers:
// the idempotency_class values (Cat 1/2), the specific event type names, and the
// investigator workflow references (Cat 2, Cat 3, Cat 6a).
//
// The test resolves the spec path at runtime so it works from any working directory.
//
// # Helper prefix
//
// All package-level identifiers in this file use the rcCatSensorFixture prefix
// per the implementer-protocol.md helper-prefix discipline.

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

// rcCatSensorFixtureRepoRoot resolves the repository root from this test
// file's source path. The test file lives at:
//
//	internal/specaudit/rc_section8_categories_test.go
//
// so the repo root is two directories up.
func rcCatSensorFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("rcCatSensorFixtureRepoRoot: runtime.Caller(0) failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// rcCatSensorFixtureSpecPath returns the absolute path to
// specs/reconciliation/spec.md.
func rcCatSensorFixtureSpecPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(rcCatSensorFixtureRepoRoot(t), "specs", "reconciliation", "spec.md")
}

// rcCatSensorFixtureLoadLines opens specFile and returns all lines.
func rcCatSensorFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("rcCatSensorFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("rcCatSensorFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// rcCatSensorFixtureAnyHeading matches any Markdown heading (level 1–4).
// Used to bound look-ahead windows when scanning section bodies.
var rcCatSensorFixtureAnyHeading = regexp.MustCompile(`^#{1,4} `)

// rcCatSensorFixtureBodyWindow is the maximum number of lines after a section
// heading to scan for requirement-body content.  §8.N sections are typically
// 12–20 lines; 60 gives generous headroom while keeping the window tight.
const rcCatSensorFixtureBodyWindow = 60

// rcCatSensorFixtureBodyLinesAfter returns the lines forming the body of the
// section starting at headingIdx: all lines from headingIdx+1 up to the next
// Markdown heading or rcCatSensorFixtureBodyWindow lines, whichever comes first.
func rcCatSensorFixtureBodyLinesAfter(lines []string, headingIdx int) []string {
	limit := headingIdx + 1 + rcCatSensorFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var body []string
	for i := headingIdx + 1; i < limit; i++ {
		if rcCatSensorFixtureAnyHeading.MatchString(lines[i]) {
			break
		}
		body = append(body, lines[i])
	}
	return body
}

// rcCatSensorFixtureBodyContains reports whether any line in body contains
// substr (case-sensitive exact substring match, per spec-corpus sensor convention).
func rcCatSensorFixtureBodyContains(body []string, substr string) bool {
	for _, line := range body {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// rcCatSensorFixtureFindHeadingIdx returns the 0-based index of the first line
// in lines matching re, or -1 if not found.
func rcCatSensorFixtureFindHeadingIdx(lines []string, re *regexp.Regexp) int {
	for i, line := range lines {
		if re.MatchString(line) {
			return i
		}
	}
	return -1
}

// rcCatSensorFixtureCategorySection describes a single §8.N category section
// in specs/reconciliation/spec.md and the mandatory corpus content for that
// section.
type rcCatSensorFixtureCategorySection struct {
	// sectionHeadingPattern matches the §8.N level-3 heading line.
	sectionHeadingPattern *regexp.Regexp

	// sectionLabel is a human-readable section identifier for failure messages.
	sectionLabel string

	// mandatoryPhrases lists (phrase, detail) pairs that must appear in the
	// section body.  Each pair is an exact substring to search for.
	mandatoryPhrases []rcCatSensorFixturePhrase

	// beadRef is the bead ID this sensor validates.
	beadRef string
}

// rcCatSensorFixturePhrase pairs a required spec-body substring with its
// diagnostic detail.
type rcCatSensorFixturePhrase struct {
	needle string
	detail string
}

// rcCatSensorFixtureCategorySections is the ordered list of §8.2–§8.11 category
// sections that must exist and must contain the listed phrases.
//
// Each entry is derived directly from the normative spec text at:
//
//	specs/reconciliation/spec.md §8.2 – §8.11
//
// Ordered by section number.  §8.4a (Cat 3a) is NOT in this batch; it is
// covered by its own bead.
var rcCatSensorFixtureCategorySections = []rcCatSensorFixtureCategorySection{
	{
		// hk-63oh.63  Cat 1 — Idempotent rerun (§8.2)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.2 Cat 1`),
		sectionLabel:          "§8.2 Cat 1 (Idempotent rerun)",
		beadRef:               "hk-63oh.63",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.2 must carry a 'Detection rule' bullet naming how Cat 1 is detected",
			},
			{
				needle: "idempotency_class",
				detail: "§8.2 detection rule must name the 'idempotency_class' attribute per EM-009",
			},
			{
				needle: "idempotent",
				detail: "§8.2 detection rule must state the class value is 'idempotent'",
			},
			{
				needle: "Default response",
				detail: "§8.2 must carry a 'Default response' bullet",
			},
			{
				needle: "re-spawn",
				detail: "§8.2 default response must name the 're-spawn' action",
			},
			{
				needle: "Escalation path",
				detail: "§8.2 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.2 must carry an 'Emitted event' bullet",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.2 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.2 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.2 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.64  Cat 2 — Non-idempotent in-flight (§8.3)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.3 Cat 2`),
		sectionLabel:          "§8.3 Cat 2 (Non-idempotent in-flight)",
		beadRef:               "hk-63oh.64",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.3 must carry a 'Detection rule' bullet naming how Cat 2 is detected",
			},
			{
				needle: "non-idempotent",
				detail: "§8.3 detection rule must name the 'non-idempotent' idempotency_class value per EM-009",
			},
			{
				needle: "recoverable-non-idempotent",
				detail: "§8.3 detection rule must name the 'recoverable-non-idempotent' idempotency_class value per EM-009",
			},
			{
				needle: "in_progress",
				detail: "§8.3 detection rule must reference the bead 'in_progress' state",
			},
			{
				needle: "RC-014",
				detail: "§8.3 detection rule must cite RC-014 (bounded JSONL divergence-evidence reader)",
			},
			{
				needle: "Default response",
				detail: "§8.3 must carry a 'Default response' bullet",
			},
			{
				needle: "investigator workflow",
				detail: "§8.3 default response must dispatch an investigator workflow",
			},
			{
				needle: "Escalation path",
				detail: "§8.3 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.3 must carry an 'Emitted event' bullet",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.3 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "reconciliation_verdict_emitted",
				detail: "§8.3 must name the downstream 'reconciliation_verdict_emitted' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.3 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.3 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.65  Cat 3 — Store disagreement generic (§8.4)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.4 Cat 3`),
		sectionLabel:          "§8.4 Cat 3 (Store disagreement generic)",
		beadRef:               "hk-63oh.65",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.4 must carry a 'Detection rule' bullet naming how Cat 3 is detected",
			},
			{
				needle: "inconsistent stories",
				detail: "§8.4 detection rule must state that stores tell 'inconsistent stories' about the same run",
			},
			{
				needle: "3a/3b/3c",
				detail: "§8.4 detection rule must state that no Cat 3 sub-category (3a/3b/3c) matches",
			},
			{
				needle: "Default response",
				detail: "§8.4 must carry a 'Default response' bullet",
			},
			{
				needle: "investigator workflow",
				detail: "§8.4 default response must dispatch an investigator workflow",
			},
			{
				needle: "RC-INV-001",
				detail: "§8.4 default response must cite RC-INV-001 (git-wins orientation)",
			},
			{
				needle: "Escalation path",
				detail: "§8.4 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.4 must carry an 'Emitted event' bullet",
			},
			{
				needle: "store_divergence_detected",
				detail: "§8.4 must name the 'store_divergence_detected' event",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.4 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.4 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.4 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.67  Cat 3b — Verdict-unexecuted (§8.5)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.5 Cat 3b`),
		sectionLabel:          "§8.5 Cat 3b (Verdict-unexecuted)",
		beadRef:               "hk-63oh.67",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.5 must carry a 'Detection rule' bullet naming how Cat 3b is detected",
			},
			{
				needle: "reconciliation_verdict_emitted",
				detail: "§8.5 detection rule must reference the 'reconciliation_verdict_emitted' commit",
			},
			{
				needle: "Harmonik-Verdict-Executed",
				detail: "§8.5 detection rule must name the 'Harmonik-Verdict-Executed' trailer per RC-025",
			},
			{
				needle: "RC-025",
				detail: "§8.5 detection rule must cite RC-025",
			},
			{
				needle: "Default response",
				detail: "§8.5 must carry a 'Default response' bullet",
			},
			{
				needle: "RC-026",
				detail: "§8.5 default response must cite RC-026 re-execution",
			},
			{
				needle: "staleness check",
				detail: "§8.5 default response must name the staleness check (RC-024)",
			},
			{
				needle: "Escalation path",
				detail: "§8.5 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.5 must carry an 'Emitted event' bullet",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.5 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.5 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.5 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.68  Cat 3c — Inverse premature-close (§8.6)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.6 Cat 3c`),
		sectionLabel:          "§8.6 Cat 3c (Inverse premature-close)",
		beadRef:               "hk-63oh.68",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.6 must carry a 'Detection rule' bullet naming how Cat 3c is detected",
			},
			{
				needle: "merge commit",
				detail: "§8.6 detection rule must reference a merge commit on the target branch",
			},
			{
				needle: "Harmonik-Run-ID",
				detail: "§8.6 detection rule must reference the 'Harmonik-Run-ID' trailer",
			},
			{
				needle: "in_progress",
				detail: "§8.6 detection rule must state the bead is still 'in_progress'",
			},
			{
				needle: "Default response",
				detail: "§8.6 must carry a 'Default response' bullet",
			},
			{
				needle: "accept-close-with-note",
				detail: "§8.6 default response must name the 'accept-close-with-note' verdict",
			},
			{
				needle: "Escalation path",
				detail: "§8.6 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.6 must carry an 'Emitted event' bullet",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.6 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "reconciliation_verdict_executed",
				detail: "§8.6 must name the downstream 'reconciliation_verdict_executed' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.6 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.6 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.69  Cat 4 — Recoverable known state (§8.7)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.7 Cat 4`),
		sectionLabel:          "§8.7 Cat 4 (Recoverable known state)",
		beadRef:               "hk-63oh.69",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.7 must carry a 'Detection rule' bullet naming how Cat 4 is detected",
			},
			{
				needle: "retry",
				detail: "§8.7 detection rule must reference retry/backoff state",
			},
			{
				needle: "Default response",
				detail: "§8.7 must carry a 'Default response' bullet",
			},
			{
				needle: "Auto-resume",
				detail: "§8.7 default response must describe auto-resume with the pending action",
			},
			{
				needle: "Escalation path",
				detail: "§8.7 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.7 must carry an 'Emitted event' bullet",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.7 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.7 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.7 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.70  Cat 5 — Clean restart (§8.8)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.8 Cat 5`),
		sectionLabel:          "§8.8 Cat 5 (Clean restart)",
		beadRef:               "hk-63oh.70",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.8 must carry a 'Detection rule' bullet naming how Cat 5 is detected",
			},
			{
				needle: "Nothing in-flight",
				detail: "§8.8 detection rule must state 'Nothing in-flight for this run'",
			},
			{
				needle: "RC-010",
				detail: "§8.8 detection rule must cite RC-010 (orphaned branches from prior runs)",
			},
			{
				needle: "Default response",
				detail: "§8.8 must carry a 'Default response' bullet",
			},
			{
				needle: "ready",
				detail: "§8.8 default response must name the 'ready' state per process-lifecycle §4.2",
			},
			{
				needle: "Escalation path",
				detail: "§8.8 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.8 must carry an 'Emitted event' bullet",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.8 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.8 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.8 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.71  Cat 6a — Integrity violation, LLM-triageable (§8.11)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.11 Cat 6a`),
		sectionLabel:          "§8.11 Cat 6a (Integrity violation, LLM-triageable)",
		beadRef:               "hk-63oh.71",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.11 must carry a 'Detection rule' bullet naming how Cat 6a is detected",
			},
			{
				needle: "workspace path",
				detail: "§8.11 detection rule must name the workspace-path-missing detector",
			},
			{
				needle: "Harmonik-Transition-ID",
				detail: "§8.11 detection rule must name the 'Harmonik-Transition-ID' trailer per EM-018",
			},
			{
				needle: "rebase",
				detail: "§8.11 detection rule must reference in-progress git operations (rebase/merge/etc.)",
			},
			{
				needle: "RC-003a",
				detail: "§8.11 must cite RC-003a priority ordering for workspace-missing/Cat 3 crossover",
			},
			{
				needle: "Default response",
				detail: "§8.11 must carry a 'Default response' bullet",
			},
			{
				needle: "investigator workflow",
				detail: "§8.11 default response must dispatch a Cat 6a investigator workflow",
			},
			{
				needle: "escalate-to-human",
				detail: "§8.11 default response must name 'escalate-to-human' as the default verdict",
			},
			{
				needle: "Escalation path",
				detail: "§8.11 must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.11 must carry an 'Emitted event' bullet",
			},
			{
				needle: "store_divergence_detected",
				detail: "§8.11 must name the 'store_divergence_detected' event",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.11 must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.11 must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.11 must carry an 'Auto-resolver?' indicator",
			},
		},
	},
	{
		// hk-63oh.72  Cat 6b — Integrity violation, mechanically unrecoverable (§8.11a)
		sectionHeadingPattern: regexp.MustCompile(`^### 8\.11a Cat 6b`),
		sectionLabel:          "§8.11a Cat 6b (Integrity violation, mechanically unrecoverable)",
		beadRef:               "hk-63oh.72",
		mandatoryPhrases: []rcCatSensorFixturePhrase{
			{
				needle: "Detection rule",
				detail: "§8.11a must carry a 'Detection rule' bullet naming how Cat 6b is detected",
			},
			{
				needle: "JSONL is corrupt",
				detail: "§8.11a detection rule must name JSONL corruption as a Cat 6b detector",
			},
			{
				needle: "git fsck",
				detail: "§8.11a detection rule must name 'git fsck' failure as a Cat 6b detector",
			},
			{
				needle: "object database",
				detail: "§8.11a detection rule must reference the git object database missing-commit detector",
			},
			{
				needle: "Default response",
				detail: "§8.11a must carry a 'Default response' bullet",
			},
			{
				needle: "Auto-escalate",
				detail: "§8.11a default response must state auto-escalation to operator without investigator spawn",
			},
			{
				needle: "operator_escalation_required",
				detail: "§8.11a default response must name the 'operator_escalation_required' event",
			},
			{
				needle: "Escalation path",
				detail: "§8.11a must carry an 'Escalation path' bullet",
			},
			{
				needle: "Emitted event",
				detail: "§8.11a must carry an 'Emitted event' bullet",
			},
			{
				needle: "reconciliation_category_assigned",
				detail: "§8.11a must name the 'reconciliation_category_assigned' event",
			},
			{
				needle: "Investigator?",
				detail: "§8.11a must carry an 'Investigator?' indicator",
			},
			{
				needle: "Auto-resolver?",
				detail: "§8.11a must carry an 'Auto-resolver?' indicator",
			},
		},
	},
}

// TestRCSection8CategoriesSpec is the binding test for reconciliation §8.2–§8.11a
// category sections.
//
// For each category section it verifies:
//
//	(a) The section heading is present.
//	(b) Each mandatory phrase is present in the section body.
//
// This sensor catches spec drift — removal, renaming, or truncation of the
// mandatory per-category content.
//
// Bead refs: hk-63oh.63, hk-63oh.64, hk-63oh.65, hk-63oh.67, hk-63oh.68,
//
//	hk-63oh.69, hk-63oh.70, hk-63oh.71, hk-63oh.72.
func TestRCSection8CategoriesSpec(t *testing.T) {
	t.Parallel()

	specFile := rcCatSensorFixtureSpecPath(t)
	lines := rcCatSensorFixtureLoadLines(t, specFile)

	for _, sec := range rcCatSensorFixtureCategorySections {
		sec := sec
		t.Run(sec.sectionLabel, func(t *testing.T) {
			t.Parallel()

			// (a) section heading must be present.
			headingIdx := rcCatSensorFixtureFindHeadingIdx(lines, sec.sectionHeadingPattern)
			if headingIdx < 0 {
				t.Fatalf(
					"RC §8 sensor (%s): section heading not found in specs/reconciliation/spec.md\n"+
						"  missing: heading matching %s\n"+
						"  bead:    %s\n"+
						"  detail:  the §8 category section heading is mandatory; its absence means "+
						"the category has been removed or renamed — a taxonomy amendment per RC-009 "+
						"and architecture.md §4.6 amendment protocol is required",
					sec.sectionLabel, sec.sectionHeadingPattern.String(), sec.beadRef,
				)
			}
			t.Logf("RC §8 sensor (%s): heading found at line %d", sec.sectionLabel, headingIdx+1)

			body := rcCatSensorFixtureBodyLinesAfter(lines, headingIdx)

			// (b) each mandatory phrase must be in the section body.
			for _, ph := range sec.mandatoryPhrases {
				ph := ph
				testName := fmt.Sprintf("phrase-%s",
					strings.ReplaceAll(strings.ReplaceAll(ph.needle, " ", "_"), "?", ""))
				t.Run(testName, func(t *testing.T) {
					t.Parallel()
					if !rcCatSensorFixtureBodyContains(body, ph.needle) {
						t.Errorf(
							"RC §8 sensor (%s): mandatory phrase not found\n"+
								"  spec:    specs/reconciliation/spec.md line ~%d (%s body)\n"+
								"  missing: %q\n"+
								"  bead:    %s\n"+
								"  detail:  %s",
							sec.sectionLabel, headingIdx+1, sec.sectionLabel,
							ph.needle, sec.beadRef, ph.detail,
						)
					}
				})
			}
		})
	}
}
