package specaudit_test

// hk-8i31.36 binding test — HC-029 agent_started event MUST NOT include
// environment variables.
//
// Spec ref: specs/handler-contract.md §4.7 HC-029.
//
// HC-029 states: "The `agent_started` event payload MUST NOT include the
// handler subprocess's environment. Launch provenance fields (binary path,
// commit hash) are permitted; environment variable maps are not."
//
// # What this test verifies
//
// This is a spec-corpus binding test. The agent_started event emitter is not
// yet implemented in Go code; the rule is a forward-looking contract that will
// govern every future implementation of the agent_started emission path. The
// test encodes the rule so that:
//
//	(a) The HC-029 requirement heading is present in specs/handler-contract.md.
//	(b) The MUST NOT prohibition is stated in the requirement body:
//	    "MUST NOT include" (the handler subprocess's environment is excluded).
//	(c) The word "environment" appears in the body, naming what is excluded.
//	(d) The permitted clause is present: "permitted" appears in the body,
//	    confirming launch provenance fields (binary path, commit hash) are
//	    explicitly allowed — this prevents the rule from being misread as
//	    prohibiting ALL payload fields.
//	(e) "environment variable maps" appears in the body, naming the specific
//	    prohibited element with precision.
//	(f) "Tags: mechanism" is present within the requirement body window.
//	(g) The §6.4 co-owned-event-payloads table also cross-references HC-029
//	    for the agent_started entry, ensuring the cross-spec anchor is present.
//
// Failure in any check means the spec has drifted from the HC-029 contract
// that downstream implementers depend on.
//
// # Why no Go-code structural check
//
// There is no agent_started emitter package in internal/ at this time. The
// enforcement surface is the spec itself. When the emitter implementation
// lands it will be covered by its own unit tests asserting the payload schema
// contains no env-var map fields; this bead encodes only the contract
// declaration as a spec-corpus sensor.
//
// # Helper prefix
//
// All package-level identifiers in this file use the i3136 prefix per the
// implementer-protocol.md helper-prefix discipline (bead hk-8i31.36 → i3136).

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

// i3136FixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/hc029_agent_started_no_env_test.go
//
// so the repo root is two directories up.
func i3136FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("i3136FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// i3136FixtureHC029Heading matches the HC-029 level-4 requirement heading line.
var i3136FixtureHC029Heading = regexp.MustCompile(`^#### HC-029 —`)

// i3136FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-029 requirement body window.
var i3136FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// i3136FixtureTagsMechanism matches a "Tags: mechanism" line (the only tag
// value HC-029 is required to carry per the spec).
var i3136FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// i3136FixtureAgentStartedHC029Ref matches the §6.4 agent_started bullet that
// cross-references HC-029 by its section anchor (§4.7.HC-029).
var i3136FixtureAgentStartedHC029Ref = regexp.MustCompile(`agent_started.*HC-029`)

// i3136FixtureBodyWindow is the maximum number of lines after the HC-029
// heading to scan for requirement-body content. Matches the 30-line cap used
// by sibling specaudit tests.
const i3136FixtureBodyWindow = 30

// i3136FixtureLoadLines opens specFile and returns all lines.
func i3136FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("i3136FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("i3136FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// i3136FixtureBodyLines returns the lines comprising the HC-029 requirement
// body: all lines after the HC-029 heading line up to (but not including) the
// next Markdown heading or i3136FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the HC-029 heading is not found.
func i3136FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if i3136FixtureHC029Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-029 heading not found; expected '#### HC-029 — agent_started event MUST NOT include environment variables' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + i3136FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if i3136FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// i3136FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func i3136FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC029AgentStartedNoEnvVars is the binding test for HC-029.
//
// It opens specs/handler-contract.md, locates the HC-029 heading, and
// validates:
//
//	(a) The HC-029 heading is present (the rule exists in the spec).
//	(b) The MUST NOT prohibition: "must not include" appears in the body.
//	(c) The prohibited element is named: "environment" appears in the body.
//	(d) The permitted-clause is present: "permitted" appears in the body,
//	    confirming launch provenance fields are allowed.
//	(e) The precise prohibited element: "environment variable maps" appears
//	    in the body.
//	(f) "Tags: mechanism" is present in the body window.
//	(g) The §6.4 co-owned-event-payloads table contains an agent_started
//	    entry that cross-references HC-029 (by "HC-029" anywhere on that line).
func TestHC029AgentStartedNoEnvVars(t *testing.T) {
	t.Parallel()

	specFile := i3136FixtureHandlerContractPath(t)
	lines := i3136FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := i3136FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-029 check(a): %s", reason)
	}
	t.Logf("HC-029 heading found at specs/handler-contract.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string // substring to search for (case-insensitive)
		detail string // shown on failure
	}

	checks := []check{
		{
			id:     "b",
			label:  "MUST NOT prohibition",
			needle: "must not include",
			detail: "HC-029 body must state that the agent_started event payload MUST NOT include " +
				"the handler subprocess's environment " +
				"(expected phrase 'MUST NOT include' or 'must not include'); " +
				"this is the load-bearing prohibition for implementers",
		},
		{
			id:     "c",
			label:  "prohibited element named",
			needle: "environment",
			detail: "HC-029 body must name 'environment' as the prohibited element " +
				"(expected the word 'environment' in the body); " +
				"this scopes the prohibition to the subprocess environment specifically",
		},
		{
			id:     "d",
			label:  "permitted-clause present",
			needle: "permitted",
			detail: "HC-029 body must include a permitted clause " +
				"(expected the word 'permitted' in the body); " +
				"this clause confirms that launch provenance fields (binary path, commit hash) " +
				"are allowed — without it the rule could be misread as prohibiting all payload fields",
		},
		{
			id:     "e",
			label:  "environment variable maps named",
			needle: "environment variable maps",
			detail: "HC-029 body must name 'environment variable maps' as the specifically prohibited element " +
				"(expected phrase 'environment variable maps'); " +
				"this distinguishes the env-var map from permitted provenance fields with precision",
		},
	}

	// Run checks b–e as sub-tests.
	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !i3136FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-029 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-029 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (f): Tags: mechanism
	t.Run("check-f-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if i3136FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-029 check(f) FAILED: Tags: mechanism not found in HC-029 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-029 body)\n"+
					"  detail: HC-029 carries tag 'mechanism' per the spec; its absence indicates the"+
					" requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	// Check (g): §6.4 cross-reference — the agent_started bullet in the
	// co-owned-event-payloads table must cross-reference HC-029.
	t.Run("check-g-sec64-cross-reference", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range lines {
			if i3136FixtureAgentStartedHC029Ref.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-029 check(g) FAILED: §6.4 agent_started entry does not cross-reference HC-029\n" +
					"  spec:   specs/handler-contract.md §6.4 (co-owned event payloads)\n" +
					"  detail: the agent_started bullet in §6.4 must contain 'HC-029' to anchor the" +
					" payload-no-env-vars obligation; its absence breaks the cross-spec traceability" +
					" chain between the requirement declaration and its application site",
			)
		}
	})

	t.Logf("HC-029 audit: checks complete — agent_started no-env-vars rule verified in specs/handler-contract.md "+
		"(heading at line %d, body = %d lines)",
		headingLineNo, len(body))
}
