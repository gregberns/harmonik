package specaudit_test

// AR-005 binding test — every normative requirement carries exactly one Tags: line.
//
// Spec ref: specs/architecture.md §4.2 AR-005.
//
// AR-005 states: "Every normative requirement in every foundation and subsystem
// spec MUST carry a `Tags:` line with exactly one of `mechanism` or `cognition`.
// The two tags are mutually exclusive per template §4.N+1. A requirement
// describing both surfaces MUST split into two requirements."
//
// This test walks specs/*.md and specs/**/spec.md, identifies every normative
// requirement heading (#### PREFIX-NNN[LETTER]? — ...) excluding open-question
// headings (OQ-), and asserts that each requirement carries exactly one Tags:
// line whose value is exactly "mechanism" or "cognition".
//
// Three failure modes:
//   1. Missing — no Tags: line found before the next heading or within a 30-line
//      look-ahead window.
//   2. Invalid — Tags: line present but value is not "mechanism" or "cognition"
//      (e.g. "Tags: mechanism, cognition" violates mutual exclusion).
//   3. Duplicate — two or more Tags: lines found for the same requirement.
//
// The test reports ALL violations, not just the first, so the full failure
// surface is visible in a single run.

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

// ar005FixtureAnyHeading matches any #### level-4 requirement heading of the
// form "#### PREFIX-... — ...". Open-question headings are identified
// separately by ar005FixtureIsOpenQuestion and excluded from the normative
// set; Go's regexp package does not support negative lookahead.
var ar005FixtureAnyHeading = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?) —`)

// ar005FixtureAnySectionHeading matches any Markdown heading line (level 1–4).
// It is used to break the look-ahead window for Tags: scanning so that Tags:
// lines in a later requirement are not attributed to an earlier one.
var ar005FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ar005FixtureIsOpenQuestion reports whether a heading line is an open-question
// entry (OQ-PREFIX-NNN).  Open questions are advisory, not normative; they
// carry "Question:", "Owner:", etc. but have no Tags: obligation per AR-005.
var ar005FixtureIsOpenQuestion = regexp.MustCompile(`^#### OQ-`)

// ar005FixtureTagsLine matches a Tags: line and captures the value after
// "Tags: ".  Valid values are exactly "mechanism" or "cognition"; any other
// value (including "mechanism, cognition") is an AR-005 violation.
var ar005FixtureTagsLine = regexp.MustCompile(`^Tags: (.+)`)

// ar005FixtureViolation records a single AR-005 violation.
type ar005FixtureViolation struct {
	file    string
	lineNo  int
	heading string
	kind    string // "missing", "invalid", "duplicate"
	detail  string
}

func (v ar005FixtureViolation) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s — %s", v.file, v.lineNo, v.kind, v.heading, v.detail)
}

// ar005FixtureSpecFiles returns the set of spec files to audit.
//
// The scope is:
//   - specs/*.md    (top-level spec files)
//   - specs/**/spec.md (subsystem spec files nested one directory deep)
//
// _registry.yaml and schemas.md are intentionally excluded: the registry is
// YAML, and schemas.md is a supplement that carries RECORD definitions, not
// normative requirement headings.
func ar005FixtureSpecFiles(t *testing.T) []string {
	t.Helper()

	// Locate the repo root relative to this test file's source path.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar005FixtureSpecFiles: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar005_tags_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	specsDir := filepath.Join(repoRoot, "specs")

	var files []string

	// Top-level *.md files (excludes _registry.yaml by extension).
	topEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("ar005FixtureSpecFiles: ReadDir %s: %v", specsDir, err)
	}
	for _, e := range topEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		files = append(files, filepath.Join(specsDir, e.Name()))
	}

	// Nested spec.md files (one level deep — e.g. specs/reconciliation/spec.md).
	subEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("ar005FixtureSpecFiles: ReadDir(sub) %s: %v", specsDir, err)
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
		t.Fatalf("ar005FixtureSpecFiles: no spec files found under %s", specsDir)
	}
	return files
}

