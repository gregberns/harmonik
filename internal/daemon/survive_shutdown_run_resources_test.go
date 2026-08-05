package daemon

// survive_shutdown_run_resources_test.go — the three run-level release sites the
// survive-shutdown condition reaches, driven through the real beadRunOne.
//
// The launch-seam sites are in survive_shutdown_gate_test.go. These three live
// in beadRunOne itself:
//
//   - the worktree is kept,
//   - the run registry record is kept, so the next boot can find the session by
//     name,
//   - and the shutdown branch returns WITHOUT reopening the bead, so the bead
//     stays in progress for a later boot to adopt.
//
// When this file was written each of the three had its own spelling of the same
// condition — two of them twenty-odd lines apart and the third six hundred lines
// later. That spread is the reason this file drives the whole function rather
// than testing a predicate: a test that re-states the condition cannot see a
// site that states it differently. Every site has since moved onto one
// runlease disposition and no site spells the condition for itself any more.
// The file still drives the whole function, because that is what makes it see a
// site that goes its own way again.
//
// Both half-conditions are covered separately for each site, because a
// conjunction is exactly where a wrong spelling hides — a site that tested only
// the session kind, or only the stopping daemon, passes the both-facts case and
// fails one of the halves.
//
// Survive is what a run ASKS for. See survive_shutdown_recovery_test.go for
// what it GETS.
//
// # The one mutant nothing here killed, and how it was closed
//
// This section used to say: change `SkipAbortKill` from
// `useIndepSession && ctx.Err() != nil` to plain `useIndepSession` and every
// test in this package still passes. That was true, and it was an equivalent
// mutant, because the abort kill is reachable with a LIVE context only through
// the stall edge in stepDispatchWorking — EvNoChangeTimeout and
// EvHeartbeatStale — which nothing in production feeds and which the segment
// loop cannot reach. The reasoning was recorded so that nobody would read the
// silence as a licence to drop the conjunct.
//
// The mutant is now unwritable, which is the better answer. `SkipAbortKill` and
// `SkipTeardown` are gone. There is no per-site predicate left to weaken: the
// abort kill, the post-wait window kill and the session's give-back all read one
// `runlease.Decide`, and the conjunction lives inside that one function, where
// TestSurvivalNeedsBothAnIndependentSessionAndAStoppingDaemon drives all eight
// input combinations. RSM-037 asked for exactly that — one value decided once
// and read everywhere — and "redundant at this one site" is no longer a
// sentence anyone can say about it.
//
// The stall edge is still the reason the conjunct matters. When the
// reactorization makes that edge reachable, a run whose agent stalls in its own
// session must still be reaped, and the disposition is what says so.
//
// # Why there is no tunnel test here
//
// The reverse tunnel is not gated by the condition, which looks like a leak in
// the same family as the hook session. No run can hold both, so there is nothing
// to test. Three independent things each make that true, and any one of them
// would be enough:
//
//  1. The two arms of beadRunOne's ConfigurePerRunSubstrate closure. A tunnel
//     belongs to a remote run, and the closure's remote arm returns before the
//     line that sets the independent-session flag, which is in the local arm.
//     Note that this is the CLOSURE, not the function: beadRunOne's own remote
//     block returns only on failure, and a healthy remote run does reach the
//     flag's declaration — with the flag left false.
//  2. The workflow driver. All inputs resolve to DOT before beadRunOne runs.
//     Its graph-node launch does not pass this ConfigurePerRunSubstrate hook,
//     so a graph run never sets the independent-session flag by that route.
//  3. The tunnel's own lifetime. It is an exec.CommandContext on the run
//     context, so a stopping daemon ends it whatever any gate says.
//
// The ungated tunnel is a hazard that arrives the day a remote run may keep its
// own session, not a live defect. Anyone who makes that combination possible
// owes this file a test.
//
// Helper prefix: surviveRun.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture
// ─────────────────────────────────────────────────────────────────────────────

