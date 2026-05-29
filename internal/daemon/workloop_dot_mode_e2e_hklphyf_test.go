package daemon_test

// workloop_dot_mode_e2e_hklphyf_test.go — end-to-end DOT workflow-mode through
// the REAL daemon work loop + a deterministic handler.
//
// # Why this file exists
//
// Every other DOT (workflow-graph) test in the tree exercises the
// parser→validator→cascade pipeline in isolation (internal/workflow,
// internal/workflow/dot) with mocked handlers. NONE of them boots the daemon
// work loop. This file closes that gap: it drives `--workflow-mode=dot` through
// the production runWorkLoop dispatch path (via the queue-pull surface that the
// CLI `harmonik run --workflow-mode dot --workflow-ref <path>` populates) with a
// real handler subprocess, and asserts how far the real DOT-mode path actually
// executes today.
//
// # STATUS of --workflow-mode=dot through the daemon (hk-9dnak: cascade WIRED)
//
// The daemon's DOT-mode branch in internal/daemon/workloop.go (beadRunOne,
// `case core.WorkflowModeDot:`) now:
//
//   1. Resolves the .dot path: itemWorkflowRef (CLI --workflow-ref) when set,
//      else <projectDir>/workflow.dot. Relative refs resolve against projectDir.
//   2. Calls workflow.LoadDotWorkflow(dotPath) — parse + validate. On failure it
//      reopens the bead with failure_class=workflow_load.
//   3. On success it hands the validated graph to driveDotWorkflow
//      (internal/daemon/dot_cascade.go, hk-9dnak), which WALKS the graph
//      node-by-node using workflow.DecideNextNode (the cascade engine, hk-bf85t):
//      non-agentic nodes synthesize SUCCESS; agentic nodes dispatch into the
//      substrate; the reviewer's verdict drives the conditional cascade; the walk
//      ends at a terminal node (close → CloseBead; close-needs-attention → reopen).
//
// So the assertions below split into two tiers:
//
//   TIER 1: boot work loop → DOT load+validate → run_started → cascade walk →
//   bead closed → queue drains.
//
//   TIER 2: the cascade node-transition walk itself — node_dispatch_requested /
//   node_dispatch_decided events plus the start → implementer → reviewer →
//   (APPROVE) → close terminal walk.
//
// GATE nodes are deliberately NOT exercised (per the bead brief): gate
// deny/success semantics are under an unresolved spec contradiction (EM-005b vs
// CP-058), and the driver returns a deterministic error for gate/sub-workflow
// nodes. The review-loop topology used here is agentic + non-agentic + reviewer
// nodes only — no gate nodes.
//
// Spec refs: specs/execution-model.md §EM-015d (review-loop topology), §7.5
// (dot-mode dispatcher); specs/workflow-graph.md §8 (terminal-node declaration);
// specs/examples/review-loop.dot.
// Bead ref: hk-lphyf (DOT scenario round-trip); hk-9dnak (cascade driver wiring);
// hk-bf85t (cascade engine); hk-qo9pq (--workflow-ref CLI wiring); hk-waj4b
// (DOT-mode load+validate branch).

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// dotE2EModuleRoot returns the harmonik module root, derived from this test
// file's location (mirrors t2FindBinary in t2_scenarios_test.go).
func dotE2EModuleRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = <root>/internal/daemon/workloop_dot_mode_e2e_hklphyf_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// dotE2ESeedWorkflow copies a .dot fixture from the module-root-relative
// srcRel into the test's project dir as workflow.dot and returns its absolute
// path. Seeding a copy into the project tree (rather than referencing the repo
// path directly) keeps the test hermetic and exercises the same
// itemWorkflowRef resolution the CLI uses.
func dotE2ESeedWorkflow(t *testing.T, projectDir, srcRel, destName string) string {
	t.Helper()
	srcPath := filepath.Join(dotE2EModuleRoot(), srcRel)
	//nolint:gosec // G304: srcRel is a hard-coded repo-relative fixture path.
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("dotE2ESeedWorkflow: read fixture %s: %v", srcPath, err)
	}
	destPath := filepath.Join(projectDir, destName)
	//nolint:gosec // G306: 0644 is fine for a test fixture in a t.TempDir tree.
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		t.Fatalf("dotE2ESeedWorkflow: write %s: %v", destPath, err)
	}
	return destPath
}

