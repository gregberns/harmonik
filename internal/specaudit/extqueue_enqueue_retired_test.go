//go:build specaudit

package specaudit_test

// hk-rb6s3 binding test — T84: conformance assertion that the `enqueue`
// operation is retired throughout the v0.1 spec corpus.
//
// Spec refs:
//   - specs/process-lifecycle.md §4.1 PL-003a — method-set registry; enqueue
//     retired in v0.4.6, four queue-* methods are the v0.1 replacement set.
//   - specs/operator-nfr.md §4.3 ON-013a — per-command supervision goroutine
//     enumeration; enqueue replaced by queue-submit/queue-append/queue-status/
//     queue-dry-run (v0.4.2 amendment).
//   - specs/operator-nfr.md §4.10 ON-041 — daemon-communicating-commands list;
//     queue (submit/status/append/dry-run) present; enqueue absent.
//   - specs/operator-nfr.md §4.10 ON-050 — harmonik attach inline-command
//     subset; enqueue removed in v0.4.2 amendment; subset is {pause, resume, stop}.
//
// # Audit frame
//
// These tests are spec-corpus sensors. They verify that the four spec locations
// declare the v0.1 queue method set and carry no remaining active reference to
// `enqueue` as a JSON-RPC method or CLI subcommand. A "retired" mention (i.e.,
// the word "enqueue" used in a retirement context) is acceptable — the tests
// scan each requirement-body window independently and assert:
//
//  1. The four v0.1 queue method names are present in the body window.
//  2. The body window does NOT present enqueue as an active method (i.e., no
//     occurrence of "enqueue" that is not bracketed by a retirement phrase such
//     as "RETIRED", "retired", "removed", or "is not").
//
// The retirement-phrase allowlist guards against false positives from the
// changelog history embedded at the bottom of each spec file. The heading-
// bounded body-window approach (max 80 lines from heading to next heading)
// limits the scan to the normative body of each requirement.
//
// # Failure modes
//
//   - Heading for PL-003a / ON-013a / ON-041 / ON-050 missing from respective spec.
//   - Any of the four queue method names absent from a body window.
//   - "enqueue" present in a body window outside a retirement phrase.
//
// # Helper prefix
//
// All package-level identifiers in this file use the extqEnqueue prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// extqEnqueueRepoRoot returns the absolute path to the repository root by
// walking upward from this file's directory.
func extqEnqueueRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("extqEnqueueRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile is internal/specaudit/<file>.go → go up 3 levels
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// extqEnqueueLoadLines opens specFile and returns all lines.
func extqEnqueueLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("extqEnqueueLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("extqEnqueueLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// extqEnqueueBodyWindow is the maximum number of lines after a requirement
// heading to scan for normative body content before stopping at the next
// heading or end-of-file.
const extqEnqueueBodyWindow = 80

// extqEnqueueAnyHeading matches any Markdown heading line (levels 1–5).
var extqEnqueueAnyHeading = regexp.MustCompile(`^#{1,5} `)

// extqEnqueueBodyLines returns the requirement-body lines starting immediately
// after headingLineIdx (0-based) and ending at the next heading or
// extqEnqueueBodyWindow lines, whichever comes first.
func extqEnqueueBodyLines(lines []string, headingLineIdx int) []string {
	limit := headingLineIdx + 1 + extqEnqueueBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var body []string
	for i := headingLineIdx + 1; i < limit; i++ {
		if extqEnqueueAnyHeading.MatchString(lines[i]) {
			break
		}
		body = append(body, lines[i])
	}
	return body
}

// extqEnqueueFindHeading scans lines for a heading containing requirementID
// (e.g. "PL-003a", "ON-013a") and returns the 0-based index of the heading
// line. Returns -1 if not found.
func extqEnqueueFindHeading(lines []string, requirementID string) int {
	for i, line := range lines {
		if extqEnqueueAnyHeading.MatchString(line) && strings.Contains(line, requirementID) {
			return i
		}
	}
	return -1
}

// extqEnqueueBodyContains reports whether any line in body contains substr
// (case-sensitive).
func extqEnqueueBodyContains(body []string, substr string) bool {
	for _, line := range body {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// extqEnqueueRetirementPhrases are substrings that, when present on the same
// line as "enqueue", indicate the reference is a retirement acknowledgment
// rather than an active method declaration.
var extqEnqueueRetirementPhrases = []string{
	"retired",
	"RETIRED",
	"removed",
	"is not",
	"NOT a registered",
	"no longer",
	"replacement",
	"prior",
	"legacy",
	"v0.4.5",
	"v0.4.6",
	"through v0.4.5",
}

// extqEnqueueIsRetirementLine reports whether a line containing "enqueue" is
// describing retirement / removal rather than an active invocation.
func extqEnqueueIsRetirementLine(line string) bool {
	for _, phrase := range extqEnqueueRetirementPhrases {
		if strings.Contains(line, phrase) {
			return true
		}
	}
	return false
}

// extqEnqueueActiveEnqueueInBody reports whether any line in body contains
// "enqueue" as an active (non-retired) reference. Returns the first offending
// line (empty string if none found).
func extqEnqueueActiveEnqueueInBody(body []string) string {
	for _, line := range body {
		if strings.Contains(line, "enqueue") && !extqEnqueueIsRetirementLine(line) {
			return line
		}
	}
	return ""
}

// extqEnqueueV01MethodNames are the four JSON-RPC method names that replace
// enqueue in the v0.1 queue surface.
var extqEnqueueV01MethodNames = []string{
	"queue-submit",
	"queue-append",
	"queue-status",
	"queue-dry-run",
}

// TestExtqueueEnqueueRetiredPL003a asserts that PL-003a in process-lifecycle.md
// declares all four v0.1 queue method names and does NOT present enqueue as an
// active method in its body window.
func TestExtqueueEnqueueRetiredPL003a(t *testing.T) {
	t.Parallel()

	root := extqEnqueueRepoRoot(t)
	specFile := filepath.Join(root, "specs", "process-lifecycle.md")
	lines := extqEnqueueLoadLines(t, specFile)

	headingIdx := extqEnqueueFindHeading(lines, "PL-003a")
	if headingIdx < 0 {
		t.Fatalf(
			"PL-003a heading not found in %s; "+
				"expected a heading line containing 'PL-003a'",
			specFile,
		)
	}
	t.Logf("PL-003a heading found at line %d of %s", headingIdx+1, specFile)

	body := extqEnqueueBodyLines(lines, headingIdx)
	t.Logf("PL-003a body window: %d lines", len(body))

	// Assert all four v0.1 method names are present.
	for _, method := range extqEnqueueV01MethodNames {
		if !extqEnqueueBodyContains(body, method) {
			t.Errorf(
				"PL-003a body does not declare v0.1 queue method %q\n"+
					"  spec: %s (heading at line %d)\n"+
					"  fix:  add %q to the PL-003a method-set enumeration",
				method, specFile, headingIdx+1, method,
			)
		}
	}

	// Assert enqueue is NOT active in the body window.
	if offending := extqEnqueueActiveEnqueueInBody(body); offending != "" {
		t.Errorf(
			"PL-003a body contains an active (non-retired) reference to 'enqueue'\n"+
				"  spec:     %s (heading at line %d)\n"+
				"  offender: %q\n"+
				"  fix:      remove or annotate the line with a retirement phrase "+
				"(e.g., 'retired', 'removed', 'NOT a registered method')",
			specFile, headingIdx+1, offending,
		)
	}
}

// TestExtqueueEnqueueRetiredON013a asserts that ON-013a in operator-nfr.md
// enumerates all four v0.1 queue method names and does NOT list enqueue as an
// active operator-command-dispatch method.
func TestExtqueueEnqueueRetiredON013a(t *testing.T) {
	t.Parallel()

	root := extqEnqueueRepoRoot(t)
	specFile := filepath.Join(root, "specs", "operator-nfr.md")
	lines := extqEnqueueLoadLines(t, specFile)

	headingIdx := extqEnqueueFindHeading(lines, "ON-013a")
	if headingIdx < 0 {
		t.Fatalf(
			"ON-013a heading not found in %s; "+
				"expected a heading line containing 'ON-013a'",
			specFile,
		)
	}
	t.Logf("ON-013a heading found at line %d of %s", headingIdx+1, specFile)

	body := extqEnqueueBodyLines(lines, headingIdx)
	t.Logf("ON-013a body window: %d lines", len(body))

	// Assert all four v0.1 method names are present.
	for _, method := range extqEnqueueV01MethodNames {
		if !extqEnqueueBodyContains(body, method) {
			t.Errorf(
				"ON-013a body does not enumerate v0.1 queue method %q\n"+
					"  spec: %s (heading at line %d)\n"+
					"  fix:  add %q to the ON-013a operator-command enumeration",
				method, specFile, headingIdx+1, method,
			)
		}
	}

	// Assert enqueue is NOT active in the body window.
	if offending := extqEnqueueActiveEnqueueInBody(body); offending != "" {
		t.Errorf(
			"ON-013a body contains an active (non-retired) reference to 'enqueue'\n"+
				"  spec:     %s (heading at line %d)\n"+
				"  offender: %q\n"+
				"  fix:      remove 'enqueue' from the ON-013a command enumeration; "+
				"the v0.1 replacement set is queue-submit / queue-append / queue-status / queue-dry-run",
			specFile, headingIdx+1, offending,
		)
	}
}

// TestExtqueueEnqueueRetiredON041 asserts that ON-041 in operator-nfr.md
// includes queue (with its v0.1 subcommands) in the daemon-communicating-
// commands list and does NOT list a bare enqueue command.
func TestExtqueueEnqueueRetiredON041(t *testing.T) {
	t.Parallel()

	root := extqEnqueueRepoRoot(t)
	specFile := filepath.Join(root, "specs", "operator-nfr.md")
	lines := extqEnqueueLoadLines(t, specFile)

	headingIdx := extqEnqueueFindHeading(lines, "ON-041")
	if headingIdx < 0 {
		t.Fatalf(
			"ON-041 heading not found in %s; "+
				"expected a heading line containing 'ON-041'",
			specFile,
		)
	}
	t.Logf("ON-041 heading found at line %d of %s", headingIdx+1, specFile)

	body := extqEnqueueBodyLines(lines, headingIdx)
	t.Logf("ON-041 body window: %d lines", len(body))

	// ON-041 names the subcommands as submit / status / append / dry-run.
	// Check that each v0.1 verb appears somewhere in the body.
	queueVerbs := []string{"submit", "append", "dry-run"}
	for _, verb := range queueVerbs {
		if !extqEnqueueBodyContains(body, verb) {
			t.Errorf(
				"ON-041 body does not name queue subcommand %q\n"+
					"  spec: %s (heading at line %d)\n"+
					"  fix:  add %q to the ON-041 daemon-communicating-commands list",
				verb, specFile, headingIdx+1, verb,
			)
		}
	}

	// Assert enqueue is NOT active in the body window.
	if offending := extqEnqueueActiveEnqueueInBody(body); offending != "" {
		t.Errorf(
			"ON-041 body contains an active (non-retired) reference to 'enqueue'\n"+
				"  spec:     %s (heading at line %d)\n"+
				"  offender: %q\n"+
				"  fix:      remove 'enqueue' from the ON-041 command list; "+
				"the v0.1 replacement is 'queue' with subcommands submit/status/append/dry-run",
			specFile, headingIdx+1, offending,
		)
	}
}

// TestExtqueueEnqueueRetiredON050 asserts that ON-050 in operator-nfr.md
// declares the harmonik attach inline-command subset as {pause, resume, stop}
// and does NOT include enqueue.
func TestExtqueueEnqueueRetiredON050(t *testing.T) {
	t.Parallel()

	root := extqEnqueueRepoRoot(t)
	specFile := filepath.Join(root, "specs", "operator-nfr.md")
	lines := extqEnqueueLoadLines(t, specFile)

	headingIdx := extqEnqueueFindHeading(lines, "ON-050")
	if headingIdx < 0 {
		t.Fatalf(
			"ON-050 heading not found in %s; "+
				"expected a heading line containing 'ON-050'",
			specFile,
		)
	}
	t.Logf("ON-050 heading found at line %d of %s", headingIdx+1, specFile)

	body := extqEnqueueBodyLines(lines, headingIdx)
	t.Logf("ON-050 body window: %d lines", len(body))

	// Assert the surviving inline-command subset members are present.
	for _, cmd := range []string{"pause", "resume", "stop"} {
		if !extqEnqueueBodyContains(body, cmd) {
			t.Errorf(
				"ON-050 body does not name inline attach command %q\n"+
					"  spec: %s (heading at line %d)\n"+
					"  fix:  add %q to the ON-050 inline-command subset",
				cmd, specFile, headingIdx+1, cmd,
			)
		}
	}

	// Assert enqueue is NOT active in the body window.
	if offending := extqEnqueueActiveEnqueueInBody(body); offending != "" {
		t.Errorf(
			"ON-050 body contains an active (non-retired) reference to 'enqueue'\n"+
				"  spec:     %s (heading at line %d)\n"+
				"  offender: %q\n"+
				"  fix:      remove 'enqueue' from the ON-050 inline-command subset; "+
				"it was retired in v0.4.2 per the extqueue v0.1 amendment",
			specFile, headingIdx+1, offending,
		)
	}
}
