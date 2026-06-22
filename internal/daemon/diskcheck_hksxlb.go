package daemon

// diskcheck_hksxlb.go — periodic disk watermark check and go-cache reap.
//
// Two functions are provided:
//
//   - diskFreeBytes(path) — returns available bytes on the filesystem containing
//     path, using syscall.Statfs. Returns (0, err) on failure.
//
//   - runPeriodicDiskCheck(ctx, deps) — called once per work-loop poll tick.
//     Rate-limited to diskCheckInterval (default 10 min) for the probe.
//
//     Sub-step A — reactive reap: when disk is below the watermark, sets
//     deps.diskLow = true, emits a disk_low event, and runs `go clean -cache`
//     only when no merge-build is in flight (runRegistry.Len()==0). If a
//     merge-build IS in flight, the reap is deferred to the next tick and a
//     warning is logged instead of corrupting the build (hk-guez fix for the
//     stopgap in 5c2276ca).
//
//     Sub-step B — proactive reap (restored by hk-guez): when disk is healthy,
//     runs `go clean -cache` every goCacheCleanInterval (default 60 min) to
//     prevent the cache from growing to 20 GiB between low-disk crossings.
//     Also gated on idle (runRegistry.Len()==0) to avoid racing merge-builds.
//
// Spec ref: bead hk-sxlb (logmine F65 disk-watermark guard).
// Fix ref:  bead hk-guez (merge-aware cache reaper).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

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
// registered in deps.runRegistry (i.e. any merge-build or run-build is active).
// Non-blocking: reads an atomic counter inside RunRegistry (ActiveRuns count check).
func mergeOrRunInFlight(deps *workLoopDeps) bool {
	return deps.runRegistry != nil && deps.runRegistry.Len() > 0
}

// runGoCleanCache executes `go clean -cache` using deps.goCacheCleanFunc when
// set (test seam) or exec.CommandContext otherwise. Returns an error on failure.
func runGoCleanCache(ctx context.Context, deps *workLoopDeps) error {
	if deps.goCacheCleanFunc != nil {
		return deps.goCacheCleanFunc()
	}
	cleanCtx, cleanCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cleanCancel()
	return exec.CommandContext(cleanCtx, "go", "clean", "-cache").Run()
}

