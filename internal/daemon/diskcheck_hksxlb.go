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
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workspace"
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

// goCleanOwnCacheEnv strips GOCACHE from an inherited environment so that a
// command which then names its OWN cache explicitly cannot be overridden by
// whatever the parent process happened to export.
//
// Used by the relocation call sites below (the per-agent cache reclaim and the
// merge gate's build env): both append an explicit GOCACHE after this strip, so
// the target is a property of the COMMAND rather than of whoever launched the
// daemon. Stripping first rather than relying on Go's last-entry-wins semantics
// keeps that guarantee independent of env ordering.
//
// NOT applied to runGoCleanCache above. That is hk-agl8b, which is HELD:
// stripping GOCACHE there makes the reap target the default cache, which does
// not eliminate the mid-build wipe (default-cache crews are still exposed, and
// that default is the macOS-purgeable path — hk-pgtbr). It also narrows what
// the reaper may reclaim without giving it a replacement source of disk, which
// risks converting a cache-corruption bug into a silently paused fleet. india
// owns the correct fix; do not add the strip there without one.
func goCleanOwnCacheEnv(parent []string) []string {
	out := make([]string, 0, len(parent))
	for _, kv := range parent {
		if strings.HasPrefix(kv, "GOCACHE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
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
func reclaimStaleWorktrees(ctx context.Context, deps *workLoopDeps) int {
	if deps.runRegistry == nil {
		return 0
	}
	worktreesDir := filepath.Join(deps.projectDir, workspace.DefaultWorktreeRoot)
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
		if _, registered := deps.runRegistry.Get(core.RunID(uid)); registered {
			continue // in-flight — never remove
		}
		stalePaths = append(stalePaths, filepath.Join(worktreesDir, e.Name()))
	}
	if len(stalePaths) == 0 {
		return 0
	}

	if reclaimErr := runWorktreeReclaim(ctx, deps, stalePaths); reclaimErr != nil {
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
// prunes the git worktree list. Uses deps.worktreeReclaimFunc as a test seam
// when non-nil; otherwise runs the production git subprocess sequence.
func runWorktreeReclaim(ctx context.Context, deps *workLoopDeps, stalePaths []string) error {
	if deps.worktreeReclaimFunc != nil {
		return deps.worktreeReclaimFunc(ctx, deps.projectDir, stalePaths)
	}
	reclaimCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for _, path := range stalePaths {
		rmCmd := exec.CommandContext(reclaimCtx, "git", "-C", deps.projectDir, "worktree", "remove", "--force", "--force", path)
		if out, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
			// Fallback: os.RemoveAll for "not a working tree" and similar git errors.
			_ = os.RemoveAll(path)
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: git worktree remove %s: %v (%s); fell back to os.RemoveAll\n",
				path, rmErr, strings.TrimSpace(string(out)))
		}
	}
	pruneCmd := exec.CommandContext(reclaimCtx, "git", "-C", deps.projectDir, "worktree", "prune")
	return pruneCmd.Run()
}

// runPeriodicDiskCheck is called once per work-loop poll tick to probe disk
// space and run the reactive go-cache cleanup. Rate-limited to
// deps.diskCheckIntervalOverride or diskCheckInterval: reads available bytes
// on the project filesystem.
//
//   - Below watermark: sets maint.diskLow = true.
//     If no merge-build is in flight (runRegistry.Len()==0), immediately runs
//     `go clean -cache` (reactive reap). If a merge-build IS in flight, skips
//     the reap and logs a loud warning — this prevents a spurious
//     merge_build_failed at the cost of one deferred clean (hk-guez).
//     A disk_low event is emitted when deps.bus is non-nil regardless.
//
//   - Above watermark: clears maint.diskLow. No cache reap happens on this
//     path — see the file-level comment (hk-gjbpp) for why a healthy-disk
//     cadence-based reap was removed rather than restored again.
func runPeriodicDiskCheck(ctx context.Context, deps *workLoopDeps, maint *loopMaintenanceState) {
	now := time.Now()

	checkInterval := deps.diskCheckIntervalOverride
	if checkInterval <= 0 {
		checkInterval = diskCheckInterval
	}
	watermark := deps.diskLowWatermark
	if watermark == 0 {
		watermark = diskLowWatermarkDefault
	}

	if time.Since(maint.lastDiskCheck) >= checkInterval {
		maint.lastDiskCheck = now
		runDiskProbe(ctx, deps, maint, now, watermark)
	}
}

// runDiskProbe performs the disk watermark probe and reactive reap.
func runDiskProbe(ctx context.Context, deps *workLoopDeps, maint *loopMaintenanceState, now time.Time, watermark uint64) {
	freeBytesFunc := deps.diskFreeBytesFunc
	if freeBytesFunc == nil {
		freeBytesFunc = diskFreeBytes
	}

	freeBytes, probeErr := freeBytesFunc(deps.projectDir)
	if probeErr != nil {
		// Non-fatal: log and leave diskLow unchanged.
		fmt.Fprintf(os.Stderr, "daemon: disk-check: Statfs %s: %v\n", deps.projectDir, probeErr)
		return
	}
	if freeBytes >= watermark {
		if maint.diskLow {
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: recovered — available=%dMiB watermark=%dMiB path=%s — dispatch resumed\n",
				freeBytes/(1024*1024), watermark/(1024*1024), deps.projectDir)
		}
		maint.diskLow = false
		return
	}

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
		var recovered bool
		recovered, cleanAttempted, cleanErrStr = runReactiveReclaim(ctx, deps, freeBytesFunc, watermark)
		if recovered {
			maint.diskLow = false
			return
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
			_ = deps.bus.Emit(ctx, core.EventTypeDiskLow, pb) //nolint:errcheck // best-effort disk_low event emit
		}
	}
	fmt.Fprintf(os.Stderr,
		"daemon: disk-check: available=%dMiB watermark=%dMiB path=%s — dispatch paused; go_clean_attempted=%v err=%q\n",
		freeBytes/(1024*1024), watermark/(1024*1024), deps.projectDir,
		cleanAttempted, cleanErrStr)
	maint.diskLow = true
}