// ─────────────────────────────────────────────────────────────────────────────
// TIER 1 — real DOT-mode daemon path: boot → load+validate → dispatch → close
// ─────────────────────────────────────────────────────────────────────────────

// TestDotMode_E2E_LoadValidateDispatchClose drives the canonical
// review-loop.dot through the REAL work loop with --workflow-mode=dot and a
// deterministic phase-aware /bin/sh handler.
//
// This asserts the load+validate+dispatch+close extent of the daemon DOT-mode
// path:
//
//   - The queue item carries WorkflowMode="dot" + WorkflowRef=<review-loop.dot>,
//     exactly as `harmonik run --workflow-mode dot --workflow-ref` populates it.
//   - beadRunOne resolves the ref, calls workflow.LoadDotWorkflow (parse +
//     validate the FULL review-loop topology: start, implementer, reviewer,
//     close, close-needs-attention + 6 edges including the conditional cascade).
//   - On successful load it hands the graph to driveDotWorkflow (hk-9dnak), which
//     walks start → implementer → reviewer → close.
//   - The bead is CLOSED (not reopened), the queue drains, and cancelOnQueueDrain
//     fires so the loop exits.
//
// The key signal: the bead is CLOSED, not REOPENED. A reopen would mean the DOT
// artifact failed to load/validate (failure_class=workflow_load) or the walk
// failed. A close means the real path got all the way through to the APPROVE
// success terminal. The deeper node-transition assertions live in
// TestDotMode_E2E_CascadeTransitions below.
func TestDotMode_E2E_LoadValidateDispatchClose(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Seed the canonical review-loop topology (agentic + non-agentic + reviewer
	// nodes; NO gate nodes — gate semantics are under EM-005b/CP-058 contradiction).
	// Seed under .harmonik/ so the artifact is treated as harmonik churn (allowlisted
	// by isHarmonikChurn) and does NOT trip the implementer-escaped-worktree guard
	// (workloop.go ~L1688) that scans MAIN's working tree for stray files.
	dotPath := dotE2ESeedWorkflow(t, projectDir, "specs/examples/review-loop.dot", ".harmonik/workflow.dot")

	const beadID = core.BeadID("hk-lphyf-dot-e2e-001")

	stateDir := t.TempDir()

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "dot-e2e-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:       beadID,
						Status:       queue.ItemStatusPending,
						WorkflowMode: string(core.WorkflowModeDot), // --workflow-mode dot
						WorkflowRef:  dotPath,                      // --workflow-ref <path>
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	drainCtx, cancelDrain := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      dotE2ECascadeHandlerScript(t, stateDir), // impl commits, reviewer APPROVEs
		HandlerArgs:        nil,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
		CancelOnQueueExit:  cancelDrain, // exit on either outcome
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(28 * time.Second):
		t.Fatalf("DOT-mode E2E: runWorkLoop did not exit; events=%v closed=%v reopened=%v",
			bus.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
	}

	events := bus.eventTypes()
	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("DOT-mode E2E: events=%v closed=%v reopened=%v", events, closed, reopened)

	// Assertion 1: run_started fired — the loop genuinely dispatched the bead.
	if !dotE2EContains(events, string(core.EventTypeRunStarted)) {
		t.Errorf("DOT-mode E2E: run_started not emitted; got %v", events)
	}

	// Assertion 2: the bead was CLOSED, not REOPENED. A reopen here would mean
	// LoadDotWorkflow failed (failure_class=workflow_load) — i.e. the real DOT
	// path never got past validate. Close proves load+validate+dispatch all ran.
	if len(reopened) > 0 {
		t.Errorf("DOT-mode E2E: bead was REOPENED (%v) — this means workflow.LoadDotWorkflow "+
			"FAILED to parse/validate review-loop.dot through the daemon; the real DOT path "+
			"did not get past validation. events=%v", reopened, events)
	}
	if len(closed) == 0 {
		t.Errorf("DOT-mode E2E: bead was neither closed nor reopened; expected a terminal close "+
			"after DOT load+validate+single-mode dispatch. events=%v", events)
	} else if closed[0] != beadID {
		t.Errorf("DOT-mode E2E: closed bead = %q; want %q", closed[0], beadID)
	}

	// Assertion 3: a run_completed event fired (single-mode fall-through closes
	// the run). This is the terminal of the REAL path today.
	if !dotE2EContains(events, string(core.EventTypeRunCompleted)) {
		t.Errorf("DOT-mode E2E: run_completed not emitted; got %v", events)
	}

	// Assertion 4: queue drained cleanly (CompleteAndUnlink ran).
	if qs.Queue() != nil {
		t.Error("DOT-mode E2E: QueueStore.Queue() is non-nil after drain; expected ClearQueue")
	}
}

// TestDotMode_E2E_InvalidWorkflowRefReopens is the negative control: when the
// --workflow-ref points at a path that does not exist, the daemon's DOT-mode
// branch MUST reopen the bead with failure_class=workflow_load rather than
// silently closing it. This proves the load+validate gate in the real path is
// genuinely exercised (not bypassed).
func TestDotMode_E2E_InvalidWorkflowRefReopens(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-lphyf-dot-e2e-badref-001")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "dot-e2e-badref-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:       beadID,
						Status:       queue.ItemStatusPending,
						WorkflowMode: string(core.WorkflowModeDot),
						WorkflowRef:  filepath.Join(projectDir, "does-not-exist.dot"),
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	exitCtx, cancelExit := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		CancelOnQueueDrain: cancelExit,
		CancelOnQueueExit:  cancelExit,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 30*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Poll for the reopen rather than relying solely on loop exit: a workflow_load
	// reopen keeps the bead pending in some queue configurations.
	deadline := time.After(20 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-loopDone:
			goto assert
		case <-deadline:
			t.Fatalf("DOT-mode badref: timed out waiting for reopen; events=%v closed=%v reopened=%v",
				bus.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
		case <-time.After(50 * time.Millisecond):
		}
	}

assert:
	testCancel()

	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("DOT-mode badref: events=%v closed=%v reopened=%v", bus.eventTypes(), closed, reopened)

	if len(reopened) == 0 {
		t.Errorf("DOT-mode badref: bead was NOT reopened on a missing --workflow-ref; "+
			"the daemon DOT-mode load gate must reopen with failure_class=workflow_load. "+
			"closed=%v events=%v", closed, bus.eventTypes())
	}
	if len(closed) > 0 {
		t.Errorf("DOT-mode badref: bead was CLOSED on a missing --workflow-ref; "+
			"a failed DOT load must reopen, not close: %v", closed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TIER 2 — cascade WIRED into the daemon (hk-9dnak): real node-transition walk
// ─────────────────────────────────────────────────────────────────────────────

// dotE2ECascadeHandlerScript returns a /bin/sh script that is PHASE-AWARE so a
// single handler binary can serve both the implementer and reviewer nodes of the
// canonical review-loop topology when driven by driveDotWorkflow (hk-9dnak).
//
// The DOT cascade walk for review-loop.dot (APPROVE path) dispatches handler
// invocations in this order (the non-agentic start node never invokes a handler):
//
//	invocation 1 → implementer node  → must COMMIT (advance HEAD)
//	invocation 2 → reviewer node     → must WRITE .harmonik/review.json (APPROVE)
//
// The script discriminates by counting its own invocations via a counter file in
// a caller-supplied state dir (outside the worktree so it survives across the two
// handler processes). Odd invocations behave as the implementer; even invocations
// behave as the reviewer and emit an APPROVE verdict, which drives the reviewer
// cascade down the `reviewer -> close [APPROVE]` edge to the success terminal.
func dotE2ECascadeHandlerScript(t *testing.T, stateDir string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "cascade-handler.sh")
	counterPath := filepath.Join(stateDir, "invocations")
	// review.json schema v1: {schema_version, verdict, flags, notes}.
	script := "#!/bin/sh\n" +
		"set -e\n" +
		"CTR=\"" + counterPath + "\"\n" +
		"N=0\n" +
		"if [ -f \"$CTR\" ]; then N=$(cat \"$CTR\"); fi\n" +
		"N=$((N+1))\n" +
		"echo \"$N\" > \"$CTR\"\n" +
		"REM=$((N % 2))\n" +
		"if [ \"$REM\" -eq 1 ]; then\n" +
		"  # Implementer phase: commit a sentinel so HEAD advances.\n" +
		"  echo \"dot-cascade impl $N $$\" > dot-cascade-impl-$N.txt\n" +
		"  git add dot-cascade-impl-$N.txt\n" +
		"  git commit -m \"dot-cascade: implementer commit $N\" >/dev/null 2>&1\n" +
		"else\n" +
		"  # Reviewer phase: write an APPROVE verdict to .harmonik/review.json.\n" +
		"  mkdir -p .harmonik\n" +
		"  printf '%s' '{\"schema_version\":1,\"verdict\":\"APPROVE\",\"flags\":[],\"notes\":\"dot-cascade e2e approve\"}' > .harmonik/review.json\n" +
		"fi\n" +
		"exit 0\n"
	//nolint:gosec // G306: 0755 required to exec the handler script in a test tree.
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("dotE2ECascadeHandlerScript: write %s: %v", scriptPath, err)
	}
	return scriptPath
}

// TestDotMode_E2E_CascadeTransitions drives the canonical review-loop.dot through
// the REAL work loop with --workflow-mode=dot and the cascade driver wired in
// (hk-9dnak). It proves the genuine node-transition walk:
//
//	start(non-agentic) → implementer(agentic, commits)
//	  → reviewer(agentic, APPROVE) → close (success terminal)
//
// The signal: the bead is CLOSED (success terminal `close` reached) AND the
// node-transition observability events (node_dispatch_requested /
// node_dispatch_decided) fired — neither of which the pre-hk-9dnak fall-through
// path emitted. This is the cascade actually walking the graph through the daemon,
// not a single-mode one-shot of the first node.
func TestDotMode_E2E_CascadeTransitions(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	dotPath := dotE2ESeedWorkflow(t, projectDir, "specs/examples/review-loop.dot", ".harmonik/workflow.dot")

	const beadID = core.BeadID("hk-9dnak-dot-cascade-001")

	stateDir := t.TempDir()

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "dot-cascade-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:       beadID,
						Status:       queue.ItemStatusPending,
						WorkflowMode: string(core.WorkflowModeDot),
						WorkflowRef:  dotPath,
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	drainCtx, cancelDrain := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      dotE2ECascadeHandlerScript(t, stateDir),
		HandlerArgs:        nil,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
		CancelOnQueueExit:  cancelDrain,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, 60*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(58 * time.Second):
		t.Fatalf("DOT cascade E2E: runWorkLoop did not exit; events=%v closed=%v reopened=%v",
			bus.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
	}

	events := bus.eventTypes()
	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("DOT cascade E2E: events=%v closed=%v reopened=%v", events, closed, reopened)

	// Assertion 1: run_started fired.
	if !dotE2EContains(events, string(core.EventTypeRunStarted)) {
		t.Errorf("DOT cascade E2E: run_started not emitted; got %v", events)
	}

	// Assertion 2: the cascade actually walked the graph — node_dispatch_requested
	// AND node_dispatch_decided fired. These are emitted ONLY by the cascade driver
	// (driveDotWorkflow); the old fall-through single-mode path never emitted them.
	if !dotE2EContains(events, string(core.EventTypeNodeDispatchRequested)) {
		t.Errorf("DOT cascade E2E: node_dispatch_requested not emitted — the cascade "+
			"driver did not walk the graph; got %v", events)
	}
	if !dotE2EContains(events, string(core.EventTypeNodeDispatchDecided)) {
		t.Errorf("DOT cascade E2E: node_dispatch_decided not emitted — the cascade engine "+
			"was not consulted for node transitions; got %v", events)
	}

	// Assertion 3: the bead was CLOSED (success terminal `close` reached), not
	// reopened. A reopen would mean the walk failed before the APPROVE terminal.
	if len(reopened) > 0 {
		t.Errorf("DOT cascade E2E: bead was REOPENED (%v) — the cascade did not reach the "+
			"APPROVE success terminal `close`. events=%v", reopened, events)
	}
	if len(closed) == 0 {
		t.Errorf("DOT cascade E2E: bead was neither closed nor reopened; expected close on "+
			"the APPROVE success terminal. events=%v", events)
	} else if closed[0] != beadID {
		t.Errorf("DOT cascade E2E: closed bead = %q; want %q", closed[0], beadID)
	}

	// Assertion 4: run_completed fired (terminal of the success path).
	if !dotE2EContains(events, string(core.EventTypeRunCompleted)) {
		t.Errorf("DOT cascade E2E: run_completed not emitted; got %v", events)
	}

	// Assertion 5: queue drained cleanly.
	if qs.Queue() != nil {
		t.Error("DOT cascade E2E: QueueStore.Queue() is non-nil after drain; expected ClearQueue")
	}
}

// TestDotMode_E2E_NonAgenticIntermediateNodeSuccess drives the
// review-loop-finalize.dot fixture through the REAL work loop, asserting that a
// run classified SUCCESS when the close terminal is reached via a non-agentic
// intermediate node (reviewer→[APPROVE]→finalize[non-agentic]→close).
//
// This is the regression test for hk-z03e8: the old dotTerminalIsSuccess
// edge-topology heuristic (forbidden by WG-021) misclassified this topology as
// FAILED because close's only inbound edge is unconditional (from finalize, not
// directly from reviewer via an APPROVE edge). The WG-021-compliant fix
// (dotTerminalNodeIsSuccess) classifies by terminal ID alone: close → SUCCESS.
//
// Handler script: same odd/even invocation pattern as dotE2ECascadeHandlerScript,
// but the graph now has an extra non-agentic finalize node that the cascade
// traverses without calling the handler — so the invocation count and commit
// semantics remain identical.
func TestDotMode_E2E_NonAgenticIntermediateNodeSuccess(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	// Seed the finalize-topology fixture (reviewer→[APPROVE]→finalize→close).
	dotPath := dotE2ESeedWorkflow(t, projectDir,
		"specs/examples/review-loop-finalize.dot", ".harmonik/workflow.dot")

	const beadID = core.BeadID("hk-z03e8-dot-finalize-001")

	stateDir := t.TempDir()

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "dot-finalize-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:       beadID,
						Status:       queue.ItemStatusPending,
						WorkflowMode: string(core.WorkflowModeDot),
						WorkflowRef:  dotPath,
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	drainCtx, cancelDrain := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      dotE2ECascadeHandlerScript(t, stateDir),
		HandlerArgs:        nil,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
		CancelOnQueueExit:  cancelDrain,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, 60*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(58 * time.Second):
		t.Fatalf("DOT finalize E2E: runWorkLoop did not exit; events=%v closed=%v reopened=%v",
			bus.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
	}

	events := bus.eventTypes()
	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("DOT finalize E2E: events=%v closed=%v reopened=%v", events, closed, reopened)

	// The bead MUST be CLOSED (success), not reopened. A reopen here means the
	// old edge-topology heuristic is still active (hk-z03e8 regression).
	if len(reopened) > 0 {
		t.Errorf("DOT finalize E2E: bead was REOPENED (%v) — the cascade "+
			"misclassified 'close' as needs-attention because its inbound edge "+
			"is unconditional (from the non-agentic finalize node). This is the "+
			"hk-z03e8 regression: dotTerminalNodeIsSuccess must classify by "+
			"terminal ID, not inbound-edge topology. events=%v", reopened, events)
	}
	if len(closed) == 0 {
		t.Errorf("DOT finalize E2E: bead was neither closed nor reopened; "+
			"expected close on the success terminal. events=%v", events)
	} else if closed[0] != beadID {
		t.Errorf("DOT finalize E2E: closed bead = %q; want %q", closed[0], beadID)
	}

	if !dotE2EContains(events, string(core.EventTypeRunCompleted)) {
		t.Errorf("DOT finalize E2E: run_completed not emitted; got %v", events)
	}
	if qs.Queue() != nil {
		t.Error("DOT finalize E2E: QueueStore.Queue() is non-nil after drain; expected ClearQueue")
	}
}

// dotE2EContains reports whether haystack contains needle.
func dotE2EContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle || strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// REQUEST_CHANGES back-edge + cap-hit E2E (hk-1xsyu)
// ─────────────────────────────────────────────────────────────────────────────

// dotE2ERequestChangesHandlerScript returns a /bin/sh handler script that is
// phase-aware and emits REQUEST_CHANGES on the first reviewer visit, then
// APPROVE on the second. This drives the reviewer→implementer back-edge exactly
// once before the run terminates at the success terminal.
//
// Invocation sequence for review-loop.dot (back-edge path):
//
//	invoc 1 → implementer (commit; HEAD advances)
//	invoc 2 → reviewer    (REQUEST_CHANGES; routes back via back-edge)
//	invoc 3 → implementer (commit again)
//	invoc 4 → reviewer    (APPROVE; routes to close terminal)
//
// Two counter files are used: the total invocation counter (odd=implementer,
// even=reviewer) and a reviewer-only counter to distinguish the first reviewer
// visit (REQUEST_CHANGES) from subsequent ones (APPROVE).
func dotE2ERequestChangesHandlerScript(t *testing.T, stateDir string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "rc-handler.sh")
	counterPath := filepath.Join(stateDir, "rc-invocations")
	reviewerCounterPath := filepath.Join(stateDir, "rc-reviewer-count")
	script := "#!/bin/sh\n" +
		"set -e\n" +
		"CTR=\"" + counterPath + "\"\n" +
		"RCTR=\"" + reviewerCounterPath + "\"\n" +
		"N=0\n" +
		"if [ -f \"$CTR\" ]; then N=$(cat \"$CTR\"); fi\n" +
		"N=$((N+1))\n" +
		"echo \"$N\" > \"$CTR\"\n" +
		"REM=$((N % 2))\n" +
		"if [ \"$REM\" -eq 1 ]; then\n" +
		"  echo \"dot-cascade rc-impl $N $$\" > dot-cascade-rc-impl-$N.txt\n" +
		"  git add dot-cascade-rc-impl-$N.txt\n" +
		"  git commit -m \"dot-cascade: rc-impl commit $N\" >/dev/null 2>&1\n" +
		"else\n" +
		"  RN=0\n" +
		"  if [ -f \"$RCTR\" ]; then RN=$(cat \"$RCTR\"); fi\n" +
		"  RN=$((RN+1))\n" +
		"  echo \"$RN\" > \"$RCTR\"\n" +
		"  mkdir -p .harmonik\n" +
		"  if [ \"$RN\" -eq 1 ]; then\n" +
		"    printf '%s' '{\"schema_version\":1,\"verdict\":\"REQUEST_CHANGES\",\"flags\":[],\"notes\":\"rc back-edge e2e\"}' > .harmonik/review.json\n" +
		"  else\n" +
		"    printf '%s' '{\"schema_version\":1,\"verdict\":\"APPROVE\",\"flags\":[],\"notes\":\"approve after rc e2e\"}' > .harmonik/review.json\n" +
		"  fi\n" +
		"fi\n" +
		"exit 0\n"
	//nolint:gosec // G306: 0755 required to exec the handler script in a test tree.
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("dotE2ERequestChangesHandlerScript: write %s: %v", scriptPath, err)
	}
	return scriptPath
}

