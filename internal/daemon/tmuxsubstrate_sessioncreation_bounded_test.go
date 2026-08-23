package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

type boundedCreationAdapter struct {
	// release gates NewSessionIn. The timeout tests never close it (a tmux server
	// that is dead). The adopt test closes it once the bound has fired, to stand in
	// for a server that was slow rather than dead and created the session late.
	release chan struct{}

	mu             sync.Mutex
	killedSessions []string
}

func newBoundedCreationAdapter() *boundedCreationAdapter {
	return &boundedCreationAdapter{release: make(chan struct{})}
}

// NewSessionIn satisfies the daemon-local sessionCreator interface. It blocks on
// release WITHOUT selecting on ctx, then reports success — the "tmux was slow,
// not dead, and the session now exists" case.
func (a *boundedCreationAdapter) NewSessionIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	<-a.release
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}

func (a *boundedCreationAdapter) KillSession(_ context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killedSessions = append(a.killedSessions, name)
	return nil
}

func (a *boundedCreationAdapter) killedSession(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, k := range a.killedSessions {
		if k == name {
			return true
		}
	}
	return false
}

func (a *boundedCreationAdapter) ProbeTmux(context.Context) error                { return nil }
func (a *boundedCreationAdapter) ListSessions(context.Context) ([]string, error) { return nil, nil }
func (a *boundedCreationAdapter) ListWindows(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *boundedCreationAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}
func (a *boundedCreationAdapter) KillWindow(context.Context, tmux.WindowHandle) error { return nil }
func (a *boundedCreationAdapter) WindowPanePID(context.Context, tmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *boundedCreationAdapter) WindowPaneID(context.Context, tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *boundedCreationAdapter) LoadBuffer(context.Context, string, []byte) error      { return nil }
func (a *boundedCreationAdapter) PasteBuffer(context.Context, string, string) error     { return nil }
func (a *boundedCreationAdapter) SendKeysEnter(context.Context, string) error           { return nil }
func (a *boundedCreationAdapter) SendKeysQuit(context.Context, string) error            { return nil }
func (a *boundedCreationAdapter) SendKeysLiteral(context.Context, string, string) error { return nil }
func (a *boundedCreationAdapter) WriteToPane(context.Context, string, string, []byte) error {
	return nil
}

var _ tmux.Adapter = (*boundedCreationAdapter)(nil)

const boundedCreationBound = 150 * time.Millisecond

const boundedCreationRunID = "0f0e0d0c-0b0a-0908-0706-050403020100"

const boundedCreationSlack = 5 * time.Second

func boundedCreationSubstrate(t *testing.T, adapter tmux.Adapter) *tmuxSubstrate {
	t.Helper()
	sub, ok := NewTmuxSubstrate(adapter, "bounded-creation-session",
		WithCrewProjectHash(core.ProjectHash("abcdef012345")),
		WithNewWindowTimeout(boundedCreationBound)).(*tmuxSubstrate)
	if !ok {
		t.Fatal("NewTmuxSubstrate did not return *tmuxSubstrate")
	}
	return sub
}

type boundedCreationResult struct {
	sess handler.SubstrateSession
	err  error
}

func boundedCreationRun(t *testing.T, spawn func() (handler.SubstrateSession, error)) boundedCreationResult {
	t.Helper()

	done := make(chan boundedCreationResult, 1)
	go func() {
		sess, err := spawn()
		done <- boundedCreationResult{sess: sess, err: err}
	}()

	select {
	case got := <-done:
		return got
	case <-time.After(boundedCreationBound + boundedCreationSlack):
		t.Fatalf("session creation did not return within %v: the constructor is not externally bounded — "+
			"a tmux server that never answers holds it forever",
			boundedCreationBound+boundedCreationSlack)
		return boundedCreationResult{}
	}
}

func boundedCreationAssertTimeout(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("session creation returned no error; want the creation-bound timeout")
	}
	if !errors.Is(err, ErrTmuxNewSessionTimeout) {
		t.Errorf("error %v does not wrap ErrTmuxNewSessionTimeout", err)
	}
	if !errors.Is(err, handler.ErrStructural) {
		t.Errorf("error %v does not wrap handler.ErrStructural; the daemon's structural-error "+
			"handling would not fire", err)
	}
}

