package specaudit_test

// hk-hqwn.16 binding test — EV-012 observer consumer class.
//
// Spec ref: specs/event-model.md §4.2 EV-012.
//
// EV-012 states: an `observer` is a passive consumer whose failures MUST NOT
// produce bus events or side effects beyond its own local logging. Observers
// MUST NOT mutate persistent state. Enforcement is by coding discipline plus a
// `depguard` rule: observer packages MUST NOT import state-mutating packages
// (git, Beads, workspace-manager). If an observer needs to mutate state, it
// MUST re-register as asynchronous.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The observer consumer class implementation
// is pending; this sensor verifies that the EV-012 requirement is correctly
// declared in the spec so that:
//
//  1. EV-012 heading is present in specs/event-model.md — the requirement
//     exists in the normative spec.
//
//  2. Failure isolation is declared — observer failures MUST NOT produce bus
//     events or side effects beyond local logging.
//
//  3. State-mutation prohibition is declared — observers MUST NOT mutate
//     persistent state.
//
//  4. depguard enforcement is named — the `depguard` rule is the enforcement
//     mechanism.
//
//  5. Banned import list names git — state-mutating package git MUST NOT be
//     imported by observer packages.
//
//  6. Banned import list names Beads — state-mutating package Beads MUST NOT
//     be imported by observer packages.
//
//  7. Banned import list names workspace-manager — state-mutating package
//     workspace-manager MUST NOT be imported by observer packages.
//
//  8. Re-register as asynchronous path is declared — an observer needing
//     state mutation MUST re-register as asynchronous.
//
//  9. Tags: mechanism is present in the EV-012 body window.
//
// # Failure modes
//
//   - EV-012 heading missing: EV-012 heading not found in specs/event-model.md.
//   - Failure isolation absent: observer failure isolation not declared.
//   - State-mutation prohibition absent: MUST NOT mutate persistent state not declared.
//   - depguard enforcement absent: `depguard` rule not named.
//   - Banned import git absent: git not in banned import list.
//   - Banned import Beads absent: Beads not in banned import list.
//   - Banned import workspace-manager absent: workspace-manager not in banned list.
//   - Re-register-asynchronous path absent: mutation escape path not declared.
//   - Tags: mechanism missing from EV-012 body window.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn16Fixture prefix per
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

// hqwn16FixtureEventModelPath returns the absolute path to specs/event-model.md
// by walking up from this test file's source path. The test file lives at:
//
//	internal/specaudit/hqwn16_observer_consumer_class_test.go
//
// so the repo root is two directories up.
func hqwn16FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn16FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn16FixtureEV012Heading matches the EV-012 level-4 requirement heading line.
var hqwn16FixtureEV012Heading = regexp.MustCompile(`^#### EV-012 —`)

// hqwn16FixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the EV-012 requirement body window.
var hqwn16FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn16FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn16FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn16FixtureBodyWindow is the maximum number of lines after the EV-012
// heading to scan for requirement-body content.
const hqwn16FixtureBodyWindow = 30

// hqwn16FixtureLoadLines opens specFile and returns all lines.
func hqwn16FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn16FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn16FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hqwn16FixtureEV012BodyLines returns the lines comprising the EV-012
// requirement body: all lines after the EV-012 heading up to (but not
// including) the next Markdown heading or hqwn16FixtureBodyWindow lines,
// whichever comes first.
//
// Returns nil and a non-empty reason string if the EV-012 heading is not found.
func hqwn16FixtureEV012BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hqwn16FixtureEV012Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "EV-012 heading not found; expected '#### EV-012 —' in specs/event-model.md"
	}

	limit := headingIdx + 1 + hqwn16FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		// Stop at the next Markdown heading (the next requirement).
		if hqwn16FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hqwn16FixtureBodyContains reports whether any line in body contains substr
// (case-insensitive).
func hqwn16FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHQWN16ObserverConsumerClass is the binding test for hk-hqwn.16.
//
// It opens specs/event-model.md, locates the EV-012 heading, and validates:
//
//	(1) The EV-012 heading is present (the requirement exists in the spec).
//	(2) Observer failure isolation is declared.
//	(3) State-mutation prohibition is declared.
//	(4) depguard enforcement is named.
//	(5) git is in the banned import list.
//	(6) Beads is in the banned import list.
//	(7) workspace-manager is in the banned import list.
//	(8) Re-register-as-asynchronous escape path is declared.
//	(9) Tags: mechanism is present in the body window.
func TestHQWN16ObserverConsumerClass(t *testing.T) {
	t.Parallel()

	specFile := hqwn16FixtureEventModelPath(t)
	lines := hqwn16FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hqwn16FixtureEV012BodyLines(lines)
	if reason != "" {
		t.Fatalf("EV-012 check(1): %s", reason)
	}
	t.Logf("EV-012 heading found at specs/event-model.md line %d; body window = %d lines",
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
			label:  "observer failure isolation no-bus-events",
			needle: "MUST NOT produce bus events",
			detail: "EV-012 body must declare that observer failures MUST NOT produce bus events " +
				"(expected phrase 'MUST NOT produce bus events'); this is the core isolation " +
				"contract that prevents observer failures from cascading into bus events",
		},
		{
			id:     "3",
			label:  "state-mutation prohibition",
			needle: "MUST NOT mutate persistent state",
			detail: "EV-012 body must declare that observers MUST NOT mutate persistent state " +
				"(expected phrase 'MUST NOT mutate persistent state'); this is the invariant " +
				"that enforces the observer's passive read-only role",
		},
		{
			id:     "4",
			label:  "depguard enforcement named",
			needle: "depguard",
			detail: "EV-012 body must name the `depguard` rule as the enforcement mechanism " +
				"(expected phrase 'depguard'); this is the static analysis tool that enforces " +
				"the observer import prohibition at build time",
		},
		{
			id:     "5",
			label:  "banned import git",
			needle: "git",
			detail: "EV-012 body must list 'git' as a state-mutating package that observer packages " +
				"MUST NOT import (expected phrase 'git'); this prevents observers from performing " +
				"git operations that would mutate project state",
		},
		{
			id:     "6",
			label:  "banned import Beads",
			needle: "Beads",
			detail: "EV-012 body must list 'Beads' as a state-mutating package that observer packages " +
				"MUST NOT import (expected phrase 'Beads'); this prevents observers from writing to " +
				"the Beads task ledger",
		},
		{
			id:     "7",
			label:  "banned import workspace-manager",
			needle: "workspace-manager",
			detail: "EV-012 body must list 'workspace-manager' as a state-mutating package that " +
				"observer packages MUST NOT import (expected phrase 'workspace-manager'); this " +
				"prevents observers from performing workspace operations",
		},
		{
			id:     "8",
			label:  "re-register as asynchronous escape path",
			needle: "re-register as asynchronous",
			detail: "EV-012 body must declare that an observer needing state mutation MUST " +
				"re-register as asynchronous (expected phrase 're-register as asynchronous'); " +
				"this is the normative escape path that avoids silent observer mutation",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hqwn16FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"EV-012 check(%s) FAILED: %s\n"+
						"  spec:    specs/event-model.md line ~%d (EV-012 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (9): Tags: mechanism in EV-012 body.
	t.Run("check-9-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hqwn16FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"EV-012 check(9) FAILED: Tags: mechanism not found in EV-012 body window\n"+
					"  spec:   specs/event-model.md line ~%d (EV-012 body)\n"+
					"  detail: EV-012 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-hqwn.16 audit complete — EV-012 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
