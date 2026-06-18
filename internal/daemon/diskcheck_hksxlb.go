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
//     When disk is below the watermark, sets deps.diskLow = true, emits a
//     disk_low event, and runs `go clean -cache` reactively. When disk
//     recovers, clears deps.diskLow.
//
//     NOTE: the proactive 60-min go-cache reap (former "Sub-step B") was
//     removed (hk-guez stopgap) — it ran `go clean -cache` unconditionally,
//     racing in-flight merge-builds and causing merge_build_failed. Only the
//     reactive below-watermark reap remains.
//
// Spec ref: bead hk-sxlb (logmine F65 disk-watermark guard).

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

// runPeriodicDiskCheck is called once per work-loop poll tick to probe disk
// space and run reactive go-cache cleanup (hk-sxlb).
//
// Sub-step A — disk probe (rate-limited to deps.diskCheckIntervalOverride or
// diskCheckInterval): reads available bytes on the project filesystem. If below
// the watermark, sets deps.diskLow = true, emits disk_low, and immediately runs
// `go clean -cache` (reactive reap). If above, clears deps.diskLow.
//
// The former proactive 60-min go-cache reap ("Sub-step B") was removed
// (hk-guez stopgap): it ran `go clean -cache` unconditionally and raced
// in-flight merge-builds (merge_build_failed). Only the reactive
// below-watermark reap above remains.
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

		freeBytes, probeErr := diskFreeBytes(deps.projectDir)
		if probeErr != nil {
			// Non-fatal: log and leave diskLow unchanged.
			fmt.Fprintf(os.Stderr, "daemon: disk-check: Statfs %s: %v\n", deps.projectDir, probeErr)
		} else if freeBytes < watermark {
			// Below watermark: reactive reap + event.
			cleanAttempted := false
			cleanErrStr := ""
			if deps.bus != nil {
				// Best-effort go clean -cache. Do NOT block dispatch with a long
				// clean; use a short timeout so the work loop stays responsive.
				cleanCtx, cleanCancel := context.WithTimeout(ctx, 5*time.Minute)
				cleanAttempted = true
				if cleanErr := exec.CommandContext(cleanCtx, "go", "clean", "-cache").Run(); cleanErr != nil {
					cleanErrStr = cleanErr.Error()
				}
				cleanCancel()
				deps.lastGoCacheClean = now // reset both timers on reactive clean

				// Emit disk_low event.
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
}