// TestSpawnRunSessionFailsStructurallyWhenTmuxSessionCreationHangs defends the
// claim that a bead run's independent tmux session is created under an external
// bound: a tmux server that never answers costs the run one bound, not the run.
func TestSpawnRunSessionFailsStructurallyWhenTmuxSessionCreationHangs(t *testing.T) {
	t.Parallel()

	adapter := newBoundedCreationAdapter()
	t.Cleanup(func() { close(adapter.release) })
	sub := boundedCreationSubstrate(t, adapter)

	got := boundedCreationRun(t, func() (handler.SubstrateSession, error) {
		return sub.SpawnRunSession(context.Background(), boundedCreationRunID,
			handler.SubstrateSpawn{Argv: []string{"claude"}})
	})

	boundedCreationAssertTimeout(t, got.err)
	if got.sess != nil {
		t.Error("SpawnRunSession returned a session alongside the timeout error")
	}
}

// TestSpawnCrewSessionFailsStructurallyWhenTmuxSessionCreationHangs defends the
// same claim for the crew constructor: an operator's crew start answers within
// the bound instead of waiting on a wedged tmux server forever.
func TestSpawnCrewSessionFailsStructurallyWhenTmuxSessionCreationHangs(t *testing.T) {
	t.Parallel()

	adapter := newBoundedCreationAdapter()
	t.Cleanup(func() { close(adapter.release) })
	sub := boundedCreationSubstrate(t, adapter)

	got := boundedCreationRun(t, func() (handler.SubstrateSession, error) {
		return sub.SpawnCrewSession(context.Background(), "alpha", handler.SubstrateSpawn{
			Argv: []string{"claude"},
		})
	})

	boundedCreationAssertTimeout(t, got.err)
	if got.sess != nil {
		t.Error("SpawnCrewSession returned a session alongside the timeout error")
	}
}

// TestAbandonedSessionCreationNamesTheSessionInItsError defends the claim that a
// bounded-out creation hands the operator evidence. The substrate cannot know
// whether the tmux server went on to create the session, so the one thing it owes
// anyone reading the failure is the name to go and look for.
func TestAbandonedSessionCreationNamesTheSessionInItsError(t *testing.T) {
	t.Parallel()

	adapter := newBoundedCreationAdapter()
	t.Cleanup(func() { close(adapter.release) })
	sub := boundedCreationSubstrate(t, adapter)

	sessName, nameErr := sub.runSessionName(boundedCreationRunID)
	if nameErr != nil {
		t.Fatalf("runSessionName: %v", nameErr)
	}

	got := boundedCreationRun(t, func() (handler.SubstrateSession, error) {
		return sub.SpawnRunSession(context.Background(), boundedCreationRunID, handler.SubstrateSpawn{
			Argv: []string{"claude"},
		})
	})
	boundedCreationAssertTimeout(t, got.err)

	if !strings.Contains(got.err.Error(), sessName) {
		t.Errorf("error %q does not name session %q: an operator reading this failure has "+
			"nowhere to look for the session tmux may have gone on to create", got.err, sessName)
	}
}

// TestAbandonedSessionCreationLeavesTheSessionForTheNextAttemptToAdopt defends
// the claim that a bounded-out creation takes NO corrective action on the session
// name.
//
// Killing it looks tidy and is wrong. The substrate cannot tell a session its own
// abandoned call created from one a concurrent attempt legitimately owns, and a
// second attempt on the same name is ordinary rather than exotic: crewSessionName
// is a pure function of the project hash and the crew name, a scheduled crew
// start that fails re-fires on that name at its next boundary (doFireAction
// records the failed fire since hk-pbdti, so the retry follows the schedule
// rather than the 2s poll), and the spawn-crew overlap check blocks a second
// attempt only while the crew is presence-online. A killer armed by the first
// attempt reaps whichever later attempt finally succeeded. SpawnCrewSession's
// ErrWindowCollision branch already adopts an existing session under that name,
// so leaving it alone is both safer and sufficient.
func TestAbandonedSessionCreationLeavesTheSessionForTheNextAttemptToAdopt(t *testing.T) {
	t.Parallel()

	adapter := newBoundedCreationAdapter()
	sub := boundedCreationSubstrate(t, adapter)

	sessName, nameErr := sub.crewSessionName("alpha")
	if nameErr != nil {
		t.Fatalf("crewSessionName: %v", nameErr)
	}

	got := boundedCreationRun(t, func() (handler.SubstrateSession, error) {
		return sub.SpawnCrewSession(context.Background(), "alpha", handler.SubstrateSpawn{
			Argv: []string{"claude"},
		})
	})
	boundedCreationAssertTimeout(t, got.err)

	close(adapter.release)

	time.Sleep(boundedCreationBound * 2)
	if adapter.killedSession(sessName) {
		t.Errorf("crew session %q was killed after the creation bound fired: the next attempt on "+
			"this exact name — the schedule's next boundary, or an operator — adopts it, so this "+
			"kills whichever attempt finally succeeded", sessName)
	}
}
