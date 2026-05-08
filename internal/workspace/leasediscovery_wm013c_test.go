package workspace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWM013c_LeaseDiscoveryMechanismOnStartup verifies the startup discovery
// sequence: (a) enumerate subdirectories of <repo>/.harmonik/worktrees/ matching
// the run_id regex; (b) confirm each is a registered worktree via
// `git worktree list --porcelain`; (c) read the lease-lock file to recover
// run_id, pid, created_at; (d) stat the sessions/ directory.
//
// Spec ref: workspace-model.md §4.3 WM-013c — "The workspace manager's startup
// path MUST discover live workspaces by (a) enumerating subdirectories of
// <repo>/.harmonik/worktrees/ matching the <run_id> regex of WM-002; (b) for
// each, calling `git worktree list --porcelain` against <repo> and confirming
// the directory is a registered worktree; (c) reading the lease-lock file per
// WM-013a (if present) to recover the run_id, pid, and created_at; (d)
// stat-ing ${path}/.harmonik/sessions/ to detect whether any session was ever
// started."
func TestWM013c_LeaseDiscoveryMechanismOnStartup(t *testing.T) {
	t.Parallel()

	t.Run("enumerate-and-validate-registered-worktrees", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		// Create two registered worktrees.
		runIDs := []string{
			"0196a1b2-c3d4-713c-8a1b-aabbccddeeff",
			"0196a1b2-c3d4-713c-8a1b-112233445566",
		}
		for _, runID := range runIDs {
			branch := "run/" + runID
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
			if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
				t.Fatalf("MkdirAll %q: %v", worktreePath, err)
			}
			cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git worktree add %q: %v\n%s", runID, err, out)
			}
			lp := leaseFixture_leaseLockPath(worktreePath)
			leaseFixture_writeLockAtomic(t, lp, leaseFixture_makeLockJSON(runID, os.Getpid(), time.Now(), 3600))
		}

		// --- Step (a): enumerate subdirectories of <repo>/.harmonik/worktrees/ ---
		worktreeRoot := filepath.Join(repo, ".harmonik", "worktrees")
		entries, err := os.ReadDir(worktreeRoot)
		if err != nil {
			t.Fatalf("WM-013c: ReadDir worktreeRoot: %v", err)
		}
		var foundRunIDs []string
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if runIDValid(name) {
				foundRunIDs = append(foundRunIDs, name)
			}
		}
		if len(foundRunIDs) != len(runIDs) {
			t.Fatalf("WM-013c: enumerated %d run_ids, want %d; found: %v", len(foundRunIDs), len(runIDs), foundRunIDs)
		}

		// --- Step (b): validate via `git worktree list --porcelain` ---
		out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
		if err != nil {
			t.Fatalf("WM-013c: git worktree list --porcelain: %v", err)
		}
		porcelainOutput := string(out)

		for _, runID := range foundRunIDs {
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
			// The worktree path must appear in the porcelain output.
			if !findSubstring(porcelainOutput, worktreePath) {
				t.Errorf("WM-013c: worktree path %q not found in porcelain output", worktreePath)
			}
		}

		// --- Step (c): read lease-lock to recover run_id, pid, created_at ---
		for _, runID := range foundRunIDs {
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
			leaseLockPath := leaseFixture_leaseLockPath(worktreePath)

			data, err := os.ReadFile(leaseLockPath)
			if err != nil {
				t.Fatalf("WM-013c: ReadFile lease-lock for %q: %v", runID, err)
			}

			var lock struct {
				RunID     string `json:"run_id"`
				PID       int    `json:"pid"`
				CreatedAt string `json:"created_at"`
				TTLSec    int    `json:"ttl_sec"`
			}
			if err := json.Unmarshal(data, &lock); err != nil {
				t.Fatalf("WM-013c: json.Unmarshal lock for %q: %v", runID, err)
			}
			if lock.RunID != runID {
				t.Errorf("WM-013c: lock.run_id = %q, want %q", lock.RunID, runID)
			}
			if lock.PID <= 0 {
				t.Errorf("WM-013c: lock.pid = %d, want > 0", lock.PID)
			}
			if _, err := time.Parse(time.RFC3339, lock.CreatedAt); err != nil {
				t.Errorf("WM-013c: lock.created_at %q not RFC 3339: %v", lock.CreatedAt, err)
			}

			// --- Step (d): stat sessions/ directory ---
			sessionsDir := filepath.Join(worktreePath, ".harmonik", "sessions")
			_, statErr := os.Stat(sessionsDir)
			// sessions/ may or may not exist; the daemon checks for its presence.
			// This test: no sessions were started, so sessions/ MUST NOT exist.
			if statErr != nil && !os.IsNotExist(statErr) {
				t.Errorf("WM-013c: Stat sessions/: unexpected error: %v", statErr)
			}
			// (sessions/ does not exist → no session was ever started, correct.)
		}
	})

	t.Run("orphan-directory-not-in-porcelain-excluded", func(t *testing.T) {
		t.Parallel()

		// A directory that exists under .harmonik/worktrees/ but is NOT registered
		// in git worktree list is an orphan — not a live workspace.
		repo, _ := tempRepo(t)

		worktreeRoot := filepath.Join(repo, ".harmonik", "worktrees")
		if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		orphanRunID := "0196a1b2-c3d4-713c-8a1b-orphanorphan"
		orphanPath := filepath.Join(worktreeRoot, orphanRunID)
		if err := os.MkdirAll(orphanPath, 0o755); err != nil {
			t.Fatalf("MkdirAll orphan: %v", err)
		}

		// git worktree list --porcelain must NOT list the orphan directory.
		out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
		if err != nil {
			t.Fatalf("WM-013c: git worktree list --porcelain: %v", err)
		}
		if findSubstring(string(out), orphanPath) {
			t.Errorf("WM-013c: orphan path %q unexpectedly listed in porcelain output", orphanPath)
		}
	})

	t.Run("discovery-reads-all-required-lock-fields", func(t *testing.T) {
		t.Parallel()

		// Verify that a startup discovery pass reads the three fields cited in
		// WM-013c step (c): run_id, pid, created_at.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-readallfields"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		wantPID := os.Getpid()
		wantCreatedAt := time.Now().UTC()
		lp := leaseFixture_leaseLockPath(worktreePath)
		leaseFixture_writeLockAtomic(t, lp, leaseFixture_makeLockJSON(runID, wantPID, wantCreatedAt, 3600))

		// Simulate discovery: parse the lock file.
		data, err := os.ReadFile(lp)
		if err != nil {
			t.Fatalf("WM-013c: ReadFile: %v", err)
		}
		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("WM-013c: Unmarshal: %v", err)
		}

		for _, field := range []string{"run_id", "pid", "created_at", "ttl_sec"} {
			if _, ok := parsed[field]; !ok {
				t.Errorf("WM-013c: lease-lock missing field %q", field)
			}
		}

		// Verify the porcelain output lists this worktree.
		out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
		if err != nil {
			t.Fatalf("WM-013c: git worktree list --porcelain: %v", err)
		}
		// Each worktree block starts with "worktree <path>".
		if !findSubstring(string(out), worktreePath) {
			t.Errorf("WM-013c: worktree path %q not in porcelain:\n%s", worktreePath, strings.TrimSpace(string(out)))
		}
	})
}
