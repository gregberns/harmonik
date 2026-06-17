package daemon

// diskcheck_hksxlb.go — periodic disk watermark check and go-cache reap.
//
// Two functions are provided:
//
//   - diskFreeBytes(path) — returns available bytes on the filesystem containing
//     path, using syscall.Statfs. Returns (0, err) on failure.
//
//   - runPeriodicDiskCheck(ctx, deps) — called once per work-loop poll tick.
//     Rate-limited to diskCheckInterval (default 10 min) for the probe and
//     goCacheCleanInterval (default 60 min) for the proactive go-cache reap.
//     When disk is below the watermark, sets deps.diskLow = true, emits a
//     disk_low event, and runs `go clean -cache` reactively. When disk
//     recovers, clears deps.diskLow.
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
// space and run proactive go-cache cleanup (hk-sxlb).
//
// Sub-step A — disk probe (rate-limited to deps.diskCheckIntervalOverride or
// diskCheckInterval): reads available bytes on the project filesystem. If below
// the watermark, sets deps.diskLow = true, emits disk_low, and immediately runs
// `go clean -cache` (reactive reap). If above, clears deps.diskLow.
//
// Sub-step B — proactive go-cache reap (rate-limited to
// deps.goCacheCleanIntervalOverride or goCacheCleanInterval): runs `go clean
// -cache` even when disk is healthy to prevent silent build-cache growth.
func runPeriodicDiskCheck(ctx context.Context, deps *workLoopDeps) {
	now := time.Now()

	checkInterval := deps.diskCheckIntervalOverride
	if checkInterval <= 0 {
		checkInterval = diskCheckInterval
	}
	cleanInterval := deps.goCacheCleanIntervalOverride
	if cleanInterval <= 0 {
		cleanInterval = goCacheCleanInterval
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

	// Sub-step B: proactive go-cache reap (only when disk is not already low,
	// to avoid double-running on the same tick as the reactive path above).
	if !deps.diskLow && time.Since(deps.lastGoCacheClean) >= cleanInterval {
		deps.lastGoCacheClean = now
		cleanCtx, cleanCancel := context.WithTimeout(ctx, 5*time.Minute)
		if cleanErr := exec.CommandContext(cleanCtx, "go", "clean", "-cache").Run(); cleanErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: disk-check: proactive go clean -cache: %v\n", cleanErr)
		}
		cleanCancel()
	}
}