// dotE2EAlwaysRequestChangesHandlerScript returns a /bin/sh handler script that
// always emits REQUEST_CHANGES from the reviewer. This drives the
// reviewer→implementer back-edge until the traversal_cap=3 fires.
//
// Invocation sequence for review-loop.dot (cap=3, fires on the 4th back-edge
// attempt; total handler invocations: 4 implementer + 4 reviewer = 8):
//
//	invoc 1,3,5,7 → implementer (commit; HEAD advances)
//	invoc 2,4,6,8 → reviewer    (REQUEST_CHANGES every time)
//
// After invoc 8 the cascade tries to traverse the back-edge a 4th time; the
// cycle counter has already reached cap=3, so SelectNextEdge returns
// FailureClassCompilationLoop and driveDotWorkflow returns needsAttention=true.
// The bead is REOPENED (needs-attention), not closed.
func dotE2EAlwaysRequestChangesHandlerScript(t *testing.T, stateDir string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "cap-handler.sh")
	counterPath := filepath.Join(stateDir, "cap-invocations")
	script := "#!/bin/sh\n" +
		"set -e\n" +
		"CTR=\"" + counterPath + "\"\n" +
		"N=0\n" +
		"if [ -f \"$CTR\" ]; then N=$(cat \"$CTR\"); fi\n" +
		"N=$((N+1))\n" +
		"echo \"$N\" > \"$CTR\"\n" +
		"REM=$((N % 2))\n" +
		"if [ \"$REM\" -eq 1 ]; then\n" +
		"  echo \"dot-cascade cap-impl $N $$\" > dot-cascade-cap-impl-$N.txt\n" +
		"  git add dot-cascade-cap-impl-$N.txt\n" +
		"  git commit -m \"dot-cascade: cap-impl commit $N\" >/dev/null 2>&1\n" +
		"else\n" +
		"  mkdir -p .harmonik\n" +
		"  printf '%s' '{\"schema_version\":1,\"verdict\":\"REQUEST_CHANGES\",\"flags\":[],\"notes\":\"always rc for cap test\"}' > .harmonik/review.json\n" +
		"fi\n" +
		"exit 0\n"
	//nolint:gosec // G306: 0755 required to exec the handler script in a test tree.
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("dotE2EAlwaysRequestChangesHandlerScript: write %s: %v", scriptPath, err)
	}
	return scriptPath
}

