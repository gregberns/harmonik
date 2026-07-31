package daemon

// survive_shutdown_gate_test.go — the survive-shutdown gate at the launch seam.
//
// One condition decides what a bead run gives back when the daemon stops: the
// agent runs in a tmux session of its own AND the run is ending because the
// daemon is shutting down. specs/run-state-machine.md §4a calls that pair the
// SURVIVE disposition (RSM-037), and internal/runlease.Decide is the pure
// statement of it. In the daemon the same pair is spelled inline at each
// release site as `useIndepSession && ctx.Err() != nil`, and until this file
// existed no test referenced it anywhere.
//
// Two of the release sites are here, in the launch: the abort kill and the
// teardown pair. Each reads its own copy of the condition through the
// SkipAbortKill / SkipTeardown predicates, so a wrong spelling at one site is
// invisible to the other. Every test below drives the REAL predicate shape the
// daemon builds — `indep && ctx.Err() != nil`, read through a live context —
// rather than a constant, and the two half-conditions are covered separately,
// because a conjunction written three ways is exactly where a half-condition
// hides.
//
// # Read this before adding a test that asserts survival
//
// Survive is what a run ASKS for. The system does not deliver it. Two things
// defeat it, and both are pinned as CURRENT BEHAVIOUR rather than as promises:
//
//  1. The completion wait kills the session itself when it finds the run
//     context already cancelled, with no reference to the gate. That is in this
//     file.
//  2. The next boot's orphan sweep kills every tmux session carrying the
//     project prefix, with no liveness test, before the pass that looks for a
//     surviving run. That is in survive_shutdown_recovery_test.go.
//
// A test named "the session survives a daemon restart" would assert a promise
// the system does not keep. Do not write one.
//
// # How a gated kill is told from an ungated one
//
// Every kill the gate covers is issued on context.Background(): the abort kill,
// the force-teardown, and the post-wait window kill all pass a deliberately
// non-cancellable context so the pane still dies after the run context is gone.
// The fixture counts kills by the context they arrive on, so a kill on a live
// context is one of those three and a kill on a cancelled one is not.
//
// Read the second half of that as a fact about THESE FIXTURES, not about the
// daemon. Two ungated sites kill on the run context — the completion wait, and
// the ready-timeout kill inside the dispatch segment — and only the first is
// reachable here. The ready-timeout kill is excluded by arithmetic rather than
// by anything structural: the fixtures cancel the run context within
// milliseconds of the launch and the ready deadline is 200ms away, so the abort
// edge always wins. Anyone widening this fixture's timings must re-earn that.
//
// Helper prefix: surviveGate.

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture
// ─────────────────────────────────────────────────────────────────────────────

// surviveGateSession is the agent's tmux session, reduced to the one question
// these tests ask of it: what killed it, and was that thing looking at the gate?
// See the file header for why the context a kill arrives on answers that.
//
// Stdout returns nil, which is what a tmux-hosted session returns. That is
// load-bearing: it makes handler.Launch hand back a nil watcher, which is the
// production shape for an agent in a tmux session and the shape the post-wait
// window kill is guarded for.
type surviveGateSession struct {
	mu             sync.Mutex
	killsLiveCtx   int
	killsCancelled int
}

var _ handler.SubstrateSession = (*surviveGateSession)(nil)

func (s *surviveGateSession) Kill(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx != nil && ctx.Err() != nil {
		s.killsCancelled++
		return nil
	}
	s.killsLiveCtx++
	return nil
}

func (s *surviveGateSession) Wait(context.Context) error { return nil }
func (s *surviveGateSession) Outcome() handler.Outcome   { return handler.Outcome{} }
func (s *surviveGateSession) PID() int                   { return 0 }
func (s *surviveGateSession) Stdout() io.Reader          { return nil }

// gatedKills counts the kills the survive gate is able to prevent.
func (s *surviveGateSession) gatedKills() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killsLiveCtx
}

// killsFromTheCompletionWait counts the kills issued on the already-cancelled
// run context. Two ungated sites kill on the run context, and the file header
// says why only the completion wait is reachable in these fixtures.
func (s *surviveGateSession) killsFromTheCompletionWait() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killsCancelled
}

// surviveGateSubstrate hands out one surviveGateSession and records that the
// spawn happened. The spawn count is the proof that a launch took place at all,
// without which "nothing was killed" would be satisfied by a run that never
// started.
type surviveGateSubstrate struct {
	mu      sync.Mutex
	spawns  int
	session *surviveGateSession
}

var _ handler.Substrate = (*surviveGateSubstrate)(nil)

