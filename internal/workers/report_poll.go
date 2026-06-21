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

	"github.com/gregberns/harmonik/internal/core"
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
//
// This is the WR3 (Phase-1) sweep: it always samples + emits a worker_report for
// every due worker. The Phase-2 adaptive sweep is breachSweep below; it reuses
// the same per-worker iteration + skip guards but throttles worker_report and
// also feeds the breach detectors.
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

// RunReportLoop drives the recurring worker-report poll until ctx is cancelled.
//
//   - Off-by-default: if no worker in cfg is ENABLED (or reg is nil) it returns
//     immediately WITHOUT arming a loop — a deployment with no workers.yaml
//     behaves byte-identically to before WR3.
//   - Adaptive cadence (worker-report Phase 2, PB3): the loop ticks at the SLOW
//     interval (cfg.ReportInterval(), default 60s) while every worker is idle,
//     and at the FAST interval (cfg.BreachSampleInterval(), default 5s) while any
//     enabled worker has a run in flight AND breach detection is enabled. When
//     breach detection is OFF (no workers / breach_detection_enabled:false) the
//     loop only ever ticks slow and only emits worker_report — byte-identical to
//     Phase 1.
//   - At the slow cadence each due worker samples + emits a worker_report (the
//     Phase-1 behaviour). At the fast cadence the worker is sampled every tick;
//     the sample feeds that worker's breach detector, but worker_report is still
//     throttled to ~the slow interval so the baseline history is unbroken without
//     a worker_report every 5s.
//   - The loop stops cleanly on ctx.Done().
//
// It is intended to be launched in its own goroutine from daemon start with the
// shutdown context. runnerFor is ProductionRunnerForWorker in production.
func RunReportLoop(ctx context.Context, cfg Config, reg *Registry, runnerFor RunnerForWorker, emit EmitFunc) {
	runReportLoopWithInterval(ctx, cfg, reg, runnerFor, emit, cfg.ReportInterval(), cfg.BreachSampleInterval())
}

// runReportLoopWithInterval is the interval-injectable core of RunReportLoop.
// Production passes cfg.ReportInterval() + cfg.BreachSampleInterval(); tests pass
// sub-second intervals (the workers.yaml fields are whole-seconds, so test
// cadences are injected here, not via config). The off-by-default guard lives
// here so both entry points honour it.
//
// slowInterval is the worker_report cadence (also the cadence when no worker is
// in flight); fastInterval is the cadence used while any worker has a run in
// flight and breach detection is enabled. A fastInterval <= 0 (or >= slow)
// collapses the loop to a single fixed slow cadence.
func runReportLoopWithInterval(ctx context.Context, cfg Config, reg *Registry, runnerFor RunnerForWorker, emit EmitFunc, slowInterval, fastInterval time.Duration) {
	if reg == nil || !hasEnabledWorker(cfg) {
		return
	}
	if slowInterval <= 0 {
		slowInterval = cfg.ReportInterval()
	}
	breachEnabled := cfg.BreachDetectionEnabled()
	if !breachEnabled || fastInterval <= 0 {
		// No adaptive behaviour: collapse to the Phase-1 fixed slow ticker so the
		// off (breach_detection_enabled:false) path is byte-identical to WR3.
		fastInterval = slowInterval
	}

	st := newBreachLoopState(cfg)
	for {
		// Choose the cadence for the NEXT wait: fast while any worker is in flight
		// and breach detection is enabled, slow otherwise. reg.InFlight() is the
		// single global in-flight counter (v1 single-worker registry).
		interval := slowInterval
		if breachEnabled && reg.InFlight() > 0 {
			interval = fastInterval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			breachSweep(ctx, cfg, reg, runnerFor, emit, st, breachEnabled, slowInterval, time.Now())
		}
	}
}

// breachLoopState holds the per-worker breach detectors and bookkeeping that
// must persist across sweeps of the adaptive loop: the stateful state machines,
// the last worker_report emit time (to throttle worker_report to the slow
// cadence while ticking fast), and the previous in-flight state (to detect the
// transition-to-idle that triggers a detector Reset). It is owned by the single
// loop goroutine and is NOT safe for concurrent use.
type breachLoopState struct {
	cfg          Config
	detectors    map[string]*breachDetector
	lastReportAt map[string]time.Time
	wasInFlight  map[string]bool
}

func newBreachLoopState(cfg Config) *breachLoopState {
	return &breachLoopState{
		cfg:          cfg,
		detectors:    map[string]*breachDetector{},
		lastReportAt: map[string]time.Time{},
		wasInFlight:  map[string]bool{},
	}
}