// runReactiveReclaim is the below-watermark, nothing-in-flight reclaim ladder:
// stale worktrees first, then the shared go-build cache, then the per-agent
// caches. Split out of runDiskProbe purely to keep that function readable once
// hk-137y6 added the third rung; the order and the locking are unchanged.
//
// Returns (recovered, cleanAttempted, cleanErr):
//   - recovered is true ONLY when the cheap stale-worktree reclaim alone put
//     disk back above the watermark, in which case the caller clears diskLow and
//     skips the cache reap entirely (hk-5uezz).
//   - cleanAttempted / cleanErr feed the disk_low event payload.
//
// EVERY rung here is reactive-path-only by construction: this function is
// called from exactly one place, inside the below-watermark branch. That is
// load-bearing, not incidental — reaping a cache that other processes are
// building against is the corruption hk-gjbpp removed the healthy-disk cadence
// to stop, and only genuine disk pressure with nothing in flight justifies it.
// Guarded by TestDiskCheck_HealthyDisk_LeavesAgentCachesAlone.
func runReactiveReclaim(
	ctx context.Context,
	deps *workLoopDeps,
	freeBytesFunc func(string) (uint64, error),
	watermark uint64,
) (recovered, cleanAttempted bool, cleanErrStr string) {
	// hk-5uezz: try stale-worktree reclaim FIRST — cheaper than wiping the
	// shared go-build cache and avoids leaving the next build with a cold
	// cache. Re-probe after reclaim; if disk is now above the watermark, skip
	// go clean -cache entirely.
	if reclaimedCount := reclaimStaleWorktrees(ctx, deps); reclaimedCount > 0 {
		if newFree, reprobeErr := freeBytesFunc(deps.projectDir); reprobeErr == nil && newFree >= watermark {
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: reclaimed %d stale worktree(s) — "+
					"disk recovered available=%dMiB watermark=%dMiB path=%s; skipping go clean -cache\n",
				reclaimedCount, newFree/(1024*1024), watermark/(1024*1024), deps.projectDir)
			return true, false, ""
		}
	}

	// Stale-worktree reclaim was insufficient. Proceed with the shared
	// go-build cache reap.
	// hk-y3frr: hold the reap↔dispatch exclusive lock for the entire duration
	// of `go clean -cache` so a run registered mid-clean cannot have its build
	// cache deleted (Register holds the RLock; it blocks until we release the
	// WLock below).
	if deps.cacheReapMu != nil {
		deps.cacheReapMu.Lock()
		defer deps.cacheReapMu.Unlock()
	}
	// Double-check: a run may have registered between the outer
	// mergeOrRunInFlight check and the WLock acquisition.
	if mergeOrRunInFlight(deps) {
		return false, false, ""
	}
	cleanAttempted = true
	if cleanErr := runGoCleanCache(ctx, deps); cleanErr != nil {
		cleanErrStr = cleanErr.Error()
	}
	// hk-137y6: the per-agent caches are the relief valve now. Once the fleet's
	// builds moved OFF Go's default cache, `go clean -cache` above reclaims
	// progressively less — without this the daemon could sit below the
	// watermark with dispatch paused and nothing left to free.
	if reaped := reapAgentGoCaches(ctx, deps); reaped > 0 {
		fmt.Fprintf(os.Stderr,
			"daemon: disk-check: reclaimed %d per-agent go-build cache(s) under disk pressure (hk-137y6); "+
				"affected agents will rebuild from a cold cache\n", reaped)
	}
	return false, cleanAttempted, cleanErrStr
}

