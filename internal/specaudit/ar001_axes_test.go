package specaudit_test

// AR-001 binding test — every Axes: line in the spec corpus is well-formed.
//
// Spec ref: specs/architecture.md §4.1 AR-001.
//
// AR-001 states: every type, interface, and evaluation point in a cross-
// subsystem contract MUST be classifiable on four axes:
//
//	llm-freedom    ∈ {none, bounded, unbounded}
//	io-determinism ∈ {deterministic, best-effort, nondeterministic}
//	replay-safety  ∈ {safe, unsafe, n/a}
//	idempotency    ∈ {idempotent, non-idempotent, recoverable-non-idempotent, n/a}
//
// Scope: this test audits the VALIDATION half of AR-001 — when an Axes: line
// is present in a normative requirement block, it MUST declare all four axes
// with values drawn from the declared vocabulary.  Coverage detection (which
// requirements MUST carry Axes: but don't) is ambiguous to mechanize because
// it depends on runtime semantics; coverage is reviewer-enforced per §10.2.
//
// Three failure modes:
//  1. incomplete   — fewer than four axis keys found; at least one axis name
//     is missing from the Axes: line.
//  2. invalid-name — an axis key present in the Axes: line is not one of the
//     four canonical names; possible typo or copy-paste error.
//  3. invalid-value — an axis key is canonical but its value is not in the
//     declared vocabulary for that axis; e.g. `idempotency=safe` confuses
//     replay-safety values with idempotency values.
//
// The test reports ALL violations, not just the first, so the full failure
// surface is visible in a single run.
//
// Known violations covered by in-flight beads are pinned in
// ar001FixtureExpectedViolations (Path B skip-list pattern per AR-005 sibling).

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

// ar001FixtureAxisNames is the ordered set of canonical axis names.
var ar001FixtureAxisNames = []string{
	"llm-freedom",
	"io-determinism",
	"replay-safety",
	"idempotency",
}

// ar001FixtureAxisValues maps each canonical axis name to its valid value set.
var ar001FixtureAxisValues = map[string]map[string]bool{
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

// ar001FixtureAnyHeading matches any #### level-4 requirement heading of the
// form "#### PREFIX-... — ...".  Open-question headings are excluded separately.
var ar001FixtureAnyHeading = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?) —`)

// ar001FixtureAnySectionHeading matches any Markdown heading line (level 1–4).
// Used to break the look-ahead window for Axes: scanning.
var ar001FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ar001FixtureIsOpenQuestion reports whether a heading line is an open-question
// entry (OQ-PREFIX-NNN).  Open questions carry advisory prose only; they have
// no Axes: obligation.
var ar001FixtureIsOpenQuestion = regexp.MustCompile(`^#### OQ-`)

// ar001FixtureAxesLine matches a standalone Axes: line and captures everything
// after "Axes: ".  The full value is then parsed by ar001FixtureParseAxes.
var ar001FixtureAxesLine = regexp.MustCompile(`^Axes: (.+)`)

// ar001FixtureAxisPair matches one "key=value" token within a semicolon-
// delimited Axes: line value.
var ar001FixtureAxisPair = regexp.MustCompile(`^\s*([a-z-]+)\s*=\s*([a-z/+-]+)\s*$`)

// ar001FixtureViolation records a single AR-001 Axes: validation violation.
type ar001FixtureViolation struct {
	file    string
	lineNo  int    // 1-based line number of the Axes: line itself
	reqID   string // requirement heading ID (e.g. "BI-025a"), or "free-floating"
	kind    string // "incomplete", "invalid-name", "invalid-value"
	subject string // axis name being reported (e.g. "llm-freedom"); "" for "incomplete"
	detail  string
}

func (v ar001FixtureViolation) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s — %s", v.file, v.lineNo, v.kind, v.reqID, v.detail)
}

// ar001FixtureSpecFiles returns the set of spec files to audit.
//
// Scope mirrors ar005FixtureSpecFiles:
//   - specs/*.md    (top-level spec files)
//   - specs/**/spec.md (subsystem spec files nested one directory deep)
func ar001FixtureSpecFiles(t *testing.T) []string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar001FixtureSpecFiles: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar001_axes_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	specsDir := filepath.Join(repoRoot, "specs")

	var files []string

	// Top-level *.md files.
	topEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("ar001FixtureSpecFiles: ReadDir %s: %v", specsDir, err)
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
		t.Fatalf("ar001FixtureSpecFiles: ReadDir(sub) %s: %v", specsDir, err)
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
		t.Fatalf("ar001FixtureSpecFiles: no spec files found under %s", specsDir)
	}
	return files
}

