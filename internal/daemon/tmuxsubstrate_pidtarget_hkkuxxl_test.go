package daemon_test

// tmuxsubstrate_pidtarget_hkkuxxl_test.go — regression for the concurrent-wave
// implementer-phase-barrier bug (hk-kuxxl).
//
// Bug: under `harmonik run --wave --max-concurrent N>1` (review-loop mode), when
// the FIRST bead in the group commits/completes, the daemon prematurely ends
// EVERY sibling's implementer phase at the same instant — slower beads fail
// `no_commit_during_implementer ... iteration 1 exit=0` mid-edit.
//
// Root cause: tmux window names contain a slash ("<bead_id>/i<n>", WM-002a), so
// the WindowHandle is "session:<bead_id>/i<n>". tmuxSubstrate.SpawnWindow captured
// each session's pane PID via WindowPanePID(handle) using that slash-bearing
// handle. `tmux display-message -t session:bead/i1 '#{pane_pid}'` MISPARSES the
// target and SILENTLY FALLS BACK to the session's currently-active pane. Under
// concurrent SpawnWindow calls, every session therefore captures the SAME
// (most-recently-spawned / active) pane PID into s.pid. When the fast sibling's
// pane shell exits, the slow siblings' runWait sees their aliased s.pid as dead
// (processDead(s.pid)==true), reports exitCodeClean=0, ends the implementer
// phase, and the no-commit guard fails the run.
//
// Fix (hk-kuxxl): SpawnWindow resolves the slash-free pane ID FIRST and uses it
// ("%NNNN") as the target for WindowPanePID, so each session pins PID resolution
// to its OWN pane. runWait's secondary pane-presence check uses the same
// slash-free target.
//
// This test models real-tmux's misparse: WindowPanePID returns the active-pane
// PID when queried by the slash-bearing handle, and the correct per-pane PID
// when queried by the slash-free "%N" pane ID. Pre-fix, both concurrently
// spawned sessions capture the active-pane PID (aliased). Post-fix, each session
// captures its own pane's PID.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// pidTargetFixtureAdapter models the tmux slash-misparse fallback (hk-kuxxl).
//
// NewWindowIn assigns a fresh slash-free pane ID ("%1", "%2", ...) and a
// per-pane PID (1001, 1002, ...). It records the most-recently-spawned pane ID
// as the session's "active" pane.
//
// WindowPanePID models tmux's targeting semantics:
//   - slash-free pane ID ("%N")     → the correct PID for that exact pane.
//   - slash-bearing handle          → tmux misparse: returns the ACTIVE pane's
//     ("session:name/i1")             PID (whatever window was spawned last).
type pidTargetFixtureAdapter struct {
	mu sync.Mutex

	paneCounter int
	// pidByPane maps "%N" → that pane's PID.
	pidByPane map[string]int
	// activePanePID is the PID of the most-recently-spawned pane (tmux's
	// fall-back target when a slash-bearing handle cannot be resolved).
	activePanePID int

	// panePIDTargets records every target string passed to WindowPanePID, in
	// call order, so the test can assert the fix routes by the slash-free pane
	// ID rather than the slash-bearing handle.
	panePIDTargets []string
}

func newPIDTargetFixtureAdapter() *pidTargetFixtureAdapter {
	return &pidTargetFixtureAdapter{pidByPane: map[string]int{}}
}

func (a *pidTargetFixtureAdapter) ProbeTmux(context.Context) error { return nil }
func (a *pidTargetFixtureAdapter) ListSessions(context.Context) ([]string, error) {
	return nil, nil
}
func (a *pidTargetFixtureAdapter) ListWindows(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *pidTargetFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.paneCounter++
	paneID := fmt.Sprintf("%%%d", a.paneCounter)
	pid := 1000 + a.paneCounter
	a.pidByPane[paneID] = pid
	a.activePanePID = pid // most-recently-spawned pane becomes the active pane
	handle := tmux.WindowHandle(params.Session + ":" + params.WindowName)
	return tmux.Outcome{Handle: handle, PaneID: paneID}
}

func (a *pidTargetFixtureAdapter) KillWindow(context.Context, tmux.WindowHandle) error { return nil }

func (a *pidTargetFixtureAdapter) WindowPanePID(_ context.Context, handle tmux.WindowHandle) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	target := string(handle)
	a.panePIDTargets = append(a.panePIDTargets, target)

	// Slash-free pane ID ("%N"): resolve the exact pane.
	if strings.HasPrefix(target, "%") {
		if pid, ok := a.pidByPane[target]; ok {
			return pid, nil
		}
		return 0, tmux.ErrNoSession
	}
	// Slash-bearing handle ("session:name/i1"): tmux misparses and falls back
	// to the session's active pane (hk-kuxxl root cause).
	return a.activePanePID, nil
}

