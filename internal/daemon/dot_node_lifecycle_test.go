package daemon_test

// dot_node_lifecycle_test.go — a graph node's session must join the run's
// lifecycle machinery and must reach a terminal state.
//
// Two consumers depend on it and both were inert on the graph path, which is the
// production default:
//
//   - the stale watcher drives Ready → Failed(silent_hang) through the machine
//     it reads off the RunHandle, and a graph run never put one there; the
//     dashboard's lifecycle read has the same source; and
//   - the HC-065 terminal transition, which emits lifecycle_transition, ran only
//     in the single-mode tail.
//
// Bead: hk-b4xf2.

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
)

// dotFixtureMachineWatchingHookStore samples the in-flight RunHandle's lifecycle
// machine at the moment the launch registers its agent-ready callback.
//
// That moment is the observation point because the launch calls the site's
// post-launch hook and then this method, in that order and on the same
// goroutine. So "was a machine on the handle by now" is a question about the
// hook, asked from production code rather than from a source-level sensor.
type dotFixtureMachineWatchingHookStore struct {
	dotFixtureHookStore

	reg        *daemon.RunRegistry
	sawMachine atomic.Bool
	sawHandle  atomic.Bool
}

func (s *dotFixtureMachineWatchingHookStore) SetAgentReadyCallback(runID, sessID string, cb func()) {
	for _, h := range s.reg.Snapshot() {
		s.sawHandle.Store(true)
		if h.GetMachine() != nil {
			s.sawMachine.Store(true)
		}
	}
	s.dotFixtureHookStore.SetAgentReadyCallback(runID, sessID, cb)
}

// TestDotNode_SessionMachineReachesTheRunHandle is the claim behind the stale
// watcher's silent-hang drive: while a graph node's agent is live, the run's
// handle carries that session's lifecycle machine.
func TestDotNode_SessionMachineReachesTheRunHandle(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	store := &dotFixtureMachineWatchingHookStore{reg: reg}

	const beadID = core.BeadID("hk-b4xf2-machine-on-handle")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{
		HookStore:   store,
		RunRegistry: reg,
	})

	if !store.sawHandle.Load() {
		t.Fatalf("the fixture never saw an in-flight run handle, so this test asserts nothing; events=%v", res.Bus.eventTypes())
	}
	if !store.sawMachine.Load() {
		t.Error("the run handle carried no lifecycle machine while the node's agent was live.\n" +
			"The stale watcher reads the machine off the handle to drive Ready → Failed(silent_hang), and stategather reads it for the dashboard; both are inert without it.")
	}
}

// TestDotNode_SessionReachesATerminalLifecycleState is the HC-065 claim: the
// node's session ends in a terminal lifecycle state and says so on the bus.
func TestDotNode_SessionReachesATerminalLifecycleState(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-b4xf2-terminal-transition")
	res := runDotFixtureBead(t, beadID, dotFixtureOpts{})

	states := dotFixtureLifecycleStates(t, res)
	if len(states) == 0 {
		t.Fatalf("no lifecycle_transition event for a completed graph node; events=%v", res.Bus.eventTypes())
	}
	if !dotFixtureContains(states, hclifecycle.StateTerminated.String()) && !dotFixtureContains(states, hclifecycle.StateFailed.String()) {
		t.Errorf("lifecycle states reached = %v; want the session to reach a terminal state (HC-065)", states)
	}
}

// dotFixtureLifecycleStates returns the to_state of every lifecycle_transition
// event the run emitted, in order.
func dotFixtureLifecycleStates(t *testing.T, res dotFixtureResult) []string {
	t.Helper()
	all := res.Bus.allEvents()
	out := make([]string, 0, len(all))
	for _, ev := range all {
		if ev.EventType != string(core.EventTypeLifecycleTransition) {
			continue
		}
		var pl struct {
			ToState string `json:"to_state"`
		}
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			t.Fatalf("lifecycle_transition payload: %v", err)
		}
		out = append(out, pl.ToState)
	}
	return out
}

func dotFixtureContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
