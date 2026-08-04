package daemon

// diskcheck_hksxlb.go — periodic disk watermark check and stale-worktree reclaim.
//
// Two functions are provided:
//
//   - diskFreeBytes(path) — returns available bytes on the filesystem containing
//     path, using syscall.Statfs. Returns (0, err) on failure.
//
//   - runPeriodicDiskCheck(ctx, port) — called once per work-loop poll tick.
//     Rate-limited to diskCheckInterval (default 10 min) for the probe.
//
//     When free space is below the watermark, the daemon sets
//     maint.diskLow = true and emits a disk_low event. It also removes its own
//     stale run worktrees under .harmonik/worktrees/ — directories whose run ID
//     is not in the run registry. Those are the daemon's own leftovers, so it
//     may reclaim them. It touches nothing else.
//
// # The daemon does not delete the Go build cache
//
// This path used to run `go clean -cache` when disk went below the watermark.
// That is removed and it must not come back in any form.
//
// `go clean -cache` deletes the default GOCACHE. That cache is shared. Two
// lanes, many agent worktrees, the daemon's own merge builds, and operator
// terminals all read and write it at the same time. Deleting it mid-build
// gave concurrent test suites "could not import os/context/testing/... no
// such file or directory", and — worse — green runs that never actually
// rebuilt anything. A build that reports success without building is a wrong
// answer that looks like a right one, and it is the reason this path is gone.
//
// The daemon cannot make this call safely. Its merge-build check
// (mergeOrRunInFlight) sees only runs in its own registry. It cannot see a
// `go build` or `go test` that a crew or the operator started in a terminal,
// so it can never know that the delete is safe. Disk pressure is a good
// reason to report a problem. It is not authority to delete a resource that
// other processes are using.
//
// Nothing replaces the reap. Go trims its own build cache, and stale
// per-checkout caches and stale worktrees are the parts that actually grow —
// see docs/disk-reclaim.md for what an operator reclaims by hand.
//
// History, so nobody re-derives it: a cadence-based reap that ran even on a
// healthy disk was removed first (hk-gjbpp, after a stopgap in 5c2276ca and a
// restore in hk-guez). The reactive reap survived that round on the argument
// that disk pressure justified the delete. It does not, for the reason above,
// and it is now removed too.
//
// Spec ref: bead hk-sxlb (logmine F65 disk-watermark guard).
// Fix ref:  bead hk-gjbpp (removed the cadence-based reap).
// Fix ref:  bead hk-5uezz (stale-worktree reclaim).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workspace"
)

// diskReclaimPort is the periodic disk probe and reclaim dependency set. It is
// a value.
//
// projectDir, bus, and runRegistry are ambient daemon values that this port's
// helpers need. Copying them here prevents the disk path from reaching back
// into legacy aggregate.
type diskReclaimPort struct {
	projectDir                string
	bus                       handlercontract.EventEmitter
	runRegistry               *RunRegistry
	diskCheckIntervalOverride time.Duration
	diskFreeBytesFunc         func(path string) (uint64, error)
	worktreeReclaimFunc       func(ctx context.Context, projectDir string, stalePaths []string) error
}

// newDiskReclaimPort projects the disk path's dependencies once at the
// composition seam.
func newDiskReclaimPort(projectDir string, bus handlercontract.EventEmitter, runRegistry *RunRegistry) diskReclaimPort {
	return diskReclaimPort{
		projectDir:  projectDir,
		bus:         bus,
		runRegistry: runRegistry,
	}
}

// diskFreeBytes returns the number of bytes available to unprivileged processes
// on the filesystem containing path. Uses syscall.Statfs (available on
// darwin and linux). Returns (0, err) when the call fails.
func diskFreeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	// Bavail is blocks available to non-superuser; Bsize is the fundamental
	// block size. Bavail * Bsize gives bytes available to unprivileged writers
	// — the relevant figure for ENOSPC prevention.
	return stat.Bavail * uint64(stat.Bsize), nil
}

// mergeOrRunInFlight returns true when one or more bead runs are currently
// registered in port.runRegistry (i.e. any merge-build or run-build is active).
// Non-blocking: reads an atomic counter inside RunRegistry (ActiveRuns count check).
func mergeOrRunInFlight(port diskReclaimPort) bool {
	return port.runRegistry != nil && port.runRegistry.Len() > 0
}

// reclaimStaleWorktrees enumerates .harmonik/worktrees/ and removes directories
// whose basename is not a currently-registered run ID. These are stale worktrees
// from crashed or otherwise-unclean runs whose deferred wtCleanup did not fire.
//
// Called on the low-disk path (disk below watermark, daemon idle). These
// directories belong to the daemon, so it may remove them. It is the only
// reclaim the daemon still performs (hk-5uezz).
//
// Only directories whose names are valid UUID strings are considered; other
// entries (e.g. .gitkeep) are silently skipped.
//
// Returns the count of directories successfully removed.
func reclaimStaleWorktrees(ctx context.Context, port diskReclaimPort) int {
	if port.runRegistry == nil {
		return 0
	}
	worktreesDir := filepath.Join(port.projectDir, workspace.DefaultWorktreeRoot)
	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "daemon: disk-check: reclaimStaleWorktrees: ReadDir %s: %v\n", worktreesDir, err)
		}
		return 0
	}

	var stalePaths []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		uid, parseErr := uuid.Parse(e.Name())
		if parseErr != nil {
			continue // not a UUID-named worktree; skip
		}
		if _, registered := port.runRegistry.Get(core.RunID(uid)); registered {
			continue // in-flight — never remove
		}
		stalePaths = append(stalePaths, filepath.Join(worktreesDir, e.Name()))
	}
	if len(stalePaths) == 0 {
		return 0
	}

	if reclaimErr := runWorktreeReclaim(ctx, port, stalePaths); reclaimErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: disk-check: reclaimStaleWorktrees: %v\n", reclaimErr)
	}
	// Count directories that no longer exist after the reclaim attempt.
	removed := 0
	for _, p := range stalePaths {
		if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
			removed++
		}
	}
	return removed
}

