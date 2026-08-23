package daemon

import (
	"context"
	"encoding/json"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
)

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

func (s *surviveGateSession) gatedKills() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killsLiveCtx
}

func (s *surviveGateSession) killsFromTheCompletionWait() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killsCancelled
}

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

type surviveGateEmitter struct{}

var _ handlercontract.EventEmitter = surviveGateEmitter{}

func (surviveGateEmitter) Emit(context.Context, core.EventType, []byte) error { return nil }
func (surviveGateEmitter) EmitWithRunID(context.Context, core.RunID, core.EventType, []byte) error {
	return nil
}

type surviveGateRun struct {
	result    agentLaunchResult
	session   *surviveGateSession
	substrate *surviveGateSubstrate
	hooks     *surviveGateHookStore

	// exitReads counts how many times a release site asked the run for its exit
	// facts, and exitReadsInsideTheLaunch is that count at the moment the launch
	// returned. Together they are the difference between "the machinery ran and
	// chose not to act" and "nothing happened", which is the whole hazard with a
	// claim whose pass condition is that something did not occur. Every site
	// reads the facts, so a site that stopped asking shows up as a drop here
	// even when nothing about the kills changes.
	exitReads                int
	exitReadsInsideTheLaunch int

	// killsInsideTheLaunch is the gated-kill count read the instant the launch
	// returned. It splits the gated kills in two, which is what lets each
	// half-condition name one site rather than "something killed it". The abort
	// kill, the ready-timeout kill and the post-wait window kill are all inside
	// the launch. The session's give-back runs at the scope close, which the
	// caller reaches through Cleanup. So a kill counted before this mark came
	// from a site inside the launch, and a kill counted after it came from the
	// give-back.
	killsInsideTheLaunch int
}

func (r *surviveGateRun) consults() (insideTheLaunch, atCleanup int) {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	return r.exitReadsInsideTheLaunch, r.exitReads - r.exitReadsInsideTheLaunch
}

func (r *surviveGateRun) killsFromInsideTheLaunch() int {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	return r.killsInsideTheLaunch
}

func (r *surviveGateRun) killsFromTheGiveBack() int {
	r.session.mu.Lock()
	defer r.session.mu.Unlock()
	return r.session.killsLiveCtx - r.killsInsideTheLaunch
}

func surviveGateDrive(t *testing.T, indepSession, daemonStops bool) *surviveGateRun {
	t.Helper()
	return surviveGateDriveInto(t, indepSession, daemonStops, nil, true)
}

func surviveGateDriveInto(t *testing.T, indepSession, daemonStops bool, runScope *runlease.Scope, callCleanup bool) *surviveGateRun {
	t.Helper()

	sess := &surviveGateSession{}
	sub := &surviveGateSubstrate{session: sess}
	hooks := &surviveGateHookStore{}
	out := &surviveGateRun{session: sess, substrate: sub, hooks: hooks}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runExit := func() runlease.Exit {
		sess.mu.Lock()
		out.exitReads++
		sess.mu.Unlock()
		return runlease.Exit{
			SessionRunsIndependently: indepSession,
			DaemonStopping:           ctx.Err() != nil,
		}
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
		RunScope: runScope,
		RunExit:  runExit,
	}

	out.result = runAgentLaunch(ctx, in)

	sess.mu.Lock()
	out.killsInsideTheLaunch = sess.killsLiveCtx
	out.exitReadsInsideTheLaunch = out.exitReads
	sess.mu.Unlock()

	if callCleanup {
		out.result.Cleanup()
	}

	return out
}

func surviveGateRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("surviveGate: register claude adapter: %v", err)
	}
	return reg
}

// TestSurviveShutdown_AnAgentInItsOwnSessionIsNotKilledByAnyGatedSiteWhenTheDaemonStops
// is the disposition's whole purpose. Both facts hold, so neither the abort kill
// nor the session's give-back may end the agent: the session is meant to outlive
// this process and be found by the next boot, and killing it here would strand
// the bead in progress with nothing alive to adopt.
//
// The assertions are deliberately not just "no kill". A kill count of zero is
// free in any fixture where nothing runs, so the test also proves the launch
// happened, the abort edge was actually reached, and the exit facts were read
// BOTH inside the launch and again at the give-back. That is the difference
// between machinery that ran and declined, and machinery that never ran.
func TestSurviveShutdown_AnAgentInItsOwnSessionIsNotKilledByAnyGatedSiteWhenTheDaemonStops(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, true)

	if got := run.substrate.spawnCount(); got != 1 {
		t.Fatalf("substrate spawns = %d, want 1. Nothing launched, so every assertion below passes for free", got)
	}
	if run.result.Dispatch.Phase != runexec.DispatchAborted {
		t.Fatalf("dispatch phase = %q, want %q. The shutdown edge never fired, so the abort kill was never on the table",
			run.result.Dispatch.Phase, runexec.DispatchAborted)
	}
	insideTheLaunch, atCleanup := run.consults()
	if insideTheLaunch == 0 {
		t.Error("no site inside the launch read the run's exit facts — the abort kill and the post-wait window kill both took a path that does not, so this test defends neither")
	}
	if atCleanup == 0 {
		t.Error("the give-back never read the run's exit facts — the scope closed without asking, so the session's fate was not decided by the disposition at all")
	}
	if got := run.session.gatedKills(); got != 0 {
		t.Errorf("gated kills = %d, want 0.\n"+
			"An agent in a session of its own, on a daemon that is stopping, must be left running by every site the disposition covers: the session outlives this process and the next boot looks for it. Killing it here strands the bead in progress with nothing alive to adopt.", got)
	}
}

