package hookrelay

// partial_env_test.go — the relay must be able to report its own failure.
//
// The defect this pins (hk-stop-relay-cannot-fail-5n2t3): envFromOS returned a
// bare error for ANY absent HARMONIK_* variable, and Run turned every one of
// those into a silent exit 0. Two very different situations shared that exit:
//
//  1. No HARMONIK_* variable is set at all. The relay is running under a hand-
//     started Claude Code session in a project whose settings.json carries the
//     hook. Nothing is wrong and nothing should be said.
//  2. The session IS harmonik-managed but one variable did not arrive. The
//     completion signal cannot be delivered, and the only channel that could
//     report that is hard-wired to succeed. The daemon then waits out the whole
//     commit budget and records the finished agent as a budget overrun.
//
// Case 2 must be loud. Case 1 must stay quiet — the daemon is legitimately down
// on a developer box, and a relay that shouted there would break every local
// session.
//
// The two cases are told apart by PRESENCE, not by emptiness, so these tests
// take care to distinguish an unexported variable from one exported as "".
//
// Spec: specs/claude-hook-bridge.md §4.6 CHB-017 ("MUST exit 1 on any
// unrecoverable failure ... env-var mismatch").

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// relayEnvKeys is every variable envFromOS reads, required and optional. It is
// DERIVED from the lists the code itself reads, never retyped. A hand-kept copy
// would agree with envFromOS only until the next variable is added, and the
// disagreement would then surface as a test that fails on a machine which
// exports the new name and passes on one that does not.
var relayEnvKeys = append(append([]string{}, requiredEnvKeys...), optionalEnvKeys...)

// unsetRelayEnv REMOVES every HARMONIK_* variable the relay reads, so the test
// starts from a known state whatever the developer's shell carries.
//
// It unsets. It does not empty. Setting each variable to "" leaves all nine
// PRESENT, which is the wired-session-with-broken-values case and not the
// no-harmonik-session case any test that calls this helper wants to stage. The
// two were interchangeable only while envFromOS decided presence with
// os.Getenv, and that conflation is the defect the reviewer caught.
//
// t.Setenv is called first for its bookkeeping alone: it records the variable's
// original value and restores it when the test ends. The testing package has no
// t.Unsetenv, so the removal is done with os.Unsetenv straight after.
func unsetRelayEnv(t *testing.T) {
	t.Helper()
	for _, k := range relayEnvKeys {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatalf("unset %s: %v", k, err)
		}
	}
}

// TestEachEnvKeyLandsInItsOwnField pins BOTH halves of envFromOS: that every
// name has a destination, and that it has the RIGHT one.
//
// Order is the half a length check cannot see. envFromOS indexes its
// destination slice by the position of the name in requiredEnvKeys, so swapping
// two names in that list alone compiles, runs, passes a length check, and files
// the daemon socket path under the run ID. Nothing downstream notices: both are
// non-empty strings, so the relay dials whatever the run ID happened to be.
//
// Giving each variable a DISTINCT value is what makes the permutation visible.
// The wantByKey map below is deliberately a SECOND, hand-written statement of
// the intended pairing — a mapping derived from the code could not disagree
// with the code, and disagreeing with it is this test's entire job.
//
// A length mismatch still fails here too, as the index panic inside envFromOS.
func TestEachEnvKeyLandsInItsOwnField(t *testing.T) {
	unsetRelayEnv(t)
	for _, k := range relayEnvKeys {
		t.Setenv(k, "value-of-"+k)
	}

	e, err := envFromOS()
	if err != nil {
		t.Fatalf("envFromOS() with every variable set = %v, want no error", err)
	}

	gotByKey := map[string]string{
		"HARMONIK_RUN_ID":             e.RunID,
		"HARMONIK_DAEMON_SOCKET":      e.DaemonSocket,
		"HARMONIK_WORKSPACE_PATH":     e.WorkspacePath,
		"HARMONIK_HANDLER_SESSION_ID": e.HandlerSessionID,
		"HARMONIK_CLAUDE_SESSION_ID":  e.ClaudeSessionID,
		"HARMONIK_WORKFLOW_ID":        e.WorkflowID,
		"HARMONIK_NODE_ID":            e.NodeID,
		"HARMONIK_AGENT_TYPE":         e.AgentType,
		"HARMONIK_PHASE":              e.Phase,
	}
	for _, k := range relayEnvKeys {
		got, mapped := gotByKey[k]
		if !mapped {
			t.Errorf("%s is read by envFromOS but this test names no field for it; add one", k)
			continue
		}
		if want := "value-of-" + k; got != want {
			t.Errorf("the field fed by %s holds %q, want %q: the value of another variable landed in this field", k, got, want)
		}
	}
}