// ar005FixtureParseViolations parses one spec file and returns all AR-005
// violations found within it.
//
// The parser is line-oriented:
//  1. A normative heading line starts a requirement context.
//  2. Lines within the look-ahead window (until the next heading or EOF) are
//     scanned for Tags: lines.
//  3. After scanning, the requirement is checked for the three failure modes.
//
// Look-ahead is capped at 30 lines to avoid false positives from deeply nested
// blocks; the spec template places Tags: within a few lines of the requirement
// body.
func ar005FixtureParseViolations(t *testing.T, specFile string) []ar005FixtureViolation {
	t.Helper()

	//nolint:gosec // G304: path comes from ar005FixtureSpecFiles which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar005FixtureParseViolations: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	type reqContext struct {
		headingLineNo int
		heading       string
		tagLines      []string // raw "Tags: <value>" lines found in look-ahead
	}

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar005FixtureParseViolations: scan %s: %v", specFile, scanErr)
	}

	var reqs []reqContext

	for i, line := range lines {
		// Skip non-headings and open-question headings.
		if !ar005FixtureAnyHeading.MatchString(line) {
			continue
		}
		if ar005FixtureIsOpenQuestion.MatchString(line) {
			continue
		}
		// Extract the requirement ID from the heading for display purposes.
		heading := line
		ctx := reqContext{
			headingLineNo: i + 1, // 1-based
			heading:       heading,
		}

		// Scan forward up to 30 lines (or until the next heading) for Tags: lines.
		limit := i + 1 + 30
		if limit > len(lines) {
			limit = len(lines)
		}
		for j := i + 1; j < limit; j++ {
			if ar005FixtureAnySectionHeading.MatchString(lines[j]) {
				break
			}
			if m := ar005FixtureTagsLine.FindStringSubmatch(lines[j]); m != nil {
				ctx.tagLines = append(ctx.tagLines, strings.TrimSpace(m[1]))
			}
		}
		reqs = append(reqs, ctx)
	}

	// Relative path for readable error messages.
	relFile := specFile
	if idx := strings.Index(specFile, "/specs/"); idx >= 0 {
		relFile = "specs/" + specFile[idx+len("/specs/"):]
	}

	var violations []ar005FixtureViolation

	for _, req := range reqs {
		switch {
		case len(req.tagLines) == 0:
			violations = append(violations, ar005FixtureViolation{
				file:    relFile,
				lineNo:  req.headingLineNo,
				heading: req.heading,
				kind:    "missing",
				detail:  "no Tags: line found within 30-line look-ahead; add `Tags: mechanism` or `Tags: cognition`",
			})

		case len(req.tagLines) > 1:
			violations = append(violations, ar005FixtureViolation{
				file:    relFile,
				lineNo:  req.headingLineNo,
				heading: req.heading,
				kind:    "duplicate",
				detail:  fmt.Sprintf("%d Tags: lines found: %s; split into two requirements or keep exactly one", len(req.tagLines), strings.Join(req.tagLines, ", ")),
			})

		default:
			val := req.tagLines[0]
			if val != "mechanism" && val != "cognition" {
				violations = append(violations, ar005FixtureViolation{
					file:    relFile,
					lineNo:  req.headingLineNo,
					heading: req.heading,
					kind:    "invalid",
					detail:  fmt.Sprintf("Tags: %q is not a valid value; must be exactly `mechanism` or `cognition` (no commas, no other values)", val),
				})
			}
		}
	}

	return violations
}

// ar005FixtureExpectedViolation is a single entry in the expected-violations
// skip-list.  A requirement in this map is known to violate AR-005 and is
// covered by an in-flight bead.  The test logs the entry rather than failing,
// and errors if the entry is stale (violation no longer present).
type ar005FixtureExpectedViolation struct {
	// pinnedBy is the bead ID that owns the fix for this violation.
	pinnedBy string
	// reason is a human-readable explanation of why the dual-tag is substantive
	// and requires a spec-level split rather than a single-tag correction.
	reason string
}