// runPeriodicDiskCheck is called once per work-loop poll tick to probe disk
// space and run reactive / proactive go-cache cleanup.
//
// Sub-step A — disk probe (rate-limited to deps.diskCheckIntervalOverride or
// diskCheckInterval): reads available bytes on the project filesystem.
//
//   - Below watermark: sets deps.diskLow = true.
//     If no merge-build is in flight (runRegistry.Len()==0), immediately runs
//     `go clean -cache` (reactive reap). If a merge-build IS in flight, skips
//     the reap and logs a loud warning — this prevents a spurious
//     merge_build_failed at the cost of one deferred clean (hk-guez).
//     A disk_low event is emitted when deps.bus is non-nil regardless.
//
//   - Above watermark: clears deps.diskLow.
//
// Sub-step B — proactive reap (hk-guez, restored from stopgap 5c2276ca):
// runs `go clean -cache` every goCacheCleanInterval (default 60 min) even
// when disk is healthy, gated on idle (runRegistry.Len()==0).
func runPeriodicDiskCheck(ctx context.Context, deps *workLoopDeps) {
	now := time.Now()

	checkInterval := deps.diskCheckIntervalOverride
	if checkInterval <= 0 {
		checkInterval = diskCheckInterval
	}
	watermark := deps.diskLowWatermark
	if watermark == 0 {
		watermark = diskLowWatermarkDefault
	}

	// Sub-step A: disk probe.
	if time.Since(deps.lastDiskCheck) >= checkInterval {
		deps.lastDiskCheck = now

		freeBytesFunc := deps.diskFreeBytesFunc
		if freeBytesFunc == nil {
			freeBytesFunc = diskFreeBytes
		}

		freeBytes, probeErr := freeBytesFunc(deps.projectDir)
		if probeErr != nil {
			// Non-fatal: log and leave diskLow unchanged.
			fmt.Fprintf(os.Stderr, "daemon: disk-check: Statfs %s: %v\n", deps.projectDir, probeErr)
		} else if freeBytes < watermark {
			// Below watermark: attempt reactive reap, then emit event.
			cleanAttempted := false
			cleanErrStr := ""

			if mergeOrRunInFlight(deps) {
				// A merge-build is in progress. Reaping the cache now would
				// race go vet/go build and produce a spurious
				// merge_build_failed. Defer to the next tick and warn loudly
				// — the operator should investigate if disk remains critical
				// across multiple ticks (hk-guez).
				fmt.Fprintf(os.Stderr,
					"daemon: disk-check: WARN available=%dMiB watermark=%dMiB path=%s — "+
						"disk below watermark but merge-build in flight; reap deferred to next tick\n",
					freeBytes/(1024*1024), watermark/(1024*1024), deps.projectDir)
			} else {
				// hk-y3frr: hold the reap↔dispatch exclusive lock for the entire
				// duration of `go clean -cache` so a run registered mid-clean
				// cannot have its build cache deleted (Register holds the RLock;
				// it blocks until we release the WLock below).
				if deps.cacheReapMu != nil {
					deps.cacheReapMu.Lock()
				}
				// Double-check: a run may have registered between the outer
				// mergeOrRunInFlight check and the WLock acquisition.
				if !mergeOrRunInFlight(deps) {
					cleanAttempted = true
					if cleanErr := runGoCleanCache(ctx, deps); cleanErr != nil {
						cleanErrStr = cleanErr.Error()
					}
					deps.lastGoCacheClean = now // reset proactive timer on reactive clean
				}
				if deps.cacheReapMu != nil {
					deps.cacheReapMu.Unlock()
				}
			}

			if deps.bus != nil {
				payload := core.DiskLowPayload{
					AvailableBytes:        freeBytes,
					WatermarkBytes:        watermark,
					ProjectPath:           deps.projectDir,
					GoCacheCleanAttempted: cleanAttempted,
					GoCacheCleanError:     cleanErrStr,
					DetectedAt:            now.UTC().Format(time.RFC3339),
				}
				if pb, marshalErr := json.Marshal(payload); marshalErr == nil {
					_ = deps.bus.Emit(ctx, core.EventTypeDiskLow, pb)
				}
			}
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: available=%dMiB watermark=%dMiB path=%s — dispatch paused; go_clean_attempted=%v err=%q\n",
				freeBytes/(1024*1024), watermark/(1024*1024), deps.projectDir,
				cleanAttempted, cleanErrStr)
			deps.diskLow = true
		} else {
			if deps.diskLow {
				fmt.Fprintf(os.Stderr,
					"daemon: disk-check: recovered — available=%dMiB watermark=%dMiB path=%s — dispatch resumed\n",
					freeBytes/(1024*1024), watermark/(1024*1024), deps.projectDir)
			}
			deps.diskLow = false
		}
	}

	// Sub-step B: proactive reap (hk-guez restored; TOCTOU fixed by hk-y3frr).
	// Runs `go clean -cache` every goCacheCleanInterval (default 60 min) even
	// when disk is healthy, gated on idle (runRegistry.Len()==0) and protected
	// by cacheReapMu WLock held for the entire clean duration.
	cleanInterval := deps.goCacheCleanIntervalOverride
	if cleanInterval <= 0 {
		cleanInterval = goCacheCleanInterval
	}
	if !deps.diskLow && time.Since(deps.lastGoCacheClean) >= cleanInterval {
		if mergeOrRunInFlight(deps) {
			// Merge-build in flight: defer proactive reap to avoid racing the
			// build cache. The timer is NOT reset so the next idle tick will
			// fire immediately (no silent skip of the 60-min cadence).
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: proactive-reap deferred — merge-build in flight\n")
		} else {
			// hk-y3frr: hold the reap↔dispatch exclusive lock for the entire
			// duration of `go clean -cache`.  Register (RLock) blocks while we
			// hold the WLock; we release only after the clean completes.
			if deps.cacheReapMu != nil {
				deps.cacheReapMu.Lock()
			}
			// Double-check after acquiring the lock: a run may have registered
			// between the outer mergeOrRunInFlight check and the WLock.
			if !mergeOrRunInFlight(deps) {
				if cleanErr := runGoCleanCache(ctx, deps); cleanErr != nil {
					fmt.Fprintf(os.Stderr,
						"daemon: disk-check: proactive go clean -cache failed: %v\n", cleanErr)
				}
				deps.lastGoCacheClean = now
			}
			if deps.cacheReapMu != nil {
				deps.cacheReapMu.Unlock()
			}
		}
	}
}
