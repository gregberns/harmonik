package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// ──────────────────────────────────────────────────────────────────────────────
// Test fixtures (daemonOrphanSweep prefix — hk-s3lav)
// ──────────────────────────────────────────────────────────────────────────────

// daemonOrphanSweepTempProjectDir creates a temporary directory that contains
// an initialised .harmonik/ sub-directory, suitable as a project root.
func daemonOrphanSweepTempProjectDir(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	harmonikDir := filepath.Join(root, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("daemonOrphanSweepTempProjectDir: MkdirAll .harmonik: %v", err)
	}
	return root
}

// daemonOrphanSweepIsPidLive probes whether the given PID is a live process
// via kill(pid, 0). Returns false on ESRCH, true on nil or EPERM.
func daemonOrphanSweepIsPidLive(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if err == syscall.ESRCH {
		return false
	}
	return true // EPERM: process exists but we cannot signal it
}

// daemonOrphanSweepFakeTmuxLister is a TmuxSessionLister that returns a fixed
// list on the first call and an empty list on subsequent calls (simulating
// sessions exiting after kill, which avoids waiting out tmuxPollCeiling).
type daemonOrphanSweepFakeTmuxLister struct {
	sessions  []string
	callCount int
}

func (f *daemonOrphanSweepFakeTmuxLister) ListTmuxSessions(_ context.Context) ([]string, error) {
	f.callCount++
	if f.callCount > 1 {
		return nil, nil
	}
	return f.sessions, nil
}

// daemonOrphanSweepFakeTmuxKiller records kill calls without invoking tmux.
type daemonOrphanSweepFakeTmuxKiller struct {
	killed []string
}

func (f *daemonOrphanSweepFakeTmuxKiller) KillTmuxSession(_ context.Context, name string) error {
	f.killed = append(f.killed, name)
	return nil
}

// daemonOrphanSweepFakeHandlerLister is an injectable HandlerProcessLister.
type daemonOrphanSweepFakeHandlerLister struct {
	pids []int
	err  error
}

func (f *daemonOrphanSweepFakeHandlerLister) ListOrphanHandlerPIDs(_ context.Context, _ core.ProjectHash) ([]int, error) {
	return f.pids, f.err
}

// daemonOrphanSweepFakeBrLister is an injectable ProcessLister for br processes.
type daemonOrphanSweepFakeBrLister struct {
	pids []int
}

func (f *daemonOrphanSweepFakeBrLister) ListOrphanBrPIDs(_ context.Context) ([]int, error) {
	return f.pids, nil
}

// daemonOrphanSweepSeedStaleIntent creates a stale intent file under
// .harmonik/beads-intents/ with mtime set to 15 minutes ago.
func daemonOrphanSweepSeedStaleIntent(t *testing.T, projectDir, intentID string) string {
	t.Helper()

	intentsDir := filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(intentsDir, 0o755); err != nil {
		t.Fatalf("daemonOrphanSweepSeedStaleIntent: MkdirAll: %v", err)
	}

	intentPath := filepath.Join(intentsDir, intentID+".json")
	content := fmt.Sprintf(`{"intent_id":%q,"bead_id":"br-0001","created_at":%q}`,
		intentID, time.Now().Add(-15*time.Minute).Format(time.RFC3339))
	if err := os.WriteFile(intentPath, []byte(content), 0o600); err != nil {
		t.Fatalf("daemonOrphanSweepSeedStaleIntent: WriteFile: %v", err)
	}
	past := time.Now().Add(-15 * time.Minute)
	if err := os.Chtimes(intentPath, past, past); err != nil {
		t.Fatalf("daemonOrphanSweepSeedStaleIntent: Chtimes: %v", err)
	}
	return intentPath
}

// daemonOrphanSweepSeedReconciliationLock creates a fake reconciliation lock
// file at .harmonik/reconciliation-locks/<name>.lock with the given creatorPID.
func daemonOrphanSweepSeedReconciliationLock(t *testing.T, projectDir, name string, creatorPID int, verdictExecuted bool) string {
	t.Helper()

	lockDir := filepath.Join(projectDir, ".harmonik", "reconciliation-locks")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("daemonOrphanSweepSeedReconciliationLock: MkdirAll: %v", err)
	}

	lockPath := filepath.Join(lockDir, name+".lock")
	verdictLine := ""
	if verdictExecuted {
		verdictLine = "Harmonik-Verdict-Executed: true\n"
	}
	content := fmt.Sprintf("creator_pid=%d\nrun_id=%s\n%s", creatorPID, name, verdictLine)
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		t.Fatalf("daemonOrphanSweepSeedReconciliationLock: WriteFile: %v", err)
	}
	return lockPath
}

