package workers

// report_poll.go — recurring worker-report poll (remote-substrate WR3).
//
// WR1/WR2/WR4 built the worker-report payload, the SSH collector (CollectReport),
// and the problem-flag derivation. They are driven only at boot today (the boot
// health check in the daemon runs once). WR3 adds a recurring ticker that calls
// CollectReport on an interval so worker resource + problem reports actually flow
// during operation.
//
// This is Phase-1 OBSERVABILITY ONLY: it emits worker_report events on a timer.
// It does NOT touch SelectWorker, max_slots, or dispatch. The poll runs in its
// own goroutine and a slow/failing CollectReport is logged-and-dropped, never
// fatal, so it cannot wedge the daemon.
//
// Off-by-default: when no worker is ENABLED the loop returns immediately without
// arming a ticker, exactly like the boot health check skips an empty registry.
// Disabled workers are skipped on every tick.
//
// Bead ref: WR3 (hk-jn3u).

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// RunnerForWorker resolves the CommandRunner used to reach a worker. Production
// passes reportRunnerForWorker (SSHRunner for transport "ssh", nil otherwise);
// tests inject a fake so the poll is exercisable without real ssh. A nil return
// causes the worker to be skipped for that tick (unsupported transport).
type RunnerForWorker func(w Worker) tmux.CommandRunner

// reportRunnerForWorker is the production RunnerForWorker: an SSHRunner for
// transport "ssh", nil for any other transport. It mirrors bootHealthRunner in
// the daemon package so the recurring poll reaches a worker exactly as the boot
// health check does.
func reportRunnerForWorker(w Worker) tmux.CommandRunner {
	if w.Transport == "ssh" {
		return tmux.SSHRunner{Host: w.Host}
	}
	return nil
}

// ProductionRunnerForWorker is the exported production runner resolver so the
// daemon can wire the recurring poll without re-deriving the SSH transport rule.
func ProductionRunnerForWorker(w Worker) tmux.CommandRunner {
	return reportRunnerForWorker(w)

}

// pollWorkerReports runs ONE sweep: for each enabled worker in cfg it resolves a
// runner via runnerFor and calls CollectReport, which emits a worker_report event
// (and derives Problems) on success. Each worker's collection runs in its own
// goroutine so a slow or failing worker cannot stall the others or the caller;
// the sweep returns once all per-worker goroutines have settled.
//
// reg supplies live-disable state (the boot health check disables a probe-failed
// worker in the registry, not in cfg) and in-flight counts for the orphaned_claude
// derivation. reg may be nil (no worker configured) in which case the sweep is a
// no-op.
//
// A CollectReport error is logged to stderr and dropped — never returned, never
// fatal. This function never blocks on the work loop and never touches dispatch.
func pollWorkerReports(ctx context.Context, cfg Config, reg *Registry, runnerFor RunnerForWorker, emit EmitFunc) {
	if reg == nil {
		return
	}
	diskFloorMB := cfg.DiskFloorMB // <= 0 ⇒ CollectReport uses DefaultDiskFloorMB

	var wg sync.WaitGroup
	for _, w := range cfg.Workers {
		if !w.Enabled {
			continue
		}
		// Honour boot-time live-disable: the registry, not cfg, holds the
		// post-health-check enabled state. registryWorkerEnabled returns false
		// when the boot health check disabled the (single, v1) worker.
		if !registryWorkerEnabled(reg, w.Name) {
			continue
		}
		runner := runnerFor(w)
		if runner == nil {
			// Unsupported transport — skip silently (matches bootHealthRunner).
			continue
		}
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := CollectReport(ctx, runner, w, reg, diskFloorMB, emit); err != nil {
				fmt.Fprintf(os.Stderr, "workers: report poll: %s: %v\n", w.Name, err)
			}
		}()
	}
	wg.Wait()
}

// RunReportLoop drives pollWorkerReports on a ticker until ctx is cancelled.
//
//   - Off-by-default: if no worker in cfg is ENABLED (or reg is nil) it returns
//     immediately WITHOUT arming a ticker — a deployment with no workers.yaml
//     behaves byte-identically to before WR3.
//   - The cadence is cfg.ReportInterval() (workers.yaml report_interval_seconds,
//     default 60s).
//   - Each tick runs pollWorkerReports (which fans out per-worker goroutines), so
//     a slow/failing collection cannot wedge the loop or the daemon.
//   - The loop stops cleanly on ctx.Done().
//
// It is intended to be launched in its own goroutine from daemon start with the
// shutdown context. runnerFor is ProductionRunnerForWorker in production.
func RunReportLoop(ctx context.Context, cfg Config, reg *Registry, runnerFor RunnerForWorker, emit EmitFunc) {
	runReportLoopWithInterval(ctx, cfg, reg, runnerFor, emit, cfg.ReportInterval())
}

// runReportLoopWithInterval is the interval-injectable core of RunReportLoop.
// Production passes cfg.ReportInterval(); tests pass a sub-second interval (the
// workers.yaml field is whole-seconds, so a test cadence is injected here, not
// via config). The off-by-default guard lives here so both entry points honour
// it.
func runReportLoopWithInterval(ctx context.Context, cfg Config, reg *Registry, runnerFor RunnerForWorker, emit EmitFunc, interval time.Duration) {
	if reg == nil || !hasEnabledWorker(cfg) {
		return
	}
	if interval <= 0 {
		interval = cfg.ReportInterval()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollWorkerReports(ctx, cfg, reg, runnerFor, emit)
		}
	}
}

// hasEnabledWorker reports whether cfg has at least one worker with Enabled==true.
// Mirrors the daemon-side guard so the off-by-default decision lives next to the
// loop it gates.
func hasEnabledWorker(cfg Config) bool {
	for _, w := range cfg.Workers {
		if w.Enabled {
			return true
		}
	}
	return false
}

// registryWorkerEnabled reports whether the registry's worker named name is still
// enabled (i.e. the boot health check did not disable it). The v1 registry holds
// at most one worker; when reg has no worker, or the name does not match, it
// returns false so the poll skips it.
func registryWorkerEnabled(reg *Registry, name string) bool {
	if reg == nil {
		return false
	}
	w := reg.WorkerSnapshot()
	if w == nil {
		return false
	}
	return w.Name == name && w.Enabled
}
