package specaudit_test

// AR-002 binding test — baseline axis values are the default.
//
// Spec ref: specs/architecture.md §4.1 AR-002.
//
// AR-002 states: the baseline axis tuple is
//
//	llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
//
// A requirement that matches baseline on every axis MAY omit the Axes: line;
// reviewers infer baseline from absence.  A requirement that deviates on any
// axis MUST declare the full four-axis tuple.
//
// # What this test audits
//
// This test performs two complementary checks against the full spec corpus
// (specs/*.md and specs/**/spec.md one directory deep):
//
//	Check A (redundant-baseline): When an Axes: line is present and its tuple
//	exactly matches the baseline, the line is syntactically valid (the spec says
//	MAY omit, not MUST omit) but verbose.  Such lines are collected and reported
//	as informational noise — they do NOT fail the suite.  A high count of
//	redundant lines is design evidence that the spec authoring guidelines should
//	encourage baseline omission.
//
//	Check B (deviation-completeness): When an Axes: line is present and its
//	tuple does NOT exactly match the baseline, the AR-002 MUST clause applies:
//	the deviation MUST declare the full four-axis tuple.  A deviation tuple that
//	omits one or more of the four canonical axes is an AR-002 violation.  This
//	check overlaps with AR-001's completeness check but is framed from AR-002's
//	perspective: incomplete deviations are particularly problematic because the
//	reader cannot determine whether the missing axes are baseline or deviant.
//
// The test does NOT check whether requirements that omit Axes: should have an
// explicit line — that would require runtime semantic judgment and is
// reviewer-enforced per §10.2.
//
// # Failure modes
//
//  1. deviation-incomplete — a non-baseline Axes: tuple is missing one or more
//     canonical axis keys.
//  2. deviation-invalid-axis — a non-baseline Axes: tuple contains an axis key
//     or value that is not in the canonical vocabulary (the grammar problem;
//     catches cases AR-001 would also flag but reported from AR-002's framing).
//
// # Known violations
//
// Known violations covered by in-flight beads are pinned in
// zs04FixtureExpectedViolations (Path B skip-list pattern per the AR-001 /
// AR-005 / AR-032 / AR-041 sibling discipline).
//
// # Stale-entry guard
//
// If a pinned violation no longer exists in the corpus, the test fails with a
// "stale skip-list entry" error so the skip-list cannot silently rot.

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

// zs04FixtureBaseline is the canonical baseline axis tuple.  All four values
// must match for an Axes: line to be classified as "redundant baseline".
var zs04FixtureBaseline = map[string]string{
	"llm-freedom":    "none",
	"io-determinism": "deterministic",
	"replay-safety":  "safe",
	"idempotency":    "idempotent",
}

// zs04FixtureAxisNames is the ordered set of canonical axis names.  The order
// is used in error messages to produce stable, human-readable output.
var zs04FixtureAxisNames = []string{
	"llm-freedom",
	"io-determinism",
	"replay-safety",
	"idempotency",
}

// zs04FixtureAxisValues is the valid vocabulary for each axis.  It mirrors the
// vocabulary in ar001FixtureAxisValues; it is redeclared here so this test
// file is self-contained and does not couple to the AR-001 sibling.
var zs04FixtureAxisValues = map[string]map[string]bool{
	"llm-freedom": {
		"none":      true,
		"bounded":   true,
		"unbounded": true,
	},
	"io-determinism": {
		"deterministic":    true,
		"best-effort":      true,
		"nondeterministic": true,
	},
	"replay-safety": {
		"safe":   true,
		"unsafe": true,
		"n/a":    true,
	},
	"idempotency": {
		"idempotent":                 true,
		"non-idempotent":             true,
		"recoverable-non-idempotent": true,
		"n/a":                        true,
	},
}

