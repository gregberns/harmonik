package daemon

// coldstarttoken_test.go — the cold-start token: a remote run must hold one to
// start its agent, it gives the token back when the agent is ready, and it gives
// it back exactly once however the run ends.
//
// # Which token this file is about
//
// There are TWO spawn tokens in this daemon and they never overlap.
//
//   - The COLD-START token, held here. One channel for the whole daemon,
//     capacity 3, built in newWorkLoopDeps and reached through
//     SharedHandles.AgentSpawnSem. beadRunOne takes one immediately before the
//     remote agent launch and gives it back as soon as the readiness phase
//     settles. A LOCAL run never takes one.
//   - The tmux substrate's own spawn cap, which bounds LOCAL window spawns. It
//     has a different owner, a different release path, and no coordination with
//     the cold-start token. A REMOTE run never takes one.
//
// Every test name here says "cold-start" for that reason. A name that only said
// "spawn token" would be true of both and would pin neither.
//
// # What was here before
//
// Nothing. The token had no test at all: not taken, not given back, not given
// back once, not given back when the readiness phase times out. It is also the
// best-behaved resource the run holds, so it is the shape the other eight are
// meant to be rewritten to, and a shape with no test cannot serve as a model.
//
// # How these tests observe the token
//
// Each test drives the real beadRunOne through runBeadOneTest with a
// pre-selected worker, which is what makes the run remote. The token is a
// channel the test owns, so its occupancy is the observable. A token already in
// the channel stands for a sibling run holding it. The tests then read three
// things:
//
//   - whether the agent process ever started, through a marker file the stub
//     agent writes as its first act,
//   - whether a free slot exists at a moment when the agent is known to be still
//     running,
//   - how many tokens are in the channel once beadRunOne has returned.
//
// The stub agent blocks until the test releases it. That is what makes "the
// token came back while the run body was still going" an observation rather than
// a race.
//
// # Mutation record
//
// Every test here was checked by breaking the thing it claims to protect and
// confirming that test went red. Each test names its own mutation. Seven were
// run in total:
//
//   - delete the take — the five remote tests all go red, and only the local
//     test stays green, which is what it is for,
//   - remove the prompt give-back — the window test goes red,
//   - make a lease spendable twice — the exactly-once test goes red,
//   - hold the token on a lease the run scope does not hold — the
//     readiness-timeout test goes red,
//   - gate a local run too — the local test goes red,
//   - set the production capacity to 2, and then to 4 — BOTH capacity subtests
//     go red both times.
//
// Removing the prompt give-back also takes a second test down with it, and that
// is honest: it trips the exactly-once test at its stated precondition, because
// that test needs both give-back paths to run.
//
// # Where the fixture lives
//
// The remote setting — the repository, the ssh shim, the tunnel seam, the
// reserved worker slot, the bead and the run environment — is in
// remoterunfixture_test.go and is shared with the tunnel-refusal test. Only what
// these tests vary is here.
//
// Helper names in this package are package-scoped. Grep the package before you
// add one, because two files that add the same name in separate worktrees merge
// cleanly and then fail to build.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/claude"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/workers"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture timings
// ─────────────────────────────────────────────────────────────────────────────

const (
	// coldstartWaitLimit bounds every "wait until X happens" loop. A test that
	// hits it fails with its own message rather than with a package-wide timeout.
	coldstartWaitLimit = 30 * time.Second

	// coldstartParkGrace is how long a test waits after the run has reached the
	// take site before it concludes the run is parked there.
	//
	// The run signals through the launch-spec builder, which is the last step
	// before the take. Nothing between the two blocks, so this grace covers
	// scheduling only.
	coldstartParkGrace = 500 * time.Millisecond

	// coldstartReadyTimeout is the readiness deadline the ready-timeout test
	// gives a remote run. It must be long enough for the sampler below to see
	// the token held and short enough to keep the test quick.
	coldstartReadyTimeout = 700 * time.Millisecond

	// coldstartSamplePeriod is how often a test polls an observable.
	coldstartSamplePeriod = 2 * time.Millisecond
)

