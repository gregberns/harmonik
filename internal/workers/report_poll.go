package workers

import (
	"context"
	"fmt"
	"log/slog"
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
		if !registryWorkerEnabled(reg, w.Name) {
			continue
		}
		runner := runnerFor(w)
		if runner == nil {
			continue
		}
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

func reportLoopInterval(breachEnabled bool, inFlight int, slowInterval, fastInterval time.Duration) time.Duration {
	if breachEnabled && inFlight > 0 {
		return fastInterval
	}
	return slowInterval
}

func runReportLoopWithInterval(ctx context.Context, cfg Config, reg *Registry, runnerFor RunnerForWorker, emit EmitFunc, slowInterval, fastInterval time.Duration) {
	if reg == nil || !hasEnabledWorker(cfg) {
		return
	}
	if slowInterval <= 0 {
		slowInterval = cfg.ReportInterval()
	}
	breachEnabled := cfg.BreachDetectionEnabled()
	if !breachEnabled || fastInterval <= 0 {
		fastInterval = slowInterval
	}

	st := newBreachLoopState(cfg)
	for {
		interval := reportLoopInterval(breachEnabled, reg.InFlight(), slowInterval, fastInterval)
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			breachSweep(ctx, cfg, reg, runnerFor, emit, st, breachEnabled, slowInterval, time.Now())
		}
	}
}

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

func (s *breachLoopState) detectorFor(name string) *breachDetector {
	d := s.detectors[name]
	if d == nil {
		d = NewBreachDetector(name, s.cfg.BreachConfig())
		s.detectors[name] = d
	}
	return d
}

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

		if breachEnabled && st.wasInFlight[w.Name] && !inFlight {
			if d := st.detectors[w.Name]; d != nil {
				for _, ev := range d.Reset(now) {
					ev.InFlight = 0
					emitResourceBreach(ctx, ev, emit)
				}
			}
		}
		st.wasInFlight[w.Name] = inFlight

		last := st.lastReportAt[w.Name]
		dueForReport := last.IsZero() || now.Sub(last) >= slowInterval
		if dueForReport {
			st.lastReportAt[w.Name] = now
		}

		var det *breachDetector
		if breachEnabled && inFlight {
			det = st.detectorFor(w.Name)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
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

func emitResourceBreach(ctx context.Context, p ResourceBreachPayload, emit EmitFunc) {
	if emit == nil {
		return
	}
	b, err := marshalResourceBreach(p)
	if err != nil {
		return
	}
	if err := emit(ctx, core.EventTypeResourceBreach, b); err != nil {
		slog.ErrorContext(ctx, "worker event emit failed", "event_type", core.EventTypeResourceBreach, "error", err)
	}
}

func hasEnabledWorker(cfg Config) bool {
	for _, w := range cfg.Workers {
		if w.Enabled {
			return true
		}
	}
	return false
}

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
