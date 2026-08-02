package daemon

// diskcheck_hksxlb.go — periodic disk watermark check and go-cache reap.
//
// Two functions are provided:
//
//   - diskFreeBytes(path) — returns available bytes on the filesystem containing
//     path, using syscall.Statfs. Returns (0, err) on failure.
//
//   - runPeriodicDiskCheck(ctx, port) — called once per work-loop poll tick.
//     Rate-limited to diskCheckInterval (default 10 min) for the probe.
//
//     Reactive reap only: when disk is below the watermark, sets
//     maint.diskLow = true, emits a disk_low event, and runs `go clean -cache`
//     only when no merge-build is in flight (runRegistry.Len()==0). If a
//     merge-build IS in flight, the reap is deferred to the next tick and a
//     warning is logged instead of corrupting the build (hk-guez fix for the
//     stopgap in 5c2276ca).
//
// A time-based proactive reap (running `go clean -cache` on a fixed cadence
// even when disk was healthy) existed here previously and was REMOVED
// (hk-gjbpp). It had no knowledge of `go build` / `go test` invoked
// out-of-band by crews and operators in terminals, and it wiped the shared
// default GOCACHE those runs depend on: observed cache collapse 122M -> 2.8M
// mid-build, with concurrent test suites failing with "could not import
// os/context/testing/... no such file or directory" and silently reporting
// wrong results in BOTH directions (phantom failures and green runs that
// never actually built) — with disk nowhere near the watermark (30GiB free
// vs ~10GiB). This path was already removed once as a stopgap (5c2276ca) and
// then restored (hk-guez) under the belief the cache would otherwise grow
// unbounded; it will NOT be restored a third time — the reactive reap (disk
// pressure) is the only legitimate trigger for wiping a cache other
// processes depend on. Do not reintroduce a cadence-based reap gated only on
// daemon idleness; it cannot see non-daemon cache consumers.
//
// Spec ref: bead hk-sxlb (logmine F65 disk-watermark guard).
// Fix ref:  bead hk-guez (merge-aware cache reaper).
// Fix ref:  bead hk-gjbpp (removed proactive reap; reactive-only).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workspace"
)

// diskReclaimPort is the periodic disk probe and reactive reclaim dependency
// set. It is a value. cacheReapMu is a pointer so registration and cleanup use
// the same lock.
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
	goCacheCleanFunc          func() error
	worktreeReclaimFunc       func(ctx context.Context, projectDir string, stalePaths []string) error
	cacheReapMu               *sync.RWMutex
}

// newDiskReclaimPort projects the disk path's dependencies once at the
// composition seam. The fresh lock is shared with every run registration for
// this work loop.
func newDiskReclaimPort(projectDir string, bus handlercontract.EventEmitter, runRegistry *RunRegistry) diskReclaimPort {
	return diskReclaimPort{
		projectDir:  projectDir,
		bus:         bus,
		runRegistry: runRegistry,
		cacheReapMu: &sync.RWMutex{},
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

// runGoCleanCache executes `go clean -cache` using port.goCacheCleanFunc when
// set (test seam) or exec.CommandContext otherwise. Returns an error on failure.
func runGoCleanCache(ctx context.Context, port diskReclaimPort) error {
	if port.goCacheCleanFunc != nil {
		return port.goCacheCleanFunc()
	}
	cleanCtx, cleanCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cleanCancel()
	return exec.CommandContext(cleanCtx, "go", "clean", "-cache").Run()
}

// reclaimStaleWorktrees enumerates .harmonik/worktrees/ and removes directories
// whose basename is not a currently-registered run ID. These are stale worktrees
// from crashed or otherwise-unclean runs whose deferred wtCleanup did not fire.
//
// Called in the reactive-reap path (disk below watermark, idle) BEFORE
// go clean -cache: stale worktrees are cheaper to reclaim and do not leave
// subsequent builds with a cold go-build cache (hk-5uezz).
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
			_ = os.RemoveAll(path)
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: git worktree remove %s: %v (%s); fell back to os.RemoveAll\n",
				path, rmErr, strings.TrimSpace(string(out)))
		}
	}
	pruneCmd := exec.CommandContext(reclaimCtx, "git", "-C", port.projectDir, "worktree", "prune")
	return pruneCmd.Run()
}