func (a *pidTargetFixtureAdapter) WindowPaneID(context.Context, tmux.WindowHandle) (string, error) {
	// Not used: NewWindowIn populates Outcome.PaneID directly.
	return "", nil
}

func (a *pidTargetFixtureAdapter) KillSession(context.Context, string) error { return nil }
func (a *pidTargetFixtureAdapter) LoadBuffer(context.Context, string, []byte) error {
	return nil
}
func (a *pidTargetFixtureAdapter) PasteBuffer(context.Context, string, string) error { return nil }
func (a *pidTargetFixtureAdapter) SendKeysLiteral(context.Context, string, string) error {
	return nil
}
func (a *pidTargetFixtureAdapter) SendKeysEnter(context.Context, string) error { return nil }
func (a *pidTargetFixtureAdapter) SendKeysQuit(context.Context, string) error  { return nil }
func (a *pidTargetFixtureAdapter) WriteToPane(context.Context, string, string, []byte) error {
	return nil
}

func (a *pidTargetFixtureAdapter) targetsCopy() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.panePIDTargets))
	copy(out, a.panePIDTargets)
	return out
}

// TestConcurrentSpawn_PIDTargetsOwnPane_HKKUXXL spawns two windows concurrently
// (modelling --wave --max-concurrent 2) and asserts each session captured ITS
// OWN pane PID, not the shared active-pane PID.
//
// Pre-fix: SpawnWindow queries WindowPanePID(slash-bearing handle) → both
// sessions capture activePanePID (whichever window spawned last) → the two PIDs
// are identical and aliased. This causes the wave barrier in production.
//
// Post-fix: SpawnWindow resolves the slash-free pane ID first and queries
// WindowPanePID("%N") → each session captures its own pane's PID.
func TestConcurrentSpawn_PIDTargetsOwnPane_HKKUXXL(t *testing.T) {
	adapter := newPIDTargetFixtureAdapter()
	substrate := daemon.NewTmuxSubstrate(adapter, "test-session")

	// Window names mirror the production form (WM-002a): "<bead_id>/i<n>",
	// which carries a slash and triggers the tmux misparse fallback.
	spawn := func(beadID string) handler.SubstrateSession {
		sess, err := substrate.SpawnWindow(context.Background(), handler.SubstrateSpawn{
			WindowName: beadID + "/i1",
			Argv:       []string{"/bin/true"},
		})
		if err != nil {
			t.Errorf("SpawnWindow(%s): %v", beadID, err)
			return nil
		}
		return sess
	}

	// Spawn two windows concurrently to model max-concurrent=2.
	var wg sync.WaitGroup
	sessions := make([]handler.SubstrateSession, 2)
	beadIDs := []string{"hk-fast", "hk-slow"}
	wg.Add(2)
	for i := range beadIDs {
		go func(idx int) {
			defer wg.Done()
			sessions[idx] = spawn(beadIDs[idx])
		}(i)
	}
	wg.Wait()

	for i, sess := range sessions {
		if sess == nil {
			t.Fatalf("session %d (%s) was not spawned", i, beadIDs[i])
		}
	}

	pid0 := sessions[0].PID()
	pid1 := sessions[1].PID()

	// Invariant 1: each session captured a DISTINCT pane PID. Pre-fix both
	// capture the active-pane PID and this fails (pid0 == pid1).
	if pid0 == pid1 {
		t.Fatalf("hk-kuxxl regression: concurrent sessions captured the SAME pane PID %d "+
			"(aliased active-pane PID) — a fast sibling's exit would prematurely end the "+
			"slow sibling's implementer phase. Each session must capture its own pane PID.",
			pid0)
	}

	// Invariant 2: every WindowPanePID call routed through the slash-free pane
	// ID ("%N"), never the slash-bearing "session:.../i1" handle. This is the
	// durable guard against the misparse fallback re-appearing.
	for _, target := range adapter.targetsCopy() {
		if strings.Contains(target, "/") || strings.Contains(target, ":") {
			t.Fatalf("hk-kuxxl regression: WindowPanePID was called with the slash-bearing "+
				"handle %q — tmux misparses this and falls back to the session active pane. "+
				"PID resolution must use the slash-free pane ID (\"%%NNNN\").", target)
		}
	}

	// Invariant 3: the captured PIDs are the per-pane PIDs assigned by
	// NewWindowIn (1001/1002), proving correct routing.
	gotPIDs := map[int]bool{pid0: true, pid1: true}
	for _, want := range []int{1001, 1002} {
		if !gotPIDs[want] {
			t.Errorf("expected one session to capture per-pane PID %d; got pid0=%d pid1=%d",
				want, pid0, pid1)
		}
	}
}
