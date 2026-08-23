package daemon

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

func (a *surviveRunAdapter) killsOnALiveContext() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.killsOnALiveCtx
}

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

func (l *surviveRunLedger) beadSettled() (reopens []string, closes int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.reopens))
	copy(out, l.reopens)
	return out, l.closes
}

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
	// agent's tmux session was created, read the way production reads it, which
	// is how this file states "written before the spawn" as an observation
	// rather than as a reading of the code.
	recordAtSpawn    runpkg.Record
	recordAtSpawnErr error
	// rawRecordAtSpawn is the same file at the same instant, decoded without the
	// production reader's identity rules. recordAtSpawnErr says THAT the record
	// is unusable; these bytes say WHICH field made it so, and the two together
	// tell a record that was never written from one written incomplete.
	rawRecordAtSpawn    runpkg.Record
	rawRecordAtSpawnErr error

	// startedMode is the workflow_mode the run stamped on its own run_started
	// event. It is how a test states "this run took the graph path" as something
	// the run said about itself, rather than as a restatement of the fixture's
	// own configuration.
	startedMode string
}

func (o *surviveRunOutcome) worktreeSurvived() bool {
	_, err := os.Stat(o.worktreePath)
	return err == nil
}

func (o *surviveRunOutcome) runRecordSurvived() bool {
	_, err := legacyRunRecord(o.projectDir, o.runID)
	return err == nil
}

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

const surviveRunProbeBead = core.BeadID("hk-survive-run-probe")

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

	// runner is the run's command runner (the Config.Runner seam). It is the
	// fact the LAUNCH reads to decide whether it takes the run's own session, so
	// it is also the fact the record write has to read. Nil leaves the run with
	// no runner, which is the ordinary local path every other test here drives.
	runner tmux.CommandRunner
}

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

func surviveRunDrive(t *testing.T, ownSession, daemonStops bool) *surviveRunOutcome {
	t.Helper()
	opts := surviveRunOpts{ownSession: ownSession}
	if daemonStops {
		opts.stopAtPanePIDCall = 1
	}
	return surviveRunDriveWith(t, opts)
}

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
		out.recordAtSpawn, out.recordAtSpawnErr = legacyRunRecord(projectDir, runID.String())
		out.rawRecordAtSpawn, out.rawRecordAtSpawnErr = rawRunRecord(projectDir, runID.String())
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

	wtPath := filepath.Join(t.TempDir(), "run-worktree")
	if opts.piRun || opts.realWorktree {
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

	registry := surviveRunSealedRegistry(t)
	if opts.agentReportsReady || opts.piRun {
		registry = surviveRunEmptySealedRegistry()
	}

	bus := eventbus.NewBusImpl()
	if _, subErr := bus.Subscribe(core.Subscription{
		ConsumerID:    "survive-run-started-mode",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  core.EventPattern{Types: map[core.EventType]struct{}{core.EventTypeRunStarted: {}}},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
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
		Runner:           opts.runner,
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

func surviveRunGitWorktree(t *testing.T, repo, path string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b",
		"survive-run-fixture", path, "HEAD")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("surviveRunGitWorktree: git worktree add: %v\n%s", err, out)
	}
}

func surviveRunPiScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pi-agent.sh")
	const script = "#!/bin/sh\necho 'pi agent: cannot reach the model endpoint'\nexit 3\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture script must be executable
		t.Fatalf("surviveRunPiScript: WriteFile: %v", err)
	}
	return path
}

func surviveRunEmptySealedRegistry() *handlercontract.AdapterRegistry {
	reg := handlercontract.NewAdapterRegistry()
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) //nolint:errcheck // called for its sealing effect; the miss is the point
	return reg
}

func surviveRunSealedRegistry(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("surviveRun: register claude adapter: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) //nolint:errcheck // called for its sealing effect
	return reg
}

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