// ─────────────────────────────────────────────────────────────────────────────
// The stub agent
// ─────────────────────────────────────────────────────────────────────────────

// coldstartAgent is a stub agent that reports when it started and then waits for
// the test to let it finish.
//
// Holding the agent open is what separates "the token came back at the end of
// the cold-start window" from "the token came back when the run ended". Without
// it both readings produce the same channel occupancy.
type coldstartAgent struct {
	// startedPath is created by the agent as its first act.
	startedPath string
	// releasePath is created by the TEST. The agent exits once it appears.
	releasePath string
}

// coldstartNewAgent builds a stub agent under its own directory.
func coldstartNewAgent(t *testing.T) coldstartAgent {
	t.Helper()
	dir := t.TempDir()
	return coldstartAgent{
		startedPath: filepath.Join(dir, "agent-started"),
		releasePath: filepath.Join(dir, "agent-may-exit"),
	}
}

// handlerArgs returns the arguments beadRunOne prepends to the launch spec. The
// binary is /bin/sh, so these make the agent a shell script.
func (a coldstartAgent) handlerArgs() []string {
	return []string{
		"-c",
		"touch " + a.startedPath + "; while [ ! -f " + a.releasePath + " ]; do sleep 0.02; done",
	}
}

// started reports whether the agent process has begun.
func (a coldstartAgent) started() bool {
	_, err := os.Stat(a.startedPath)
	return err == nil
}

// waitStarted blocks until the agent begins, or reports false at the limit.
func (a coldstartAgent) waitStarted() bool {
	return coldstartWaitFor(coldstartWaitLimit, a.started)
}

// letFinish tells the agent it may exit.
func (a coldstartAgent) letFinish(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(a.releasePath, []byte("go\n"), 0o600); err != nil {
		t.Fatalf("coldstartAgent: release the stub agent: %v", err)
	}
}