// ──────────────────────────────────────────────────────────────────────────────
// OrphanSweepResult → payload conversion
// ──────────────────────────────────────────────────────────────────────────────

// TestPL006_OrphanSweepResult_ToPayload verifies that OrphanSweepResult.ToPayload
// maps all fields correctly to the core payload type.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — "emit daemon_orphan_sweep_completed
// with counts of tmux sessions killed, locks cleared, handler subprocesses killed,
// br subprocesses killed, reconciliation lock files removed, and stale intents
// observed."
func TestPL006_OrphanSweepResult_ToPayload(t *testing.T) {
	t.Parallel()

	sweepTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	result := OrphanSweepResult{
		TmuxSessionsKilled:         3,
		LocksCleared:               2,
		SubprocessesKilled:         1,
		BrSubprocessesKilled:       4,
		ReconciliationLocksRemoved: 5,
		StaleIntentsObserved:       6,
		SweptAt:                    sweepTime,
	}

	payload := result.ToPayload()

	if payload.TmuxSessionsKilled != 3 {
		t.Errorf("ToPayload: TmuxSessionsKilled = %d, want 3", payload.TmuxSessionsKilled)
	}
	if payload.LocksCleared != 2 {
		t.Errorf("ToPayload: LocksCleared = %d, want 2", payload.LocksCleared)
	}
	if payload.SubprocessesKilled != 1 {
		t.Errorf("ToPayload: SubprocessesKilled = %d, want 1", payload.SubprocessesKilled)
	}
	if payload.BrSubprocessesKilled != 4 {
		t.Errorf("ToPayload: BrSubprocessesKilled = %d, want 4", payload.BrSubprocessesKilled)
	}
	if payload.ReconciliationLocksRemoved != 5 {
		t.Errorf("ToPayload: ReconciliationLocksRemoved = %d, want 5", payload.ReconciliationLocksRemoved)
	}
	if payload.StaleIntentsObserved != 6 {
		t.Errorf("ToPayload: StaleIntentsObserved = %d, want 6", payload.StaleIntentsObserved)
	}
	if payload.SweptAt != "2026-01-02T03:04:05Z" {
		t.Errorf("ToPayload: SweptAt = %q, want %q", payload.SweptAt, "2026-01-02T03:04:05Z")
	}
	if !payload.Valid() {
		t.Error("ToPayload: payload.Valid() = false, want true")
	}
}

// TestPL006_DaemonOrphanSweepCompletedPayload_ValidNewFields verifies the Valid
// function rejects negative values in the new fields added to the payload struct.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 event bullet + event-model.md §8.7.14.
func TestPL006_DaemonOrphanSweepCompletedPayload_ValidNewFields(t *testing.T) {
	t.Parallel()

	base := OrphanSweepResult{SweptAt: time.Now()}.ToPayload()

	t.Run("negative-br-subprocesses-killed", func(t *testing.T) {
		t.Parallel()
		p := base
		p.BrSubprocessesKilled = -1
		if p.Valid() {
			t.Error("Valid: expected false for BrSubprocessesKilled = -1, got true")
		}
	})

	t.Run("negative-reconciliation-locks-removed", func(t *testing.T) {
		t.Parallel()
		p := base
		p.ReconciliationLocksRemoved = -1
		if p.Valid() {
			t.Error("Valid: expected false for ReconciliationLocksRemoved = -1, got true")
		}
	})

	t.Run("negative-stale-intents-observed", func(t *testing.T) {
		t.Parallel()
		p := base
		p.StaleIntentsObserved = -1
		if p.Valid() {
			t.Error("Valid: expected false for StaleIntentsObserved = -1, got true")
		}
	})

	t.Run("all-zero-valid", func(t *testing.T) {
		t.Parallel()
		p := base
		// All new fields default to 0 — Valid must accept that.
		if !p.Valid() {
			t.Errorf("Valid: expected true for all-zero payload with non-empty SweptAt, got false")
		}
	})
}

// ──────────────────────────────────────────────────────────────────────────────
// RunOrphanSweep integration
// ──────────────────────────────────────────────────────────────────────────────

