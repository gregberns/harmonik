package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

const (
	coldstartWaitLimit = 30 * time.Second

	coldstartParkGrace = 500 * time.Millisecond

	coldstartReadyTimeout = 700 * time.Millisecond

	coldstartSamplePeriod = 2 * time.Millisecond
)

type coldstartAgent struct {
	// startedPath is created by the agent as its first act.
	startedPath string
	// releasePath is created by the TEST. The agent exits once it appears.
	releasePath string
}

func coldstartNewAgent(t *testing.T) coldstartAgent {
	t.Helper()
	dir := t.TempDir()
	return coldstartAgent{
		startedPath: filepath.Join(dir, "agent-started"),
		releasePath: filepath.Join(dir, "agent-may-exit"),
	}
}

func (a coldstartAgent) handlerArgs() []string {
	return []string{
		"-c",
		"touch " + a.startedPath + "; while [ ! -f " + a.releasePath + " ]; do sleep 0.02; done",
	}
}

func (a coldstartAgent) started() bool {
	_, err := os.Stat(a.startedPath)
	return err == nil
}

func (a coldstartAgent) waitStarted() bool {
	return coldstartWaitFor(coldstartWaitLimit, a.started)
}

func (a coldstartAgent) letFinish(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(a.releasePath, []byte("go\n"), 0o600); err != nil {
		t.Fatalf("coldstartAgent: release the stub agent: %v", err)
	}
}

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

var coldstartReadyAtOnce = coldstartAdapter{ready: true}

var coldstartNeverReady = coldstartAdapter{ready: false}

func coldstartRegistryFor(t *testing.T, adapter handlercontract.Adapter) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := reg.Register(core.AgentTypeClaudeCode, adapter); err != nil {
		t.Fatalf("coldstartRegistryFor: Register: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) //nolint:errcheck // called for its sealing effect; beadRunOne reads the adapter back through the registry
	return reg
}

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

type coldstartRun struct {
	deps        testRuntime
	env         runloop.RunEnv
	preSelected *workers.Worker
	token       chan struct{}
	ledger      *runplanacqLedger

	// agent is the stub agent this run launches. start uses it to release the
	// agent on the way out, so a failing test cannot abandon a live run.
	agent coldstartAgent

	// atTakeSite closes once the run has built its launch spec, which is the
	// last step before it takes the token. A test that must observe the run
	// PARKED at the take waits on this first, so its grace covers scheduling
	// rather than the whole tunnel and worktree setup.
	atTakeSite chan struct{}

	// bus collects the events the run emits. Kept so a failure can name the
	// reason the run gave rather than only that no agent appeared.
	bus *runplanBus

	// mu guards the two outcome fields below, which the run goroutine writes
	// and a failing assertion on the test goroutine reads.
	mu sync.Mutex
	// returned records that beadRunOne came back, which distinguishes a run
	// still working from one that already gave up.
	returned bool
	// ok is what beadRunOne reported.
	ok bool
}

func (r *coldstartRun) outcome() string {
	r.mu.Lock()
	returned, ok := r.returned, r.ok
	r.mu.Unlock()
	if !returned {
		return "the run had NOT returned, so it is still parked somewhere upstream of the agent spawn"
	}
	emitted := r.bus.seen()
	seen := make([]string, 0, len(emitted))
	for _, ev := range emitted {
		seen = append(seen, string(ev))
	}
	events := strings.Join(seen, ", ")
	if events == "" {
		events = "none"
	}
	reason := string(r.bus.firstPayload(core.EventTypeRunFailed))
	if reason == "" {
		reason = "(no run_failed payload)"
	}
	return fmt.Sprintf("the run ALREADY RETURNED (beadRunOne reported %t) before the agent ever "+
		"started, so this wait could never have succeeded. Events emitted: %s\nrun_failed: %s",
		ok, events, reason)
}

func coldstartAddWorktree(t *testing.T, repoDir, dir string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "--detach", dir, "HEAD")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("coldstartAddWorktree: git worktree add %s: %v\n%s", dir, err, out)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rm := exec.CommandContext(ctx, "git", "worktree", "remove", "--force", dir)
		rm.Dir = repoDir
		if out, err := rm.CombinedOutput(); err != nil {
			t.Logf("coldstartAddWorktree: cleanup %s: %v\n%s", dir, err, out)
		}
	})
}

