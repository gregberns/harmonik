package daemon_test

// diskcheck_hksxlb_test.go — unit tests for the merge-aware cache reaper (hk-guez).
//
// DONE-CHECK (from bead spec):
//   1. The reaper does NOT run `go clean -cache` while a merge-build is in
//      flight (runRegistry.Len() > 0 — ActiveRuns count guard).
//   2. The reaper DOES run `go clean -cache` when idle (runRegistry.Len()==0).
//
// Both the reactive (below-watermark) and proactive (60-min cadence) paths are
// covered for each scenario.
//
// Shared stubs (stubBeadLedger, stubEventCollector) are defined in
// workloop_test.go in this same package (daemon_test).
//
// Helper prefix: diskCheckFixture (per implementer-protocol.md §Helper-prefix).
// Bead ref: hk-guez.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// diskCheckFixtureBelowWatermark returns a diskFreeBytesFunc that always
// reports 1 byte free — guaranteed below diskLowWatermarkDefault (10 GiB).
func diskCheckFixtureBelowWatermark() func(string) (uint64, error) {
	return func(_ string) (uint64, error) { return 1, nil }
}

// diskCheckFixtureAboveWatermark returns a diskFreeBytesFunc that always
// reports 100 GiB free — guaranteed above the default watermark.
func diskCheckFixtureAboveWatermark() func(string) (uint64, error) {
	return func(_ string) (uint64, error) { return 100 * 1024 * 1024 * 1024, nil }
}

// diskCheckFixtureCounter returns a goCacheCleanFunc that atomically increments
// a counter on each invocation, and a pointer to that counter for assertions.
func diskCheckFixtureCounter() (*int32, func() error) {
	var n int32
	return &n, func() error { atomic.AddInt32(&n, 1); return nil }
}

// diskCheckFixtureRegisterRuns populates reg with runCount fake RunHandles.
// Each handle gets a unique UUIDv7 run-id.
func diskCheckFixtureRegisterRuns(t *testing.T, reg *daemon.RunRegistry, runCount int) {
	t.Helper()
	for range runCount {
		runUUID, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("diskCheckFixtureRegisterRuns: uuid: %v", err)
		}
		daemon.ExportedRunRegistryRegister(reg, core.RunID(runUUID), &daemon.RunHandle{BeadID: "test-bead"})
	}
}