// TestDotMode_E2E_RequestChangesBackEdge drives review-loop.dot through the REAL
// work loop with a handler that emits REQUEST_CHANGES on the first reviewer visit
// and APPROVE on the second. It asserts the reviewer→implementer back-edge is
// traversed exactly once before the run reaches the success terminal.
//
// Walk: start → implementer → reviewer(RC) → implementer → reviewer(APPROVE) → close
//
// Bead ref: hk-1xsyu (REQUEST_CHANGES back-edge E2E).
func TestDotMode_E2E_RequestChangesBackEdge(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	dotPath := dotE2ESeedWorkflow(t, projectDir, "specs/examples/review-loop.dot", ".harmonik/workflow.dot")

	const beadID = core.BeadID("hk-1xsyu-rc-back-edge-001")

	stateDir := t.TempDir()

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "dot-rc-back-edge-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:       beadID,
						Status:       queue.ItemStatusPending,
						WorkflowMode: string(core.WorkflowModeDot),
						WorkflowRef:  dotPath,
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	drainCtx, cancelDrain := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      dotE2ERequestChangesHandlerScript(t, stateDir),
		HandlerArgs:        nil,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
		CancelOnQueueExit:  cancelDrain,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, 90*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
	case <-time.After(88 * time.Second):
		t.Fatalf("DOT RC back-edge E2E: runWorkLoop did not exit; events=%v closed=%v reopened=%v",
			bus.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
	}

	events := bus.eventTypes()
	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("DOT RC back-edge E2E: events=%v closed=%v reopened=%v", events, closed, reopened)

	// Assertion 1: run_started fired.
	if !dotE2EContains(events, string(core.EventTypeRunStarted)) {
		t.Errorf("DOT RC back-edge E2E: run_started not emitted; got %v", events)
	}

	// Assertion 2: the cascade walked the graph — node_dispatch_requested and
	// node_dispatch_decided fired. These are only emitted by driveDotWorkflow.
	if !dotE2EContains(events, string(core.EventTypeNodeDispatchRequested)) {
		t.Errorf("DOT RC back-edge E2E: node_dispatch_requested not emitted; got %v", events)
	}
	if !dotE2EContains(events, string(core.EventTypeNodeDispatchDecided)) {
		t.Errorf("DOT RC back-edge E2E: node_dispatch_decided not emitted; got %v", events)
	}

	// Assertion 3: the bead was CLOSED (success terminal `close` reached after the
	// back-edge traversal and second-visit APPROVE). A reopen means the back-edge
	// or second-pass reviewer failed to reach the success terminal.
	if len(reopened) > 0 {
		t.Errorf("DOT RC back-edge E2E: bead was REOPENED (%v) — the cascade did not "+
			"reach the APPROVE success terminal after the REQUEST_CHANGES back-edge. "+
			"events=%v", reopened, events)
	}
	if len(closed) == 0 {
		t.Errorf("DOT RC back-edge E2E: bead was neither closed nor reopened; "+
			"expected close on the APPROVE terminal. events=%v", events)
	} else if closed[0] != beadID {
		t.Errorf("DOT RC back-edge E2E: closed bead = %q; want %q", closed[0], beadID)
	}

	// Assertion 4: run_completed fired (success terminal path).
	if !dotE2EContains(events, string(core.EventTypeRunCompleted)) {
		t.Errorf("DOT RC back-edge E2E: run_completed not emitted; got %v", events)
	}

	// Assertion 5: queue drained cleanly.
	if qs.Queue() != nil {
		t.Error("DOT RC back-edge E2E: QueueStore.Queue() is non-nil after drain; expected ClearQueue")
	}
}