// runPeriodicDiskCheck is called once per work-loop poll tick to probe disk
// space and run the reactive go-cache cleanup. Rate-limited to
// port.diskCheckIntervalOverride or diskCheckInterval: reads available bytes
// on the project filesystem.
//
//   - Below watermark: sets maint.diskLow = true.
//     If no merge-build is in flight (runRegistry.Len()==0), immediately runs
//     `go clean -cache` (reactive reap). If a merge-build IS in flight, skips
//     the reap and logs a loud warning — this prevents a spurious
//     merge_build_failed at the cost of one deferred clean (hk-guez).
//     A disk_low event is emitted when port.bus is non-nil regardless.
//
//   - Above watermark: clears maint.diskLow. No cache reap happens on this
//     path — see the file-level comment (hk-gjbpp) for why a healthy-disk
//     cadence-based reap was removed rather than restored again.
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

// runDiskProbe performs the disk watermark probe and reactive reap.
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

	// Below watermark: attempt reactive reap, then emit event.
	cleanAttempted := false
	cleanErrStr := ""

	if mergeOrRunInFlight(port) {
		// A merge-build is in progress. Reaping the cache now would
		// race go vet/go build and produce a spurious
		// merge_build_failed. Defer to the next tick and warn loudly
		// — the operator should investigate if disk remains critical
		// across multiple ticks (hk-guez).
		fmt.Fprintf(os.Stderr,
			"daemon: disk-check: WARN available=%dMiB watermark=%dMiB path=%s — "+
				"disk below watermark but merge-build in flight; reap deferred to next tick\n",
			freeBytes/(1024*1024), watermark/(1024*1024), port.projectDir)
	} else {
		// hk-5uezz: try stale-worktree reclaim FIRST — cheaper than
		// wiping the shared go-build cache and avoids leaving the next
		// build with a cold cache. Re-probe after reclaim; if disk is
		// now above the watermark, skip go clean -cache entirely.
		if reclaimedCount := reclaimStaleWorktrees(ctx, port); reclaimedCount > 0 {
			if newFree, reprobeErr := freeBytesFunc(port.projectDir); reprobeErr == nil && newFree >= watermark {
				fmt.Fprintf(os.Stderr,
					"daemon: disk-check: reclaimed %d stale worktree(s) — "+
						"disk recovered available=%dMiB watermark=%dMiB path=%s; skipping go clean -cache\n",
					reclaimedCount, newFree/(1024*1024), watermark/(1024*1024), port.projectDir)
				maint.diskLow = false
				return
			}
		}

		// Stale-worktree reclaim was insufficient. Proceed with the
		// shared go-build cache reap.
		// hk-y3frr: hold the reap↔dispatch exclusive lock for the entire
		// duration of `go clean -cache` so a run registered mid-clean
		// cannot have its build cache deleted (Register holds the RLock;
		// it blocks until we release the WLock below).
		if port.cacheReapMu != nil {
			port.cacheReapMu.Lock()
		}
		// Double-check: a run may have registered between the outer
		// mergeOrRunInFlight check and the WLock acquisition.
		if !mergeOrRunInFlight(port) {
			cleanAttempted = true
			if cleanErr := runGoCleanCache(ctx, port); cleanErr != nil {
				cleanErrStr = cleanErr.Error()
			}
		}
		if port.cacheReapMu != nil {
			port.cacheReapMu.Unlock()
		}
	}

	if port.bus != nil {
		payload := core.DiskLowPayload{
			AvailableBytes:        freeBytes,
			WatermarkBytes:        watermark,
			ProjectPath:           port.projectDir,
			GoCacheCleanAttempted: cleanAttempted,
			GoCacheCleanError:     cleanErrStr,
			DetectedAt:            now.UTC().Format(time.RFC3339),
		}
		if pb, marshalErr := json.Marshal(payload); marshalErr == nil {
			_ = port.bus.Emit(ctx, core.EventTypeDiskLow, pb) //nolint:errcheck // best-effort disk_low event emit
		}
	}
	fmt.Fprintf(os.Stderr,
		"daemon: disk-check: available=%dMiB watermark=%dMiB path=%s — dispatch paused; go_clean_attempted=%v err=%q\n",
		freeBytes/(1024*1024), watermark/(1024*1024), port.projectDir,
		cleanAttempted, cleanErrStr)
	maint.diskLow = true
}
