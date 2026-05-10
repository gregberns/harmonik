package specaudit_test

// hk-sx9r.51 binding test — ON-035 every subsystem emits structured logs (NDJSON wire format).
//
// Spec ref: specs/operator-nfr.md §4.9 ON-035.
//
// ON-035 states: every subsystem MUST emit structured logs. Unstructured log lines are
// forbidden at spec-declared emission points. Minimum NDJSON record carries: ts (RFC 3339
// with ms), log_schema_version (current "1.0"), level ∈ {debug, info, warn, error},
// subsystem, source_subsystem, run_id?, node_id?, event_id?, msg, fields. Secrets-redaction
// per ON-022 MUST apply before emission. Log files MUST rotate at 100 MiB or 24 hours.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that ON-035 is correctly declared in the spec so that:
//
//  1. ON-035 heading is present in specs/operator-nfr.md.
//  2. "log_schema_version" is declared as a required field.
//  3. "source_subsystem" is declared as a required field.
//  4. "100 MiB" rotation threshold is declared.
//  5. "producer-side" redaction direction is declared.
//  6. Tags: mechanism is present in the ON-035 body window.
//
// # Failure modes
//
//   - ON-035 heading missing.
//   - log_schema_version absent.
//   - source_subsystem absent.
//   - 100 MiB absent.
//   - producer-side absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on035Fixture prefix per
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

// on035FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on035FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on035FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on035FixtureHeading matches the ON-035 level-4 requirement heading line.
var on035FixtureHeading = regexp.MustCompile(`^#### ON-035 —`)

// on035FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on035FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on035FixtureTagsMechanism matches a "Tags: mechanism" line.
var on035FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on035FixtureBodyWindow is the maximum number of lines to scan after the heading.
const on035FixtureBodyWindow = 15

// on035FixtureLoadLines opens specFile and returns all lines.
func on035FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on035FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on035FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on035FixtureBodyLines returns the lines comprising the ON-035 body.
func on035FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on035FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-035 heading not found; expected '#### ON-035 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on035FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on035FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on035FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on035FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON035StructuredLogsNDJSON is the binding test for hk-sx9r.51.
func TestON035StructuredLogsNDJSON(t *testing.T) {
	t.Parallel()

	specFile := on035FixtureOperatorNFRPath(t)
	lines := on035FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on035FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-035 check(1): %s", reason)
	}
	t.Logf("ON-035 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "log-schema-version-field",
			needle: "log_schema_version",
			detail: "ON-035 body must declare 'log_schema_version' as a required field " +
				"(expected phrase 'log_schema_version'); this is the N-1 compat discriminator — " +
				"consumers use it to select the correct parser for the log record; any breaking " +
				"change to the log shape MUST bump this version per ON-INV-001",
		},
		{
			id:     "3",
			label:  "source-subsystem-field",
			needle: "source_subsystem",
			detail: "ON-035 body must declare 'source_subsystem' as a required field " +
				"(expected phrase 'source_subsystem'); this field names the subsystem that emitted " +
				"the log record — it enables filtering by subsystem in observability tools and " +
				"correlates with the event-model's EV-034a source_subsystem field",
		},
		{
			id:     "4",
			label:  "100-mib-rotation-threshold",
			needle: "100 MiB",
			detail: "ON-035 body must declare '100 MiB' as the log rotation threshold " +
				"(expected phrase '100 MiB'); this is the size-based rotation threshold — " +
				"when a log file exceeds 100 MiB, it is rotated to .harmonik/logs/ with a timestamp " +
				"suffix; the 24-hour time-based threshold is the companion rotation trigger",
		},
		{
			id:     "5",
			label:  "producer-side-redaction",
			needle: "producer-side",
			detail: "ON-035 body must declare 'producer-side' as the redaction direction " +
				"(expected phrase 'producer-side'); redaction is applied before emission — " +
				"the producer (the subsystem emitting the log) is responsible for redacting secrets, " +
				"and consumers MUST NOT re-redact (which could corrupt already-redacted values)",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on035FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-035 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-035 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in ON-035 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on035FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-035 check(6) FAILED: Tags: mechanism not found in ON-035 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-035 body)\n"+
					"  detail: ON-035 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.51 audit complete — ON-035 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
