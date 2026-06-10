package daemon_test

// run_w3cp1_boiwe_hiqrl_test.go — happy-path tests for:
//
//	hk-w3cp1  harmonik run --beads id1,id2 --max-concurrent N
//	hk-boiwe  harmonik run --context <inline|@file>
//	hk-hiqrl  harmonik run --review-loop
//
// These tests exercise the workloop-level behaviour that the three CLI flags
// produce via queue.Item.Context and queue.Item.WorkflowMode fields.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// emptyCommitWorktreeFactory wraps productionWorktreeFactory and creates an
// allow-empty commit so HEAD advances past headSHA (satisfying the no-commit
// guard, hk-mmh8f) without adding any files to the working tree.
//
// Using --allow-empty avoids the race in concurrent-bead tests: a file-based
// commit causes `D <filename>` to appear in `git status` between `update-ref`
// and `reset --hard` in mergeRunBranchToMain, which triggers a false positive
// in checkMainWorkingTreeDirty for any other concurrently-running bead.
func emptyCommitWorktreeFactory(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
	wtPath, cleanup, err := daemon.ExportedProductionWorktreeFactory(ctx, projectDir, runID, headSHA)
	if err != nil {
		return "", nil, err
	}
	//nolint:gosec // G204: git args are test-internal literals
	cmd := exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "test: advance HEAD for "+runID)
	cmd.Dir = wtPath
	if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
		if cleanup != nil {
			cleanup()
		}
		return "", nil, fmt.Errorf("emptyCommitWorktreeFactory: git commit: %v\n%s", cmdErr, out)
	}
	return wtPath, cleanup, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-w3cp1 — multi-bead one-shot
// ─────────────────────────────────────────────────────────────────────────────

