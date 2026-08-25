package daemon

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

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/workspace"
)

type diskReclaimPort struct {
	projectDir                string
	bus                       handlercontract.EventEmitter
	runRegistry               *runregistry.RunRegistry
	diskCheckIntervalOverride time.Duration
	diskFreeBytesFunc         func(path string) (uint64, error)
	worktreeReclaimFunc       func(ctx context.Context, projectDir string, stalePaths []string) error
}

func newDiskReclaimPort(projectDir string, bus handlercontract.EventEmitter, runRegistry *runregistry.RunRegistry) diskReclaimPort {
	return diskReclaimPort{
		projectDir:  projectDir,
		bus:         bus,
		runRegistry: runRegistry,
	}
}

func diskFreeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

func mergeOrRunInFlight(port diskReclaimPort) bool {
	return port.runRegistry != nil && port.runRegistry.Len() > 0
}

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
	removed := 0
	for _, p := range stalePaths {
		if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
			removed++
		}
	}
	return removed
}

func runWorktreeReclaim(ctx context.Context, port diskReclaimPort, stalePaths []string) error {
	if port.worktreeReclaimFunc != nil {
		return port.worktreeReclaimFunc(ctx, port.projectDir, stalePaths)
	}
	reclaimCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, path := range stalePaths {
		rmCmd := exec.CommandContext(reclaimCtx, "git", "-C", port.projectDir, "worktree", "remove", "--force", "--force", path)
		if out, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
			fallbackErr := os.RemoveAll(path)
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: git worktree remove %s: %v (%s); fell back to os.RemoveAll (err=%v)\n",
				path, rmErr, strings.TrimSpace(string(out)), fallbackErr)
		}
	}
	pruneCmd := exec.CommandContext(reclaimCtx, "git", "-C", port.projectDir, "worktree", "prune")
	return pruneCmd.Run()
}

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

func runDiskProbe(ctx context.Context, port diskReclaimPort, maint *loopMaintenanceState, now time.Time, watermark uint64) {
	freeBytesFunc := port.diskFreeBytesFunc
	if freeBytesFunc == nil {
		freeBytesFunc = diskFreeBytes
	}

	freeBytes, probeErr := freeBytesFunc(port.projectDir)
	if probeErr != nil {
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

	if mergeOrRunInFlight(port) {
		fmt.Fprintf(os.Stderr,
			"daemon: disk-check: WARN available=%dMiB watermark=%dMiB path=%s — "+
				"disk below watermark but a run is in flight; worktree reclaim deferred to next tick\n",
			freeBytes/(1024*1024), watermark/(1024*1024), port.projectDir)
	} else if reclaimedCount := reclaimStaleWorktrees(ctx, port); reclaimedCount > 0 {
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
