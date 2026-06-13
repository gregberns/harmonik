package daemon

// crewstart_hkmmlqt_test.go — regression tests for hk-mmlqt.
//
// Bug: crews were spawned as tmux WINDOWS inside the daemon's own session, so
// daemon SIGTERM / supervisor-revive tore down every crew window.
//
// Fix: crews are now spawned via crewSessionSpawner.SpawnCrewSession which
// creates an independent tmux session, decoupled from the daemon's lifecycle.
// The crew-stop path uses crewSessionStopper.StopCrewSession to kill the whole
// independent session rather than just the window.
//
// These tests verify:
//   - SpawnCrewSession is called (not SpawnWindow) when substrate implements crewSessionSpawner.
//   - The independent session name follows the project-qualified form (hk-ohd fleet-portability T2).
//   - The crew registry handle is recorded from the independent session.
//   - StopCrewSession is called (not StopWindowByHandle) when substrate implements crewSessionStopper.
//   - The fallback SpawnWindow path remains intact for substrates that don't implement crewSessionSpawner.
//
// Bead ref: hk-mmlqt.

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test doubles
// ─────────────────────────────────────────────────────────────────────────────

// hkmmlqtCrewSessionSubstrate implements handler.Substrate, crewSessionSpawner,
// and crewSessionStopper to exercise the independent-session path.
type hkmmlqtCrewSessionSubstrate struct {
	// SpawnCrewSession records
	spawnSessionCalled bool
	spawnSessionName   string // crewName passed to SpawnCrewSession
	spawnWindowCalled  bool   // true if SpawnWindow was called instead (fallback)
	spawnErr           error

	// StopCrewSession records
	stopSessionCalled bool
	stopSessionName   string
	stopWindowCalled  bool // true if StopWindowByHandle was called instead
}

// SpawnWindow satisfies handler.Substrate. Should NOT be called on the
// independent-session path.
func (f *hkmmlqtCrewSessionSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	f.spawnWindowCalled = true
	return &fakeSession{handle: "fallback-window"}, nil
}

// SpawnCrewSession implements crewSessionSpawner.
func (f *hkmmlqtCrewSessionSubstrate) SpawnCrewSession(_ context.Context, crewName string, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	f.spawnSessionCalled = true
	f.spawnSessionName = crewName
	if f.spawnErr != nil {
		return nil, f.spawnErr
	}
	// Fake uses the legacy fallback session name to keep the test simple.
	sessName := "hk-crew-" + crewName
	return &fakeSession{handle: sessName + ":hk-crew-" + crewName}, nil
}

// StopWindowByHandle satisfies crewPaneStopper. Should NOT be called when
// crewSessionStopper is available.
func (f *hkmmlqtCrewSessionSubstrate) StopWindowByHandle(_ context.Context, _ string) error {
	f.stopWindowCalled = true
	return nil
}

// StopCrewSession implements crewSessionStopper.
func (f *hkmmlqtCrewSessionSubstrate) StopCrewSession(_ context.Context, crewName string, _ string) error {
	f.stopSessionCalled = true
	f.stopSessionName = crewName
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestCrewStart_IndependentSession_hkmmlqt verifies that when the substrate
// implements crewSessionSpawner, SpawnCrewSession (not SpawnWindow) is called
// and the registry handle encodes the independent session name.
func TestCrewStart_IndependentSession_hkmmlqt(t *testing.T) {
	sub := &hkmmlqtCrewSessionSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)

	result := mustCrewStart(t, h, CrewStartRequest{
		Name:  "chani",
		Queue: "crew-chani",
	})

	if result.SessionID == "" {
		t.Error("crew-start: expected non-empty session_id")
	}

	// crewSessionSpawner.SpawnCrewSession must be called, not SpawnWindow.
	if !sub.spawnSessionCalled {
		t.Error("SpawnCrewSession was not called; substrate's crewSessionSpawner was not used")
	}
	if sub.spawnWindowCalled {
		t.Error("SpawnWindow was called; should use SpawnCrewSession on the independent-session path")
	}
	if sub.spawnSessionName != "chani" {
		t.Errorf("SpawnCrewSession crewName = %q, want %q", sub.spawnSessionName, "chani")
	}

	// The recorded handle must carry the independent session name prefix.
	// The fake substrate uses "hk-crew-<name>" as session name.
	rec, loadErr := crew.Load(dir, "chani")
	if loadErr != nil {
		t.Fatalf("crew.Load: %v", loadErr)
	}
	wantPrefix := "hk-crew-chani:"
	if !strings.HasPrefix(rec.Handle, wantPrefix) {
		t.Errorf("registry handle = %q, want prefix %q (independent session)", rec.Handle, wantPrefix)
	}
}

