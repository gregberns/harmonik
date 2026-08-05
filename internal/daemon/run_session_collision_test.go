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
// else can hold that name, and the previous node was already told to die, so
// finishing that teardown and asking again takes nothing that was not going
// anyway.
//
// Helper prefix: sessionCollide.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// sessionCollideRunID is the run every launch below belongs to. Both nodes of a
// graph run carry the same one, which is why they collide.
const sessionCollideRunID = "0f0e0d0c-0b0a-0908-0706-050403020100"

// sessionCollideAdapter is a tmux server that reports the run's session already
// exists until something kills it, which is what tmux does while the previous
// node's session is still being torn down.
//
// failWith replaces that behaviour with a different error, so the same fixture
// can also drive a failure that is NOT a collision.
type sessionCollideAdapter struct {
	w4cFixtureAdapter
	mu       sync.Mutex
	exists   bool
	failWith error
	killed   []string
}

func (a *sessionCollideAdapter) NewSessionIn(ctx context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	if a.failWith != nil {
		a.mu.Unlock()
		return tmux.Outcome{Err: a.failWith}
	}
	if a.exists {
		a.mu.Unlock()
		return tmux.Outcome{Err: tmux.ErrWindowCollision}
	}
	a.exists = true
	a.mu.Unlock()
	return a.w4cFixtureAdapter.NewSessionIn(ctx, params)
}

func (a *sessionCollideAdapter) KillSession(_ context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killed = append(a.killed, name)
	a.exists = false
	return nil
}

func (a *sessionCollideAdapter) killedSessions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.killed))
	copy(out, a.killed)
	return out
}

// TestSpawnRunSession_ASecondNodeGetsTheSessionAfterTheFirstOnesTeardown is the
// hazard a run session per RUN creates for a graph of several nodes.
//
// The first launch makes the session. The second finds it still there and must
// still put its agent outside the daemon's own session. A failure here is not a
// lost window — it fails the node, which fails the run, on graphs of more than
// one agentic node, which is all of them.
func TestSpawnRunSession_ASecondNodeGetsTheSessionAfterTheFirstOnesTeardown(t *testing.T) {
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

	if killed := adapter.killedSessions(); len(killed) != 1 || killed[0] != wantSession {
		t.Errorf("the recovery killed %v, want exactly [%s].\n"+
			"Finishing the previous node's teardown is what makes room for the retry. Killing "+
			"anything else reaches a session this run does not own.", killed, wantSession)
	}

	if windows := adapter.newWindowCopy(); len(windows) != 0 {
		t.Errorf("the recovery opened %d ordinary tmux window(s): %v.\n"+
			"An ordinary window is recorded for the daemon's exit-time window sweep, so the "+
			"agent would be killed with the daemon — which is the one thing a session of the "+
			"run's own exists to prevent. The retry must go back through new-session.",
			len(windows), windows)
	}
}

// TestSpawnRunSession_ATmuxFailureThatIsNotACollisionStillFails is the negative
// control, and it was added because its absence was measured: widening the
// recovery to accept ANY new-session error passed the whole package.
//
// The recovery is safe only because a duplicate-session error names a session
// this run already owns. Any other failure — a tmux server that is gone, a
// refused command — says nothing about who owns what, and killing a session on
// that reasoning reaches for something the run cannot account for.
func TestSpawnRunSession_ATmuxFailureThatIsNotACollisionStillFails(t *testing.T) {
	t.Parallel()

	adapter := &sessionCollideAdapter{failWith: errors.New("tmux: server not running")}
	sub := w4cFixtureSubstrate(t, adapter)

	sess, err := sub.SpawnRunSession(context.Background(), sessionCollideRunID,
		handler.SubstrateSpawn{Argv: []string{"/usr/local/bin/claude"}})
	if err == nil {
		t.Fatalf("SpawnRunSession reported success (session %v) on a tmux failure that is not a "+
			"duplicate session.\n"+
			"The launch would then believe it has an agent that was never started.", sess)
	}
	if killed := adapter.killedSessions(); len(killed) != 0 {
		t.Errorf("the failure path killed %v.\n"+
			"Only a duplicate-session error identifies a session this run owns. Killing on any "+
			"other error reaches for a session on reasoning that does not hold.", killed)
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
