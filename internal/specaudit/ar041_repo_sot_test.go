//go:build specaudit

package specaudit_test

// AR-041 binding test — no normative artifact depends on external (out-of-repo) sources.
//
// Spec ref: specs/architecture.md §4.9 AR-041.
//
// AR-041 states: "Every normative artifact — specs, policies, workflow DOT
// files, skill registries, conventions — MUST live in the repository.
// External wikis, out-of-band knowledge bases, and tribal-knowledge channels
// MUST NOT be load-bearing for any spec's conformance."
//
// # Audit frame (combined Option A + Option C)
//
// Option A — URL audit: scan normative requirement bodies for external URLs
// (http:// or https://).  An external URL inside a normative requirement body
// is a candidate AR-041 violation: it may indicate that conformance depends
// on an out-of-repo artifact.  Known-good library-citation URLs (e.g. a Go
// module's GitHub page cited for attribution, where the actual normative
// artifact is the in-repo go.mod entry) are pinned in
// ar041FixtureExpectedViolations.  New unknown URLs fail the suite.
//
// Option C — prohibited-channel audit: scan normative requirement bodies for
// explicit references to prohibited external knowledge channels: "Slack",
// "Linear", "external wiki", "tribal knowledge", "out-of-band knowledge
// base".  A match inside a normative body that is load-bearing for conformance
// is an AR-041 violation.  Informative lines ("> ...") and revision-history
// table rows ("| ...") are excluded from both audits.
//
// # Exclusion rules (both options)
//
//   - Lines that begin with ">" are INFORMATIVE; they are advisory and carry
//     no normative conformance obligation.
//   - Lines that begin with "|" are revision-history table rows; they are
//     retrospective prose, not normative requirements.
//   - Lines inside fenced code blocks (``` ... ```) are excluded; they are
//     implementation illustrations, not normative prose.
//   - The look-ahead window is capped at 30 lines per heading (matching the
//     AR-001 / AR-005 / AR-032 sibling discipline).
//
// # Failure mode
//
// A single failure mode: a disallowed external URL or a prohibited-channel
// name is found inside a normative requirement body and is not listed in
// ar041FixtureExpectedViolations.
//
// Known violations that genuinely require a spec-level fix (outside this
// bead's scope) are pinned in ar041FixtureExpectedViolations with a follow-up
// bead reference (Path B per HANDOFF.md scar #2).
//
// # Helper prefix
//
// All package-level identifiers in this file use the ar041Fixture prefix per
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

// ar041FixtureAnyHeading matches any #### level-4 requirement heading of the
// form "#### PREFIX-... — ...". Open-question headings are identified
// separately by ar041FixtureIsOpenQuestion and excluded from the normative
// set; Go's regexp package does not support negative lookahead.
var ar041FixtureAnyHeading = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?) —`)

// ar041FixtureAnySectionHeading matches any Markdown heading line (level 1–4).
// It is used to break the look-ahead window so content in a later requirement
// is not attributed to an earlier one.
var ar041FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// ar041FixtureIsOpenQuestion reports whether a heading line is an open-question
// entry (OQ-PREFIX-NNN). Open questions are advisory and carry no conformance
// obligation under AR-041.
var ar041FixtureIsOpenQuestion = regexp.MustCompile(`^#### OQ-`)

// ar041FixtureIsInformative reports whether a line is an INFORMATIVE block
// (Markdown blockquote beginning with ">"). INFORMATIVE text is advisory;
// content on these lines is excluded from both Option A and Option C audits.
var ar041FixtureIsInformative = regexp.MustCompile(`^>`)

// ar041FixtureIsRevisionRow reports whether a line is a revision-history table
// row (beginning with "|"). Revision rows are retrospective prose and are
// excluded from both audits even when they fall inside a look-ahead window.
var ar041FixtureIsRevisionRow = regexp.MustCompile(`^\|`)

// ar041FixtureFenceMarker matches a fenced code block delimiter line
// (``` possibly followed by a language identifier).
var ar041FixtureFenceMarker = regexp.MustCompile("^```")

// ar041FixtureExternalURL matches an external URL beginning with http:// or
// https://.  It captures the full URL up to the first whitespace, angle
// bracket, backtick, or closing parenthesis.
// Capture group 1: the URL.
var ar041FixtureExternalURL = regexp.MustCompile("(https?://[^\\s`>\\)\\]]+)")

