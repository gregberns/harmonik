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
// lifted off testRuntime). Tests create one via ExportedNewMaintState and
// thread it through ExportedRunPeriodicDiskCheck so per-run state (diskLow,
// last-probe timestamps) persists across calls, as it did on deps before.
type ExportedMaintState struct{ m loopMaintenanceState }

// ExportedNewMaintState returns a fresh maintenance-state handle.
func ExportedNewMaintState() *ExportedMaintState { return &ExportedMaintState{} }

// ExportedRunPeriodicDiskCheck calls runPeriodicDiskCheck with the given port
// and maintenance-state handle, to drive the reaper directly without running
// the full work loop (hk-guez).
//
// Used by loopmaintenance_test.go TestDiskLowBranch. The original caller,
// diskcheck_hksxlb_test.go, was deleted, and this comment went on naming it —
// which left every shim in this file looking covered while all four had zero
// callers. Name a live caller here or say there is none.
func ExportedRunPeriodicDiskCheck(ctx context.Context, port diskReclaimPort, ms *ExportedMaintState) {
	runPeriodicDiskCheck(ctx, port, &ms.m)
}

// ExportedDiskCheckDiskLow reads the diskLow field from the maintenance-state
// handle, to assert post-call state (hk-guez).
//
// Used by loopmaintenance_test.go TestDiskLowBranch.
func ExportedDiskCheckDiskLow(ms *ExportedMaintState) bool {
	return ms.m.diskLow
}

// ExportedDiskCheckSetCheckInterval overrides the disk-probe interval on port
// so tests fire immediately. A zero override restores the production default
// (diskCheckInterval).
//
// Used by loopmaintenance_test.go TestDiskLowBranch.
//
// Bead ref: hk-guez.
func ExportedDiskCheckSetCheckInterval(port *diskReclaimPort, d time.Duration) {
	port.diskCheckIntervalOverride = d
}

// ExportedDiskReclaimPortForTesting projects a disk port from the supplied
// work-loop values, then installs all test seams. The caller passes the value
// to ExportedRunWorkLoopWithDiskReclaim.
//
// There is no go-cache seam any more. The daemon does not run
// `go clean -cache`, so there is no subprocess left to stub on that path.
func ExportedDiskReclaimPortForTesting(deps testRuntime, interval time.Duration,
	freeBytes func(string) (uint64, error),
	reclaim func(context.Context, string, []string) error,
) diskReclaimPort {
	port := deps.diskReclaim()
	port.diskCheckIntervalOverride = interval
	port.diskFreeBytesFunc = freeBytes
	port.worktreeReclaimFunc = reclaim
	return port
}

// newTestDiskReclaimPort keeps broad work-loop tests independent of the host
// filesystem. Disk-specific tests use ExportedDiskReclaimPortForTesting to
// select their own probe result and cleanup seams.
func newTestDiskReclaimPort(deps testRuntime) diskReclaimPort {
	port := deps.diskReclaim()
	port.diskFreeBytesFunc = func(string) (uint64, error) { return 1 << 62, nil }
	port.worktreeReclaimFunc = func(context.Context, string, []string) error { return nil }
	return port
}

// ExportedReclaimStaleWorktrees calls reclaimStaleWorktrees with the given port
// and returns the count of stale worktrees removed, to drive the reclaim step
// directly (hk-5uezz).
//
// NO CALLER at present. diskcheck_hksxlb_test.go, which this comment used to
// name, was deleted. TestDiskLowBranch covers the low-disk branch but not the
// stale-worktree reclaim, which needs a run registry and UUID-named worktree
// directories. Wire port.worktreeReclaimFunc when you write that test — it keeps
// `git worktree remove` out of the run.
func ExportedReclaimStaleWorktrees(ctx context.Context, port diskReclaimPort) int {
	return reclaimStaleWorktrees(ctx, port)
}
