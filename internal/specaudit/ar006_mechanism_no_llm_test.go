//go:build specaudit

package specaudit_test

// AR-006 binding test — mechanism-tagged requirements MUST NOT carry a non-zero
// llm-freedom axis value.
//
// Spec ref: specs/architecture.md §4.2 AR-006.
//
// AR-006 states: "A `mechanism`-tagged evaluation point MUST NOT invoke an LLM.
// A mechanism-tagged point whose behavior depends on semantic judgment (keyword
// matching for completion, heuristic fallback trees, regex parsing of unstructured
// output, hardcoded quality scoring) is a ZFC violation and MUST be refactored
// into a deterministic evaluator or a cognition-tagged delegation."
//
// This test enforces the mechanically-detectable subset of AR-006: the Axes-line
// contradiction.  When a normative requirement carries both `Tags: mechanism` and
// an `Axes:` line with `llm-freedom=bounded` or `llm-freedom=unbounded`, the
// spec author has declared that (a) the evaluation point is deterministic and
// MUST NOT invoke an LLM, and (b) the evaluation point uses an LLM.  That is a
// direct logical contradiction and is unconditionally a ZFC violation.
//
// Detection scope:
//   - Every normative requirement heading (#### PREFIX-NNN[LETTER]? — ...) in
//     specs/*.md and specs/**/spec.md, excluding open-question headings (OQ-).
//   - For each mechanism-tagged requirement, the test scans a 30-line look-ahead
//     window (until the next heading) for an Axes: line.
//   - A mechanism-tagged requirement is flagged when its Axes: line contains
//     llm-freedom=bounded or llm-freedom=unbounded.
//
// Limitation: this test cannot detect the body-text ZFC violations described in
// AR-006 (keyword matching, heuristic fallback trees, regex parsing of unstructured
// output, hardcoded quality scoring) because those require semantic judgment to
// distinguish normative-prohibitions ("MUST NOT do X") from normative-obligations
// ("does X"). That class of violation remains reviewer-enforced per §10.2.
//
// The test reports ALL violations (not just the first) so the full failure surface
// is visible in a single run.

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

// ar006FixtureAnyHeading matches any #### level-4 requirement heading of the
// form "#### PREFIX-... — ...". Open-question headings are identified separately
// by ar006FixtureIsOpenQuestion and excluded.
var ar006FixtureAnyHeading = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?) —`)

// ar006FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to break the look-ahead window for Tags: / Axes: scanning.
var ar006FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ar006FixtureIsOpenQuestion reports whether a heading line is an open-question
// entry (OQ-PREFIX-NNN). Open questions carry advisory prose only; they have
// no Tags: or Axes: obligation.
var ar006FixtureIsOpenQuestion = regexp.MustCompile(`^#### OQ-`)

// ar006FixtureTagsLine matches a Tags: line and captures the value.
var ar006FixtureTagsLine = regexp.MustCompile(`^Tags: (.+)`)

// ar006FixtureAxesLine matches a standalone Axes: line and captures the full
// semicolon-delimited value.
var ar006FixtureAxesLine = regexp.MustCompile(`^Axes: (.+)`)

// ar006FixtureLLMFreedomNonZero matches an llm-freedom token whose value is
// bounded or unbounded (both are "non-zero" — they indicate LLM involvement).
var ar006FixtureLLMFreedomNonZero = regexp.MustCompile(`\bllm-freedom=(bounded|unbounded)\b`)

// ar006FixtureViolation records a single AR-006 Axes-contradiction violation.
type ar006FixtureViolation struct {
	file          string
	headingLineNo int    // 1-based line number of the requirement heading
	axesLineNo    int    // 1-based line number of the Axes: line
	reqID         string // requirement ID from the heading (e.g. "CP-011a")
	llmFreedom    string // the non-zero llm-freedom value found (e.g. "bounded")
}

func (v ar006FixtureViolation) String() string {
	return fmt.Sprintf(
		"%s:%d (Axes: at line %d): [axes-contradiction] %s — mechanism-tagged requirement has llm-freedom=%s; mechanism-tagged points MUST have llm-freedom=none (AR-006)",
		v.file, v.headingLineNo, v.axesLineNo, v.reqID, v.llmFreedom,
	)
}

