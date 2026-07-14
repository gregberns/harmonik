//go:build specaudit

package specaudit_test

// hk-i0tw.30 binding test — SH-028: harness MUST NOT depend on external network access.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-028.
//
// SH-028 states: every scenario MUST execute without any non-loopback outbound
// network call from the harness, the per-scenario daemon, the orchestrator,
// the watchers, the agent-runner, or the twin binaries.  Permitted: loopback
// (127.0.0.0/8, ::1), AF_UNIX sockets within the synthetic project root,
// filesystem-only operations.  Forbidden: any non-loopback IPv4/IPv6 address;
// outbound DNS to a non-loopback resolver.
//
// Verification mechanism floor: Linux = unshare(CLONE_NEWNET); macOS = pf
// packet-filter.  Non-loopback connection attempt → verdict=error /
// failure_class=harness-internal-error.  §10.2 conformance lane MUST run with
// sandbox enabled.
//
// # Audit frame
//
// Spec-corpus binding test verifying SH-028 is present and declares:
//
//  1. Heading present — "#### SH-028 —".
//  2. Loopback-only permitted range (127.0.0.0/8, ::1).
//  3. Non-loopback outbound prohibition.
//  4. Outbound DNS prohibition.
//  5. Linux verification mechanism: unshare(CLONE_NEWNET).
//  6. macOS verification mechanism: pf packet-filter.
//  7. Non-loopback connection attempt → harness-internal-error.
//  8. §10.2 conformance lane sandbox obligation.
//  9. Tags: mechanism.
//
// # Helper prefix: sh028Fixture

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

func sh028FixtureScenarioHarnessPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("sh028FixtureScenarioHarnessPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "scenario-harness.md")
}

func sh028FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: path derived from runtime.Caller + known specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("sh028FixtureLoadLines: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("sh028FixtureLoadLines: scan %s: %v", specFile, scanErr)
	}
	return lines
}

var (
	sh028FixtureSH028Heading      = regexp.MustCompile(`^#### SH-028 —`)
	sh028FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)
	sh028FixtureTagsMechanism     = regexp.MustCompile(`^Tags:.*\bmechanism\b`)
)

// sh028FixtureBodyWindow is larger than the standard 30 because SH-028 has
// a two-paragraph body (prohibition + verification mechanism floor).
const sh028FixtureBodyWindow = 40

func sh028FixtureBodyLines(lines []string) (body []string, headingLineNo int, reason string) {
	headingIdx := -1
	for i, line := range lines {
		if sh028FixtureSH028Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return nil, 0, "SH-028 heading '#### SH-028 —' not found in specs/scenario-harness.md"
	}
	limit := headingIdx + 1 + sh028FixtureBodyWindow
	if limit > len(lines) {
		limit = len(lines)
	}
	var bodyLines []string
	for i := headingIdx + 1; i < limit; i++ {
		if sh028FixtureAnySectionHeading.MatchString(lines[i]) {
			break
		}
		bodyLines = append(bodyLines, lines[i])
	}
	return bodyLines, headingIdx + 1, ""
}

func sh028FixtureBodyContains(body []string, substr string) bool {
	lower := strings.ToLower(substr)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), lower) {
			return true
		}
	}
	return false
}

// TestSH028NoExternalNetwork is the binding test for SH-028.
func TestSH028NoExternalNetwork(t *testing.T) {
	t.Parallel()

	specFile := sh028FixtureScenarioHarnessPath(t)
	lines := sh028FixtureLoadLines(t, specFile)

	body, headingLineNo, reason := sh028FixtureBodyLines(lines)
	if reason != "" {
		t.Fatalf("SH-028 check(a): %s", reason)
	}
	t.Logf("SH-028 heading found at specs/scenario-harness.md line %d; body = %d lines",
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
			label:  "loopback-permitted-range",
			needle: "127.0.0.0/8",
			detail: "SH-028 body must declare the loopback-permitted IPv4 range (127.0.0.0/8); " +
				"this is the network-boundary definition",
		},
		{
			id:     "c",
			label:  "ipv6-loopback-permitted",
			needle: "::1",
			detail: "SH-028 body must permit ::1 (IPv6 loopback)",
		},
		{
			id:     "d",
			label:  "non-loopback-prohibition",
			needle: "non-loopback",
			detail: "SH-028 body must state that non-loopback outbound calls are forbidden",
		},
		{
			id:     "e",
			label:  "outbound-dns-prohibition",
			needle: "DNS",
			detail: "SH-028 body must explicitly forbid outbound DNS to non-loopback resolvers; " +
				"DNS is a common inadvertent network escape path",
		},
		{
			id:     "f",
			label:  "linux-unshare-mechanism",
			needle: "CLONE_NEWNET",
			detail: "SH-028 body must name the Linux verification mechanism: unshare(CLONE_NEWNET); " +
				"this is the mechanism floor for the Linux conformance lane",
		},
		{
			id:     "g",
			label:  "macos-pf-mechanism",
			needle: "pf",
			detail: "SH-028 body must name the macOS verification mechanism: pf packet-filter; " +
				"this is the mechanism floor for the macOS conformance lane",
		},
		{
			id:     "h",
			label:  "non-loopback-connection-harness-internal-error",
			needle: "harness-internal-error",
			detail: "SH-028 body must state that a scenario with a detectable non-loopback connection " +
				"MUST fail with failure_class=harness-internal-error",
		},
		{
			id:     "i",
			label:  "conformance-lane-sandbox-obligation",
			needle: "conformance lane",
			detail: "SH-028 body must state the §10.2 conformance lane MUST run with the sandbox enabled",
		},
	}

	for _, c := range checks {
		c := c
		t.Run(fmt.Sprintf("check-%s-%s", c.id, c.label), func(t *testing.T) {
			t.Parallel()
			if !sh028FixtureBodyContains(body, c.needle) {
				t.Errorf(
					"SH-028 check(%s) FAILED: %s\n"+
						"  spec:    specs/scenario-harness.md line ~%d (SH-028 body)\n"+
						"  missing: %q\n"+
						"  detail:  %s",
					c.id, strings.ReplaceAll(c.label, "-", " "), headingLineNo, c.needle, c.detail,
				)
			}
		})
	}

	t.Run("check-j-tags-mechanism", func(t *testing.T) {
		t.Parallel()
		found := false
		for _, line := range body {
			if sh028FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"SH-028 check(j) FAILED: Tags: mechanism not found in SH-028 body window\n"+
					"  spec:   specs/scenario-harness.md line ~%d (SH-028 body)\n"+
					"  detail: SH-028 carries tag 'mechanism'; its absence indicates the requirement body was truncated",
				headingLineNo,
			)
		}
	})

	t.Logf("SH-028 audit complete (heading at line %d, body = %d lines)", headingLineNo, len(body))
}
