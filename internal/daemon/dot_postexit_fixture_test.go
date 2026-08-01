package daemon_test

// dot_postexit_fixture_test.go — one hermetic fixture for the DOT post-exit
// region.
//
// The graph path is the production default and it carries essentially all
// traffic, but almost every test that drives a real graph run sits behind the
// `scenario` build tag, so the gate never sees it. This fixture runs ONE bead
// through the real work loop in DOT mode with a three-node graph, a shell
// implementer that commits, and stubbed hook state. It needs no tmux, no ssh and
// no `go` toolchain, so it runs untagged and in -short.
//
// Every test built on it asserts on what the run DID — which bead transition it
// reached, which events it emitted — and not on the shape of the code.

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
	"github.com/gregberns/harmonik/internal/harness/shared"
	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/runloop"
)

// dotFixtureGraph is a minimal valid DOT workflow: start (non-agentic noop) →
// implement (agentic implementer, commit required) → close (success terminal).
//
// It carries no reviewer node (a reviewer needs a review.json verdict a shell
// stub cannot write) and no commit_gate tool node (that needs `go` and the test
// suite), so the run is hermetic while still walking every post-exit probe the
// graph node makes.
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

// dotFixtureHookStore is a hook store that answers with a fixed outcome_emitted
// payload. It replaces the real store so a test can state what the agent
// reported through the socket without standing up the relay, and so the run
// never pays the 3-second stop-hook grace window.
//
// A nil Outcome means "the agent reported nothing" — CHB-020 branch 3.
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

// dotFixtureFailureSignal is the outcome_emitted payload a Claude agent produces
// when its Stop hook reports failure (CHB-020 branch 2).
const dotFixtureFailureSignal = `{"kind":"FAILURE_SIGNAL","sub_reason":"claude_reported_failure","suggested_class":"structural"}`

// dotFixtureWorkComplete is the outcome_emitted payload of a clean agent turn
// (CHB-020 branch 1).
const dotFixtureWorkComplete = `{"kind":"WORK_COMPLETE"}`

// dotFixtureOpts are the knobs a post-exit test turns. Everything else about the
// run is fixed.
type dotFixtureOpts struct {
	// HookOutcome is the raw outcome_emitted payload the agent reported, or
	// empty for "nothing arrived".
	HookOutcome string

	// HandlerScript is the /bin/sh script the implementer node runs. Empty
	// installs dotFixtureCommittingHandler — an agent that commits real work.
	HandlerScript string

	// Runner is the CommandRunner the DOT path routes its git probes and its
	// workspace writes through (the Config.Runner seam). Nil keeps every probe
	// bare-local.
	Runner tmuxPkg.CommandRunner

	// HookStore replaces the default fixture store. Nil installs
	// dotFixtureHookStore over HookOutcome.
	HookStore runloop.HookStore

	// RunRegistry lets a test read the in-flight RunHandle while the run is
	// still live. Nil creates a fresh one inside the deps.
	RunRegistry *daemon.RunRegistry

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

	// WorkflowMode is the per-item mode. Empty runs the bead in dot mode, which
	// is what this fixture exists for.
	WorkflowMode core.WorkflowMode
}

// dotFixtureResult is what the caller asserts on.
type dotFixtureResult struct {
	ProjectDir string
	Ledger     *stubBeadLedger
	Bus        *stubEventCollector
}

// dotFixtureLedger is stubBeadLedger with a bead body. The body is what carries
// a `## Branching` block, and that block is how a bead declares the target_repo
// a cross-repo run lands in.
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

// dotFixtureCommittingHandler writes a /bin/sh implementer that commits a file
// in its working directory (the run worktree, set from spec.WorkDir), so the
// node's HEAD advances DURING the node the way a real implementer's does. The
// commit carries the bead's `Refs:` trailer, which the subsumption and
// no-commit guards read.
func dotFixtureCommittingHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-implementer.sh", dotFixtureCommitLines(bead)+"exit 0\n")
}

// dotFixtureCommitThenCrashHandler writes an implementer that commits real work
// and then exits non-zero with nothing reported through the socket — CHB-020
// branch 3 with a crash.
func dotFixtureCommitThenCrashHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-crash.sh", dotFixtureCommitLines(bead)+"exit 3\n")
}

// dotFixtureCommitThenGarbageHandler writes an implementer that commits real
// work and then writes a line the NDJSON progress-stream reader cannot parse,
// which puts the watcher into a structural error.
func dotFixtureCommitThenGarbageHandler(t *testing.T, bead core.BeadID) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-garbage.sh",
		dotFixtureCommitLines(bead)+"printf 'this line is not NDJSON\\n'\nexit 0\n")
}

// dotFixtureCommitLines is the shell prologue that makes the run worktree's HEAD
// advance, shared by every handler that must look like an agent which did work.
func dotFixtureCommitLines(bead core.BeadID) string {
	return "set -e\n" +
		"echo \"work for " + string(bead) + " $$\" > fixture-work.txt\n" +
		"git add fixture-work.txt\n" +
		"git commit -m \"feat: dot fixture work\n\nRefs: " + string(bead) + "\" >/dev/null 2>&1\n"
}

// dotFixtureHandlerScript writes body as an executable /bin/sh script.
func dotFixtureHandlerScript(t *testing.T, name, body string) string {
	t.Helper()
	scriptPath := filepath.Join(t.TempDir(), name)
	//nolint:gosec // G306: 0755 is required to exec a handler script in a test tree.
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("dotFixtureHandlerScript: write %s: %v", scriptPath, err)
	}
	return scriptPath
}

// dotFixtureNoCommitHandler writes a /bin/sh implementer that does nothing and
// exits 0 — the shape of an agent that produced no work.
func dotFixtureNoCommitHandler(t *testing.T) string {
	t.Helper()
	return dotFixtureHandlerScript(t, "dot-fixture-nowork.sh", "exit 0\n")
}

// runDotFixtureBead drives ONE bead through the real work loop in DOT mode and
// returns when the bead reaches a terminal transition (closed or reopened).
func runDotFixtureBead(t *testing.T, beadID core.BeadID, opts dotFixtureOpts) dotFixtureResult {
	t.Helper()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	//nolint:gosec // G306: test fixture file.
	if err := os.WriteFile(filepath.Join(projectDir, "workflow.dot"), []byte(dotFixtureGraph), 0o644); err != nil {
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
	ledger := &dotFixtureLedger{stubBeadLedger: &stubBeadLedger{}, description: opts.BeadDescription, labels: opts.BeadLabels}
	bus := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		AllowedRepos:      opts.AllowedRepos,
		ProjectCfg:        opts.ProjectCfg,
		DefaultHarness:    opts.DefaultHarness,
		LaunchSpecBuilder: opts.LaunchSpecBuilder,
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

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		_ = daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck // the loop's own error is not the claim under test; the bead transition is
	}()

	deadline := time.After(50 * time.Second)
	for len(ledger.closedIDs()) == 0 && len(ledger.reopenedIDs()) == 0 {
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
