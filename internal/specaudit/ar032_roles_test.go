package specaudit_test

// AR-032 binding test — every role name used in the spec corpus is one of the
// seven canonical names (or an allowed synthesized value).
//
// Spec ref: specs/architecture.md §4.8 AR-032.
//
// AR-032 states: "Foundation names seven roles drawn from the AlphaGo north-
// star: Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor.
// These role names MUST be the canonical vocabulary across all specs. A
// subsystem spec that invents an alternative role name for a function already
// covered by one of the seven fails review."
//
// Canonical vocabulary per AR-032 + synthesized values per EM-046:
//
//	Planner, Researcher, Builder, Reviewer, Verifier, Scheduler, Governor
//	daemon          (daemon-synthesized transitions, EM-046)
//	reconciliation  (reconciliation-directed transitions, EM-046)
//
// Cross-check: internal/core/actorrole.go declares the same nine values as the
// closed ActorRole enum. This test MUST NOT import core (the audit walks markdown
// specs, not Go code); actorrole.go is a cross-reference only.
//
// # Audit frame (Path A)
//
// The spec corpus uses role names in two syntactic frames:
//
//  1. Backtick-quoted key=value tokens: `actor_role=Reviewer`, `role=investigator`.
//     Both the `actor_role=<V>` and `role=<V>` forms (with or without spaces
//     around "=") appear inline in requirement bodies.
//
//  2. `**role** = \`<V>\`` delegation-path prose (architecture.md §4.8 delegation
//     path template).
//
// The test scans every normative requirement body (30-line look-ahead window per
// the AR-005/AR-001 sibling pattern) for lines matching these frames, extracts
// the value token, and validates it against the canonical vocabulary.
//
// Exclusions:
//   - Lines beginning with ">" are INFORMATIVE; role tokens on those lines are
//     not normative and are excluded from the audit.
//   - Revision-history table rows (lines beginning with "|") are excluded; they
//     are retrospective prose, not normative requirements.
//   - The look-ahead window ends at the next heading or at 30 lines, whichever
//     comes first, matching ar001/ar005 sibling discipline.
//
// # Failure mode
//
// A single failure mode: a role value matched by the audit frame is not in the
// canonical vocabulary. No "missing" or "duplicate" checks — this is a negative
// coverage audit (no spec MAY invent names for covered functions), not a
// presence audit.
//
// Known violations covered by in-flight beads are pinned in
// ar032FixtureExpectedViolations (Path B skip-list pattern per AR-001/AR-005
// siblings).

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

// ar032FixtureCanonicalRoles is the full canonical role vocabulary per
// AR-032 (seven declared roles) and EM-046 (two synthesized values).
// Values are case-sensitive; the casing matches actorrole.go constants.
var ar032FixtureCanonicalRoles = map[string]bool{
	"Planner":        true,
	"Researcher":     true,
	"Builder":        true,
	"Reviewer":       true,
	"Verifier":       true,
	"Scheduler":      true,
	"Governor":       true,
	"daemon":         true,
	"reconciliation": true,
}

// ar032FixtureCanonicalRoleNames is an ordered slice of the canonical role
// names for use in error messages.
var ar032FixtureCanonicalRoleNames = []string{
	"Planner", "Researcher", "Builder", "Reviewer",
	"Verifier", "Scheduler", "Governor",
	"daemon", "reconciliation",
}

// ar032FixtureAnyHeading matches any #### level-4 requirement heading of the
// form "#### PREFIX-... — ...". Open-question headings are identified
// separately by ar032FixtureIsOpenQuestion.
var ar032FixtureAnyHeading = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?) —`)

// ar032FixtureAnySectionHeading matches any Markdown heading line (level 1–4).
// Used to break the look-ahead window for role scanning.
var ar032FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ar032FixtureIsOpenQuestion reports whether a heading line is an open-question
// entry (OQ-PREFIX-NNN). Open questions are advisory and carry no role-vocabulary
// obligation.
var ar032FixtureIsOpenQuestion = regexp.MustCompile(`^#### OQ-`)

// ar032FixtureIsInformative reports whether a line is an INFORMATIVE block
// (Markdown blockquote beginning with ">"). INFORMATIVE text is advisory; role
// tokens on INFORMATIVE lines are excluded from the normative audit.
var ar032FixtureIsInformative = regexp.MustCompile(`^>`)