// ar005FixtureExpectedViolations is the skip-list of known AR-005 violations
// that are intentionally deferred.
//
// Key format: "<relative-spec-path>:<line-number>:<requirement-id>".
// The line number MUST be the heading line of the requirement (1-based).
//
// Rules:
//   - An entry whose violation is NOT present in the current corpus causes
//     t.Errorf("stale skip-list entry …") — remove stale entries promptly.
//   - An entry whose violation IS present produces t.Logf and does NOT fail.
//   - Any NEW violation NOT in this map DOES fail the suite.
var ar005FixtureExpectedViolations = map[string]ar005FixtureExpectedViolation{}

// ar005FixtureViolationKey returns the skip-list lookup key for a violation.
// Format: "<file>:<lineNo>:<requirementID>" where requirementID is extracted
// from the heading via ar005FixtureReqID.
func ar005FixtureViolationKey(v ar005FixtureViolation) string {
	return fmt.Sprintf("%s:%d:%s", v.file, v.lineNo, ar005FixtureReqID(v.heading))
}

// ar005FixtureReqID extracts the requirement identifier (e.g. "RC-015") from a
// level-4 Markdown heading line of the form "#### RC-015 — ...".
var ar005FixtureReqIDPattern = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?)`)

func ar005FixtureReqID(heading string) string {
	if m := ar005FixtureReqIDPattern.FindStringSubmatch(heading); m != nil {
		return m[1]
	}
	return "UNKNOWN"
}

// TestAR005TagsMutualExclusion is the binding test for AR-005.
//
// It walks every spec file in scope, extracts all normative requirements, and
// asserts that each carries exactly one Tags: line with a valid value.
//
// Known violations that are covered by in-flight beads are listed in
// ar005FixtureExpectedViolations.  Those entries are logged (not failed) and
// produce an error if they become stale (violation no longer present).
func TestAR005TagsMutualExclusion(t *testing.T) {
	specFiles := ar005FixtureSpecFiles(t)

	var allViolations []ar005FixtureViolation
	for _, sf := range specFiles {
		violations := ar005FixtureParseViolations(t, sf)
		allViolations = append(allViolations, violations...)
	}

	// Build a set of violation keys found in the current corpus.
	foundKeys := make(map[string]ar005FixtureViolation, len(allViolations))
	for _, v := range allViolations {
		foundKeys[ar005FixtureViolationKey(v)] = v
	}

	// Check for stale skip-list entries (expected violations that no longer exist).
	for key, entry := range ar005FixtureExpectedViolations {
		if _, present := foundKeys[key]; !present {
			t.Errorf("AR-005 skip-list: stale entry %q (pinned by %s) — violation no longer present; remove from ar005FixtureExpectedViolations",
				key, entry.pinnedBy)
		}
	}

	// Separate violations into expected (pinned) and unexpected (new failures).
	var unexpected []ar005FixtureViolation
	for _, v := range allViolations {
		key := ar005FixtureViolationKey(v)
		if entry, pinned := ar005FixtureExpectedViolations[key]; pinned {
			t.Logf("AR-005 expected violation (pinned by %s): %s — %s", entry.pinnedBy, key, entry.reason)
			continue
		}
		unexpected = append(unexpected, v)
	}

	if len(unexpected) == 0 {
		t.Logf("AR-005 audit: all %d spec files pass — every normative requirement carries exactly one valid Tags: line (%d known violation(s) pinned to in-flight beads)",
			len(specFiles), len(ar005FixtureExpectedViolations))
		return
	}

	// Report ALL unexpected violations verbatim so the full failure surface is visible.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"AR-005 violation: %d NEW requirement(s) in the spec corpus do not satisfy the Tags: mutual-exclusion rule\n",
		len(unexpected),
	))
	sb.WriteString("(specs/architecture.md §4.2 AR-005: every normative requirement MUST carry exactly one of `Tags: mechanism` or `Tags: cognition`)\n\n")
	for _, v := range unexpected {
		sb.WriteString("  ")
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	t.Error(sb.String())
}
