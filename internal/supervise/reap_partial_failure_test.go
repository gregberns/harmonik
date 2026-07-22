package supervise

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// perSessionKillAdapter fails the kill for exactly one named session and
// succeeds for every other, so a test can drive a PARTIAL-failure reap pass.
type perSessionKillAdapter struct {
	sessions []FlywheelSession
	failFor  string
	failErr  error
	killed   []string
}

func (f *perSessionKillAdapter) ListFlywheelSessions(_ context.Context) ([]FlywheelSession, error) {
	return f.sessions, nil
}

func (f *perSessionKillAdapter) KillSession(_ context.Context, name string) error {
	f.killed = append(f.killed, name)
	if name == f.failFor {
		return f.failErr
	}
	return nil
}

// TestReap_PartialKillFailure_KeepsGoingAndReportsSuccesses is the regression
// gate for the "return on the first kill error" bug: a pass that kills A and B
// and then fails on C must (a) still attempt every candidate, (b) report A and B
// in Reaped with one tmux_orphan_reaped event each, (c) NOT report C, and
// (d) still return the kill error so the caller exits nonzero.
//
// Reverting the fix (returning on the first kill error) makes this fail: D is
// never attempted and its event is lost.
func TestReap_PartialKillFailure_KeepsGoingAndReportsSuccesses(t *testing.T) {
	a := "harmonik-aaaaaaaaaaaa-flywheel"
	b := "harmonik-bbbbbbbbbbbb-flywheel"
	c := "harmonik-cccccccccccc-flywheel" // kill fails here
	d := "harmonik-dddddddddddd-flywheel" // must still be attempted after c fails

	boom := errors.New("permission denied")
	adapter := &perSessionKillAdapter{
		sessions: []FlywheelSession{
			{Name: a, PaneDead: true},
			{Name: b, PaneDead: true},
			{Name: c, PaneDead: true},
			{Name: d, PaneDead: true},
		},
		failFor: c,
		failErr: boom,
	}

	result, err := ReapOrphanFlywheelSessions(context.Background(), adapter, ReapOptions{})

	if err == nil {
		t.Fatal("partial kill failure returned nil error; the pass must still fail")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("returned error %v does not wrap the kill error %v", err, boom)
	}
	if !strings.Contains(err.Error(), c) {
		t.Errorf("error %q does not name the failing session %q", err, c)
	}

	// Every candidate was attempted — the loop did not abort on c.
	wantAttempts := []string{a, b, c, d}
	if len(adapter.killed) != len(wantAttempts) {
		t.Fatalf("kill attempts = %v, want %v (loop aborted early)", adapter.killed, wantAttempts)
	}
	for i, want := range wantAttempts {
		if adapter.killed[i] != want {
			t.Fatalf("kill attempts = %v, want %v", adapter.killed, wantAttempts)
		}
	}

	// The successes are reported; the failure is not.
	wantReaped := []string{a, b, d}
	if len(result.Reaped) != len(wantReaped) {
		t.Fatalf("result.Reaped = %v, want %v", result.Reaped, wantReaped)
	}
	for i, want := range wantReaped {
		if result.Reaped[i] != want {
			t.Fatalf("result.Reaped = %v, want %v", result.Reaped, wantReaped)
		}
	}
	for _, name := range result.Reaped {
		if name == c {
			t.Fatalf("failed kill %q reported as reaped", c)
		}
	}

	// One event per successful kill, in reap order — this is the observability
	// that the first-error return silently dropped.
	if len(result.Events) != len(wantReaped) {
		t.Fatalf("result.Events = %+v, want %d events (%v)", result.Events, len(wantReaped), wantReaped)
	}
	for i, ev := range result.Events {
		if ev.Event != "tmux_orphan_reaped" {
			t.Errorf("event[%d].Event = %q, want tmux_orphan_reaped", i, ev.Event)
		}
		if ev.Session != wantReaped[i] {
			t.Errorf("event[%d].Session = %q, want %q", i, ev.Session, wantReaped[i])
		}
	}
}

// TestReap_MultipleKillFailures_JoinsEveryError verifies the aggregate error
// names every failing session, not just the first.
func TestReap_MultipleKillFailures_JoinsEveryError(t *testing.T) {
	a := "harmonik-aaaaaaaaaaaa-flywheel"
	b := "harmonik-bbbbbbbbbbbb-flywheel"

	boom := errors.New("permission denied")
	adapter := &fakeReapAdapter{
		sessions: []FlywheelSession{
			{Name: a, PaneDead: true},
			{Name: b, PaneDead: true},
		},
		killErr: boom,
	}

	result, err := ReapOrphanFlywheelSessions(context.Background(), adapter, ReapOptions{})
	if err == nil {
		t.Fatal("expected an error when every kill fails")
	}
	if !strings.Contains(err.Error(), a) || !strings.Contains(err.Error(), b) {
		t.Errorf("joined error %q must name both %q and %q", err, a, b)
	}
	if len(result.Reaped) != 0 || len(result.Events) != 0 {
		t.Fatalf("failed kills reported as successful: %+v", result)
	}
}

