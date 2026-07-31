package daemon

// survive_shutdown_run_resources_test.go — the three run-level release sites the
// survive-shutdown condition reaches, driven through the real beadRunOne.
//
// The launch-seam sites are in survive_shutdown_gate_test.go. These three live
// in beadRunOne itself, each with its own spelling of the same condition:
//
//   - the worktree cleanup defer keeps the worktree,
//   - the run registry defer keeps the record, so the next boot can find the
//     session by name,
//   - and the shutdown branch returns WITHOUT reopening the bead, so the bead
//     stays in progress for a later boot to adopt.
//
// One condition, three spellings, two of them twenty-odd lines apart and the
// third six hundred lines later. That spread is the reason this file drives the
// whole function rather than testing a predicate: a test that re-states the
// condition cannot see a site that states it differently.
//
// Both half-conditions are covered separately for each site, because a
// conjunction is exactly where a wrong spelling hides — a site that tested only
// the session kind, or only the stopping daemon, passes the both-facts case and
// fails one of the halves.
//
// Survive is what a run ASKS for. See survive_shutdown_recovery_test.go for
// what it GETS.
//
// # The one mutant nothing here kills, and why it is still not redundant
//
// Change SkipAbortKill from `useIndepSession && ctx.Err() != nil` to plain
// `useIndepSession` and every test in this package still passes. That is an
// equivalent mutant TODAY, so there is no behaviour to defend and no test is
// invented for it. Do not read that as the conjunct being redundant. It is not,
// for two separate reasons, and both matter at the moment this predicate moves
// onto a runlease disposition.
//
// First, the abort kill is reachable with a LIVE context. KillAbort is the
// non-ReadyTimeout arm of ActKillAgent, and ActKillAgent has three emitters, not
// one. The third is the stall edge in stepDispatchWorking — EvNoChangeTimeout
// and EvHeartbeatStale — which by definition fires while the context is live. It
// is unreachable now for two reasons that are both scheduled to change: nothing
// in production feeds those two events, and the segment loop halts at
// DispatchWorking, the only phase where that edge exists. When the
// reactorization lands, dropping the conjunct silently stops reaping the session
// of a stalled independent run.
//
// Second, and this is the stronger reason: the value is SHARED. RSM-037 requires
// one disposition decided once for the run and read at every release site. The
// sibling consumer SkipTeardown demonstrably needs the conjunct — mutate it
// alone and a test here fails. "Redundant at this one site" is a claim about a
// per-site predicate, which is the exact shape RSM-037 exists to forbid, so it
// cannot license dropping the conjunct from the value both sites read.
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
//  2. The workflow mode. A graph run returns from the mode switch before the
//     single-mode launch, and the single-mode launch is the only caller that
//     passes ConfigurePerRunSubstrate at all, so a graph run never sets the flag
//     by any route.
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
	"errors"
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
}

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
	if err := os.MkdirAll(wtPath, 0o755); err != nil { //nolint:gosec // test fixture dir
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
	if opts.agentReportsReady {
		registry = surviveRunEmptySealedRegistry()
	}

	deps := ExportedWorkLoopDeps(WorkLoopDepsParams{
		BrAdapter:        out.ledger,
		Bus:              eventbus.NewBusImpl(),
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
	})

	bead := core.BeadRecord{
		BeadID:   core.BeadID("hk-survive-run-probe"),
		Title:    "survive-shutdown probe",
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
	}
	env := deps.runEnv(runID, bead, "", nil, nil, 0, "", "", nil, false, "", core.AgentType(""))
	runBeadOneTest(ctx, deps, env, "", nil, false)

	mu.Lock()
	defer mu.Unlock()
	return out
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
// The record the whole thing hangs on
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_TheRunRecordIsOnDiskBeforeTheAgentSessionIsCreated is the
// precondition for every other claim in this file.
//
// A run that means to outlive the daemon is discoverable only by name: the next
// boot lists the registry, reads SessionName and asks tmux about it. If the
// record were written after the spawn, a daemon killed in between would leave a
// live tmux session that nothing on disk names — untracked, unadoptable, and
// swept as an orphan with no record of what it was.
//
// The observation is taken from inside the spawn call itself, so it states the
// ordering as a fact about the run rather than as a reading of the source.
func TestSurviveShutdown_TheRunRecordIsOnDiskBeforeTheAgentSessionIsCreated(t *testing.T) {
	t.Parallel()

	out := surviveRunDrive(t, true, false)

	if len(out.adapter.sessions()) == 0 {
		t.Fatal("no independent session was created, so nothing observed the ordering")
	}
	if out.recordAtSpawnErr != nil {
		t.Fatalf("the run registry held no record when the agent's session was created: %v.\n"+
			"A daemon killed between the spawn and the write leaves a live session that nothing on disk names — the next boot cannot adopt it and cannot even say what it was.", out.recordAtSpawnErr)
	}
	if out.recordAtSpawn.SessionName == "" {
		t.Error("the record was written without a session name.\n" +
			"Both adoption passes match on that string. A record without it is adopted as dead however healthy the session is.")
	}
	if got, want := out.recordAtSpawn.SessionName, out.adapter.sessions()[0]; got != want {
		t.Errorf("record SessionName = %q, but the session created is %q.\n"+
			"The two must be the same string, or the next boot asks tmux about a session that does not exist and reaps a live run.", got, want)
	}
	if out.recordAtSpawn.RunID != out.runID {
		t.Errorf("record RunID = %q, want %q", out.recordAtSpawn.RunID, out.runID)
	}
	if out.recordAtSpawn.BeadID != "hk-survive-run-probe" {
		t.Errorf("record BeadID = %q, want the bead this run is working — without it the adoption pass has no bead to reset",
			out.recordAtSpawn.BeadID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Both facts hold — the run keeps what a later boot needs
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_AStoppingDaemonLeavesAnOwnSessionRunItsWorktreeRecordAndBead
// drives the case the whole gate exists for. The agent has a tmux session of its
// own and the daemon is stopping, so all three run-level sites must decline:
//
//   - the worktree stays, because the agent is still working in it,
//   - the record stays, because it is how the next boot finds the session,
//   - and the bead stays in progress, because an agent is still on it.
//
// Each of those is a claim of the form "X did not happen", which is free in a
// fixture where nothing happens. So the test first proves the machinery ran:
// the session was created, and the record existed by then. A run that refused
// before the launch would fail those checks rather than sail past them.
func TestSurviveShutdown_AStoppingDaemonLeavesAnOwnSessionRunItsWorktreeRecordAndBead(t *testing.T) {
	t.Parallel()

	out := surviveRunDrive(t, true, true)

	if len(out.adapter.sessions()) == 0 {
		t.Fatal("no independent session was created, so this run never reached the sites under test")
	}
	if out.recordAtSpawnErr != nil {
		t.Fatalf("no run record existed at spawn time: %v — the run did not take the independent-session path", out.recordAtSpawnErr)
	}

	if got := out.adapter.killsOnALiveContext(); got != 0 {
		t.Errorf("%d kill(s) reached tmux on a live context, want 0.\n"+
			"Every site the survive gate covers kills on context.Background(). A kill arriving on a live context here is the abort kill firing on a run whose session is meant to outlive this process.", got)
	}
	if out.worktreeCleanups != 0 {
		t.Errorf("the worktree cleanup ran %d time(s), want 0.\n"+
			"The agent is still working in that directory. Removing it takes the work away from a live agent and races git worktree remove against a running process.", out.worktreeCleanups)
	}
	if !out.worktreeSurvived() {
		t.Errorf("the worktree at %s is gone", out.worktreePath)
	}
	if !out.runRecordSurvived() {
		t.Error("the run registry record was removed.\n" +
			"It is the only thing that names the surviving session. Without it the next boot has nothing to adopt and nothing to reset, and the bead is stuck in progress for ever.")
	}
	reopens, closes := out.ledger.beadSettled()
	if len(reopens) != 0 || closes != 0 {
		t.Errorf("the run settled its bead (reopens=%v closes=%d), want neither.\n"+
			"A surviving run has an agent still working the bead. Reopening it dispatches the same work to a second agent; closing it claims work that has not finished.", reopens, closes)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Half-condition one: an own session, but the daemon keeps running
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_ARunThatEndsWhileTheDaemonRunsGivesBackItsWorktreeAndRecord
// is the half a site that tested only the session kind would get wrong.
//
// The run has a session of its own, so the first half of the condition holds.
// The daemon is not stopping, so the run is ending on its own terms and has
// nothing to survive for. Everything must go back. A site that keyed on the
// session kind alone would leak a worktree and a registry record on every
// independent-session run, and the both-facts test above would stay green
// throughout.
func TestSurviveShutdown_ARunThatEndsWhileTheDaemonRunsGivesBackItsWorktreeAndRecord(t *testing.T) {
	t.Parallel()

	out := surviveRunDrive(t, true, false)

	if len(out.adapter.sessions()) == 0 {
		t.Fatal("no independent session was created, so this is not the half-condition it claims to be")
	}
	if out.recordAtSpawnErr != nil {
		t.Fatalf("no run record existed at spawn time: %v", out.recordAtSpawnErr)
	}

	if out.worktreeCleanups == 0 {
		t.Error("the worktree cleanup never ran.\n" +
			"The daemon is still running and this run has ended. Keeping the worktree leaks a directory for every independent-session run until the age prune reaches it.")
	}
	if out.worktreeSurvived() {
		t.Errorf("the worktree at %s is still on disk", out.worktreePath)
	}
	if out.runRecordSurvived() {
		t.Error("the run registry record is still on disk.\n" +
			"The run is over and its session is gone. A record left behind makes the next boot adopt a run that no longer exists.")
	}
	if reopens, _ := out.ledger.beadSettled(); len(reopens) == 0 {
		t.Error("the bead was left in progress.\n" +
			"Nothing is working it: the daemon is up and this run has ended. Only a surviving run may leave its bead for a later boot.")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Half-condition two: a stopping daemon, but no session of its own
// ─────────────────────────────────────────────────────────────────────────────

// TestSurviveShutdown_AStoppingDaemonGivesBackTheWorktreeOfARunItHosts is the
// other half, and the one a site testing only for a stopping daemon would get
// wrong.
//
// The daemon is stopping, so the second half of the condition holds. The agent
// runs in a window of the daemon's own tmux session, which does not outlive the
// daemon, so there is nothing that COULD survive. The worktree must go back —
// keeping it leaks a directory on every shutdown, and there is no agent left to
// use it.
//
// No registry record is asserted here because none is written: the record is
// what a run in its own session leaves behind, and this run has no such session.
// That absence is itself checked, so the test cannot pass by way of a record
// that quietly appeared.
func TestSurviveShutdown_AStoppingDaemonGivesBackTheWorktreeOfARunItHosts(t *testing.T) {
	t.Parallel()

	out := surviveRunDrive(t, false, true)

	if len(out.adapter.sessions()) != 0 {
		t.Fatalf("an independent session was created (%v), so this is not the half-condition it claims to be", out.adapter.sessions())
	}
	if out.adapter.windows() == 0 {
		t.Fatal("no window was opened in the daemon's shared session, so this run never launched and nothing below is meaningful")
	}
	if !errors.Is(out.recordAtSpawnErr, runpkg.ErrNotFound) {
		t.Errorf("a run registry record existed at spawn time (err=%v), but a run that shares the daemon's session writes none", out.recordAtSpawnErr)
	}

	if out.worktreeCleanups == 0 {
		t.Error("the worktree cleanup never ran.\n" +
			"This agent lives in the daemon's own tmux session and dies with it. Keeping the worktree leaks a directory on every shutdown with nothing left to use it.")
	}
	if out.worktreeSurvived() {
		t.Errorf("the worktree at %s is still on disk", out.worktreePath)
	}
	if out.runRecordSurvived() {
		t.Error("a run registry record is on disk for a run that never had a session of its own")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The teardown gate, where it can actually be seen
// ─────────────────────────────────────────────────────────────────────────────
//
// The tests above end a run through a site that kills the session with no
// reference to the gate: the ready-timeout kill, or the completion wait. The
// session's kill is once-guarded, so by the time the teardown pair runs there is
// nothing left for it to prevent, and a gate that always said "skip" would look
// identical. These three drive a run that REACHES WORKING, where the post-wait
// window kill is the first thing that would end the session and the gate is the
// only thing standing between it and the agent.

// TestSurviveShutdown_AWorkingRunThatEndsWhileTheDaemonRunsHasItsWindowKilled is
// the ordinary end of an own-session run: it reached working, it finished, and
// the daemon is still up. Nothing is meant to outlive anything, so the window
// kill must reach tmux.
//
// This is the one place a teardown gate that forgot the stopping-daemon half of
// the condition shows up. Such a gate skips the kill here, leaks the tmux
// session on every own-session run, and passes every other test in this file.
func TestSurviveShutdown_AWorkingRunThatEndsWhileTheDaemonRunsHasItsWindowKilled(t *testing.T) {
	t.Parallel()

	out := surviveRunDriveWith(t, surviveRunOpts{ownSession: true, agentReportsReady: true})

	if len(out.adapter.sessions()) == 0 {
		t.Fatal("no independent session was created, so this run never reached the site under test")
	}
	if got := out.adapter.killsReachingTmux(); got == 0 {
		t.Error("no kill reached tmux.\n" +
			"This run reached working and ended while the daemon was still up. Its session has nothing to outlive, so the window kill must run — skipping it leaks a tmux session on every own-session run.")
	}
	// The gated site is the one that acted: a run ending this way passes through
	// neither the ready-timeout kill nor the completion wait's cancel kill.
	if got := out.adapter.killsOnALiveContext(); got == 0 {
		t.Error("the kill did not arrive on a live context, so it did not come from the site the gate covers")
	}
}

// TestSurviveShutdown_ADaemonStoppingDuringACompletionWaitNeverTouchesAnOwnSession
// is the survive case as the system can actually deliver it, and the only run
// this fixture can drive where the agent's session is never touched at all.
//
// The daemon stops while the run is already working, so the dispatch never
// aborts and the completion wait was entered on a live context. That leaves the
// post-wait window kill as the first and only thing that would end the session,
// and the gate stops it. Zero kills reach tmux.
//
// A teardown gate replaced by "never skip" kills the agent here. Nothing else in
// this file sees that, because every other ordering has already burned the
// session's one kill somewhere ungated.
func TestSurviveShutdown_ADaemonStoppingDuringACompletionWaitNeverTouchesAnOwnSession(t *testing.T) {
	t.Parallel()

	out := surviveRunDriveWith(t, surviveRunOpts{
		ownSession: true, agentReportsReady: true, stopAtPanePIDCall: 2,
	})

	if len(out.adapter.sessions()) == 0 {
		t.Fatal("no independent session was created, so this run never reached the site under test")
	}
	if out.recordAtSpawnErr != nil {
		t.Fatalf("no run record existed at spawn time: %v — the run did not take the independent-session path", out.recordAtSpawnErr)
	}
	if got := out.adapter.killsReachingTmux(); got != 0 {
		t.Errorf("%d kill(s) reached tmux, want 0.\n"+
			"The agent has a session of its own and the daemon is stopping, so nothing in this process may end it. This is the one ordering where the daemon can genuinely leave the session standing, and the window kill is the only thing that would have taken it.", got)
	}

	// The same disposition reaches the run-level sites, and they agree.
	if out.worktreeCleanups != 0 {
		t.Errorf("the worktree cleanup ran %d time(s), want 0", out.worktreeCleanups)
	}
	if !out.runRecordSurvived() {
		t.Error("the run registry record was removed, so nothing on disk names the session that was just left standing")
	}
}

// TestSurviveShutdown_ADaemonStoppingDuringACompletionWaitStillKillsAWindowItHosts
// is the half-condition at the teardown site.
//
// Same ordering as the test above, and the daemon is stopping just the same, but
// the agent lives in a window of the daemon's own tmux session. Nothing about it
// outlives the daemon, so the window kill must run. Leaving it would orphan a
// live pane inside a session that is about to be torn down.
func TestSurviveShutdown_ADaemonStoppingDuringACompletionWaitStillKillsAWindowItHosts(t *testing.T) {
	t.Parallel()

	out := surviveRunDriveWith(t, surviveRunOpts{agentReportsReady: true, stopAtPanePIDCall: 2})

	if len(out.adapter.sessions()) != 0 {
		t.Fatalf("an independent session was created (%v), so this is not the half-condition it claims to be", out.adapter.sessions())
	}
	if out.adapter.windows() == 0 {
		t.Fatal("no window was opened, so this run never reached the site under test")
	}
	if got := out.adapter.killsReachingTmux(); got == 0 {
		t.Error("no kill reached tmux.\n" +
			"This agent lives in a window of the daemon's own session and dies with it. Skipping the kill orphans a live pane inside a session that is about to be torn down.")
	}
}
