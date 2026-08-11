package workers

// breach_poll_test.go — unit tests for the adaptive breach-detection poll
// (worker-report Phase 2, PB3). Intra-package so breachSweep /
// runReportLoopWithInterval / breachLoopState can be driven directly with an
// injected `now`, exercising the dwell math deterministically (no real time).
//
// The fast/slow cadence selection is tested via runReportLoopWithInterval with
// sub-second injected intervals; the breach/clear/reset semantics are tested via
// breachSweep with an injected `now` so the 20s/15s dwells are exact.
//
// Bead ref: PB3 (hk-kaf0).

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// highCPUCollectorStdout is collector output with load5/ncpu = 8.0/8 = 1.0 — well
// over the cpu enter threshold (0.85) — and memory/swap OK, so it drives the CPU
// signal into breach while leaving the others resting.
const highCPUCollectorStdout = `load=8.00 8.00 8.00
ncpu=8
memtotal=17179869184
vmstat<<Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              100000.
Pages inactive:                           50000.
Pages active:                            200000.

swap=total = 2048.00M  used = 0.00M  free = 2048.00M  (encrypted)
disk=/dev/disk1s1   476802   12345   400000    24%  1234567  9876543   11%   /System/Volumes/Data
claude=1
pagesize=16384
`

// stdoutRunner is a fake CommandRunner echoing a fixed collector stdout and
// counting invocations. The stdout can be swapped between sweeps via a pointer so
// a test can drive a high-CPU then a low-CPU sample through the same runner.
type stdoutRunner struct {
	stdout *string
	calls  *int64
	// swept, when set, is signalled once per invocation so a test can WAIT for
	// a sweep instead of sleeping for a window and counting what arrived. It is
	// buffered and the send is non-blocking, so a runner nobody is reading from
	// behaves exactly as it did before this field existed.
	swept chan<- struct{}
}

func (r stdoutRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if r.calls != nil {
		atomic.AddInt64(r.calls, 1)
	}
	if r.swept != nil {
		select {
		case r.swept <- struct{}{}:
		default:
		}
	}
	//nolint:gosec // G204: stdout is a controlled test fixture, passed as shell data rather than code.
	return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$0"`, *r.stdout)
}

var _ tmux.CommandRunner = stdoutRunner{}

// safeCapture is a concurrency-safe (type,payload) recorder for the sweep's
// per-worker goroutines.
type safeCapture struct {
	mu       sync.Mutex
	captured []struct {
		Type    core.EventType
		Payload []byte
	}
}

func (s *safeCapture) emit() EmitFunc {
	return func(ctx context.Context, et core.EventType, b []byte) error {
		s.mu.Lock()
		s.captured = append(s.captured, struct {
			Type    core.EventType
			Payload []byte
		}{et, b})
		s.mu.Unlock()
		return nil
	}
}

func (s *safeCapture) breachPayloads() []ResourceBreachPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ResourceBreachPayload
	for _, c := range s.captured {
		if c.Type != core.EventTypeResourceBreach {
			continue
		}
		var p ResourceBreachPayload
		if err := json.Unmarshal(c.Payload, &p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func (s *safeCapture) countType(t core.EventType) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.captured {
		if c.Type == t {
			n++
		}
	}
	return n
}

// regInFlight returns a registry for the one test worker with n slots reserved.
func regInFlight(n int) *Registry {
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := NewRegistry(cfg)
	for i := 0; i < n; i++ {
		reg.SelectWorker()
	}
	return reg
}

// --- cadence-selection (fast only while InFlight>0) ---

// TestReportLoopInterval_SelectsFastOnlyWhenInFlight is where the cadence rule
// is actually tested. It reads the decision the loop makes rather than timing
// how often the OS ran it.
//
// This replaces a test that ran the real loop for 120 ms and asserted at least
// five sweeps (hk-vp02y). Nothing in the product promises the OS schedules a
// goroutine at a given rate, and under the merge decision's own load it does
// not: that assertion failed inside `make full` while passing in isolation, so
// the gate returned a different verdict on an unchanged commit.
func TestReportLoopInterval_SelectsFastOnlyWhenInFlight(t *testing.T) {
	const slow, fast = 200 * time.Millisecond, 5 * time.Millisecond

	cases := []struct {
		name          string
		breachEnabled bool
		inFlight      int
		want          time.Duration
	}{
		{"in flight and breach on → fast", true, 1, fast},
		{"several in flight → still fast", true, 4, fast},
		{"idle → slow", true, 0, slow},
		{"breach detection off → slow even in flight", false, 1, slow},
		{"breach detection off and idle → slow", false, 0, slow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reportLoopInterval(tc.breachEnabled, tc.inFlight, slow, fast); got != tc.want {
				t.Errorf("reportLoopInterval(breachEnabled=%v, inFlight=%d) = %v, want %v", tc.breachEnabled, tc.inFlight, got, tc.want)
			}
		})
	}
}

