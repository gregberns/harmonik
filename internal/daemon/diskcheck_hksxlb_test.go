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
// fires when idle AND the proactive interval has elapsed.
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
