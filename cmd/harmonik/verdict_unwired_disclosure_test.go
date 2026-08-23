package main

import (
	"strings"
	"testing"
)

const notConnectedMarker = "NOT CONNECTED"

// TestVerdictHelpDeclaresNotConnected fails when either command's --help
// presents the feature as available.
func TestVerdictHelpDeclaresNotConnected(t *testing.T) {
	cases := []struct {
		name  string
		usage func()
	}{
		{"confirm-verdict", confirmVerdictUsage},
		{"veto-verdict", vetoVerdictUsage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _ := captureUsageIO(t, tc.usage)

			if !strings.Contains(stdout, notConnectedMarker) {
				t.Errorf("%s --help does not contain %q; a reader cannot tell this "+
					"command can never succeed:\n%s", tc.name, notConnectedMarker, stdout)
			}

			if !strings.Contains(stdout, "cannot succeed") {
				t.Errorf("%s --help contains the %q marker but never claims the "+
					"command cannot succeed; a bare cross-reference is not a "+
					"disclosure:\n%s", tc.name, notConnectedMarker, stdout)
			}
			if !strings.Contains(stdout, "parks") {
				t.Errorf("%s --help states %q but never says what is missing "+
					"(that nothing parks a run); the disclosure is not actionable:\n%s",
					tc.name, notConnectedMarker, stdout)
			}
		})
	}
}

// TestTopLevelUsageDeclaresVerdictNotConnected fails when the subcommand list
// advertises the two verbs as ordinary working commands. The list is where an
// operator first learns the commands exist, so it is where the false impression
// starts.
func TestTopLevelUsageDeclaresVerdictNotConnected(t *testing.T) {
	stdout, stderr := captureUsageIO(t, harmonikUsage)
	all := stdout + stderr

	for _, verb := range []string{"confirm-verdict", "veto-verdict"} {
		line := usageLineFor(t, all, verb)
		if !strings.Contains(line, notConnectedMarker) {
			t.Errorf("subcommand-list entry for %q does not say %q, so the list "+
				"advertises a command that can never succeed:\n\t%s",
				verb, notConnectedMarker, line)
		}
	}
}

func usageLineFor(t *testing.T, usage, verb string) string {
	t.Helper()
	for _, line := range strings.Split(usage, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, verb+" ") {
			return trimmed
		}
	}
	t.Fatalf("no subcommand-list line found for %q; the scan is reading the "+
		"wrong output and cannot detect a missing disclosure", verb)
	return ""
}

// TestVerdictRefusalDeclaresNotConnected drives the real socket path against a
// fake daemon that returns error_code 16 — the ONLY response these commands can
// get in production — and checks the stderr an operator actually reads.
//
// This is the load-bearing case in this file. The help text is opt-in; this
// message is the one that arrives unbidden at the moment of confusion.
func TestVerdictRefusalDeclaresNotConnected(t *testing.T) {
	for _, op := range []string{"confirm_verdict", "veto_verdict"} {
		t.Run(op, func(t *testing.T) {
			dir, _ := startFakeVerdictDaemon(t, map[string]any{
				"ok": false, "error_code": 16, "error": "no pending",
			})

			var code int
			_, stderr := captureUsageIO(t, func() {
				code = sendVerdictOverrideRequest(dir, "run-abc", op, "")
			})

			if code != 16 {
				t.Fatalf("exit code = %d, want 16; the disclosure must not change "+
					"the outcome, only what the operator is told", code)
			}

			if !strings.Contains(stderr, notConnectedMarker) {
				t.Errorf("refusal does not contain %q, so it reads as an idle "+
					"command rather than an unbuilt one:\n%s", notConnectedMarker, stderr)
			}

			if !strings.Contains(stderr, "run_id you gave is not the problem") {
				t.Errorf("refusal never tells the operator their run_id is not at "+
					"fault; they will go hunting for the run:\n%s", stderr)
			}
		})
	}
}
