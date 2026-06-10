package specaudit_test

// hk-iljnj binding test — HC-045b: hook-bridge connection regime for short-lived
// subprocesses.
//
// Spec ref: specs/handler-contract.md §4.10 HC-045b.
//
// HC-045b states: handler subsystems (notably the claude-code bridge) may spawn
// additional short-lived subprocesses that open one-shot NDJSON connections to the
// daemon socket. Such connections MUST carry both run_id and claude_session_id at the
// top level of the envelope. Per-connection lifetime: dial timeout ≤ 5 s, single
// message ≤ 1 MiB per HC-007a, optional ack-line read with 5 s deadline, then close.
//
// # What this test verifies
//
// Two layered checks:
//
//  1. Spec-corpus check — confirms HC-045b is correctly declared in
//     specs/handler-contract.md:
//     (a) HC-045b heading is present.
//     (b) "short-lived subprocesses" appears in the body (the subprocess class).
//     (c) "run_id" and "claude_session_id" appear in the body (envelope routing keys).
//     (d) "sole bidirectional channel" appears in the body (HC-007 exception clause).
//     (e) "5 s" appears in the body (per-connection lifetime timeout requirement).
//     (f) Tags: mechanism is present in the body window.
//
//  2. Code-traceability check — confirms that the hook-relay implementation file
//     (internal/hookrelay/hookrelay.go) contains a cite back to HC-045b. This
//     ensures the normative connection-regime contract is anchored in the code that
//     realizes the one-shot subprocess connection pattern.
//
// # Failure modes
//
//   - Spec check (a): HC-045b heading absent from specs/handler-contract.md.
//   - Spec check (b): "short-lived subprocesses" absent from HC-045b body.
//   - Spec check (c): "run_id" or "claude_session_id" absent from HC-045b body.
//   - Spec check (d): "sole bidirectional channel" absent from HC-045b body.
//   - Spec check (e): "5 s" absent from HC-045b body.
//   - Spec check (f): Tags: mechanism absent from HC-045b body window.
//   - Code check (2): "HC-045b" not cited in internal/hookrelay/hookrelay.go.
//
// # Helper prefix
//
// All package-level identifiers use the hc045bFixture prefix per the
// implementer-protocol.md helper-prefix discipline (bead hk-iljnj).

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

// hc045bFixtureRepoRoot resolves the repository root from this test file's
// source path. The test file lives at:
//
//	internal/specaudit/hc045b_hookrelay_connection_regime_test.go
//
// so the repo root is two directories up.
func hc045bFixtureRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc045bFixtureRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile: .../internal/specaudit/hc045b_hookrelay_connection_regime_test.go
	// internal/ is one up; repo root is two up.
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// hc045bFixtureHandlerContractPath returns the absolute path to
// specs/handler-contract.md.
func hc045bFixtureHandlerContractPath(t *testing.T, repoRoot string) string {
	t.Helper()
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc045bFixtureHookRelayPath returns the absolute path to
// internal/hookrelay/hookrelay.go.
func hc045bFixtureHookRelayPath(t *testing.T, repoRoot string) string {
	t.Helper()
	return filepath.Join(repoRoot, "internal", "hookrelay", "hookrelay.go")
}

// hc045bFixtureHC045bHeading matches the HC-045b level-4 requirement heading
// line in specs/handler-contract.md.
var hc045bFixtureHC045bHeading = regexp.MustCompile(`^#### HC-045b —`)

// hc045bFixtureAnySectionHeading matches any Markdown heading (level 1–4).
// Used to detect the end of the HC-045b body window.
var hc045bFixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc045bFixtureTagsMechanism matches a "Tags: mechanism" line.
var hc045bFixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc045bFixtureBodyWindow is the maximum number of lines after the HC-045b
// heading to scan for requirement-body content.
const hc045bFixtureBodyWindow = 20

// hc045bFixtureLoadLines opens the file at path and returns all lines.
func hc045bFixtureLoadLines(t *testing.T, path string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known project paths; not user input.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("hc045bFixtureLoadLines: open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc045bFixtureLoadLines: scan %s: %v", path, scanErr)
	}
	return lines
}

// hc045bFixtureBodyLines returns the lines comprising the HC-045b requirement
// body: all lines after the HC-045b heading up to (but not including) the next
// Markdown heading or hc045bFixtureBodyWindow lines, whichever comes first.
//
// Returns (nil, 0, reason) when the heading is not found.
func hc045bFixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc045bFixtureHC045bHeading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0,
			"HC-045b heading not found; expected '#### HC-045b —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc045bFixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc045bFixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc045bFixtureBodyContains reports whether any line in body contains substr
