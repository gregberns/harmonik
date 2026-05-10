package specaudit_test

// hk-sx9r.62 binding test — ON-051 multi-attach arbitration.
//
// Spec ref: specs/operator-nfr.md §4.10 ON-051.
//
// ON-051 states: multiple operators MAY attach simultaneously to the same daemon.
// Each attach session has a unique session_id; each operator-command emission carries
// the originating session_id. Concurrent commands are serialized per ON-011's mutex
// discipline; losers observe the post-winner state per ON-013c idempotency. Detach by
// one operator MUST NOT affect other attached operators or the daemon's state.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The multi-attach implementation is pending;
// this sensor verifies that ON-051 is correctly declared in the spec so that:
//
//  1. ON-051 heading is present in specs/operator-nfr.md.
//  2. "MAY attach simultaneously" permits multi-operator sessions.
//  3. "unique session_id" is required per attach session.
//  4. Operator-command emissions carry the originating session_id.
//  5. Concurrent commands are serialized per ON-011 mutex discipline.
//  6. "MUST NOT affect" other attached operators on detach.
//  7. Tags: mechanism is present in the ON-051 body window.
//
// # Failure modes
//
//   - ON-051 heading missing.
//   - MAY attach simultaneously absent.
//   - unique session_id absent.
//   - session_id carried on operator-command absent.
//   - ON-011 mutex serialization absent.
//   - MUST NOT affect other operators absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on051Fixture prefix per
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

// on051FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on051FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on051FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on051FixtureON051Heading matches the ON-051 level-4 requirement heading line.
var on051FixtureON051Heading = regexp.MustCompile(`^#### ON-051 —`)

// on051FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on051FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on051FixtureTagsMechanism matches a "Tags: mechanism" line.
var on051FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on051FixtureBodyWindow is the maximum number of lines after the ON-051
// heading to scan for requirement-body content.
const on051FixtureBodyWindow = 30

// on051FixtureLoadLines opens specFile and returns all lines.
func on051FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on051FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on051FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on051FixtureON051BodyLines returns the lines comprising the ON-051 body.
func on051FixtureON051BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on051FixtureON051Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-051 heading not found; expected '#### ON-051 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on051FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on051FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on051FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on051FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON051MultiAttachArbitration is the binding test for hk-sx9r.62.
func TestON051MultiAttachArbitration(t *testing.T) {
	t.Parallel()

	specFile := on051FixtureOperatorNFRPath(t)
	lines := on051FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on051FixtureON051BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-051 check(1): %s", reason)
	}
	t.Logf("ON-051 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "may-attach-simultaneously",
			needle: "MAY attach simultaneously",
			detail: "ON-051 body must declare 'MAY attach simultaneously' " +
				"(expected phrase 'MAY attach simultaneously'); this is the normative permission " +
				"for multi-operator attach — the daemon MUST support concurrent sessions without " +
				"requiring serialization at the attach level",
		},
		{
			id:     "3",
			label:  "unique-session-id-per-session",
			needle: "unique session_id",
			detail: "ON-051 body must require a 'unique session_id' per attach session " +
				"(expected phrase 'unique session_id'); session_id is the identity token that " +
				"distinguishes concurrent operator sessions from each other for command routing " +
				"and deduplication per ON-013c",
		},
		{
			id:     "4",
			label:  "session-id-carried-on-commands",
			needle: "originating session_id",
			detail: "ON-051 body must declare that operator-command emissions carry the originating " +
				"session_id (expected phrase 'originating session_id'); carrying the session_id " +
				"on every command emission enables audit-trail correlation per ON-039 and " +
				"deduplication per ON-013c",
		},
		{
			id:     "5",
			label:  "concurrent-commands-serialized-on011-mutex",
			needle: "ON-011",
			detail: "ON-051 body must cite 'ON-011' for concurrent command serialization " +
				"(expected phrase 'ON-011'); ON-011's single mutex is the mechanism that serializes " +
				"concurrent operator commands — losers observe the post-winner state rather than " +
				"seeing conflicting transitions",
		},
		{
			id:     "6",
			label:  "detach-must-not-affect-other-operators",
			needle: "MUST NOT affect",
			detail: "ON-051 body must declare 'MUST NOT affect' for detach isolation " +
				"(expected phrase 'MUST NOT affect'); when one operator detaches, the daemon " +
				"state and all remaining operator sessions MUST be unaffected — detach is " +
				"purely a session teardown, not a state-machine transition",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on051FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-051 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-051 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in ON-051 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on051FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-051 check(7) FAILED: Tags: mechanism not found in ON-051 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-051 body)\n"+
					"  detail: ON-051 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.62 audit complete — ON-051 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
