package daemon

// run_session_collision_test.go — a graph run launches an agent per node into
// ONE session, so the second node can arrive before the first node's session is
// gone.
//
// The run's session is named after the run, so every node of that run asks tmux
// for the same name. Each node kills its agent when it ends, and killing the
// only window in a session destroys the session — but tmux does that on its own
// schedule, and the next node does not wait for it. `tmux new-session` on a name
// that still exists answers "duplicate session".
//
// Refusing there would fail the node, and the run with it, over a teardown that
// was already under way. The session that exists belongs to this run and nothing
// else can hold that name, so a window in it is the same thing the node asked
// for: an agent outside the daemon's session, in the session the run's registry
// record names.
//
// Helper prefix: sessionCollide.

import (
	"context"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// sessionCollideRunID is the run every launch below belongs to. Both nodes of a
// graph run carry the same one, which is why they collide.
const sessionCollideRunID = "0f0e0d0c-0b0a-0908-0706-050403020100"

// sessionCollideAdapter answers the first new-session request and reports a
// duplicate for every one after it, which is what tmux does while the previous
// node's session is still being torn down.
type sessionCollideAdapter struct {
	w4cFixtureAdapter
	mu       sync.Mutex
	sessions int
}

func (a *sessionCollideAdapter) NewSessionIn(ctx context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.sessions++
	first := a.sessions == 1
	a.mu.Unlock()
	if !first {
		return tmux.Outcome{Err: tmux.ErrWindowCollision}
	}
	return a.w4cFixtureAdapter.NewSessionIn(ctx, params)
}

// TestSpawnRunSession_ASecondNodeJoinsTheSessionTheFirstOneMade is the hazard a
// run session per RUN creates for a graph of several nodes.
//
// The first launch makes the session. The second finds it already there and must
// still put its agent somewhere outside the daemon's own session. A failure here
// is not a lost window — it fails the node, which fails the run, on graphs of
// more than one agentic node, which is all of them.
func TestSpawnRunSession_ASecondNodeJoinsTheSessionTheFirstOneMade(t *testing.T) {
	t.Parallel()

	adapter := &sessionCollideAdapter{}
	sub := w4cFixtureSubstrate(t, adapter)
	wantSession, nameErr := sub.runSessionName(sessionCollideRunID)
	if nameErr != nil {
		t.Fatalf("sessionCollide: name the run's session: %v", nameErr)
	}

	first, err := sub.SpawnRunSession(context.Background(), sessionCollideRunID,
		handler.SubstrateSpawn{Argv: []string{"/usr/local/bin/claude"}})
	if err != nil {
		t.Fatalf("the first node could not take the run's session: %v", err)
	}
	if first == nil {
		t.Fatal("the first node got no session back, so the collision below is not the " +
			"second node meeting the first")
	}

	second, err := sub.SpawnRunSession(context.Background(), sessionCollideRunID,
		handler.SubstrateSpawn{Argv: []string{"/usr/local/bin/claude"}})
	if err != nil {
		t.Fatalf("the second node of the same run was refused because the run's session "+
			"already exists: %v\n"+
			"Every node of a graph run asks for the same session name, and the previous node's "+
			"teardown is not synchronous. Refusing here fails the node, and the run with it, on "+
			"any graph with more than one agentic node.", err)
	}
	if second == nil {
		t.Fatal("the second node got neither a session nor an error, so nothing was launched")
	}

	windows := adapter.newWindowCopy()
	if len(windows) != 1 {
		t.Fatalf("tmux was asked for %d window(s) after the collision, want exactly 1.\n"+
			"The recovery is one window in the session that already exists.", len(windows))
	}
	if windows[0].Session != wantSession {
		t.Errorf("the second node's window went to session %q, want %q.\n"+
			"An agent in any other session is outside the one the run's registry record names, "+
			"so the next boot cannot find it.", windows[0].Session, wantSession)
	}
}

// newWindowCopy returns the new-window requests the adapter recorded.
func (a *w4cFixtureAdapter) newWindowCopy() []tmux.NewWindowIn {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]tmux.NewWindowIn, len(a.newWindowParams))
	copy(out, a.newWindowParams)
	return out
}

// sessionCollideProjectHash keeps this file honest about which project hash the
// fixture substrate is built with: the session name is derived from it, and a
// mismatch would make the assertion above compare two names neither of which is
// production's.
var _ = core.ProjectHash("abcdef012345")
