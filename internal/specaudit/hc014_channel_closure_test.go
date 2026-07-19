//go:build specaudit

package specaudit_test

// hk-8i31.17 binding test — HC-014 channel closure rule.
//
// Spec ref: specs/handler-contract.md §4.3 HC-014.
//
// HC-014 states: "An emitter of a Go channel used across subsystem boundaries
// MUST close the channel on end-of-stream. Consumers MUST treat a closed
// channel as end-of-stream, NOT as error. This rule applies to the
// watcher-to-event-bus publication channel and to any future cross-subsystem
// channel."
//
// # What this test verifies
//
// This is a spec-corpus binding test. The watcher-to-event-bus channel is not
// yet implemented in Go code; the rule is a forward-looking contract that will
// govern every future cross-subsystem channel emitter/consumer pair. The test
// encodes the rule so that:
//
//  (a) The HC-014 requirement heading is present in specs/handler-contract.md.
//  (b) Both MUST obligations are stated in the requirement body:
//      - emitter-close:  "MUST close" (or "close the channel on end-of-stream")
//      - consumer-EOS:   "MUST treat a closed channel as end-of-stream"
//      - consumer-not-error: the NOT-as-error clause is present (prevents the
//        common misread that a channel-close should raise an error)
//  (c) The watcher-to-event-bus channel is named as the concrete instance.
//  (d) "Tags: mechanism" is present within the requirement body window.
//
// Failure in any check means the spec has drifted from the HC-014 contract
// that downstream implementers depend on.
//
// # Why no Go-code structural check
//
// There is no watcher package or event-bus channel type in internal/ at this
// time. The enforcement surface is the spec itself. When the channel
// implementation lands it will be covered by its own integration tests; this
// bead encodes only the contract declaration as a spec-corpus sensor.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc014Fixture prefix per
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

// hc014FixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md by walking up from this test file's source path.
// The test file lives at:
//
//	internal/specaudit/hc014_channel_closure_test.go
//
// so the repo root is two directories up.
func hc014FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc014FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc014FixtureHC014Heading matches the HC-014 level-4 requirement heading line.
var hc014FixtureHC014Heading = regexp.MustCompile(`^#### HC-014 —`)

// hc014FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-014 requirement body window.
var hc014FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc014FixtureTagsMechanism matches a "Tags: mechanism" line (the only tag
// value HC-014 is required to carry per the spec).
var hc014FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc014FixtureBodyWindow is the maximum number of lines after the HC-014
// heading to scan for requirement-body content. Matches the 30-line cap used
// by sibling specaudit tests.
const hc014FixtureBodyWindow = 30

// hc014FixtureLoadLines opens specFile and returns all lines.
func hc014FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc014FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc014FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc014FixtureBodyLines returns the lines comprising the HC-014 requirement
// body: all lines after the HC-014 heading line up to (but not including) the
// next Markdown heading or hc014FixtureBodyWindow lines, whichever comes first.
//
// Returns nil and a non-empty reason string if the HC-014 heading is not found.
func hc014FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc014FixtureHC014Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-014 heading not found; expected '#### HC-014 — Channel closure rule' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc014FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hc014FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, "" // headingLineNo is 1-based
}

// hc014FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hc014FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC014ChannelClosureRulePresent is the binding test for HC-014.
//
// It opens specs/handler-contract.md, locates the HC-014 heading, and
// validates:
//
//	(a) The HC-014 heading is present (the rule exists in the spec).
//	(b) The emitter-close obligation: "MUST close" appears in the body.
//	(c) The consumer-EOS obligation: "MUST treat a closed channel as
//	    end-of-stream" (or equivalent) appears in the body.
//	(d) The NOT-as-error clause: "NOT as error" (or "not as error") appears
//	    in the body, ruling out silent channel-close→error misinterpretation.
//	(e) The named concrete instance: "watcher-to-event-bus" appears in the
//	    body, confirming the rule is grounded to the specific channel.
//	(f) "Tags: mechanism" is present in the body window.
func TestHC014ChannelClosureRulePresent(t *testing.T) {
	t.Parallel()

	specFile := hc014FixtureHandlerContractPath(t)
	lines := hc014FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc014FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-014 check(a): %s", reason)
	}
	t.Logf("HC-014 heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "emitter-close obligation",
			needle: "must close",
			detail: "HC-014 body must state that the emitter MUST close the channel on end-of-stream " +
				"(expected phrase 'MUST close' or 'must close'); " +
				"this obligation is the load-bearing contract for emitter implementers",
		},
		{
			id:     "c",
			label:  "consumer-EOS obligation",
			needle: "must treat a closed channel as end-of-stream",
			detail: "HC-014 body must state that consumers MUST treat a closed channel as end-of-stream " +
				"(expected phrase 'MUST treat a closed channel as end-of-stream'); " +
				"this obligation governs consumer-side handling",
		},
		{
			id:     "d",
			label:  "NOT-as-error clause",
			needle: "not as error",
			detail: "HC-014 body must include the NOT-as-error clause " +
				"(expected phrase 'NOT as error' or 'not as error'); " +
				"this clause prevents the common misread that a channel close should be treated as an error condition",
		},
		{
			id:     "e",
			label:  "named concrete instance",
			needle: "watcher-to-event-bus",
			detail: "HC-014 body must name the watcher-to-event-bus publication channel as the concrete instance " +
				"(expected phrase 'watcher-to-event-bus'); " +
				"this grounds the abstract rule to the specific channel it governs",
		},
	}

	// Run checks b–e as sub-tests.
	var failures []string
	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc014FixtureBodyContains(body, c.needle) {
				msg := fmt.Sprintf(
					"HC-014 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-014 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
				t.Error(msg)
				failures = append(failures, c.id)
			}
		})
	}

	// Check (f): Tags: mechanism
	t.Run("check-f-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc014FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-014 check(f) FAILED: Tags: mechanism not found in HC-014 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-014 body)\n"+
					"  detail: HC-014 carries tag 'mechanism' per the spec; its absence indicates the"+
					" requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	if len(failures) == 0 {
		t.Logf("HC-014 audit: all checks pass — channel closure rule is correctly declared in specs/handler-contract.md "+
			"(heading at line %d, body = %d lines)",
			headingLineNo, len(body))
	}
}