// stopStdin is a well-formed Stop hook payload. session_id matches the value the
// wired test below puts in HARMONIK_CLAUDE_SESSION_ID.
const stopStdin = `{"session_id":"c1","hook_event_name":"Stop",` +
	`"transcript_path":"/tmp/t.jsonl","cwd":"/tmp","message":"done"}`

// TestUnsetRelayEnvRemovesTheVariablesRatherThanEmptyingThem holds the helper
// itself honest. Every test below is stated in terms of what is PRESENT, so a
// helper that quietly exports empty strings would make all of them measure a
// case other than the one their name claims.
func TestUnsetRelayEnvRemovesTheVariablesRatherThanEmptyingThem(t *testing.T) {
	for _, k := range relayEnvKeys {
		t.Setenv(k, "leftover")
	}

	unsetRelayEnv(t)

	for _, k := range relayEnvKeys {
		if v, present := os.LookupEnv(k); present {
			t.Fatalf("%s is still present with value %q; the helper must remove it, not empty it", k, v)
		}
	}
}

// TestRelayStaysQuietWhenNoHarmonikVariableIsSet is the guard on the fix. A
// developer session outside harmonik must see exit 0 and an empty stderr.
func TestRelayStaysQuietWhenNoHarmonikVariableIsSet(t *testing.T) {
	unsetRelayEnv(t)

	var stderr bytes.Buffer
	rc := Run("Stop", strings.NewReader(stopStdin), &stderr, nil)

	if rc != 0 {
		t.Fatalf("exit code = %d, want 0: a session with no harmonik wiring is not a harmonik session; stderr=%q", rc, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty: nothing is wrong outside harmonik", stderr.String())
	}
}

// TestRelayReportsAWiredSessionWithAMissingVariable is the defect. Seven of the
// eight required variables are present, so this IS a harmonik-managed session,
// and the Stop signal cannot be delivered. The relay must say so.
//
// Against the old code this fails with "exit code = 0": the relay reported
// success for a signal it never sent.
func TestRelayReportsAWiredSessionWithAMissingVariable(t *testing.T) {
	unsetRelayEnv(t)
	t.Setenv("HARMONIK_RUN_ID", "r1")
	t.Setenv("HARMONIK_WORKSPACE_PATH", "/tmp")
	t.Setenv("HARMONIK_HANDLER_SESSION_ID", "h1")
	t.Setenv("HARMONIK_CLAUDE_SESSION_ID", "c1")
	t.Setenv("HARMONIK_WORKFLOW_ID", "w1")
	t.Setenv("HARMONIK_NODE_ID", "n1")
	t.Setenv("HARMONIK_AGENT_TYPE", "claude-code")
	// HARMONIK_DAEMON_SOCKET stays unset: there is nowhere to send the signal.

	var stderr bytes.Buffer
	rc := Run("Stop", strings.NewReader(stopStdin), &stderr, nil)

	if rc != 1 {
		t.Fatalf("exit code = %d, want 1: a wired session that cannot deliver its Stop must report it; stderr=%q", rc, stderr.String())
	}
	// The message has to name the variable, or the reader learns only that
	// something is wrong.
	if !strings.Contains(stderr.String(), "HARMONIK_DAEMON_SOCKET") {
		t.Fatalf("stderr = %q, want it to name the absent variable HARMONIK_DAEMON_SOCKET", stderr.String())
	}
}

// TestRelayReportsAWiredSessionWhoseVariablesAreAllEmpty is the case an
// emptiness test cannot see. All eight required variables are EXPORTED and set
// to "", which is a launcher that ran and produced nothing usable — the daemon
// socket path is "", so the Stop signal has nowhere to go. os.Getenv reports
// the same "" for these as for eight variables that were never exported, so the
// pre-review code counted 8 of 8 "missing", concluded this was not a harmonik
// session, and exited 0 without a word. That is the vacuous success the whole
// fix exists to remove, reachable by a different door.
func TestRelayReportsAWiredSessionWhoseVariablesAreAllEmpty(t *testing.T) {
	unsetRelayEnv(t)
	for _, k := range relayEnvKeys {
		t.Setenv(k, "")
	}

	var stderr bytes.Buffer
	rc := Run("Stop", strings.NewReader(stopStdin), &stderr, nil)

	if rc != 1 {
		t.Fatalf("exit code = %d, want 1: eight exported-but-empty variables are broken wiring, not the absence of wiring; stderr=%q", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "present but empty") {
		t.Fatalf("stderr = %q, want it to say the variables are present but empty, which is a different repair from absent ones", stderr.String())
	}
	if !strings.Contains(stderr.String(), "HARMONIK_DAEMON_SOCKET") {
		t.Fatalf("stderr = %q, want it to name HARMONIK_DAEMON_SOCKET", stderr.String())
	}
}

// TestRelayReportsAWiredSessionThatMixesAbsentAndEmptyVariables covers the
// realistic shape of a half-built environment: some variables never arrived,
// one arrived with nothing in it. The diagnostic must separate the two, because
// the repair differs — an absent variable is a launcher that skipped an export,
// an empty one is a launcher that exported a value it failed to compute.
func TestRelayReportsAWiredSessionThatMixesAbsentAndEmptyVariables(t *testing.T) {
	unsetRelayEnv(t)
	t.Setenv("HARMONIK_RUN_ID", "r1")
	t.Setenv("HARMONIK_WORKSPACE_PATH", "/tmp")
	t.Setenv("HARMONIK_DAEMON_SOCKET", "") // computed to nothing
	// The remaining five required variables stay unset.

	var stderr bytes.Buffer
	rc := Run("Stop", strings.NewReader(stopStdin), &stderr, nil)

	if rc != 1 {
		t.Fatalf("exit code = %d, want 1; stderr=%q", rc, stderr.String())
	}
	got := stderr.String()
	if !strings.Contains(got, "absent: ") || !strings.Contains(got, "present but empty: HARMONIK_DAEMON_SOCKET") {
		t.Fatalf("stderr = %q, want it to list the absent variables and HARMONIK_DAEMON_SOCKET as present but empty, separately", got)
	}
	if !strings.Contains(got, "HARMONIK_CLAUDE_SESSION_ID") {
		t.Fatalf("stderr = %q, want the absent list to name HARMONIK_CLAUDE_SESSION_ID", got)
	}
}

// TestRelayStaysQuietOnAnUnknownEventKindEvenWhenWired holds the CHB-011 no-op
// in place. The env check must not fire before the unknown-kind check.
func TestRelayStaysQuietOnAnUnknownEventKindEvenWhenWired(t *testing.T) {
	unsetRelayEnv(t)
	t.Setenv("HARMONIK_RUN_ID", "r1")

	var stderr bytes.Buffer
	rc := Run("PreToolUse", strings.NewReader(stopStdin), &stderr, nil)

	if rc != 0 || stderr.Len() != 0 {
		t.Fatalf("exit code = %d, stderr = %q; want 0 and empty for an event kind the relay does not handle", rc, stderr.String())
	}
}
