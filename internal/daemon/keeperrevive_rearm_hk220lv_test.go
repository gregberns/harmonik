package daemon

// keeperrevive_rearm_hk220lv_test.go — tests for tmuxSubstrate.ReArmCrewKeeperWindow,
// the re-arm seam the keeper-revive watcher publishes a
// session_keeper_watcher_revived event on.
//
// The contract under test is narrow and load-bearing: a nil return MUST mean a
// keeper process was actually spawned. The presence-based rule used on the
// crew-start path (ensureCrewKeeperWindow: "a window named keeper exists →
// done") is WRONG here, because the watcher only calls this after the flock has
// read unheld for a full grace window — a surviving "keeper" window at that
// point is a corpse left standing by tmux remain-on-exit, not a live keeper.
//
// Tests cover:
//   - No keeper window present → one keeper window spawned, nil error.
//   - STALE keeper window present → it is KILLED, then a fresh keeper window is
//     spawned (never a silent no-op reporting success).
//   - ListWindows failure (e.g. the session is gone) → non-nil error and no
//     spawn, so no false "revived" event can be published.
//   - KillWindow failure → non-nil error and no spawn.
//   - Empty session name → non-nil error, no tmux calls.
//
// Helper prefix: kra
//
// Bead ref: hk-220lv.