// TestSurviveShutdown_AnAgentInItsOwnSessionIsTornDownWhenTheDaemonKeepsRunning
// is the first half-condition: the session is independent, but the run is
// ending on its own terms rather than because the daemon is stopping. There is
// nothing to survive for, so the give-back must run exactly as it does for any
// other run.
//
// This is the half that a decision testing only the session kind would get
// wrong, and it is invisible to the both-facts test above: that one would stay
// green while every independent-session run leaked its tmux session.
func TestSurviveShutdown_AnAgentInItsOwnSessionIsTornDownWhenTheDaemonKeepsRunning(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, false)

	if got := run.substrate.spawnCount(); got != 1 {
		t.Fatalf("substrate spawns = %d, want 1", got)
	}
	if _, atCleanup := run.consults(); atCleanup == 0 {
		t.Fatal("the give-back never read the run's exit facts, so this test says nothing about the disposition")
	}
	if got := run.killsFromTheGiveBack(); got == 0 {
		t.Error("the session's give-back killed nothing, want at least one kill.\n" +
			"The daemon is still running, so this run's session has nothing to outlive. Leaving it standing leaks a tmux session on every independent-session run.")
	}
}

// TestSurviveShutdown_AnAgentSharingTheDaemonSessionIsKilledWhenTheDaemonStops
// is the second half-condition: the daemon is stopping, but the agent runs in a
// window of the daemon's own session, which does not outlive the daemon. There
// is nothing that COULD survive, so the abort kill and the give-back must both
// run.
//
// A decision testing only for a stopping daemon would skip the kill here and
// orphan the agent — a live process in a pane with no daemon watching it and no
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
	if insideTheLaunch, _ := run.consults(); insideTheLaunch == 0 {
		t.Fatal("no site inside the launch read the run's exit facts, so this test says nothing about the abort kill")
	}
	if got := run.killsFromInsideTheLaunch(); got == 0 {
		t.Error("the abort kill killed nothing, want at least one kill.\n" +
			"This agent shares the daemon's own tmux session, so nothing about it outlives the daemon. Skipping the kill orphans a live agent with no daemon watching it.")
	}
}

// TestSurviveShutdown_TheHookSessionIsKeptWhenTheAgentIsLeftRunning is the
// BEHAVIOUR CHANGE this commit makes, and it reverses the claim this file used
// to pin.
//
// The hook session is the channel the agent reports through. Before the launch's
// resources moved onto a lease it was closed on every exit path, with no
// reference to the survive condition, so a run meant to outlive the daemon kept
// its session and lost the ability to say anything about it. RSM-037 puts the
// hook session in the survive set, runlease.Disposition.Releases has always
// answered that way, and the give-back site now asks it.
//
// The old assertion — "closes = 0 is a defect, current behaviour is that it
// closes" — was written to make this change read as a deliberate edit rather
// than as drift. This is that edit.
//
// The zero close count is paired with positive evidence, because "nothing
// happened" is free in a fixture where nothing runs: the hook session was
// registered, a give-back site did read the exit facts, and the sibling test
// below proves the same site DOES close the hook session for a run that is
// reclaimed.
func TestSurviveShutdown_TheHookSessionIsKeptWhenTheAgentIsLeftRunning(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, true)

	registered, closed := run.hooks.counts()
	if registered != 1 {
		t.Fatalf("hook session registrations = %d, want 1 — nothing was registered, so keeping it proves nothing", registered)
	}
	if got := run.session.gatedKills(); got != 0 {
		t.Fatalf("gated kills = %d, want 0 — the survive case did not hold, so this test is not observing what it claims", got)
	}
	if insideTheLaunch, atCleanup := run.consults(); insideTheLaunch+atCleanup == 0 {
		t.Fatal("no site read the run's exit facts, so the hook session was not KEPT — it was never reached")
	}
	if closed != 0 {
		t.Errorf("hook session closes = %d, want 0.\n"+
			"This agent keeps its tmux session and goes on working after the daemon stops. Closing its hook session leaves it holding a session it can no longer report through, which is the defect RSM-037 puts the hook session in the survive set to fix.", closed)
	}
}

