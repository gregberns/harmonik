package daemon_test

// codex_spawnproof_hk47u9z_test.go — a SessionIDCaptured harness must emit a
// spawn proof when it captures its session id, so the never-spawned reaper
// disarms instead of acting as an absolute 30-minute wall-clock cap.
//
// Coverage:
//   - the capture callback emits exactly ONE agent_ready, carrying the run id
//     (an envelope with a nil RunID is silently dropped by the watcher, so
//     emitting without it would look correct and do nothing — the hk-wths
//     failure shape)
//   - a codex-shaped run whose session id was captured is NOT reaped 31 min in
//   - a codex-shaped run that never produced output IS still reaped, so the
//     genuine launch-stall guard survives the fix
//   - the end-to-end chain: REAL codex JSONL -> REAL interceptor -> emit ->
//     REAL StaleWatcher, with no hand-rolled restatement of the reaper's
//     firing condition anywhere in this file
//
// Why the bug existed: agent_ready is emitted only by the Claude SessionStart
// hook path. Codex is SessionIDCaptured and structurally never emits it, so
// agentReadySeen stayed false for the entire life of every codex run and the
// launch-stall guard silently became a cap on TOTAL run time. It killed a run
// with commit_landed=true and exit_code=0 at 30m19s and reported it as
// "context cancelled at node commit_gate" — the wrong node and the wrong
// cause, which is why this read as "codex is unreliable" rather than as a
// platform defect.
//
// Capturing a session id off the harness's own stdout IS proof the child
// spawned, which is exactly what this reaper wants to know.
//
// NOTE: a child that spawns and THEN wedges is caught by nothing on this path.
// The rationale is stated once, at the two dot_cascade.go sites (the capture
// callback and newCapturedSpawnProof's doc); it is deliberately NOT restated
// here, because triplicating it is how the earlier false run_stale claim ended
// up in three places at once. The gap is pre-existing and tracked as hk-spqhh.
//
// This file deliberately drives the PRODUCTION StaleWatcher through the same
// exported seams the hk-0z5x tests use, and the PRODUCTION codex interceptor.
// An earlier draft restated the reaper's firing condition inline; that would
// have tested a copy of the logic rather than the logic, which is the exact
// pattern that let two other defects in this lane survive their own tests.
//
// Helper prefix: sp47.
//
// Bead ref: hk-47u9z.

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// sp47CodexStream is a realistic codex JSONL prefix. Two thread.started lines:
// a harness may re-announce, and the reaper needs only the first.
const sp47CodexStream = `{"type":"thread.started","thread_id":"th_hk47u9z_one"}
{"type":"turn.started","turn_id":"tr_1"}
{"type":"thread.started","thread_id":"th_hk47u9z_two"}
`

// sp47ProductionSpawnProof returns the PRODUCTION spawn-proof closure wired to
// a collecting emitter. It deliberately does NOT rebuild the closure: an
// earlier draft of this test did, and it passed with the production wiring in
// dot_cascade.go fully reverted — a false green, caught only by testing the
// revert. Driving the real closure is what makes the assertions below mean
// anything.
func sp47ProductionSpawnProof(em *sp47CollectingEmitter, runID core.RunID) func() {
	return daemon.ExportedNewCapturedSpawnProof(context.Background(), em, runID)
}

// sp47CollectingEmitter records every envelope the production tap emits.
type sp47CollectingEmitter struct {
	mu   sync.Mutex
	envs []core.EventEnvelope
}

func (e *sp47CollectingEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.envs = append(e.envs, core.EventEnvelope{Type: string(eventType), Payload: payload})
	return nil
}

func (e *sp47CollectingEmitter) EmitWithRunID(_ context.Context, runID core.RunID, eventType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	rid := runID
	e.envs = append(e.envs, core.EventEnvelope{Type: string(eventType), RunID: &rid, Payload: payload})
	return nil
}

func (e *sp47CollectingEmitter) agentReady() []core.EventEnvelope {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []core.EventEnvelope
	for _, ev := range e.envs {
		if core.EventType(ev.Type) == core.EventTypeAgentReady {
			out = append(out, ev)
		}
	}
	return out
}

// sp47CaptureThroughRealInterceptor runs the PRODUCTION codex interceptor over
// a real codex JSONL stream, invoking onCapture for every captured id exactly
// as dot_cascade.go's StdoutWrapper does. Returns the captured ids.
func sp47CaptureThroughRealInterceptor(t *testing.T, stream string, onCapture func(string)) []string {
	t.Helper()
	h := &daemon.CodexHarness{}
	var ids []string
	r := h.NewSessionIDInterceptor(strings.NewReader(stream), func(id string) {
		ids = append(ids, id)
		onCapture(id)
	}, func() {})
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("sp47: read intercepted stream: %v", err)
	}
	if string(body) != stream {
		t.Fatalf("sp47: interceptor altered the stream; it must pass bytes through unchanged")
	}
	return ids
}