// TestRunReportLoop_FastWhenInFlight keeps the integration half — that the LOOP
// consults the rule above rather than holding a fixed cadence of its own — with
// margins wide enough that box load cannot flip either answer.
//
// The margins are the whole design. In flight, the test waits for sweeps with a
// generous deadline and no rate at all: a loop that wrongly chose the slow
// cadence would wait an hour, so the deadline separates the two answers by four
// orders of magnitude rather than by a scheduling delay. Idle, it asserts that
// NO sweep happens in a short window against a correct answer of one hour. A
// machine slow enough to fail either of these is a machine that is not running.
func TestRunReportLoop_FastWhenInFlight(t *testing.T) {
	// slow=1h: a loop that picks it never sweeps within any test's lifetime.
	const slow, fast = time.Hour, 5 * time.Millisecond
	const wantSweeps = 5

	sout := highCPUCollectorStdout
	capture := &safeCapture{}
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}

	// ── in flight: the loop must sweep repeatedly, however long it takes ──
	var calls int64
	swept := make(chan struct{}, 1)
	runner := stdoutRunner{stdout: &sout, calls: &calls, swept: swept}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runReportLoopWithInterval(ctx, cfg, regInFlight(1), fixedRunnerFor(runner), capture.emit(), slow, fast)
		close(done)
	}()
	deadline := time.After(60 * time.Second)
	for atomic.LoadInt64(&calls) < wantSweeps {
		select {
		case <-swept:
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("the loop swept %d times in 60s with fast=%v in flight; a loop holding the slow cadence (%v) would look exactly like this", atomic.LoadInt64(&calls), fast, slow)
		}
	}
	cancel()
	<-done

	// ── idle: the loop must NOT sweep, because the slow cadence is an hour away ──
	atomic.StoreInt64(&calls, 0)
	idleRunner := stdoutRunner{stdout: &sout, calls: &calls}
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() {
		runReportLoopWithInterval(ctx2, cfg, NewRegistry(cfg), fixedRunnerFor(idleRunner), capture.emit(), slow, fast)
		close(done2)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel2()
	<-done2

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("an IDLE loop swept %d times inside 200ms; the slow cadence is %v away, so it took the fast one", got, slow)
	}
}

// --- breach + recovery (sustained over enter ⇒ one breach; under exit ⇒ one clear) ---

