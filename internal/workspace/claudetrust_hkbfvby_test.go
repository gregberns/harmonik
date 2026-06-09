package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// hk-bfvby — the daemon's ~16-min spawn stall was EnsureWorktreeTrust taking an
// UNBOUNDED LOCK_EX over a bloated ~/.claude.json on EVERY launch, even when the
// worktree was already trusted. These tests pin the fix:
//
//   - the already-trusted path takes NO exclusive lock and does NOT rewrite the
//     file (mtime + a held LOCK_EX both prove it never enters the write path);
//   - the bounded-acquire path returns a STRUCTURAL error instead of hanging when
//     the write lock is held by another process.

// hkbfvbyWriteLargeConfig writes a config file with n already-trusted project
// entries plus the one under test, so the file is large enough to mirror the
// production bloat that made the per-call rewrite expensive.
func hkbfvbyWriteLargeConfig(t *testing.T, cfgPath, trustedPath string, n int) {
	t.Helper()
	projects := make(map[string]interface{}, n+1)
	for i := 0; i < n; i++ {
		projects[fmt.Sprintf("/leaked/worktree/run-%05d", i)] = map[string]interface{}{
			"hasTrustDialogAccepted": true,
			"lastCost":               float64(i),
		}
	}
	projects[trustedPath] = map[string]interface{}{
		"hasTrustDialogAccepted": true,
	}
	cfg := map[string]interface{}{"projects": projects}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("hk-bfvby: marshal large config: %v", err)
	}
	if err := os.WriteFile(cfgPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("hk-bfvby: write large config: %v", err)
	}
}

// hkbfvbyMtime returns the modification time of path.
func hkbfvbyMtime(t *testing.T, path string) time.Time {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("hk-bfvby: stat %s: %v", path, err)
	}
	return fi.ModTime()
}

// TestHkbfvby_AlreadyTrusted_NoRewrite verifies that when the worktree path is
// already trusted, ensureWorktreeTrustAt does NOT rewrite the config file: its
// mtime is unchanged. This is the primary fix — the already-trusted launch must
// not enter the read-modify-write (and thus must not take LOCK_EX).
func TestHkbfvby_AlreadyTrusted_NoRewrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-trusted")

	hkbfvbyWriteLargeConfig(t, cfgPath, worktreePath, 2000)
	before := hkbfvbyMtime(t, cfgPath)

	// Back-date the file so even a coarse-granularity filesystem mtime can detect
	// a rewrite, and sleep past the timestamp resolution boundary.
	old := before.Add(-2 * time.Second)
	if err := os.Chtimes(cfgPath, old, old); err != nil {
		t.Fatalf("hk-bfvby: chtimes: %v", err)
	}
	backdated := hkbfvbyMtime(t, cfgPath)

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("hk-bfvby: ensureWorktreeTrustAt (already-trusted): %v", err)
	}

	after := hkbfvbyMtime(t, cfgPath)
	if !after.Equal(backdated) {
		t.Errorf("hk-bfvby: already-trusted path rewrote the file (mtime changed %v -> %v); the fast path must not write",
			backdated, after)
	}
}

// TestHkbfvby_AlreadyTrusted_LockFree proves the already-trusted path takes no
// exclusive lock: we hold LOCK_EX on the sidecar lockfile for the whole call.
// If the fast path took LOCK_EX it would block on our held lock until the bound
// elapsed (or forever pre-fix); instead it returns promptly via the lock-free
// probe. We assert it returns well under the bounded write-lock timeout.
func TestHkbfvby_AlreadyTrusted_LockFree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-lockfree")

	hkbfvbyWriteLargeConfig(t, cfgPath, worktreePath, 500)

	// Hold LOCK_EX on the sidecar for the duration of the call.
	lockPath := cfgPath + ".lock"
	lockFd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("hk-bfvby: open lockfile: %v", err)
	}
	defer lockFd.Close() //nolint:errcheck // advisory lock fd
	if err := syscall.Flock(int(lockFd.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("hk-bfvby: hold LOCK_EX: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- ensureWorktreeTrustAt(worktreePath, cfgPath) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("hk-bfvby: already-trusted lock-free call errored: %v", err)
		}
		// Returned while we still hold LOCK_EX → it never needed the write lock.
	case <-time.After(2 * time.Second):
		t.Fatal("hk-bfvby: already-trusted call blocked while LOCK_EX was held; the fast path is not lock-free")
	}
}