// zs04FixtureAnyHeading matches any #### level-4 requirement heading of the
// form "#### PREFIX-... — ...".  Open-question headings are excluded separately.
var zs04FixtureAnyHeading = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?) —`)

// zs04FixtureAnySectionHeading matches any Markdown heading line (level 1–4).
// Used to break the look-ahead window for Axes: scanning.
var zs04FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// zs04FixtureIsOpenQuestion reports whether a heading line is an open-question
// entry (OQ-PREFIX-NNN).  Open questions are advisory; they carry no Axes:
// obligation.
var zs04FixtureIsOpenQuestion = regexp.MustCompile(`^#### OQ-`)

// zs04FixtureAxesLine matches a standalone Axes: line and captures everything
// after "Axes: ".
var zs04FixtureAxesLine = regexp.MustCompile(`^Axes: (.+)`)

// zs04FixtureAxisPair matches one "key=value" token within a semicolon-
// delimited Axes: line value.
var zs04FixtureAxisPair = regexp.MustCompile(`^\s*([a-z-]+)\s*=\s*([a-z/+-]+)\s*$`)

// zs04FixtureViolation records a single AR-002 violation found by the audit.
type zs04FixtureViolation struct {
	file    string
	lineNo  int    // 1-based line number of the Axes: line itself
	reqID   string // owning requirement ID (e.g. "BI-025a") or "free-floating"
	kind    string // "deviation-incomplete" or "deviation-invalid-axis"
	subject string // axis name being reported; "" for "deviation-incomplete" with no specific axis
	detail  string
}

func (v zs04FixtureViolation) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s — %s", v.file, v.lineNo, v.kind, v.reqID, v.detail)
}

// zs04FixtureRedundantEntry records an Axes: line that exactly matches the
// baseline and is therefore noise (valid but verbose per AR-002's MAY rule).
type zs04FixtureRedundantEntry struct {
	file   string
	lineNo int
	reqID  string
}

func (r zs04FixtureRedundantEntry) String() string {
	return fmt.Sprintf("%s:%d: [redundant-baseline] %s", r.file, r.lineNo, r.reqID)
}

// zs04FixtureSpecFiles returns the set of spec files to audit.
//
// Scope mirrors ar001FixtureSpecFiles and ar041FixtureSpecFiles:
//   - specs/*.md    (top-level spec files)
//   - specs/**/spec.md (subsystem spec files nested one directory deep)
func zs04FixtureSpecFiles(t *testing.T) []string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("zs04FixtureSpecFiles: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/zs04_baseline_axis_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	specsDir := filepath.Join(repoRoot, "specs")

	var files []string

	// Top-level *.md files.
	topEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("zs04FixtureSpecFiles: ReadDir %s: %v", specsDir, err)
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
		t.Fatalf("zs04FixtureSpecFiles: ReadDir(sub) %s: %v", specsDir, err)
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
		t.Fatalf("zs04FixtureSpecFiles: no spec files found under %s", specsDir)
	}
	return files
}