// surviveRunAdapter is a tmux server that answers every call and starts
// nothing. onSpawn fires the moment a window or a session is created, which is
// where a test lands a daemon shutdown: the agent has just been started and has
// not yet reported ready.
//
// It deliberately does NOT implement the daemon's sessionCreator interface, so
// a run built on it takes the shared-session path and gets no independent
// session. Its sibling below does.
//
// It also deliberately does NOT embed the package's shared noopTmuxAdapter, and
// its sibling in survive_shutdown_recovery_test.go does. The difference is what
// each fixture asks of tmux. That one asks a single question and a no-op answer
// is honest for the rest. This one drives a real spawn, a real liveness poll and
// a real kill through the production substrate, and what the adapter answers
// decides what these tests observe. Spelling every method out means a method
// added to tmux.Adapter breaks the build here instead of quietly arriving as a
// no-op that changes the observation and fails nothing.
type surviveRunAdapter struct {
	mu sync.Mutex
	// onSpawn runs inside the spawn call, before it returns.
	onSpawn func()
	// stopDaemon runs on the stopAtCall-th pane-PID resolution. The daemon
	// resolves a pane PID at two points a test can aim at: call 1 is the spawn
	// itself, before the launch waits for the agent to report ready, and call 2
	// is the first liveness poll inside the completion wait. A shutdown landing
	// on the first aborts the dispatch; one landing on the second reaches a run
	// that is already working, which is the only ordering under which the
	// post-wait window kill is the FIRST thing that would end the session — and
	// so the only one under which the teardown gate can be observed at all.
	// Zero never stops the daemon.
	stopDaemon  func()
	stopAtCall  int
	panePIDCall int
	// sessionsCreated names each independent session the adapter was asked for.
	sessionsCreated []string
	windowsCreated  int

	// killsOnALiveCtx and killsOnACancelledCtx count the kill-window calls that
	// reached tmux, split by the context they arrived on. See the file header
	// for why the split names the site.
	killsOnALiveCtx      int
	killsOnACancelledCtx int
}

var _ tmux.Adapter = (*surviveRunAdapter)(nil)

func (a *surviveRunAdapter) ProbeTmux(context.Context) error                { return nil }
func (a *surviveRunAdapter) ListSessions(context.Context) ([]string, error) { return nil, nil }

func (a *surviveRunAdapter) ListWindows(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *surviveRunAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.windowsCreated++
	hook := a.onSpawn
	a.mu.Unlock()
	if hook != nil {
		hook()
	}
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}

func (a *surviveRunAdapter) KillWindow(ctx context.Context, _ tmux.WindowHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx != nil && ctx.Err() != nil {
		a.killsOnACancelledCtx++
		return nil
	}
	a.killsOnALiveCtx++
	return nil
}

// killsOnALiveContext counts the kill-window calls that reached tmux on a live
// context. Every site the survive gate covers kills on context.Background(), so
// on a run that is meant to survive this must be zero.
//
// The converse does NOT hold in general: the ready-timeout kill is ungated and
// also arrives on a live context. Each caller below says which ordering it
// drives and therefore which sites could have produced the kill it counts.
func (a *surviveRunAdapter) killsOnALiveContext() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.killsOnALiveCtx
}

// killsReachingTmux counts every kill-window call, however it arrived.
func (a *surviveRunAdapter) killsReachingTmux() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.killsOnALiveCtx + a.killsOnACancelledCtx
}

func (a *surviveRunAdapter) WindowPanePID(context.Context, tmux.WindowHandle) (int, error) {
	a.mu.Lock()
	a.panePIDCall++
	var stop func()
	if a.stopDaemon != nil && a.panePIDCall == a.stopAtCall {
		stop = a.stopDaemon
	}
	a.mu.Unlock()
	if stop != nil {
		stop()
	}
	return 0, tmux.ErrNoSession
}

func (a *surviveRunAdapter) WindowPaneID(context.Context, tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *surviveRunAdapter) KillSession(context.Context, string) error             { return nil }
func (a *surviveRunAdapter) LoadBuffer(context.Context, string, []byte) error      { return nil }
func (a *surviveRunAdapter) PasteBuffer(context.Context, string, string) error     { return nil }
func (a *surviveRunAdapter) SendKeysEnter(context.Context, string) error           { return nil }
func (a *surviveRunAdapter) SendKeysQuit(context.Context, string) error            { return nil }
func (a *surviveRunAdapter) SendKeysLiteral(context.Context, string, string) error { return nil }

func (a *surviveRunAdapter) WriteToPane(context.Context, string, string, []byte) error {
	return nil
}

// windows counts the windows opened in the daemon's own shared tmux session.
func (a *surviveRunAdapter) windows() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.windowsCreated
}

func (a *surviveRunAdapter) sessions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, len(a.sessionsCreated))
	copy(out, a.sessionsCreated)
	return out
}

