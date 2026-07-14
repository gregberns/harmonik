//go:build specaudit

package specaudit_test

// hk-8i31.35 binding test — HC-028 secrets injected via environment variable
// (HARMONIK_SECRET_* prefix).
//
// Spec ref: specs/handler-contract.md §4.7 HC-028.
//
// HC-028 states: secrets (API keys, tokens) MUST be delivered to the handler subprocess
// as environment variables with the stable prefix HARMONIK_SECRET_*. LaunchSpec MUST NOT
// carry secret values in any field; the fact that a given secret is required MAY be encoded
// in LaunchSpec indirectly (via freedom_profile_ref resolution), but the value itself
// travels only via process environment.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler secrets implementation is pending; this
// sensor verifies that HC-028 is correctly declared in the spec so that:
//
//  1. HC-028 heading is present in specs/handler-contract.md.
//  2. "HARMONIK_SECRET_*" is named as the required prefix.
//  3. "LaunchSpec MUST NOT carry secret values" is declared.
//  4. "travels only via process environment" is declared for secret values.
//  5. Tags: mechanism is present in the HC-028 body window.
//
// # Failure modes
//
//   - HC-028 heading missing.
//   - HARMONIK_SECRET_* prefix absent.
//   - LaunchSpec MUST NOT carry secret values absent.
//   - travels only via process environment absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc028Fixture prefix per
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

// hc028FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc028FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc028FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc028FixtureHC028Heading matches the HC-028 level-4 requirement heading line.
var hc028FixtureHC028Heading = regexp.MustCompile(`^#### HC-028 —`)

// hc028FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc028FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc028FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc028FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc028FixtureBodyWindow is the maximum number of lines after the HC-028
// heading to scan for requirement-body content.
const hc028FixtureBodyWindow = 30

// hc028FixtureLoadLines opens specFile and returns all lines.
func hc028FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc028FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc028FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc028FixtureHC028BodyLines returns the lines comprising the HC-028 body.
func hc028FixtureHC028BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc028FixtureHC028Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-028 heading not found; expected '#### HC-028 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc028FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc028FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc028FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc028FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC028SecretsEnvVarPrefix is the binding test for hk-8i31.35.
func TestHC028SecretsEnvVarPrefix(t *testing.T) {
	t.Parallel()

	specFile := hc028FixtureHandlerContractPath(t)
	lines := hc028FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc028FixtureHC028BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-028 check(1): %s", reason)
	}
	t.Logf("HC-028 heading found at specs/handler-contract.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "2",
			label:  "harmonik-secret-star-prefix",
			needle: "HARMONIK_SECRET_*",
			detail: "HC-028 body must name 'HARMONIK_SECRET_*' as the required env var prefix " +
				"(expected phrase 'HARMONIK_SECRET_*'); the stable prefix enables generic secret " +
				"scrubbing — any tool that knows the prefix can redact secret values without " +
				"knowing which specific secrets are in use for a given handler type",
		},
		{
			id:     "3",
			label:  "launchspec-must-not-carry-secret-values",
			needle: "LaunchSpec MUST NOT carry secret values",
			detail: "HC-028 body must declare 'LaunchSpec MUST NOT carry secret values' " +
				"(expected phrase 'LaunchSpec MUST NOT carry secret values'); LaunchSpec is " +
				"serialized to JSON and may be written to disk or transmitted — putting secrets " +
				"in it would expose them to any reader of the JSON",
		},
		{
			id:     "4",
			label:  "value-travels-via-process-environment",
			needle: "travels only via process environment",
			detail: "HC-028 body must declare the secret value 'travels only via process environment' " +
				"(expected phrase 'travels only via process environment'); process environment is " +
				"the POSIX-standard mechanism for secret injection — it is not serialized to disk " +
				"by the launcher, it is not included in any log, and it is only visible to the " +
				"subprocess's own process tree",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc028FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-028 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-028 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (5): Tags: mechanism in HC-028 body.
	t.Run("check-5-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc028FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-028 check(5) FAILED: Tags: mechanism not found in HC-028 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-028 body)\n"+
					"  detail: HC-028 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.35 audit complete — HC-028 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