// ar006FixtureSpecFiles returns the set of spec files to audit.
//
// Scope mirrors ar005FixtureSpecFiles:
//   - specs/*.md    (top-level spec files)
//   - specs/**/spec.md (subsystem spec files nested one directory deep)
func ar006FixtureSpecFiles(t *testing.T) []string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar006FixtureSpecFiles: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar006_mechanism_no_llm_test.go
	// repo root is two directories up.
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	specsDir := filepath.Join(repoRoot, "specs")

	var files []string

	// Top-level *.md files.
	topEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("ar006FixtureSpecFiles: ReadDir %s: %v", specsDir, err)
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
		t.Fatalf("ar006FixtureSpecFiles: ReadDir(sub) %s: %v", specsDir, err)
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
		t.Fatalf("ar006FixtureSpecFiles: no spec files found under %s", specsDir)
	}
	return files
}

// ar006FixtureParseViolations parses one spec file and returns all AR-006
// Axes-contradiction violations found within it.
//
// Parser strategy:
//  1. Walk lines collecting normative requirement headings (excluding OQ-).
//  2. For each heading, scan a 30-line look-ahead window (stopping at the next
//     heading) to collect the Tags: value and any Axes: line.
//  3. If Tags: mechanism and the Axes: line has llm-freedom=bounded or
//     llm-freedom=unbounded, record a violation.
//
// A requirement with Tags: mechanism but no Axes: line in its look-ahead window
// is NOT flagged — the absence of an Axes: line is permitted (baseline axes per
// template §4.N+1) and carries no llm-freedom declaration.
func ar006FixtureParseViolations(t *testing.T, specFile string) []ar006FixtureViolation {
	t.Helper()

	//nolint:gosec // G304: path comes from ar006FixtureSpecFiles which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar006FixtureParseViolations: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar006FixtureParseViolations: scan %s: %v", specFile, scanErr)
	}

	// Relative path for readable error messages.
	relFile := specFile
	if idx := strings.Index(specFile, "/specs/"); idx >= 0 {
		relFile = "specs/" + specFile[idx+len("/specs/"):]
	}

	type reqContext struct {
		headingLineNo int    // 1-based
		reqID         string // e.g. "CP-011"
		tagsValue     string // "mechanism" or "cognition" (or "" if not yet found)
		tagsLineNo    int    // 1-based line number of the Tags: line (0 if not found)
		axesValue     string // raw Axes: value (or "" if not found)
		axesLineNo    int    // 1-based line number of the Axes: line (0 if not found)
	}

	var reqs []reqContext

	for i, line := range lines {
		if !ar006FixtureAnyHeading.MatchString(line) {
			continue
		}
		if ar006FixtureIsOpenQuestion.MatchString(line) {
			continue
		}
		m := ar006FixtureAnyHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ctx := reqContext{
			headingLineNo: i + 1, // 1-based
			reqID:         m[1],
		}

		// Scan forward up to 30 lines (or until the next heading) for Tags:/Axes:.
		limit := i + 1 + 30
		if limit > len(lines) {
			limit = len(lines)
		}
		for j := i + 1; j < limit; j++ {
			if ar006FixtureAnySectionHeading.MatchString(lines[j]) {
				break
			}
			if ctx.tagsValue == "" {
				if tm := ar006FixtureTagsLine.FindStringSubmatch(lines[j]); tm != nil {
					ctx.tagsValue = strings.TrimSpace(tm[1])
					ctx.tagsLineNo = j + 1 // 1-based
				}
			}
			if ctx.axesValue == "" {
				if am := ar006FixtureAxesLine.FindStringSubmatch(lines[j]); am != nil {
					ctx.axesValue = strings.TrimSpace(am[1])
					ctx.axesLineNo = j + 1 // 1-based
				}
			}
		}

		reqs = append(reqs, ctx)
	}

	var violations []ar006FixtureViolation

	for _, req := range reqs {
		// Only mechanism-tagged requirements are subject to AR-006.
		if req.tagsValue != "mechanism" {
			continue
		}
		// No Axes: line → no Axes-contradiction violation (baseline axes apply).
		if req.axesValue == "" {
			continue
		}
		// Check for llm-freedom=bounded or llm-freedom=unbounded.
		m := ar006FixtureLLMFreedomNonZero.FindStringSubmatch(req.axesValue)
		if m == nil {
			continue
		}
		violations = append(violations, ar006FixtureViolation{
			file:          relFile,
			headingLineNo: req.headingLineNo,
			axesLineNo:    req.axesLineNo,
			reqID:         req.reqID,
			llmFreedom:    m[1],
		})
	}

	return violations
}