// (case-insensitive substring match).
func hc045bFixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC045bHookRelayConnectionRegime is the binding test for hk-iljnj (HC-045b).
//
// It runs two checks:
//
//  1. Spec-corpus check: opens specs/handler-contract.md, locates the HC-045b
//     heading, and validates the required phrases and Tags: mechanism.
//
//  2. Code-traceability check: verifies that internal/hookrelay/hookrelay.go
//     contains a "HC-045b" citation so the hook-relay implementation is traceable
//     back to its governing connection-regime spec requirement.
func TestHC045bHookRelayConnectionRegime(t *testing.T) {
	t.Parallel()

	repoRoot := hc045bFixtureRepoRoot(t)

	// ── Check 1: spec-corpus ───────────────────────────────────────────────────
	t.Run("spec-corpus", func(t *testing.T) {
		t.Parallel()

		specFile := hc045bFixtureHandlerContractPath(t, repoRoot)
		lines := hc045bFixtureLoadLines(t, specFile)

		body, headingLineNo, reason := hc045bFixtureBodyLines(lines)
		if reason != "" {
			t.Fatalf("HC-045b check(a): %s", reason)
		}
		t.Logf("HC-045b heading found at specs/handler-contract.md line %d; body window = %d lines",
			headingLineNo, len(body))

		type check struct {
			id     string
			label  string
			needle string
			detail string
		}

		checks := []check{
			{
				id:     "b",
				label:  "short-lived-subprocesses-class",
				needle: "short-lived subprocesses",
				detail: "HC-045b body must name 'short-lived subprocesses' as the subprocess class " +
					"(expected substring 'short-lived subprocesses'); this is the load-bearing " +
					"description that distinguishes hook-relay invocations from the long-lived " +
					"handler-process connection and motivates the one-shot connection regime",
			},
			{
				id:     "c-run_id",
				label:  "envelope-routing-run_id",
				needle: "run_id",
				detail: "HC-045b body must name 'run_id' as a required envelope routing key " +
					"(expected substring 'run_id'); without run_id the daemon's connection " +
					"acceptor cannot route the hook-relay message to the correct session watcher",
			},
			{
				id:     "c-claude_session_id",
				label:  "envelope-routing-claude_session_id",
				needle: "claude_session_id",
				detail: "HC-045b body must name 'claude_session_id' as a required envelope routing key " +
					"(expected substring 'claude_session_id'); without claude_session_id the daemon " +
					"cannot disambiguate hook-relay messages across concurrent sessions sharing a run_id",
			},
			{
				id:     "d",
				label:  "hc007-bidirectional-channel-exception",
				needle: "sole bidirectional channel",
				detail: "HC-045b body must reference the 'sole bidirectional channel' phrasing from HC-007 " +
					"(expected substring 'sole bidirectional channel'); this is the normative exception " +
					"clause that carves out hook-relay one-shot connections from HC-007's single-channel " +
					"constraint — without it, the two-contributor model (handler + relay) appears to " +
					"violate HC-007",
			},
			{
				id:     "e",
				label:  "per-connection-timeout",
				needle: "5 s",
				detail: "HC-045b body must state the per-connection timeout as '5 s' " +
					"(expected substring '5 s'); this is the normative dial timeout and ack-read " +
					"deadline that hook-relay implementations must honor — hookrelay.go's sendToSocket " +
					"implements dialTimeout=5s and readTimeout=5s per this requirement",
			},
		}

		for _, c := range checks {
			c := c
			t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
				t.Parallel()
				if !hc045bFixtureBodyContains(body, c.needle) {
					t.Errorf(
						"HC-045b check(%s) FAILED: %s\n"+
							"  spec:    specs/handler-contract.md line ~%d (HC-045b body)\n"+
							"  missing: %q\n"+
							"  detail:  %s",
						c.id, c.label, headingLineNo, c.needle, c.detail,
					)
				}
			})
		}

		// Check (f): Tags: mechanism
		t.Run("check-f-tags-mechanism", func(t *testing.T) {
			t.Parallel()
			found := false
			for _, line := range body {
				if hc045bFixtureTagsMechanism.MatchString(line) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf(
					"HC-045b check(f) FAILED: Tags: mechanism not found in HC-045b body window\n"+
						"  spec:   specs/handler-contract.md line ~%d (HC-045b body)\n"+
						"  detail: HC-045b carries tag 'mechanism'; its absence indicates the "+
						"requirement body has been truncated or the Tags: line removed",
					headingLineNo,
				)
			}
		})

		t.Logf("HC-045b spec-corpus check complete — heading at line %d, body = %d lines",
			headingLineNo, len(body))
	})

	// ── Check 2: code-traceability — hookrelay.go cites HC-045b ────────────────
	t.Run("code-traceability-hookrelay", func(t *testing.T) {
		t.Parallel()

		hookRelayFile := hc045bFixtureHookRelayPath(t, repoRoot)
		lines := hc045bFixtureLoadLines(t, hookRelayFile)

		found := false
		for _, line := range lines {
			if strings.Contains(line, "HC-045b") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-045b code-traceability FAILED: 'HC-045b' not cited in %s\n"+
					"  detail: internal/hookrelay/hookrelay.go implements the one-shot connection "+
					"regime (sendToSocket: dial timeout 5s, write one NDJSON line ≤1 MiB, "+
					"read ack within 5s, then close). It MUST cite HC-045b so that a future "+
					"reader can trace from the code to the governing connection-regime requirement. "+
					"Add 'HC-045b' to the package-level doc comment's Spec: line "+
					"(e.g. '// Spec: specs/handler-contract.md §4.10 HC-045b ...').",
				hookRelayFile,
			)
		} else {
			t.Logf("HC-045b code-traceability PASS: 'HC-045b' found in internal/hookrelay/hookrelay.go")
		}
	})
}