func coldstartPrepare(t *testing.T, opt coldstartOptions) *coldstartRun {
	t.Helper()

	projectDir := remotefixRepo(t)

	worktreeDir := filepath.Join(t.TempDir(), "worktree")
	coldstartAddWorktree(t, projectDir, worktreeDir)
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		return worktreeDir, func() {}, nil
	}

	remotefixSSHShimAnsweringHEAD(t, 0, worktreeDir)
	remotefixTunnelSeam(t, remotefixIdleTunnel)

	var workerReg *workers.Registry
	var preSelected *workers.Worker
	if opt.remote {
		workerReg, preSelected = remotefixReserveWorker(t)
	}

	atTakeSite := make(chan struct{})
	var takeSiteOnce bool
	launchSpecBuilder := func(ctx context.Context, lc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error) {
		spec, artifacts, err := claude.BuildLaunchSpec(ctx, lc)
		if !takeSiteOnce {
			takeSiteOnce = true
			close(atTakeSite)
		}
		return spec, artifacts, err
	}

	ledger := &runplanacqLedger{}
	bus := &runplanBus{}
	params := remotefixParams(t, projectDir)
	params.BrAdapter = ledger
	params.Bus = bus
	params.HandlerArgs = opt.agent.handlerArgs()
	params.AdapterRegistry2 = coldstartRegistryFor(t, opt.adapter)
	params.WorkerRegistry = workerReg
	params.WorktreeFactory = worktreeFactory
	params.LaunchSpecBuilder = launchSpecBuilder
	params.AgentSpawnSem = opt.token
	params.AgentReadyTimeout = opt.readyTimeout
	params.RemoteAgentReadyTimeout = opt.readyTimeout
	deps := ExportedTestRuntime(params)

	env := remotefixRunEnv(deps, remotefixBead("hk-coldstart-probe", "cold-start token probe"))

	return &coldstartRun{
		deps:        deps,
		env:         env,
		preSelected: preSelected,
		token:       opt.token,
		ledger:      ledger,
		agent:       opt.agent,
		atTakeSite:  atTakeSite,
		bus:         bus,
	}
}

func (r *coldstartRun) start(t *testing.T) <-chan struct{} {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*coldstartWaitLimit)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		ok := runBeadOneTest(ctx, r.deps, r.env, "", r.preSelected, false)
		r.mu.Lock()
		r.returned, r.ok = true, ok
		r.mu.Unlock()
	}()
	t.Cleanup(func() {
		if wErr := os.WriteFile(r.agent.releasePath, []byte("go\n"), 0o600); wErr != nil {
			t.Logf("coldstart cleanup: release marker %s: %v", r.agent.releasePath, wErr)
		}
		select {
		case <-done:
		case <-time.After(coldstartWaitLimit):
			t.Errorf("the run did not return within %v of releasing the stub agent, so it is still "+
				"live after this test ended and will be reported as a leak somewhere else",
				coldstartWaitLimit)
		}
	})
	return done
}

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

func coldstartProductionToken(t *testing.T) chan struct{} {
	t.Helper()
	bus := eventbus.NewBusImpl()
	deps, err := newTestRuntime(t.Context(),
		Config{ProjectDir: t.TempDir(), HandlerBinary: "/bin/sh", BrPath: "/bin/true"},
		bus, "", handlercontract.NewAdapterRegistry(), nil)
	if err != nil {
		t.Fatalf("coldstartProductionToken: newTestRuntime: %v", err)
	}
	if deps.handles.AgentSpawnSem == nil {
		t.Fatal("the production constructor left the cold-start channel nil, so every remote run " +
			"is ungated and the capacity below bounds nothing")
	}
	return deps.handles.AgentSpawnSem
}

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

func coldstartFreeSlot(token chan struct{}) bool {
	select {
	case token <- struct{}{}:
		<-token
		return true
	default:
		return false
	}
}