// zs04FixtureParseAxes parses the value portion of an Axes: line (the part
// after "Axes: ") and returns:
//   - the parsed key→value map
//   - whether the parsed tuple exactly matches the baseline
//   - any violations (deviation-incomplete or deviation-invalid-axis)
//
// The returned violations are only for non-baseline deviations; baseline-
// matching tuples never produce violations regardless of grammar (since AR-001
// already covers grammar and this test focuses on the deviation-completeness
// MUST rule from AR-002).
//
// However, we do validate grammar on non-baseline tuples so this test is
// self-sufficient for AR-002's MUST clause: "deviation requires the full
// four-axis tuple".
func zs04FixtureParseAxes(
	raw string,
	file string,
	axesLineNo int,
	reqID string,
) (parsed map[string]string, isBaseline bool, violations []zs04FixtureViolation) {
	tokens := strings.Split(raw, ";")
	parsed = make(map[string]string, 4)

	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		m := zs04FixtureAxisPair.FindStringSubmatch(tok)
		if m == nil {
			// Unparseable token — defer grammar check to AR-001; skip here to
			// avoid double-counting.
			continue
		}
		key, val := m[1], m[2]
		parsed[key] = val
	}

	// Check whether the parsed tuple exactly matches the baseline.
	// A tuple matches baseline iff all four baseline keys are present with
	// baseline values and no extra keys are present.
	matchesBaseline := len(parsed) == len(zs04FixtureBaseline)
	if matchesBaseline {
		for k, bv := range zs04FixtureBaseline {
			if parsed[k] != bv {
				matchesBaseline = false
				break
			}
		}
	}

	if matchesBaseline {
		return parsed, true, nil
	}

	// Non-baseline tuple: apply AR-002's MUST clause.
	// 1. Check completeness: all four axes must be declared.
	var missingAxes []string
	for _, name := range zs04FixtureAxisNames {
		if _, ok := parsed[name]; !ok {
			missingAxes = append(missingAxes, name)
		}
	}
	if len(missingAxes) > 0 {
		violations = append(violations, zs04FixtureViolation{
			file:    file,
			lineNo:  axesLineNo,
			reqID:   reqID,
			kind:    "deviation-incomplete",
			subject: strings.Join(missingAxes, "+"),
			detail: fmt.Sprintf(
				"Axes: line deviates from baseline but is missing axis name(s): %s; "+
					"AR-002 MUST: a deviation MUST declare the full four-axis tuple",
				strings.Join(missingAxes, ", "),
			),
		})
	}

	// 2. Check value validity for each present axis.
	for _, name := range zs04FixtureAxisNames {
		val, present := parsed[name]
		if !present {
			continue // already reported as missing above
		}
		validVals, keyKnown := zs04FixtureAxisValues[name]
		if !keyKnown {
			violations = append(violations, zs04FixtureViolation{
				file:    file,
				lineNo:  axesLineNo,
				reqID:   reqID,
				kind:    "deviation-invalid-axis",
				subject: name,
				detail: fmt.Sprintf(
					"axis name %q in deviation tuple is not canonical; valid names are: %s",
					name, strings.Join(zs04FixtureAxisNames, ", "),
				),
			})
			continue
		}
		if !validVals[val] {
			var allowed []string
			for v := range validVals {
				allowed = append(allowed, v)
			}
			violations = append(violations, zs04FixtureViolation{
				file:    file,
				lineNo:  axesLineNo,
				reqID:   reqID,
				kind:    "deviation-invalid-axis",
				subject: name,
				detail: fmt.Sprintf(
					"axis %q has value %q in deviation tuple; valid values for this axis are: {%s}",
					name, val, strings.Join(allowed, ", "),
				),
			})
		}
	}

	return parsed, false, violations
}