// ar032FixtureIsRevisionRow reports whether a line is a revision-history table
// row (beginning with "|"). Revision rows are retrospective prose; they are
// excluded from the normative audit even when they fall inside a look-ahead
// window.
var ar032FixtureIsRevisionRow = regexp.MustCompile(`^\|`)

// ar032FixtureActorRolePattern matches a backtick-quoted `actor_role=<value>`
// token (with optional whitespace around "=").
// Capture group 1: the role value.
var ar032FixtureActorRolePattern = regexp.MustCompile("`actor_role\\s*=\\s*([A-Za-z][A-Za-z0-9_-]*)`")

// ar032FixtureRoleEqPattern matches a backtick-quoted `role=<value>` token
// (with optional whitespace around "="). This frame captures LaunchSpec
// field-value pairs like `role = investigator` or `role=Builder`.
// Capture group 1: the role value.
var ar032FixtureRoleEqPattern = regexp.MustCompile("`role\\s*=\\s*([A-Za-z][A-Za-z0-9_-]*)`")

// ar032FixtureDelegPathRolePattern matches the delegation-path prose form
// "**role** = `<value>`" that appears in normative delegation-path blocks
// (e.g., "**role** = `Reviewer` per §4.8").
// Capture group 1: the role value.
var ar032FixtureDelegPathRolePattern = regexp.MustCompile(`\*\*role\*\*\s*=\s*` + "`([A-Za-z][A-Za-z0-9_-]*)`")

// ar032FixtureRoleValuePattern matches the prose form "the `role` value `<V>`"
// that appears when a requirement declares a LaunchSpec field value inline
// (e.g., "the `role` value `investigator` are the canonical pair").
// Capture group 1: the role value.
var ar032FixtureRoleValuePattern = regexp.MustCompile("`role`\\s+value\\s+`([A-Za-z][A-Za-z0-9_-]*)`")

// ar032FixtureViolation records a single AR-032 role-vocabulary violation.
type ar032FixtureViolation struct {
	file   string
	lineNo int    // 1-based line number of the violating line
	reqID  string // owning requirement ID, or "free-floating"
	frame  string // syntactic frame matched: "actor_role=", "role=", "**role**="
	value  string // the non-canonical role value found
	detail string
}

func (v ar032FixtureViolation) String() string {
	return fmt.Sprintf("%s:%d: [invalid-role] %s — frame=%s value=%q — %s",
		v.file, v.lineNo, v.reqID, v.frame, v.value, v.detail)
}

// ar032FixtureSpecFiles returns the set of spec files to audit.
//
// Scope mirrors ar005FixtureSpecFiles:
//   - specs/*.md    (top-level spec files)
//   - specs/**/spec.md (subsystem spec files nested one directory deep)
func ar032FixtureSpecFiles(t *testing.T) []string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar032FixtureSpecFiles: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar032_roles_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	specsDir := filepath.Join(repoRoot, "specs")

	var files []string

	// Top-level *.md files.
	topEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("ar032FixtureSpecFiles: ReadDir %s: %v", specsDir, err)
	}
	for _, e := range topEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		files = append(files, filepath.Join(specsDir, e.Name()))
	}

	// Nested spec.md files (one level deep).
	subEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("ar032FixtureSpecFiles: ReadDir(sub) %s: %v", specsDir, err)
	}
	for _, e := range subEntries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(specsDir, e.Name(), "spec.md")
		if _, statErr := os.Stat(candidate); statErr == nil {
			files = append(files, candidate)
		}
	}

	if len(files) == 0 {
		t.Fatalf("ar032FixtureSpecFiles: no spec files found under %s", specsDir)
	}
	return files
}