// detectorFor returns the worker's breach detector, creating it lazily on first
// sight (held across sweeps because the state machine is stateful). The detector
// is built from the cfg's Phase-2 knobs via cfg.BreachConfig().
func (s *breachLoopState) detectorFor(name string) *breachDetector {
	d := s.detectors[name]
	if d == nil {
		d = NewBreachDetector(name, s.cfg.BreachConfig())
		s.detectors[name] = d
	}
	return d
}

// breachSweep runs ONE adaptive sweep. For each enabled, live-enabled,
// reachable worker it:
//
//   - samples the box once via CollectReport, but emits the worker_report only
//     when ~slowInterval has elapsed since the worker's last report (so a 5s fast
//     tick does NOT emit a worker_report — only every ~Nth tick does);
//   - when breachEnabled AND the worker has a run in flight, feeds the same
//     sample to the worker's breach detector and emits each returned breach/clear
//     event, stamping the worker's current reg.InFlight() onto each;
//   - on the transition-to-idle (in flight last sweep, idle now) calls the
//     detector's Reset and emits any resulting clear events (InFlight 0), so a
//     breach can't dangle open across runs.
//
// Workers are swept concurrently (one goroutine each) so a slow/failing
// collection cannot stall the others; ALL breachLoopState map mutation happens
// SYNCHRONOUSLY in this function before the per-worker goroutine starts (the
// goroutine touches only the already-resolved *detector), so the shared maps are
// never written from a goroutine and need no lock. A CollectReport error is
// logged and dropped — never fatal.
func breachSweep(ctx context.Context, cfg Config, reg *Registry, runnerFor RunnerForWorker, emit EmitFunc, st *breachLoopState, breachEnabled bool, slowInterval time.Duration, now time.Time) {
	if reg == nil {
		return
	}
	diskFloorMB := cfg.DiskFloorMB // <= 0 ⇒ CollectReport uses DefaultDiskFloorMB

	var wg sync.WaitGroup
	for _, w := range cfg.Workers {
		if !w.Enabled {
			continue
		}
		if !registryWorkerEnabled(reg, w.Name) {
			continue
		}
		runner := runnerFor(w)
		if runner == nil {
			continue
		}

		inFlight := reg.InFlight() > 0

		// Transition-to-idle: a worker that was in flight last sweep but is idle
		// now gets its detector Reset, emitting a clear for any dangling breach.
		// This runs even though we won't feed a sample this sweep below. Detector
		// + state mutation here is for THIS worker's keys only.
		if breachEnabled && st.wasInFlight[w.Name] && !inFlight {
			if d := st.detectors[w.Name]; d != nil {
				for _, ev := range d.Reset(now) {
					ev.InFlight = 0
					emitResourceBreach(ctx, ev, emit)
				}
			}
		}
		st.wasInFlight[w.Name] = inFlight

		// worker_report throttle: emit only when ~slowInterval has elapsed since
		// this worker's last report. The zero-value last-report time (first sight)
		// is always due. We still SAMPLE every sweep (for breach feeding); we just
		// suppress the worker_report emit between slow boundaries.
		last := st.lastReportAt[w.Name]
		dueForReport := last.IsZero() || now.Sub(last) >= slowInterval
		if dueForReport {
			st.lastReportAt[w.Name] = now
		}

		// Resolve the worker's detector SYNCHRONOUSLY (mutating st.detectors) so
		// the per-worker goroutine touches only the already-resolved *detector —
		// the shared maps are never written from a goroutine, keeping the sweep
		// race-free under -race.
		var det *breachDetector
		if breachEnabled && inFlight {
			det = st.detectorFor(w.Name)
		}

		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Suppress the worker_report emit when not due by passing a nil emit
			// to CollectReport; we still need the parsed sample for breach feeding.
			reportEmit := emit
			if !dueForReport {
				reportEmit = nil
			}
			rep, err := CollectReport(ctx, runner, w, reg, diskFloorMB, reportEmit)
			if err != nil {
				fmt.Fprintf(os.Stderr, "workers: report poll: %s: %v\n", w.Name, err)
				return
			}
			if det != nil {
				for _, ev := range det.Observe(rep, now) {
					ev.InFlight = reg.InFlight()
					emitResourceBreach(ctx, ev, emit)
				}
			}
		}()
	}
	wg.Wait()
}

// emitResourceBreach marshals and emits a resource_breach event (PB3). No-op
// when emit is nil (mirrors emitWorkerReport in telemetry.go). A marshal failure
// is dropped silently — a breach event is observability, never fatal. InFlight is
// expected to be stamped by the caller before this is called.
func emitResourceBreach(ctx context.Context, p ResourceBreachPayload, emit EmitFunc) {
	if emit == nil {
		return
	}
	b, err := marshalResourceBreach(p)
	if err != nil {
		return
	}
	_ = emit(ctx, core.EventTypeResourceBreach, b)
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
