package daemon_test

// run_w3cp1_boiwe_hiqrl_test.go — happy-path tests for:
//
//	hk-w3cp1  harmonik run --beads id1,id2 --max-concurrent N
//	hk-boiwe  harmonik run --context <inline|@file>
//	hk-hiqrl  queue.Item.WorkflowMode (tier-0 per-item mode override)
//
// These tests exercise the workloop-level behaviour that the three CLI flags
// produce via queue.Item.Context and queue.Item.WorkflowMode fields.

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// workLoopDrainBudget is how long the work loop gets to drain its queue, and it
// is the timeout on the loop's own context.
//
// It used to be shadowed by a second, shorter deadline. Each wait in this file
// selected on a time.After nested inside a context that outlived it — four at
// 25s inside 30s, one at 15s inside 20s — so the inner timer always won and the
// context budget was decorative. That made
// every one of these tests a bet on a wall clock, and the bet loses under this
// package's parallel load: three different tests here —
// TestMultiBead_MaxConcurrentOne, TestMultiBead_TwoBeadsCompleteBothClose and
// TestSmoke_MultiBead_MaxConcurrent2_BothComplete — failed at ~25.2s on trees
// whose code was fine, while passing 20 out of 20 in isolation (hk-33e6p).
//
// Both budgets here are deliberately generous. They are backstops for a genuine
// hang, not measurements of how fast a drain ought to be. This one is still a
// bet — a real drain that runs past 90s fails, and it fails with a message that
// says the drain did not happen — but 90s is 3.6x the budget that was losing,
// and it is no longer shadowed by a shorter timer that always won first.
const workLoopDrainBudget = 90 * time.Second

// workLoopExitGrace is how long the loop then gets to actually RETURN once its
// context has been cancelled. Separate from the drain budget on purpose: a loop
// that drains and unwinds slowly is a different failure from one that never
// drains, and only the second is what these tests are about.
const workLoopExitGrace = 30 * time.Second

// awaitWorkLoopExit waits for the work loop to return and proves it returned for
// the RIGHT REASON.
//
// The discriminator is the subtle part, and the obvious version of it is wrong.
// These tests build testCtx as a child of workDone, and production signals a
// completed drain BY CANCELLING workDone. So a successful drain necessarily
// leaves testCtx.Err() non-nil, and "did the context get cancelled?" cannot tell
// success from failure — it is true either way. The parent tells them apart.
// A drain cancels workDone, so workDone.Err() is context.Canceled. A blown
// budget expires testCtx, the CHILD, and a child's deadline never propagates
// upward — so workDone.Err() stays nil. Both parents are WithCancel(Background),
// so context.DeadlineExceeded never appears on them at all: nil is the
// blown-budget signature, and a check written against DeadlineExceeded would
// pass on nil and restore the exact false pass this helper removes. Canceled is
// a fact about the work, not a reading of a clock, which is the point here.
//
// For the same reason the wait cannot select on testCtx.Done(): that fires the
// instant the drain cancels, which is exactly when the loop is being given the
// news and has not returned yet. One wall clock is still load-bearing —
// workLoopDrainBudget decides the verdict for a drain that runs past it — but it
// sits far above any plausible drain instead of below every one of them.
//
// workDone is the context production cancels when the work finishes (drainCtx or
// exitCtx). drained describes what the caller was waiting for.
func awaitWorkLoopExit(t *testing.T, workDone context.Context, loopDone <-chan error, drained string) {
	t.Helper()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
		// A loop whose budget simply expired also returns cleanly here. Without
		// this check that would read as success and the test would pass without
		// the queue ever draining.
		if !errors.Is(workDone.Err(), context.Canceled) {
			t.Fatalf("runWorkLoop exited, but %s did not happen (cancel context err = %v)",
				drained, workDone.Err())
		}
	case <-time.After(workLoopDrainBudget + workLoopExitGrace):
		t.Fatalf("runWorkLoop did not return within %s after %s",
			workLoopDrainBudget+workLoopExitGrace, drained)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-w3cp1 — multi-bead one-shot
// ─────────────────────────────────────────────────────────────────────────────

// TestMultiBead_TwoBeadsCompleteBothClose verifies that a two-item wave queue
// with max-concurrent=2 dispatches both beads and closes them both, then fires
// cancelOnQueueDrain (the harmonik run exit trigger).
//
// Spec ref: specs/execution-model.md §4.11 EM-049 (capacity gate: both items dispatch
// concurrently up to max_concurrent=2); §4.11 EM-051 (max_concurrent configuration).
// Bead ref: hk-w3cp1.
func TestMultiBead_TwoBeadsCompleteBothClose(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk-w3cp1-multi-a")
		beadB = core.BeadID("hk-w3cp1-multi-b")
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadA, Status: queue.ItemStatusPending},
					{BeadID: beadB, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}

	drainCtx, cancelDrain := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	// The fake agent commits during its run rather than in the worktree factory,
	// and the bead carries workflow:single. Both are load-bearing — see
	// workloopFixtureAdvanceHeadHandlerArgs and stubBeadLedger.labels.
	handlerArgs := workloopFixtureAdvanceHeadHandlerArgs(t)
	ledger := &stubBeadLedger{labels: workloopFixtureSingleLabels}

	p := daemon.TestRuntimeParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        handlerArgs, // each bead commits during its run, then succeeds
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		MaxConcurrent:      2, // hk-w3cp1: allow both items to dispatch concurrently
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
	}
	deps := daemon.ExportedTestRuntime(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, workLoopDrainBudget)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoopWithTestPorts(testCtx, deps, p)
	}()

	awaitWorkLoopExit(t, drainCtx, loopDone, "the two-bead queue drained (cancelOnQueueDrain not invoked)")

	// Both beads must have been closed.
	closed := ledger.closedIDs()
	if len(closed) < 2 {
		t.Errorf("expected 2 beads closed; got %d: %v", len(closed), closed)
	}

	// QueueStore must be nil after successful drain.
	if qs.Queue() != nil {
		t.Error("QueueStore.Queue() is non-nil after drain; expected ClearQueue to have run")
	}
}

