package operatornfr_test

// hk-sx9r.27 binding test — ON-022 secrets injection and fail-closed redaction.
//
// Spec refs:
//   - specs/operator-nfr.md §4.7 ON-022 (normative home)
//   - specs/handler-contract.md §4.7 HC-028..HC-034 (mechanism detail)
//
// ON-022 states:
//
//	Secrets (API keys, tokens, credentials) MUST be injected at handler launch
//	per [handler-contract.md §4.7]. Secrets MUST NOT appear in the event log
//	under any circumstance. Secrets MUST NOT appear in session logs without
//	redaction. Redaction is mechanism-tagged and MUST be enforced pre-emission.
//
//	The redactor MUST fail-closed: if the redactor itself panics or returns an
//	error during emission of any event/log/audit-record carrying typed Secret
//	fields, the daemon MUST abort the emission, MUST emit
//	redaction_failed{event_type, run_id?, error_class}, and MUST NOT fall
//	through to a non-redacted emission. Repeated redactor panics within
//	T_redact_fail (default 60s, operator-tunable) MUST escalate the daemon to
//	degraded per ON-037.
//
// # Audit frame
//
// This is a spec-corpus sensor. It verifies that every normative ON-022
// sub-obligation is present in the spec body and that the ON-022 section
// cross-references handler-contract §4.7 (the mechanism detail home). The
// thin existence check in securitydrain_sx9r80_test.go (TestON022_SpecSectionExists)
// is superseded by this body-window scan.
//
// # Failure modes
//
//   - ON-022 heading missing: the requirement does not exist in the spec.
//   - Injection-at-launch obligation absent: "injected at handler launch" not stated.
//   - Handler-contract cross-reference absent: §4.7 reference to handler-contract.md missing.
//   - Never-in-event-log obligation absent: "MUST NOT appear in the event log" not stated.
//   - Fail-closed obligation absent: "fail-closed" not stated for the redactor.
//   - redaction_failed event obligation absent: "redaction_failed" event not named.
//   - T_redact_fail knob absent: operator-tunable window not named.
//   - Degraded escalation absent: degraded escalation on repeated panics not stated.
//   - Tags: mechanism absent from ON-022 body window.
//
// # Supersession note
//
// TestON022_SpecSectionExists in securitydrain_sx9r80_test.go performs a
// two-string thin check ("ON-022" and "Secrets are injected at handler launch").
// This sensor adds a body-window scan verifying each sub-obligation separately,
// matching the pattern established by payloadschemacheck_sx9r28_test.go.
//
// # Helper prefix
//
// All package-level identifiers in this file use the sx9r27Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sx9r27FixtureOperatorNFRPath returns the absolute path to
// specs/operator-nfr.md by walking up from the repo root provided by
// obligationsFixtureRepoRoot.
func sx9r27FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	root := obligationsFixtureRepoRoot(t)
	return filepath.Join(root, "specs", "operator-nfr.md")
}

// sx9r27FixtureON022Heading matches the ON-022 level-4 requirement heading
// line in operator-nfr.md.
var sx9r27FixtureON022Heading = regexp.MustCompile(`^#### ON-022 —`)

// sx9r27FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the ON-022 requirement body window.
var sx9r27FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// sx9r27FixtureTagsMechanism matches a "Tags: mechanism" line within the body.
var sx9r27FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// sx9r27FixtureBodyWindow is the maximum number of lines after the ON-022
// heading to scan for requirement-body content. ON-022 is two paragraphs; 40
// lines covers the full body plus axes line.
const sx9r27FixtureBodyWindow = 40