// surviveRunSessionAdapter adds NewSessionIn, which is the whole difference
// between a run that gets a tmux session of its own and one that shares the
// daemon's. The daemon looks for exactly this method to decide.
type surviveRunSessionAdapter struct {
	surviveRunAdapter
}

func (a *surviveRunSessionAdapter) NewSessionIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	a.sessionsCreated = append(a.sessionsCreated, params.Session)
	hook := a.onSpawn
	a.mu.Unlock()
	if hook != nil {
		hook()
	}
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}

// surviveRunLedger records the bead writes a run makes on its way out. Only
// ReopenBead and CloseBead say anything about the bead's fate; a run that
// leaves the bead in progress calls neither.
type surviveRunLedger struct {
	mu      sync.Mutex
	reopens []string
	closes  int
}

func (l *surviveRunLedger) Ready(context.Context) ([]core.BeadRecord, error) { return nil, nil }

func (l *surviveRunLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id}, nil
}

func (l *surviveRunLedger) ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error {
	return nil
}

func (l *surviveRunLedger) CloseBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID, bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closes++
	return nil
}

func (l *surviveRunLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reopens = append(l.reopens, reason)
	return nil
}

// beadSettled reports whether the run decided the bead's fate rather than
// leaving it in progress.
func (l *surviveRunLedger) beadSettled() (reopens []string, closes int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.reopens))
	copy(out, l.reopens)
	return out, l.closes
}

// surviveRunOutcome is what one drive of beadRunOne left behind.
type surviveRunOutcome struct {
	projectDir string
	runID      string
	ledger     *surviveRunLedger
	adapter    *surviveRunAdapter

	// worktreePath is the directory the run was given.
	worktreePath string
	// worktreeCleanups counts how many times the run called the cleanup it was
	// handed with the worktree.
	worktreeCleanups int
	// recordAtSpawn is the run registry record as it stood at the instant the
	// agent's tmux session was created, which is how this file states "written
	// before the spawn" as an observation rather than as a reading of the code.
	recordAtSpawn    runpkg.Record
	recordAtSpawnErr error

	// startedMode is the workflow_mode the run stamped on its own run_started
	// event. It is how a test states "this run took the graph path" as something
	// the run said about itself, rather than as a restatement of the fixture's
	// own configuration.
	startedMode string
}

// worktreeSurvived reports whether the worktree directory is still on disk.
func (o *surviveRunOutcome) worktreeSurvived() bool {
	_, err := os.Stat(o.worktreePath)
	return err == nil
}

// runRecordSurvived reports whether the registry record is still on disk, which
// is the only way a later daemon boot learns this run existed.
func (o *surviveRunOutcome) runRecordSurvived() bool {
	_, err := runpkg.Load(o.projectDir, o.runID)
	return err == nil
}

// surviveRunRepo initialises a git repository with one commit on main.
func surviveRunRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("surviveRunRepo: git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "test@harmonik.local")
	run("config", "user.name", "Harmonik Test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("survive-shutdown fixture\n"), 0o600); err != nil {
		t.Fatalf("surviveRunRepo: WriteFile: %v", err)
	}
	run("add", "README")
	run("commit", "-m", "Initial commit")
	return dir
}

// surviveRunProbeBead is the bead every drive of this fixture works. It is a
// const so that a test in another file can ask a reader about THIS run's bead
// without re-spelling the literal.
const surviveRunProbeBead = core.BeadID("hk-survive-run-probe")

