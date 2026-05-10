package specaudit_test

// hk-8mup.42 binding test — PL-027 upgrade contract obligation (daemon-internal side).
//
// Spec ref: specs/process-lifecycle.md §4.9 PL-027.
//
// PL-027 owns the daemon-internal mechanics of `harmonik upgrade`. The daemon MUST replace
// the old binary via execve (or platform-equivalent) preserving the daemon PID. When
// launched via exec-replacement (detectable by HARMONIK_UPGRADE=1), the new instance MUST
// skip orphan sweep (step 3). Socket continuity is achieved via fd-passing — outgoing daemon
// clears FD_CLOEXEC on the listener fd before execve and passes it via HARMONIK_LISTENER_FD.
// The daemon MUST write .harmonik/daemon.upgrading before execve per ON-020a.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The daemon implementation is pending; this sensor
// verifies that PL-027 is correctly declared in the spec so that:
//
//  1. PL-027 heading is present in specs/process-lifecycle.md.
//  2. "execve" is declared as the exec-replacement mechanism.
//  3. "HARMONIK_UPGRADE=1" is declared as the upgrade environment marker.
//  4. "HARMONIK_LISTENER_FD" is declared for socket fd-passing.
//  5. "operator_upgrading" event is named as a pre-exec emission.
//  6. "operator_upgrade_completed" is named as a post-ready emission.
//  7. Tags: mechanism is present in the PL-027 body window.
//
// # Failure modes
//
//   - PL-027 heading missing.
//   - execve absent.
//   - HARMONIK_UPGRADE=1 absent.
//   - HARMONIK_LISTENER_FD absent.
//   - operator_upgrading absent.
//   - operator_upgrade_completed absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the pl027Fixture prefix per
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

// pl027FixtureProcessLifecyclePath returns the absolute path to specs/process-lifecycle.md.
func pl027FixtureProcessLifecyclePath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("pl027FixtureProcessLifecyclePath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "process-lifecycle.md")
}

// pl027FixtureHeading matches the PL-027 level-4 requirement heading line.
var pl027FixtureHeading = regexp.MustCompile(`^#### PL-027 —`)

// pl027FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var pl027FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// pl027FixtureTagsMechanism matches a "Tags: mechanism" line.
var pl027FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// pl027FixtureBodyWindow is the maximum number of lines to scan after the heading.
// Extended to 30 to capture the full multi-point upgrade mechanics.
const pl027FixtureBodyWindow = 30

// pl027FixtureLoadLines opens specFile and returns all lines.
func pl027FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("pl027FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("pl027FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// pl027FixtureBodyLines returns the lines comprising the PL-027 body.
func pl027FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if pl027FixtureHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "PL-027 heading not found; expected '#### PL-027 —' in specs/process-lifecycle.md"
	}

	limit := headingIdx + 1 + pl027FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if pl027FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// pl027FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func pl027FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestPL027UpgradeContractDaemonInternal is the binding test for hk-8mup.42.
func TestPL027UpgradeContractDaemonInternal(t *testing.T) {
	t.Parallel()

	specFile := pl027FixtureProcessLifecyclePath(t)
	lines := pl027FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := pl027FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("PL-027 check(1): %s", reason)
	}
	t.Logf("PL-027 heading found at specs/process-lifecycle.md line %d; body window = %d lines",
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
			label:  "execve-exec-replacement",
			needle: "execve",
			detail: "PL-027 body must declare 'execve' as the exec-replacement mechanism " +
				"(expected phrase 'execve'); execve is the system call that replaces the current " +
				"process image with the new binary while preserving the PID — this is how the " +
				"daemon achieves zero-downtime upgrades without a PID change",
		},
		{
			id:     "3",
			label:  "harmonik-upgrade-env-marker",
			needle: "HARMONIK_UPGRADE=1",
			detail: "PL-027 body must declare 'HARMONIK_UPGRADE=1' as the upgrade environment marker " +
				"(expected phrase 'HARMONIK_UPGRADE=1'); the new binary reads this env var to detect " +
				"it is running as an exec-replacement — it then applies the skip-path (no orphan sweep) " +
				"and socket adoption semantics instead of the normal startup sequence",
		},
		{
			id:     "4",
			label:  "harmonik-listener-fd-socket-passing",
			needle: "HARMONIK_LISTENER_FD",
			detail: "PL-027 body must declare 'HARMONIK_LISTENER_FD' for socket fd-passing " +
				"(expected phrase 'HARMONIK_LISTENER_FD'); the outgoing daemon passes the listener " +
				"fd number via this env var so the new binary can adopt the existing socket without " +
				"calling bind() — this is the mechanism that achieves gap-free socket continuity",
		},
		{
			id:     "5",
			label:  "operator-upgrading-pre-exec-emission",
			needle: "operator_upgrading",
			detail: "PL-027 body must name 'operator_upgrading' as a pre-exec emission " +
				"(expected phrase 'operator_upgrading'); the daemon emits this event before invoking " +
				"execve — it signals to operators and reconciliation that an upgrade is in progress " +
				"so they can handle the upgrade window correctly",
		},
		{
			id:     "6",
			label:  "operator-upgrade-completed-post-ready",
			needle: "operator_upgrade_completed",
			detail: "PL-027 body must name 'operator_upgrade_completed' as the post-ready emission " +
				"(expected phrase 'operator_upgrade_completed'); the new instance emits this event " +
				"after reaching ready — it closes the upgrade window and tells operators/reconciliation " +
				"that the upgrade succeeded and the daemon is accepting requests again",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !pl027FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"PL-027 check(%s) FAILED: %s\n"+
						"  spec:    specs/process-lifecycle.md line ~%d (PL-027 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in PL-027 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if pl027FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"PL-027 check(7) FAILED: Tags: mechanism not found in PL-027 body window\n"+
					"  spec:   specs/process-lifecycle.md line ~%d (PL-027 body)\n"+
					"  detail: PL-027 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8mup.42 audit complete — PL-027 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
