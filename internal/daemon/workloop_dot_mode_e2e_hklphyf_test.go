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
// # HONEST STATUS of --workflow-mode=dot through the daemon (as of this commit)
//
// The daemon's DOT-mode branch in internal/daemon/workloop.go (beadRunOne,
// `case core.WorkflowModeDot:`, ~L1260) does exactly THREE things today:
//
//   1. Resolves the .dot path: itemWorkflowRef (CLI --workflow-ref) when set,
//      else <projectDir>/workflow.dot. Relative refs resolve against projectDir.
//   2. Calls workflow.LoadDotWorkflow(dotPath) — parse + validate. On failure it
//      reopens the bead with failure_class=workflow_load.
//   3. On success it logs "(cascade engine not yet wired — hk-bf85t)" and
//      FALLS THROUGH to single-mode dispatch.
//
// In other words: the DOT graph is loaded and validated, then the FIRST node is
// effectively dispatched as a one-shot single-mode run. The cascade engine
// (node transitions, conditional edge evaluation, reviewer back-edge, terminal-
// node walk) is NOT wired into the daemon — that is the open TODO(hk-bf85t).
//
// So the assertions below split into two tiers:
//
//   TIER 1 (real, asserted): boot work loop → DOT load+validate → run_started →
//   single-mode dispatch → bead closed → queue drains. This is the genuine
//   extent of the real DOT-mode daemon path today.
//
//   TIER 2 (unreached, documented + t.Skip): the cascade — node-transition
//   events, reviewer verdict routing, REQUEST_CHANGES back-edge, terminal-node
//   walk to `close` / `close-needs-attention`. These cannot be asserted because
//   the daemon never hands the loaded graph to a cascade driver. The
//   conditional-before-unconditional edge ordering fix (commit b7e23bc) is
//   exercised by internal/workflow's scenario_roundtrip_em75 tests, NOT by the
//   daemon, because the daemon never evaluates edges.
//
// GATE nodes are deliberately NOT exercised (per the bead brief): gate
// deny/success semantics are under an unresolved spec contradiction (EM-005b vs
// CP-058). The review-loop topology used here is agentic + non-agentic +
// reviewer nodes only — no gate nodes.
//
// Spec refs: specs/execution-model.md §EM-015d (review-loop topology);
// specs/workflow-graph.md §8 (terminal-node declaration); specs/examples/review-loop.dot.
// Bead ref: hk-lphyf (DOT scenario round-trip); hk-bf85t (cascade-engine TODO);
// hk-qo9pq (--workflow-ref CLI wiring); hk-waj4b (DOT-mode load+validate branch).

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

// dotE2ECommitHandlerScript returns a /bin/sh script (written to a temp file)
// that, when launched with WorkDir = the run worktree, creates a file and
// commits it — advancing the worktree HEAD past its parent — then exits 0.
//
// This is the minimal deterministic handler that lets the single-mode
// fall-through path (the DOT-mode branch falls through to single-mode after
// load+validate) reach the clean-close branch (workloop.go ~L1741): HEAD
// advanced + exit 0 + no watcher error → mergeRunBranchToMain (FF to main) →
// CloseBead. A bare `exit 0` handler instead trips the no-commit guard
// (workloop.go ~L1710) and reopens, so it cannot prove the close path.
func dotE2ECommitHandlerScript(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "commit-handler.sh")
	// The handler runs in the worktree (its WorkDir). Commit a sentinel so HEAD
	// advances; the run branch already has user.name/email from the fixture repo.
	script := "#!/bin/sh\n" +
		"set -e\n" +
		"echo \"dot-e2e sentinel $$\" > dot-e2e-sentinel.txt\n" +
		"git add dot-e2e-sentinel.txt\n" +
		"git commit -m \"dot-e2e: handler commit\" >/dev/null 2>&1\n" +
		"exit 0\n"
	//nolint:gosec // G306: 0755 required to exec the handler script in a test tree.
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("dotE2ECommitHandlerScript: write %s: %v", scriptPath, err)
	}
	return scriptPath
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
// deterministic /bin/sh handler that exits 0.
//
// This is the honest end-to-end extent of the daemon DOT-mode path today:
//
//   - The queue item carries WorkflowMode="dot" + WorkflowRef=<review-loop.dot>,
//     exactly as `harmonik run --workflow-mode dot --workflow-ref` populates it.
//   - beadRunOne resolves the ref, calls workflow.LoadDotWorkflow (parse +
//     validate the FULL review-loop topology: start, implementer, reviewer,
//     close, close-needs-attention + 6 edges including the conditional cascade).
//   - On successful load it falls through to single-mode dispatch.
//   - The handler exits 0, the bead is CLOSED (not reopened), the queue drains,
//     and cancelOnQueueDrain fires so the loop exits.
//
// The key signal: the bead is CLOSED, not REOPENED. A reopen would mean the DOT
// artifact failed to load/validate (failure_class=workflow_load). A close means
// the real path got all the way through load+validate+dispatch.
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
						WorkflowRef:  dotPath,                       // --workflow-ref <path>
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
		HandlerBinary:      dotE2ECommitHandlerScript(t), // commits in worktree → HEAD advances
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
// TIER 2 — cascade (NOT wired into the daemon today): documented + skipped
// ─────────────────────────────────────────────────────────────────────────────

// TestDotMode_E2E_CascadeTransitions_NOT_WIRED documents — and guards with an
// explicit t.Skip — the assertions that CANNOT pass today because the daemon
// never hands the loaded DOT graph to a cascade driver.
//
// If a future change wires hk-bf85t (the cascade engine) into beadRunOne, this
// test should be un-skipped and fleshed out to assert the genuine node-transition
// sequence. Until then it stands as an honest, machine-discoverable marker that
// the DOT cascade does NOT execute through the daemon.
//
// The two paths that a wired cascade would need to exercise (per the bead brief):
//
//   PATH A — APPROVE:  start → implementer → reviewer → (APPROVE) → close
//   PATH B — back-edge: start → implementer → reviewer → (REQUEST_CHANGES) →
//            implementer → reviewer → (APPROVE) → close
//
// PATH B exercises the conditional-before-unconditional edge ordering fix
// (commit b7e23bc): the reviewer cascade must evaluate the conditional
// REQUEST_CHANGES back-edge BEFORE the unconditional fallback to
// close-needs-attention. That ordering is verified TODAY in
// internal/workflow/scenario_roundtrip_em75_hklphyf_test.go
// (TestScenarioEM75_CascadeFallback_ReviewerUnconditional) at the cascade-engine
// layer — but NOT through the daemon, because the daemon does not evaluate edges.
func TestDotMode_E2E_CascadeTransitions_NOT_WIRED(t *testing.T) {
	t.Skip("DOT cascade engine is not wired into the daemon work loop (TODO hk-bf85t). " +
		"beadRunOne's WorkflowModeDot branch loads+validates the .dot graph then falls " +
		"through to single-mode dispatch — it never evaluates node transitions or edge " +
		"conditions. Node-transition / reviewer-verdict / back-edge / terminal-node-walk " +
		"assertions cannot be made against the daemon until the cascade driver lands. " +
		"The cascade itself (incl. the conditional-before-unconditional ordering fix, " +
		"commit b7e23bc) is covered at the engine layer by " +
		"internal/workflow/scenario_roundtrip_em75_hklphyf_test.go.")
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