// TestSurviveShutdown_TheHookSessionIsGivenBackWhenTheRunIsReclaimed is the
// control for the test above, and it is what makes that test's zero mean
// something.
//
// Same fixture, same give-back site, one fact flipped: the daemon keeps running,
// so the run is reclaimed and everything goes back. A give-back that had simply
// stopped closing hook sessions would pass the test above and fail this one.
func TestSurviveShutdown_TheHookSessionIsGivenBackWhenTheRunIsReclaimed(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, false)

	registered, closed := run.hooks.counts()
	if registered != 1 {
		t.Fatalf("hook session registrations = %d, want 1", registered)
	}
	if closed != 1 {
		t.Errorf("hook session closes = %d, want exactly 1.\n"+
			"This run ended on its own terms while the daemon was up, so nothing survives and the hook session must go back. Zero leaks a registration for every run. More than one means the give-back ran twice, which the lease exists to make impossible.", closed)
	}
}

// TestSurviveShutdown_TheCompletionWaitKillsTheSessionEveryGatedSiteSpared pins
// a DEFECT, and it is the one that makes the survive case hollow inside the
// daemon's own process.
//
// The completion wait kills the session when it finds the run context already
// cancelled. It does not read the disposition. On the ordering a real shutdown
// produces — the daemon stops while the agent is still coming up — the session
// that the abort kill and the give-back both carefully spared is ended a few
// lines later by the wait, before the next boot ever gets a chance to look for
// it.
//
// So the disposition is not the only thing that can end the agent, and sparing
// the sites it covers does not deliver survival. Filed as hk-jyh5t.
func TestSurviveShutdown_TheCompletionWaitKillsTheSessionEveryGatedSiteSpared(t *testing.T) {
	t.Parallel()

	run := surviveGateDrive(t, true, true)

	if got := run.session.gatedKills(); got != 0 {
		t.Fatalf("gated kills = %d, want 0 — the survive case did not hold, so this test is not observing what it claims", got)
	}
	if got := run.session.killsFromTheCompletionWait(); got == 0 {
		t.Error("the completion wait did not kill the session.\n" +
			"CURRENT BEHAVIOUR IS THAT IT DOES: it kills on an already-cancelled run context with no reference to the disposition. If this now fails because the wait learned about the disposition, that is the intended fix — update this test and say so. Do not make it pass again by adding a kill back.")
	}
}

// TestSurviveShutdown_TheRunScopeGivesBackASessionTheLaunchNeverClosed is what
// the nesting buys.
//
// A graph run takes one hook session and one agent session per node while it
// holds one worktree for the whole run, so the launch's resources sit in a child
// of the run's scope. The child is normally closed by the Cleanup the caller
// defers. This test never calls it — which is the shape of beadRunOne's
// pre-launch refusals, where the run returns before that defer is registered —
// and closes the RUN scope instead. The agent session must still be torn down.
//
// Without the nest the launch would hold a scope nobody else can reach, and the
// tmux session would stay standing on that path with no live agent in it.
//
// The claim is proved twice over: the run scope names the agent session in what
// it released, and a kill reaches the session that no site inside the launch
// issued.
func TestSurviveShutdown_TheRunScopeGivesBackASessionTheLaunchNeverClosed(t *testing.T) {
	t.Parallel()

	var runScope runlease.Scope
	run := surviveGateDriveInto(t, false, false, &runScope, false)

	if got := run.substrate.spawnCount(); got != 1 {
		t.Fatalf("substrate spawns = %d, want 1 — nothing launched, so nothing was held", got)
	}
	if got := run.killsFromTheGiveBack(); got != 0 {
		t.Fatalf("%d kill(s) arrived before the run scope closed, so this test cannot tell what the close did", got)
	}

	rep := runScope.Close(runlease.Reclaim)

	if err := rep.Err(); err != nil {
		t.Errorf("the run scope reported give-back failures: %v", err)
	}
	if !slices.Contains(rep.Released, runlease.AgentSession) {
		t.Errorf("the run scope released %v, want the agent session among them.\n"+
			"The launch's resources are meant to hang in a child of this scope. A report without the agent session means the child was never added to it, so a caller that returns before its Cleanup defer leaves the tmux session standing.", rep.Released)
	}
	if got := run.killsFromTheGiveBack(); got != 1 {
		t.Errorf("kills issued by the run scope's close = %d, want 1.\n"+
			"Naming the resource in the report is not the same as ending the session. The close must reach tmux.", got)
	}
}

