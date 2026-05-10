package specaudit_test

// hk-8i31.29 binding test — HC-024a socket-level I/O error is distinct from subprocess crash.
//
// Spec ref: specs/handler-contract.md §4.5 HC-024a.
//
// HC-024a states: a socket-level I/O error from the progress-stream read-loop — EPIPE,
// ECONNRESET, the socket file being unlinked, or a decoder error — MUST be distinguished
// from subprocess-level termination. On the first occurrence without a prior terminal event,
// the watcher MUST: (a) emit agent_failed with class ErrTransient and sub-reason
// socket_io_error; (b) attempt ONE reconnect within a bounded window (default 500ms).
// If reconnect succeeds and subprocess is still alive, the watcher MAY resume; else
// reclassify to ErrStructural with sub-reason progress_stream_broken, send SIGKILL,
// and mark session terminated. Sessions are single-socket-lifetime at MVH.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler implementation is pending; this sensor
// verifies that HC-024a is correctly declared in the spec so that:
//
//  1. HC-024a heading is present in specs/handler-contract.md.
//  2. "ErrTransient" is declared as the class for the first socket error.
//  3. "socket_io_error" sub-reason is named.
//  4. "ONE reconnect" attempt is declared.
//  5. "ErrStructural" is declared for persistent socket failure.
//  6. "progress_stream_broken" sub-reason is named.
//  7. Tags: mechanism is present in the HC-024a body window.
//
// # Failure modes
//
//   - HC-024a heading missing.
//   - ErrTransient absent.
//   - socket_io_error absent.
//   - ONE reconnect absent.
//   - ErrStructural absent.
//   - progress_stream_broken absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc024aFixture prefix per
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

// hc024aFixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc024aFixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc024aFixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc024aFixtureHeading matches the HC-024a level-4 requirement heading line.
var hc024aFixtureHeading = regexp.MustCompile(`^#### HC-024a —`)

// hc024aFixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc024aFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc024aFixtureTagsMechanism matches a "Tags: mechanism" line.
var hc024aFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc024aFixtureBodyWindow is the maximum number of lines to scan after the heading.
const hc024aFixtureBodyWindow = 15

// hc024aFixtureLoadLines opens specFile and returns all lines.
func hc024aFixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc024aFixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc024aFixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc024aFixtureBodyLines returns the lines comprising the HC-024a body.
func hc024aFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc024aFixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-024a heading not found; expected '#### HC-024a —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc024aFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc024aFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc024aFixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc024aFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC024aSocketIODistinctFromCrash is the binding test for hk-8i31.29.
func TestHC024aSocketIODistinctFromCrash(t *testing.T) {
	t.Parallel()

	specFile := hc024aFixtureHandlerContractPath(t)
	lines := hc024aFixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc024aFixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-024a check(1): %s", reason)
	}
	t.Logf("HC-024a heading found at specs/handler-contract.md line %d; body window = %d lines",
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
			label:  "errtransient-for-first-socket-error",
			needle: "ErrTransient",
			detail: "HC-024a body must declare 'ErrTransient' as the class for the first socket error " +
				"(expected phrase 'ErrTransient'); the transient classification allows the watcher to " +
				"attempt a reconnect — if reconnect succeeds, the session can resume without being " +
				"marked as a permanent failure",
		},
		{
			id:     "3",
			label:  "socket-io-error-sub-reason",
			needle: "socket_io_error",
			detail: "HC-024a body must name 'socket_io_error' as the sub-reason " +
				"(expected phrase 'socket_io_error'); this sub-reason discriminates socket transport " +
				"failures from subprocess crash (agent_process_died) in the agent_failed event — " +
				"operators can query the bus for this specific failure class",
		},
		{
			id:     "4",
			label:  "one-reconnect-attempt",
			needle: "ONE reconnect",
			detail: "HC-024a body must declare 'ONE reconnect' attempt " +
				"(expected phrase 'ONE reconnect'); exactly one reconnect attempt is the protocol — " +
				"not zero (which would be too aggressive) and not multiple (which would delay " +
				"termination of a truly broken session)",
		},
		{
			id:     "5",
			label:  "errstructural-persistent-failure",
			needle: "ErrStructural",
			detail: "HC-024a body must declare 'ErrStructural' for persistent socket failure " +
				"(expected phrase 'ErrStructural'); after the ONE reconnect fails or produces another " +
				"socket error, the session is reclassified to structural — meaning the transport " +
				"is fundamentally broken and the session must be terminated",
		},
		{
			id:     "6",
			label:  "progress-stream-broken-sub-reason",
			needle: "progress_stream_broken",
			detail: "HC-024a body must name 'progress_stream_broken' as the structural sub-reason " +
				"(expected phrase 'progress_stream_broken'); this discriminates the structural " +
				"socket-failure class from other ErrStructural scenarios — it tells the watcher " +
				"to SIGKILL the subprocess and mark the session terminated",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc024aFixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-024a check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-024a body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in HC-024a body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc024aFixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-024a check(7) FAILED: Tags: mechanism not found in HC-024a body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-024a body)\n"+
					"  detail: HC-024a carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.29 audit complete — HC-024a heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