// coldstartWaitFor polls cond until it holds or the limit passes.
func coldstartWaitFor(limit time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(limit)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(coldstartSamplePeriod)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Adapters — the two readiness outcomes these tests need
// ─────────────────────────────────────────────────────────────────────────────

// coldstartAdapter answers the readiness question with a constant. The two
// answers are the only thing these tests need out of an adapter, so they are one
// type with a field rather than two types carrying the same five methods.
type coldstartAdapter struct{ ready bool }

func (a coldstartAdapter) DetectReady(_ core.EventEnvelope) bool { return a.ready }

func (coldstartAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}

func (coldstartAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}

func (coldstartAdapter) RotateAccount(_ context.Context) error { return nil }

func (coldstartAdapter) Diagnose(_ context.Context) (handlercontract.DiagnosticReport, error) {
	return handlercontract.DiagnosticReport{}, handlercontract.ErrDeterministic
}

// coldstartReadyAtOnce treats the first event of the run as agent_ready.
//
// It stands in for the production claude adapter, whose ready signal arrives over
// the hook relay that no in-process fixture runs. The shape it reproduces is the
// one the token depends on: readiness settles while the agent keeps working.
// Under the real adapter this fixture's agent is never ready at all, the
// cold-start window swallows the whole run, and "the token came back early" stops
// being expressible.
var coldstartReadyAtOnce = coldstartAdapter{ready: true}

// coldstartNeverReady never reports ready, so the run ends on the readiness
// deadline.
var coldstartNeverReady = coldstartAdapter{ready: false}

// coldstartRegistryFor registers one adapter under claude-code, which is the
// agent type every bead in this file resolves to, and then seals the registry.
//
// Sealing is not load-bearing here. A registry seals itself on the first ForAgent
// call, and beadRunOne makes that call, so an unsealed registry reaches the same
// state a moment later. It is done at construction because that is the order
// production is in — every Register at boot, then reads for the life of the
// daemon — and because eight more resources get fixtures modelled on this one.
func coldstartRegistryFor(t *testing.T, adapter handlercontract.Adapter) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, adapter); err != nil {
		t.Fatalf("coldstartRegistryFor: Register: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) //nolint:errcheck // called for its sealing effect; beadRunOne reads the adapter back through the registry
	return reg
}

// ─────────────────────────────────────────────────────────────────────────────
// The run under test
// ─────────────────────────────────────────────────────────────────────────────

// coldstartOptions are the four things the tests vary.
type coldstartOptions struct {
	// remote decides whether beadRunOne is handed a pre-selected worker. Only a
	// remote run reaches the cold-start token.
	remote bool
	// token is the cold-start channel. The test owns it and reads its occupancy.
	token chan struct{}
	// agent is the stub agent this run launches.
	agent coldstartAgent
	// adapter decides how the readiness phase ends.
	adapter handlercontract.Adapter
	// readyTimeout is the readiness deadline. Zero leaves the production
	// default, which is 210 seconds for a remote run and far past any test.
	readyTimeout time.Duration
}

// coldstartRun is one prepared call of beadRunOne.
type coldstartRun struct {
	deps        workLoopDeps
	env         runloop.RunEnv
	preSelected *workers.Worker
	token       chan struct{}
	ledger      *runplanacqLedger

	// atTakeSite closes once the run has built its launch spec, which is the
	// last step before it takes the token. A test that must observe the run
	// PARKED at the take waits on this first, so its grace covers scheduling
	// rather than the whole tunnel and worktree setup.
	atTakeSite chan struct{}
}

// coldstartPrepare builds a runnable beadRunOne call.
//
// It must be called from the test goroutine: it sets PATH and swaps a
// package-level seam in internal/transport/tunnel.
func coldstartPrepare(t *testing.T, opt coldstartOptions) *coldstartRun {
	t.Helper()

	projectDir := remotefixRepo(t)
	// Exit 0: the tunnel readiness probe runs over this shim, and a non-zero exit
	// refuses the run before it reaches the cold-start token.
	remotefixSSHShim(t, 0)
	remotefixTunnelSeam(t, remotefixIdleTunnel)

	var workerReg *workers.Registry
	var preSelected *workers.Worker
	if opt.remote {
		workerReg, preSelected = remotefixReserveWorker(t)
	}

	// The worktree is handed in rather than created, so no test here depends on
	// git worktree add against a worker that does not exist.
	worktreeDir := t.TempDir()
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		return worktreeDir, func() {}, nil
	}

	atTakeSite := make(chan struct{})
	var takeSiteOnce bool
	launchSpecBuilder := func(ctx context.Context, lc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		spec, artifacts, err := claude.BuildLaunchSpec(ctx, lc)
		// The builder runs once per run on the run's own goroutine, so the flag
		// needs no lock.
		if !takeSiteOnce {
			takeSiteOnce = true
			close(atTakeSite)
		}
		return spec, artifacts, err
	}

	ledger := &runplanacqLedger{}
	params := remotefixParams(t, projectDir)
	params.BrAdapter = ledger
	params.Bus = &runplanBus{}
	params.HandlerArgs = opt.agent.handlerArgs()
	params.AdapterRegistry2 = coldstartRegistryFor(t, opt.adapter)
	params.WorkerRegistry = workerReg
	params.WorktreeFactory = worktreeFactory
	params.LaunchSpecBuilder = launchSpecBuilder
	params.AgentSpawnSem = opt.token
	params.AgentReadyTimeout = opt.readyTimeout
	params.RemoteAgentReadyTimeout = opt.readyTimeout
	deps := ExportedWorkLoopDeps(params)

	env := remotefixRunEnv(deps, remotefixBead("hk-coldstart-probe", "cold-start token probe"))

	return &coldstartRun{
		deps:        deps,
		env:         env,
		preSelected: preSelected,
		token:       opt.token,
		ledger:      ledger,
		atTakeSite:  atTakeSite,
	}
}

// start runs beadRunOne on its own goroutine and returns a channel that closes
// when it returns.
func (r *coldstartRun) start(t *testing.T) <-chan struct{} {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*coldstartWaitLimit)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		runBeadOneTest(ctx, r.deps, r.env, "", r.preSelected, false)
	}()
	return done
}