// TestBreachSweep_SustainedBreachThenClear drives several fast sweeps of a
// high-CPU sample across the breach dwell (20s) — expecting exactly one
// resource_breach{breach} with InFlight set — then low-CPU samples across the
// clear dwell (15s) — expecting exactly one {clear}.
func TestBreachSweep_SustainedBreachThenClear(t *testing.T) {
	sout := highCPUCollectorStdout
	runner := stdoutRunner{stdout: &sout}
	capture := &safeCapture{}
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := regInFlight(2) // InFlight()==2 ⇒ breaches stamp InFlight=2
	st := newBreachLoopState(cfg)
	const slow = 60 * time.Second

	// Sweep at t=0,5,10,15,20,25s — sustained over enter. Breach fires once the
	// dwell (20s from the first over-enter at t=0) matures, i.e. at t>=20s.
	for _, sec := range []int{0, 5, 10, 15, 20, 25} {
		breachSweep(context.Background(), cfg, reg, fixedRunnerFor(runner), capture.emit(), st, true, slow, at(sec))
	}

	breaches := capture.breachPayloads()
	var fired []ResourceBreachPayload
	for _, b := range breaches {
		if b.Kind == "breach" {
			fired = append(fired, b)
		}
	}
	if len(fired) != 1 {
		t.Fatalf("breach events: got %d, want exactly 1 (%+v)", len(fired), breaches)
	}
	if fired[0].Signal != "cpu" {
		t.Errorf("breach signal: got %q, want cpu", fired[0].Signal)
	}
	if fired[0].InFlight != 2 {
		t.Errorf("breach InFlight: got %d, want 2 (stamped from reg)", fired[0].InFlight)
	}

	// Recovery: low-CPU samples from t=30s under the exit threshold; the clear
	// fires once the clear dwell (15s) matures, i.e. at t>=45s.
	sout = cannedCollectorStdout // load5/ncpu = 1.10/8 ≈ 0.14, well under exit 0.70
	for _, sec := range []int{30, 35, 40, 45} {
		breachSweep(context.Background(), cfg, reg, fixedRunnerFor(runner), capture.emit(), st, true, slow, at(sec))
	}

	var clears []ResourceBreachPayload
	for _, b := range capture.breachPayloads() {
		if b.Kind == "clear" {
			clears = append(clears, b)
		}
	}
	if len(clears) != 1 {
		t.Fatalf("clear events: got %d, want exactly 1 (%+v)", len(clears), clears)
	}
	if clears[0].Signal != "cpu" {
		t.Errorf("clear signal: got %q, want cpu", clears[0].Signal)
	}
}

// --- transition-to-idle while mid-breach emits a clear via Reset ---

// TestBreachSweep_IdleTransitionResetsBreach drives the CPU signal into BREACHED
// while in flight, then runs a sweep with the worker now idle (InFlight()==0) —
// expecting the detector Reset to emit a clear (InFlight 0) so the breach doesn't
// dangle, and no new breach.
func TestBreachSweep_IdleTransitionResetsBreach(t *testing.T) {
	sout := highCPUCollectorStdout
	runner := stdoutRunner{stdout: &sout}
	capture := &safeCapture{}
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := regInFlight(1)
	st := newBreachLoopState(cfg)
	const slow = 60 * time.Second

	// Drive into BREACHED (over enter sustained past the 20s dwell).
	for _, sec := range []int{0, 5, 10, 15, 20} {
		breachSweep(context.Background(), cfg, reg, fixedRunnerFor(runner), capture.emit(), st, true, slow, at(sec))
	}
	if n := countKind(capture, "breach"); n != 1 {
		t.Fatalf("pre-idle breach events: got %d, want 1", n)
	}

	// Transition to idle: release the slot so InFlight()==0, then sweep. The
	// detector should Reset and emit a clear with InFlight 0.
	reg.ReleaseSlot()
	breachSweep(context.Background(), cfg, reg, fixedRunnerFor(runner), capture.emit(), st, true, slow, at(25))

	clears := []ResourceBreachPayload{}
	for _, b := range capture.breachPayloads() {
		if b.Kind == "clear" {
			clears = append(clears, b)
		}
	}
	if len(clears) != 1 {
		t.Fatalf("idle-transition clear: got %d, want 1 (%+v)", len(clears), clears)
	}
	if clears[0].InFlight != 0 {
		t.Errorf("reset clear InFlight: got %d, want 0", clears[0].InFlight)
	}
}

// --- worker_report throttled to the slow cadence, not every fast tick ---