// surviveRunOpts selects which run this fixture drives.
type surviveRunOpts struct {
	// ownSession decides whether the daemon's substrate can create an
	// independent tmux session, which is the first half of the survive
	// condition. It is chosen by the adapter's shape, exactly as in production.
	ownSession bool

	// stopAtPanePIDCall says which pane-PID resolution cancels the run context,
	// which is the second half of the condition. 1 is the spawn, so the daemon
	// stops while the agent is still coming up and the dispatch aborts. 2 is the
	// first liveness poll inside the completion wait, so the daemon stops on a
	// run that is already working. 0 never stops the daemon.
	stopAtPanePIDCall int

	// agentReportsReady selects the readiness path. True gives the dispatch no
	// adapter for the resolved agent type, which is the daemon's own "skip the
	// ready-wait" posture: the segment feeds a synthetic ready and the run
	// reaches Working, so it ends through the post-wait window kill rather than
	// through the ready-timeout kill.
	//
	// This matters more than it looks. Every other path this fixture can drive
	// kills the session through an UNGATED site first — the ready-timeout kill
	// or the completion wait — and the session's kill is once-guarded, so the
	// teardown gate has nothing left to prevent and cannot be observed at all.
	// The working path is the only one where the first kill on the session is
	// the one the teardown gate covers.
	agentReportsReady bool

	// piRun resolves the run's harness to Pi, which is the harness that writes
	// its captured output INSIDE the worktree. It is the second reason a run
	// keeps a worktree, and it is independent of the survive condition: the
	// agent exits without committing, so the run fails, and a failed run's
	// capture is the only record of why.
	piRun bool

	// realWorktree hands the run a real git worktree instead of a plain
	// directory. Every graph run resolves HEAD in its worktree before it reaches
	// an agentic node, and a plain directory cannot answer, so a run that must
	// reach a real agent launch needs this. piRun implies it.
	realWorktree bool

	// graphMode explicitly selects DOT instead of the historical default input.
	// Both choices execute DOT. The explicit form keeps direct graph selection covered.
	graphMode bool

	// seedProject runs once the project directory exists and before the run
	// starts. It is how a test puts something in the project directory that the
	// run will meet — see run_terminal_writer_lifetime_test.go, which seeds a
	// named pipe so the run's own background writer can be held still.
	seedProject func(projectDir string)
}

// surviveRunOneAgenticNodeGraph is the smallest graph that reaches a real agent
// launch: start → implement (agentic) → a terminal. The Pi agent this fixture
// launches exits without committing, so `implement` fails and the cascade lands
// on close-needs-attention, which is the failed run the retention is for.
const surviveRunOneAgenticNodeGraph = `digraph "survive-run-evidence" {
    schema_version="1";
    version="1.0";
    workflow_id="survive-run-evidence";
    start_node="start";
    terminal_node_ids="close,close-needs-attention";

    start [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    implement [type="agentic", agent_type="implementer", handler_ref="pi-implementer", idempotency_class="non-idempotent"];
    close [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];
    "close-needs-attention" [type="non-agentic", handler_ref="noop", idempotency_class="idempotent"];

    start -> implement;
    implement -> close [condition="outcome.status == 'SUCCESS'"];
    implement -> "close-needs-attention";
}
`

// surviveRunDrive runs one bead through the real beadRunOne, on the shutdown
// ordering that aborts the dispatch. It is the two-fact spelling of
// surviveRunDriveWith for the tests that predate the options.
func surviveRunDrive(t *testing.T, ownSession, daemonStops bool) *surviveRunOutcome {
	t.Helper()
	opts := surviveRunOpts{ownSession: ownSession}
	if daemonStops {
		opts.stopAtPanePIDCall = 1
	}
	return surviveRunDriveWith(t, opts)
}

