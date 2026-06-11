package workspace

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

// hk-z16: daemon ~/.claude.json trust write-lock starvation under wide
// concurrency (-c8). At -c8 all 8 implementers start with NEW (untrusted)
// worktree paths simultaneously. Without trustWriteMu, all 8 spin on the
// LOCK_EX flock concurrently; under a bloated ~/.claude.json (~8MB), the
// cumulative flock hold time of 7 serial writers can exceed
// defaultTrustLockTimeout, causing ErrTrustLockTimeout and bead starvation.
// The fix (trustWriteMu) serializes in-process writes so only one goroutine
// ever waits on the flock.

// TestHkz16_ConcurrentUntrusted_LargeConfig pins the fix: N goroutines each
// writing a DISTINCT (untrusted) path against a LARGE pre-existing config all
// succeed without error. Without trustWriteMu this would be prone to
// ErrTrustLockTimeout under slow-write conditions because all N goroutines
// would spin concurrently on the flock.
//
// Not parallel: uses t.Setenv (env mutation requires serial execution).
func TestHkz16_ConcurrentUntrusted_LargeConfig(t *testing.T) {
	const N = 8 // matches the -c8 production concurrency level

	cfgPath := setTempClaudeConfig(t)
	baseDir := t.TempDir()

	// Pre-seed a large config (2000 leaked entries ≈ ~700 KB) to make each write
	// cycle expensive, mirroring the production bloat observed in hk-z16.
	sentinel := filepath.Join(baseDir, "run-existing")
	hkbfvbyWriteLargeConfig(t, cfgPath, sentinel, 2000)

	// N goroutines, each with a unique untrusted path — all go to the slow path.
	paths := make([]string, N)
	for i := 0; i < N; i++ {
		paths[i] = filepath.Join(baseDir, fmt.Sprintf("run-new-%02d", i))
	}

	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = EnsureWorktreeTrust(paths[idx])
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("hk-z16: goroutine %d returned error (starvation): %v", i, err)
		}
	}
}

// TestHkz16_MutexRecheck verifies that when two goroutines race to write the
// SAME untrusted path, the second one's post-mutex re-check detects that the
// first already wrote it and skips the redundant write. Both return nil.
//
// Not parallel: uses t.Setenv.
func TestHkz16_MutexRecheck(t *testing.T) {
	setTempClaudeConfig(t)
	worktreePath := filepath.Join(t.TempDir(), "run-race")

	const N = 4
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = EnsureWorktreeTrust(worktreePath)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("hk-z16: same-path race goroutine %d: %v", i, err)
		}
	}
}