// ar001FixtureParseAxes parses the value portion of an Axes: line (the part
// after "Axes: ") and validates each axis key and value.  It returns the set
// of violations found for that single Axes: line.
func ar001FixtureParseAxes(raw string, file string, axesLineNo int, reqID string) []ar001FixtureViolation {
	tokens := strings.Split(raw, ";")

	// Build a map of key → value from the parsed tokens.
	parsed := make(map[string]string, 4)
	var violations []ar001FixtureViolation

	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		m := ar001FixtureAxisPair.FindStringSubmatch(tok)
		if m == nil {
			violations = append(violations, ar001FixtureViolation{
				file:    file,
				lineNo:  axesLineNo,
				reqID:   reqID,
				kind:    "invalid-name",
				subject: tok,
				detail:  fmt.Sprintf("token %q is not a valid `key=value` pair; expected `axisname=value`", tok),
			})
			continue
		}
		key, val := m[1], m[2]
		parsed[key] = val

		// Check key is canonical.
		validVals, keyKnown := ar001FixtureAxisValues[key]
		if !keyKnown {
			violations = append(violations, ar001FixtureViolation{
				file:    file,
				lineNo:  axesLineNo,
				reqID:   reqID,
				kind:    "invalid-name",
				subject: key,
				detail: fmt.Sprintf("axis name %q is not canonical; valid names are: %s",
					key, strings.Join(ar001FixtureAxisNames, ", ")),
			})
			continue
		}

		// Check value is in the declared vocabulary for this axis.
		if !validVals[val] {
			var allowed []string
			for v := range validVals {
				allowed = append(allowed, v)
			}
			violations = append(violations, ar001FixtureViolation{
				file:    file,
				lineNo:  axesLineNo,
				reqID:   reqID,
				kind:    "invalid-value",
				subject: key,
				detail: fmt.Sprintf("axis %q has value %q which is not in the declared vocabulary {%s}",
					key, val, strings.Join(allowed, ", ")),
			})
		}
	}

	// Check completeness: all four axes must be present.
	var missingAxes []string
	for _, name := range ar001FixtureAxisNames {
		if _, ok := parsed[name]; !ok {
			missingAxes = append(missingAxes, name)
		}
	}
	if len(missingAxes) > 0 {
		violations = append(violations, ar001FixtureViolation{
			file:    file,
			lineNo:  axesLineNo,
			reqID:   reqID,
			kind:    "incomplete",
			subject: strings.Join(missingAxes, "+"),
			detail: fmt.Sprintf("Axes: line is missing axis name(s): %s; all four axes must be declared when Axes: is present",
				strings.Join(missingAxes, ", ")),
		})
	}

	return violations
}