// reapAgentGoCaches reclaims the per-agent Go build caches under
// .harmonik/go-cache (hk-137y6) and returns how many were reclaimed.
//
// Called ONLY from the reactive disk-low path, never from the proactive timer:
// these caches belong to other agents, and wiping one mid-`go test` is exactly
// the mid-verification corruption hk-gjbpp was filed to stop. Genuine disk
// pressure with nothing in flight justifies it; a healthy-disk cadence does not.
//
// A cache is reaped ONLY when it can be shown QUIESCENT (see
// goCacheQuiescenceWindow). A reclaim that cannot tell a running cache from a
// stale one MUST NOT delete either — on 2026-07-22 something ran `go clean
// -cache` against a crew's private cache mid-experiment (483M -> 8.0K) and its
// build then failed with "could not import os/exec" naming that crew's OWN
// path. That did not merely cost a rebuild: the cache-miss errors nearly
// entered a flaky-test dataset as real failures. A contaminated measurement
// that looks like a result is worse than no measurement.
//
// Uses `go clean -cache` per directory rather than a bare rm -rf so the Go
// toolchain removes its own cache on its own terms; a directory that fails is
// skipped rather than force-removed.
func reapAgentGoCaches(ctx context.Context, deps *workLoopDeps) int {
	if deps == nil || deps.projectDir == "" {
		return 0
	}
	root := goCacheRootDir(deps.projectDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0
	}
	reaped := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if !goCacheQuiescent(dir, time.Now()) {
			// In use, or too recently used to be sure. Skipping is loud on
			// purpose: an operator staring at a paused-for-disk daemon needs to
			// know reclaim found candidates and declined them, otherwise the
			// silence reads as "there was nothing to free".
			fmt.Fprintf(os.Stderr,
				"daemon: disk-check: SKIPPING go-cache reap for %q — active within %s; "+
					"a reclaim that cannot prove a cache is stale must not delete it (hk-137y6)\n",
				e.Name(), goCacheQuiescenceWindow)
			continue
		}
		cleanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		cmd := exec.CommandContext(cleanCtx, "go", "clean", "-cache")
		// Strip any inherited GOCACHE before naming our own, so the target is
		// unambiguous rather than resting on Go's last-entry-wins env semantics
		// (hk-agl8b: an inherited GOCACHE is how the suite ate a crew's cache).
		cmd.Env = append(goCleanOwnCacheEnv(os.Environ()), "GOCACHE="+dir)
		runErr := cmd.Run()
		cancel()
		if runErr == nil {
			reaped++
		}
	}
	return reaped
}

// goCacheQuiescenceWindow is how long a per-agent cache must have gone untouched
// before the disk-pressure reclaim may delete it.
//
// Deliberately far longer than a build: a full `go test ./...` on this repo runs
// in minutes, so 30 minutes of no writes means no build is using it. The cost of
// being wrong is asymmetric and that is why the window is generous — reaping too
// eagerly destroys a live working set and silently corrupts whatever measurement
// it was feeding, while reaping too little only means we free less disk on this
// tick and try again on the next.
const goCacheQuiescenceWindow = 30 * time.Minute

// goCacheQuiescent reports whether the Go build cache at dir has gone untouched
// for at least goCacheQuiescenceWindow — i.e. whether it is provably NOT in use.
//
// Go writes into 256 hex-named shard subdirectories as it builds, so a live
// cache always has a recent mtime somewhere in its top level. Checking the
// directory plus its immediate children is enough and costs a couple of hundred
// stats.
//
// FAILS CLOSED: any error reading the directory returns false (treat as in use).
// The one thing this function must never do is report "safe to delete" about a
// cache it could not inspect.
func goCacheQuiescent(dir string, now time.Time) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	newest := info.ModTime()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		ei, infoErr := e.Info()
		if infoErr != nil {
			return false // cannot inspect it -> cannot claim it is stale
		}
		if ei.ModTime().After(newest) {
			newest = ei.ModTime()
		}
	}
	return now.Sub(newest) >= goCacheQuiescenceWindow
}