// TestOSReapAdapter_KillVanishedServerIsNoOp binds finding 2(a): if the tmux
// SERVER exits between the list and the kill, tmux does not say "can't find
// session" — it says "no server running on <socket>" (stale socket file) or
// "error connecting to <socket> (No such file or directory)" (socket gone too).
// Both are the same benign already-dead race and must stay no-ops.
//
// The two strings are verbatim tmux 3.6a output, captured with:
//
//	tmux -L gone kill-session -t =nosuch     (after `tmux -L gone kill-server`)
//	tmux -L never list-sessions              (socket that never existed)
func TestOSReapAdapter_KillVanishedServerIsNoOp(t *testing.T) {
	cases := map[string]string{
		"stale_socket_no_server_running": "no server running on /private/tmp/tmux-502/hkstale-80458",
		"socket_gone_connect_enoent":     "error connecting to /private/tmp/tmux-502/hkgone-80458 (No such file or directory)",
	}
	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			installReapTmuxFixture(t, msg)
			if err := (osReapAdapter{}).KillSession(context.Background(), "harmonik-0123456789ab-flywheel"); err != nil {
				t.Fatalf("vanished tmux server: KillSession = %v, want nil (benign race)", err)
			}
		})
	}
}

// TestOSReapAdapter_ListVanishedServerIsNoOp is the list-side twin: a host whose
// tmux socket never existed must enumerate cleanly, not error. (The pre-existing
// "no server running" case is covered by TestOSReapAdapter_NoServerIsEmpty.)
func TestOSReapAdapter_ListVanishedServerIsNoOp(t *testing.T) {
	installReapTmuxFixture(t, "error connecting to /private/tmp/tmux-502/hkgone-80458 (No such file or directory)")

	got, err := (osReapAdapter{}).ListFlywheelSessions(context.Background())
	if err != nil || got != nil {
		t.Fatalf("socket-absent list: got sessions=%v err=%v, want nil,nil", got, err)
	}
}

// TestOSReapAdapter_MissingTmuxBinaryIsNoOp binds finding 2(b): on a host with
// no tmux at all, enumeration is a clean no-op, so `harmonik supervise reap`
// still exits 0. Detection is structural (exec.ErrNotFound), not a string match
// on the OS's "executable file not found" phrasing.
func TestOSReapAdapter_MissingTmuxBinaryIsNoOp(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // an empty dir: no tmux anywhere on PATH

	got, err := (osReapAdapter{}).ListFlywheelSessions(context.Background())
	if err != nil || got != nil {
		t.Fatalf("tmux binary absent: got sessions=%v err=%v, want nil,nil (clean no-op)", got, err)
	}
}

// TestOSReapAdapter_ListKeepsRealFailures guards against over-widening the
// tolerated cases: a connect failure with a DIFFERENT errno is a real problem
// and must still surface.
func TestOSReapAdapter_ListKeepsRealFailures(t *testing.T) {
	installReapTmuxFixture(t, "error connecting to /private/tmp/tmux-502/locked (Permission denied)")

	got, err := (osReapAdapter{}).ListFlywheelSessions(context.Background())
	if err == nil {
		t.Fatalf("permission-denied connect: got sessions=%v err=nil, want an error", got)
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("error %q omits tmux's output", err)
	}

	// Same for the kill path.
	if killErr := (osReapAdapter{}).KillSession(context.Background(), "harmonik-0123456789ab-flywheel"); killErr == nil {
		t.Fatal("permission-denied kill: got nil, want an error")
	}
}

// TestReap_MissingTmuxBinary_EndToEndNoOp exercises the whole reaper against the
// real OS adapter on a PATH with no tmux: zero scanned, zero reaped, no error.
func TestReap_MissingTmuxBinary_EndToEndNoOp(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	result, err := ReapOrphanFlywheelSessions(context.Background(), OSReapAdapter(), ReapOptions{
		DaemonStartTime: time.Now(),
	})
	if err != nil {
		t.Fatalf("no tmux on PATH: reap returned %v, want a clean no-op", err)
	}
	if result.Scanned != 0 || len(result.Reaped) != 0 || len(result.Events) != 0 {
		t.Fatalf("no tmux on PATH: got %+v, want zero-everything", result)
	}
}

// TestOSReapAdapter_NonExecutableTmuxIsAlsoNoOp records a measured edge of the
// missing-binary tolerance: a tmux file on PATH that is not executable is
// reported by os/exec.LookPath as exec.ErrNotFound (not fs.ErrPermission), so it
// reads as "no tmux installed" and is a no-op too. Documented, not aspirational
// — asserting an error here would be asserting behavior Go does not provide.
func TestOSReapAdapter_NonExecutableTmuxIsAlsoNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("write non-executable tmux: %v", err)
	}
	t.Setenv("PATH", dir)

	got, err := (osReapAdapter{}).ListFlywheelSessions(context.Background())
	if err != nil || got != nil {
		t.Fatalf("non-executable tmux: got sessions=%v err=%v, want nil,nil", got, err)
	}
}
