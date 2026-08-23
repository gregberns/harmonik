package workspace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
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

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed entirely by this test.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		wantPath := filepath.Join(worktreePath, ".harmonik", "lease.lock")

		gotPath := LeaseLockPath(worktreePath)
		if gotPath != wantPath {
			t.Errorf("WM-013a: LeaseLockPath = %q, want %q", gotPath, wantPath)
		}

		hcPath := filepath.Join(worktreePath, ".lock")
		if gotPath == hcPath {
			t.Errorf("WM-013a: LeaseLockPath %q matches HC-044a path; WM's path is authoritative per OQ-WM-005", gotPath)
		}
	})

	t.Run("json-content-fields-required", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013b"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed entirely by this test.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		pid := os.Getpid()
		now := time.Now().UTC()
		ttlSec := 3600

		u := uuid.MustParse(runID)
		lock := &core.LeaseLockFile{
			RunID:     core.RunID(u),
			PID:       pid,
			CreatedAt: now,
			TTLSec:    ttlSec,
		}

		leaseLockPath := LeaseLockPath(worktreePath)
		if err := WriteLeaseLockAtomic(leaseLockPath, lock); err != nil {
			t.Fatalf("WM-013a: WriteLeaseLockAtomic: %v", err)
		}

		data := mustReadFile(t, leaseLockPath)

		var parsed struct {
			RunID     string `json:"run_id"`
			PID       int    `json:"pid"`
			CreatedAt string `json:"created_at"`
			TTLSec    int    `json:"ttl_sec"`
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("WM-013a: json.Unmarshal lease-lock: %v\ncontent: %s", err, data)
		}

		if parsed.RunID != runID {
			t.Errorf("WM-013a: lock.run_id = %q, want %q", parsed.RunID, runID)
		}
		if parsed.PID != pid {
			t.Errorf("WM-013a: lock.pid = %d, want %d", parsed.PID, pid)
		}
		if _, err := time.Parse(time.RFC3339, parsed.CreatedAt); err != nil {
			t.Errorf("WM-013a: lock.created_at %q is not RFC 3339: %v", parsed.CreatedAt, err)
		}
		if parsed.TTLSec <= 0 {
			t.Errorf("WM-013a: lock.ttl_sec = %d, want > 0", parsed.TTLSec)
		}
	})

	t.Run("atomic-write-no-orphan-tmp-files", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013c"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed entirely by this test.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		u := uuid.MustParse(runID)
		lock := &core.LeaseLockFile{
			RunID:     core.RunID(u),
			PID:       os.Getpid(),
			CreatedAt: time.Now().UTC(),
			TTLSec:    3600,
		}

		leaseLockPath := LeaseLockPath(worktreePath)
		if err := WriteLeaseLockAtomic(leaseLockPath, lock); err != nil {
			t.Fatalf("WM-013a: WriteLeaseLockAtomic: %v", err)
		}

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

		if len(files) != 1 {
			t.Errorf("WM-013a: .harmonik contains %d file(s) after atomic write, want exactly 1; files: %v", len(files), files)
		} else if files[0] != "lease.lock" {
			t.Errorf("WM-013a: .harmonik file = %q, want %q", files[0], "lease.lock")
		}

		for _, name := range files {
			if strings.Contains(name, ".tmp-") {
				t.Errorf("WM-013a: orphan temp file %q found in .harmonik after atomic write", name)
			}
		}
	})

	t.Run("lock-absent-before-leased-state", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013d"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed entirely by this test.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		leaseLockPath := LeaseLockPath(worktreePath)
		if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
			t.Errorf("WM-013a: lease-lock present before leased state; want absent at workspace_created")
		}

		u := uuid.MustParse(runID)
		lock := &core.LeaseLockFile{
			RunID:     core.RunID(u),
			PID:       os.Getpid(),
			CreatedAt: time.Now().UTC(),
			TTLSec:    3600,
		}
		if err := WriteLeaseLockAtomic(leaseLockPath, lock); err != nil {
			t.Fatalf("WM-013a: WriteLeaseLockAtomic: %v", err)
		}
		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Errorf("WM-013a: lease-lock absent after leased state; want present: %v", err)
		}
	})

	t.Run("read-roundtrip", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713a-8a1b-2c3d4e5f013e"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		// #nosec G204 -- git worktree fixture arguments are constructed entirely by this test.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		u := uuid.MustParse(runID)
		now := time.Now().UTC().Truncate(time.Second)
		written := &core.LeaseLockFile{
			RunID:     core.RunID(u),
			PID:       os.Getpid(),
			CreatedAt: now,
			TTLSec:    7200,
		}

		leaseLockPath := LeaseLockPath(worktreePath)
		if err := WriteLeaseLockAtomic(leaseLockPath, written); err != nil {
			t.Fatalf("WM-013a: WriteLeaseLockAtomic: %v", err)
		}

		read, err := ReadLeaseLock(leaseLockPath)
		if err != nil {
			t.Fatalf("WM-013a: ReadLeaseLock: %v", err)
		}
		if read == nil {
			t.Fatal("WM-013a: ReadLeaseLock returned nil for existing lock")
		}

		if read.RunID.String() != written.RunID.String() {
			t.Errorf("WM-013a: roundtrip run_id = %q, want %q", read.RunID, written.RunID)
		}
		if read.PID != written.PID {
			t.Errorf("WM-013a: roundtrip pid = %d, want %d", read.PID, written.PID)
		}
		if !read.CreatedAt.Equal(written.CreatedAt) {
			t.Errorf("WM-013a: roundtrip created_at = %v, want %v", read.CreatedAt, written.CreatedAt)
		}
		if read.TTLSec != written.TTLSec {
			t.Errorf("WM-013a: roundtrip ttl_sec = %d, want %d", read.TTLSec, written.TTLSec)
		}
	})

	t.Run("read-absent-returns-nil", func(t *testing.T) {
		t.Parallel()

		lock, err := ReadLeaseLock("/nonexistent/workspace/.harmonik/lease.lock")
		if err != nil {
			t.Errorf("WM-013a: ReadLeaseLock absent path: want nil error, got %v", err)
		}
		if lock != nil {
			t.Errorf("WM-013a: ReadLeaseLock absent path: want nil lock, got %+v", lock)
		}
	})
}
