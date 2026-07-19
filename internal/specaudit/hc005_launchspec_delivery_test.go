//go:build specaudit

package specaudit_test

// hk-8i31.5 binding test — HC-005 LaunchSpec delivery is JSON-on-stdin by default;
// file-path when over 1 MiB.
//
// Spec ref: specs/handler-contract.md §4.2 HC-005.
//
// HC-005 states: the daemon MUST deliver the LaunchSpec to the handler subprocess via ONE
// of two mechanisms: (a) JSON on stdin (default, for LaunchSpec payloads ≤ 1 MiB), or
// (b) a file-path argument --launch-spec <path> pointing at a JSON file (for LaunchSpec
// payloads > 1 MiB). The handler MUST accept both forms. Selection MUST be driven by
// payload size at call time, NOT by handler type.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler launch implementation is pending; this
// sensor verifies that HC-005 is correctly declared in the spec so that:
//
//  1. HC-005 heading is present in specs/handler-contract.md.
//  2. "JSON on stdin" is named as the default delivery mechanism.
//  3. "1 MiB" is declared as the size threshold for both delivery paths.
//  4. "--launch-spec <path>" file argument is named as the alternative.
//  5. "handler MUST accept both forms" is declared.
//  6. "payload size" (NOT handler type) drives selection.
//  7. Tags: mechanism is present in the HC-005 body window.
//
// # Failure modes
//
//   - HC-005 heading missing.
//   - JSON on stdin absent.
//   - 1 MiB threshold absent.
//   - --launch-spec file argument absent.
//   - handler MUST accept both forms absent.
//   - NOT by handler type absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc005Fixture prefix per
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

// hc005FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc005FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc005FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc005FixtureHC005Heading matches the HC-005 level-4 requirement heading line.
var hc005FixtureHC005Heading = regexp.MustCompile(`^#### HC-005 —`)

// hc005FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc005FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc005FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc005FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc005FixtureBodyWindow is the maximum number of lines after the HC-005
// heading to scan for requirement-body content.
const hc005FixtureBodyWindow = 30

// hc005FixtureLoadLines opens specFile and returns all lines.
func hc005FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc005FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc005FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc005FixtureHC005BodyLines returns the lines comprising the HC-005 body.
func hc005FixtureHC005BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc005FixtureHC005Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-005 heading not found; expected '#### HC-005 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc005FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc005FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc005FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc005FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC005LaunchSpecDeliveryStdinOrFile is the binding test for hk-8i31.5.
func TestHC005LaunchSpecDeliveryStdinOrFile(t *testing.T) {
	t.Parallel()

	specFile := hc005FixtureHandlerContractPath(t)
	lines := hc005FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc005FixtureHC005BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-005 check(1): %s", reason)
	}
	t.Logf("HC-005 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "json-on-stdin-default",
			needle: "JSON on stdin",
			detail: "HC-005 body must name 'JSON on stdin' as the default delivery mechanism " +
				"(expected phrase 'JSON on stdin'); stdin is the default because it requires no " +
				"temp file management and works for the common case of small LaunchSpecs; " +
				"handlers receive the entire spec before they start — no streaming",
		},
		{
			id:     "3",
			label:  "one-mib-size-threshold",
			needle: "1 MiB",
			detail: "HC-005 body must declare '1 MiB' as the size threshold for delivery mode selection " +
				"(expected phrase '1 MiB'); LaunchSpecs ≤ 1 MiB use stdin; larger payloads use " +
				"the file-path argument — this threshold aligns with NDJSON line-length cap in HC-007a",
		},
		{
			id:     "4",
			label:  "launch-spec-file-argument",
			needle: "--launch-spec",
			detail: "HC-005 body must name '--launch-spec' as the file-path argument " +
				"(expected phrase '--launch-spec'); this is the CLI flag the daemon passes when " +
				"the LaunchSpec exceeds 1 MiB — the handler reads the JSON from the named file " +
				"path rather than from stdin",
		},
		{
			id:     "5",
			label:  "handler-must-accept-both-forms",
			needle: "handler MUST accept both",
			detail: "HC-005 body must declare 'handler MUST accept both' forms " +
				"(expected phrase 'handler MUST accept both'); handlers MUST NOT assume they will " +
				"always receive stdin delivery — a handler that only accepts stdin would fail for " +
				"large LaunchSpecs from high-skill-set workflows",
		},
		{
			id:     "6",
			label:  "not-by-handler-type",
			needle: "NOT by handler type",
			detail: "HC-005 body must declare selection is 'NOT by handler type' " +
				"(expected phrase 'NOT by handler type'); delivery mode is determined solely by " +
				"payload size at call time — the daemon MUST NOT hard-code delivery mode per " +
				"handler class, which would prevent generic handlers from receiving large specs",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc005FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-005 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-005 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in HC-005 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc005FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-005 check(7) FAILED: Tags: mechanism not found in HC-005 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-005 body)\n"+
					"  detail: HC-005 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.5 audit complete — HC-005 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
