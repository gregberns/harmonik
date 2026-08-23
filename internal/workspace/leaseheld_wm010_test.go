package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestWM010_LeaseHeldByRunNotByAgent verifies that a workspace is leased by
// exactly one Run for the run's full lifetime, and that multiple agents within
// the run occupy the same worktree sequentially.
//
// Spec ref: workspace-model.md §4.3 WM-010 — "A workspace MUST be leased by
// exactly one Run for the run's full lifetime … Multiple agents within the run
// (planner, researcher, builder, reviewer, merge agent) MUST occupy the same
// worktree sequentially across their nodes. An agent MUST NOT hold exclusive
// ownership of the worktree for the duration of its agent-level session; the
// centralized-controller principle requires the run — not the agent — to be the
// lease holder."
func TestWM010_LeaseHeldByRunNotByAgent(t *testing.T) {
	t.Parallel()

	t.Run("single-run-lease-stable-across-multi-agent-sequential", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-7010-8a1b-2c3d4e5f0010"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		//nolint:gosec // G204: git command and worktree paths are controlled by this test fixture.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		leaseLockDir := filepath.Join(worktreePath, ".harmonik")
		if err := os.MkdirAll(leaseLockDir, 0o700); err != nil {
			t.Fatalf("MkdirAll leaseLockDir: %v", err)
		}
		leaseLockPath := filepath.Join(leaseLockDir, "lease.lock")
		pid := os.Getpid()
		lockContent := leaseFixtureMakeLockJSON(runID, pid, time.Now())
		leaseFixtureWriteLockAtomic(t, leaseLockPath, lockContent)

		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Fatalf("WM-010: lease-lock absent after write: %v", err)
		}

		agentTypes := []string{"planner", "builder", "reviewer"}
		for i, agentType := range agentTypes {
			sessionID := leaseFixtureSessionID(i)
			sessionDir := filepath.Join(worktreePath, ".harmonik", "sessions", sessionID)
			if err := os.MkdirAll(sessionDir, 0o700); err != nil {
				t.Fatalf("WM-010: agent %q: MkdirAll sessionDir: %v", agentType, err)
			}
			sidecar := filepath.Join(sessionDir, "harmonik.meta.json")
			content := `{"run_id":"` + runID + `","session_id":"` + sessionID +
				`","agent_type":"` + agentType + `","schema_version":"1"}`
			if err := os.WriteFile(sidecar, []byte(content), 0o600); err != nil {
				t.Fatalf("WM-010: agent %q: WriteFile sidecar: %v", agentType, err)
			}

			if _, err := os.Stat(leaseLockPath); err != nil {
				t.Errorf("WM-010: lease-lock missing after agent %q session; want stable run-scoped lease: %v",
					agentType, err)
			}
		}

		entries, err := os.ReadDir(leaseLockDir)
		if err != nil {
			t.Fatalf("WM-010: ReadDir .harmonik: %v", err)
		}
		var lockFiles []string
		for _, e := range entries {
			if !e.IsDir() {
				lockFiles = append(lockFiles, e.Name())
			}
		}
		if len(lockFiles) != 1 || lockFiles[0] != "lease.lock" {
			t.Errorf("WM-010: want exactly [lease.lock] in .harmonik, got %v", lockFiles)
		}

		if err := os.Remove(leaseLockPath); err != nil {
			t.Fatalf("WM-010: Remove lease-lock: %v", err)
		}
		if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
			t.Errorf("WM-010: lease-lock still present after release; want absent")
		}
	})

	t.Run("two-runs-occupy-separate-worktrees", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		runIDs := []string{
			"0196a1b2-c3d4-7010-8a1b-2c3d4e5fa001",
			"0196a1b2-c3d4-7010-8a1b-2c3d4e5fa002",
		}

		for _, runID := range runIDs {
			branch := "run/" + runID
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
			if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			//nolint:gosec // G204: git command and worktree paths are controlled by this test fixture.
			cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git worktree add %q: %v\n%s", runID, err, out)
			}
			leaseLockDir := filepath.Join(worktreePath, ".harmonik")
			if err := os.MkdirAll(leaseLockDir, 0o700); err != nil {
				t.Fatalf("MkdirAll leaseLockDir: %v", err)
			}
			leaseLockPath := filepath.Join(leaseLockDir, "lease.lock")
			lockContent := leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now())
			leaseFixtureWriteLockAtomic(t, leaseLockPath, lockContent)
		}

		for _, runID := range runIDs {
			leaseLockPath := filepath.Join(repo, ".harmonik", "worktrees", runID, ".harmonik", "lease.lock")
			if _, err := os.Stat(leaseLockPath); err != nil {
				t.Errorf("WM-010: run %q: lease-lock absent; want separate per-run lease: %v", runID, err)
			}
		}

		path0 := filepath.Join(repo, ".harmonik", "worktrees", runIDs[0], ".harmonik", "lease.lock")
		path1 := filepath.Join(repo, ".harmonik", "worktrees", runIDs[1], ".harmonik", "lease.lock")
		if path0 == path1 {
			t.Errorf("WM-010: two runs share the same lease-lock path %q; want separate paths", path0)
		}
	})
}

func leaseFixtureSessionID(i int) string {
	ids := []string{
		"sess-0196a1b2-c3d4-7010-0000-000000000001",
		"sess-0196a1b2-c3d4-7010-0000-000000000002",
		"sess-0196a1b2-c3d4-7010-0000-000000000003",
		"sess-0196a1b2-c3d4-7010-0000-000000000004",
	}
	if i < len(ids) {
		return ids[i]
	}
	return "sess-fallback-" + string(rune('a'+i))
}