// ar032FixtureExtractRoleValues scans a single line for role value tokens
// matched by the four audit frames. It returns one entry per match:
// (frame, value). The line number and requirement context are supplied by
// the caller.
func ar032FixtureExtractRoleValues(line string) []struct{ frame, value string } {
	var hits []struct{ frame, value string }

	for _, m := range ar032FixtureActorRolePattern.FindAllStringSubmatch(line, -1) {
		hits = append(hits, struct{ frame, value string }{"actor_role=", m[1]})
	}
	for _, m := range ar032FixtureRoleEqPattern.FindAllStringSubmatch(line, -1) {
		hits = append(hits, struct{ frame, value string }{"role=", m[1]})
	}
	for _, m := range ar032FixtureDelegPathRolePattern.FindAllStringSubmatch(line, -1) {
		hits = append(hits, struct{ frame, value string }{"**role**=", m[1]})
	}
	for _, m := range ar032FixtureRoleValuePattern.FindAllStringSubmatch(line, -1) {
		hits = append(hits, struct{ frame, value string }{"role-value", m[1]})
	}

	return hits
}

// ar032FixtureParseViolations parses one spec file and returns all AR-032
// role-vocabulary violations found within normative requirement bodies.
//
// Parser strategy (mirrors ar001/ar005):
//  1. Walk lines, tracking the current normative requirement heading.
//  2. For each line in the 30-line look-ahead window (before the next heading),
//     skip INFORMATIVE lines ("> ...") and revision-history table rows ("| ...").
//  3. Extract role value tokens via the three audit frames.
//  4. Validate each value against the canonical vocabulary.
func ar032FixtureParseViolations(t *testing.T, specFile string) []ar032FixtureViolation {
	t.Helper()

	//nolint:gosec // G304: path comes from ar032FixtureSpecFiles which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar032FixtureParseViolations: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar032FixtureParseViolations: scan %s: %v", specFile, scanErr)
	}

	// Relative path for readable error messages.
	relFile := specFile
	if idx := strings.Index(specFile, "/specs/"); idx >= 0 {
		relFile = "specs/" + specFile[idx+len("/specs/"):]
	}

	// Collect all normative requirement headings (1-based line numbers).
	type reqContext struct {
		headingLineNo int
		reqID         string
	}
	var reqs []reqContext
	for i, line := range lines {
		if !ar032FixtureAnyHeading.MatchString(line) {
			continue
		}
		if ar032FixtureIsOpenQuestion.MatchString(line) {
			continue
		}
		m := ar032FixtureAnyHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		reqs = append(reqs, reqContext{
			headingLineNo: i + 1, // 1-based
			reqID:         m[1],
		})
	}

	// ownerFor returns the requirement ID that owns line at index idx (0-based),
	// or "free-floating" if no requirement window covers it.
	ownerFor := func(lineIdx int) string {
		ownerReqID := "free-floating"
		for _, req := range reqs {
			hIdx := req.headingLineNo - 1 // 0-based
			if hIdx >= lineIdx {
				break
			}
			windowEnd := hIdx + 1 + 30
			if windowEnd > len(lines) {
				windowEnd = len(lines)
			}
			if lineIdx >= windowEnd {
				continue
			}
			// Check that no new heading interrupts the window between the
			// requirement heading and the candidate line.
			interrupted := false
			for k := hIdx + 1; k < lineIdx && k < windowEnd; k++ {
				if ar032FixtureAnySectionHeading.MatchString(lines[k]) {
					interrupted = true
					break
				}
			}
			if !interrupted {
				ownerReqID = req.reqID
			}
		}
		return ownerReqID
	}

	var violations []ar032FixtureViolation

	for i, line := range lines {
		// Skip INFORMATIVE blocks and revision-history table rows.
		if ar032FixtureIsInformative.MatchString(line) {
			continue
		}
		if ar032FixtureIsRevisionRow.MatchString(line) {
			continue
		}

		hits := ar032FixtureExtractRoleValues(line)
		if len(hits) == 0 {
			continue
		}

		reqID := ownerFor(i)
		lineNo := i + 1 // 1-based

		for _, hit := range hits {
			if ar032FixtureCanonicalRoles[hit.value] {
				continue // value is canonical — no violation
			}
			violations = append(violations, ar032FixtureViolation{
				file:   relFile,
				lineNo: lineNo,
				reqID:  reqID,
				frame:  hit.frame,
				value:  hit.value,
				detail: fmt.Sprintf(
					"role value %q is not in the canonical vocabulary {%s}; "+
						"use one of the AR-032 names or an EM-046 synthesized value",
					hit.value,
					strings.Join(ar032FixtureCanonicalRoleNames, ", "),
				),
			})
		}
	}

	return violations
}

