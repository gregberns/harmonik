package daemon

// export_maintenance_test.go — test-seam exports for the internal/daemon
// periodic-maintenance loop (RT19.17 split of export_test.go): the
// loopMaintenanceState handle, disk-check and stale-worktree-reclaim seams.
// package daemon test file; see export_test.go header for the seam rationale.
// Bead: hk-ecrxy.

import (
	"context"
	"time"
)

// ExportedMaintState is an opaque test handle over the runWorkLoop-local
// loopMaintenanceState (RSM-011: the periodic-maintenance value fields were
// lifted off workLoopDeps). Tests create one via ExportedNewMaintState and
// thread it through ExportedRunPeriodicDiskCheck so per-run state (diskLow,
// last-probe timestamps) persists across calls, as it did on deps before.
type ExportedMaintState struct{ m loopMaintenanceState }

// ExportedNewMaintState returns a fresh maintenance-state handle.
func ExportedNewMaintState() *ExportedMaintState { return &ExportedMaintState{} }

// ExportedRunPeriodicDiskCheck calls runPeriodicDiskCheck with the given deps
// and maintenance-state handle. Used by diskcheck_hksxlb_test.go to drive the
// reaper directly without running the full work loop (hk-guez).
func ExportedRunPeriodicDiskCheck(ctx context.Context, deps *workLoopDeps, ms *ExportedMaintState) {
	runPeriodicDiskCheck(ctx, deps, &ms.m)
}

// ExportedDiskCheckDiskLow reads the diskLow field from the maintenance-state
// handle. Used by diskcheck_hksxlb_test.go to assert post-call state (hk-guez).
func ExportedDiskCheckDiskLow(ms *ExportedMaintState) bool {
	return ms.m.diskLow
}

// ExportedDiskCheckSetCheckInterval overrides the disk-probe interval on deps
// so tests fire immediately. A zero override restores the production default
// (diskCheckInterval).
//
// Bead ref: hk-guez.
func ExportedDiskCheckSetCheckInterval(deps *workLoopDeps, d time.Duration) {
	deps.diskCheckIntervalOverride = d
}

// ExportedReclaimStaleWorktrees calls reclaimStaleWorktrees with the given deps
// and returns the count of stale worktrees removed. Used by
// diskcheck_hksxlb_test.go to drive the reclaim step directly (hk-5uezz).
func ExportedReclaimStaleWorktrees(ctx context.Context, deps *workLoopDeps) int {
	return reclaimStaleWorktrees(ctx, deps)
}