// ar006FixtureExpectedViolation is a single entry in the expected-violations
// skip-list.  A requirement in this map is a known AR-006 violation covered by
// an in-flight bead.  The test logs it rather than failing, and errors if the
// entry becomes stale (violation no longer present).
type ar006FixtureExpectedViolation struct {
	// pinnedBy is the bead ID that owns the fix for this violation.
	pinnedBy string
	// reason is a human-readable explanation.
	reason string
}

// ar006FixtureExpectedViolations is the skip-list of known AR-006 violations
// that are intentionally deferred.
//
// Key format: "<relative-spec-path>:<heading-line-number>:<requirement-id>".
// The line number MUST be the 1-based heading line of the requirement.
//
// Rules (mirrors ar005FixtureExpectedViolations):
//   - A skip-list entry whose violation is NOT present causes t.Errorf("stale …").
//   - A skip-list entry whose violation IS present produces t.Logf (no failure).
//   - Any NEW violation NOT in this map DOES fail the suite.
var ar006FixtureExpectedViolations = map[string]ar006FixtureExpectedViolation{}

// ar006FixtureViolationKey returns the skip-list lookup key for a violation.
// Format: "<file>:<headingLineNo>:<reqID>".
func ar006FixtureViolationKey(v ar006FixtureViolation) string {
	return fmt.Sprintf("%s:%d:%s", v.file, v.headingLineNo, v.reqID)
}

// TestAR006MechanismNoLLM is the binding test for AR-006 (Axes-contradiction
// class).
//
// It walks every spec file in scope, extracts all normative requirements, and
// for each mechanism-tagged requirement that carries an Axes: line asserts that
// llm-freedom=none (i.e., the Axes: line does NOT contain llm-freedom=bounded
// or llm-freedom=unbounded).
//
// Known violations covered by in-flight beads are pinned in
// ar006FixtureExpectedViolations.  Those entries are logged (not failed) and
// produce an error if they become stale.
func TestAR006MechanismNoLLM(t *testing.T) {
	specFiles := ar006FixtureSpecFiles(t)

	var allViolations []ar006FixtureViolation
	for _, sf := range specFiles {
		vs := ar006FixtureParseViolations(t, sf)
		allViolations = append(allViolations, vs...)
	}

	// Build a set of violation keys found in the current corpus.
	foundKeys := make(map[string]ar006FixtureViolation, len(allViolations))
	for _, v := range allViolations {
		foundKeys[ar006FixtureViolationKey(v)] = v
	}

	// Check for stale skip-list entries (expected violations that no longer exist).
	for key, entry := range ar006FixtureExpectedViolations {
		if _, present := foundKeys[key]; !present {
			t.Errorf("AR-006 skip-list: stale entry %q (pinned by %s) — violation no longer present; remove from ar006FixtureExpectedViolations",
				key, entry.pinnedBy)
		}
	}

	// Separate violations into expected (pinned) and unexpected (new failures).
	var unexpected []ar006FixtureViolation
	for _, v := range allViolations {
		key := ar006FixtureViolationKey(v)
		if entry, pinned := ar006FixtureExpectedViolations[key]; pinned {
			t.Logf("AR-006 expected violation (pinned by %s): %s — %s", entry.pinnedBy, key, entry.reason)
			continue
		}
		unexpected = append(unexpected, v)
	}

	if len(unexpected) == 0 {
		t.Logf("AR-006 audit: all %d spec files pass — no mechanism-tagged requirement carries a non-zero llm-freedom axis value (%d known violation(s) pinned to in-flight beads)",
			len(specFiles), len(ar006FixtureExpectedViolations))
		return
	}

	// Report ALL unexpected violations so the full failure surface is visible.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"AR-006 violation: %d mechanism-tagged requirement(s) in the spec corpus carry a non-zero llm-freedom Axes: value\n",
		len(unexpected),
	))
	sb.WriteString("(specs/architecture.md §4.2 AR-006: a mechanism-tagged evaluation point MUST NOT invoke an LLM; " +
		"llm-freedom=(bounded|unbounded) in a mechanism-tagged requirement's Axes: line is a direct ZFC violation)\n")
	sb.WriteString("Remedy: either change the Tags: line to `Tags: cognition` and add the required delegation path (AR-007),\n")
	sb.WriteString("or change the Axes: llm-freedom value to `none` if the requirement truly invokes no LLM.\n\n")
	for _, v := range unexpected {
		sb.WriteString("  ")
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	t.Error(sb.String())
}