// runWorktreeReclaim removes stale worktree paths via git worktree remove and
// prunes the git worktree list. Uses port.worktreeReclaimFunc as a test seam
// when non-nil; otherwise runs the production git subprocess sequence.
func runWorktreeReclaim(ctx context.Context, port diskReclaimPort, stalePaths []string) error {
	if port.worktreeReclaimFunc != nil {
		return port.worktreeReclaimFunc(ctx, port.projectDir, stalePaths)
	}
	reclaimCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, path := range stalePaths {
		rmCmd := exec.CommandContext(reclaimCtx, "git", "-C", port.projectDir, "worktree", "remove", "--force", "--force", path)
		if out, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
			// Fallback: os.RemoveAll for "not a working tree" and similar git errors.
			fallbackErr := os.RemoveAll(path)
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: git worktree remove %s: %v (%s); fell back to os.RemoveAll (err=%v)\n",
				path, rmErr, strings.TrimSpace(string(out)), fallbackErr)
		}
	}
	pruneCmd := exec.CommandContext(reclaimCtx, "git", "-C", port.projectDir, "worktree", "prune")
	return pruneCmd.Run()
}

// runPeriodicDiskCheck is called once per work-loop poll tick to probe disk
// space. Rate-limited to port.diskCheckIntervalOverride or diskCheckInterval:
// reads available bytes on the project filesystem.
//
//   - Below watermark: sets maint.diskLow = true and emits a disk_low event
//     when port.bus is non-nil. When no run is in flight, the daemon first
//     removes its own stale run worktrees; if that alone brings free space
//     back above the watermark, the latch stays clear.
//
//   - Above watermark: clears maint.diskLow.
//
// The daemon does not delete the Go build cache on either path. See the
// file-level comment for why.
func runPeriodicDiskCheck(ctx context.Context, port diskReclaimPort, maint *loopMaintenanceState) {
	now := time.Now()

	checkInterval := port.diskCheckIntervalOverride
	if checkInterval <= 0 {
		checkInterval = diskCheckInterval
	}
	if time.Since(maint.lastDiskCheck) >= checkInterval {
		maint.lastDiskCheck = now
		runDiskProbe(ctx, port, maint, now, diskLowWatermarkDefault)
	}
}

// runDiskProbe performs the disk watermark probe, the stale-worktree reclaim,
// and the disk_low report.
func runDiskProbe(ctx context.Context, port diskReclaimPort, maint *loopMaintenanceState, now time.Time, watermark uint64) {
	freeBytesFunc := port.diskFreeBytesFunc
	if freeBytesFunc == nil {
		freeBytesFunc = diskFreeBytes
	}

	freeBytes, probeErr := freeBytesFunc(port.projectDir)
	if probeErr != nil {
		// Non-fatal: log and leave diskLow unchanged.
		fmt.Fprintf(os.Stderr, "daemon: disk-check: Statfs %s: %v\n", port.projectDir, probeErr)
		return
	}
	if freeBytes >= watermark {
		if maint.diskLow {
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: recovered — available=%dMiB watermark=%dMiB path=%s — dispatch resumed\n",
				freeBytes/(1024*1024), watermark/(1024*1024), port.projectDir)
		}
		maint.diskLow = false
		return
	}

	// Below watermark: reclaim what the daemon owns, then report.
	if mergeOrRunInFlight(port) {
		// A run holds a worktree under .harmonik/worktrees/. Skip the
		// reclaim this tick and warn. The operator should look at the
		// box if disk stays low across several ticks.
		fmt.Fprintf(os.Stderr,
			"daemon: disk-check: WARN available=%dMiB watermark=%dMiB path=%s — "+
				"disk below watermark but a run is in flight; worktree reclaim deferred to next tick\n",
			freeBytes/(1024*1024), watermark/(1024*1024), port.projectDir)
	} else if reclaimedCount := reclaimStaleWorktrees(ctx, port); reclaimedCount > 0 {
		// Re-probe. If the daemon's own leftovers were the problem, the
		// disk is healthy again and there is nothing to report.
		if newFree, reprobeErr := freeBytesFunc(port.projectDir); reprobeErr == nil && newFree >= watermark {
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: reclaimed %d stale worktree(s) — "+
					"disk recovered available=%dMiB watermark=%dMiB path=%s\n",
				reclaimedCount, newFree/(1024*1024), watermark/(1024*1024), port.projectDir)
			maint.diskLow = false
			return
		}
	}

	if port.bus != nil {
		// GoCacheCleanAttempted and GoCacheCleanError stay at their zero
		// values on purpose. The daemon no longer runs `go clean -cache`,
		// so there is never anything to report in them. The fields remain
		// on the payload for wire compatibility and are owned by
		// internal/core; removing them is a separate change.
		payload := core.DiskLowPayload{
			AvailableBytes: freeBytes,
			WatermarkBytes: watermark,
			ProjectPath:    port.projectDir,
			DetectedAt:     now.UTC().Format(time.RFC3339),
		}
		if pb, marshalErr := json.Marshal(payload); marshalErr == nil {
			_ = port.bus.Emit(ctx, core.EventTypeDiskLow, pb) //nolint:errcheck // best-effort disk_low event emit
		}
	}
	fmt.Fprintf(os.Stderr,
		"daemon: disk-check: available=%dMiB watermark=%dMiB path=%s — dispatch paused\n",
		freeBytes/(1024*1024), watermark/(1024*1024), port.projectDir)
	maint.diskLow = true
}
