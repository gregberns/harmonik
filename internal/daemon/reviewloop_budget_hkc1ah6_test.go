package daemon_test

// reviewloop_budget_hkc1ah6_test.go — review-loop global retry-spend budget (hk-c1ah6).
//
// Verifies that when a queue-dispatched review-loop bead reaches MaxReviewLoopFailures,
// the workloop permanently closes the bead with needsAttention=true (CloseBead) instead
// of reopening it for another retry.
//
// # Scenario
//
// A queue item starts with ReviewLoopFailures = MaxReviewLoopFailures - 1 (one away
// from the budget limit). The handler exits without committing, which triggers the
// no_commit_during_implementer path (needsAttention=true, completionReason=error).
// The budget check increments ReviewLoopFailures to MaxReviewLoopFailures and fires
// the budget-exhausted branch: CloseBead(needsAttention=true) is called.
//
// # Assertions
//
//  1. CloseBead is called exactly once for the seeded bead.
//  2. CloseBead is called with needsAttention=true.
//  3. ReopenBead is NOT called.
//
// Helper prefix: rlBudget (per implementer-protocol.md §Helper-prefix discipline).
//
// Bead: hk-c1ah6.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

const rlBudgetBeadID core.BeadID = "rl-budget-test-001"

// rlBudgetLedger is a stub beadLedger that records CloseBead calls (including
// needsAttention) and ReopenBead calls.
type rlBudgetLedger struct {
	mu sync.Mutex

	// closeArgs records each CloseBead call as (beadID, needsAttention).
	closeArgs []rlBudgetCloseArg

	// reopenCount records how many times ReopenBead was called.
	reopenCount int
}

type rlBudgetCloseArg struct {
	beadID         core.BeadID
	needsAttention bool
}

func (l *rlBudgetLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	return []core.BeadRecord{}, nil
}

func (l *rlBudgetLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{BeadID: id, Status: core.CoarseStatusOpen}, nil
}

func (l *rlBudgetLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID) error {
	return nil
}

func (l *rlBudgetLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, beadID core.BeadID, needsAttention bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closeArgs = append(l.closeArgs, rlBudgetCloseArg{beadID: beadID, needsAttention: needsAttention})
	return nil
}

func (l *rlBudgetLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, _ core.BeadID, _ string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reopenCount++
	return nil
}

func (l *rlBudgetLedger) snapshotCloseArgs() []rlBudgetCloseArg {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]rlBudgetCloseArg, len(l.closeArgs))
	copy(out, l.closeArgs)
	return out
}

func (l *rlBudgetLedger) snapshotReopenCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.reopenCount
}

// rlBudgetProjectDir creates the minimal project directory tree and a git repo.
func rlBudgetProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Resolve symlinks — br rejects symlinked paths.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("rlBudgetProjectDir: EvalSymlinks %q: %v", dir, err)
	}
	//nolint:gosec // G301: test-only temp directory
	if mkErr := os.MkdirAll(filepath.Join(resolved, ".harmonik", "events"), 0o755); mkErr != nil {
		t.Fatalf("rlBudgetProjectDir: mkdir events: %v", mkErr)
	}
	//nolint:gosec // G301: test-only temp directory
	if mkErr := os.MkdirAll(filepath.Join(resolved, ".harmonik", "beads-intents"), 0o755); mkErr != nil {
		t.Fatalf("rlBudgetProjectDir: mkdir beads-intents: %v", mkErr)
	}
	workloopFixtureGitRepo(t, resolved)
	return resolved
}