// waitAtTakeSite blocks until the run has built its launch spec, then waits out
// the park grace. After it returns, a run that is going to park at the take is
// parked there.
func (r *coldstartRun) waitAtTakeSite(t *testing.T) {
	t.Helper()
	select {
	case <-r.atTakeSite:
	case <-time.After(coldstartWaitLimit):
		t.Fatal("the run never built a launch spec, so it never reached the cold-start take site " +
			"and nothing below would be evidence about the token")
	}
	time.Sleep(coldstartParkGrace)
}

// coldstartProductionToken returns the cold-start channel the production
// constructor builds.
//
// The capacity test reads the number 3 from here rather than writing 3 into the
// fixture. A test that made its own channel would keep passing after someone
// changed the production capacity, which is the one thing that test exists to
// notice.
func coldstartProductionToken(t *testing.T) chan struct{} {
	t.Helper()
	bus := eventbus.NewBusImpl()
	deps, err := newWorkLoopDeps(t.Context(),
		Config{ProjectDir: t.TempDir(), HandlerBinary: "/bin/sh", BrPath: "/bin/true"},
		bus, "", handlercontract.NewAdapterRegistry(), nil)
	if err != nil {
		t.Fatalf("coldstartProductionToken: newWorkLoopDeps: %v", err)
	}
	if deps.agentSpawnSem == nil {
		t.Fatal("the production constructor left the cold-start channel nil, so every remote run " +
			"is ungated and the capacity below bounds nothing")
	}
	return deps.agentSpawnSem
}

// coldstartFill puts n tokens in the channel, standing for n sibling runs that
// already hold one.
func coldstartFill(t *testing.T, token chan struct{}, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case token <- struct{}{}:
		default:
			t.Fatalf("coldstartFill: the cold-start channel took only %d of %d tokens, so its capacity "+
				"is below what this test was built to read", i, n)
		}
	}
}

// coldstartAdmitByFreeingASlot parks the run on a channel that is already full,
// proves it is waiting there for a token, and then frees one so it may proceed.
//
// Every test that measures the GIVE-BACK needs this preamble. Without it, "a
// free slot exists" and "the count is back where it started" are both true of a
// run that never took a token at all, so a give-back test would stay green after
// the take was deleted. Parking the run first is what rules that out, and it is
// deterministic: the run signals when it reaches the take site, and a run that is
// going to wait is already waiting by the time the grace has passed.
//
// The caller must have filled the channel to capacity before starting the run.
func coldstartAdmitByFreeingASlot(t *testing.T, run *coldstartRun, agent coldstartAgent) {
	t.Helper()
	run.waitAtTakeSite(t)
	if agent.started() {
		t.Fatal("the agent started while every cold-start token was held, so this run took no token " +
			"and nothing this test measures about giving one back would be evidence")
	}
	<-run.token
	if !agent.waitStarted() {
		t.Fatalf("the agent never started within %v after a cold-start token came free", coldstartWaitLimit)
	}
}

// coldstartWatchForFullChannel starts a sampler that records whether the channel
// was ever at capacity. Call the returned stop function before reading it.
//
// This is the other way to show the run took a token, for a test that cannot use
// the parking preamble above.
func coldstartWatchForFullChannel(token chan struct{}) (seenFull func() bool, stop func()) {
	full := make(chan struct{}, 1)
	watching := make(chan struct{})
	go func() {
		for {
			select {
			case <-watching:
				return
			default:
			}
			if len(token) == cap(token) {
				select {
				case full <- struct{}{}:
				default:
				}
				return
			}
			time.Sleep(coldstartSamplePeriod)
		}
	}()
	var stopOnce sync.Once
	return func() bool { return len(full) > 0 },
		func() { stopOnce.Do(func() { close(watching) }) }
}

