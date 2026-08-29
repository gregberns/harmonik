package daemon_test

import (
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	hclifecycle "github.com/gregberns/harmonik/internal/handlercontract/lifecycle"
	"github.com/gregberns/harmonik/internal/runregistry"
)

type dotFixtureMachineWatchingHookStore struct {
	dotFixtureHookStore

	reg        *runregistry.RunRegistry
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

	reg := runregistry.NewRunRegistry()
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