// TestMultiBead_MaxConcurrentOne verifies that max-concurrent=1 still dispatches
// both beads (sequentially) and they both complete.
//
// Spec ref: specs/execution-model.md §4.11 EM-049 (capacity gate: at most 1 run
// in-flight); §4.11 EM-051 (max_concurrent default 1 — single-threaded behavior).
// Bead ref: hk-w3cp1.
func TestMultiBead_MaxConcurrentOne(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadA = core.BeadID("hk-w3cp1-serial-a")
		beadB = core.BeadID("hk-w3cp1-serial-b")
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadA, Status: queue.ItemStatusPending},
					{BeadID: beadB, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	drainCtx, cancelDrain := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	// The fake agent commits during its run rather than in the worktree factory,
	// and the bead carries workflow:single. Both are load-bearing — see
	// workloopFixtureAdvanceHeadHandlerArgs and stubBeadLedger.labels.
	handlerArgs := workloopFixtureAdvanceHeadHandlerArgs(t)
	ledger := &stubBeadLedger{labels: workloopFixtureSingleLabels}

	p := daemon.TestRuntimeParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        handlerArgs,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		MaxConcurrent:      1, // hk-w3cp1: serialised dispatch
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
	}
	deps := daemon.ExportedTestRuntime(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, workLoopDrainBudget)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoopWithTestPorts(testCtx, deps, p)
	}()

	awaitWorkLoopExit(t, drainCtx, loopDone, "the sequential two-bead queue drained")

	closed := ledger.closedIDs()
	if len(closed) < 2 {
		t.Errorf("expected 2 beads closed; got %d: %v", len(closed), closed)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-boiwe — per-item context injection
// ─────────────────────────────────────────────────────────────────────────────

// TestExtraContext_ItemFieldRoundTrip verifies that queue.Item.Context survives
// a JSON marshal/unmarshal round-trip and is preserved in the queue struct.
//
// Bead ref: hk-boiwe.
func TestExtraContext_ItemFieldRoundTrip(t *testing.T) {
	t.Parallel()

	const extraCtx = "predecessor: abc123; note: dependency landed in bf2db81"

	item := queue.Item{
		BeadID:  "hk-boiwe-ctx-001",
		Status:  queue.ItemStatusPending,
		Context: extraCtx,
	}

	// JSON round-trip.
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal queue.Item: %v", err)
	}
	var got queue.Item
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal queue.Item: %v", err)
	}
	if got.Context != extraCtx {
		t.Errorf("Context round-trip: got %q, want %q", got.Context, extraCtx)
	}
	if got.WorkflowMode != "" {
		t.Errorf("WorkflowMode should be empty (omitempty), got %q", got.WorkflowMode)
	}
}