// TestHkbfvby_BoundedAcquire_TimesOut verifies the write (untrusted) path bounds
// its LOCK_EX acquire: with another holder of LOCK_EX, the call returns a
// structural ErrTrustLockTimeout within roughly the bound rather than hanging
// indefinitely. We shrink the bound via acquireExclusiveBounded directly so the
// test stays fast and deterministic.
func TestHkbfvby_BoundedAcquire_TimesOut(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".claude.json.lock")

	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("hk-bfvby: open holder: %v", err)
	}
	defer holder.Close() //nolint:errcheck // advisory lock fd
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("hk-bfvby: holder LOCK_EX: %v", err)
	}

	waiter, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("hk-bfvby: open waiter: %v", err)
	}
	defer waiter.Close() //nolint:errcheck // advisory lock fd

	start := time.Now()
	gotErr := acquireExclusiveBounded(int(waiter.Fd()), 200*time.Millisecond)
	elapsed := time.Since(start)

	if gotErr == nil {
		t.Fatal("hk-bfvby: bounded acquire returned nil while LOCK_EX was held; it must time out")
	}
	if !errors.Is(gotErr, ErrTrustLockTimeout) {
		t.Errorf("hk-bfvby: bounded acquire error = %v; want ErrTrustLockTimeout", gotErr)
	}
	if !errors.Is(gotErr, handlercontract.ErrStructural) {
		t.Errorf("hk-bfvby: bounded-acquire timeout is not structural (errors.Is ErrStructural false): %v", gotErr)
	}
	// It returned around the bound, not "minutes" / never. Allow generous slack.
	if elapsed > 3*time.Second {
		t.Errorf("hk-bfvby: bounded acquire took %v; expected to bail near the 200ms bound", elapsed)
	}
}

// TestHkbfvby_BoundedAcquire_SucceedsWhenFree verifies the bounded acquire is
// not a no-op: when the lock is free it acquires immediately and returns nil.
func TestHkbfvby_BoundedAcquire_SucceedsWhenFree(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	lockPath := filepath.Join(dir, ".claude.json.lock")

	fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("hk-bfvby: open lock: %v", err)
	}
	defer fd.Close() //nolint:errcheck // advisory lock fd

	if err := acquireExclusiveBounded(int(fd.Fd()), 2*time.Second); err != nil {
		t.Fatalf("hk-bfvby: bounded acquire on a free lock errored: %v", err)
	}
}

// TestHkbfvby_ConcurrentAlreadyTrusted_NoWrites verifies that N concurrent
// already-trusted launches against the same large config perform NO writes
// (mtime unchanged) and never error — the production hot path under load.
func TestHkbfvby_ConcurrentAlreadyTrusted_NoWrites(t *testing.T) {
	t.Parallel()
	const n = 24
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-hot")

	hkbfvbyWriteLargeConfig(t, cfgPath, worktreePath, 1000)
	old := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(cfgPath, old, old); err != nil {
		t.Fatalf("hk-bfvby: chtimes: %v", err)
	}
	before := hkbfvbyMtime(t, cfgPath)

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = ensureWorktreeTrustAt(worktreePath, cfgPath)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("hk-bfvby: concurrent already-trusted goroutine %d errored: %v", i, err)
		}
	}
	if after := hkbfvbyMtime(t, cfgPath); !after.Equal(before) {
		t.Errorf("hk-bfvby: concurrent already-trusted launches rewrote the file (mtime %v -> %v)", before, after)
	}
}

// TestHkbfvby_PruneWorktreeTrust_RemovesEntry verifies the leak fix: pruning a
// worktree's trust key removes only that entry, preserves the rest, and is a
// no-op (no rewrite) when the entry is absent.
func TestHkbfvby_PruneWorktreeTrust_RemovesEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	target := filepath.Join(dir, "worktrees", "run-prune")
	keep := filepath.Join(dir, "worktrees", "run-keep")

	cfg := map[string]interface{}{
		"theme": "dark",
		"projects": map[string]interface{}{
			target: map[string]interface{}{"hasTrustDialogAccepted": true},
			keep:   map[string]interface{}{"hasTrustDialogAccepted": true},
		},
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(cfgPath, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("hk-bfvby: write config: %v", err)
	}

	if err := pruneWorktreeTrustAt(target, cfgPath); err != nil {
		t.Fatalf("hk-bfvby: pruneWorktreeTrustAt: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	var got map[string]interface{}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("hk-bfvby: unmarshal after prune: %v", err)
	}
	if got["theme"] != "dark" {
		t.Errorf("hk-bfvby: prune lost top-level key; theme=%v", got["theme"])
	}
	projects, _ := got["projects"].(map[string]interface{})
	if _, present := projects[target]; present {
		t.Errorf("hk-bfvby: prune did not remove target entry %s", target)
	}
	if _, present := projects[keep]; !present {
		t.Errorf("hk-bfvby: prune removed an unrelated entry %s", keep)
	}

	// Pruning an absent entry must be a no-op that does not rewrite the file.
	old := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(cfgPath, old, old); err != nil {
		t.Fatalf("hk-bfvby: chtimes: %v", err)
	}
	before := hkbfvbyMtime(t, cfgPath)
	if err := pruneWorktreeTrustAt(target, cfgPath); err != nil {
		t.Fatalf("hk-bfvby: prune absent entry: %v", err)
	}
	if after := hkbfvbyMtime(t, cfgPath); !after.Equal(before) {
		t.Errorf("hk-bfvby: prune of an absent entry rewrote the file (mtime %v -> %v)", before, after)
	}
}
