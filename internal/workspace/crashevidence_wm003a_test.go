package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWM003a_ClassifyCrashEvidence verifies that ClassifyCrashEvidence correctly
// identifies the two orphan evidence types defined by workspace-model.md §4.1 WM-003a:
//
//   - EvidenceBareWorktreeNoLease: registered worktree, no lease-lock, no sessions dir.
//   - EvidenceSidecarWithoutLease: registered worktree, sidecar present, no lease-lock.
//
// Both states arise from SIGKILL / power loss between `git worktree add` and
// the lease-lock fsync gate of WM-016.
func TestWM003a_ClassifyCrashEvidence(t *testing.T) {
	t.Parallel()

	t.Run("bare-worktree-no-lease", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0020"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-003a: CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		evidenceType, err := ClassifyCrashEvidence(worktreePath)
		if !errors.Is(err, ErrBareWorktreeNoLease) {
			t.Fatalf("WM-003a: bare-worktree-no-lease: expected ErrBareWorktreeNoLease, got: %v", err)
		}
		if evidenceType != EvidenceBareWorktreeNoLease {
			t.Errorf("WM-003a: bare-worktree-no-lease: evidenceType = %q, want %q",
				evidenceType, EvidenceBareWorktreeNoLease)
		}
	})

	t.Run("sidecar-without-lease", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0021"
		sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000000021"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-003a: CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		sidecarDir := filepath.Join(SessionLogRootPath(worktreePath), sessionID)
		if err := os.MkdirAll(sidecarDir, 0o700); err != nil {
			t.Fatalf("WM-003a: MkdirAll sidecarDir: %v", err)
		}
		sidecarPath := filepath.Join(sidecarDir, "harmonik.meta.json")
		sidecarContent := `{"run_id":"` + runID + `","session_id":"` + sessionID + `","schema_version":1}`
		if err := os.WriteFile(sidecarPath, []byte(sidecarContent), 0o600); err != nil {
			t.Fatalf("WM-003a: WriteFile sidecar: %v", err)
		}

		evidenceType, err := ClassifyCrashEvidence(worktreePath)
		if !errors.Is(err, ErrSidecarWithoutLease) {
			t.Fatalf("WM-003a: sidecar-without-lease: expected ErrSidecarWithoutLease, got: %v", err)
		}
		if evidenceType != EvidenceSidecarWithoutLease {
			t.Errorf("WM-003a: sidecar-without-lease: evidenceType = %q, want %q",
				evidenceType, EvidenceSidecarWithoutLease)
		}
	})

	t.Run("lease-lock-present-returns-error", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0022"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-003a: CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
		leaseLockPath := LeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

		_, err := ClassifyCrashEvidence(worktreePath)
		if err == nil {
			t.Error("WM-003a: expected error when lease-lock is present, got nil")
		}
	})

	t.Run("evidence-type-string-values-are-spec-canonical", func(t *testing.T) {
		t.Parallel()

		if EvidenceBareWorktreeNoLease != "bare-worktree-no-lease" {
			t.Errorf("WM-003a: EvidenceBareWorktreeNoLease = %q, want %q",
				EvidenceBareWorktreeNoLease, "bare-worktree-no-lease")
		}
		if EvidenceSidecarWithoutLease != "sidecar-without-lease" {
			t.Errorf("WM-003a: EvidenceSidecarWithoutLease = %q, want %q",
				EvidenceSidecarWithoutLease, "sidecar-without-lease")
		}
	})

	t.Run("sessions-dir-present-but-no-sidecar-is-bare", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0023"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("WM-003a: CreateWorktree: %v", err)
		}
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		sessionsRoot := SessionLogRootPath(worktreePath)
		if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
			t.Fatalf("WM-003a: MkdirAll sessionsRoot: %v", err)
		}

		evidenceType, err := ClassifyCrashEvidence(worktreePath)
		if !errors.Is(err, ErrBareWorktreeNoLease) {
			t.Fatalf("WM-003a: sessions-no-sidecar: expected ErrBareWorktreeNoLease, got: %v", err)
		}
		if evidenceType != EvidenceBareWorktreeNoLease {
			t.Errorf("WM-003a: sessions-no-sidecar: evidenceType = %q, want %q",
				evidenceType, EvidenceBareWorktreeNoLease)
		}
	})
}