// diskCheckFixtureBuildDeps constructs a WorkLoopDepsParams configured for
// disk-check unit tests.
//
//   - runCount fake RunHandles are pre-registered so runRegistry.Len()==runCount.
//   - freeBytesFunc controls the apparent free-space reading.
//   - cleanFn captures or stubs `go clean -cache`.
//
// The caller must set interval overrides via ExportedDiskCheckSetCheckInterval /
// ExportedDiskCheckSetGoCacheCleanInterval before calling
// ExportedRunPeriodicDiskCheck.
func diskCheckFixtureBuildDeps(
	t *testing.T,
	runCount int,
	freeBytesFunc func(string) (uint64, error),
	cleanFn func() error,
) daemon.WorkLoopDepsParams {
	t.Helper()

	reg := daemon.ExportedNewRunRegistry()
	diskCheckFixtureRegisterRuns(t, reg, runCount)

	return daemon.WorkLoopDepsParams{
		Bus:               &stubEventCollector{}, // defined in workloop_test.go
		BrAdapter:         &stubBeadLedger{},     // defined in workloop_test.go
		ProjectDir:        t.TempDir(),
		IntentLogDir:      t.TempDir(),
		RunRegistry:       reg,
		DiskFreeBytesFunc: freeBytesFunc,
		GoCacheCleanFunc:  cleanFn,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — reactive (below-watermark) path
// ─────────────────────────────────────────────────────────────────────────────

// TestDiskCheck_ReactiveReaper_SkipsWhenRunsInFlight verifies that the reactive
// reap does NOT invoke `go clean -cache` when ActiveRuns > 0 (merge-build in
// flight). The disk-low flag is still set so dispatch is paused.
func TestDiskCheck_ReactiveReaper_SkipsWhenRunsInFlight(t *testing.T) {
	t.Parallel()

	cleanCount, cleanFn := diskCheckFixtureCounter()
	deps := daemon.ExportedWorkLoopDeps(
		diskCheckFixtureBuildDeps(t, 1 /* run in flight */, diskCheckFixtureBelowWatermark(), cleanFn),
	)
	daemon.ExportedDiskCheckSetCheckInterval(&deps, time.Nanosecond)

	daemon.ExportedRunPeriodicDiskCheck(context.Background(), &deps)

	if got := atomic.LoadInt32(cleanCount); got != 0 {
		t.Errorf("go clean -cache called %d time(s) with a run in flight; want 0", got)
	}
	if !daemon.ExportedDiskCheckDiskLow(&deps) {
		t.Error("diskLow should be true after a below-watermark probe")
	}
}

// TestDiskCheck_ReactiveReaper_RunsWhenIdle verifies that the reactive reap
// DOES invoke `go clean -cache` when ActiveRuns == 0 (idle).
func TestDiskCheck_ReactiveReaper_RunsWhenIdle(t *testing.T) {
	t.Parallel()

	cleanCount, cleanFn := diskCheckFixtureCounter()
	deps := daemon.ExportedWorkLoopDeps(
		diskCheckFixtureBuildDeps(t, 0 /* idle */, diskCheckFixtureBelowWatermark(), cleanFn),
	)
	daemon.ExportedDiskCheckSetCheckInterval(&deps, time.Nanosecond)

	daemon.ExportedRunPeriodicDiskCheck(context.Background(), &deps)

	if got := atomic.LoadInt32(cleanCount); got != 1 {
		t.Errorf("go clean -cache called %d time(s) when idle; want 1", got)
	}
	if !daemon.ExportedDiskCheckDiskLow(&deps) {
		t.Error("diskLow should be true after a below-watermark probe")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — proactive (60-min cadence) path
// ─────────────────────────────────────────────────────────────────────────────

// TestDiskCheck_ProactiveReaper_SkipsWhenRunsInFlight verifies that the
// proactive reap does NOT fire when ActiveRuns > 0, even when the proactive
// interval has elapsed and disk is healthy.
func TestDiskCheck_ProactiveReaper_SkipsWhenRunsInFlight(t *testing.T) {
	t.Parallel()

	cleanCount, cleanFn := diskCheckFixtureCounter()
	deps := daemon.ExportedWorkLoopDeps(
		diskCheckFixtureBuildDeps(t, 1 /* run in flight */, diskCheckFixtureAboveWatermark(), cleanFn),
	)
	daemon.ExportedDiskCheckSetCheckInterval(&deps, time.Nanosecond)
	daemon.ExportedDiskCheckSetGoCacheCleanInterval(&deps, time.Nanosecond)

	daemon.ExportedRunPeriodicDiskCheck(context.Background(), &deps)

	if got := atomic.LoadInt32(cleanCount); got != 0 {
		t.Errorf("proactive go clean -cache called %d time(s) with a run in flight; want 0", got)
	}
}

// TestDiskCheck_ProactiveReaper_RunsWhenIdle verifies that the proactive reap
// fires when idle AND the proactive interval has elapsed (hk-y3frr restored).
func TestDiskCheck_ProactiveReaper_RunsWhenIdle(t *testing.T) {
	t.Parallel()

	cleanCount, cleanFn := diskCheckFixtureCounter()
	deps := daemon.ExportedWorkLoopDeps(
		diskCheckFixtureBuildDeps(t, 0 /* idle */, diskCheckFixtureAboveWatermark(), cleanFn),
	)
	daemon.ExportedDiskCheckSetCheckInterval(&deps, time.Nanosecond)
	daemon.ExportedDiskCheckSetGoCacheCleanInterval(&deps, time.Nanosecond)

	daemon.ExportedRunPeriodicDiskCheck(context.Background(), &deps)

	if got := atomic.LoadInt32(cleanCount); got != 1 {
		t.Errorf("proactive go clean -cache called %d time(s) when idle; want 1", got)
	}
}

// TestDiskCheck_ProactiveReaper_TOCTOU verifies that cacheReapMu provides
// reap↔dispatch mutual exclusion: Register (RLock) blocks while the proactive
// reap holds the WLock, and proceeds immediately after the reap releases it.
//
// Bead ref: hk-y3frr.
func TestDiskCheck_ProactiveReaper_TOCTOU(t *testing.T) {
	t.Parallel()

	// cleanStarted signals that the reap has acquired the WLock and is
	// executing the (stubbed) clean.  cleanRelease controls when the stub
	// releases the WLock back (simulating a long-running `go clean -cache`).
	cleanStarted := make(chan struct{})
	cleanRelease := make(chan struct{})

	// Stub clean: signal that we've started, then block until the test allows
	// us to finish — simulating a slow `go clean -cache`.
	stubClean := func() error {
		close(cleanStarted)
		<-cleanRelease
		return nil
	}

	mu := &sync.RWMutex{}
	params := diskCheckFixtureBuildDeps(t, 0 /* idle */, diskCheckFixtureAboveWatermark(), stubClean)
	params.CacheReapMu = mu
	deps := daemon.ExportedWorkLoopDeps(params)
	daemon.ExportedDiskCheckSetCheckInterval(&deps, time.Nanosecond)
	daemon.ExportedDiskCheckSetGoCacheCleanInterval(&deps, time.Nanosecond)

	// Start the proactive reap in a goroutine; it will block inside stubClean.
	reapDone := make(chan struct{})
	go func() {
		defer close(reapDone)
		daemon.ExportedRunPeriodicDiskCheck(context.Background(), &deps)
	}()

	// Wait until the reap has acquired the WLock (stub signals cleanStarted).
	select {
	case <-cleanStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reap to start")
	}

	// With the WLock held by the reap, an RLock attempt (simulating Register)
	// must block — verify it does not succeed immediately.
	rlockAcquired := make(chan struct{})
	go func() {
		mu.RLock()
		close(rlockAcquired)
		mu.RUnlock()
	}()

	select {
	case <-rlockAcquired:
		t.Fatal("RLock (Register) should block while reap holds WLock — TOCTOU not fixed")
	case <-time.After(50 * time.Millisecond):
		// Good: RLock is blocked.
	}

	// Release the reap; the RLock should now succeed promptly.
	close(cleanRelease)

	select {
	case <-rlockAcquired:
		// Good: Register can proceed after the reap finishes.
	case <-time.After(5 * time.Second):
		t.Fatal("RLock (Register) did not unblock after reap released WLock")
	}

	<-reapDone
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests — stale worktree reclaim path (hk-5uezz)
// ─────────────────────────────────────────────────────────────────────────────

// diskCheckFixtureStaleWorktrees creates fake worktree directories under
// projectDir's .harmonik/worktrees/ directory to simulate stale leftovers.
// Returns the UUIDs of the created directories.
func diskCheckFixtureStaleWorktrees(t *testing.T, projectDir string, count int) []string {
	t.Helper()
	worktreesDir := filepath.Join(projectDir, ".harmonik", "worktrees")
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		t.Fatalf("diskCheckFixtureStaleWorktrees: mkdir %s: %v", worktreesDir, err)
	}
	ids := make([]string, count)
	for i := range count {
		uid, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("diskCheckFixtureStaleWorktrees: uuid: %v", err)
		}
		dir := filepath.Join(worktreesDir, uid.String())
		if mkErr := os.Mkdir(dir, 0o755); mkErr != nil {
			t.Fatalf("diskCheckFixtureStaleWorktrees: mkdir %s: %v", dir, mkErr)
		}
		ids[i] = uid.String()
	}
	return ids
}

// TestDiskCheck_ReactiveReaper_ReclaimsWorktreesBeforeClean verifies that when
// stale worktrees are reclaimed and disk recovers above the watermark, the
// reactive path returns without calling `go clean -cache` (hk-5uezz).
func TestDiskCheck_ReactiveReaper_ReclaimsWorktreesBeforeClean(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	_ = diskCheckFixtureStaleWorktrees(t, projectDir, 2) // 2 stale worktrees

	// Disk probe: low first call, high after reclaim.
	probeCallCount := 0
	freeBytesFunc := func(_ string) (uint64, error) {
		probeCallCount++
		if probeCallCount == 1 {
			return 1, nil // below watermark
		}
		return 100 * 1024 * 1024 * 1024, nil // above watermark after reclaim
	}

	// Reclaim func: delete the dirs so the re-probe sees freed space.
	reclaimedPaths := make([]string, 0)
	reclaimFunc := func(_ context.Context, _ string, stalePaths []string) error {
		reclaimedPaths = append(reclaimedPaths, stalePaths...)
		for _, p := range stalePaths {
			_ = os.RemoveAll(p)
		}
		return nil
	}

	cleanCount, cleanFn := diskCheckFixtureCounter()

	params := diskCheckFixtureBuildDeps(t, 0, freeBytesFunc, cleanFn)
	params.ProjectDir = projectDir
	params.WorktreeReclaimFunc = reclaimFunc
	deps := daemon.ExportedWorkLoopDeps(params)
	daemon.ExportedDiskCheckSetCheckInterval(&deps, time.Nanosecond)

	daemon.ExportedRunPeriodicDiskCheck(context.Background(), &deps)

	if got := atomic.LoadInt32(cleanCount); got != 0 {
		t.Errorf("go clean -cache called %d time(s) after worktree reclaim recovered disk; want 0", got)
	}
	if len(reclaimedPaths) != 2 {
		t.Errorf("reclaimFunc called with %d paths; want 2", len(reclaimedPaths))
	}
	if daemon.ExportedDiskCheckDiskLow(&deps) {
		t.Error("diskLow should be false after disk recovered via worktree reclaim")
	}
}

// TestDiskCheck_ReactiveReaper_FallsBackToCacheCleanWhenReclaimInsufficient
// verifies that when stale worktree reclaim does not recover disk above the
// watermark, the reactive path still falls through to `go clean -cache`
// (hk-5uezz).
func TestDiskCheck_ReactiveReaper_FallsBackToCacheCleanWhenReclaimInsufficient(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	_ = diskCheckFixtureStaleWorktrees(t, projectDir, 1) // 1 stale worktree

	// Disk probe: always below watermark (reclaim didn't help enough).
	freeBytesFunc := func(_ string) (uint64, error) {
		return 1, nil // always below watermark
	}

	// Reclaim func: delete the dir but disk stays low.
	reclaimFunc := func(_ context.Context, _ string, stalePaths []string) error {
		for _, p := range stalePaths {
			_ = os.RemoveAll(p)
		}
		return nil
	}

	cleanCount, cleanFn := diskCheckFixtureCounter()

	params := diskCheckFixtureBuildDeps(t, 0, freeBytesFunc, cleanFn)
	params.ProjectDir = projectDir
	params.WorktreeReclaimFunc = reclaimFunc
	deps := daemon.ExportedWorkLoopDeps(params)
	daemon.ExportedDiskCheckSetCheckInterval(&deps, time.Nanosecond)

	daemon.ExportedRunPeriodicDiskCheck(context.Background(), &deps)

	if got := atomic.LoadInt32(cleanCount); got != 1 {
		t.Errorf("go clean -cache called %d time(s) after insufficient reclaim; want 1", got)
	}
	if !daemon.ExportedDiskCheckDiskLow(&deps) {
		t.Error("diskLow should be true when disk remains below watermark after reclaim")
	}
}

// TestDiskCheck_ReactiveReaper_SkipsRegisteredWorktrees verifies that worktrees
// whose directory name matches a registered run ID are NOT removed by
// reclaimStaleWorktrees (hk-5uezz).
func TestDiskCheck_ReactiveReaper_SkipsRegisteredWorktrees(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	// Create two worktrees: one "stale" (not registered), one "active" (registered).
	worktreesDir := filepath.Join(projectDir, ".harmonik", "worktrees")
	if err := os.MkdirAll(worktreesDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", worktreesDir, err)
	}

	staleUID, _ := uuid.NewV7()
	staleDir := filepath.Join(worktreesDir, staleUID.String())
	if err := os.Mkdir(staleDir, 0o755); err != nil {
		t.Fatalf("mkdir stale worktree: %v", err)
	}

	activeUID, _ := uuid.NewV7()
	activeDir := filepath.Join(worktreesDir, activeUID.String())
	if err := os.Mkdir(activeDir, 0o755); err != nil {
		t.Fatalf("mkdir active worktree: %v", err)
	}

	// Register the active worktree's run ID in the registry.
	reg := daemon.ExportedNewRunRegistry()
	daemon.ExportedRunRegistryRegister(reg, core.RunID(activeUID), &daemon.RunHandle{BeadID: "active-bead"})

	var reclaimedPaths []string
	reclaimFunc := func(_ context.Context, _ string, stalePaths []string) error {
		reclaimedPaths = append(reclaimedPaths, stalePaths...)
		return nil
	}

	params := daemon.WorkLoopDepsParams{
		Bus:                 &stubEventCollector{},
		BrAdapter:           &stubBeadLedger{},
		ProjectDir:          projectDir,
		IntentLogDir:        t.TempDir(),
		RunRegistry:         reg,
		WorktreeReclaimFunc: reclaimFunc,
	}
	deps := daemon.ExportedWorkLoopDeps(params)

	daemon.ExportedReclaimStaleWorktrees(context.Background(), &deps)

	if len(reclaimedPaths) != 1 {
		t.Errorf("reclaimFunc called with %d paths; want 1 (only the stale one)", len(reclaimedPaths))
	}
	if len(reclaimedPaths) == 1 && filepath.Base(reclaimedPaths[0]) != staleUID.String() {
		t.Errorf("reclaimFunc called with %q; want stale dir %q", reclaimedPaths[0], staleUID.String())
	}
	// Active worktree dir must still exist (was never passed to reclaimFunc).
	if _, statErr := os.Stat(activeDir); os.IsNotExist(statErr) {
		t.Error("active worktree was incorrectly removed")
	}
}
