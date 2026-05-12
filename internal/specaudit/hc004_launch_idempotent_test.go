package specaudit_test

// hk-8i31.4 binding test — HC-004 Launch is idempotent on (run_id, node_id).
//
// Spec ref: specs/handler-contract.md §4.1 HC-004.
//
// HC-004 states: Handler.Launch MUST be idempotent on (spec.run_id, spec.node_id)
// within one daemon generation: a second Launch call with the same key MUST return the
// existing Session (or ErrTransient if prior session is terminating) rather than spawn
// a duplicate subprocess. Concurrent-launch second call MUST block on handshake outcome.
// Reconciliation-driven re-launches after daemon restart are a new daemon generation.
//
// # Audit frame
//
// This test is a spec-corpus sensor. The handler implementation is pending; this sensor
// verifies that HC-004 is correctly declared in the spec so that:
//
//  1. HC-004 heading is present in specs/handler-contract.md.
//  2. "idempotent on the key (spec.run_id, spec.node_id)" is declared.
//  3. "within one daemon generation" scopes the idempotency.
//  4. "return the existing Session" is the required behavior on duplicate launch.
//  5. "ErrTransient" is named as the error for prior session terminating.
//  6. Concurrent-launch second call "MUST block" on handshake is declared.
//  7. Tags: mechanism is present in the HC-004 body window.
//
// # Failure modes
//
//   - HC-004 heading missing.
//   - idempotent on the key absent.
//   - within one daemon generation absent.
//   - return the existing Session absent.
//   - ErrTransient absent.
//   - MUST block absent.
//   - Tags: mechanism missing.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hc004Fixture prefix per
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

// hc004FixtureHandlerContractPath returns the absolute path to specs/handler-contract.md.
func hc004FixtureHandlerContractPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hc004FixtureHandlerContractPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "handler-contract.md")
}

// hc004FixtureHC004Heading matches the HC-004 level-4 requirement heading line.
var hc004FixtureHC004Heading = regexp.MustCompile(`^#### HC-004 —`)

// hc004FixtureAnySectionHeading matches any Markdown heading (level 1–4).
var hc004FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hc004FixtureTagsMechanism matches a "Tags: mechanism" line.
var hc004FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hc004FixtureBodyWindow is the maximum number of lines after the HC-004
// heading to scan for requirement-body content.
const hc004FixtureBodyWindow = 30

// hc004FixtureLoadLines opens specFile and returns all lines.
func hc004FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hc004FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hc004FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

// hc004FixtureHC004BodyLines returns the lines comprising the HC-004 body.
func hc004FixtureHC004BodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if hc004FixtureHC004Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "HC-004 heading not found; expected '#### HC-004 —' in specs/handler-contract.md"
	}

	limit := headingIdx + 1 + hc004FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}

	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		line := lines[i]
		if hc004FixtureAnySectionHeading.MatchString(line) {
			break
		}
		bodyLines = append(bodyLines, line)
	}
	return bodyLines, headingIdx + 1, ""
}

// hc004FixtureBodyContains reports whether any line in body contains substr (case-insensitive).
func hc004FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestHC004LaunchIdempotentOnRunIDNodeID is the binding test for hk-8i31.4.
func TestHC004LaunchIdempotentOnRunIDNodeID(t *testing.T) {
	t.Parallel()

	specFile := hc004FixtureHandlerContractPath(t)
	lines := hc004FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := hc004FixtureHC004BodyLines(lines)
	if reason != "" {
		t.Fatalf("HC-004 check(1): %s", reason)
	}
	t.Logf("HC-004 heading found at specs/handler-contract.md line %d; body window = %d lines",
		headingLineNo, len(body))

	type check struct {
		id     string
		label  string
		needle string
		detail string
	}

	checks := []check{
		{
			id:    "2",
			label: "idempotent-on-run-id-node-id",
			// HC-004 was extended (hk-wb0ci hygiene sweep) to include phase and
			// iteration_count in the key for multi-phase modes; the canonical
			// phrase changed from "idempotent on the key" to the 2-tuple /
			// 4-tuple description.  The needle below matches the stable
			// sub-phrase present in both the old and new spec text.
			needle: "spec.run_id, spec.node_id",
			detail: "HC-004 body must name spec.run_id and spec.node_id as idempotency key components " +
				"(expected phrase 'spec.run_id, spec.node_id'); the (run_id, node_id) pair is the " +
				"minimum idempotency key for single-mode launches — two launches for the same node " +
				"within a run MUST NOT produce two subprocesses",
		},
		{
			id:     "3",
			label:  "within-one-daemon-generation",
			needle: "within one daemon generation",
			detail: "HC-004 body must scope idempotency 'within one daemon generation' " +
				"(expected phrase 'within one daemon generation'); daemon restart creates a new " +
				"generation — a reconciliation-driven re-launch after restart is a new launch " +
				"even if the run_id and node_id are the same",
		},
		{
			id:     "4",
			label:  "return-existing-session",
			needle: "return the existing",
			detail: "HC-004 body must declare 'return the existing' Session on duplicate launch " +
				"(expected phrase 'return the existing'); the second Launch must not spawn a new " +
				"subprocess — it returns the already-running session so the caller can attach " +
				"its watcher to the existing subprocess",
		},
		{
			id:     "5",
			label:  "errtransient-prior-session-terminating",
			needle: "ErrTransient",
			detail: "HC-004 body must name 'ErrTransient' as the error for prior session terminating " +
				"(expected phrase 'ErrTransient'); when the prior session is in the process of " +
				"terminating (e.g., watcher observing agent_completed but subprocess not yet reaped), " +
				"the caller SHOULD retry after backoff — ErrTransient signals this retryable state",
		},
		{
			id:     "6",
			label:  "concurrent-launch-must-block",
			needle: "MUST block",
			detail: "HC-004 body must declare concurrent-launch second call 'MUST block' on handshake outcome " +
				"(expected phrase 'MUST block'); when two goroutines launch the same (run_id, node_id) " +
				"simultaneously, the second must wait for the first's handshake to complete before " +
				"returning — it MUST NOT spawn a second subprocess",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, strings.ReplaceAll(c.label, " ", "_")), func(t *testing.T) {
			t.Parallel()
			if !hc004FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"HC-004 check(%s) FAILED: %s\n"+
						"  spec:    specs/handler-contract.md line ~%d (HC-004 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, c.label, headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	// Check (7): Tags: mechanism in HC-004 body.
	t.Run("check-7-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if hc004FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"HC-004 check(7) FAILED: Tags: mechanism not found in HC-004 body window\n"+
					"  spec:   specs/handler-contract.md line ~%d (HC-004 body)\n"+
					"  detail: HC-004 carries tag 'mechanism'; its absence indicates the "+
					"requirement body has been truncated or the Tags: line removed",
				headingLineNo,
			)
		}
	})

	t.Logf("hk-8i31.4 audit complete — HC-004 heading at line %d (body = %d lines)",
		headingLineNo, len(body))
}
