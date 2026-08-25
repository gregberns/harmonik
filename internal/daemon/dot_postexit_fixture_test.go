package daemon_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/runregistry"
)

const dotFixtureGraph = `digraph "dot-postexit-fixture" {
    schema_version="1";
    version="1.0";
    workflow_id="dot-postexit-fixture";
    start_node="start";
    terminal_node_ids="close,close-needs-attention";

    start [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="entry"
    ];

    implement [
        type="agentic",
        agent_type="implementer",
        handler_ref="claude-implementer",
        idempotency_class="non-idempotent",
        role="produce the change; commit required"
    ];

    close [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="success terminal"
    ];

    "close-needs-attention" [
        type="non-agentic",
        handler_ref="noop",
        idempotency_class="idempotent",
        role="failure terminal"
    ];

    start -> implement;

    implement -> close [
        condition="outcome.status == 'SUCCESS'"
    ];

    implement -> "close-needs-attention";
}
`

type dotFixtureHookStore struct {
	Outcome json.RawMessage
}

func (dotFixtureHookStore) RegisterHookSession(string, string) {}
func (dotFixtureHookStore) CloseHookSession(string, string)    {}

func (s dotFixtureHookStore) LatestOutcome(string, string) *json.RawMessage {
	if len(s.Outcome) == 0 {
		return nil
	}
	out := s.Outcome
	return &out
}

func (s dotFixtureHookStore) WaitForOutcome(context.Context, string, string) (json.RawMessage, error) {
	return s.Outcome, nil
}

// SetAgentReadyCallback fires the callback at once. The fixture registers no
// claude adapter, so the readiness handshake is skipped and nothing waits on
// this — but a store that silently dropped the callback would be a fixture that
// disables a production edge, so it is honoured here.
func (dotFixtureHookStore) SetAgentReadyCallback(_, _ string, cb func()) {
	if cb != nil {
		cb()
	}
}

const dotFixtureFailureSignal = `{"kind":"FAILURE_SIGNAL","sub_reason":"claude_reported_failure","suggested_class":"structural"}`

const dotFixtureWorkComplete = `{"kind":"WORK_COMPLETE"}`

type dotFixtureOpts struct {
	// RunContext replaces the fixture's ordinary timeout context. Tests use it
	// to stop a live graph run at a precise point, such as after its handler
	// commits and before the graph driver reaches its terminal node.
	//
	// The context lives in the options struct on purpose. dotFixtureOpts is a
	// bag of knobs, not a call scope: the run context is the thing under test
	// here, so a test must be able to supply its own instead of taking the
	// fixture's default.
	RunContext context.Context

	// HookOutcome is the raw outcome_emitted payload the agent reported, or
	// empty for "nothing arrived".
	HookOutcome string

	// HandlerScript is the /bin/sh script the implementer node runs. Empty
	// installs dotFixtureCommittingHandler — an agent that commits real work.
	HandlerScript string

	// Graph replaces dotFixtureGraph as the run's workflow.dot. A test that needs
	// a node class the default three-node graph omits — a reviewer, say —
	// supplies its own. Empty installs dotFixtureGraph.
	Graph string

	// Runner is the CommandRunner the DOT path routes its git probes and its
	// workspace writes through (the Config.Runner seam). Nil keeps every probe
	// bare-local.
	Runner tmuxPkg.CommandRunner

	// HookStore replaces the default fixture store. Nil installs
	// dotFixtureHookStore over HookOutcome.
	HookStore runloop.HookStore

	// runregistry.RunRegistry lets a test read the in-flight runregistry.RunHandle while the run is
	// still live. Nil creates a fresh one inside the deps.
	RunRegistry *runregistry.RunRegistry

	// BeadDescription is the bead body the ledger reports. A `## Branching`
	// block in it is how a bead declares a cross-repo target_repo.
	BeadDescription string

	// AllowedRepos is the cross-repo dispatch safelist. A target_repo outside it
	// is refused before the run starts.
	AllowedRepos []string

	// BeforeRun runs against the freshly built project dir, after its git repo
	// exists and before the work loop starts.
	BeforeRun func(t *testing.T, projectDir string)

	// BeadLabels are the bead's labels. `profile:<name>` is how a bead selects a
	// Pi provider profile.
	BeadLabels []string

	// ProjectCfg is the decoded project config the run resolves against.
	ProjectCfg projectconfig.ProjectConfig

	// DefaultHarness is the daemon-level harness default (tier 4).
	DefaultHarness core.AgentType

	// LaunchSpecBuilder replaces the run's launch-spec port. Nil keeps the
	// production one.
	LaunchSpecBuilder func(context.Context, shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)

	// HarnessRegistry is how the run looks up a resolved agent type's harness.
	// Nil leaves it unset, which is what makes the process-exit commit fallback a
	// no-op for a fixture run — supply one to reach that branch.
	HarnessRegistry *handlercontract.HarnessRegistry

	// WorkflowMode is the per-item mode. Empty runs the bead in dot mode, which
	// is what this fixture exists for.
	WorkflowMode core.WorkflowMode

	// CloseError makes the fixture ledger reject the terminal CloseBead call.
	// It exercises release failure after a real DOT handler committed work.
	CloseError error

	// WaitForRunTerminal keeps the work loop alive until a close or reopen also
	// reaches run_completed or run_failed.
	WaitForRunTerminal bool

	// ObserveTerminalStep records terminal ledger and bus steps in their real
	// call order. It receives only close, close_failed, reopen, bead_closed,
	// run_completed, run_failed, and outcome_emitted.
	ObserveTerminalStep func(string)
}