// TestPL006_RunOrphanSweep_EmptyProjectDir verifies that RunOrphanSweep
// succeeds with an empty project directory (no orphan resources to sweep).
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — full sweep orchestration.
func TestPL006_RunOrphanSweep_EmptyProjectDir(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hash := lifecycle.ComputeProjectHash(projectDir)
	daemonStart := time.Now()

	cfg := OrphanSweepConfig{
		TmuxLister:    &daemonOrphanSweepFakeTmuxLister{},
		TmuxKiller:    &daemonOrphanSweepFakeTmuxKiller{},
		HandlerLister: &daemonOrphanSweepFakeHandlerLister{},
		BrLister:      &daemonOrphanSweepFakeBrLister{},
	}

	result, err := RunOrphanSweep(t.Context(), projectDir, hash, daemonStart, cfg)
	if err != nil {
		// git worktree prune may fail on a non-git dir; that is acceptable in the
		// fixture context (the project dir is a temp dir, not a git repo).
		t.Logf("PL-006 run empty: RunOrphanSweep returned error (may be worktree prune): %v", err)
	}

	if result.TmuxSessionsKilled != 0 {
		t.Errorf("PL-006 run empty: TmuxSessionsKilled = %d, want 0", result.TmuxSessionsKilled)
	}
	if result.SubprocessesKilled != 0 {
		t.Errorf("PL-006 run empty: SubprocessesKilled = %d, want 0", result.SubprocessesKilled)
	}
	if result.StaleIntentsObserved != 0 {
		t.Errorf("PL-006 run empty: StaleIntentsObserved = %d, want 0", result.StaleIntentsObserved)
	}
	if result.ReconciliationLocksRemoved != 0 {
		t.Errorf("PL-006 run empty: ReconciliationLocksRemoved = %d, want 0", result.ReconciliationLocksRemoved)
	}
	if result.SweptAt.IsZero() {
		t.Error("PL-006 run empty: SweptAt is zero; want non-zero")
	}
}

// TestPL006_RunOrphanSweep_AllCounters verifies that RunOrphanSweep aggregates
// counts from all sweep phases into the OrphanSweepResult.
//
// Spec ref: process-lifecycle.md §4.2 PL-006 — event payload must carry all
// six counters.
func TestPL006_RunOrphanSweep_AllCounters(t *testing.T) {
	t.Parallel()

	projectDir := daemonOrphanSweepTempProjectDir(t)
	hash := lifecycle.ComputeProjectHash(projectDir)
	prefix := lifecycle.TmuxSessionPrefix(hash)

	// Seed:
	//   - 1 tmux session matching the prefix
	//   - 3 stale intents
	//   - 1 stale reconciliation lock
	tmuxSession := prefix + "run-counter"
	// Use a smart lister: returns the session on first call, empty on subsequent
	// calls — this simulates session exit after kill, avoiding a 2-second poll wait.
	lister := &daemonOrphanSweepFakeTmuxLister{sessions: []string{tmuxSession}}
	killer := &daemonOrphanSweepFakeTmuxKiller{}

	for _, id := range []string{"ci-a", "ci-b", "ci-c"} {
		daemonOrphanSweepSeedStaleIntent(t, projectDir, id)
	}
	daemonStart := time.Now()

	const deadPID = 99990
	if daemonOrphanSweepIsPidLive(deadPID) {
		t.Skipf("PL-006 run all-counters: PID %d is live; skipping", deadPID)
	}
	daemonOrphanSweepSeedReconciliationLock(t, projectDir, "run-all-counters", deadPID, false)

	cfg := OrphanSweepConfig{
		TmuxLister:    lister,
		TmuxKiller:    killer,
		HandlerLister: &daemonOrphanSweepFakeHandlerLister{},
		BrLister:      &daemonOrphanSweepFakeBrLister{},
	}

	result, err := RunOrphanSweep(t.Context(), projectDir, hash, daemonStart, cfg)
	if err != nil {
		// git worktree prune failure from non-git dir is expected.
		t.Logf("PL-006 run all-counters: RunOrphanSweep error (possibly worktree prune): %v", err)
	}

	if result.TmuxSessionsKilled != 1 {
		t.Errorf("PL-006 run all-counters: TmuxSessionsKilled = %d, want 1", result.TmuxSessionsKilled)
	}
	if result.StaleIntentsObserved != 3 {
		t.Errorf("PL-006 run all-counters: StaleIntentsObserved = %d, want 3", result.StaleIntentsObserved)
	}
	if result.ReconciliationLocksRemoved != 1 {
		t.Errorf("PL-006 run all-counters: ReconciliationLocksRemoved = %d, want 1", result.ReconciliationLocksRemoved)
	}
}
