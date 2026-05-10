package specaudit_test

// hk-sx9r.44 + hk-sx9r.45 binding test — ON-030 restart reconstruction path (git + Beads,
// no JSONL replay); ON-030a pause-state durable marker (.harmonik/daemon.state).
// Coalesced per §2.3 (consecutive headings in the same spec section, related scope).
//
// Spec ref: specs/operator-nfr.md §4.8 ON-030 and ON-030a.
//
// ON-030 states: daemon restart MUST reconstruct the in-memory model by walking the git
// checkpoint trail and querying Beads. The JSONL event log MUST NOT be replayed for state
// reconstruction (locked decision #12 — no DTW). Reconciliation workflows MUST spawn for
// in-flight runs.
//
// ON-030a states: the operator-control state machine MUST persist its current state via
// an atomic-written marker file `.harmonik/daemon.state` containing the current DaemonStatus
// plus pause-reason discriminator. Write MUST use temp+rename+fsync+parent-fsync. On daemon
// startup, PL-005 step 0 MUST read the marker; if paused/upgrade-prepare, daemon MUST
// initialize into that state. Marker MUST be removed on clean transition to running.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that ON-030 and ON-030a are correctly declared in the spec so that:
//
// ON-030 checks:
//  1. ON-030 heading is present in specs/operator-nfr.md.
//  2. "git checkpoint trail" is declared as the reconstruction source.
//  3. "MUST NOT be replayed" is declared for the JSONL event log.
//  4. Tags: mechanism is present in the ON-030 body window.
//
// ON-030a checks:
//  5. ON-030a heading is present in specs/operator-nfr.md.
//  6. ".harmonik/daemon.state" is declared as the marker file path.
//  7. "pause-reason discriminator" is named as a marker field.
//  8. Tags: mechanism is present in the ON-030a body window.
//
// # Failure modes
//
//   - ON-030 heading missing.
//   - git checkpoint trail absent.
//   - MUST NOT be replayed absent.
//   - ON-030 Tags: mechanism missing.
//   - ON-030a heading missing.
//   - .harmonik/daemon.state absent.
//   - pause-reason discriminator absent.
//   - ON-030a Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on030Fixture prefix per
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

// on030FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on030FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on030FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on030FixtureON030Heading matches the ON-030 level-4 requirement heading line.
var on030FixtureON030Heading = regexp.MustCompile(`^#### ON-030 —`)

// on030FixtureON030aHeading matches the ON-030a level-4 requirement heading line.
var on030FixtureON030aHeading = regexp.MustCompile(`^#### ON-030a —`)

// on030FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on030FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on030FixtureTagsMechanism matches a "Tags: mechanism" line.
var on030FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on030FixtureBodyWindow is the maximum number of lines to scan after each heading.
const on030FixtureBodyWindow = 15

// on030FixtureLoadLines opens specFile and returns all lines.
func on030FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on030FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on030FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on030FixtureON030BodyLines returns the lines comprising the ON-030 body.
func on030FixtureON030BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on030FixtureON030Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-030 heading not found; expected '#### ON-030 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on030FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on030FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on030FixtureON030aBodyLines returns the lines comprising the ON-030a body.
func on030FixtureON030aBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on030FixtureON030aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-030a heading not found; expected '#### ON-030a —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on030FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on030FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on030FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on030FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON030RestartReconstructionAndMarker is the binding test for hk-sx9r.44 (ON-030) and
// hk-sx9r.45 (ON-030a), coalesced per §2.3.
func TestON030RestartReconstructionAndMarker(t *testing.T) {
	t.Parallel()

	specFile := on030FixtureOperatorNFRPath(t)
	lines := on030FixtureLoadLines(t, specFile)

	// --- ON-030 checks ---

	on030Body, on030LineNo, on030Reason := on030FixtureON030BodyLines(lines)
	if on030Reason != "" {
		t.Fatalf("ON-030 check(1): %s", on030Reason)
	}
	t.Logf("ON-030 heading found at specs/operator-nfr.md line %d; body window = %d lines",
		on030LineNo, len(on030Body))

	// --- ON-030a checks ---

	on030aBody, on030aLineNo, on030aReason := on030FixtureON030aBodyLines(lines)
	if on030aReason != "" {
		t.Fatalf("ON-030a check(5): %s", on030aReason)
	}
	t.Logf("ON-030a heading found at specs/operator-nfr.md line %d; body window = %d lines",
		on030aLineNo, len(on030aBody))

	type check struct {
		id     string
		label  string
		needle string
		detail string
		body   []string
		lineNo int
		req    string
	}

	checks := []check{
		{
			id:     "2",
			label:  "git-checkpoint-trail-reconstruction",
			needle: "git checkpoint trail",
			detail: "ON-030 body must declare 'git checkpoint trail' as the reconstruction source " +
				"(expected phrase 'git checkpoint trail'); walking the git checkpoint trail is one " +
				"of two sources for restart reconstruction — the other is Beads queries; together " +
				"they fully reconstruct the in-memory model without replaying JSONL events",
			body:   on030Body,
			lineNo: on030LineNo,
			req:    "ON-030",
		},
		{
			id:     "3",
			label:  "jsonl-must-not-be-replayed",
			needle: "MUST NOT be replayed",
			detail: "ON-030 body must declare the JSONL event log 'MUST NOT be replayed' " +
				"(expected phrase 'MUST NOT be replayed'); this is the negative constraint that " +
				"implements locked decision #12 (no DTW) — the JSONL log is the durable on-disk " +
				"form but is not the state-reconstruction source; git + Beads own that role",
			body:   on030Body,
			lineNo: on030LineNo,
			req:    "ON-030",
		},
		{
			id:     "6",
			label:  "daemon-state-marker-path",
			needle: ".harmonik/daemon.state",
			detail: "ON-030a body must declare '.harmonik/daemon.state' as the marker file path " +
				"(expected phrase '.harmonik/daemon.state'); this is the well-known path for the " +
				"pause-state durable marker — PL-005 step 0 reads it on startup to restore " +
				"the operator-control state machine's paused/upgrade-prepare state across crashes",
			body:   on030aBody,
			lineNo: on030aLineNo,
			req:    "ON-030a",
		},
		{
			id:     "7",
			label:  "pause-reason-discriminator",
			needle: "pause-reason discriminator",
			detail: "ON-030a body must name 'pause-reason discriminator' as a marker field " +
				"(expected phrase 'pause-reason discriminator'); the discriminator is one of " +
				"operator/improvement/upgrade-prepare — it lets the daemon restore not just the " +
				"paused state but also WHY it paused, which affects what actions are permitted " +
				"while paused",
			body:   on030aBody,
			lineNo: on030aLineNo,
			req:    "ON-030a",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on030FixtureBodyContains(c.body, c.needle) {
				t.Errorf(
					"%s check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (%s body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.req, c.id, c.label, c.lineNo, c.req, c.needle, c.detail,
				)
			}
		})
	}

	// Check (4): Tags: mechanism in ON-030 body.
	t.Run("check-4-on030-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range on030Body {
			if on030FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-030 check(4) FAILED: Tags: mechanism not found in ON-030 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-030 body)\n"+
					"  detail: ON-030 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				on030LineNo,
			)
		}
	})

	// Check (8): Tags: mechanism in ON-030a body.
	t.Run("check-8-on030a-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range on030aBody {
			if on030FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-030a check(8) FAILED: Tags: mechanism not found in ON-030a body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-030a body)\n"+
					"  detail: ON-030a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				on030aLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.44/.45 audit complete — ON-030 at line %d, ON-030a at line %d",
		on030LineNo, on030aLineNo)
}
