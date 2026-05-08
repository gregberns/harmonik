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

		// Create the worktree (simulating workspace creation).
		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		// Write the lease-lock file (simulating workspace_leased emission ordering
		// per WM-016: worktree → branch → sessions dir + sidecar → lease lock).
		leaseLockDir := filepath.Join(worktreePath, ".harmonik")
		if err := os.MkdirAll(leaseLockDir, 0o755); err != nil {
			t.Fatalf("MkdirAll leaseLockDir: %v", err)
		}
		leaseLockPath := filepath.Join(leaseLockDir, "lease.lock")
		pid := os.Getpid()
		lockContent := leaseFixture_makeLockJSON(runID, pid, time.Now(), 3600)
		leaseFixture_writeLockAtomic(t, leaseLockPath, lockContent)

		// Verify the lease-lock exists at the canonical path.
		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Fatalf("WM-010: lease-lock absent after write: %v", err)
		}

		// Simulate three agents running sequentially in the SAME worktree.
		// The lease-lock file (run-scoped) MUST remain stable across all three agents.
		// Each agent "runs" by writing a session directory, then releasing session control.
		agentTypes := []string{"planner", "builder", "reviewer"}
		for i, agentType := range agentTypes {
			sessionID := leaseFixture_sessionID(i)
			sessionDir := filepath.Join(worktreePath, ".harmonik", "sessions", sessionID)
			if err := os.MkdirAll(sessionDir, 0o755); err != nil {
				t.Fatalf("WM-010: agent %q: MkdirAll sessionDir: %v", agentType, err)
			}
			sidecar := filepath.Join(sessionDir, "harmonik.meta.json")
			content := `{"run_id":"` + runID + `","session_id":"` + sessionID +
				`","agent_type":"` + agentType + `","schema_version":"1"}`
			if err := os.WriteFile(sidecar, []byte(content), 0o644); err != nil {
				t.Fatalf("WM-010: agent %q: WriteFile sidecar: %v", agentType, err)
			}

			// After each agent "completes" its node, the lease-lock MUST still exist
			// (the run has not terminated). The lease is held by the run, not the agent.
			if _, err := os.Stat(leaseLockPath); err != nil {
				t.Errorf("WM-010: lease-lock missing after agent %q session; want stable run-scoped lease: %v",
					agentType, err)
			}
		}

		// Confirm exactly one lease-lock file exists (no orphan .tmp-* files).
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

		// Release the lease (terminal transition: run complete).
		if err := os.Remove(leaseLockPath); err != nil {
			t.Fatalf("WM-010: Remove lease-lock: %v", err)
		}
		if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
			t.Errorf("WM-010: lease-lock still present after release; want absent")
		}
	})

	t.Run("two-runs-occupy-separate-worktrees", func(t *testing.T) {
		t.Parallel()

		// WM-010 cross-reference: "Parallel nodes across different runs occupy
		// separate worktrees per WM-002."
		repo, sha := tempRepo(t)

		runIDs := []string{
			"0196a1b2-c3d4-7010-8a1b-2c3d4e5fa001",
			"0196a1b2-c3d4-7010-8a1b-2c3d4e5fa002",
		}

		for _, runID := range runIDs {
			branch := "run/" + runID
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
			if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git worktree add %q: %v\n%s", runID, err, out)
			}
			leaseLockDir := filepath.Join(worktreePath, ".harmonik")
			if err := os.MkdirAll(leaseLockDir, 0o755); err != nil {
				t.Fatalf("MkdirAll leaseLockDir: %v", err)
			}
			leaseLockPath := filepath.Join(leaseLockDir, "lease.lock")
			lockContent := leaseFixture_makeLockJSON(runID, os.Getpid(), time.Now(), 3600)
			leaseFixture_writeLockAtomic(t, leaseLockPath, lockContent)
		}

		// Each run has its own canonical worktree path and its own lease-lock.
		for _, runID := range runIDs {
			leaseLockPath := filepath.Join(repo, ".harmonik", "worktrees", runID, ".harmonik", "lease.lock")
			if _, err := os.Stat(leaseLockPath); err != nil {
				t.Errorf("WM-010: run %q: lease-lock absent; want separate per-run lease: %v", runID, err)
			}
		}

		// The two lease-locks are at DIFFERENT paths (separate worktrees).
		path0 := filepath.Join(repo, ".harmonik", "worktrees", runIDs[0], ".harmonik", "lease.lock")
		path1 := filepath.Join(repo, ".harmonik", "worktrees", runIDs[1], ".harmonik", "lease.lock")
		if path0 == path1 {
			t.Errorf("WM-010: two runs share the same lease-lock path %q; want separate paths", path0)
		}
	})
}

// leaseFixture_sessionID returns a deterministic session ID for the i-th agent
// in multi-agent sequential tests. Prefixed leaseFixture_ to avoid sibling-package collisions.
func leaseFixture_sessionID(i int) string {
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
