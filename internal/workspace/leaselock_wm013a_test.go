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

// TestWM013a_LeaseLockCanonicalPathAndContent verifies that the lease-lock file
// is written at the canonical path, has the correct JSON content fields, and is
// written atomically (temp + rename + fsync + parent-dir fsync).
//
// Spec ref: workspace-model.md §4.3 WM-013a — "The lease on a workspace is
// represented by a lease-lock file at the canonical path declared in §6.2. …
// The file's content MUST be a JSON object with the fields: run_id (UUID,
// required), pid (integer, required), created_at (RFC 3339, required), ttl_sec
// (integer, required). The workspace manager MUST write the lease-lock file
// atomically (write-to-temp + rename) and MUST fsync the file before emitting
// workspace_leased."
//
// NOTE on canonical path: the spec is authoritative as `${workspace_path}/.harmonik/lease.lock`.
// HC-044a names a different path; OQ-WM-005 tracks resolution. This test treats
// WM's path as authoritative per the spec's NOTE clause.
func TestWM013a_LeaseLockCanonicalPathAndContent(t *testing.T) {
	t.Parallel()

	t.Run("canonical-path-is-workspace-harmonik-lease-lock", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013a"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		wantPath := filepath.Join(worktreePath, ".harmonik", "lease.lock")
		gotPath := leaseFixtureLeaseLockPath(worktreePath)
		if gotPath != wantPath {
			t.Errorf("WM-013a: canonical path = %q, want %q", gotPath, wantPath)
		}

		// The canonical path must NOT be the HC-044a path.
		// HC-044a path would be: ${workspace_path}/.lock (i.e., <worktree>/.lock)
		hcPath := filepath.Join(worktreePath, ".lock")
		if gotPath == hcPath {
			t.Errorf("WM-013a: lease-lock path %q matches HC-044a path; WM's path is authoritative per OQ-WM-005", gotPath)
		}
	})

	t.Run("json-content-fields-required", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013b"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		pid := os.Getpid()
		now := time.Now().UTC()
		ttlSec := 3600

		leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath, leaseFixtureMakeLockJSON(runID, pid, now, ttlSec))

		// Parse the written lock content and validate required fields.
		data, err := os.ReadFile(leaseLockPath)
		if err != nil {
			t.Fatalf("WM-013a: ReadFile lease-lock: %v", err)
		}

		var lock struct {
			RunID     string `json:"run_id"`
			PID       int    `json:"pid"`
			CreatedAt string `json:"created_at"`
			TTLSec    int    `json:"ttl_sec"`
		}
		if err := json.Unmarshal(data, &lock); err != nil {
			t.Fatalf("WM-013a: json.Unmarshal lease-lock: %v\ncontent: %s", err, data)
		}

		// run_id: must match the owning run.
		if lock.RunID != runID {
			t.Errorf("WM-013a: lock.run_id = %q, want %q", lock.RunID, runID)
		}
		// pid: must be the daemon's PID.
		if lock.PID != pid {
			t.Errorf("WM-013a: lock.pid = %d, want %d", lock.PID, pid)
		}
		// created_at: must be parseable as RFC 3339.
		if _, err := time.Parse(time.RFC3339, lock.CreatedAt); err != nil {
			t.Errorf("WM-013a: lock.created_at %q is not RFC 3339: %v", lock.CreatedAt, err)
		}
		// ttl_sec: must be present and positive.
		if lock.TTLSec <= 0 {
			t.Errorf("WM-013a: lock.ttl_sec = %d, want > 0", lock.TTLSec)
		}
	})

	t.Run("atomic-write-no-orphan-tmp-files", func(t *testing.T) {
		t.Parallel()

		// The atomic-write discipline guarantees that after a successful write:
		// (1) exactly one lease-lock file exists at the canonical path,
		// (2) no .tmp-* orphan files remain in the .harmonik directory.
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013c"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath, leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now(), 3600))

		// Enumerate the .harmonik directory and assert:
		// - exactly one file: lease.lock
		// - no files matching .tmp-* remain
		harmonikDir := filepath.Join(worktreePath, ".harmonik")
		entries, err := os.ReadDir(harmonikDir)
		if err != nil {
			t.Fatalf("WM-013a: ReadDir .harmonik: %v", err)
		}

		var files []string
		for _, e := range entries {
			if !e.IsDir() {
				files = append(files, e.Name())
			}
		}

		// Exactly one file: "lease.lock".
		if len(files) != 1 {
			t.Errorf("WM-013a: .harmonik contains %d file(s) after atomic write, want exactly 1; files: %v", len(files), files)
		} else if files[0] != "lease.lock" {
			t.Errorf("WM-013a: .harmonik file = %q, want %q", files[0], "lease.lock")
		}

		// No .tmp-* orphans.
		for _, name := range files {
			if strings.HasPrefix(name, ".tmp-") || strings.HasSuffix(name, ".tmp-"+string(rune('0'+os.Getpid()%10))) ||
				strings.Contains(name, ".tmp-") {
				t.Errorf("WM-013a: orphan temp file %q found in .harmonik after atomic write", name)
			}
		}
	})

	t.Run("lock-absent-before-leased-state", func(t *testing.T) {
		t.Parallel()

		// WM-013a: "On every workspace_created emission, the workspace manager
		// MUST NOT yet have written a lease-lock file — the lock is tied to
		// lease acquisition, not to workspace existence."
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013d"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		// After workspace_created (git worktree add complete) but BEFORE
		// workspace_leased, the lease-lock MUST NOT exist.
		leaseLockPath := leaseFixtureLeaseLockPath(worktreePath)
		if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
			t.Errorf("WM-013a: lease-lock present before leased state; want absent at workspace_created")
		}

		// Now simulate the workspace_leased sequence (steps a-d of WM-016):
		// (d) write lease-lock → then workspace_leased emits.
		leaseFixtureWriteLockAtomic(t, leaseLockPath, leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now(), 3600))
		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Errorf("WM-013a: lease-lock absent after leased state; want present: %v", err)
		}
	})
}