// TestSurviveShutdown_TheHeartbeatStopsEvenForARunThatKeepsItsSession pins the
// one half of Cleanup the disposition must NOT reach.
//
// Cleanup does two things that read as a pair and are not: it stops the CHB-019
// heartbeat, and it closes the launch's scope. The scope close is the give-back
// and the disposition answers it. The heartbeat stop is a step — the goroutine
// belongs to this process, and a surviving agent does not hold it — so it runs
// whatever the run gives back.
//
// The old code said that by POSITION: one sync.Once closed the channel and only
// then reached the skip check. The new code composes two calls, so gating the
// heartbeat on the disposition is a one-line edit. This is the test that sees it.
//
// The exit facts are reported directly rather than derived from the context, and
// that is what makes the test possible. A real survive always comes with a
// cancelled run context, which would reap the heartbeat goroutine on its own and
// hide the leak. Here the context stays LIVE while the facts say survive, so the
// explicit stop is the ONLY thing that can end that goroutine.
func TestSurviveShutdown_TheHeartbeatStopsEvenForARunThatKeepsItsSession(t *testing.T) {
	t.Parallel()

	sess := &surviveGateSession{}
	sub := &surviveGateSubstrate{session: sess}
	wt := t.TempDir()

	heartbeatsBefore := surviveGateHeartbeatGoroutines(t)

	in := agentLaunchInput{
		Env: runloop.RunEnv{
			ProjectDir:              wt,
			AgentReadyTimeout:       200 * time.Millisecond,
			RemoteAgentReadyTimeout: 200 * time.Millisecond,
		},
		Ports: runloop.RunPorts{
			Emitter: surviveGateEmitter{},
			Clock:   substrate.SystemClock{},
		},
		Handles: runloop.SharedHandles{
			AdapterRegistry: surviveGateRegistry(t),
			HookStore:       &surviveGateHookStore{},
		},
		RunID:         core.RunID(uuid.New()),
		LogPrefix:     "daemon: survive-shutdown heartbeat test",
		Spec:          handler.LaunchSpec{Binary: "/bin/true", WorkDir: wt},
		Artifacts:     shared.LaunchArtifacts{ResolvedAgentType: core.AgentTypeClaudeCode},
		WorktreePath:  wt,
		BaseSubstrate: sub,
		RunExit: func() runlease.Exit {
			return runlease.Exit{SessionRunsIndependently: true, DaemonStopping: true}
		},
	}

	res := runAgentLaunch(t.Context(), in)

	if got := sub.spawnCount(); got != 1 {
		t.Fatalf("substrate spawns = %d, want 1 — nothing launched, so no heartbeat was ever started", got)
	}
	mine := surviveGateNewHeartbeatGoroutines(t, heartbeatsBefore)
	if len(mine) == 0 {
		t.Fatal("this launch started no heartbeat goroutine of its own, so its absence afterwards proves nothing")
	}
	killsBeforeCleanup := sess.gatedKills()

	res.Cleanup()

	if got := sess.gatedKills(); got != killsBeforeCleanup {
		t.Fatalf("Cleanup issued %d further kill(s) on the session, want 0.\n"+
			"The disposition did not decide survive at the give-back, so this test is not observing the case it claims.", got-killsBeforeCleanup)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		still := surviveGateHeartbeatGoroutines(t)
		alive := false
		for id := range mine {
			if still[id] {
				alive = true
				break
			}
		}
		if !alive {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the CHB-019 heartbeat goroutine this launch started is still running after Cleanup.\n" +
				"Stopping it is a STEP, not a give-back: it belongs to this process, and a surviving agent does not hold it. Gating it on the disposition leaks the goroutine on exactly the path the disposition is for.")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func surviveGateHeartbeatGoroutines(t *testing.T) map[string]bool {
	t.Helper()

	buf := make([]byte, 1<<20)
	var dump string
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			dump = string(buf[:n])
			break
		}
		buf = make([]byte, 2*len(buf))
	}

	ids := make(map[string]bool)
	for _, block := range strings.Split(dump, "\n\ngoroutine ") {
		if !strings.Contains(block, "handler.RunHeartbeatLoop") {
			continue
		}
		header := strings.TrimPrefix(block, "goroutine ")
		id, _, found := strings.Cut(header, " ")
		if found && id != "" {
			ids[id] = true
		}
	}
	return ids
}

func surviveGateNewHeartbeatGoroutines(t *testing.T, before map[string]bool) map[string]bool {
	t.Helper()

	mine := make(map[string]bool)
	for id := range surviveGateHeartbeatGoroutines(t) {
		if !before[id] {
			mine[id] = true
		}
	}
	return mine
}