// TestBreachSweep_WorkerReportThrottledToSlow asserts that across many fast
// sweeps within one slow interval, worker_report is emitted only on the
// slow-boundary sweeps — not every fast tick.
func TestBreachSweep_WorkerReportThrottledToSlow(t *testing.T) {
	sout := cannedCollectorStdout
	runner := stdoutRunner{stdout: &sout}
	capture := &safeCapture{}
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}}
	reg := regInFlight(1)
	st := newBreachLoopState(cfg)
	const slow = 60 * time.Second

	// Fast sweeps at 5s spacing across two slow boundaries: t=0 (first, due),
	// 5,10,...,60 (the t=60 sweep crosses the next slow boundary, due), 65.
	secs := []int{0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65}
	for _, sec := range secs {
		breachSweep(context.Background(), cfg, reg, fixedRunnerFor(runner), capture.emit(), st, true, slow, at(sec))
	}

	reports := capture.countType(core.EventTypeWorkerReport)
	// Due at t=0 (first sight) and t=60 (>= slow since last report). 14 fast
	// sweeps but only 2 worker_reports.
	if reports != 2 {
		t.Fatalf("worker_report emits across 14 fast sweeps: got %d, want 2 (slow-throttled)", reports)
	}
}

// --- breach_detection_enabled:false ⇒ no breach events, identical to Phase 1 ---

// TestBreachSweep_DisabledNoBreachEvents asserts that with breachEnabled=false a
// sustained high-CPU sample produces NO resource_breach events — only the
// worker_report — identical to Phase 1.
func TestBreachSweep_DisabledNoBreachEvents(t *testing.T) {
	sout := highCPUCollectorStdout
	runner := stdoutRunner{stdout: &sout}
	capture := &safeCapture{}
	disabled := false
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}, BreachDetectionEnabledPtr: &disabled}
	reg := regInFlight(1)
	st := newBreachLoopState(cfg)
	const slow = 60 * time.Second

	for _, sec := range []int{0, 5, 10, 15, 20, 25} {
		breachSweep(context.Background(), cfg, reg, fixedRunnerFor(runner), capture.emit(), st, false /*breachEnabled*/, slow, at(sec))
	}

	if n := capture.countType(core.EventTypeResourceBreach); n != 0 {
		t.Fatalf("disabled: resource_breach events got %d, want 0", n)
	}
	if n := capture.countType(core.EventTypeWorkerReport); n == 0 {
		t.Fatalf("disabled: expected at least one worker_report (Phase-1 behaviour), got 0")
	}
}

// TestRunReportLoop_DisabledNeverFast asserts that with breach_detection_enabled
// false the loop NEVER ticks fast even with a run in flight — byte-identical
// cadence to Phase 1 (slow only).
func TestRunReportLoop_DisabledNeverFast(t *testing.T) {
	var calls int64
	sout := highCPUCollectorStdout
	runner := stdoutRunner{stdout: &sout, calls: &calls}
	capture := &safeCapture{}
	disabled := false
	cfg := Config{Version: 1, Workers: []Worker{reportTestWorker()}, BreachDetectionEnabledPtr: &disabled}
	reg := regInFlight(1) // in flight, but breach disabled ⇒ still slow

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		// slow=40ms, fast=2ms: if the loop honoured fast we'd see ~50 sweeps; with
		// breach disabled it must collapse to slow ⇒ only a handful.
		runReportLoopWithInterval(ctx, cfg, reg, fixedRunnerFor(runner), capture.emit(), 40*time.Millisecond, 2*time.Millisecond)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if got := atomic.LoadInt64(&calls); got > 6 {
		t.Fatalf("disabled loop ticked fast: got %d sweeps in 100ms, want slow (<=6 @40ms)", got)
	}
	if n := capture.countType(core.EventTypeResourceBreach); n != 0 {
		t.Fatalf("disabled loop emitted %d resource_breach events, want 0", n)
	}
}

// --- empty registry ⇒ no-op ---