// TestDotMode_E2E_CapHit drives review-loop.dot through the REAL work loop with
// a handler that always emits REQUEST_CHANGES from the reviewer. The
// traversal_cap=3 on the reviewer→implementer back-edge fires after 3 successful
// traversals; the 4th attempt returns FailureClassCompilationLoop and the daemon
// reopens the bead as needs-attention.
//
// Walk: start → impl → rev(RC) → impl → rev(RC) → impl → rev(RC) → impl → rev(RC) → [cap_hit] → reopen
//
// Total handler invocations: 8 (4 implementer + 4 reviewer). The cap fires when
// driveDotWorkflow's cascade engine checks the counter at back-edge attempt 4
// (count=3 >= cap=3).
//
// Bead ref: hk-1xsyu (cap-hit path E2E).
func TestDotMode_E2E_CapHit(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	dotPath := dotE2ESeedWorkflow(t, projectDir, "specs/examples/review-loop.dot", ".harmonik/workflow.dot")

	const beadID = core.BeadID("hk-1xsyu-cap-hit-001")

	stateDir := t.TempDir()

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "dot-cap-hit-queue",
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:       beadID,
						Status:       queue.ItemStatusPending,
						WorkflowMode: string(core.WorkflowModeDot),
						WorkflowRef:  dotPath,
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	exitCtx, cancelExit := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      dotE2EAlwaysRequestChangesHandlerScript(t, stateDir),
		HandlerArgs:        nil,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelExit,
		CancelOnQueueExit:  cancelExit,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	// Use a longer timeout: 8 handler invocations each involve a git commit.
	testCtx, testCancel := context.WithTimeout(exitCtx, 120*time.Second)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoop(testCtx, deps)
	}()

	// Poll for the reopen (cap-hit terminates the run with a reopen, which may
	// fire before the loop fully exits if the queue pauses on failure).
	deadline := time.After(118 * time.Second)
	for {
		if len(ledger.reopenedIDs()) > 0 {
			break
		}
		select {
		case <-loopDone:
			goto assertCapHit
		case <-deadline:
			t.Fatalf("DOT cap-hit E2E: timed out waiting for reopen; events=%v closed=%v reopened=%v",
				bus.eventTypes(), ledger.closedIDs(), ledger.reopenedIDs())
		case <-time.After(100 * time.Millisecond):
		}
	}

