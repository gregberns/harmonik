//go:build specaudit

package specaudit_test

// hk-8i31.66 binding test — HC-INV-003 sensor: no secret value crosses the
// event-bus or log-emission boundary.
//
// Spec ref: specs/handler-contract.md §5 HC-INV-003.
//
// HC-INV-003 states: No secret value MAY be observable in any event payload
// delivered to any consumer, in any persisted event record, or in any
// structured log line written to disk. The redaction registry (§4.7) enforces
// this at producer time; the compile-time schema check (§4.7.HC-033) closes
// the schema-drift path.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The redaction pipeline implementation is
// pending full end-to-end wiring; this sensor verifies that HC-INV-003 is
// correctly declared in the spec so that:
//
//  1. HC-INV-003 heading is present in specs/handler-contract.md — the
//     invariant exists in the normative spec.
//
//  2. "No secret value MAY be observable" is declared — the scope of the
//     prohibition covers any observable secret value, not just named fields.
//
//  3. "any event payload" is declared — the prohibition covers all event
//     payloads delivered to consumers, not just a subset.
//
//  4. "any persisted event record" is declared — the prohibition extends to
//     durable event storage (events.jsonl), not just in-memory delivery.
//
//  5. "any structured log line" is declared — the prohibition extends to
//     structured log emission to disk, closing the log-exfiltration path.
//
//  6. "redaction registry" is declared as the enforcement mechanism — the
//     mechanism-tagged component that enforces the invariant is named.
//
//  7. "HC-033" is declared as closing the schema-drift path — the compile-time
//     payload-schema check is referenced as the second enforcement surface.
//
//  8. Tags: mechanism is present in the HC-INV-003 body window — the
//     mechanism tag is required for all HC requirement bodies.
//
// # Failure modes
//
//   - HC-INV-003 heading missing.
//   - "No secret value MAY be observable" absent.
//   - "any event payload" absent.
//   - "any persisted event record" absent.
//   - "any structured log line" absent.
//   - "redaction registry" absent.
//   - "HC-033" absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hcInv003Fixture prefix per
// the implementer-protocol.md helper-prefix discipline (bead hk-8i31.66 →
// HC-INV-003 sensor).

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

// hcInv003FixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/hcinv003_no_secret_in_eventbus_test.go
//
// so the repo root is two directories up.
func hcInv003FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hcInv003FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hcInv003FixtureHeading matches the HC-INV-003 level-4 requirement heading line.
var hcInv003FixtureHeading = regexp.MustCompile(`^#### HC-INV-003 —`)

// hcInv003FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-INV-003 requirement body window.
var hcInv003FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hcInv003FixtureTagsMechanism matches a "Tags: mechanism" line.
var hcInv003FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hcInv003FixtureBodyWindow is the maximum number of lines after the heading
// to scan for requirement-body content.
const hcInv003FixtureBodyWindow = 15

// hcInv003FixtureLoadLines opens specFile and returns all lines.
func hcInv003FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hcInv003FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hcInv003FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hcInv003FixtureBodyLines returns the lines comprising the HC-INV-003 body:
// all lines after the HC-INV-003 heading up to (but not including) the next
// Markdown heading or hcInv003FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the HC-INV-003 heading is not
// found.
func hcInv003FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hcInv003FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-INV-003 heading not found; expected '#### HC-INV-003 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hcInv003FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hcInv003FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hcInv003FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hcInv003FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHCInv003NoSecretInEventBus is the binding test for hk-8i31.66.
//
// It opens specs/handler-contract.md, locates the HC-INV-003 heading, and
// validates that all required clauses of the invariant are present.
func TestHCInv003NoSecretInEventBus(t *testing.T) {
	t.Parallel()

	specFile := hcInv003FixtureHandlerContractPath(t)
	lines := hcInv003FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hcInv003FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-INV-003 check(1): %s", reason)
	}
	t.Logf("HC-INV-003 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "no-secret-value-may-be-observable",
			needle: "No secret value MAY be observable",
			detail: "HC-INV-003 body must state 'No secret value MAY be observable' " +
				"(expected phrase 'No secret value MAY be observable'); this is the load-bearing " +
				"prohibition that covers all observable forms of a secret — field name matches, " +
				"value-pattern matches, and unintended literal inclusion; scoping to 'any' " +
				"observable form prevents redaction-bypass via indirect emission paths",
		},
		{
			id:     "3",
			label:  "any-event-payload",
			needle: "any event payload",
			detail: "HC-INV-003 body must declare 'any event payload' as a covered surface " +
				"(expected phrase 'any event payload'); the prohibition must cover all event " +
				"payloads delivered to consumers — limiting it to a subset of event types or " +
				"consumer classes would create a bypass path for secrets reaching non-covered " +
				"consumers",
		},
		{
			id:     "4",
			label:  "any-persisted-event-record",
			needle: "any persisted event record",
			detail: "HC-INV-003 body must declare 'any persisted event record' as a covered surface " +
				"(expected phrase 'any persisted event record'); the prohibition must extend to " +
				"durable event storage (events.jsonl) — in-memory redaction without durable " +
				"enforcement still leaks secrets to disk and any reader of the event log",
		},
		{
			id:     "5",
			label:  "any-structured-log-line",
			needle: "any structured log line",
			detail: "HC-INV-003 body must declare 'any structured log line' as a covered surface " +
				"(expected phrase 'any structured log line'); structured logs are an independent " +
				"emission path from the event bus — secrets could escape redaction if the " +
				"log-emission path is not covered by the same middleware",
		},
		{
			id:     "6",
			label:  "redaction-registry-enforcement",
			needle: "redaction registry",
			detail: "HC-INV-003 body must name 'redaction registry' as the enforcement mechanism " +
				"(expected phrase 'redaction registry'); naming the specific component anchors the " +
				"invariant to the mechanism-tagged implementation (§4.7) — implementers can locate " +
				"the enforcement point and verify it covers both the event bus and the log-emission " +
				"path",
		},
		{
			id:     "7",
			label:  "hc-033-closes-schema-drift-path",
			needle: "HC-033",
			detail: "HC-INV-003 body must reference 'HC-033' as closing the schema-drift path " +
				"(expected phrase 'HC-033'); HC-033 is the compile-time event-schema payload check " +
				"— without this reference the spec omits the second enforcement surface that " +
				"prevents a registered event type from silently carrying a secret-named field " +
				"through an unredacted code path",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, "-", "_")), func(t *testing.T) {
			t.Parallel()
			if !hcInv003FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-INV-003 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-INV-003 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in HC-INV-003 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hcInv003FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-INV-003 check(8) FAILED: Tags: mechanism not found in HC-INV-003 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-INV-003 body)\n"+
					"  detail: HC-INV-003 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.66 audit complete — HC-INV-003 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
