//go:build specaudit

package specaudit_test

// hk-sx9r.23 binding test — ON-019 migration releases are operator-paused boundaries.
//
// Spec ref: specs/operator-nfr.md §4.5 ON-019.
//
// ON-019 states: a migration release (any release that bumps an N-1-covered schema
// version to break the compat window) MUST require an operator pause before
// installation. The `harmonik upgrade` contract of §4.6 MUST refuse to exec-replace
// into a migration release unless the daemon is in the `paused` state AND the on-disk
// state's schema version is within the new binary's supported set. Installing a
// migration release MUST NOT auto-migrate on-disk state; a dedicated migration
// workflow (post-MVH) is the path.
//
// # Audit frame
//
// This test is a spec-text-binding sensor. The `harmonik upgrade` subcommand handler
// (including migration-release precondition checks) is not yet implemented; the sensor
// verifies that ON-019 is correctly and completely declared in the spec so that the
// normative phrases cannot be silently eroded by spec edits.
//
// Spec-text checks (7 assertions on the ON-019 body window):
//
//  1. ON-019 heading is present in specs/operator-nfr.md.
//  2. "migration release" is declared in the ON-019 body.
//  3. "operator pause" is required before installation.
//  4. "harmonik upgrade" contract must "refuse" exec-replace into a migration release.
//  5. "MUST refuse to exec-replace" into a migration release without the paused
//     precondition is declared.
//  6. "paused" state is named as the required daemon state.
//  7. "schema version" is named as the on-disk state compat check.
//  8. Tags: mechanism is present in the ON-019 body window.
//
// # Code-corpus check (TODO)
//
// TODO(hk-sx9r.24): once `harmonik upgrade` subcommand handler lands (tracked in
// hk-sx9r.24 "`harmonik upgrade` contract obligation"), add a code-corpus sensor
// asserting:
//   - cmd/harmonik/main.go or the upgrade handler package contains a call that
//     checks daemon-state == paused before exec-replacing.
//   - The handler contains a schema-version-within-supported-set check before
//     exec-replacing.
// Until hk-sx9r.24 is closed, the sensor is text-only.
//
// # Failure modes
//
//   - ON-019 heading missing.
//   - "migration release" absent from ON-019 body.
//   - "operator pause" absent from ON-019 body.
//   - "harmonik upgrade" contract reference absent.
//   - "MUST refuse" / "refuse" exec-replace precondition absent.
//   - "paused" state name absent.
//   - "schema version" absent.
//   - Tags: mechanism absent from ON-019 body.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on019Fixture prefix per
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

// on019FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on019FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on019FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on019FixtureON019Heading matches the ON-019 level-4 requirement heading line.
var on019FixtureON019Heading = regexp.MustCompile(`^#### ON-019 —`)

// on019FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on019FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on019FixtureTagsMechanism matches a "Tags: mechanism" line.
var on019FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on019FixtureBodyWindow is the maximum number of lines after the ON-019
// heading to scan for requirement-body content.
const on019FixtureBodyWindow = 15

// on019FixtureLoadLines opens specFile and returns all lines.
func on019FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on019FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on019FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on019FixtureON019BodyLines returns the lines comprising the ON-019 body
// (lines between the heading and the next heading, up to bodyWindow).
func on019FixtureON019BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on019FixtureON019Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-019 heading not found; expected '#### ON-019 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on019FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on019FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on019FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on019FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON019MigrationReleasePausedBoundary is the binding test for hk-sx9r.23.
func TestON019MigrationReleasePausedBoundary(t *testing.T) {
	t.Parallel()

	specFile := on019FixtureOperatorNFRPath(t)
	lines := on019FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on019FixtureON019BodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-019 check(1): %s", reason)
	}
	t.Logf("ON-019 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "migration-release-declared",
			needle: "migration release",
			detail: "ON-019 body must declare the term 'migration release' " +
				"(expected phrase 'migration release'); a migration release is the " +
				"normative unit of change that bumps an N-1-covered schema version " +
				"to break the compat window — this term is the boundary marker for " +
				"ON-019's precondition checks",
		},
		{
			id:     "3",
			label:  "operator-pause-required",
			needle: "operator pause",
			detail: "ON-019 body must require an 'operator pause' before installation " +
				"(expected phrase 'operator pause'); the operator pause is the " +
				"synchronisation barrier that prevents a migration release from " +
				"landing while the daemon has in-flight runs that cannot read the " +
				"new schema version",
		},
		{
			id:     "4",
			label:  "harmonik-upgrade-contract-referenced",
			needle: "harmonik upgrade",
			detail: "ON-019 body must reference the 'harmonik upgrade' contract " +
				"(expected phrase 'harmonik upgrade'); the upgrade contract of §4.6 " +
				"is the implementation surface that enforces the paused-precondition " +
				"check — without this cross-reference the contract gap is invisible",
		},
		{
			id:     "5",
			label:  "refuse-exec-replace-declared",
			needle: "refuse",
			detail: "ON-019 body must declare that `harmonik upgrade` MUST 'refuse' " +
				"to exec-replace into a migration release unless preconditions hold " +
				"(expected phrase 'refuse'); the refusal is the fail-closed gate — " +
				"without it an operator could install a migration release while the " +
				"daemon is running, silently breaking in-flight runs",
		},
		{
			id:     "6",
			label:  "paused-state-named",
			needle: "paused",
			detail: "ON-019 body must name the 'paused' state as the required daemon " +
				"state before a migration release can be installed " +
				"(expected phrase 'paused'); naming the specific state ties the " +
				"upgrade gate to the §7.1 state machine — only after the full " +
				"drain sequence (ON-027) has completed and the daemon has entered " +
				"`paused` may a migration release proceed",
		},
		{
			id:     "7",
			label:  "schema-version-check-declared",
			needle: "schema version",
			detail: "ON-019 body must declare a 'schema version' compatibility check " +
				"as an exec-replace precondition (expected phrase 'schema version'); " +
				"the schema-version check verifies that the on-disk state is within " +
				"the new binary's supported set — without it the daemon could exec-replace " +
				"to a binary that cannot read the existing on-disk state, causing " +
				"immediate startup failure with no safe rollback path",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on019FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-019 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-019 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (8): Tags: mechanism in ON-019 body.
	t.Run("check-8-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on019FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-019 check(8) FAILED: Tags: mechanism not found in ON-019 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-019 body)\n"+
					"  detail: ON-019 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.23 audit complete — ON-019 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