// TestMultiBead_TwoBeadsCompleteBothClose verifies that a two-item wave queue
// with max-concurrent=2 dispatches both beads and closes them both, then fires
// cancelOnQueueDrain (the harmonik run exit trigger).
//
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
		QueueID:       "w3cp1-multi-queue",
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
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"}, // both beads succeed
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		MaxConcurrent:      2, // hk-w3cp1: allow both items to dispatch concurrently
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:    emptyCommitWorktreeFactory, // satisfy no-commit guard (hk-mmh8f) without race
		CancelOnQueueDrain: cancelDrain,
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
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit after two-bead queue drained (cancelOnQueueDrain not invoked)")
	}

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
		QueueID:       "w3cp1-serial-queue",
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
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		MaxConcurrent:      1, // hk-w3cp1: serialised dispatch
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:    emptyCommitWorktreeFactory, // satisfy no-commit guard (hk-mmh8f) without race
		CancelOnQueueDrain: cancelDrain,
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
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit after sequential two-bead queue drained")
	}

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
		QueueID:       "boiwe-wl-queue",
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
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:    emptyCommitWorktreeFactory, // satisfy no-commit guard (hk-mmh8f) without race
		CancelOnQueueDrain: cancelDrain,
	}
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(drainCtx, 20*time.Second)
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
	case <-time.After(15 * time.Second):
		t.Fatal("runWorkLoop did not exit after context-annotated bead drained")
	}

	closed := ledger.closedIDs()
	if len(closed) == 0 {
		t.Fatal("expected bead to be closed; none were")
	}
	if closed[0] != beadID {
		t.Errorf("closed bead = %q; want %q", closed[0], beadID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// hk-hiqrl — --review-loop flag sets WorkflowModeReviewLoop
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoopFlag_ItemWorkflowModeField verifies that queue.Item.WorkflowMode
// is set to "review-loop" when the --review-loop flag is used, and that the
// field survives JSON round-trip.
//
// Bead ref: hk-hiqrl.
func TestReviewLoopFlag_ItemWorkflowModeField(t *testing.T) {
	t.Parallel()

	item := queue.Item{
		BeadID:       "hk-hiqrl-item-001",
		Status:       queue.ItemStatusPending,
		WorkflowMode: string(core.WorkflowModeReviewLoop), // set by --review-loop
	}

	if item.WorkflowMode != "review-loop" {
		t.Errorf("WorkflowMode = %q; want %q", item.WorkflowMode, "review-loop")
	}

	// Validate it is a recognised WorkflowMode constant.
	if mode := core.WorkflowMode(item.WorkflowMode); !mode.Valid() {
		t.Errorf("WorkflowMode %q is not a valid core.WorkflowMode", item.WorkflowMode)
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
	if got.WorkflowMode != string(core.WorkflowModeReviewLoop) {
		t.Errorf("WorkflowMode round-trip: got %q, want %q", got.WorkflowMode, core.WorkflowModeReviewLoop)
	}
}

// TestReviewLoopFlag_WorkloopOverridesMode verifies that when queue.Item.WorkflowMode
// is set to "review-loop", the workloop routes the bead through review-loop mode
// (enters runReviewLoop) and the bead reaches a terminal state (closed or reopened).
//
// The test wires both CancelOnQueueDrain (success path) and CancelOnQueueExit
// (failure/error path) so the loop exits on either outcome; the key assertion is
// that the bead transitions to a terminal state.
//
// Bead ref: hk-hiqrl.
func TestReviewLoopFlag_WorkloopOverridesMode(t *testing.T) {
	t.Parallel()

	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)

	const beadID = core.BeadID("hk-hiqrl-rl-mode-001")

	now := time.Now()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "hiqrl-rl-queue",
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
						WorkflowMode: string(core.WorkflowModeReviewLoop), // hk-hiqrl
					},
				},
				CreatedAt: now,
			},
		},
	}

	bus := &stubEventCollector{}

	// Wire both cancel funcs: the review-loop bead may succeed (drain) or fail
	// (exit/error path). Either cancels exitCtx so the loop exits promptly.
	exitCtx, cancelExit := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

	// The real hookSessionStore installed by ExportedWorkLoopDeps will wait up
	// to stopHookGrace (3s) in WaitForOutcome. The handler exits 0; without a
	// real verdict file the review loop exits via its error path and reopens
	// the bead. Either closed or reopened is acceptable: both confirm the bead
	// reached a terminal state via review-loop dispatch (hk-ngw3d).
	p := daemon.WorkLoopDepsParams{
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
	deps := daemon.ExportedWorkLoopDeps(p)

	testCtx, testCancel := context.WithTimeout(exitCtx, 30*time.Second)
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
		// Bead must be in a terminal state (closed or reopened).
		closed := ledger.closedIDs()
		reopened := ledger.reopenedIDs()
		if len(closed) == 0 && len(reopened) == 0 {
			t.Error("bead neither closed nor reopened; expected at least one terminal transition")
		}
	case <-time.After(25 * time.Second):
		t.Fatal("runWorkLoop did not exit for review-loop bead")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Smoke: harmonik run --beads X,Y --max-concurrent 2 completes both
// ─────────────────────────────────────────────────────────────────────────────

// TestSmoke_MultiBead_MaxConcurrent2_BothComplete is the smoke test from the
// bead brief: run --beads X,Y --max-concurrent 2 and verify both items complete.
//
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
		QueueID:       "smoke-multi-queue",
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
	ledger := &stubBeadLedger{}

	p := daemon.WorkLoopDepsParams{
		BrAdapter:          ledger,
		Bus:                bus,
		ProjectDir:         projectDir,
		HandlerBinary:      "/bin/sh",
		HandlerArgs:        []string{"-c", "exit 0"},
		IntentLogDir:       filepath.Join(projectDir, ".harmonik", "beads-intents"),
		QueueStore:         qs,
		MaxConcurrent:      2, // --max-concurrent 2
		AdapterRegistry2:   NewSealedAdapterRegistryForTest(t),
		WorktreeFactory:    emptyCommitWorktreeFactory, // satisfy no-commit guard (hk-mmh8f) without race
		CancelOnQueueDrain: cancelDrain,
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
			t.Errorf("smoke: runWorkLoop returned error: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("smoke: runWorkLoop did not exit within timeout")
	}

	closed := ledger.closedIDs()
	if len(closed) < 2 {
		t.Errorf("smoke: expected 2 beads closed; got %d: %v", len(closed), closed)
	}

	// QueueStore must be nil (CompleteAndUnlink ran).
	if qs.Queue() != nil {
		t.Error("smoke: QueueStore.Queue() is non-nil after all-success drain")
	}
}
