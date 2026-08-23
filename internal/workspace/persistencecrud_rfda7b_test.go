package workspace

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

func rfda7bFixtureMakeLock(t *testing.T, runID string) *core.LeaseLockFile {
	t.Helper()
	u := uuid.MustParse(runID)
	return &core.LeaseLockFile{
		RunID:     core.RunID(u),
		PID:       os.Getpid(),
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		TTLSec:    3600,
	}
}

// TestRFDA7b_PersistenceCRUD_FullLeaseLockLifecycle exercises the complete
// Create → Read → Release integration path for workspace persistence:
//
//   - Create: git worktree add + WriteLeaseLockAtomic
//   - Read:   ReadLeaseLock round-trip field verification
//   - Delete: WriteLeaseReleasedMarker (before unlink) + ReleaseLeaseLock
//
// Spec refs:
//   - workspace-model.md §4.3 WM-013a — lease-lock write and read
//   - workspace-model.md §4.3 WM-013b — marker-before-unlink + idempotent release
func TestRFDA7b_PersistenceCRUD_FullLeaseLockLifecycle(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196fb00-0000-7b59-8000-000000000001"
	workspaceID := WorkspaceIDFromRunID(runID)
	worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("Create: CreateWorktree: %v", err)
	}
	lock := rfda7bFixtureMakeLock(t, runID)
	leaseLockPath := LeaseLockPath(worktreePath)
	if err := WriteLeaseLockAtomic(leaseLockPath, lock); err != nil {
		t.Fatalf("Create: WriteLeaseLockAtomic: %v", err)
	}

	read, err := ReadLeaseLock(leaseLockPath)
	if err != nil {
		t.Fatalf("Read: ReadLeaseLock: %v", err)
	}
	if read == nil {
		t.Fatal("Read: ReadLeaseLock returned nil for existing lock")
	}
	if read.RunID.String() != runID {
		t.Errorf("Read: run_id = %q, want %q", read.RunID, runID)
	}
	if read.PID != lock.PID {
		t.Errorf("Read: pid = %d, want %d", read.PID, lock.PID)
	}
	if read.TTLSec != lock.TTLSec {
		t.Errorf("Read: ttl_sec = %d, want %d", read.TTLSec, lock.TTLSec)
	}
	if !read.CreatedAt.Equal(lock.CreatedAt) {
		t.Errorf("Read: created_at = %v, want %v", read.CreatedAt, lock.CreatedAt)
	}

	ref, err := LookupWorkspace(repo, runID, NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("Read: LookupWorkspace: %v", err)
	}
	if !ref.ExistsOnDisk {
		t.Error("Read: LookupWorkspace ExistsOnDisk = false after create; want true")
	}

	if err := WriteLeaseReleasedMarker(worktreePath, runID, workspaceID, "merged"); err != nil {
		t.Fatalf("Delete: WriteLeaseReleasedMarker: %v", err)
	}
	if err := ReleaseLeaseLock(leaseLockPath); err != nil {
		t.Fatalf("Delete: ReleaseLeaseLock: %v", err)
	}

	if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
		t.Error("Delete: lease-lock still present after release; want absent")
	}
	eventsFile := WorkspaceLocalEventsPath(worktreePath, workspaceID)
	if _, err := os.Stat(eventsFile); err != nil {
		t.Errorf("Delete: events JSONL absent after release; want present: %v", err)
	}
	if err := ReleaseLeaseLock(leaseLockPath); err != nil {
		t.Errorf("Delete: idempotent second release: %v", err)
	}

	read2, err := ReadLeaseLock(leaseLockPath)
	if err != nil {
		t.Errorf("Delete: ReadLeaseLock after release error = %v; want nil", err)
	}
	if read2 != nil {
		t.Errorf("Delete: ReadLeaseLock after release returned non-nil; want nil")
	}
}

// TestRFDA7b_PersistenceCRUD_LookupExistsOnDiskBeforeAndAfterCreate verifies
// the ExistsOnDisk field of WorkspaceRef transitions correctly around
// CreateWorktree per WM-013c (filesystem existence check).
func TestRFDA7b_PersistenceCRUD_LookupExistsOnDiskBeforeAndAfterCreate(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196fb00-0000-7b59-8000-000000000002"

	ref, err := LookupWorkspace(repo, runID, NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("LookupWorkspace before create: %v", err)
	}
	if ref.ExistsOnDisk {
		t.Error("ExistsOnDisk = true before CreateWorktree; want false")
	}

	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	ref2, err := LookupWorkspace(repo, runID, NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("LookupWorkspace after create: %v", err)
	}
	if !ref2.ExistsOnDisk {
		t.Error("ExistsOnDisk = false after CreateWorktree; want true")
	}

	if ref.WorkspaceID != ref2.WorkspaceID {
		t.Errorf("WorkspaceID unstable: %q (before) vs %q (after)", ref.WorkspaceID, ref2.WorkspaceID)
	}
	if ref.Branch != ref2.Branch {
		t.Errorf("Branch unstable: %q (before) vs %q (after)", ref.Branch, ref2.Branch)
	}
	if ref.Path != ref2.Path {
		t.Errorf("Path unstable: %q (before) vs %q (after)", ref.Path, ref2.Path)
	}
}

