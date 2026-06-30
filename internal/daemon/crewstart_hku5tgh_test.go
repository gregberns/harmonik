package daemon

// crewstart_hku5tgh_test.go — regression tests for hk-u5tgh.
//
// Bug: when ctx-watchdog does crew-stop + crew-start, crew-stop removes the
// registry but sometimes cannot kill the tmux session (e.g. the pane is frozen).
// The subsequent crew-start hits ErrWindowCollision in SpawnCrewSession and
// fails entirely, leaving the crew alive but keeper-less.
//
// Fix: SpawnCrewSession now handles ErrWindowCollision gracefully by:
//   (a) checking whether the keeper window is already present via ListWindows
//   (b) calling spawnCrewKeeperWindow if the keeper is absent
//   (c) returning the existing session (existingCrewSession) so HandleCrewStart
//       can update the registry handle and run the keeper liveness probe.
//
// Tests:
//   - Collision with keeper absent → keeper window added, no error.
//   - Collision with keeper present → no duplicate keeper window, no error.
//   - Collision with ListWindows error → spawnCrewKeeperWindow attempted anyway.
//
// Bead ref: hk-u5tgh.

import (
	"context"
	"fmt"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// crewCollisionAdapter — adapter double that simulates a surviving crew session
// ─────────────────────────────────────────────────────────────────────────────

// crewCollisionAdapter simulates a tmux adapter where the crew session already
// exists (NewSessionIn always returns ErrWindowCollision). ListWindows returns
// the configured existingWindows to exercise the keeper-present / keeper-absent
// branches. NewWindowIn calls are recorded for assertion.
type crewCollisionAdapter struct {
	existingWindows []string           // returned by ListWindows for the crew session
	listWindowsErr  error              // if non-nil, ListWindows returns this error
	newWindowCalls  []tmux.NewWindowIn // records all NewWindowIn calls (keeper re-arm)
}

func (a *crewCollisionAdapter) NewSessionIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Err: tmux.ErrWindowCollision}
}

func (a *crewCollisionAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.newWindowCalls = append(a.newWindowCalls, params)
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}

func (a *crewCollisionAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	if a.listWindowsErr != nil {
		return nil, a.listWindowsErr
	}
	return a.existingWindows, nil
}

func (a *crewCollisionAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *crewCollisionAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}

func (a *crewCollisionAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error {
	return nil
}

func (a *crewCollisionAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *crewCollisionAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "%99", nil
}
func (a *crewCollisionAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *crewCollisionAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *crewCollisionAdapter) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *crewCollisionAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *crewCollisionAdapter) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *crewCollisionAdapter) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *crewCollisionAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*crewCollisionAdapter)(nil)

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestSpawnCrewSession_Collision_NoKeeperWindow_hku5tgh verifies that when the
// crew session already exists (ErrWindowCollision) AND the "keeper" window is
// absent, SpawnCrewSession re-arms the keeper window (via NewWindowIn) and
// returns the existing session without error.
func TestSpawnCrewSession_Collision_NoKeeperWindow_hku5tgh(t *testing.T) {
	const testHash core.ProjectHash = "deadbeef1234"
	adapter := &crewCollisionAdapter{
		existingWindows: []string{tmux.WindowAgent}, // keeper absent
	}
	sub := &tmuxSubstrate{adapter: adapter, sessionName: "daemon-default", projectHash: testHash}

	spawn := handler.SubstrateSpawn{
		Cwd:  "/home/op/proj",
		Env:  []string{"HARMONIK_PROJECT=/home/op/proj"},
		Argv: []string{"claude", "--remote-control", "paul"},
	}

	sess, err := sub.SpawnCrewSession(context.Background(), "paul", spawn)
	if err != nil {
		t.Fatalf("SpawnCrewSession: unexpected error on collision (want graceful recovery): %v", err)
	}
	if sess == nil {
		t.Fatal("SpawnCrewSession: returned nil session on collision (want existing session)")
	}

	// The keeper window must have been added via NewWindowIn.
	if len(adapter.newWindowCalls) == 0 {
		t.Error("NewWindowIn not called: keeper window was not re-armed on collision")
	}
	var keeperAdded bool
	for _, p := range adapter.newWindowCalls {
		if p.WindowName == tmux.WindowKeeper {
			keeperAdded = true
		}
	}
	if !keeperAdded {
		t.Errorf("NewWindowIn calls = %v, want a call with WindowName=%q (keeper re-arm)", adapter.newWindowCalls, tmux.WindowKeeper)
	}

	// The returned session handle must reference the existing session's agent window.
	wantSession := lifecycle.TmuxSessionName(testHash, "crew-paul")
	wantHandle := tmux.WindowHandle(wantSession + ":" + tmux.WindowAgent)
	if whe, ok := sess.(windowHandleExposer); ok {
		if got := tmux.WindowHandle(whe.WindowHandle()); got != wantHandle {
			t.Errorf("session handle = %q, want %q (existing agent window)", got, wantHandle)
		}
	} else {
		t.Error("session does not implement windowHandleExposer")
	}
}