// ar041FixtureProhibitedChannels is the set of prohibited external-channel
// names searched by the Option C audit.  A match inside a normative body is
// a candidate AR-041 violation.
//
// The vocabulary is restricted to the four AR-041-prohibited *category* names
// ("external wikis", "out-of-band knowledge bases", "tribal-knowledge
// channels"). Concrete tool names (Slack, Linear, Notion, Confluence, Jira,
// etc.) are deliberately omitted: when such names appear in the spec corpus,
// they typically sit in operator-discretion parentheticals (e.g., ON-005a
// lists Slack as one possible source for an operator-supplied hash) rather
// than as conformance dependencies. Adding them as audit terms would over-fire
// on legitimate operator-guidance prose. A future bead may revisit this if
// false-negative coverage becomes a real concern.
//
// Note: "out-of-band" on its own is a common technical phrase in protocol
// specs (e.g. "out-of-band replay", "out-of-band signal") and is NOT included
// here — the audit targets the AR-041-specific phrase pattern only.
var ar041FixtureProhibitedChannels = []string{
	"external wiki",
	"tribal knowledge",
	"tribal-knowledge",
	"out-of-band knowledge base",
}

// ar041FixtureViolation records a single AR-041 violation.
type ar041FixtureViolation struct {
	file    string
	lineNo  int    // 1-based line number of the violating line
	reqID   string // owning requirement ID, or "free-floating"
	kind    string // "external-url" or "prohibited-channel"
	subject string // the URL or prohibited phrase found
	detail  string
}

func (v ar041FixtureViolation) String() string {
	return fmt.Sprintf("%s:%d: [%s] %s — subject=%q — %s",
		v.file, v.lineNo, v.kind, v.reqID, v.subject, v.detail)
}

// ar041FixtureSpecFiles returns the set of spec files to audit.
//
// Scope mirrors the AR-001 / AR-005 / AR-032 siblings:
//   - specs/*.md    (top-level spec files)
//   - specs/**/spec.md (subsystem spec files nested one directory deep)
func ar041FixtureSpecFiles(t *testing.T) []string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar041FixtureSpecFiles: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar041_repo_sot_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	specsDir := filepath.Join(repoRoot, "specs")

	var files []string

	// Top-level *.md files (excludes _registry.yaml by extension).
	topEntries, err := os.ReadDir(specsDir)
	if err != nil {
		t.Fatalf("ar041FixtureSpecFiles: ReadDir %s: %v", specsDir, err)
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
		t.Fatalf("ar041FixtureSpecFiles: ReadDir(sub) %s: %v", specsDir, err)
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
		t.Fatalf("ar041FixtureSpecFiles: no spec files found under %s", specsDir)
	}
	return files
}

// ar041FixtureReqIDPattern extracts the requirement identifier from a level-4
// Markdown heading line of the form "#### RC-015 — ...".
var ar041FixtureReqIDPattern = regexp.MustCompile(`^#### ([A-Z]+-[A-Z0-9]+[a-z]?)`)

// ar041FixtureReqID extracts the requirement identifier from a heading line.
func ar041FixtureReqID(heading string) string {
	if m := ar041FixtureReqIDPattern.FindStringSubmatch(heading); m != nil {
		return m[1]
	}
	return "UNKNOWN"
}