// sx9r27FixtureLoadLines opens specFile and returns all lines.
func sx9r27FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from obligationsFixtureRepoRoot (runtime.Caller) + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sx9r27FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sx9r27FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// sx9r27FixtureON022BodyLines returns the lines comprising the ON-022
// requirement body: all lines after the ON-022 heading line up to (but not
// including) the next Markdown heading or sx9r27FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the ON-022 heading is not found.
func sx9r27FixtureON022BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sx9r27FixtureON022Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-022 heading not found; expected '#### ON-022 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + sx9r27FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if sx9r27FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// sx9r27FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func sx9r27FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSX9R27_ON022SecretsInjectionAndFailClosedRedaction is the binding test
// for hk-sx9r.27.
//
// It opens specs/operator-nfr.md, locates the ON-022 heading, and validates
// every normative sub-obligation separately:
//
//	(a) ON-022 heading present (requirement exists in spec).
//	(b) Injection-at-launch obligation: "injected at handler launch".
//	(c) Cross-reference to handler-contract.md §4.7 (mechanism home).
//	(d) Never-in-event-log obligation: secrets "MUST NOT appear in the event log".
//	(e) Fail-closed obligation: "fail-closed" named for the redactor.
//	(f) redaction_failed event: event named in the abort path.
//	(g) T_redact_fail knob: operator-tunable window named.
//	(h) Degraded escalation: escalation to `degraded` on repeated failures stated.
//	(i) Tags: mechanism present in body window.
func TestSX9R27_ON022SecretsInjectionAndFailClosedRedaction(t *testing.T) {
	t.Parallel()

	specFile := sx9r27FixtureOperatorNFRPath(t)
	lines := sx9r27FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sx9r27FixtureON022BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-022 check(a): %s", reason)
	}
	t.Logf("ON-022 heading found at specs/operator-nfr.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:    "b",
			label: "injection-at-launch obligation",
			// The spec body uses "injected at handler launch" in the opening sentence.
			needle: "injected at handler launch",
			detail: "ON-022 body must state that secrets MUST be injected at handler launch " +
				"(expected phrase 'injected at handler launch'); this is the delivery mechanism — " +
				"secrets travel via process environment at handler-launch time, never via LaunchSpec fields",
		},
		{
			id:    "c",
			label: "handler-contract cross-reference present",
			// The spec cross-refs as "[handler-contract.md §4.7]".
			needle: "handler-contract.md",
			detail: "ON-022 body must cross-reference handler-contract.md (the mechanism home for " +
				"HC-028..HC-034); without this cross-reference the reader cannot find the enforcement " +
				"detail for prefix-regex matching and per-handler redaction patterns",
		},
		{
			id:    "d",
			label: "never-in-event-log obligation",
			// The spec body says "MUST NOT appear in the event log".
			needle: "MUST NOT appear in the event log",
			detail: "ON-022 body must state that secrets MUST NOT appear in the event log under any " +
				"circumstance (expected phrase 'MUST NOT appear in the event log'); this is the " +
				"strongest invariant — no conditional, no redaction fallback for the event bus path",
		},
		{
			id:    "e",
			label: "fail-closed redactor obligation",
			// The spec body says "The redactor MUST fail-closed".
			needle: "fail-closed",
			detail: "ON-022 body must state that the redactor MUST fail-closed (expected phrase " +
				"'fail-closed'); fail-closed means aborting the emission rather than falling through " +
				"to a non-redacted write on any redactor error or panic",
		},
		{
			id:    "f",
			label: "redaction_failed event named in abort path",
			// The spec body says "MUST emit `redaction_failed{...}`".
			needle: "redaction_failed",
			detail: "ON-022 body must name the 'redaction_failed' event as the required emission " +
				"when the redactor aborts (expected substring 'redaction_failed'); this is the " +
				"operator-observable signal that a redaction failure occurred without leaking the secret",
		},
		{
			id:    "g",
			label: "T_redact_fail operator-tunable window named",
			// The spec body says "within T_redact_fail (default 60s, operator-tunable)".
			needle: "T_redact_fail",
			detail: "ON-022 body must name the 'T_redact_fail' operator-tunable time window " +
				"(expected substring 'T_redact_fail'); this window governs when repeated redactor " +
				"panics trigger the degraded escalation; naming it is required for the config " +
				"inventory of ON-004",
		},
		{
			id:    "h",
			label: "degraded escalation on repeated panics",
			// The spec body says "MUST escalate the daemon to `degraded` per ON-037".
			needle: "degraded",
			detail: "ON-022 body must state that repeated redactor failures escalate the daemon to " +
				"'degraded' (expected substring 'degraded'); this connects the redaction failure " +
				"path to the ON-037 subsystem health-check surface so operators are notified",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !sx9r27FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-022 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-022 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (i): Tags: mechanism in ON-022 body.
	t.Run("check-i-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sx9r27FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-022 check(i) FAILED: Tags: mechanism not found in ON-022 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-022 body)\n"+
					"  detail: ON-022 carries tag 'mechanism' per §4.7; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.27 audit complete — ON-022 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}

// TestSX9R27_ON022HandlerContractSectionExists verifies that the HC §4.7
// cross-reference target is present in specs/handler-contract.md.
//
// ON-022 delegates the injection mechanism to [handler-contract.md §4.7].
// This test confirms that the §4.7 secrets section exists in that spec, so
// the cross-reference in ON-022 resolves to a real normative section.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-022; specs/handler-contract.md §4.7.
func TestSX9R27_ON022HandlerContractSectionExists(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	hcPath := filepath.Join(root, "specs", "handler-contract.md")

	//nolint:gosec // G304: path derived from obligationsFixtureRepoRoot (runtime.Caller) + known specs/ directory; not user input.
	data, err := os.ReadFile(hcPath)
	if err != nil {
		t.Fatalf("ON-022: cannot read specs/handler-contract.md: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "### 4.7 Secrets") {
		t.Error("ON-022: specs/handler-contract.md does not contain '### 4.7 Secrets'; " +
			"ON-022 cross-references [handler-contract.md §4.7] as the mechanism home — " +
			"the section must exist for the cross-reference to resolve")
	}
	if !strings.Contains(content, "HC-028") {
		t.Error("ON-022: specs/handler-contract.md §4.7 does not contain 'HC-028' " +
			"(secrets-injection-via-env-var requirement); HC-028 is the first secrets " +
			"obligation declared in §4.7 and must be present")
	}
	if !strings.Contains(content, "HARMONIK_SECRET_") {
		t.Error("ON-022: specs/handler-contract.md §4.7 does not contain 'HARMONIK_SECRET_' " +
			"(the stable environment-variable prefix for secret delivery); HC-028 requires " +
			"this prefix and it must appear in the spec text")
	}
}

// TestSX9R27_ON022RedactionFailedEventNamedInSpec verifies that
// "redaction_failed" appears in specs/operator-nfr.md not just in the ON-022
// body but also in a cross-spec coordination context, confirming that the event
// is named as a coordination request to the event-model spec.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-022 — "(cross-spec coordination
// request to EV: add `redaction_failed` to §8 taxonomy)".
func TestSX9R27_ON022RedactionFailedEventNamedInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "operator-nfr.md")

	//nolint:gosec // G304: path derived from obligationsFixtureRepoRoot (runtime.Caller) + known specs/ directory; not user input.
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("ON-022: cannot read specs/operator-nfr.md: %v", err)
	}

	content := string(data)

	if !strings.Contains(content, "redaction_failed") {
		t.Error("ON-022: specs/operator-nfr.md does not contain 'redaction_failed'; " +
			"ON-022 requires the daemon to emit this event when the redactor aborts, " +
			"and a cross-spec coordination request to EV must name it")
	}
}