assertCapHit:
	testCancel()

	events := bus.eventTypes()
	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	t.Logf("DOT cap-hit E2E: events=%v closed=%v reopened=%v", events, closed, reopened)

	// Assertion 1: run_started fired.
	if !dotE2EContains(events, string(core.EventTypeRunStarted)) {
		t.Errorf("DOT cap-hit E2E: run_started not emitted; got %v", events)
	}

	// Assertion 2: the cascade walked the graph (multiple node_dispatch_requested
	// events fire across the 4 implementer + 4 reviewer invocations).
	if !dotE2EContains(events, string(core.EventTypeNodeDispatchRequested)) {
		t.Errorf("DOT cap-hit E2E: node_dispatch_requested not emitted; got %v", events)
	}
	if !dotE2EContains(events, string(core.EventTypeNodeDispatchDecided)) {
		t.Errorf("DOT cap-hit E2E: node_dispatch_decided not emitted; got %v", events)
	}

	// Assertion 3: the bead was REOPENED (needs-attention), not closed. The cap
	// fires when SelectNextEdge sees count=3 >= cap=3 and returns
	// FailureClassCompilationLoop; driveDotWorkflow returns needsAttention=true and
	// the daemon calls ReopenBead. A close here would mean the cap was not enforced.
	if len(closed) > 0 {
		t.Errorf("DOT cap-hit E2E: bead was CLOSED (%v) — the traversal_cap=3 was not "+
			"enforced; the run should have been reopened as needs-attention when the "+
			"4th back-edge traversal was attempted. events=%v", closed, events)
	}
	if len(reopened) == 0 {
		t.Errorf("DOT cap-hit E2E: bead was NOT reopened after cap hit; expected "+
			"ReopenBead on FailureClassCompilationLoop. events=%v", events)
	} else if reopened[0] != beadID {
		t.Errorf("DOT cap-hit E2E: reopened bead = %q; want %q", reopened[0], beadID)
	}

	// Assertion 4: run_failed fired (non-success terminal path).
	if !dotE2EContains(events, string(core.EventTypeRunFailed)) {
		t.Errorf("DOT cap-hit E2E: run_failed not emitted; got %v", events)
	}
}