type dotFixtureResult struct {
	ProjectDir string
	Ledger     *stubBeadLedger
	Bus        *stubEventCollector
}

type dotFixtureLedger struct {
	*stubBeadLedger
	description string
	labels      []string
}

func (l *dotFixtureLedger) ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error) {
	rec, err := l.stubBeadLedger.ShowBead(ctx, id)
	if err != nil {
		return rec, err
	}
	rec.Description = l.description
	rec.Labels = l.labels
	return rec, nil
}

func dotFixtureCommittingHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-implementer.sh", dotFixtureCommitLines(bead)+"exit 0\n")
}

func dotFixtureCommitThenCrashHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-crash.sh", dotFixtureCommitLines(bead)+"exit 3\n")
}

func dotFixtureCommitThenGarbageHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-garbage.sh",
		dotFixtureCommitLines(bead)+"printf 'this line is not NDJSON\\n'\nexit 0\n")
}

func dotFixtureCommitLines(bead core.BeadID) string {
	return "set -e\n" +
		"echo \"work for " + string(bead) + " $$\" > fixture-work.txt\n" +
		"git add fixture-work.txt\n" +
		"git commit -m \"feat: dot fixture work\n\nRefs: " + string(bead) + "\" >/dev/null 2>&1\n"
}

func dotFixtureHandlerScript(t *testing.T, name, body string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: 0755 is required to exec a handler script in a test tree.
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("dotFixtureHandlerScript: write %s: %v", scriptPath, err)
	}
	return scriptPath
}

func dotFixtureNoCommitHandler(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-nowork.sh", "exit 0\n")
}

func runDotFixtureBead(t *testing.T, beadID core.BeadID, opts dotFixtureOpts) dotFixtureResult {
	t.Helper()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	graph := opts.Graph
	if graph == "" {
		graph = dotFixtureGraph
	}
	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(filepath.Join(projectDir, "workflow.dot"), []byte(graph), 0o644); err != nil {
		t.Fatalf("runDotFixtureBead: write workflow.dot: %v", err)
	}
	var hookStore runloop.HookStore = dotFixtureHookStore{Outcome: json.RawMessage(opts.HookOutcome)}
	if opts.HookStore != nil {
		hookStore = opts.HookStore
	}

	if opts.BeforeRun != nil {
		opts.BeforeRun(t, projectDir)
	}

	handlerScript := opts.HandlerScript
	if handlerScript == "" {
		handlerScript = dotFixtureCommittingHandler(t, beadID)
	}

	mode := opts.WorkflowMode
	if mode == "" {
		mode = core.WorkflowModeDot
	}

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{{
				BeadID:       beadID,
				Status:       queue.ItemStatusPending,
				WorkflowMode: string(mode),
			}},
			CreatedAt: now,
		}},
	}

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	observe := opts.ObserveTerminalStep
	ledger := &dotFixtureLedger{
		stubBeadLedger: &stubBeadLedger{
			closeErr: opts.CloseError,
			onClose: func(err error) {
				if observe == nil {
					return
				}
				if err != nil {
					observe("close_failed")
					return
				}
				observe("close")
			},
			onReopen: func() {
				if observe != nil {
					observe("reopen")
				}
			},
		},
		description: opts.BeadDescription,
		labels:      opts.BeadLabels,
	}
	bus := &stubEventCollector{onEmit: func(eventType core.EventType) {
		if observe == nil {
			return
		}
		switch eventType {
		case core.EventTypeBeadClosed, core.EventTypeRunCompleted, core.EventTypeRunFailed, core.EventTypeOutcomeEmitted:
			observe(string(eventType))
		}
	}}

	deps := daemon.ExportedTestRuntime(daemon.TestRuntimeParams{
		BrAdapter:         ledger,
		AllowedRepos:      opts.AllowedRepos,
		ProjectCfg:        opts.ProjectCfg,
		DefaultHarness:    opts.DefaultHarness,
		LaunchSpecBuilder: opts.LaunchSpecBuilder,
		HarnessRegistry:   opts.HarnessRegistry,
		Bus:               bus,
		ProjectDir:        projectDir,
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{handlerScript},
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:        qs,
		HookStore:         hookStore,
		RunRegistry:       opts.RunRegistry,
		Runner:            opts.Runner,
		// No claude adapter: the shell implementer never relays agent_ready, so
		// the readiness gate is bypassed and the run proceeds on the process exit
		// (hk-ngw3d).
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
	})

	ctx := opts.RunContext
	cancel := func() {}
	if ctx == nil {
		ctx, cancel = context.WithTimeout(t.Context(), 60*time.Second)
	}
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck // the loop's own error is not the claim under test; the bead transition is
	}()

	deadline := time.After(50 * time.Second)
	for {
		terminalTransition := len(ledger.closedIDs()) > 0 || len(ledger.reopenedIDs()) > 0
		if terminalTransition && (!opts.WaitForRunTerminal || dotFixtureRunTerminalSeen(bus)) {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("runDotFixtureBead: bead %s reached no terminal transition; events=%v", beadID, bus.eventTypes())
		case <-time.After(25 * time.Millisecond):
		}
	}
	cancel()
	<-loopDone

	return dotFixtureResult{ProjectDir: projectDir, Ledger: ledger.stubBeadLedger, Bus: bus}
}

func dotFixtureRunTerminalSeen(bus *stubEventCollector) bool {
	for _, eventType := range bus.eventTypes() {
		if eventType == string(core.EventTypeRunCompleted) || eventType == string(core.EventTypeRunFailed) {
			return true
		}
	}
	return false
}