// TestCrewStop_IndependentSession_hkmmlqt verifies that when the substrate
// implements crewSessionStopper, StopCrewSession (not StopWindowByHandle) is
// called on crew-stop.
func TestCrewStop_IndependentSession_hkmmlqt(t *testing.T) {
	sub := &hkmmlqtCrewSessionSubstrate{}
	h, _ := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "stilgar", Queue: "crew-stilgar"})
	mustCrewStop(t, h, CrewStopRequest{Name: "stilgar"})

	if !sub.stopSessionCalled {
		t.Error("StopCrewSession was not called; substrate's crewSessionStopper was not used")
	}
	if sub.stopWindowCalled {
		t.Error("StopWindowByHandle was called; should use StopCrewSession on the independent-session path")
	}
	if sub.stopSessionName != "stilgar" {
		t.Errorf("StopCrewSession crewName = %q, want %q", sub.stopSessionName, "stilgar")
	}
}

// TestCrewStart_FallbackToSpawnWindow_hkmmlqt verifies that substrates that do
// NOT implement crewSessionSpawner fall back to SpawnWindow (existing behaviour).
func TestCrewStart_FallbackToSpawnWindow_hkmmlqt(t *testing.T) {
	// fakeSubstrate does not implement crewSessionSpawner.
	sub := &fakeSubstrate{}
	h, _ := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "duncan", Queue: "crew-duncan"})

	if !sub.spawnCalled {
		t.Error("SpawnWindow was not called; expected fallback to SpawnWindow for non-crewSessionSpawner substrate")
	}
}

// TestCrewStop_FallbackToStopWindowByHandle_hkmmlqt verifies that substrates
// that do NOT implement crewSessionStopper fall back to StopWindowByHandle.
func TestCrewStop_FallbackToStopWindowByHandle_hkmmlqt(t *testing.T) {
	sub := &fakeSubstrate{}
	h, _ := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "liet", Queue: "crew-liet"})
	mustCrewStop(t, h, CrewStopRequest{Name: "liet"})

	if !sub.stopCalled {
		t.Error("StopWindowByHandle was not called; expected fallback for non-crewSessionStopper substrate")
	}
}

// TestCrewSessionName_hkmmlqt verifies the deterministic session name formula.
// Fleet-portability T2 (hk-ohd): project-qualified form "harmonik-<hash>-crew-<name>"
// when projectHash is set; legacy "hk-crew-<name>" fallback when projectHash is zero.
func TestCrewSessionName_hkmmlqt(t *testing.T) {
	const testHash core.ProjectHash = "abc123def456"

	cases := []struct {
		name        string
		projectHash core.ProjectHash
		want        string
	}{
		// Legacy fallback: no project hash.
		{"alpha", "", "hk-crew-alpha"},
		{"chani-1", "", "hk-crew-chani-1"},
		// Project-qualified (T2): with project hash.
		{"alpha", testHash, lifecycle.TmuxSessionName(testHash, "crew-alpha")},
		{"chani-1", testHash, lifecycle.TmuxSessionName(testHash, "crew-chani-1")},
	}
	for _, tc := range cases {
		sub := &tmuxSubstrate{projectHash: tc.projectHash}
		if got := sub.crewSessionName(tc.name); got != tc.want {
			t.Errorf("crewSessionName(%q, projectHash=%q) = %q, want %q", tc.name, tc.projectHash, got, tc.want)
		}
	}
}