func (s *surviveGateSubstrate) SpawnWindow(context.Context, handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spawns++
	return s.session, nil
}

func (s *surviveGateSubstrate) spawnCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.spawns
}

// surviveGateHookStore records the hook-session registration and its close. The
// hook session is the agent's channel back to this daemon: an agent that keeps
// its session but loses its hook session is holding a session it can no longer
// report through.
type surviveGateHookStore struct {
	mu         sync.Mutex
	registered int
	closed     int
}

var _ runloop.HookStore = (*surviveGateHookStore)(nil)

func (h *surviveGateHookStore) RegisterHookSession(string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.registered++
}

func (h *surviveGateHookStore) CloseHookSession(string, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed++
}

func (h *surviveGateHookStore) LatestOutcome(string, string) *json.RawMessage { return nil }

func (h *surviveGateHookStore) WaitForOutcome(context.Context, string, string) (json.RawMessage, error) {
	return nil, nil
}

func (h *surviveGateHookStore) SetAgentReadyCallback(string, string, func()) {}

func (h *surviveGateHookStore) counts() (registered, closed int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.registered, h.closed
}

// surviveGateEmitter drops every event. None of them is what these tests read.
type surviveGateEmitter struct{}

var _ handlercontract.EventEmitter = surviveGateEmitter{}

func (surviveGateEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }
func (surviveGateEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

// surviveGateRun is one launch driven to a terminal, plus what the run gave
// back on the way out.
type surviveGateRun struct {
	result    agentLaunchResult
	session   *surviveGateSession
	substrate *surviveGateSubstrate
	hooks     *surviveGateHookStore

	// abortConsults and teardownConsults count how many times the launch ASKED
	// the gate. They are the difference between "the machinery ran and chose not
	// to act" and "nothing happened", which is the whole hazard with a claim
	// whose pass condition is that something did not occur.
	abortConsults    int
	teardownConsults int

	// killsBeforeTeardown is the gated-kill count read the FIRST time the
	// teardown gate was consulted. It splits the gated kills in two, which is
	// what lets each half-condition name one site rather than "something killed
	// it". The launch reaches the abort kill (or, on a run that ends by ready
	// timeout, the ready kill) before it ever asks the teardown gate, and the
	// teardown pair and the post-wait window kill both come after. So a kill
	// counted before this mark came from the abort site, and a kill counted
	// after it came from the teardown site.
	killsBeforeTeardown int
}

func (r *surviveGateRun) consults() (abort, teardown int) {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	return r.abortConsults, r.teardownConsults
}

// killsFromTheAbortSite is the gated kills issued before the teardown gate was
// first asked.
func (r *surviveGateRun) killsFromTheAbortSite() int {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	return r.killsBeforeTeardown
}

// killsFromTheTeardownSite is the gated kills issued after the teardown gate
// was first asked.
func (r *surviveGateRun) killsFromTheTeardownSite() int {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	return r.session.killsLiveCtx - r.killsBeforeTeardown
}

// surviveGateDrive runs one launch under the real gate predicate.
//
// indepSession is the first half of the condition — an agent in a session of
// its own. daemonStops is the second half: when true the run context is
// cancelled the instant the agent is launched, which is what a daemon shutdown
// looks like to a run still waiting for its agent to report ready.
//
// The predicate handed to the launch is the same conjunction beadRunOne builds,
// read through the same live context, so flipping one half here exercises the
// same expression production does.
func surviveGateDrive(t *testing.T, indepSession, daemonStops bool) *surviveGateRun {
	t.Helper()

	sess := &surviveGateSession{}
	sub := &surviveGateSubstrate{session: sess}
	hooks := &surviveGateHookStore{}
	out := &surviveGateRun{session: sess, substrate: sub, hooks: hooks}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The counters share the session's mutex so a gate consulted from the
	// dispatch goroutine and a read from the test goroutine cannot race.
	abortGate := func() bool {
		sess.mu.Lock()
		out.abortConsults++
		sess.mu.Unlock()
		return indepSession && ctx.Err() != nil
	}
	teardownGate := func() bool {
		sess.mu.Lock()
		if out.teardownConsults == 0 {
			out.killsBeforeTeardown = sess.killsLiveCtx
		}
		out.teardownConsults++
		sess.mu.Unlock()
		return indepSession && ctx.Err() != nil
	}

	wt := t.TempDir()
	in := agentLaunchInput{
		Env: runloop.RunEnv{
			ProjectDir: wt,
			// Short enough that the daemon-keeps-running cases settle on the
			// ready-timeout edge in well under a second, long enough that the
			// shutdown cases reach the abort edge first.
			AgentReadyTimeout:       200 * time.Millisecond,
			RemoteAgentReadyTimeout: 200 * time.Millisecond,
		},
		Ports: runloop.RunPorts{
			Emitter: surviveGateEmitter{},
			Clock:   substrate.SystemClock{},
		},
		Handles: runloop.SharedHandles{
			AdapterRegistry: surviveGateRegistry(t),
			HookStore:       hooks,
		},
		RunID:         core.RunID(uuid.New()),
		LogPrefix:     "daemon: survive-shutdown gate test",
		Spec:          handler.LaunchSpec{Binary: "/bin/true", WorkDir: wt},
		Artifacts:     shared.LaunchArtifacts{ResolvedAgentType: core.AgentTypeClaudeCode},
		WorktreePath:  wt,
		BaseSubstrate: sub,
		OnLaunchedExtra: func(context.Context, handler.Session) {
			if daemonStops {
				cancel()
			}
		},
		SkipAbortKill: abortGate,
		SkipTeardown:  teardownGate,
	}

	out.result = runAgentLaunch(ctx, in)
	// Every caller of runAgentLaunch defers Cleanup. Running it is what
	// exercises the teardown half of the gate.
	out.result.Cleanup()

	return out
}

// surviveGateRegistry returns a registry carrying the real claude adapter, so
// the dispatch segment has a readiness detector and holds in its ready wait
// instead of synthesizing an immediate ready.
func surviveGateRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("surviveGate: register claude adapter: %v", err)
	}
	return reg
}