// TestSpawnCrewSession_Collision_KeeperWindowPresent_hku5tgh verifies that when
// the crew session already exists AND the "keeper" window is already present,
// SpawnCrewSession does NOT add a duplicate keeper window and returns no error.
func TestSpawnCrewSession_Collision_KeeperWindowPresent_hku5tgh(t *testing.T) {
	const testHash core.ProjectHash = "deadbeef1234"
	adapter := &crewCollisionAdapter{
		existingWindows: []string{tmux.WindowAgent, tmux.WindowKeeper}, // keeper present
	}
	sub := &tmuxSubstrate{adapter: adapter, sessionName: "daemon-default", projectHash: testHash}

	spawn := handler.SubstrateSpawn{
		Cwd:  "/home/op/proj",
		Env:  []string{"HARMONIK_PROJECT=/home/op/proj"},
		Argv: []string{"claude", "--remote-control", "paul"},
	}

	sess, err := sub.SpawnCrewSession(context.Background(), "paul", spawn)
	if err != nil {
		t.Fatalf("SpawnCrewSession: unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("SpawnCrewSession: returned nil session (want existing session)")
	}

	// No NewWindowIn call — the keeper window already exists.
	for _, p := range adapter.newWindowCalls {
		if p.WindowName == tmux.WindowKeeper {
			t.Errorf("NewWindowIn called with keeper window (%q): must not add a duplicate keeper", p.WindowName)
		}
	}
}

// TestSpawnCrewSession_Collision_ListWindowsError_hku5tgh verifies that when
// ListWindows returns an error, SpawnCrewSession still attempts to add the
// keeper window (fallback: try regardless, log if it fails) and returns no error.
func TestSpawnCrewSession_Collision_ListWindowsError_hku5tgh(t *testing.T) {
	const testHash core.ProjectHash = "deadbeef1234"
	adapter := &crewCollisionAdapter{
		listWindowsErr: fmt.Errorf("tmux: session gone"),
	}
	sub := &tmuxSubstrate{adapter: adapter, sessionName: "daemon-default", projectHash: testHash}

	spawn := handler.SubstrateSpawn{
		Cwd:  "/home/op/proj",
		Env:  []string{"HARMONIK_PROJECT=/home/op/proj"},
		Argv: []string{"claude", "--remote-control", "paul"},
	}

	sess, err := sub.SpawnCrewSession(context.Background(), "paul", spawn)
	if err != nil {
		t.Fatalf("SpawnCrewSession: unexpected error on ListWindows failure: %v", err)
	}
	if sess == nil {
		t.Fatal("SpawnCrewSession: returned nil session on ListWindows failure")
	}

	// spawnCrewKeeperWindow must have been attempted (best-effort fallback).
	var keeperAttempted bool
	for _, p := range adapter.newWindowCalls {
		if p.WindowName == tmux.WindowKeeper {
			keeperAttempted = true
		}
	}
	if !keeperAttempted {
		t.Error("NewWindowIn not called with keeper window: expected fallback attempt when ListWindows fails")
	}
}