// The PRODUCTION spawn proof must fire exactly once from real codex output,
// and the emitted agent_ready must carry the run id — the watcher's observe()
// silently drops any envelope with a nil RunID, so emitting without it would
// look correct and change nothing (the hk-wths failure shape).
func TestCodexSpawnProof_CaptureEmitsOneAgentReadyWithRunID_hk47u9z(t *testing.T) {
	t.Parallel()

	em := &sp47CollectingEmitter{}
	runID := nsrNewRunID(t)
	proof := sp47ProductionSpawnProof(em, runID)

	ids := sp47CaptureThroughRealInterceptor(t, sp47CodexStream, func(string) { proof() })

	if len(ids) == 0 {
		t.Fatalf("no session id captured from a real codex thread.started stream — the spawn proof can never fire, so every codex run stays capped at 30 min")
	}
	ready := em.agentReady()
	if len(ready) != 1 {
		t.Fatalf("production spawn proof emitted %d agent_ready events, want exactly 1 (0 = the reaper never disarms and the cap stands; >1 = duplicate noise in events.jsonl on every harness re-announce)", len(ready))
	}
	if ready[0].RunID == nil {
		t.Fatalf("agent_ready carries a nil RunID — the stale watcher drops such envelopes, so the reaper would still fire while this emit looked correct")
	}
	if *ready[0].RunID != runID {
		t.Errorf("agent_ready RunID = %v, want %v", *ready[0].RunID, runID)
	}
}

// END-TO-END, and the assertion that fails against the pre-fix code: a
// codex-shaped run whose session id was captured must survive past the
// never-spawned deadline. Drives the REAL watcher.
func TestCodexSpawnProof_CapturedRunSurvivesDeadline_hk47u9z(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 7, 22, 5, 49, 48, 0, time.UTC) // the field incident's launch time
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-acdx-z08", // the bead the field incident killed
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	// The codex child starts producing output a second later. The PRODUCTION
	// spawn-proof closure decides whether anything is emitted at all; this test
	// only relays what it emits into the watcher. With the dot_cascade wiring
	// reverted, nothing is emitted and this test fails — which is the point.
	em := &sp47CollectingEmitter{}
	proof := sp47ProductionSpawnProof(em, runID)
	ids := sp47CaptureThroughRealInterceptor(t, sp47CodexStream, func(string) { proof() })
	for _, ev := range em.agentReady() {
		if ev.RunID == nil {
			t.Fatalf("production proof emitted agent_ready with a nil RunID; the watcher would drop it")
		}
		nsrSimulateEvent(w, clk, runID, core.EventTypeAgentReady, epoch.Add(time.Second))
	}
	if len(ids) == 0 {
		t.Fatalf("sp47: no id captured; the rest of this test would pass vacuously")
	}

	// Well past the 30-minute deadline — the point at which the field incident
	// was killed with commit_landed=true and exit_code=0.
	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("never-spawned reaper cancelled a codex run 31 min in, after its session id was captured — this is the hk-47u9z hard cap: the guard stops being a launch-stall detector and becomes an absolute wall-clock limit on total run time")
	}
	if daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("run marked aborted despite a captured session id proving the child spawned")
	}
}

// The guard must still do its real job. A launch that never produces output
// never reaches the capture callback, so no proof is emitted and the reaper
// must still fire — otherwise the fix trades a hard cap for a run that can
// hang forever.
func TestCodexSpawnProof_StalledLaunchStillReaped_hk47u9z(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 7, 22, 5, 49, 48, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{
		BeadID:    "hk-47u9z-stalled",
		StartedAt: epoch,
		Cancel:    func() { cancelCalled.Store(true) },
	}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	// A stream with NO thread.started: the child never announced itself, so the
	// production interceptor never fires and no proof is emitted.
	ids := sp47CaptureThroughRealInterceptor(t,
		"{\"type\":\"turn.started\",\"turn_id\":\"tr_1\"}\n",
		func(string) { t.Fatalf("sp47: capture fired on a stream with no thread.started") })
	if len(ids) != 0 {
		t.Fatalf("sp47: captured %v from a stream with no thread.started", ids)
	}

	clk.Set(epoch.Add(31 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if !cancelCalled.Load() {
		t.Error("never-spawned reaper did NOT fire for a launch that produced no output at all — the genuine launch-stall guard has been disarmed, letting a wedged launch hang forever")
	}
	if !daemon.ExportedRunHandleIsAborted(handle) {
		t.Error("stalled run not marked aborted; the bead will not be reopened")
	}
}

// Before the deadline nothing fires, so the surviving-run assertion above is
// about the DEADLINE being disarmed, not about the clock never reaching it.
func TestCodexSpawnProof_StalledLaunchNotReapedEarly_hk47u9z(t *testing.T) {
	t.Parallel()

	reg := daemon.NewRunRegistry()
	runID := nsrNewRunID(t)
	epoch := time.Date(2026, 7, 22, 5, 49, 48, 0, time.UTC)
	clk := newMutableClock(epoch)

	var cancelCalled atomic.Bool
	handle := &daemon.RunHandle{BeadID: "hk-47u9z-early", StartedAt: epoch, Cancel: func() { cancelCalled.Store(true) }}
	reg.Register(runID, handle)

	w, _ := nsrBuildWatcher(t, reg, 30*time.Minute, clk)
	nsrSimulateEvent(w, clk, runID, core.EventTypeLaunchInitiated, epoch)

	clk.Set(epoch.Add(29 * time.Minute))
	daemon.ExportedStalewatchScan(w, context.Background())
	time.Sleep(50 * time.Millisecond)

	if cancelCalled.Load() {
		t.Error("never-spawned reaper fired BEFORE its deadline")
	}
}