// ar041FixtureParseViolations parses one spec file and returns all AR-041
// violations found within it.
//
// Parser strategy:
//  1. Walk lines, tracking the current normative requirement heading and
//     whether we are inside a fenced code block.
//  2. A level-4 heading matching the requirement pattern starts a new context;
//     open-question headings clear the context (they are not normative).
//  3. For each line in the 30-line look-ahead window (before the next heading),
//     skip INFORMATIVE lines ("> ..."), revision-history table rows ("| ..."),
//     and lines inside fenced code blocks.
//  4. On remaining lines, apply Option A (external URL check) and Option C
//     (prohibited-channel name check).
func ar041FixtureParseViolations(t *testing.T, specFile string) []ar041FixtureViolation {
	t.Helper()

	//nolint:gosec // G304: path comes from ar041FixtureSpecFiles which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar041FixtureParseViolations: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar041FixtureParseViolations: scan %s: %v", specFile, scanErr)
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
		if !ar041FixtureAnyHeading.MatchString(line) {
			continue
		}
		if ar041FixtureIsOpenQuestion.MatchString(line) {
			continue
		}
		m := ar041FixtureAnyHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		reqs = append(reqs, reqContext{
			headingLineNo: i + 1, // 1-based
			reqID:         m[1],
		})
	}

	// ownerFor returns the requirement ID owning line at 0-based index lineIdx,
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
			// Check that no new heading interrupts the window.
			interrupted := false
			for k := hIdx + 1; k < lineIdx && k < windowEnd; k++ {
				if ar041FixtureAnySectionHeading.MatchString(lines[k]) {
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

	var violations []ar041FixtureViolation
	inCodeBlock := false

	for i, line := range lines {
		// Track fenced code block state.
		if ar041FixtureFenceMarker.MatchString(line) {
			inCodeBlock = !inCodeBlock
			continue
		}

		// Lines inside fenced code blocks are illustrations, not normative prose.
		if inCodeBlock {
			continue
		}

		// Skip INFORMATIVE blocks and revision-history table rows.
		if ar041FixtureIsInformative.MatchString(line) {
			continue
		}
		if ar041FixtureIsRevisionRow.MatchString(line) {
			continue
		}

		// Only audit lines that fall inside a normative requirement window.
		// Lines with ownerReqID == "free-floating" are outside any requirement
		// context (e.g. section-level prose, informative §rationale blocks);
		// we skip them to avoid false positives from non-requirement prose.
		reqID := ownerFor(i)
		if reqID == "free-floating" {
			continue
		}

		lineNo := i + 1 // 1-based

		// Option A — external URL audit.
		for _, m := range ar041FixtureExternalURL.FindAllStringSubmatch(line, -1) {
			url := m[1]
			violations = append(violations, ar041FixtureViolation{
				file:    relFile,
				lineNo:  lineNo,
				reqID:   reqID,
				kind:    "external-url",
				subject: url,
				detail: "external URL found in normative requirement body; if this is a library attribution " +
					"or RFC citation, add it to ar041FixtureExpectedViolations with a rationale; " +
					"if it is a load-bearing external knowledge source, it violates AR-041",
			})
		}

		// Option C — prohibited-channel name audit.
		lowerLine := strings.ToLower(line)
		for _, phrase := range ar041FixtureProhibitedChannels {
			if strings.Contains(lowerLine, strings.ToLower(phrase)) {
				violations = append(violations, ar041FixtureViolation{
					file:    relFile,
					lineNo:  lineNo,
					reqID:   reqID,
					kind:    "prohibited-channel",
					subject: phrase,
					detail: fmt.Sprintf(
						"prohibited external-channel reference %q found in normative requirement body; "+
							"AR-041 forbids external wikis, out-of-band knowledge bases, and tribal-knowledge channels "+
							"from being load-bearing for conformance",
						phrase,
					),
				})
			}
		}
	}

	return violations
}

// ar041FixtureExpectedViolation is a single entry in the expected-violations
// skip-list.  A requirement in this map is a known AR-041 candidate violation
// that has been evaluated and either (a) determined to be non-load-bearing
// and pinned as an allowable citation, or (b) determined to require a spec-
// level fix tracked by an in-flight bead.
type ar041FixtureExpectedViolation struct {
	// pinnedBy is the bead ID that owns the evaluation or fix for this entry.
	// Use "ar041-allowable" for entries that are structurally allowable (library
	// citations, RFC attribution) and do not need a spec fix.
	pinnedBy string
	// reason is a human-readable explanation of why this entry is not an
	// AR-041 violation (or why it is pinned to a follow-up bead).
	reason string
}

// ar041FixtureExpectedViolations is the skip-list of known AR-041 candidate
// violations that are intentionally deferred or evaluated as non-load-bearing.
//
// Key format: "<relative-spec-path>:<line-number>:<requirementID>:<kind>:<subject>".
// The line number MUST be the 1-based line number of the violating line.
//
// Rules:
//   - An entry whose violation is NOT present causes t.Errorf("stale skip-list
//     entry …") — remove stale entries promptly.
//   - An entry whose violation IS present produces t.Logf and does NOT fail.
//   - Any NEW violation NOT in this map DOES fail the suite.
var ar041FixtureExpectedViolations = map[string]ar041FixtureExpectedViolation{
	// CP-034 (specs/control-points.md line 319): `https://github.com/expr-lang/expr`
	//
	// This URL is a library-attribution citation for the `expr-lang/expr` Go
	// module, which CP-034 mandates as the policy-expression grammar.  The
	// actual normative artifact is the module entry in the repo's go.mod
	// (in-repo, not external).  The GitHub URL is referenced for attribution
	// and human-readable cross-reference only — it is not a load-bearing
	// external knowledge source.  No spec-fix bead is required; this entry
	// documents the evaluation decision.
	// Line shifted from 315 → 316 when hk-zs0.1 added spec-category to
	// control-points.md front matter (one line inserted above line 315);
	// then 316 → 319 (hk-feow8) as control-points.md grew above CP-034.
	"specs/control-points.md:319:CP-034:external-url:https://github.com/expr-lang/expr": {
		pinnedBy: "ar041-allowable",
		reason: "CP-034 cites github.com/expr-lang/expr for the policy-expression grammar library; " +
			"the normative artifact is the in-repo go.mod entry, not the external URL; " +
			"this is an attribution citation and is not load-bearing for conformance",
	},

	// AR-041 self-referential prohibited-channel mentions (architecture.md line 378).
	//
	// The AR-041 requirement body names "external wikis", "out-of-band knowledge
	// bases", and "tribal-knowledge channels" as part of its own prohibition
	// clause ("MUST NOT be load-bearing for any spec's conformance").  These
	// phrases are definitional — the requirement is declaring what is forbidden,
	// not depending on those channels.  Matching them as violations would be
	// circular self-application.  Pinned as structurally allowable; no spec fix
	// required.
	"specs/architecture.md:378:AR-041:prohibited-channel:external wiki": {
		pinnedBy: "ar041-allowable",
		reason: "AR-041 names 'external wiki' in its own prohibition clause; the phrase is definitional " +
			"(declaring what is forbidden), not a load-bearing reference to an external wiki",
	},
	"specs/architecture.md:378:AR-041:prohibited-channel:tribal-knowledge": {
		pinnedBy: "ar041-allowable",
		reason: "AR-041 names 'tribal-knowledge' in its own prohibition clause; definitional self-reference, " +
			"not a load-bearing external knowledge dependency",
	},
	"specs/architecture.md:378:AR-041:prohibited-channel:out-of-band knowledge base": {
		pinnedBy: "ar041-allowable",
		reason: "AR-041 names 'out-of-band knowledge bases' in its own prohibition clause; definitional " +
			"self-reference, not a load-bearing external knowledge dependency",
	},
}

// ar041FixtureViolationKey returns the skip-list lookup key for a violation.
// Format: "<file>:<lineNo>:<reqID>:<kind>:<subject>".
func ar041FixtureViolationKey(v ar041FixtureViolation) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", v.file, v.lineNo, v.reqID, v.kind, v.subject)
}