// ar032FixtureExpectedViolation is a single entry in the expected-violations
// skip-list. A requirement in this map is a known AR-032 role-vocabulary defect
// covered by an in-flight bead.
type ar032FixtureExpectedViolation struct {
	// pinnedBy is the bead ID that owns the fix for this violation.
	pinnedBy string
	// reason is a human-readable explanation of the defect.
	reason string
}

// ar032FixtureViolationKey returns the skip-list lookup key for a violation.
// Format: "<file>:<lineNo>:<reqID>:<frame>:<value>".
func ar032FixtureViolationKey(v ar032FixtureViolation) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", v.file, v.lineNo, v.reqID, v.frame, v.value)
}

// ar032FixtureExpectedViolations is the skip-list of known AR-032 role-vocabulary
// violations that are intentionally deferred.
//
// Key format: "<relative-spec-path>:<line-number>:<requirementID>:<frame>:<value>".
// The line number MUST be the 1-based line number of the violating line.
//
// Rules (mirrors ar001FixtureExpectedViolations):
//   - An entry whose violation is NOT present causes t.Errorf("stale skip-list entry …").
//   - An entry whose violation IS present produces t.Logf and does NOT fail.
//   - Any NEW violation NOT in this map DOES fail the suite.
var ar032FixtureExpectedViolations = map[string]ar032FixtureExpectedViolation{}

// TestAR032RolesVocabulary is the binding test for AR-032 role-vocabulary
// enforcement.
//
// It walks every spec file in scope, scans normative requirement bodies for
// role name occurrences in the declared syntactic frames, and asserts that
// every extracted value is in the AR-032 canonical vocabulary (or the EM-046
// synthesized values).
//
// Known violations covered by in-flight beads are listed in
// ar032FixtureExpectedViolations. Those entries are logged (not failed) and
// produce an error if they become stale (violation no longer present).
func TestAR032RolesVocabulary(t *testing.T) {
	specFiles := ar032FixtureSpecFiles(t)

	var allViolations []ar032FixtureViolation
	for _, sf := range specFiles {
		vs := ar032FixtureParseViolations(t, sf)
		allViolations = append(allViolations, vs...)
	}

	// Build a set of violation keys found in the current corpus.
	foundKeys := make(map[string]ar032FixtureViolation, len(allViolations))
	for _, v := range allViolations {
		foundKeys[ar032FixtureViolationKey(v)] = v
	}

	// Check for stale skip-list entries (pinned violations that no longer exist).
	for key, entry := range ar032FixtureExpectedViolations {
		if _, present := foundKeys[key]; !present {
			t.Errorf("AR-032 skip-list: stale entry %q (pinned by %s) — violation no longer present; remove from ar032FixtureExpectedViolations",
				key, entry.pinnedBy)
		}
	}

	// Separate violations into expected (pinned) and unexpected (new failures).
	var unexpected []ar032FixtureViolation
	for _, v := range allViolations {
		key := ar032FixtureViolationKey(v)
		if entry, pinned := ar032FixtureExpectedViolations[key]; pinned {
			t.Logf("AR-032 expected violation (pinned by %s): %s — %s",
				entry.pinnedBy, key, entry.reason)
			continue
		}
		unexpected = append(unexpected, v)
	}

	if len(unexpected) == 0 {
		t.Logf("AR-032 audit: all %d spec files pass — every role name in the audited frames is canonical (%d known violation(s) pinned to in-flight beads)",
			len(specFiles), len(ar032FixtureExpectedViolations))
		return
	}

	// Report ALL unexpected violations so the full failure surface is visible.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"AR-032 violation: %d non-canonical role name(s) found in the spec corpus\n",
		len(unexpected),
	))
	sb.WriteString("(specs/architecture.md §4.8 AR-032: role names MUST be drawn from the canonical vocabulary)\n\n")
	sb.WriteString("Canonical role names: ")
	sb.WriteString(strings.Join(ar032FixtureCanonicalRoleNames, ", "))
	sb.WriteString("\n\n")
	for _, v := range unexpected {
		sb.WriteString("  ")
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	t.Error(sb.String())
}