import (
	"context"
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers (prefix: kra)
// ─────────────────────────────────────────────────────────────────────────────

// kraAdapter is a tmux.Adapter double that records the calls
// ReArmCrewKeeperWindow makes and lets each one be failed on demand.
type kraAdapter struct {
	windows        []string
	listWindowsErr error
	killWindowErr  error
	newWindowErr   error

	killed     []tmux.WindowHandle
	newWindows []tmux.NewWindowIn
}

func (a *kraAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	if a.listWindowsErr != nil {
		return nil, a.listWindowsErr
	}
	return a.windows, nil
}

func (a *kraAdapter) KillWindow(_ context.Context, handle tmux.WindowHandle) error {
	a.killed = append(a.killed, handle)
	return a.killWindowErr
}

func (a *kraAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.newWindows = append(a.newWindows, params)
	if a.newWindowErr != nil {
		return tmux.Outcome{Err: a.newWindowErr}
	}
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}

func (a *kraAdapter) NewSessionIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{}
}
func (a *kraAdapter) ProbeTmux(_ context.Context) error                { return nil }
func (a *kraAdapter) ListSessions(_ context.Context) ([]string, error) { return nil, nil }
func (a *kraAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *kraAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "%1", nil
}
func (a *kraAdapter) KillSession(_ context.Context, _ string) error          { return nil }
func (a *kraAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error { return nil }
func (a *kraAdapter) PasteBuffer(_ context.Context, _, _ string) error       { return nil }
func (a *kraAdapter) SendKeysLiteral(_ context.Context, _, _ string) error   { return nil }
func (a *kraAdapter) SendKeysEnter(_ context.Context, _ string) error        { return nil }
func (a *kraAdapter) SendKeysQuit(_ context.Context, _ string) error         { return nil }
func (a *kraAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*kraAdapter)(nil)

// kraSubstrate builds a tmuxSubstrate over the adapter double.
func kraSubstrate(a *kraAdapter) *tmuxSubstrate {
	return &tmuxSubstrate{adapter: a, sessionName: "daemon-default", projectHash: core.ProjectHash("deadbeef1234")}
}

// kraKeeperWindows counts NewWindowIn calls that created a keeper window.
func kraKeeperWindows(a *kraAdapter) int {
	n := 0
	for _, w := range a.newWindows {
		if w.WindowName == tmux.WindowKeeper {
			n++
		}
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestReArmCrewKeeperWindow_NoExistingWindow_SpawnsKeeper_hk220lv: the ordinary
// case — tmux already removed the dead keeper's window, so there is nothing to
// kill and a fresh keeper is spawned.
func TestReArmCrewKeeperWindow_NoExistingWindow_SpawnsKeeper_hk220lv(t *testing.T) {
	t.Parallel()

	a := &kraAdapter{windows: []string{tmux.WindowAgent}}
	s := kraSubstrate(a)

	if err := s.ReArmCrewKeeperWindow(context.Background(), "kilo", "harmonik-abc123-crew-kilo", "/proj"); err != nil {
		t.Fatalf("ReArmCrewKeeperWindow: unexpected error: %v", err)
	}
	if got := len(a.killed); got != 0 {
		t.Errorf("KillWindow calls = %d; want 0 (regression: killing a window that was not there)", got)
	}
	if got := kraKeeperWindows(a); got != 1 {
		t.Errorf("keeper windows spawned = %d; want 1 (regression: the re-arm returned success without "+
			"spawning a keeper, leaving the crew unmonitored while the event log claims it was revived)", got)
	}
	if got := a.newWindows[0].Session; got != "harmonik-abc123-crew-kilo" {
		t.Errorf("spawn session = %q; want %q", got, "harmonik-abc123-crew-kilo")
	}
}

// TestReArmCrewKeeperWindow_StaleWindow_KilledThenRespawned_hk220lv: this is the
// blocking case. A "keeper" window that survives its dead process (tmux
// remain-on-exit) must NOT short-circuit the re-arm — it must be killed and
// replaced, or the sweep reports green forever while spawning nothing.
func TestReArmCrewKeeperWindow_StaleWindow_KilledThenRespawned_hk220lv(t *testing.T) {
	t.Parallel()

	a := &kraAdapter{windows: []string{tmux.WindowAgent, tmux.WindowKeeper}}
	s := kraSubstrate(a)

	if err := s.ReArmCrewKeeperWindow(context.Background(), "kilo", "harmonik-abc123-crew-kilo", "/proj"); err != nil {
		t.Fatalf("ReArmCrewKeeperWindow: unexpected error: %v", err)
	}

	wantHandle := tmux.WindowHandle("harmonik-abc123-crew-kilo:" + tmux.WindowKeeper)
	if len(a.killed) != 1 || a.killed[0] != wantHandle {
		t.Errorf("killed windows = %v; want exactly [%q] (regression: the stale keeper window was left standing, "+
			"so NewWindowIn will hit ErrWindowCollision forever and the crew is never re-monitored)",
			a.killed, wantHandle)
	}
	if got := kraKeeperWindows(a); got != 1 {
		t.Errorf("keeper windows spawned = %d; want 1 (regression: presence of a window named %q was treated as "+
			"a successful re-arm — report-green-do-nothing, the exact bug hk-220lv closes)", got, tmux.WindowKeeper)
	}
}

// TestReArmCrewKeeperWindow_ListWindowsFails_ErrorsWithoutSpawn_hk220lv: a gone
// session (ErrNoSession) must surface as an error so the watcher counts a failed
// attempt instead of publishing session_keeper_watcher_revived.
func TestReArmCrewKeeperWindow_ListWindowsFails_ErrorsWithoutSpawn_hk220lv(t *testing.T) {
	t.Parallel()

	a := &kraAdapter{listWindowsErr: tmux.ErrNoSession}
	s := kraSubstrate(a)

	err := s.ReArmCrewKeeperWindow(context.Background(), "kilo", "harmonik-abc123-crew-kilo", "/proj")
	if err == nil {
		t.Fatal("ReArmCrewKeeperWindow: want non-nil error when the tmux session is gone; got nil " +
			"(regression: a nil return publishes session_keeper_watcher_revived for a keeper that was " +
			"never spawned)")
	}
	if !errors.Is(err, tmux.ErrNoSession) {
		t.Errorf("error = %v; want it to wrap tmux.ErrNoSession so the cause is diagnosable", err)
	}
	if got := kraKeeperWindows(a); got != 0 {
		t.Errorf("keeper windows spawned = %d; want 0 (a spawn into a nonexistent session cannot succeed)", got)
	}
}

// TestReArmCrewKeeperWindow_KillFails_ErrorsWithoutSpawn_hk220lv: if the stale
// window cannot be killed, the spawn would collide — report the failure rather
// than a phantom success.
func TestReArmCrewKeeperWindow_KillFails_ErrorsWithoutSpawn_hk220lv(t *testing.T) {
	t.Parallel()

	killErr := errors.New("tmux: kill-window refused")
	a := &kraAdapter{windows: []string{tmux.WindowKeeper}, killWindowErr: killErr}
	s := kraSubstrate(a)

	err := s.ReArmCrewKeeperWindow(context.Background(), "kilo", "harmonik-abc123-crew-kilo", "/proj")
	if err == nil {
		t.Fatal("ReArmCrewKeeperWindow: want non-nil error when the stale window cannot be killed; got nil")
	}
	if !errors.Is(err, killErr) {
		t.Errorf("error = %v; want it to wrap the KillWindow cause", err)
	}
	if got := kraKeeperWindows(a); got != 0 {
		t.Errorf("keeper windows spawned = %d; want 0 (spawning over a surviving window hits ErrWindowCollision)", got)
	}
}

// TestReArmCrewKeeperWindow_EmptySession_ErrorsWithoutTmuxCalls_hk220lv: a crew
// record with no tmux handle yields no session name; refuse rather than touching
// tmux with an empty target.
func TestReArmCrewKeeperWindow_EmptySession_ErrorsWithoutTmuxCalls_hk220lv(t *testing.T) {
	t.Parallel()

	a := &kraAdapter{}
	s := kraSubstrate(a)

	if err := s.ReArmCrewKeeperWindow(context.Background(), "kilo", "", "/proj"); err == nil {
		t.Fatal("ReArmCrewKeeperWindow: want non-nil error for an empty session name; got nil")
	}
	if len(a.newWindows) != 0 || len(a.killed) != 0 {
		t.Errorf("tmux calls made with an empty session target: newWindows=%d killed=%d; want 0/0",
			len(a.newWindows), len(a.killed))
	}
}