// TestAR041RepoSingleSourceOfTruth is the binding test for AR-041.
//
// It walks every spec file in scope, scans normative requirement bodies for
// (a) external URLs and (b) prohibited external-channel names, and asserts
// that no new violations exist beyond those pinned in
// ar041FixtureExpectedViolations.
//
// Known violations that are either structurally allowable (library citations)
// or require a spec-level fix tracked by an in-flight bead are listed in
// ar041FixtureExpectedViolations.  Those entries are logged (not failed) and
// produce an error if they become stale (violation no longer present).
func TestAR041RepoSingleSourceOfTruth(t *testing.T) {
	specFiles := ar041FixtureSpecFiles(t)

	var allViolations []ar041FixtureViolation
	for _, sf := range specFiles {
		vs := ar041FixtureParseViolations(t, sf)
		allViolations = append(allViolations, vs...)
	}

	// Build a set of violation keys found in the current corpus.
	foundKeys := make(map[string]ar041FixtureViolation, len(allViolations))
	for _, v := range allViolations {
		foundKeys[ar041FixtureViolationKey(v)] = v
	}

	// Check for stale skip-list entries (pinned violations that no longer exist).
	for key, entry := range ar041FixtureExpectedViolations {
		if _, present := foundKeys[key]; !present {
			t.Errorf("AR-041 skip-list: stale entry %q (pinned by %s) — violation no longer present; remove from ar041FixtureExpectedViolations",
				key, entry.pinnedBy)
		}
	}

	// Separate violations into expected (pinned) and unexpected (new failures).
	var unexpected []ar041FixtureViolation
	for _, v := range allViolations {
		key := ar041FixtureViolationKey(v)
		if entry, pinned := ar041FixtureExpectedViolations[key]; pinned {
			t.Logf("AR-041 expected violation (pinned by %s): %s — %s", entry.pinnedBy, key, entry.reason)
			continue
		}
		unexpected = append(unexpected, v)
	}

	if len(unexpected) == 0 {
		t.Logf("AR-041 audit: all %d spec files pass — no normative requirement bodies contain unresolved external URLs or prohibited-channel references (%d known entry(ies) pinned)",
			len(specFiles), len(ar041FixtureExpectedViolations))
		return
	}

	// Report ALL unexpected violations so the full failure surface is visible.
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"AR-041 violation: %d normative requirement body(ies) contain external URL(s) or prohibited-channel reference(s)\n",
		len(unexpected),
	))
	sb.WriteString("(specs/architecture.md §4.9 AR-041: external wikis, out-of-band knowledge bases, and tribal-knowledge channels\n")
	sb.WriteString("MUST NOT be load-bearing for any spec's conformance; external URLs in normative bodies are candidates)\n\n")
	sb.WriteString("Prohibited channel names: ")
	sb.WriteString(strings.Join(ar041FixtureProhibitedChannels, ", "))
	sb.WriteString("\n\n")
	for _, v := range unexpected {
		sb.WriteString("  ")
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	t.Error(sb.String())
}