// surviveRunDriveWith runs one bead through the real beadRunOne under opts.
func surviveRunDriveWith(t *testing.T, opts surviveRunOpts) *surviveRunOutcome {
	t.Helper()

	projectDir := surviveRunRepo(t)
	if opts.seedProject != nil {
		opts.seedProject(projectDir)
	}
	runID := core.RunID(uuid.New())
	out := &surviveRunOutcome{
		projectDir: projectDir,
		runID:      runID.String(),
		ledger:     &surviveRunLedger{},
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var mu sync.Mutex
	onSpawn := func() {
		mu.Lock()
		defer mu.Unlock()
		// Read the registry from inside the spawn call. If the record is not
		// there yet, no later boot could find this session by name.
		out.recordAtSpawn, out.recordAtSpawnErr = runpkg.Load(projectDir, runID.String())
	}
	var adapter *surviveRunAdapter
	var tmuxAdapter tmux.Adapter
	if opts.ownSession {
		sa := &surviveRunSessionAdapter{}
		adapter, tmuxAdapter = &sa.surviveRunAdapter, sa
	} else {
		a := &surviveRunAdapter{}
		adapter, tmuxAdapter = a, a
	}
	adapter.onSpawn = onSpawn
	adapter.stopDaemon = cancel
	adapter.stopAtCall = opts.stopAtPanePIDCall
	out.adapter = adapter

	sub := NewTmuxSubstrate(tmuxAdapter, "survive-run-default",
		WithCrewProjectHash(core.ProjectHash("abcdef012345")))

	// A worktree the run is handed and may or may not give back. The directory
	// is real, so "kept" and "removed" are observable on disk rather than only
	// in a counter.
	wtPath := filepath.Join(t.TempDir(), "run-worktree")
	if opts.piRun || opts.realWorktree {
		// A real git worktree, for either of two reasons. A Pi run has to FAIL in
		// the ordinary way — the agent produced no commit — and the no-commit
		// guard asks git, so a bare temp dir ends the run as a success and the
		// retention branch is never reached. Any run that must reach a real agent
		// launch needs one too, because a graph run resolves HEAD in its worktree
		// on the way to an agentic node and a plain directory cannot answer.
		surviveRunGitWorktree(t, projectDir, wtPath)
	} else if err := os.MkdirAll(wtPath, 0o755); err != nil { //nolint:gosec // test fixture dir
		t.Fatalf("surviveRun: create worktree fixture: %v", err)
	}
	out.worktreePath = wtPath
	worktreeFactory := func(context.Context, string, string, string) (string, func(), error) {
		return wtPath, func() {
			mu.Lock()
			out.worktreeCleanups++
			mu.Unlock()
			if err := os.RemoveAll(wtPath); err != nil {
				t.Errorf("surviveRun: worktree cleanup: %v", err)
			}
		}, nil
	}

	// An empty registry gives the dispatch no adapter for the resolved agent
	// type, which is the daemon's own skip-the-ready-wait posture. A populated
	// one holds the run in its ready wait until the timeout or the abort.
	registry := surviveRunSealedRegistry(t)
	if opts.agentReportsReady || opts.piRun {
		registry = surviveRunEmptySealedRegistry()
	}

	// A synchronous consumer, not an observer: the mode is read after the run
	// returns, and an observer's fan-out may not have landed by then.
	bus := eventbus.NewBusImpl()
	if _, subErr := bus.Subscribe(core.Subscription{
		ConsumerID:    "survive-run-started-mode",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  core.EventPattern{Types: map[core.EventType]struct{}{core.EventTypeRunStarted: {}}},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			// An unreadable payload leaves startedMode empty, and the test that
			// reads it fails on the empty value with its own message. Returning the
			// error here would instead fail the EMIT, which changes the run under
			// test to report a problem in the reader.
			var pl core.RunStartedPayload
			if uErr := json.Unmarshal(evt.Payload, &pl); uErr == nil {
				mu.Lock()
				out.startedMode = string(pl.WorkflowMode)
				mu.Unlock()
			}
			return nil
		},
	}); subErr != nil {
		t.Fatalf("surviveRun: subscribe the run_started mode reader: %v", subErr)
	}

	params := TestRuntimeParams{
		BrAdapter:        out.ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		IntentLogDir:     t.TempDir(),
		MaxConcurrent:    1,
		AdapterRegistry2: registry,
		Substrate:        sub,
		WorktreeFactory:  worktreeFactory,
		TargetBranch:     "main",
		// Short enough that the daemon-keeps-running case reaches its ready
		// timeout quickly; the shutdown cases abort long before it.
		AgentReadyTimeout: 500 * time.Millisecond,
	}
	if opts.piRun {
		params.LaunchSpecBuilder = ExportedPiProcessExitLaunchSpecBuilder(surviveRunPiScript(t))
		harnesses, err := ExportedNewHarnessRegistry()
		if err != nil {
			t.Fatalf("surviveRun: build the harness registry: %v", err)
		}
		params.HarnessRegistry = harnesses
	}
	if opts.graphMode {
		// The graph is loaded from <projectDir>/workflow.dot, which is the tier-2
		// source a real project uses. Seeding a file here also keeps the run off the
		// embedded standard-bead.dot, whose reviewer and commit-gate nodes need a
		// verdict and a toolchain this fixture cannot supply.
		dotPath := filepath.Join(projectDir, "workflow.dot")
		if err := os.WriteFile(dotPath, []byte(surviveRunOneAgenticNodeGraph), 0o600); err != nil {
			t.Fatalf("surviveRun: seed workflow.dot: %v", err)
		}
		params.WorkflowModeDefault = core.WorkflowModeDot
	}

	deps := ExportedTestRuntime(params)

	bead := core.BeadRecord{
		BeadID:   surviveRunProbeBead,
		Title:    "survive-shutdown probe",
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
	}
	env := deps.runEnv(runID, bead, "", "", core.AgentType(""))
	runBeadOneTest(ctx, deps, env, "", nil, false)

	mu.Lock()
	defer mu.Unlock()
	return out
}

