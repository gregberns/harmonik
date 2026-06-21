package workers

// report_poll_test.go — unit tests for the recurring worker-report poll (WR3).
//
// Intra-package (package workers) so the unexported pollWorkerReports sweep can
// be exercised directly alongside the exported RunReportLoop ticker. The fake
// runner / capture-emit helpers are shared with telemetry_test.go.
//
// Bead ref: WR3 (hk-jn3u).

import (
	"context"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// countingRunner is a fake CommandRunner that counts how many times Command is
// invoked (i.e. how many CollectReport sweeps ran) while returning the canned
// collector stdout so parsing + emit succeed. Safe for concurrent use.
type countingRunner struct {
	calls *int64
}

func (r countingRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	atomic.AddInt64(r.calls, 1)
	return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$0"`, cannedCollectorStdout)
}

var _ tmux.CommandRunner = countingRunner{}

// fixedRunnerFor returns a RunnerForWorker that always yields runner, regardless
// of the worker's transport (so tests need not configure ssh plumbing).
func fixedRunnerFor(runner tmux.CommandRunner) RunnerForWorker {
	return func(w Worker) tmux.CommandRunner { return runner }
}

// TestPollWorkerReports_EmitsAndDerivesProblems asserts a single sweep calls the
// collector for the enabled worker and emits exactly one worker_report event.
func TestPollWorkerReports_EmitsAndDerivesProblems(t *testing.T) {
	var calls int64
	runner := countingRunner{calls: &calls}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := NewRegistry(cfg)

	pollWorkerReports(context.Background(), cfg, reg, fixedRunnerFor(runner), emit)

	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("CollectReport invocations: got %d, want 1", got)
	}
	if len(captured) != 1 {
		t.Fatalf("worker_report events: got %d, want 1", len(captured))
	}
	if captured[0].Type != core.EventType("worker_report") {
		t.Errorf("event type: got %q, want worker_report", captured[0].Type)
	}
}

// TestPollWorkerReports_NilRegistryNoOp asserts a nil registry (no worker
// configured — the empty-workers.yaml case) is a no-op: no collector call, no
// event. This is the off-by-default invariant at the sweep level.
func TestPollWorkerReports_NilRegistryNoOp(t *testing.T) {
	var calls int64
	runner := countingRunner{calls: &calls}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	// cfg with no workers + nil registry.
	pollWorkerReports(context.Background(), Config{}, nil, fixedRunnerFor(runner), emit)

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("CollectReport invocations: got %d, want 0 (nil registry)", got)
	}
	if len(captured) != 0 {
		t.Errorf("worker_report events: got %d, want 0 (nil registry)", len(captured))
	}
}

// TestPollWorkerReports_SkipsDisabledWorker asserts a worker with Enabled=false
// in cfg is skipped — no collector call, no event.
func TestPollWorkerReports_SkipsDisabledWorker(t *testing.T) {
	var calls int64
	runner := countingRunner{calls: &calls}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	w := reportTestWorker()
	w.Enabled = false
	cfg := Config{Version: 1, Workers: []Worker{w}}
	reg := NewRegistry(cfg)

	pollWorkerReports(context.Background(), cfg, reg, fixedRunnerFor(runner), emit)

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("CollectReport invocations: got %d, want 0 (disabled worker)", got)
	}
	if len(captured) != 0 {
		t.Errorf("worker_report events: got %d, want 0 (disabled worker)", len(captured))
	}
}

// TestPollWorkerReports_RespectsLiveDisable asserts a worker that the boot health
// check disabled in the REGISTRY (SetEnabled(false)) — while still Enabled in cfg
// — is skipped by the poll.
func TestPollWorkerReports_RespectsLiveDisable(t *testing.T) {
	var calls int64
	runner := countingRunner{calls: &calls}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := NewRegistry(cfg)
	reg.SetEnabled(false) // simulate boot health check disabling the worker

	pollWorkerReports(context.Background(), cfg, reg, fixedRunnerFor(runner), emit)

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("CollectReport invocations: got %d, want 0 (live-disabled)", got)
	}
}

// TestPollWorkerReports_SkipsNilRunnerTransport asserts a worker whose runner
// resolves to nil (unsupported transport) is skipped without a collector call.
func TestPollWorkerReports_SkipsNilRunnerTransport(t *testing.T) {
	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := NewRegistry(cfg)

	nilRunnerFor := func(w Worker) tmux.CommandRunner { return nil }
	pollWorkerReports(context.Background(), cfg, reg, nilRunnerFor, emit)

	if len(captured) != 0 {
		t.Errorf("worker_report events: got %d, want 0 (nil runner)", len(captured))
	}
}

// TestRunReportLoop_TicksAndEmits asserts the ticker loop drives at least one
// sweep at a fast interval and emits worker_report, then stops cleanly on ctx
// cancel. Uses a short real interval with a generous upper bound so the test is
// not flaky under load while still proving the cadence fires.
func TestRunReportLoop_TicksAndEmits(t *testing.T) {
	var calls int64
	runner := countingRunner{calls: &calls}

	var mu sync.Mutex
	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := func(ctx context.Context, et core.EventType, b []byte) error {
		mu.Lock()
		captured = append(captured, struct {
			Type    core.EventType
			Payload []byte
		}{et, b})
		mu.Unlock()
		return nil
	}

	// workers.yaml report_interval_seconds is whole-seconds, so drive the loop
	// with an explicitly short (10ms) interval via runReportLoopWithInterval so a
	// couple of ticks land well within the test deadline.
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := NewRegistry(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runReportLoopWithInterval(ctx, cfg, reg, fixedRunnerFor(runner), emit, 10*time.Millisecond)
		close(done)
	}()

	// Wait until at least one sweep emitted, bounded.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(captured)
		mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("RunReportLoop emitted no worker_report within 2s (calls=%d)", atomic.LoadInt64(&calls))
		case <-time.After(2 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunReportLoop did not stop within 2s of ctx cancel")
	}

	mu.Lock()
	if captured[0].Type != core.EventType("worker_report") {
		t.Errorf("event type: got %q, want worker_report", captured[0].Type)
	}
	mu.Unlock()
}

// TestRunReportLoop_OffByDefault asserts the loop returns immediately (no panic,
// no goroutine leak) when the registry is nil or no worker is enabled. With a nil
// registry RunReportLoop must not block, so a direct (non-goroutine) call returns.
func TestRunReportLoop_OffByDefault(t *testing.T) {
	// nil registry → immediate return.
	RunReportLoop(context.Background(), Config{}, nil, fixedRunnerFor(countingRunner{calls: new(int64)}), nil)

	// registry present but worker disabled → immediate return.
	w := reportTestWorker()
	w.Enabled = false
	cfg := Config{Version: 1, Workers: []Worker{w}}
	RunReportLoop(context.Background(), cfg, NewRegistry(cfg), fixedRunnerFor(countingRunner{calls: new(int64)}), nil)
}

// TestConfigReportInterval_Default asserts the default cadence is applied when
// report_interval_seconds is unset.
func TestConfigReportInterval_Default(t *testing.T) {
	if got := (Config{}).ReportInterval(); got != time.Duration(DefaultReportIntervalSeconds)*time.Second {
		t.Errorf("default ReportInterval: got %v, want %v", got, time.Duration(DefaultReportIntervalSeconds)*time.Second)
	}
	if got := (Config{ReportIntervalSeconds: 5}).ReportInterval(); got != 5*time.Second {
		t.Errorf("configured ReportInterval: got %v, want 5s", got)
	}
}