// ─────────────────────────────────────────────────────────────────────────────
// The gate holds
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_AnAgentInItsOwnSessionIsNotKilledByEitherGatedSiteWhenTheDaemonStops
// is the gate's whole purpose. Both facts hold, so neither the abort kill nor
// the teardown pair may end the agent: the session is meant to outlive this
// process and be found by the next boot, and killing it here would strand the
// bead in progress with nothing alive to adopt.
//
// The assertions are deliberately not just "no kill". A kill count of zero is
// free in any fixture where nothing runs, so the test also proves the launch
// happened, the abort edge was actually reached, and BOTH gates were asked.
// That is the difference between machinery that ran and declined, and machinery
// that never ran.
func TestSurviveShutdown_AnAgentInItsOwnSessionIsNotKilledByEitherGatedSiteWhenTheDaemonStops(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, true)

	if got := run.substrate.spawnCount(); got != 1 {
		t.Fatalf("substrate spawns = %d, want 1. Nothing launched, so every assertion below passes for free", got)
	}
	if run.result.Dispatch.Phase != runexec.DispatchAborted {
		t.Fatalf("dispatch phase = %q, want %q. The shutdown edge never fired, so the abort kill was never on the table",
			run.result.Dispatch.Phase, runexec.DispatchAborted)
	}
	abort, teardown := run.consults()
	if abort == 0 {
		t.Error("the abort kill never asked the gate — it took a path that does not consult it, so this test does not defend the abort site")
	}
	if teardown == 0 {
		t.Error("the teardown pair never asked the gate — it took a path that does not consult it, so this test does not defend the teardown site")
	}
	if got := run.session.gatedKills(); got != 0 {
		t.Errorf("gated kills = %d, want 0.\n"+
			"An agent in a session of its own, on a daemon that is stopping, must be left running by every site the gate covers: the session outlives this process and the next boot looks for it. Killing it here strands the bead in progress with nothing alive to adopt.", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The two half-conditions
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_AnAgentInItsOwnSessionIsTornDownWhenTheDaemonKeepsRunning
// is the first half-condition: the session is independent, but the run is
// ending on its own terms rather than because the daemon is stopping. There is
// nothing to survive for, so the teardown must run exactly as it does for any
// other run.
//
// This is the half that a gate testing only the session kind would get wrong,
// and it is invisible to the both-facts test above: that one would stay green
// while every independent-session run leaked its tmux session.
func TestSurviveShutdown_AnAgentInItsOwnSessionIsTornDownWhenTheDaemonKeepsRunning(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, false)

	if got := run.substrate.spawnCount(); got != 1 {
		t.Fatalf("substrate spawns = %d, want 1", got)
	}
	if _, teardown := run.consults(); teardown == 0 {
		t.Fatal("the teardown pair never asked the gate, so this test says nothing about the gate")
	}
	if got := run.killsFromTheTeardownSite(); got == 0 {
		t.Error("the teardown pair killed nothing, want at least one kill.\n" +
			"The daemon is still running, so this run's session has nothing to outlive. Leaving it standing leaks a tmux session on every independent-session run.")
	}
}

// TestSurviveShutdown_AnAgentSharingTheDaemonSessionIsKilledWhenTheDaemonStops
// is the second half-condition: the daemon is stopping, but the agent runs in a
// window of the daemon's own session, which does not outlive the daemon. There
// is nothing that COULD survive, so the abort kill and the teardown must both
// run.
//
// A gate testing only for a stopping daemon would skip the kill here and orphan
// the agent — a live process in a pane with no daemon watching it and no
// session that outlives the one being torn down.
func TestSurviveShutdown_AnAgentSharingTheDaemonSessionIsKilledWhenTheDaemonStops(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, false, true)

	if got := run.substrate.spawnCount(); got != 1 {
		t.Fatalf("substrate spawns = %d, want 1", got)
	}
	if run.result.Dispatch.Phase != runexec.DispatchAborted {
		t.Fatalf("dispatch phase = %q, want %q — the shutdown edge never fired",
			run.result.Dispatch.Phase, runexec.DispatchAborted)
	}
	if abort, _ := run.consults(); abort == 0 {
		t.Fatal("the abort kill never asked the gate, so this test says nothing about the gate")
	}
	if got := run.killsFromTheAbortSite(); got == 0 {
		t.Error("the abort kill killed nothing, want at least one kill.\n" +
			"This agent shares the daemon's own tmux session, so nothing about it outlives the daemon. Skipping the kill orphans a live agent with no daemon watching it.")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// What the gate does NOT cover — current behaviour, pinned so a change reads as
// a deliberate diff rather than an accident
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_TheCompletionWaitKillsTheSessionBothGatedSitesSpared pins
// a DEFECT, and it is the one that makes the survive case hollow inside the
// daemon's own process.
//
// The completion wait kills the session when it finds the run context already
// cancelled. It does not read the gate. On the ordering a real shutdown
// produces — the daemon stops while the agent is still coming up — the session
// that the abort kill and the teardown pair both carefully spared is ended a
// few lines later by the wait, before the next boot ever gets a chance to look
// for it.
//
// So the gate is not the only thing that can end the agent, and skipping the
// two sites it covers does not deliver the survive disposition. Stating that
// here means the migration onto internal/runlease cannot quietly inherit the
// belief that it does.
func TestSurviveShutdown_TheCompletionWaitKillsTheSessionBothGatedSitesSpared(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, true)

	if got := run.session.gatedKills(); got != 0 {
		t.Fatalf("gated kills = %d, want 0 — the survive case did not hold, so this test is not observing what it claims", got)
	}
	if got := run.session.killsFromTheCompletionWait(); got == 0 {
		t.Error("the completion wait did not kill the session.\n" +
			"CURRENT BEHAVIOUR IS THAT IT DOES: it kills on an already-cancelled run context with no reference to the survive gate. If this now fails because the wait learned about the disposition, that is the intended migration — update this test and say so. Do not make it pass again by adding a kill back.")
	}
}

// TestSurviveShutdown_TheHookSessionIsClosedEvenWhenTheGatedSitesSpareTheAgent
// pins a second DEFECT.
//
// The gate covers the agent session. It does not cover the hook session, which
// is closed on every exit path. A run meant to outlive the daemon therefore
// keeps its session and loses the channel it reports through: the surviving
// agent can still work and can no longer say anything about it.
//
// specs/run-state-machine.md §4a RSM-037 puts the hook session in the survive
// set, and internal/runlease.Disposition.Releases already answers that way. The
// migration that moves this site onto the lease will CHANGE this assertion.
// That is why it is pinned now — so the change reads as a deliberate edit to a
// named claim rather than as a drift nobody noticed.
func TestSurviveShutdown_TheHookSessionIsClosedEvenWhenTheGatedSitesSpareTheAgent(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, true)

	registered, closed := run.hooks.counts()
	if registered == 0 {
		t.Fatal("no hook session was registered, so its close proves nothing")
	}
	if got := run.session.gatedKills(); got != 0 {
		t.Fatalf("gated kills = %d, want 0 — the survive case did not hold, so this test is not observing what it claims", got)
	}
	if closed == 0 {
		t.Error("hook session closes = 0.\n" +
			"CURRENT BEHAVIOUR IS THAT IT CLOSES. If this now fails because the hook session joined the survive set, that is the intended migration — update this test and say so. Do not make it pass again by re-closing the hook session.")
	}
}