// surviveRunGitWorktree adds a real git worktree of repo at path.
func surviveRunGitWorktree(t *testing.T, repo, path string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b",
		"survive-run-fixture", path, "HEAD")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("surviveRunGitWorktree: git worktree add: %v\n%s", err, out)
	}
}

// surviveRunPiScript writes the agent a Pi run launches. It prints one line and
// exits cleanly WITHOUT committing, which is the fast-fail this retention exists
// for: the run produced no commit, so it failed, and the printed line is the
// only thing that says what it did.
func surviveRunPiScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pi-agent.sh")
	const script = "#!/bin/sh\necho 'pi agent: cannot reach the model endpoint'\nexit 3\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture script must be executable
		t.Fatalf("surviveRunPiScript: WriteFile: %v", err)
	}
	return path
}

// surviveRunEmptySealedRegistry seals a registry with no adapters in it, so
// ForAgent always fails and the dispatch takes the daemon's skip-the-ready-wait
// path. The package ships the same helper, but in package daemon_test, and this
// file is white-box.
func surviveRunEmptySealedRegistry() *handlercontract.AdapterRegistry {
	reg := handlercontract.NewAdapterRegistry()
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) //nolint:errcheck // called for its sealing effect; the miss is the point
	return reg
}

// surviveRunSealedRegistry registers the real claude adapter and seals it.
func surviveRunSealedRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("surviveRun: register claude adapter: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) //nolint:errcheck // called for its sealing effect
	return reg
}

// ─────────────────────────────────────────────────────────────────────────────
// The other reason a worktree is kept: it holds the only record of a failure
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_AFailedPiRunKeepsTheWorktreeItsCapturedOutputIsIn asserts
// that a run whose captured agent output lives inside its worktree keeps that
// worktree when it fails.
//
// This is the SECOND reason a run keeps a worktree and it has nothing to do with
// surviving a shutdown. The daemon is up and the run has ended, which is exactly
// the case the test above requires to give everything back. The difference is
// that a Pi agent writes its stdout under the worktree, so removing it destroys
// the only record of why the run failed (hk-j6wm7).
//
// The test asserts the capture is really there before it asserts the worktree
// survived. Without that, "the directory still exists" would also be true of a
// run that captured nothing, and the retention would be defended without anyone
// checking it protects something.
//
// The test above is the nearest thing to a control: a run that also ends while
// the daemon is up, and whose worktree goes away. It is not a one-variable
// control, and saying so matters more than the symmetry. That run has a session
// of its own, gets a plain directory rather than a git worktree, and holds the
// claude adapter. Only the last of those could plausibly matter here, and the
// mutations below are what actually establishes the claim.
//
// Four mutations were run. Each one deletes the capture and turns this test red.
// Make RetainEvidence release the worktree in internal/runlease. Report the exit
// with EvidenceWorthKeeping hard-coded false. Delete the SetCapturedAgentOutput
// call in runAgentLaunch. Drop the handle registration from runBeadOneTest — the
// mark then lands nowhere, which is what this fixture did before this test's
// graph-mode sibling was written.
//
// SCOPE: this drives the historical default input, which resolves to standard DOT.
// TestSurviveShutdown_AFailedGraphModePiRunKeepsTheWorktreeItsCapturedOutputIsIn
// drives direct DOT selection. The two tests ensure the launch records captured
// evidence for both the default and direct graph inputs.
func TestSurviveShutdown_AFailedPiRunKeepsTheWorktreeItsCapturedOutputIsIn(t *testing.T) {
	t.Parallel()

	out := surviveRunDriveWith(t, surviveRunOpts{piRun: true})

	capture := filepath.Join(out.worktreePath, ".harmonik", "pi-agent", "pi-stdout.log")
	if _, err := os.Stat(capture); err != nil {
		// Two very different failures reach here, so name which one it is. A
		// removed worktree is the behaviour under test breaking; a missing capture
		// under a live worktree is the fixture no longer reaching the Pi path.
		if !out.worktreeSurvived() {
			t.Fatalf("the worktree at %s was removed, and the captured Pi output went with it.\n"+
				"A failed Pi run's capture is inside the worktree and nowhere else, so removing it "+
				"deletes the only record of why the run failed.", out.worktreePath)
		}
		t.Fatalf("the run captured no Pi output at %s: %v\n"+
			"This run must reach the Pi launch path and write its stdout inside the worktree. "+
			"Without a capture, a surviving worktree below would protect nothing and this test "+
			"would pass whatever the run decided.", capture, err)
	}
	if reopens, _ := out.ledger.beadSettled(); len(reopens) == 0 {
		t.Fatal("the run did not fail.\n" +
			"The retention is for FAILED runs only. A successful run's worktree is removed, so a " +
			"fixture whose run succeeded would measure the wrong branch.")
	}

	if out.worktreeCleanups != 0 {
		t.Errorf("the worktree cleanup ran %d times.\n"+
			"A failed Pi run's captured output is inside the worktree and nowhere else. Removing it "+
			"deletes the only evidence of why the run failed.", out.worktreeCleanups)
	}
	if !out.worktreeSurvived() {
		t.Errorf("the worktree at %s was removed, and the captured Pi output went with it",
			out.worktreePath)
	}
	if out.runRecordSurvived() {
		t.Error("the run registry record is still on disk.\n" +
			"Keeping the EVIDENCE does not mean keeping the run. The daemon is up and this run has " +
			"ended, so a record left behind makes the next boot adopt a run that no longer exists.")
	}
}

