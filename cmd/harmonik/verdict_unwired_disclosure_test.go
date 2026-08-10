package main

// verdict_unwired_disclosure_test.go — every operator-facing surface of
// confirm-verdict and veto-verdict must say the feature is not connected.
//
// Bead ref: hk-verdict-override-unwired-aqjxo.
//
// Both commands are shipped, documented, and reachable, and neither can succeed
// under any input: no production code parks a run awaiting an operator decision,
// so there is never a pending verdict and every invocation exits 16. The danger
// is not the refusal, which is correct and non-zero. The danger is that the
// refusal is INDISTINGUISHABLE from a command that is merely idle — an operator
// reads "no pending verdict for run X", believes they picked the wrong run, and
// spends real time hunting a run for a feature wired to nothing.
//
// Three surfaces can mislead, so this file checks all three: the subcommand list
// in harmonikUsage, each command's own --help, and the runtime refusal that only
// appears after a socket round-trip. The runtime one is the one an operator
// actually hits, and it is the one a help-text-only test would miss.
//
// These tests are deliberately about the CLAIM, not the wording. They assert the
// disclosure is present and that the refusal does not blame the operator's
// run_id. They do not pin whole sentences, so the text can be improved without
// editing this file. When the feature IS connected, the disclosure must come out
// of all three surfaces and these tests must be deleted in that same change —
// TestAwaitHasNoProductionCaller in internal/daemon is the tripwire that says so.

import (
	"strings"
	"testing"
)

// notConnectedMarker is the shared disclosure token. One token across all three
// surfaces means an operator who greps for it after reading one message finds
// the others, and means this test cannot pass on a surface that merely sounds
// apologetic without stating the fact.
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

			// The marker alone is too easy to satisfy. An early draft of this
			// test passed against help text whose ONLY remaining mention was a
			// cross-reference ("see NOT CONNECTED above") pointing at a section
			// that had been deleted. Require the claim itself, and require the
			// mechanism — otherwise the marker degrades into a scary label that
			// says nothing an operator can act on.
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

// usageLineFor returns the subcommand-list line describing verb. It fails the
// test when no line mentions the verb, which catches the case where the entry
// was deleted rather than corrected — the assertion above would otherwise pass
// vacuously on an empty string.
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

			// The refusal must stay a refusal. A disclosure that also turned the
			// exit code green would be far worse than the silence it replaces.
			if code != 16 {
				t.Fatalf("exit code = %d, want 16; the disclosure must not change "+
					"the outcome, only what the operator is told", code)
			}

			if !strings.Contains(stderr, notConnectedMarker) {
				t.Errorf("refusal does not contain %q, so it reads as an idle "+
					"command rather than an unbuilt one:\n%s", notConnectedMarker, stderr)
			}

			// An operator's next move after a refusal is to doubt their argument.
			// The message must head that off explicitly, not merely omit blame.
			if !strings.Contains(stderr, "run_id you gave is not the problem") {
				t.Errorf("refusal never tells the operator their run_id is not at "+
					"fault; they will go hunting for the run:\n%s", stderr)
			}
		})
	}
}