// TestExtraContext_WorkloopSingleBead verifies the full workloop path: a queue
// item with Context set dispatches successfully and the bead is closed.
// The context field must not break dispatch or the bead close path.
//
// Bead ref: hk-boiwe.
func TestExtraContext_WorkloopSingleBead(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadID   = core.BeadID("hk-boiwe-wl-001")
		extraCtx = "predecessor: abc123; landing note from orchestrator"
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID:  beadID,
						Status:  queue.ItemStatusPending,
						Context: extraCtx, // hk-boiwe
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
	// The fake agent commits during its run rather than in the worktree factory,
	// and the bead carries workflow:single. Both are load-bearing — see
	// workloopFixtureAdvanceHeadHandlerArgs and stubBeadLedger.labels.
	handlerArgs := workloopFixtureAdvanceHeadHandlerArgs(t)
	ledger := &stubBeadLedger{labels: workloopFixtureSingleLabels}

	p := daemon.TestRuntimeParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        handlerArgs,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
	}
	deps := daemon.ExportedTestRuntime(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, workLoopDrainBudget)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoopWithTestPorts(testCtx, deps, p)
	}()

	awaitWorkLoopExit(t, drainCtx, loopDone, "the context-annotated bead drained")

	closed := ledger.closedIDs()
	if len(closed) == 0 {
		t.Fatal("expected bead to be closed; none were")
	}
	if closed[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closed[0], beadID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-hiqrl — the tier-0 per-item queue.Item.WorkflowMode override
// ─────────────────────────────────────────────────────────────────────────────
//
// These tests were written for the --review-loop flag, which set
// queue.Item.WorkflowMode = "review-loop". Both flag and mode are retired
// (EM-015d), but the field they exercised — the tier-0 per-item mode override —
// is live, so the tests carry a valid mode now instead of being deleted.

// TestQueueItemWorkflowMode_Field verifies that queue.Item.WorkflowMode carries a
// declared WorkflowMode and survives a JSON round-trip, and that the retired
// "review-loop" value is NOT accepted as a tier-0 override (beadRunOne applies
// the per-item mode only when candidate.Valid(), so a stale queue file degrades
// to the resolved mode instead of wedging).
//
// Bead ref: hk-hiqrl.
func TestQueueItemWorkflowMode_Field(t *testing.T) {
	t.Parallel()

	item := queue.Item{
		BeadID:       "hk-hiqrl-item-001",
		Status:       queue.ItemStatusPending,
		WorkflowMode: string(core.WorkflowModeDot),
	}

	if item.WorkflowMode != "dot" {
		t.Errorf("WorkflowMode = %q; want %q", item.WorkflowMode, "dot")
	}

	// Validate it is a recognised WorkflowMode constant.
	if mode := core.WorkflowMode(item.WorkflowMode); !mode.Valid() {
		t.Errorf("WorkflowMode %q is not a valid core.WorkflowMode", item.WorkflowMode)
	}

	// A queue file written before the retirement must not pass the tier-0
	// validity gate.
	if core.WorkflowMode(core.WorkflowModeRetiredReviewLoop).Valid() {
		t.Error("retired review-loop is still a valid tier-0 per-item override; want rejected")
	}

	// JSON round-trip.
	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got queue.Item
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.WorkflowMode != string(core.WorkflowModeDot) {
		t.Errorf("WorkflowMode round-trip: got %q, want %q", got.WorkflowMode, core.WorkflowModeDot)
	}
}

// TestQueueItemWorkflowMode_WorkloopHonoursItemMode verifies that when
// queue.Item.WorkflowMode is set, the workloop dispatches the bead in that mode
// and the bead reaches a terminal state (closed or reopened).
//
// The test wires both CancelOnQueueDrain (success path) and CancelOnQueueExit
// (failure/error path) so the loop exits on either outcome; the key assertion is
// that the bead transitions to a terminal state.
//
// Bead ref: hk-hiqrl.
func TestQueueItemWorkflowMode_WorkloopHonoursItemMode(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-hiqrl-rl-mode-001")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
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
						WorkflowMode: string(core.WorkflowModeDot), // hk-hiqrl
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}

	// Wire both cancel funcs: the bead may succeed (drain) or fail
	// (exit/error path). Either cancels exitCtx so the loop exits promptly.
	exitCtx, cancelExit := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	// The real hookSessionStore installed by ExportedTestRuntime will wait up
	// to stopHookGrace (3s) in WaitForOutcome. The handler exits 0; without a
	// real verdict file the run exits via its error path and reopens
	// the bead. Either closed or reopened is acceptable: both confirm the bead
	// reached a terminal state via per-item-mode dispatch (hk-ngw3d).
	p := daemon.TestRuntimeParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		CancelOnQueueDrain: cancelExit, // success path (APPROVE)
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueExit:  cancelExit, // failure/error path (BLOCK/error)
	}
	deps := daemon.ExportedTestRuntime(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, workLoopDrainBudget)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoopWithTestPorts(testCtx, deps, p)
	}()

	awaitWorkLoopExit(t, exitCtx, loopDone, "the per-item-mode bead drained")

	// Bead must be in a terminal state (closed or reopened). This ran inside the
	// select's success case before; it belongs after the wait, because the wait
	// now fails the test outright on every other path.
	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	if len(closed) == 0 && len(reopened) == 0 {
		t.Error("bead neither closed nor reopened; expected at least one terminal transition")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Smoke: harmonik run --beads X,Y --max-concurrent 2 completes both
// ─────────────────────────────────────────────────────────────────────────────

// TestSmoke_MultiBead_MaxConcurrent2_BothComplete is the smoke test from the
// bead brief: run --beads X,Y --max-concurrent 2 and verify both items complete.
//
// Spec ref: specs/execution-model.md §4.11 EM-049 (capacity gate); §4.11 EM-051
// (max_concurrent configuration: --max-concurrent 2 accepted and honored).
// Bead ref: hk-w3cp1 (smoke test requirement).
func TestSmoke_MultiBead_MaxConcurrent2_BothComplete(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const (
		beadX = core.BeadID("hk-w3cp1-smoke-x")
		beadY = core.BeadID("hk-w3cp1-smoke-y")
	)

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		SubmittedAt:   now,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: beadX, Status: queue.ItemStatusPending},
					{BeadID: beadY, Status: queue.ItemStatusPending},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}
	drainCtx, cancelDrain := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	// The fake agent commits during its run rather than in the worktree factory,
	// and the bead carries workflow:single. Both are load-bearing — see
	// workloopFixtureAdvanceHeadHandlerArgs and stubBeadLedger.labels.
	handlerArgs := workloopFixtureAdvanceHeadHandlerArgs(t)
	ledger := &stubBeadLedger{labels: workloopFixtureSingleLabels}

	p := daemon.TestRuntimeParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        handlerArgs,
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		MaxConcurrent:      2, // --max-concurrent 2
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		CancelOnQueueDrain: cancelDrain,
	}
	deps := daemon.ExportedTestRuntime(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, workLoopDrainBudget)
	defer testCancel()

	loopDone := make(chan error, 1)
	go func() {
		loopDone <- daemon.ExportedRunWorkLoopWithTestPorts(testCtx, deps, p)
	}()

	awaitWorkLoopExit(t, drainCtx, loopDone, "the smoke queue drained")

	closed := ledger.closedIDs()
	if len(closed) < 2 {
		t.Errorf("smoke: expected 2 beads closed; got %d: %v", len(closed), closed)
	}

	// QueueStore must be nil (CompleteAndUnlink ran).
	if qs.Queue() != nil {
		t.Error("smoke: QueueStore.Queue() is non-nil after all-success drain")
	}
}
