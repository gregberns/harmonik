package specaudit_test

// hk-8mup.25 binding test — PL-014a per-daemon concurrency ceiling (rlimit-derived default).
//
// Spec ref: specs/process-lifecycle.md §4.3 PL-014a.
//
// PL-014a states: the daemon MUST enforce a configurable ceiling on simultaneously-running
// agent subprocesses. The default ceiling is min(RLIMIT_NOFILE_soft / FDS_PER_HANDLER,
// FALLBACK_CAP) where FDS_PER_HANDLER = 8 and FALLBACK_CAP = 1024. The daemon MUST
// getrlimit(RLIMIT_NOFILE) at PL-005 step 0; if soft below MIN_NOFILE = 4096, the daemon
// MUST attempt setrlimit to raise to min(4096, hard) and MUST log a warning on failure.
// An operator-configured ceiling per operator-nfr.md §4.3 takes precedence. Exceeding
// the ceiling MUST emit dispatch_deferred{reason="per_daemon_ceiling_exhausted"}.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The process lifecycle implementation is pending;
// this sensor verifies that PL-014a is correctly declared in the spec so that:
//
//  1. PL-014a heading is present in specs/process-lifecycle.md.
//  2. "configurable ceiling" on simultaneously-running agent subprocesses is declared.
//  3. "FDS_PER_HANDLER = 8" is declared as the per-handler FD estimate.
//  4. "FALLBACK_CAP = 1024" is declared as the fallback upper bound.
//  5. "MIN_NOFILE = 4096" is declared as the minimum rlimit threshold.
//  6. "per_daemon_ceiling_exhausted" is named as the dispatch_deferred reason.
//  7. Tags: mechanism is present in the PL-014a body window.
//
// # Failure modes
//
//   - PL-014a heading missing.
//   - configurable ceiling absent.
//   - FDS_PER_HANDLER = 8 absent.
//   - FALLBACK_CAP = 1024 absent.
//   - MIN_NOFILE = 4096 absent.
//   - per_daemon_ceiling_exhausted absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the pl014aFixture prefix per
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

// pl014aFixtureProcessLifecyclePath returns the absolute path to specs/process-lifecycle.md.
func pl014aFixtureProcessLifecyclePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("pl014aFixtureProcessLifecyclePath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "process-lifecycle.md")
}

// pl014aFixturePL014aHeading matches the PL-014a level-4 requirement heading line.
var pl014aFixturePL014aHeading = regexp.MustCompile(`^#### PL-014a —`)

// pl014aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var pl014aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// pl014aFixtureTagsMechanism matches a "Tags: mechanism" line.
var pl014aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// pl014aFixtureBodyWindow is the maximum number of lines after the PL-014a
// heading to scan for requirement-body content.
const pl014aFixtureBodyWindow = 30

// pl014aFixtureLoadLines opens specFile and returns all lines.
func pl014aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("pl014aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("pl014aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// pl014aFixturePL014aBodyLines returns the lines comprising the PL-014a body.
func pl014aFixturePL014aBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if pl014aFixturePL014aHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "PL-014a heading not found; expected '#### PL-014a —' in specs/process-lifecycle.md"
	}

	limit := headingIdx + 1 + pl014aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if pl014aFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// pl014aFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func pl014aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestPL014aPerDaemonConcurrencyCeiling is the binding test for hk-8mup.25.
func TestPL014aPerDaemonConcurrencyCeiling(t *testing.T) {
	t.Parallel()

	specFile := pl014aFixtureProcessLifecyclePath(t)
	lines := pl014aFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := pl014aFixturePL014aBodyLines(lines)
	if reason != "" {
		t.Fatalf("PL-014a check(1): %s", reason)
	}
	t.Logf("PL-014a heading found at specs/process-lifecycle.md line %d; body window = %d lines",
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
			label:  "configurable-ceiling-declared",
			needle: "configurable ceiling",
			detail: "PL-014a body must declare 'configurable ceiling' on simultaneously-running agent subprocesses " +
				"(expected phrase 'configurable ceiling'); the ceiling is configurable so operators can tune " +
				"per workload — the rlimit-derived default is the safety floor, not a hard constant",
		},
		{
			id:     "3",
			label:  "fds-per-handler-8",
			needle: "FDS_PER_HANDLER = 8",
			detail: "PL-014a body must declare 'FDS_PER_HANDLER = 8' as the per-handler FD estimate " +
				"(expected phrase 'FDS_PER_HANDLER = 8'); this constant (stdin/stdout/stderr/socket + " +
				"transient spikes) is the divisor in the rlimit-derived ceiling formula — changing it " +
				"would change the default ceiling on all deployments",
		},
		{
			id:     "4",
			label:  "fallback-cap-1024",
			needle: "FALLBACK_CAP = 1024",
			detail: "PL-014a body must declare 'FALLBACK_CAP = 1024' as the fallback upper bound " +
				"(expected phrase 'FALLBACK_CAP = 1024'); when rlimit yields a very large value " +
				"(e.g., RLIMIT_NOFILE = 1048576), the fallback cap prevents the daemon from " +
				"scheduling an unreasonably large number of concurrent agents",
		},
		{
			id:     "5",
			label:  "min-nofile-4096",
			needle: "MIN_NOFILE = 4096",
			detail: "PL-014a body must declare 'MIN_NOFILE = 4096' as the minimum rlimit threshold " +
				"(expected phrase 'MIN_NOFILE = 4096'); on macOS the default RLIMIT_NOFILE=256 would " +
				"yield a ceiling of 32 (256/8) which is too low; the daemon raises rlimit to at least " +
				"4096 to ensure a reasonable default ceiling of 512 (4096/8)",
		},
		{
			id:     "6",
			label:  "per-daemon-ceiling-exhausted-reason",
			needle: "per_daemon_ceiling_exhausted",
			detail: "PL-014a body must name 'per_daemon_ceiling_exhausted' as the dispatch_deferred reason " +
				"(expected phrase 'per_daemon_ceiling_exhausted'); this distinguishes per-daemon ceiling " +
				"exhaustion from ON-041's cross-daemon 'machine_ceiling_exhausted' — operators can " +
				"diagnose whether to raise the per-daemon ceiling or add more machines",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !pl014aFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"PL-014a check(%s) FAILED: %s\n"+
						"  spec:    specs/process-lifecycle.md line ~%d (PL-014a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in PL-014a body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if pl014aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"PL-014a check(7) FAILED: Tags: mechanism not found in PL-014a body window\n"+
					"  spec:   specs/process-lifecycle.md line ~%d (PL-014a body)\n"+
					"  detail: PL-014a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mup.25 audit complete — PL-014a heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