// TestSurviveShutdown_AFailedGraphModePiRunKeepsTheWorktreeItsCapturedOutputIsIn
// is the same claim as the test above, for a run driven through the DOT cascade.
//
// The cascade launches the Pi agent and writes its capture inside the worktree.
// Direct DOT selection must retain that evidence on failure.
//
// The test proves the run really took the graph path before it asserts anything
// about the worktree. Without that check a fixture that silently used the
// default input would pass this test while defending nothing.
//
// Four mutations were run. Each one turns this test red. Delete the
// SetCapturedAgentOutput call in runAgentLaunch. Report the exit with
// EvidenceWorthKeeping hard-coded false. Make RetainEvidence release the
// worktree. The launch records the captured output on the run handle, so both
// selection forms retain the same evidence.
//
// A fifth turns it red too. Drop the handle registration from runBeadOneTest.
//
// A sixth pins the mode guard itself. Drop the WorkflowModeDefault assignment in
// the fixture and this test fails on the MODE check rather than passing as a
// second copy of the single-mode test.
func TestSurviveShutdown_AFailedGraphModePiRunKeepsTheWorktreeItsCapturedOutputIsIn(t *testing.T) {
	t.Parallel()

	out := surviveRunDriveWith(t, surviveRunOpts{piRun: true, graphMode: true})

	if out.startedMode != string(core.WorkflowModeDot) {
		t.Fatalf("the run started in workflow mode %q, want %q.\n"+
			"This test only means something if the run used direct DOT selection. A fixture that used "+
			"the default input would re-test the case above.",
			out.startedMode, core.WorkflowModeDot)
	}

	capture := filepath.Join(out.worktreePath, ".harmonik", "pi-agent", "pi-stdout.log")
	if _, err := os.Stat(capture); err != nil {
		// Same two very different failures as the single-mode test, named apart for
		// the same reason.
		if !out.worktreeSurvived() {
			t.Fatalf("the worktree at %s was removed, and the captured Pi output went with it.\n"+
				"A graph node's capture is inside the run's worktree and nowhere else, so removing "+
				"it deletes the only record of why the run failed.", out.worktreePath)
		}
		t.Fatalf("the graph run captured no Pi output at %s: %v\n"+
			"The cascade must reach the agentic node's Pi launch and write its stdout inside the "+
			"worktree. Without a capture, a surviving worktree below would protect nothing.", capture, err)
	}
	if reopens, _ := out.ledger.beadSettled(); len(reopens) == 0 {
		t.Fatal("the run did not fail.\n" +
			"The retention is for FAILED runs only. A successful run's worktree is removed, so a " +
			"fixture whose run succeeded would measure the wrong branch.")
	}

	if out.worktreeCleanups != 0 {
		t.Errorf("the worktree cleanup ran %d times.\n"+
			"A failed graph-mode Pi run's captured output is inside the worktree and nowhere else.",
			out.worktreeCleanups)
	}
	if !out.worktreeSurvived() {
		t.Errorf("the worktree at %s was removed, and the captured Pi output went with it",
			out.worktreePath)
	}
}