// zs04FixtureScanFile parses one spec file and returns:
//   - all AR-002 violations (deviation-incomplete or deviation-invalid-axis)
//   - all redundant-baseline entries (Axes: lines that exactly match baseline)
//
// Parser strategy mirrors ar001FixtureParseViolations:
//  1. Walk lines tracking the current normative requirement heading.
//  2. A level-4 heading matching the requirement pattern starts a new context;
//     open-question headings clear the context.
//  3. Any Axes: line found within the 30-line look-ahead window of a
//     requirement is classified as either baseline-redundant or
//     deviation-requiring-completeness.
//  4. Axes: lines outside any requirement context are attributed to
//     "free-floating" for violation reporting, but are NOT counted as
//     redundant-baseline entries (free-floating lines have no requirement
//     ownership context to reason about).
func zs04FixtureScanFile(
	t *testing.T,
	specFile string,
) (violations []zs04FixtureViolation, redundant []zs04FixtureRedundantEntry) {
	t.Helper()

	//nolint:gosec // G304: path comes from zs04FixtureSpecFiles which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("zs04FixtureScanFile: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("zs04FixtureScanFile: scan %s: %v", specFile, scanErr)
	}

	// Relative path for readable error messages.
	relFile := specFile
	if idx := strings.Index(specFile, "/specs/"); idx >= 0 {
		relFile = "specs/" + specFile[idx+len("/specs/"):]
	}

	// Collect all normative requirement headings in order.
	type reqContext struct {
		headingLineNo int
		reqID         string
	}
	var reqs []reqContext
	for i, line := range lines {
		if !zs04FixtureAnyHeading.MatchString(line) {
			continue
		}
		if zs04FixtureIsOpenQuestion.MatchString(line) {
			continue
		}
		m := zs04FixtureAnyHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		reqs = append(reqs, reqContext{
			headingLineNo: i + 1,
			reqID:         m[1],
		})
	}

	// ownerFor returns the requirement ID owning the line at 0-based index
	// lineIdx, or "free-floating" if no requirement window covers it.
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
			// No new heading between hIdx+1 and lineIdx.
			interrupted := false
			for k := hIdx + 1; k < lineIdx && k < windowEnd; k++ {
				if zs04FixtureAnySectionHeading.MatchString(lines[k]) {
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

	for i, line := range lines {
		m := zs04FixtureAxesLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		axesValue := strings.TrimSpace(m[1])
		axesLineNo := i + 1
		reqID := ownerFor(i)

		_, isBaseline, vs := zs04FixtureParseAxes(axesValue, relFile, axesLineNo, reqID)

		if isBaseline && reqID != "free-floating" {
			// Baseline-matching Axes: under a known requirement — redundant noise.
			redundant = append(redundant, zs04FixtureRedundantEntry{
				file:   relFile,
				lineNo: axesLineNo,
				reqID:  reqID,
			})
		}

		violations = append(violations, vs...)
	}

	return violations, redundant
}

// zs04FixtureExpectedViolationEntry is a single entry in the expected-
// violations skip-list.  A requirement in this map is a known AR-002 deviation-
// completeness defect covered by an in-flight bead.
type zs04FixtureExpectedViolationEntry struct {
	// pinnedBy is the bead ID that owns the fix for this violation.
	pinnedBy string
	// reason is a human-readable explanation of the defect.
	reason string
}

// zs04FixtureExpectedViolations is the skip-list of known AR-002 violations
// that are intentionally deferred to follow-up beads.
//
// Key format: "<relative-spec-path>:<axes-line-number>:<requirementID>:<kind>:<subject>".
// The line number MUST be the 1-based line number of the Axes: line itself.
//
// Rules (mirrors ar001FixtureExpectedViolations):
//   - An entry whose violation is NOT present causes t.Errorf("stale skip-list entry …").
//   - An entry whose violation IS present produces t.Logf and does NOT fail.
//   - Any NEW violation NOT in this map DOES fail the suite.
//
// AR-002 deviation-completeness violations are a strict subset of AR-001
// Axes: well-formedness violations: every violation here is also in
// ar001FixtureExpectedViolations (AR-001 catches grammar AND completeness;
// AR-002 catches deviation-completeness specifically).  Keeping the skip-lists
// in sync ensures both tests stay current with the spec corpus.
var zs04FixtureExpectedViolations = map[string]zs04FixtureExpectedViolationEntry{}

// zs04FixtureViolationKey returns the skip-list lookup key for a violation.
// Format: "<file>:<axesLineNo>:<reqID>:<kind>:<subject>".
func zs04FixtureViolationKey(v zs04FixtureViolation) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", v.file, v.lineNo, v.reqID, v.kind, v.subject)
}

