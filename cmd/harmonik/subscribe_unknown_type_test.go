package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSubscribeRefusesUnknownTypeBeforeDial is the end-to-end proof. It runs
// the real subcommand against a project directory with NO daemon socket in it.
//
// Before the fix the unknown type sailed through argument parsing, the command
// went on to dial, found no socket, and exited 17 ("daemon not running") — the
// bad filter was never mentioned. After the fix the filter is refused first, so
// the exit code is 1 and stderr names the offending value. The two outcomes are
// distinguishable without a daemon, which is the point: the refusal must not
// depend on anything being up.
func TestSubscribeRefusesUnknownTypeBeforeDial(t *testing.T) {
	projectDir := t.TempDir()

	var code int
	_, stderr := captureStd(t, func() {
		code = runSubscribeSubcommand([]string{
			"--project", projectDir,
			"--types", "not_a_real_type",
		})
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 (argument error). Exit 17 means the unknown type "+
			"was accepted and the command went on to dial: got stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "not_a_real_type") {
		t.Errorf("stderr does not name the bad value; got %q", stderr)
	}
	if !strings.Contains(stderr, "--types") {
		t.Errorf("stderr does not name the offending flag; got %q", stderr)
	}
	if !strings.Contains(stderr, "--list-types") {
		t.Errorf("stderr does not name the remedy; got %q", stderr)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik")); !os.IsNotExist(err) {
		t.Errorf("subscribe touched the project directory before refusing the filter: %v", err)
	}
}

// TestSubscribeRefusesUnknownTypeUnderFollow covers the --follow path, which
// reconnects on its own. An accepted-but-impossible filter there is worse than
// on the plain path: the client re-dials forever and keeps looking alive.
func TestSubscribeRefusesUnknownTypeUnderFollow(t *testing.T) {
	projectDir := t.TempDir()

	var code int
	_, stderr := captureStd(t, func() {
		code = runSubscribeSubcommand([]string{
			"--project", projectDir,
			"--types", "run_completed,not_a_real_type",
			"--follow",
		})
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "not_a_real_type") {
		t.Errorf("stderr does not name the bad value; got %q", stderr)
	}
}

// TestSubscribeAcceptsRealTypes guards the other direction. A validator that
// refuses valid input is a worse outage than the bug it fixes, so the real
// names in the canonical monitor pattern must still pass, and so must the
// synthetic "heartbeat" line type, which is not a registry event type but is
// documented and accepted.
func TestSubscribeAcceptsRealTypes(t *testing.T) {
	for _, types := range [][]string{
		nil,
		{"run_completed", "run_failed"},
		{"heartbeat", "run_completed"},
		{"agent_message"},
		{"epic_completed"},
		{"decision_resolved", "decision_withdrawn"},
	} {
		if err := validateSubscribeTypes(types); err != nil {
			t.Errorf("validateSubscribeTypes(%v) = %v, want nil", types, err)
		}
	}
}

// TestSubscribeUnknownTypeSuggestsNearest checks the message does the one thing
// that saves the operator a round trip: name the type they meant.
func TestSubscribeUnknownTypeSuggestsNearest(t *testing.T) {
	for _, tc := range []struct {
		bad  string
		want string
	}{
		{bad: "run_complete", want: "run_completed"},     // dropped suffix
		{bad: "run_faild", want: "run_failed"},           // dropped character
		{bad: "agent_mesage", want: "agent_message"},     // dropped character
		{bad: "queue_submited", want: "queue_submitted"}, // dropped character
	} {
		err := validateSubscribeTypes([]string{tc.bad})
		if err == nil {
			t.Errorf("validateSubscribeTypes(%q) = nil, want an error", tc.bad)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("validateSubscribeTypes(%q) message %q does not suggest %q", tc.bad, err, tc.want)
		}
	}
}

// TestSubscribeUnknownTypeReportsEveryBadValue makes sure a list with two typos
// does not send the operator round the loop twice.
func TestSubscribeUnknownTypeReportsEveryBadValue(t *testing.T) {
	err := validateSubscribeTypes([]string{"zzzz_not_a_type", "run_completed", "qqqq_also_not"})
	if err == nil {
		t.Fatal("validateSubscribeTypes returned nil for two unknown types")
	}
	for _, want := range []string{"zzzz_not_a_type", "qqqq_also_not"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not name %q", err, want)
		}
	}
}

// TestSubscribeListTypesPrintsVocabulary checks the remedy the error message
// points at actually works, and that it prints the names the monitor pattern
// uses.
func TestSubscribeListTypesPrintsVocabulary(t *testing.T) {
	var code int
	stdout, _ := captureStd(t, func() {
		code = runSubscribeSubcommand([]string{"--list-types"})
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	lines := strings.Fields(stdout)
	if len(lines) < 50 {
		t.Fatalf("--list-types printed %d names, want the full registry", len(lines))
	}
	for _, want := range []string{"run_completed", "run_failed", "heartbeat"} {
		if !strings.Contains(stdout, want+"\n") {
			t.Errorf("--list-types output is missing %q", want)
		}
	}
}