// ar001FixtureParseViolations parses one spec file and returns all AR-001
// Axes: validation violations found within it.
//
// Parser strategy:
//  1. Walk lines tracking the current normative requirement heading (if any).
//  2. A level-4 heading matching the requirement pattern starts a new context;
//     open-question headings clear the context (they are not normative).
//  3. Any Axes: line found within the look-ahead window of a requirement
//     (before the next heading) is validated.
//  4. Axes: lines found outside any requirement context are also validated and
//     reported with reqID="free-floating".
//
// The look-ahead cap of 30 lines matches ar005; spec template places Axes:
// within a few lines of the requirement body.
func ar001FixtureParseViolations(t *testing.T, specFile string) []ar001FixtureViolation {
	t.Helper()

	//nolint:gosec // G304: path comes from ar001FixtureSpecFiles which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar001FixtureParseViolations: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar001FixtureParseViolations: scan %s: %v", specFile, scanErr)
	}

	// Relative path for readable error messages.
	relFile := specFile
	if idx := strings.Index(specFile, "/specs/"); idx >= 0 {
		relFile = "specs/" + specFile[idx+len("/specs/"):]
	}

	// Build a mapping from each Axes: line number to its owning requirement.
	// We do this via a forward pass: track the active requirement context and
	// attribute each Axes: line found within its 30-line look-ahead window.

	type reqContext struct {
		headingLineNo int    // 1-based
		reqID         string // e.g. "BI-025a"
	}

	// Collect all requirement headings in order.
	var reqs []reqContext
	for i, line := range lines {
		if !ar001FixtureAnyHeading.MatchString(line) {
			continue
		}
		if ar001FixtureIsOpenQuestion.MatchString(line) {
			continue
		}
		m := ar001FixtureAnyHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		reqs = append(reqs, reqContext{
			headingLineNo: i + 1, // 1-based
			reqID:         m[1],
		})
	}

	// For each Axes: line, determine which requirement context owns it.
	// An Axes: line at position j is owned by the latest requirement whose
	// heading precedes j AND whose look-ahead window [heading, heading+30)
	// contains j AND where no new heading appears between heading and j.
	// If no requirement owns it, the Axes: line is "free-floating".
	ownerFor := func(axesLineIdx int) string {
		// Find the most recent requirement that started before axesLineIdx.
		ownerReqID := "free-floating"
		for _, req := range reqs {
			hIdx := req.headingLineNo - 1 // 0-based
			if hIdx >= axesLineIdx {
				break
			}
			// Check the window: no new heading between hIdx+1 and axesLineIdx.
			windowEnd := hIdx + 1 + 30
			if windowEnd > len(lines) {
				windowEnd = len(lines)
			}
			if axesLineIdx >= windowEnd {
				continue
			}
			interrupted := false
			for k := hIdx + 1; k < axesLineIdx && k < windowEnd; k++ {
				if ar001FixtureAnySectionHeading.MatchString(lines[k]) {
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

	var violations []ar001FixtureViolation

	for i, line := range lines {
		m := ar001FixtureAxesLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		axesValue := strings.TrimSpace(m[1])
		axesLineNo := i + 1 // 1-based
		reqID := ownerFor(i)

		vs := ar001FixtureParseAxes(axesValue, relFile, axesLineNo, reqID)
		violations = append(violations, vs...)
	}

	return violations
}

// ar001FixtureExpectedViolation is a single entry in the expected-violations
// skip-list.  A requirement in this map is a known AR-001 Axes: defect that
// is covered by an in-flight bead.  The test logs the entry rather than
// failing, and errors if the entry becomes stale (violation no longer present).
type ar001FixtureExpectedViolation struct {
	// pinnedBy is the bead ID that owns the fix for this violation.
	pinnedBy string
	// reason is a human-readable explanation of the defect.
	reason string
}

// ar001FixtureExpectedViolations is the skip-list of known AR-001 Axes:
// violations that are intentionally deferred.
//
// Key format: "<relative-spec-path>:<axes-line-number>:<requirementID>:<kind>:<subject>".
// The line number MUST be the 1-based line number of the Axes: line itself.
// The subject is the axis name for invalid-name/invalid-value violations, or
// the missing axis names (joined by "+") for incomplete violations.
//
// Rules (mirrors ar005FixtureExpectedViolations):
//   - An entry whose violation is NOT present causes t.Errorf("stale skip-list entry …").
//   - An entry whose violation IS present produces t.Logf and does NOT fail.
//   - Any NEW violation NOT in this map DOES fail the suite.
var ar001FixtureExpectedViolations = map[string]ar001FixtureExpectedViolation{
	// BI-025a: `idempotency=safe` is invalid; "safe" is a replay-safety value.
	// Likely a copy-paste error where the replay-safety value was duplicated
	// into the idempotency slot.  Spec fix tracked in hk-zs0.59.
	"specs/beads-integration.md:335:BI-025a:invalid-value:idempotency": {
		pinnedBy: "hk-zs0.59",
		reason:   "BI-025a Axes: has `idempotency=safe`; `safe` is not a valid idempotency value (valid: idempotent, non-idempotent, recoverable-non-idempotent, n/a); requires spec correction",
	},
	// BI-025b: same defect pattern as BI-025a.
	"specs/beads-integration.md:342:BI-025b:invalid-value:idempotency": {
		pinnedBy: "hk-zs0.59",
		reason:   "BI-025b Axes: has `idempotency=safe`; same defect as BI-025a; requires spec correction",
	},
	// BI-025c: same defect pattern as BI-025a.
	"specs/beads-integration.md:351:BI-025c:invalid-value:idempotency": {
		pinnedBy: "hk-zs0.59",
		reason:   "BI-025c Axes: has `idempotency=safe`; same defect as BI-025a; requires spec correction",
	},
	// RC-015: `llm-freedom=delegated` is not a valid llm-freedom value (valid: none, bounded, unbounded).
	// RC-015 is also covered by the AR-005 skip-list (hk-zs0.58) for a Tags: dual-tag violation.
	// The Axes: defects are distinct from the Tags: defect and tracked separately.
	"specs/reconciliation/spec.md:465:RC-015:invalid-value:llm-freedom": {
		pinnedBy: "hk-zs0.60",
		reason:   "RC-015 Axes: has `llm-freedom=delegated`; `delegated` is not a valid llm-freedom value (valid: none, bounded, unbounded); requires spec correction",
	},
	// RC-015: `io-determinism=non-deterministic` uses a hyphenated form; the canonical token is `nondeterministic` (no hyphen).
	"specs/reconciliation/spec.md:465:RC-015:invalid-value:io-determinism": {
		pinnedBy: "hk-zs0.60",
		reason:   "RC-015 Axes: has `io-determinism=non-deterministic`; the canonical token is `nondeterministic` (no hyphen); requires spec correction",
	},
}

// ar001FixtureViolationKey returns the skip-list lookup key for a violation.
// Format: "<file>:<axesLineNo>:<reqID>:<kind>:<subject>".
// The subject disambiguates multiple violations of the same kind on the same
// Axes: line (e.g. two invalid-value violations for different axis names).
func ar001FixtureViolationKey(v ar001FixtureViolation) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", v.file, v.lineNo, v.reqID, v.kind, v.subject)
}

// TestAR001AxesValidation is the binding test for AR-001 Axes: line
// well-formedness.
//
// It walks every spec file in scope, finds every Axes: line, attributes it
// to its owning normative requirement (or marks it "free-floating"), and
// validates that the four-axis tuple is complete and uses valid vocabulary.
//
// Known violations covered by in-flight beads are pinned in
// ar001FixtureExpectedViolations.  Those entries are logged (not failed) and
// produce an error if they become stale.
func TestAR001AxesValidation(t *testing.T) {
	specFiles := ar001FixtureSpecFiles(t)

	var allViolations []ar001FixtureViolation
	for _, sf := range specFiles {
		vs := ar001FixtureParseViolations(t, sf)
		allViolations = append(allViolations, vs...)
	}

	// Build a set of violation keys found in the current corpus.
	foundKeys := make(map[string]ar001FixtureViolation, len(allViolations))
	for _, v := range allViolations {
		foundKeys[ar001FixtureViolationKey(v)] = v
	}

	// Check for stale skip-list entries (pinned violations that no longer exist).
	for key, entry := range ar001FixtureExpectedViolations {
		if _, present := foundKeys[key]; !present {
			t.Errorf("AR-001 skip-list: stale entry %q (pinned by %s) — violation no longer present; remove from ar001FixtureExpectedViolations",
				key, entry.pinnedBy)
		}
	}

	// Separate violations into expected (pinned) and unexpected (new failures).
	var unexpected []ar001FixtureViolation
	for _, v := range allViolations {
		key := ar001FixtureViolationKey(v)
		if entry, pinned := ar001FixtureExpectedViolations[key]; pinned {
			t.Logf("AR-001 expected violation (pinned by %s): %s — %s", entry.pinnedBy, key, entry.reason)
			continue
		}
		unexpected = append(unexpected, v)
	}

	if len(unexpected) == 0 {
		t.Logf("AR-001 audit: all %d spec files pass — every Axes: line is well-formed (%d known violation(s) pinned to in-flight beads)",
			len(specFiles), len(ar001FixtureExpectedViolations))
		return
	}

	// Report ALL unexpected violations so the full failure surface is visible.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"AR-001 violation: %d Axes: line(s) in the spec corpus are malformed\n",
		len(unexpected),
	))
	sb.WriteString("(specs/architecture.md §4.1 AR-001: when Axes: is present it MUST declare all four axes with valid vocabulary)\n\n")
	sb.WriteString("Valid axis names and values:\n")
	for _, name := range ar001FixtureAxisNames {
		var vals []string
		for v := range ar001FixtureAxisValues[name] {
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
