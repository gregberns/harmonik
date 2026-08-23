package daemon_test

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

const workLoopDrainBudget = 90 * time.Second

const workLoopExitGrace = 30 * time.Second

func awaitWorkLoopExit(t *testing.T, workDone context.Context, loopDone <-chan error, drained string) {
	t.Helper()
	select {
	case err := <-loopDone:
		if err != nil {
			t.Errorf("runWorkLoop returned non-nil error: %v", err)
		}
		if !errors.Is(workDone.Err(), context.Canceled) {
			t.Fatalf("runWorkLoop exited, but %s did not happen (cancel context err = %v)",
				drained, workDone.Err())
		}
	case <-time.After(workLoopDrainBudget + workLoopExitGrace):
		t.Errorf("runWorkLoop did not return within %s after %s",
			workLoopDrainBudget+workLoopExitGrace, drained)
		select {
		case <-loopDone:
		case <-time.After(workLoopExitGrace):
			t.Errorf("runWorkLoop still had not returned %s later; the project dir is about to be deleted under a running loop, so treat any persist or no-such-file error in a LATER test in this package as fallout from THIS failure, not as a defect of its own",
				workLoopExitGrace)
		}
		t.FailNow()
	}
}

// TestMultiBead_TwoBeadsCompleteBothClose verifies that a two-item wave queue
// with max-concurrent=2 dispatches both beads and closes them both, then fires
// cancelOnQueueDrain (the harmonik run exit trigger).
//
// Spec ref: specs/execution-model.md §4.11 EM-049 (capacity gate: both items dispatch
// concurrently up to max_concurrent=2); §4.11 EM-051 (max_concurrent configuration).
// Bead ref: hk-w3cp1.
func TestMultiBead_TwoBeadsCompleteBothClose(t *testing.T) {
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

	closed := ledger.closedIDs()
	if len(closed) < 2 {
		t.Errorf("expected 2 beads closed; got %d: %v", len(closed), closed)
	}

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

	if mode := core.WorkflowMode(item.WorkflowMode); !mode.Valid() {
		t.Errorf("WorkflowMode %q is not a valid core.WorkflowMode", item.WorkflowMode)
	}

	if core.WorkflowMode(core.WorkflowModeRetiredReviewLoop).Valid() {
		t.Error("retired review-loop is still a valid tier-0 per-item override; want rejected")
	}

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

	exitCtx, cancelExit := context.WithCancel(context.Background())

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)
	ledger := &stubBeadLedger{}

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

	closed := ledger.closedIDs()
	reopened := ledger.reopenedIDs()
	if len(closed) == 0 && len(reopened) == 0 {
		t.Error("bead neither closed nor reopened; expected at least one terminal transition")
	}
}

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

	if qs.Queue() != nil {
		t.Error("smoke: QueueStore.Queue() is non-nil after all-success drain")
	}
}