// TestAR002BaselineAxisDefault is the binding test for AR-002.
//
// It walks every spec file in scope and performs two checks:
//
//	Check A (redundant-baseline): collects Axes: lines that exactly match the
//	baseline tuple.  These are logged as informational — they are valid per the
//	AR-002 MAY rule but verbose.  A high count is design evidence that the spec
//	authoring guidelines should encourage baseline omission.
//
//	Check B (deviation-completeness): finds Axes: lines that deviate from
//	baseline and verifies they declare the full four-axis tuple with valid
//	vocabulary.  An incomplete or invalid-vocabulary deviation is a hard failure
//	(AR-002 MUST clause: "A requirement that deviates on any axis MUST declare
//	the full four-axis tuple.").
//
// Known violations covered by in-flight beads are listed in
// zs04FixtureExpectedViolations.  Those entries are logged (not failed) and
// produce an error if they become stale (violation no longer present in the
// corpus).
func TestAR002BaselineAxisDefault(t *testing.T) {
	specFiles := zs04FixtureSpecFiles(t)

	var allViolations []zs04FixtureViolation
	var allRedundant []zs04FixtureRedundantEntry

	for _, sf := range specFiles {
		vs, rs := zs04FixtureScanFile(t, sf)
		allViolations = append(allViolations, vs...)
		allRedundant = append(allRedundant, rs...)
	}

	// ── Stale-entry guard ────────────────────────────────────────────────────
	// Build a set of violation keys found in the current corpus.
	foundKeys := make(map[string]zs04FixtureViolation, len(allViolations))
	for _, v := range allViolations {
		foundKeys[zs04FixtureViolationKey(v)] = v
	}

	for key, entry := range zs04FixtureExpectedViolations {
		if _, present := foundKeys[key]; !present {
			t.Errorf(
				"AR-002 skip-list: stale entry %q (pinned by %s) — violation no longer present; "+
					"remove from zs04FixtureExpectedViolations",
				key, entry.pinnedBy,
			)
		}
	}

	// ── Check A: redundant-baseline report ───────────────────────────────────
	// These are valid (MAY omit, not MUST omit) but verbose.  Log them so the
	// count is visible in CI output and can be tracked over time.
	for _, r := range allRedundant {
		t.Logf("AR-002 redundant-baseline (valid but verbose — MAY omit per AR-002): %s", r.String())
	}

	// ── Check B: deviation-completeness failures ─────────────────────────────
	var unexpected []zs04FixtureViolation
	for _, v := range allViolations {
		key := zs04FixtureViolationKey(v)
		if entry, pinned := zs04FixtureExpectedViolations[key]; pinned {
			t.Logf("AR-002 expected violation (pinned by %s): %s — %s",
				entry.pinnedBy, key, entry.reason)
			continue
		}
		unexpected = append(unexpected, v)
	}

	if len(unexpected) == 0 {
		t.Logf(
			"AR-002 audit: %d spec file(s) scanned — "+
				"check-A: %d redundant-baseline Axes: line(s) (valid but verbose); "+
				"check-B: %d known violation(s) pinned to in-flight beads; "+
				"no new deviation-completeness violations",
			len(specFiles),
			len(allRedundant),
			len(zs04FixtureExpectedViolations),
		)
		return
	}

	// Report ALL unexpected violations so the full failure surface is visible.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"AR-002 violation: %d Axes: line(s) deviate from baseline but are incomplete or use invalid vocabulary\n",
		len(unexpected),
	))
	sb.WriteString("(specs/architecture.md §4.1 AR-002: a deviation MUST declare the full four-axis tuple)\n\n")
	sb.WriteString("Baseline tuple:\n")
	for _, name := range zs04FixtureAxisNames {
		sb.WriteString(fmt.Sprintf("  %s=%s\n", name, zs04FixtureBaseline[name]))
	}
	sb.WriteString("\nValid axis values:\n")
	for _, name := range zs04FixtureAxisNames {
		var vals []string
		for v := range zs04FixtureAxisValues[name] {
			vals = append(vals, v)
		}
		sb.WriteString(fmt.Sprintf("  %s: {%s}\n", name, strings.Join(vals, ", ")))
	}
	sb.WriteString("\n")
	for _, v := range unexpected {
		sb.WriteString("  ")
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	t.Error(sb.String())
}
