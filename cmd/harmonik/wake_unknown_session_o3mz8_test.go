package main

// wake_unknown_session_o3mz8_test.go — `harmonik wake --agent <name>` must not
// report a wake for a name this project has no session for.
//
// The old surface answered exit 0 with "wake: <name> nudged" for every string,
// including strings that cannot name a session at all:
//
//	harmonik wake --agent nosuchagent  -> rc=0  "wake: nosuchagent nudged"
//	harmonik wake --agent ../../etc    -> rc=0  "wake: ../../etc nudged"
//	harmonik wake --agent "a b c"      -> rc=0  "wake: a b c nudged"
//	harmonik wake --agent alpha        -> rc=0  "wake: alpha nudged"
//
// The last line is a real session and the first three are not, so nothing in
// the output separated them. wake is the fleet-stall escape hatch, so it lies
// exactly when somebody is deciding whether the wake path or the session is
// broken.
//
// The daemon cannot supply the answer: HandleDaemonWake treats an unmatched
// name as informational and returns success, and the reply carries no count.
// So the check lives in the CLI, over the same three sources the arbiter keys
// its sleeping map by.
//
// Bead ref: hk-o3mz8.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
)

// wakeProjectWithCrew builds a temp project holding one crew registry record.
func wakeProjectWithCrew(t *testing.T, crewName string) string {
	t.Helper()
	dir := shortProjectDir(t)
	if err := crew.Write(dir, crew.Record{
		SchemaVersion: 1,
		Name:          crewName,
		SessionID:     "00000000-0000-4000-8000-00000000000a",
		Queue:         crewName,
		Handle:        "harmonik-test-crew-" + crewName,
		StartedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write crew record: %v", err)
	}
	return dir
}

// TestWakeRefusesANameNoSessionCanHave covers the strings that cannot key a
// session under any circumstance. Each one must be refused by name.
func TestWakeRefusesANameNoSessionCanHave(t *testing.T) {
	ctx := context.Background()
	dir := shortProjectDir(t)

	for _, name := range []string{"../../etc", "a b c", "Captain", "crew/one", strings.Repeat("x", 65)} {
		t.Run(name, func(t *testing.T) {
			out, code := captureSleepWakeIO(t, func() int {
				return runWakeSubcommand(ctx, []string{"--agent", name, "--project", dir})
			})
			if code != 1 {
				t.Fatalf("wake --agent %q exited %d, want 1 — a string that cannot name a session must be refused (out=%q)", name, code, out)
			}
			if strings.Contains(out, "nudged") && !strings.Contains(out, "Nothing was nudged") {
				t.Errorf("wake --agent %q claims a nudge: %q", name, out)
			}
			if !strings.Contains(out, name) {
				t.Errorf("the refusal for %q does not name it, so the operator cannot see the typo: %q", name, out)
			}
		})
	}
}

// TestWakeRefusesAnUnknownSessionName is the defect itself: a well-formed name
// that matches no session in this project.
func TestWakeRefusesAnUnknownSessionName(t *testing.T) {
	ctx := context.Background()
	dir := wakeProjectWithCrew(t, "alpha")

	out, code := captureSleepWakeIO(t, func() int {
		return runWakeSubcommand(ctx, []string{"--agent", "nosuchagent", "--project", dir})
	})
	if code != 1 {
		t.Fatalf("wake --agent nosuchagent exited %d, want 1 — this project has no such session (out=%q)", code, out)
	}
	if !strings.Contains(out, "nosuchagent") {
		t.Errorf("the refusal does not name the session it could not find: %q", out)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("the refusal does not list the names this project does know, so the operator cannot correct the typo: %q", out)
	}
}

// TestWakeReachesTheDaemonForANameThisProjectHas is the control. Every name the
// arbiter can key its sleeping map by must still get through to the daemon, or
// the fix has broken the escape hatch it was meant to make honest. Exit 17
// means the request passed validation and only the absent daemon stopped it.
func TestWakeReachesTheDaemonForANameThisProjectHas(t *testing.T) {
	ctx := context.Background()
	dir := wakeProjectWithCrew(t, "alpha")

	// A parked session with no crew record is keyed by session id, so its
	// on-disk marker has to count as a known name too.
	const strandedSession = "3f1c2a90-0000-4000-8000-00000000000b"
	markerPath := filepath.Join(dir, ".harmonik", ".sleeping."+strandedSession)
	if err := os.WriteFile(markerPath, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write sleep marker: %v", err)
	}

	for _, name := range []string{"captain", "watch", "alpha", strandedSession} {
		t.Run(name, func(t *testing.T) {
			out, code := captureSleepWakeIO(t, func() int {
				return runWakeSubcommand(ctx, []string{"--agent", name, "--project", dir})
			})
			if code != 17 {
				t.Fatalf("wake --agent %s exited %d, want 17 (no daemon socket) — validation must let a real session name through (out=%q)", name, code, out)
			}
			if !strings.Contains(out, "daemon not running") {
				t.Errorf("wake --agent %s stopped before the dial: %q", name, out)
			}
		})
	}
}

// serveAlwaysOkDaemon stands up a unix socket at the project's daemon.sock that
// answers every request with ok=true, which is what the real daemon does for a
// wake it could not match to any session.
func serveAlwaysOkDaemon(t *testing.T, projectDir string) {
	t.Helper()
	body, err := json.Marshal(sleepWakeSocketResponse{Ok: true})
	if err != nil {
		t.Fatalf("marshal canned wake reply: %v", err)
	}
	serveCannedUnixSocket(t, filepath.Join(projectDir, ".harmonik", "daemon.sock"), body)
}

// TestWakeAgainstALiveDaemonStillRefusesAnUnknownName is the bead reproduced in
// full. With a daemon answering ok to everything, the old code printed
// "wake: nosuchagent nudged" and exited 0. The refusal has to happen in the CLI
// and before the dial, because the reply cannot tell the two cases apart.
func TestWakeAgainstALiveDaemonStillRefusesAnUnknownName(t *testing.T) {
	ctx := context.Background()
	dir := wakeProjectWithCrew(t, "alpha")
	serveAlwaysOkDaemon(t, dir)

	out, code := captureSleepWakeIO(t, func() int {
		return runWakeSubcommand(ctx, []string{"--agent", "nosuchagent", "--project", dir})
	})
	if code == 0 {
		t.Fatalf("wake --agent nosuchagent exited 0 against a daemon that matched nothing: %q", out)
	}
	if code != 1 {
		t.Fatalf("wake --agent nosuchagent exited %d, want 1 (out=%q)", code, out)
	}
	if !strings.Contains(out, "nosuchagent") {
		t.Errorf("the refusal does not name the session it could not find: %q", out)
	}

	// The control: a real name still reaches the daemon and succeeds.
	out, code = captureSleepWakeIO(t, func() int {
		return runWakeSubcommand(ctx, []string{"--agent", "alpha", "--project", dir})
	})
	if code != 0 {
		t.Fatalf("wake --agent alpha exited %d, want 0 — a known session must still reach the daemon (out=%q)", code, out)
	}
}
