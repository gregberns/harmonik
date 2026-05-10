package specaudit_test

// hk-hqwn.12 binding test — EV-009 subscription is declared at registration, not inferred.
//
// Spec ref: specs/event-model.md §4.2 EV-009.
//
// EV-009 states: a consumer MUST register its subscription with the bus at
// daemon startup, declaring (a) the event types it consumes, (b) its class,
// (c) its consumer identifier. Dynamic mid-run subscription is forbidden; the
// bus is sealed after `daemon.Start()` returns, and post-seal `Subscribe`
// calls MUST return a typed sealed-bus error.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The EventBus implementation is pending;
// this sensor verifies that the EV-009 requirement is correctly declared in
// the spec so that:
//
//  1. EV-009 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. The three-part subscription declaration is present — consumers declare
//     (a) event types, (b) class, and (c) consumer identifier at registration.
//
//  3. Dynamic mid-run subscription is explicitly forbidden — the spec names
//     "dynamic mid-run subscription" or equivalent as forbidden.
//
//  4. Bus-seal after daemon.Start() is declared — the spec names
//     `daemon.Start()` as the seal point.
//
//  5. Post-seal Subscribe returns a typed sealed-bus error — the spec names
//     a typed error for post-seal Subscribe calls.
//
//  6. Tags: mechanism is present in the EV-009 body window.
//
// # Failure modes
//
//   - EV-009 heading missing: EV-009 heading not found in specs/event-model.md.
//   - Three-part declaration absent: event types / class / consumer identifier
//     not declared in EV-009 body.
//   - Dynamic mid-run prohibition absent: spec does not forbid mid-run subscription.
//   - daemon.Start() seal point absent: seal timing not declared.
//   - Typed sealed-bus error absent: post-seal error type not declared.
//   - Tags: mechanism missing from EV-009 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn12Fixture prefix per
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

// hqwn12FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn12_subscription_declared_test.go
//
// so the repo root is two directories up.
func hqwn12FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn12FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn12FixtureEV009Heading matches the EV-009 level-4 requirement heading line.
var hqwn12FixtureEV009Heading = regexp.MustCompile(`^#### EV-009 —`)

// hqwn12FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-009 requirement body window.
var hqwn12FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn12FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn12FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn12FixtureBodyWindow is the maximum number of lines after the EV-009
// heading to scan for requirement-body content.
const hqwn12FixtureBodyWindow = 30

// hqwn12FixtureLoadLines opens specFile and returns all lines.
func hqwn12FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn12FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn12FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn12FixtureEV009BodyLines returns the lines comprising the EV-009
// requirement body: all lines after the EV-009 heading up to (but not
// including) the next Markdown heading or hqwn12FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-009 heading is not found.
func hqwn12FixtureEV009BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn12FixtureEV009Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-009 heading not found; expected '#### EV-009 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn12FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn12FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn12FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn12FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN12SubscriptionDeclaredAtRegistration is the binding test for hk-hqwn.12.
//
// It opens specs/event-model.md, locates the EV-009 heading, and validates:
//
//	(a) The EV-009 heading is present (the requirement exists in the spec).
//	(b) Three-part declaration: event types, class, consumer identifier.
//	(c) Dynamic mid-run subscription is forbidden.
//	(d) Bus is sealed after daemon.Start() returns.
//	(e) Post-seal Subscribe calls MUST return a typed sealed-bus error.
//	(f) Tags: mechanism is present in the body window.
func TestHQWN12SubscriptionDeclaredAtRegistration(t *testing.T) {
	t.Parallel()

	specFile := hqwn12FixtureEventModelPath(t)
	lines := hqwn12FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn12FixtureEV009BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-009 check(a): %s", reason)
	}
	t.Logf("EV-009 heading found at specs/event-model.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:     "b1",
			label:  "three-part declaration event-types",
			needle: "event types",
			detail: "EV-009 body must declare that consumers register the event types they consume " +
				"(expected phrase 'event types'); this is part (a) of the required three-part " +
				"subscription declaration",
		},
		{
			id:     "b2",
			label:  "three-part declaration class",
			needle: "its class",
			detail: "EV-009 body must declare that consumers register their class " +
				"(expected phrase 'its class'); this is part (b) of the required three-part " +
				"subscription declaration",
		},
		{
			id:     "b3",
			label:  "three-part declaration consumer identifier",
			needle: "consumer identifier",
			detail: "EV-009 body must declare that consumers register their consumer identifier " +
				"(expected phrase 'consumer identifier'); this is part (c) of the required " +
				"three-part subscription declaration",
		},
		{
			id:     "c",
			label:  "dynamic mid-run subscription forbidden",
			needle: "Dynamic mid-run",
			detail: "EV-009 body must declare that dynamic mid-run subscription is forbidden " +
				"(expected phrase 'Dynamic mid-run'); this is the prohibition that prevents " +
				"late-subscription race conditions",
		},
		{
			id:     "d",
			label:  "bus sealed after daemon.Start() returns",
			needle: "daemon.Start()",
			detail: "EV-009 body must name `daemon.Start()` as the seal point " +
				"(expected phrase 'daemon.Start()'); this anchors the registration window " +
				"to a concrete lifecycle boundary",
		},
		{
			id:     "e",
			label:  "post-seal Subscribe returns typed sealed-bus error",
			needle: "sealed-bus error",
			detail: "EV-009 body must declare that post-seal Subscribe calls MUST return a typed " +
				"sealed-bus error (expected phrase 'sealed-bus error'); this is the enforcement " +
				"mechanism that prevents runtime subscription",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn12FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-009 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-009 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (f): Tags: mechanism in EV-009 body.
	t.Run("check-f-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn12FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-009 check(f) FAILED: Tags: mechanism not found in EV-009 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-009 body)\n"+
					"  detail: EV-009 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.12 audit complete — EV-009 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
