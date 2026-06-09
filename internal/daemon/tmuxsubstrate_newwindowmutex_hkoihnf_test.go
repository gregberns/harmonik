package daemon_test

// tmuxsubstrate_newwindowmutex_hkoihnf_test.go — regression test for the
// concurrent-new-window contention wedge (hk-oihnf).
//
// # The bug
//
// All implementer/reviewer child windows are created in the daemon's single
// shared tmux session (sessionName, immutable). Two `tmux new-window` execs
// issued at the same instant contend on the tmux server's GLOBAL command lock:
// one serializes behind the other and, under load, can crawl ~16 min behind. A
// single bead never collides; the contention only appears under MaxConcurrent>1.
//
// # The fix
//
// SpawnWindow now holds a daemon-wide mutex (newWindowMu) around ONLY the bounded
// new-window exec (callNewWindowBounded), making window creation strictly
// one-at-a-time daemon-wide. The mutex is NOT held across the semaphore acquire or
// the spec-build, and is released on every return path (success / error / the
// 60 s timeout bound), so a hung new-window cannot block all launches forever.
//
// # What is tested
//
//   - NewWindowMutex_SerializesConcurrentSpawns: N concurrent SpawnWindow calls
//     against a recording adapter that flags any overlap of two NewWindowIn execs.
//     With the mutex, no two execs overlap. Without it (regression), the adapter's
//     concurrency counter exceeds 1 and the test fails.
//
// # Helper prefix
//
// Helpers use the prefix "hkoihnf" per implementer-protocol.md (namespaced by
// bead id to avoid redeclaration collisions with parallel daemon test beads).
//
// # Bead
//
//   - hk-oihnf (serialize tmux new-window + wire launch-timeout diagnostics + DOT reopen).

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workflow"
)

// hkoihnfOverlapAdapter is a concurrency-safe fake tmux adapter whose NewWindowIn
// records the maximum number of concurrently-executing NewWindowIn calls. If the
// daemon-wide mutex serializes window creation, maxConcurrent never exceeds 1.
// Each NewWindowIn sleeps briefly so that, absent serialization, two concurrent
// callers would reliably overlap.
type hkoihnfOverlapAdapter struct {
	// inFlight is the number of NewWindowIn calls currently executing.
	inFlight int32
	// maxConcurrent is the high-water mark of inFlight observed across all calls.
	maxConcurrent int32
	// calls counts total NewWindowIn invocations.
	calls int32
	// dwell is how long each NewWindowIn body runs (widens the overlap window).
	dwell time.Duration
	// paneSeq mints a unique pane id per call so sessions don't alias.
	paneSeq int32
}

func newHKOIHNFOverlapAdapter(dwell time.Duration) *hkoihnfOverlapAdapter {
	return &hkoihnfOverlapAdapter{dwell: dwell}
}

func (a *hkoihnfOverlapAdapter) ProbeTmux(_ context.Context) error { return nil }
func (a *hkoihnfOverlapAdapter) ListSessions(_ context.Context) ([]string, error) {
	return nil, nil
}
func (a *hkoihnfOverlapAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

// NewWindowIn records concurrency: it bumps inFlight, updates the high-water
// mark, dwells, then decrements. With the mutex the high-water mark stays at 1.
func (a *hkoihnfOverlapAdapter) NewWindowIn(_ context.Context, _ tmux.NewWindowIn) tmux.Outcome {
	atomic.AddInt32(&a.calls, 1)
	cur := atomic.AddInt32(&a.inFlight, 1)
	// Update the high-water mark (CAS loop; cheap, only widens).
	for {
		hw := atomic.LoadInt32(&a.maxConcurrent)
		if cur <= hw || atomic.CompareAndSwapInt32(&a.maxConcurrent, hw, cur) {
			break
		}
	}
	time.Sleep(a.dwell)
	atomic.AddInt32(&a.inFlight, -1)
	seq := atomic.AddInt32(&a.paneSeq, 1)
	return tmux.Outcome{
		Handle: tmux.WindowHandle("hkoihnf-session:win"),
		// Distinct pane id per call so each session targets its own pane.
		PaneID: "%" + itoaHKOIHNF(int(seq)),
	}
}

func (a *hkoihnfOverlapAdapter) KillWindow(_ context.Context, _ tmux.WindowHandle) error { return nil }
func (a *hkoihnfOverlapAdapter) WindowPanePID(_ context.Context, _ tmux.WindowHandle) (int, error) {
	return 0, nil
}
func (a *hkoihnfOverlapAdapter) WindowPaneID(_ context.Context, _ tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *hkoihnfOverlapAdapter) KillSession(_ context.Context, _ string) error { return nil }
func (a *hkoihnfOverlapAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (a *hkoihnfOverlapAdapter) PasteBuffer(_ context.Context, _, _ string) error     { return nil }
func (a *hkoihnfOverlapAdapter) SendKeysLiteral(_ context.Context, _, _ string) error { return nil }
func (a *hkoihnfOverlapAdapter) SendKeysEnter(_ context.Context, _ string) error      { return nil }
func (a *hkoihnfOverlapAdapter) SendKeysQuit(_ context.Context, _ string) error       { return nil }
func (a *hkoihnfOverlapAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error {
	return nil
}

var _ tmux.Adapter = (*hkoihnfOverlapAdapter)(nil)

// itoaHKOIHNF is a tiny base-10 int formatter (avoids importing strconv just for
// the pane-id suffix, keeping this test file self-contained).
func itoaHKOIHNF(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// TestNewWindowMutex_SerializesConcurrentSpawns fires several concurrent
// SpawnWindow calls and asserts the daemon-wide mutex serialized the underlying
// `tmux new-window` execs — i.e. no two NewWindowIn bodies ever overlapped.
//
// Pre-fix (no mutex) the recording adapter observes maxConcurrent > 1 because the
// per-goroutine SpawnWindow calls reach NewWindowIn concurrently — exactly the
// tmux-server-lock contention this fix removes.
func TestNewWindowMutex_SerializesConcurrentSpawns(t *testing.T) {
	t.Parallel()

	const n = 6
	adapter := newHKOIHNFOverlapAdapter(20 * time.Millisecond)

	// No spawn cap: isolate the new-window mutex (the contention point) from the
	// semaphore. A high cap (or none) lets all N goroutines race into the
	// new-window exec simultaneously, which is what the mutex must serialize.
	sub := daemon.NewTmuxSubstrate(adapter, "hkoihnf-session")

	var wg sync.WaitGroup
	errs := make(chan error, n)
	// Release all goroutines at once to maximise the chance of overlap under a
	// regression.
	gate := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, err := sub.SpawnWindow(context.Background(), handler.SubstrateSpawn{
				Argv:       []string{"claude"},
				WindowName: "hk-oihnf-window",
			})
			errs <- err
		}()
	}
	close(gate)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("SpawnWindow returned error: %v", err)
		}
	}

	if got := atomic.LoadInt32(&adapter.calls); got != n {
		t.Fatalf("NewWindowIn call count = %d, want %d", got, n)
	}
	if hw := atomic.LoadInt32(&adapter.maxConcurrent); hw > 1 {
		t.Fatalf("concurrent tmux new-window execs observed (max=%d) — the daemon-wide "+
			"new-window mutex did NOT serialize window creation (hk-oihnf contention regression)", hw)
	}
}