// rlBudgetSeedQueue writes a queue.json with a single stream item that has
// ReviewLoopFailures = MaxReviewLoopFailures - 1 (one failure away from budget
// exhaustion) and WorkflowMode = "review-loop".
func rlBudgetSeedQueue(t *testing.T, projectDir string) *daemon.QueueStore {
	t.Helper()
	item := queue.Item{
		BeadID:             rlBudgetBeadID,
		Status:             queue.ItemStatusPending,
		WorkflowMode:       string(core.WorkflowModeReviewLoop),
		ReviewLoopFailures: queue.MaxReviewLoopFailures - 1,
	}
	grp := queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusActive,
		Items:      []queue.Item{item},
		CreatedAt:  time.Now(),
	}
	q := &queue.Queue{
		QueueID: "rl-budget-test-queue-0001",
		Status:  queue.QueueStatusActive,
		Groups:  []queue.Group{grp},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("rlBudgetSeedQueue: Persist: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	lq := qs.LockForMutation()
	lq.SetQueue(q)
	lq.Done()
	return qs
}

// ─────────────────────────────────────────────────────────────────────────────
// TestReviewLoopBudget_ExhaustionCallsCloseBead
// ─────────────────────────────────────────────────────────────────────────────

// TestReviewLoopBudget_ExhaustionCallsCloseBead verifies that a queue-dispatched
// review-loop bead is permanently closed (CloseBead needsAttention=true) when the
// ReviewLoopFailures counter reaches MaxReviewLoopFailures.
//
// The handler exits without committing, triggering the no_commit path
// (needsAttention=true). Since the item starts with ReviewLoopFailures =
// MaxReviewLoopFailures - 1, the increment pushes it to the limit and the
// budget-exhausted branch fires.
//
// Bead: hk-c1ah6.
func TestReviewLoopBudget_ExhaustionCallsCloseBead(t *testing.T) {
	projectDir := rlBudgetProjectDir(t)

	// Redirect claude config to a test-local path to avoid contention on
	// ~/.claude.json.lock from the production daemon or concurrent test runs.
	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	qs := rlBudgetSeedQueue(t, projectDir)
	ledger := &rlBudgetLedger{}

	// cancelOnQueueExit cancels dispatchCtx once the queue pauses by failure,
	// unblocking workloopIdleWait so ExportedRunWorkLoop returns cleanly.
	dispatchCtx, dispatchCancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer dispatchCancel()

	// Empty sealed registry — ForAgent returns an error, causing waitAgentReady
	// to be skipped. The handler exits immediately so no agent_ready is needed.
	bus := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		HandlerBinary:    "/bin/sh",
		HandlerArgs:      []string{"-c", "exit 0"},
		QueueStore:       qs,
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		// CancelOnQueueExit cancels dispatchCtx when the queue pauses by failure.
		CancelOnQueueExit: dispatchCancel,
	})

	if err := daemon.ExportedRunWorkLoop(dispatchCtx, deps); err != nil {
		t.Fatalf("ExportedRunWorkLoop: %v", err)
	}

	// Assert: CloseBead called once with needsAttention=true.
	closeArgs := ledger.snapshotCloseArgs()
	if len(closeArgs) == 0 {
		t.Fatal("expected CloseBead to be called (budget exhausted), but it was not called")
	}
	if len(closeArgs) > 1 {
		t.Fatalf("expected CloseBead called exactly once, got %d calls", len(closeArgs))
	}
	if closeArgs[0].beadID != rlBudgetBeadID {
		t.Fatalf("CloseBead called for wrong bead: got %q, want %q", closeArgs[0].beadID, rlBudgetBeadID)
	}
	if !closeArgs[0].needsAttention {
		t.Fatalf("CloseBead called with needsAttention=false; expected true for budget-exhausted close")
	}

	// Assert: ReopenBead not called.
	if reopened := ledger.snapshotReopenCount(); reopened > 0 {
		t.Fatalf("expected ReopenBead NOT to be called, but it was called %d time(s)", reopened)
	}
}

// TestReviewLoopBudget_BelowLimitCallsReopenBead verifies that when
// ReviewLoopFailures is below MaxReviewLoopFailures after a failure, the bead
// is reopened (not permanently closed).
//
// Bead: hk-c1ah6.
func TestReviewLoopBudget_BelowLimitCallsReopenBead(t *testing.T) {
	projectDir := rlBudgetProjectDir(t)

	claudeConfigPath := filepath.Join(t.TempDir(), ".claude.json")
	t.Setenv("HARMONIK_CLAUDE_CONFIG_PATH", claudeConfigPath)

	// Seed with ReviewLoopFailures = 0 (well below the limit).
	item := queue.Item{
		BeadID:             rlBudgetBeadID,
		Status:             queue.ItemStatusPending,
		WorkflowMode:       string(core.WorkflowModeReviewLoop),
		ReviewLoopFailures: 0,
	}
	grp := queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusActive,
		Items:      []queue.Item{item},
		CreatedAt:  time.Now(),
	}
	q := &queue.Queue{
		QueueID: "rl-budget-below-limit-0001",
		Status:  queue.QueueStatusActive,
		Groups:  []queue.Group{grp},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	qs := daemon.ExportedNewQueueStore()
	lq := qs.LockForMutation()
	lq.SetQueue(q)
	lq.Done()

	ledger := &rlBudgetLedger{}

	dispatchCtx, dispatchCancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer dispatchCancel()

	bus := &stubEventCollector{}

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:         ledger,
		Bus:               bus,
		ProjectDir:        projectDir,
		IntentLogDir:      filepath.Join(projectDir, ".harmonik", "beads-intents"),
		HandlerBinary:     "/bin/sh",
		HandlerArgs:       []string{"-c", "exit 0"},
		QueueStore:        qs,
		AdapterRegistry2:  NewEmptySealedAdapterRegistryForTest(t),
		CancelOnQueueExit: dispatchCancel,
	})

	if err := daemon.ExportedRunWorkLoop(dispatchCtx, deps); err != nil {
		t.Fatalf("ExportedRunWorkLoop: %v", err)
	}

	// Assert: ReopenBead called (budget not exhausted).
	if reopened := ledger.snapshotReopenCount(); reopened == 0 {
		t.Fatal("expected ReopenBead to be called (budget not exhausted), but it was not called")
	}

	// Assert: CloseBead not called (bead should be reopened, not permanently closed).
	if closeArgs := ledger.snapshotCloseArgs(); len(closeArgs) > 0 {
		t.Fatalf("expected CloseBead NOT to be called, but it was called for: %v", closeArgs)
	}
}