// TestColdStartToken_ARemoteRunTakesATokenBeforeItStartsItsAgent drives a remote
// run against a cold-start channel that is already full and asserts the run
// starts no agent until a token comes free.
//
// The claim is about ORDER, so the test is built as one: with no token the agent
// must not exist, and the same run must produce that agent as soon as a token
// appears. The second half is what makes the first mean something. Without it
// "no agent" would also be true of a fixture that could never launch one.
//
// Mutation: delete the take (the select on AgentSpawnSem in runAgentLaunch). The
// agent then starts while the channel is full and the first assertion goes red.
func TestColdStartToken_ARemoteRunTakesATokenBeforeItStartsItsAgent(t *testing.T) {
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

	<-token
	if !agent.waitStarted() {
		t.Fatalf("the agent never started within %v after a cold-start token came free.\n"+
			"The assertion above — no agent while the channel was full — is only evidence if this "+
			"same fixture does launch one once a token exists.", coldstartWaitLimit)
	}

	agent.letFinish(t)
	coldstartAwait(t, done)
}

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
// Mutation: drop the in.Remote guard on the take. The local run then parks on
// the full channel, no agent appears, and this test fails at the wait.
func TestColdStartToken_ALocalRunTakesNoColdStartToken(t *testing.T) {
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
			"A local run constructs no reverse tunnel and must skip the cold-start gate entirely.\n"+
			"%s",
			coldstartWaitLimit, run.outcome())
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
// Mutation: delete the prompt give-back — the coldStart.Release call that sits
// after the readiness phase in runAgentLaunch — which leaves only the scope's
// close at the end of the launch. The channel then stays full for as long as the
// agent is blocked and this test fails.
func TestColdStartToken_TheTokenComesBackWhenTheColdStartWindowEndsNotWhenTheRunEnds(t *testing.T) {
	agent := coldstartNewAgent(t)
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

// TestColdStartToken_TheTokenComesBackExactlyOnceAcrossBothGiveBackPaths drives
// one run on which BOTH give-back paths execute and asserts the channel is left
// exactly as it was found.
//
// Both paths do run on this run. The prompt give-back fires when readiness
// settles, which the test observes directly. The launch scope's close then fires
// when the caller runs the deferred launch cleanup, because a defer has no
// condition. Only the lease between them stops the second from taking a token
// that belongs to somebody else: a lease runs its give-back at most once, and
// the scope's close finds it already spent.
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
	agent := coldstartNewAgent(t)
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

// TestColdStartToken_TheTokenComesBackWhenReadinessTimesOut asserts the deferred
// give-back covers the path where the prompt one never runs.
//
// The two paths are not redundant. When the readiness phase fails on its
// deadline, the launch returns before the prompt give-back is reached, so the
// scope's close is the ONLY thing that returns the token. A leak here is silent
// and permanent: after three timed-out remote runs the daemon's remote dispatch
// stops for the rest of its life.
//
// The test also watches the channel while the run is inside the readiness window
// and requires that the token was really held. Without that, "the count is back
// to one" would also be true of a run that never took a token at all.
//
// Mutation: hold the token on a lease no scope holds, so only the prompt
// give-back is left. The timed-out run then keeps its token and the final count
// reads 2.
func TestColdStartToken_TheTokenComesBackWhenReadinessTimesOut(t *testing.T) {
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
	seenFull, stopWatch := coldstartWatchForFullChannel(token)
	defer stopWatch()
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

	calls := run.ledger.calls()
	if len(calls) != 1 {
		t.Fatalf("ReopenBead call count = %d, want exactly 1\ncalls=%+v", len(calls), calls)
	}
	if !strings.Contains(calls[0].reason, "agent_ready_timeout") {
		t.Errorf("the run ended for reason %q, want one carrying %q — a different reason means this "+
			"test did not reach the readiness-timeout path", calls[0].reason, "agent_ready_timeout")
	}

	agent.letFinish(t)
}

// TestColdStartToken_ThreeRemoteColdStartsMayRunAtOnceAndAFourthWaits reads the
// capacity the production constructor builds and drives a run against both sides
// of it.
//
// The two subtests pin the number from opposite directions. Lower the capacity
// and "two out" is already full, so the admitted case fails. Raise it and "three
// out" leaves room, so the parked case fails. Neither subtest alone says
// anything about the number.
//
// The channel comes from newTestRuntime rather than from this file. A fixture
// that made its own channel of three would keep passing after somebody changed
// the production capacity, which is the only thing this test exists to notice.
//
// Mutation: change the capacity in newTestRuntime from 3 to 2, and again to 4.
// BOTH subtests go red both times, because each also checks that the channel
// reaches capacity exactly when a capacity of 3 says it should.
func TestColdStartToken_ThreeRemoteColdStartsMayRunAtOnceAndAFourthWaits(t *testing.T) {
	t.Run("a remote run starts its agent while two cold-start tokens are out", func(t *testing.T) {
		agent := coldstartNewAgent(t)
		token := coldstartProductionToken(t)
		coldstartFill(t, token, 2)

		run := coldstartPrepare(t, coldstartOptions{
			remote:       true,
			token:        token,
			agent:        agent,
			adapter:      coldstartNeverReady,
			readyTimeout: coldstartReadyTimeout,
		})
		seenFull, stopWatch := coldstartWatchForFullChannel(token)
		defer stopWatch()
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

		<-token
		if !agent.waitStarted() {
			t.Fatalf("the agent never started within %v after a token came free", coldstartWaitLimit)
		}
		agent.letFinish(t)
		coldstartAwait(t, done)
	})
}

func coldstartClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func coldstartAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * coldstartWaitLimit):
		t.Fatalf("beadRunOne did not return within %v", 2*coldstartWaitLimit)
	}
}