// TestScenario_DotMode_NewWindowTimeoutEmitsDiagnosticAndReopens verifies the
// hk-oihnf Part-3 fix: the DOT cascade agentic-node dispatch surfaces a structural
// `tmux new-window` timeout (the no-spawn wedge) as a tmux_new_window_timeout
// diagnostic event and routes to bead-reopen — mirroring the single-mode path
// (workloop.go beadRunOne errors.Is branch).
//
// Setup reuses the hk-3qjwl DOT harness, but injects a *tmuxSubstrate built on the
// hk-r1rup blocking adapter (NewWindowIn never returns) with a short new-window
// timeout. handler.Launch → SpawnWindow therefore fails fast with
// ErrTmuxNewWindowTimeout, hitting the new branch.
//
// Pre-fix (no errors.Is branch in dispatchDotAgenticNode) the DOT path returned an
// opaque "launch node ...: %w" error: needsAttention was still set (so the bead
// reopened) but NO tmux_new_window_timeout event was emitted — the operator never
// saw WHY. This test's event assertion is the regression guard for that gap.
func TestScenario_DotMode_NewWindowTimeoutEmitsDiagnosticAndReopens(t *testing.T) {
	t.Parallel()

	projectDir := implReadyFixtureProjectDir(t)
	wtPath, parentSHA := implReadyFixtureWorktree(t, projectDir)
	adapterReg := implReadyFixtureAdapterRegistry(t)

	dotPath := filepath.Join(dotE2EModuleRoot(), "specs", "examples", "review-loop.dot")
	graph, loadErr := workflow.LoadDotWorkflow(dotPath)
	if loadErr != nil {
		t.Fatalf("LoadDotWorkflow(%s): %v", dotPath, loadErr)
	}

	collector := &stubEventCollector{}
	ledger := &stubBeadLedger{}

	// Inject a real *tmuxSubstrate whose adapter hangs in NewWindowIn. The short
	// new-window timeout makes SpawnWindow return ErrTmuxNewWindowTimeout promptly,
	// reproducing the no-spawn wedge as a prompt structural launch failure.
	blockingAdapter := newHKR1RUPBlockingAdapter() // release never closed → NewWindowIn hangs
	substrate := daemon.NewTmuxSubstrate(blockingAdapter, "hkoihnf-dot-session",
		daemon.WithNewWindowTimeout(150*time.Millisecond))

	deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
		BrAdapter:           ledger,
		Bus:                 collector,
		ProjectDir:          projectDir,
		HandlerBinary:       "claude",
		IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
		WorkflowModeDefault: core.WorkflowModeDot,
		AdapterRegistry2:    adapterReg,
		AgentReadyTimeout:   100 * time.Millisecond,
		HookStore:           daemon.ExportedNewHookSessionStore(),
		Substrate:           substrate,
	})

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()

	result := daemon.ExportedDriveDotWorkflow(
		ctx, deps,
		implReadyFixtureRunID(t),
		core.BeadID("dot-newwindow-timeout-001"),
		wtPath, parentSHA,
		graph,
	)

	t.Logf("result=%+v events=%v", result, collector.eventTypes())

	// The agentic-node launch failed structurally → walk did not reach success,
	// and the needsAttention flag is what beadRunOne acts on to ReopenBead.
	if result.Success {
		t.Errorf("expected success=false on new-window timeout; got true")
	}
	if !result.NeedsAttention {
		t.Errorf("expected needsAttention=true (the reopen signal); got false (summary=%q)", result.Summary)
	}

	// The diagnostic event must be emitted from the DOT path (the Part-3 fix).
	found := false
	for _, et := range collector.eventTypes() {
		if et == string(core.EventTypeTmuxNewWindowTimeout) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("tmux_new_window_timeout event NOT emitted from the DOT launch-error path "+
			"(hk-oihnf Part-3 regression); events: %v", collector.eventTypes())
	}
}