// coldstartFreeSlot reports whether the channel has room for one more token,
// WITHOUT keeping the slot. A free slot at a moment when the agent is known to
// be running is how these tests see that the run gave its token back.
func coldstartFreeSlot(token chan struct{}) bool {
	select {
	case token <- struct{}{}:
		<-token
		return true
	default:
		return false
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// A remote run must hold a token before it may start its agent
// ─────────────────────────────────────────────────────────────────────────────

// TestColdStartToken_ARemoteRunTakesATokenBeforeItStartsItsAgent drives a remote
// run against a cold-start channel that is already full and asserts the run
// starts no agent until a token comes free.
//
// The claim is about ORDER, so the test is built as one: with no token the agent
// must not exist, and the same run must produce that agent as soon as a token
// appears. The second half is what makes the first mean something. Without it
// "no agent" would also be true of a fixture that could never launch one.
//
// Mutation: delete the take (the select on AgentSpawnSem in beadRunOne). The
// agent then starts while the channel is full and the first assertion goes red.
func TestColdStartToken_ARemoteRunTakesATokenBeforeItStartsItsAgent(t *testing.T) {
	// Not parallel: sets PATH and swaps a package-level seam in
	// internal/transport/tunnel.
	agent := coldstartNewAgent(t)
	token := make(chan struct{}, 1)
	coldstartFill(t, token, 1)

	run := coldstartPrepare(t, coldstartOptions{
		remote:  true,
		token:   token,
		agent:   agent,
		adapter: coldstartReadyAtOnce,
	})
	done := run.start(t)

	run.waitAtTakeSite(t)
	if agent.started() {
		t.Fatal("the agent started while every cold-start token was held.\n" +
			"beadRunOne must take a token BEFORE the remote launch. Above the take, the cap bounds " +
			"nothing and the reviewer cold-start that the cap exists to stagger runs at the same time " +
			"as the implementer's.")
	}
	select {
	case <-done:
		t.Fatal("the run finished while every cold-start token was held, so it never waited for one")
	default:
	}

	// Give a token back on behalf of the sibling run, and the parked run must
	// proceed.
	<-token
	if !agent.waitStarted() {
		t.Fatalf("the agent never started within %v after a cold-start token came free.\n"+
			"The assertion above — no agent while the channel was full — is only evidence if this "+
			"same fixture does launch one once a token exists.", coldstartWaitLimit)
	}

	agent.letFinish(t)
	coldstartAwait(t, done)
}

// ─────────────────────────────────────────────────────────────────────────────
// A local run is not gated at all
// ─────────────────────────────────────────────────────────────────────────────

// TestColdStartToken_ALocalRunTakesNoColdStartToken drives a LOCAL run against a
// cold-start channel that is already full and asserts the agent starts anyway
// and the channel is never touched.
//
// The cap exists for one cost: a second claude cold-start over a reverse SSH
// tunnel. A local run builds no tunnel, so gating it would queue work behind a
// constraint it does not carry.
//
// This test rests on the one above for its control. That test shows a full
// channel really can park a run on this same fixture, which is what makes "the
// local run launched anyway" a statement about the local branch rather than
// about a channel that never blocked anyone.
//
// Mutation: drop the rbc != nil guard on the take. The local run then parks on
// the full channel, no agent appears, and this test fails at the wait.
func TestColdStartToken_ALocalRunTakesNoColdStartToken(t *testing.T) {
	// Not parallel: sets PATH and swaps a package-level seam in
	// internal/transport/tunnel.
	agent := coldstartNewAgent(t)
	token := make(chan struct{}, 1)
	coldstartFill(t, token, 1)

	run := coldstartPrepare(t, coldstartOptions{
		remote:  false,
		token:   token,
		agent:   agent,
		adapter: coldstartReadyAtOnce,
	})
	done := run.start(t)

	if !agent.waitStarted() {
		t.Fatalf("a LOCAL run started no agent within %v against a full cold-start channel.\n"+
			"A local run constructs no reverse tunnel and must skip the cold-start gate entirely.",
			coldstartWaitLimit)
	}
	if got := len(token); got != 1 {
		t.Errorf("cold-start tokens outstanding while a local run was live = %d, want 1 — the one "+
			"held by the sibling run this fixture seeded. A local run must not touch the channel", got)
	}

	agent.letFinish(t)
	coldstartAwait(t, done)

	if got := len(token); got != 1 {
		t.Errorf("cold-start tokens outstanding after a local run = %d, want 1. The local run gave "+
			"back a token it never took, which steals the sibling run's slot", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The token comes back at the end of the cold-start window
// ─────────────────────────────────────────────────────────────────────────────

// TestColdStartToken_TheTokenComesBackWhenTheColdStartWindowEndsNotWhenTheRunEnds
// asserts the run gives its token back as soon as readiness settles, while its
// agent is still working.
//
// This is the whole point of the cap. It bounds concurrent cold STARTS, not
// concurrent runs. Held for the run body, three long runs would block every
// later remote dispatch for hours.
//
// The observation is a free slot at a moment when the agent is known to be
// blocked in its wait loop, so the run cannot have ended.
//
// The channel starts FULL and the run is admitted only after the test frees a
// slot. That preamble is load-bearing. A free slot is also what a run that never
// took a token leaves behind, so without the preamble this test would stay green
// after the take was deleted.
//
// Mutation: remove AfterReadyResolved from the launch input, which leaves only
// the deferred give-back at the end of the run. The channel then stays full for
// as long as the agent is blocked and this test fails.
func TestColdStartToken_TheTokenComesBackWhenTheColdStartWindowEndsNotWhenTheRunEnds(t *testing.T) {
	// Not parallel: sets PATH and swaps a package-level seam in
	// internal/transport/tunnel.
	agent := coldstartNewAgent(t)
	// Capacity two, both tokens out: one stands for a sibling run and one is the
	// slot this test hands over.
	token := make(chan struct{}, 2)
	coldstartFill(t, token, 2)

	run := coldstartPrepare(t, coldstartOptions{
		remote:  true,
		token:   token,
		agent:   agent,
		adapter: coldstartReadyAtOnce,
	})
	done := run.start(t)

	coldstartAdmitByFreeingASlot(t, run, agent)

	freed := coldstartWaitFor(coldstartWaitLimit, func() bool { return coldstartFreeSlot(token) })
	stillRunning := !coldstartClosed(done)
	if !freed {
		t.Errorf("no cold-start slot came free within %v while the agent was still blocked.\n"+
			"The token must be given back when the readiness phase settles, not when the run returns. "+
			"Held for the run body, the capacity bounds concurrent RUNS instead of concurrent "+
			"cold starts.", coldstartWaitLimit)
	}
	if !stillRunning {
		t.Error("the run returned before the free slot was observed, so the observation cannot tell " +
			"an early give-back from the deferred one")
	}

	agent.letFinish(t)
	coldstartAwait(t, done)
}

// ─────────────────────────────────────────────────────────────────────────────
// The token comes back exactly once
// ─────────────────────────────────────────────────────────────────────────────

// TestColdStartToken_TheTokenComesBackExactlyOnceAcrossBothGiveBackPaths drives
// one run on which BOTH give-back paths execute and asserts the channel is left
// exactly as it was found.
//
// Both paths do run on this run. The prompt give-back fires when readiness
// settles, which the test observes directly. The run scope's close then fires
// when beadRunOne returns, because a defer has no condition. Only the lease
// between them stops the second from taking a token that belongs to somebody
// else: a lease runs its give-back at most once, and the scope's close finds it
// already spent.
//
// The sibling token is what makes that visible. A double give-back cannot push
// the count below zero — it takes the sibling's token instead — so the failure
// mode is a silent theft, and an empty channel would hide it completely.
//
// The channel starts FULL and the run is admitted only after the test frees a
// slot, for the same reason the previous test does it: a final count of one is
// also what a run that never took a token leaves behind.
//
// Mutation: make a lease spendable twice — have runlease.Lease.spend return the
// give-back call without clearing it. The scope's close then makes a second
// receive, the sibling token is gone, and the final count reads 0 instead of 1.
func TestColdStartToken_TheTokenComesBackExactlyOnceAcrossBothGiveBackPaths(t *testing.T) {
	// Not parallel: sets PATH and swaps a package-level seam in
	// internal/transport/tunnel.
	agent := coldstartNewAgent(t)
	// Capacity two, both tokens out: one stands for a sibling run and one is the
	// slot this test hands over.
	token := make(chan struct{}, 2)
	coldstartFill(t, token, 2)

	run := coldstartPrepare(t, coldstartOptions{
		remote:  true,
		token:   token,
		agent:   agent,
		adapter: coldstartReadyAtOnce,
	})
	done := run.start(t)

	coldstartAdmitByFreeingASlot(t, run, agent)

	if !coldstartWaitFor(coldstartWaitLimit, func() bool { return coldstartFreeSlot(token) }) {
		t.Fatalf("the prompt give-back never fired within %v.\n"+
			"Only one give-back path ran on this run, so the count below cannot show that a second "+
			"one was suppressed.", coldstartWaitLimit)
	}

	agent.letFinish(t)
	coldstartAwait(t, done)

	if got := len(token); got != 1 {
		t.Errorf("cold-start tokens outstanding after the run = %d, want 1 — the sibling run's.\n"+
			"Both give-back paths ran on this run: the prompt one when readiness settled, and the "+
			"deferred one when beadRunOne returned. A count of 0 means the deferred path took a "+
			"second token, which is the sibling run's slot handed to nobody.", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The token comes back when readiness times out
// ─────────────────────────────────────────────────────────────────────────────

// TestColdStartToken_TheTokenComesBackWhenReadinessTimesOut asserts the deferred
// give-back covers the path where the prompt one never runs.
//
// The two paths are not redundant. When the readiness phase fails on its
// deadline, the launch returns before the prompt give-back is reached, so the
// deferred one is the ONLY thing that returns the token. A leak here is silent
// and permanent: after three timed-out remote runs the daemon's remote dispatch
// stops for the rest of its life.
//
// The test also watches the channel while the run is inside the readiness window
// and requires that the token was really held. Without that, "the count is back
// to one" would also be true of a run that never took a token at all.
//
// Mutation: delete the deferred give-back and keep only AfterReadyResolved. The
// timed-out run then keeps its token and the final count reads 2.
func TestColdStartToken_TheTokenComesBackWhenReadinessTimesOut(t *testing.T) {
	// Not parallel: sets PATH and swaps a package-level seam in
	// internal/transport/tunnel.
	agent := coldstartNewAgent(t)
	token := make(chan struct{}, 2)
	coldstartFill(t, token, 1)

	run := coldstartPrepare(t, coldstartOptions{
		remote:       true,
		token:        token,
		agent:        agent,
		adapter:      coldstartNeverReady,
		readyTimeout: coldstartReadyTimeout,
	})
	// Sample the channel while the run is inside its readiness window. The
	// deadline holds that window open long enough to be seen.
	seenFull, stopWatch := coldstartWatchForFullChannel(token)
	done := run.start(t)

	coldstartAwait(t, done)
	stopWatch()

	if !seenFull() {
		t.Error("the cold-start channel was never full while the run was live, so this run never " +
			"took a token and the count below is not evidence that it gave one back")
	}
	if got := len(token); got != 1 {
		t.Errorf("cold-start tokens outstanding after a run that timed out on readiness = %d, want 1.\n"+
			"The readiness failure returns from the launch BEFORE the prompt give-back, so the "+
			"deferred one is the only thing that returns this token. A count of 2 is a permanent "+
			"leak: three such runs park every later remote dispatch for the life of the daemon.", got)
	}

	// The run must have ended on the readiness deadline. Any other reason means
	// the fixture measured a different path.
	calls := run.ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d, want exactly 1\ncalls=%+v", len(calls), calls)
	}
	if calls[0].reason != "agent_ready_timeout" {
		t.Errorf("the run ended for reason %q, want %q — a different reason means this test did not "+
			"reach the readiness-timeout path", calls[0].reason, "agent_ready_timeout")
	}

	agent.letFinish(t)
}

// ─────────────────────────────────────────────────────────────────────────────
// The capacity is three
// ─────────────────────────────────────────────────────────────────────────────

// TestColdStartToken_ThreeRemoteColdStartsMayRunAtOnceAndAFourthWaits reads the
// capacity the production constructor builds and drives a run against both sides
// of it.
//
// The two subtests pin the number from opposite directions. Lower the capacity
// and "two out" is already full, so the admitted case fails. Raise it and "three
// out" leaves room, so the parked case fails. Neither subtest alone says
// anything about the number.
//
// The channel comes from newWorkLoopDeps rather than from this file. A fixture
// that made its own channel of three would keep passing after somebody changed
// the production capacity, which is the only thing this test exists to notice.
//
// Mutation: change the capacity in newWorkLoopDeps from 3 to 2, and again to 4.
// BOTH subtests go red both times, because each also checks that the channel
// reaches capacity exactly when a capacity of 3 says it should.
func TestColdStartToken_ThreeRemoteColdStartsMayRunAtOnceAndAFourthWaits(t *testing.T) {
	// Not parallel: the subtests set PATH and swap a package-level seam in
	// internal/transport/tunnel.
	t.Run("a remote run starts its agent while two cold-start tokens are out", func(t *testing.T) {
		agent := coldstartNewAgent(t)
		token := coldstartProductionToken(t)
		coldstartFill(t, token, 2)

		// The readiness deadline holds the cold-start window open long enough for
		// the sampler below to see the channel at capacity. Without that the
		// window closes in microseconds, the sampler misses it, and "the agent
		// started" would also be true of a run that took no token.
		run := coldstartPrepare(t, coldstartOptions{
			remote:       true,
			token:        token,
			agent:        agent,
			adapter:      coldstartNeverReady,
			readyTimeout: coldstartReadyTimeout,
		})
		seenFull, stopWatch := coldstartWatchForFullChannel(token)
		done := run.start(t)

		if !agent.waitStarted() {
			t.Fatalf("no agent started within %v while only two cold-start tokens were out.\n"+
				"The production capacity admits a third concurrent cold start. If this run parked, "+
				"the capacity is now below 3 and the daemon serialises remote dispatch more than it "+
				"was built to.", coldstartWaitLimit)
		}
		coldstartAwait(t, done)
		stopWatch()
		if !seenFull() {
			t.Errorf("the cold-start channel never reached its capacity of %d while this run was live.\n"+
				"Two tokens were out and this run took a third, so a capacity of 3 means the channel "+
				"was full for the length of the readiness window. It was not, which means either the "+
				"take is gone or the capacity is now above 3.", cap(token))
		}
		agent.letFinish(t)
	})

	t.Run("a remote run waits while three cold-start tokens are out", func(t *testing.T) {
		agent := coldstartNewAgent(t)
		token := coldstartProductionToken(t)
		coldstartFill(t, token, 3)

		run := coldstartPrepare(t, coldstartOptions{
			remote:  true,
			token:   token,
			agent:   agent,
			adapter: coldstartReadyAtOnce,
		})
		done := run.start(t)

		run.waitAtTakeSite(t)
		if agent.started() {
			t.Fatal("a fourth remote cold start began while three tokens were already out.\n" +
				"The production capacity is 3. A higher one puts a fourth claude cold start on the " +
				"reverse tunnel at the same time as the other three, which is the contention that " +
				"trips agent_ready_timeout under a full ramp.")
		}

		// Let the run finish so the fixture tears down cleanly.
		<-token
		if !agent.waitStarted() {
			t.Fatalf("the agent never started within %v after a token came free", coldstartWaitLimit)
		}
		agent.letFinish(t)
		coldstartAwait(t, done)
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Small shared helpers
// ─────────────────────────────────────────────────────────────────────────────

// coldstartClosed reports whether the run has returned, without blocking.
func coldstartClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// coldstartAwait blocks until the run returns, or fails the test.
func coldstartAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * coldstartWaitLimit):
		t.Fatalf("beadRunOne did not return within %v", 2*coldstartWaitLimit)
	}
}
