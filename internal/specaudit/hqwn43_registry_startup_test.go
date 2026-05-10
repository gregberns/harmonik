package specaudit_test

// hk-hqwn.43 binding test — EV-034 registry registration is startup-time.
//
// Spec ref: specs/event-model.md §4.9 EV-034.
//
// EV-034 states: payload-type registration MUST happen at daemon init (via
// `init()` functions or `RegisterEventType` calls during startup). Runtime
// registration after the first event is emitted MUST be a startup-time error.
// The registry is sealed at the same time the bus is sealed (EV-009).
//
// # Audit frame
//
// This test is a spec-corpus sensor. The payload-type registry implementation
// is pending; this sensor verifies that the EV-034 requirement is correctly
// declared in the spec so that:
//
//  1. EV-034 heading is present in specs/event-model.md — the requirement exists
//     in the normative spec.
//
//  2. Daemon-init registration is declared — the spec names `init()` functions
//     or `RegisterEventType` calls during startup as the required registration
//     mechanism.
//
//  3. Runtime-registration prohibition is declared — registration after the first
//     event is emitted MUST be a startup-time error.
//
//  4. Registry-seal coupling to bus-seal is declared — the registry is sealed at
//     the same time the bus is sealed, and EV-009 is cited.
//
//  5. Tags: mechanism is present in the EV-034 body window.
//
// # Failure modes
//
//   - EV-034 heading missing: EV-034 heading not found in specs/event-model.md.
//   - Daemon-init registration absent: `init()` / `RegisterEventType` not stated.
//   - Runtime-registration prohibition absent: startup-time error not declared.
//   - Registry-seal / bus-seal coupling absent: EV-009 not cited.
//   - Tags: mechanism missing from EV-034 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn43Fixture prefix per
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

// hqwn43FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn43_registry_startup_test.go
//
// so the repo root is two directories up.
func hqwn43FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn43FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn43FixtureEV034Heading matches the EV-034 level-4 requirement heading line.
var hqwn43FixtureEV034Heading = regexp.MustCompile(`^#### EV-034 —`)

// hqwn43FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-034 requirement body window.
var hqwn43FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn43FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn43FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn43FixtureBodyWindow is the maximum number of lines after the EV-034
// heading to scan for requirement-body content.
const hqwn43FixtureBodyWindow = 30

// hqwn43FixtureLoadLines opens specFile and returns all lines.
func hqwn43FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn43FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn43FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn43FixtureEV034BodyLines returns the lines comprising the EV-034 requirement
// body: all lines after the EV-034 heading line up to (but not including) the
// next Markdown heading or hqwn43FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-034 heading is not found.
func hqwn43FixtureEV034BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn43FixtureEV034Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-034 heading not found; expected '#### EV-034 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn43FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn43FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn43FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn43FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN43RegistryRegistrationIsStartupTime is the binding test for hk-hqwn.43.
//
// It opens specs/event-model.md, locates the EV-034 heading, and validates:
//
//	(a) The EV-034 heading is present (the requirement exists in the spec).
//	(b) Daemon-init registration is declared: `init()` / `RegisterEventType`.
//	(c) Runtime-registration prohibition: startup-time error declared.
//	(d) Registry-seal coupling to bus-seal: EV-009 cited.
//	(e) Tags: mechanism is present in the body window.
func TestHQWN43RegistryRegistrationIsStartupTime(t *testing.T) {
	t.Parallel()

	specFile := hqwn43FixtureEventModelPath(t)
	lines := hqwn43FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn43FixtureEV034BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-034 check(a): %s", reason)
	}
	t.Logf("EV-034 heading found at specs/event-model.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "b",
			label:  "daemon-init registration via init() functions",
			needle: "init()",
			detail: "EV-034 body must declare that payload-type registration happens via `init()` " +
				"functions (expected phrase 'init()'); this is the canonical Go startup-time " +
				"registration mechanism and must be normatively named",
		},
		{
			id:     "b2",
			label:  "daemon-init registration via RegisterEventType",
			needle: "RegisterEventType",
			detail: "EV-034 body must name `RegisterEventType` as the explicit registration call " +
				"(expected phrase 'RegisterEventType'); this is the other normative registration " +
				"mechanism alongside init() functions",
		},
		{
			id:     "c",
			label:  "runtime-registration startup-time error",
			needle: "startup-time error",
			detail: "EV-034 body must declare that runtime registration after the first event is " +
				"emitted MUST be a startup-time error (expected phrase 'startup-time error'); " +
				"this is the enforcement mechanism that prevents late registration",
		},
		{
			id:     "d",
			label:  "registry-seal coupling to bus-seal EV-009",
			needle: "EV-009",
			detail: "EV-034 body must cite EV-009 for the registry-seal / bus-seal coupling " +
				"(expected phrase 'EV-009'); the registry is sealed at the same time the bus is " +
				"sealed, and the cross-reference makes the relationship normative",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn43FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-034 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-034 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (e): Tags: mechanism in EV-034 body.
	t.Run("check-e-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn43FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-034 check(e) FAILED: Tags: mechanism not found in EV-034 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-034 body)\n"+
					"  detail: EV-034 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.43 audit complete — EV-034 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