// TestRFDA7b_PersistenceCRUD_MultipleTerminalPaths verifies that the
// marker-before-unlink pattern (WM-013b) works correctly for all four
// terminal-path reason values.
func TestRFDA7b_PersistenceCRUD_MultipleTerminalPaths(t *testing.T) {
	t.Parallel()

	terminalReasons := []string{
		"merged",
		"run_failed",
		"post_escalation",
		"verdict_driven",
	}

	for i, reason := range terminalReasons {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			harmonikDir := filepath.Join(dir, ".harmonik")
			if err := os.MkdirAll(harmonikDir, 0o700); err != nil {
				t.Fatalf("MkdirAll harmonikDir: %v", err)
			}

			workspaceID := "ws-0196fb00-0000-7b59-8000-00000000010" + string(rune('0'+i))
			runID := "0196fb00-0000-7b59-8000-00000000010" + string(rune('0'+i))

			leaseLockPath := LeaseLockPath(dir)
			leaseFixtureWriteLockAtomic(t, leaseLockPath,
				leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

			if err := WriteLeaseReleasedMarker(dir, runID, workspaceID, reason); err != nil {
				t.Fatalf("WriteLeaseReleasedMarker(%s): %v", reason, err)
			}
			if err := ReleaseLeaseLock(leaseLockPath); err != nil {
				t.Fatalf("ReleaseLeaseLock(%s): %v", reason, err)
			}

			if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
				t.Errorf("[%s]: lease-lock present after release; want absent", reason)
			}
			eventsFile := WorkspaceLocalEventsPath(dir, workspaceID)
			if _, err := os.Stat(eventsFile); err != nil {
				t.Errorf("[%s]: events JSONL absent; want present: %v", reason, err)
			}
		})
	}
}

// TestRFDA7b_PersistenceCRUD_InterruptStateIntegration verifies the
// interrupt_state_changed marker integration: SetInterruptStateToNone writes
// a durable marker and clears the workspace field atomically.
//
// Spec refs:
//   - workspace-model.md §4.10 WM-038a — marker write before field mutation
//   - workspace-model.md §4.10 WM-040  — cause-required clears
func TestRFDA7b_PersistenceCRUD_InterruptStateIntegration(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		initial  core.InterruptState
		cause    InterruptStateClearCause
		runIDSfx string
	}{
		{
			"operator-paused-cleared-by-operator-resuming",
			core.InterruptStateOperatorPaused,
			InterruptStateClearCauseOperatorResuming,
			"000000000020",
		},
		{
			"operator-stopped-graceful-cleared-by-operator-resuming",
			core.InterruptStateOperatorStoppedGraceful,
			InterruptStateClearCauseOperatorResuming,
			"000000000021",
		},
		{
			"operator-stopped-immediate-cleared-by-reconciliation-verdict",
			core.InterruptStateOperatorStoppedImmediate,
			InterruptStateClearCauseReconciliationVerdict,
			"000000000022",
		},
		{
			"daemon-crash-suspected-cleared-by-reconciliation-verdict",
			core.InterruptStateDaemonCrashSuspected,
			InterruptStateClearCauseReconciliationVerdict,
			"000000000023",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			workspaceID := "ws-0196fb00-0000-7b59-8000-" + tc.runIDSfx
			runID := "0196fb00-0000-7b59-8000-" + tc.runIDSfx

			ws := &Workspace{
				WorkspaceID:    workspaceID,
				State:          core.WorkspaceStateLeased,
				InterruptState: tc.initial,
				Metadata: map[string]string{
					"created_at":           "2026-05-26T00:00:00Z",
					"operator_fingerprint": "test",
				},
				SchemaVersion: 1,
			}

			priorInterrupt := ws.InterruptState

			if err := SetInterruptStateToNone(ws, dir, runID, tc.cause); err != nil {
				t.Fatalf("SetInterruptStateToNone: %v", err)
			}

			if ws.InterruptState != core.InterruptStateNone {
				t.Errorf("interrupt_state = %q after clear; want none", ws.InterruptState)
			}

			eventsFile := WorkspaceLocalEventsPath(dir, workspaceID)
			data := mustReadFile(t, eventsFile)
			if len(data) == 0 {
				t.Fatal("events JSONL is empty; want interrupt_state_changed marker")
			}

			content := string(data)
			priorStr := string(priorInterrupt)
			if !containsSubstring(content, priorStr) {
				t.Errorf("marker does not contain prior interrupt_state %q; content: %s", priorStr, content)
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" || func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}())
}

// TestRFDA7b_PersistenceCRUD_WorkspaceIDFromRunIDStability verifies that
// WorkspaceIDFromRunID is deterministic across multiple calls (no store needed)
// per WM-004.
func TestRFDA7b_PersistenceCRUD_WorkspaceIDFromRunIDStability(t *testing.T) {
	t.Parallel()

	runIDs := []string{
		"0196fb00-0000-7b59-8000-000000000030",
		"0196fb00-0000-7b59-8000-000000000031",
		"0196fb00-0000-7b59-8000-000000000032",
	}

	for _, runID := range runIDs {
		t.Run(runID, func(t *testing.T) {
			t.Parallel()

			id1 := WorkspaceIDFromRunID(runID)
			id2 := WorkspaceIDFromRunID(runID)
			if id1 != id2 {
				t.Errorf("WM-004: WorkspaceIDFromRunID non-deterministic: %q vs %q", id1, id2)
			}
			want := "ws-" + runID
			if id1 != want {
				t.Errorf("WM-004: WorkspaceIDFromRunID(%q) = %q, want %q", runID, id1, want)
			}
		})
	}
}
