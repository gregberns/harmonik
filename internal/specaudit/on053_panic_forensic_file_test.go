package specaudit_test

// hk-sx9r.49 binding test — ON-053 post-panic forensic file written by the panic barrier.
//
// Spec ref: specs/operator-nfr.md §4.9 ON-053.
//
// ON-053 states: when the daemon's top-level panic barrier (PL-018a) intercepts a panic
// and exits with §8 code 19 (runtime-panic), the daemon MUST atomically write a forensic
// file to `.harmonik/panic-<timestamp>.log` containing: (a) Go runtime panic message +
// stack trace; (b) PID, PGID, project_hash, binary commit hash; (c) wall-clock + monotonic
// timestamps; (d) last-emitted run_id / node_id / event_id (best-effort). Write MUST
// follow temp+rename+fsync+parent-fsync atomicity discipline of WM-026.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that ON-053 is correctly declared in the spec so that:
//
//  1. ON-053 heading is present in specs/operator-nfr.md.
//  2. "panic-<timestamp>.log" file path pattern is declared.
//  3. "project_hash" is named as a content field.
//  4. "time.Since(boot)" monotonic form is declared.
//  5. "temp+rename+fsync" atomicity discipline is declared.
//  6. Tags: mechanism is present in the ON-053 body window.
//
// # Failure modes
//
//   - ON-053 heading missing.
//   - panic-<timestamp>.log pattern absent.
//   - project_hash absent.
//   - time.Since(boot) absent.
//   - temp+rename+fsync absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the on053Fixture prefix per
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

// on053FixtureOperatorNFRPath returns the absolute path to specs/operator-nfr.md.
func on053FixtureOperatorNFRPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("on053FixtureOperatorNFRPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "operator-nfr.md")
}

// on053FixtureHeading matches the ON-053 level-4 requirement heading line.
var on053FixtureHeading = regexp.MustCompile(`^#### ON-053 —`)

// on053FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var on053FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// on053FixtureTagsMechanism matches a "Tags: mechanism" line.
var on053FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// on053FixtureBodyWindow is the maximum number of lines to scan after the heading.
const on053FixtureBodyWindow = 15

// on053FixtureLoadLines opens specFile and returns all lines.
func on053FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("on053FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("on053FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// on053FixtureBodyLines returns the lines comprising the ON-053 body.
func on053FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if on053FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "ON-053 heading not found; expected '#### ON-053 —' in specs/operator-nfr.md"
	}

	limit := headingIdx + 1 + on053FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if on053FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// on053FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func on053FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestON053PanicForensicFile is the binding test for hk-sx9r.49.
func TestON053PanicForensicFile(t *testing.T) {
	t.Parallel()

	specFile := on053FixtureOperatorNFRPath(t)
	lines := on053FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := on053FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("ON-053 check(1): %s", reason)
	}
	t.Logf("ON-053 heading found at specs/operator-nfr.md line %d; body window = %d lines",
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
			label:  "panic-timestamp-log-path",
			needle: "panic-<timestamp>.log",
			detail: "ON-053 body must declare 'panic-<timestamp>.log' as the forensic file path pattern " +
				"(expected phrase 'panic-<timestamp>.log'); the timestamp in the filename makes each " +
				"panic file unique — multiple panic files MAY accumulate without overwriting each other, " +
				"and operators can correlate file timestamps with external event records",
		},
		{
			id:     "3",
			label:  "project-hash-content-field",
			needle: "project_hash",
			detail: "ON-053 body must name 'project_hash' as a content field in the forensic file " +
				"(expected phrase 'project_hash'); the project hash is the project-scoped provenance " +
				"marker (PL-006a) — it lets operators correlate the panic file with a specific " +
				"project instance and its daemon's pidfile",
		},
		{
			id:     "4",
			label:  "monotonic-since-boot-timestamp",
			needle: "time.Since(boot)",
			detail: "ON-053 body must name 'time.Since(boot)' monotonic form for the panic timestamp " +
				"(expected phrase 'time.Since(boot)'); the monotonic companion to the wall-clock " +
				"timestamp lets operators correlate the panic to the daemon's boot time even if the " +
				"system clock was adjusted after boot",
		},
		{
			id:     "5",
			label:  "atomicity-temp-rename-fsync",
			needle: "temp+rename+fsync",
			detail: "ON-053 body must declare 'temp+rename+fsync' atomicity discipline for the write " +
				"(expected phrase 'temp+rename+fsync'); the atomic write ensures that if the daemon " +
				"panics again during forensic file writing, the file is either fully written or absent — " +
				"no partial forensic files that would mislead post-mortem analysis",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !on053FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"ON-053 check(%s) FAILED: %s\n"+
						"  spec:    specs/operator-nfr.md line ~%d (ON-053 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (6): Tags: mechanism in ON-053 body.
	t.Run("check-6-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if on053FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"ON-053 check(6) FAILED: Tags: mechanism not found in ON-053 body window\n"+
					"  spec:   specs/operator-nfr.md line ~%d (ON-053 body)\n"+
					"  detail: ON-053 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-sx9r.49 audit complete — ON-053 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