// TestBreachSweep_NilRegistryNoOp asserts a nil registry (no worker configured) is
// a no-op for the adaptive sweep: no collector call, no event.
func TestBreachSweep_NilRegistryNoOp(t *testing.T) {
	var calls int64
	sout := highCPUCollectorStdout
	runner := stdoutRunner{stdout: &sout, calls: &calls}
	capture := &safeCapture{}
	st := newBreachLoopState(Config{})

	breachSweep(context.Background(), Config{}, nil, fixedRunnerFor(runner), capture.emit(), st, true, 60*time.Second, at(0))

	if got := atomic.LoadInt64(&calls); got != 0 {
		t.Errorf("nil-registry sweep: collector calls got %d, want 0", got)
	}
	if len(capture.captured) != 0 {
		t.Errorf("nil-registry sweep: events got %d, want 0", len(capture.captured))
	}
}

// TestRunReportLoop_EmptyRegistryReturns asserts RunReportLoop with an empty
// registry / no enabled worker returns immediately (no goroutine, no panic) —
// the off-by-default invariant at the loop level, unchanged by PB3.
func TestRunReportLoop_EmptyRegistryReturns(t *testing.T) {
	// nil registry → immediate return.
	RunReportLoop(context.Background(), Config{}, nil, fixedRunnerFor(stdoutRunner{stdout: new(string)}), nil)
	// no enabled worker → immediate return.
	w := reportTestWorker()
	w.Enabled = false
	cfg := Config{Version: 1, Workers: []Worker{w}}
	RunReportLoop(context.Background(), cfg, NewRegistry(cfg), fixedRunnerFor(stdoutRunner{stdout: new(string)}), nil)
}

// --- Phase-2 config accessors apply the right defaults ---

func TestConfig_BreachAccessorsDefaults(t *testing.T) {
	c := Config{}
	if !c.BreachDetectionEnabled() {
		t.Errorf("BreachDetectionEnabled default: got false, want true")
	}
	off := false
	if (Config{BreachDetectionEnabledPtr: &off}).BreachDetectionEnabled() {
		t.Errorf("explicit breach_detection_enabled:false: got true, want false")
	}
	if got := c.BreachSampleInterval(); got != time.Duration(DefaultBreachSampleIntervalSeconds)*time.Second {
		t.Errorf("BreachSampleInterval default: got %v, want %ds", got, DefaultBreachSampleIntervalSeconds)
	}
	if got := (Config{BreachSampleIntervalSeconds: 3}).BreachSampleInterval(); got != 3*time.Second {
		t.Errorf("BreachSampleInterval configured: got %v, want 3s", got)
	}
	if got := c.CPUSourceOrDefault(); got != DefaultCPUSource {
		t.Errorf("CPUSourceOrDefault default: got %q, want %q", got, DefaultCPUSource)
	}
	// Unset dwell/thresholds ⇒ zero ⇒ breach.go withDefaults fills them. Verify
	// the BreachConfig passes zeros through (defaulting happens in NewBreachDetector).
	bc := c.BreachConfig()
	if bc.BreachDwell != 0 || bc.ClearDwell != 0 || bc.CPUEnter != 0 {
		t.Errorf("unset BreachConfig should be zero-valued (defaulted later), got %+v", bc)
	}
	// A configured dwell + threshold flows through.
	bc2 := Config{BreachDwellSeconds: 30, ClearDwellSeconds: 10, CPUEnter: 0.9, MemFreeEnter: 0.05, SwapEnterMB: 512}.BreachConfig()
	if bc2.BreachDwell != 30*time.Second || bc2.ClearDwell != 10*time.Second {
		t.Errorf("configured dwell flow-through: got %+v", bc2)
	}
	if bc2.CPUEnter != 0.9 || bc2.MemEnter != 0.05 || bc2.SwapEnter != 512 {
		t.Errorf("configured thresholds flow-through: got %+v", bc2)
	}
}

// countKind counts captured resource_breach events of a given Kind.
func countKind(c *safeCapture, kind string) int {
	n := 0
	for _, b := range c.breachPayloads() {
		if b.Kind == kind {
			n++
		}
	}
	return n
}
