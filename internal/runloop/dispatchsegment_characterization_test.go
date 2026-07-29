package runloop

// dispatchsegment_characterization_test.go — characterization of the LAUNCH and
// DISPATCH steps of the shared launch → dispatch → wait → probe → teardown
// sequence.
//
// DispatchSegment is the one primitive all four dispatch sites share:
// workloop.go's single mode, reviewloop.go's implementer/reviewer phases,
// dot_cascade_core.go's per-node dispatch, and dot_gate.go's cognition gates.
// The pre-existing dispatchsegment_test.go covers the resume shape (the
// stalled-resume ready-timeout edge and the transitional resume probe) plus the
// launch-error classifier in isolation. This file covers the rest of the
// contract: the fresh-launch happy path, the launch-failure branch as the
// segment actually drives it, the two harness postures (adapter-less and
// completion-by-process-exit), the exec-path agent exit, and the abort edge.
//
// What is pinned is the HOOK CONTRACT and the resulting terminal — which hooks
// fire, in what order, and which ones must NOT fire on each branch. That is the
// behaviour the four sites depend on and the behaviour a Phase-3 decomposition
// must preserve; it says nothing about how the segment is structured
// internally, so a refactor that keeps the contract keeps these green.
//
// Everything here runs in virtual time (substrate.FakeClock) or synchronously.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

// hookLog records the order in which the segment invoked the site's hooks. The
// order is the contract: launch_initiated is held back until the launch
// succeeded (hk-4l7zs), and the brief is never delivered before readiness (the
// hk-kunm4 "do not paste before the REPL accepts input" invariant).
type hookLog struct {
	mu   sync.Mutex
	seen []string
}

func (l *hookLog) add(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, name)
}

func (l *hookLog) order() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.seen...)
}

func (l *hookLog) has(name string) bool {
	for _, s := range l.order() {
		if s == name {
			return true
		}
	}
	return false
}

func (l *hookLog) indexOf(name string) int {
	for i, s := range l.order() {
		if s == name {
			return i
		}
	}
	return -1
}

// charSegConfig is the fresh-launch (non-resume) dispatch policy the single-mode
// and DOT-implementer sites build. Durations are virtual.
func charSegConfig() runexec.DispatchConfig {
	return runexec.DispatchConfig{
		MaxInputAttempts: 1,
		ReadyTimeout:     30 * time.Second,
		InputAck:         DispatchSegmentInputAckWindow,
		ReadyKillReap:    10 * time.Second,
	}
}

// newCharSegment builds a segment with every hook wired to the log. Callers
// override the fields they are exercising.
func newCharSegment(t *testing.T, log *hookLog, cfg runexec.DispatchConfig) (*DispatchSegment, *segRecordingEmitter) {
	t.Helper()
	runID := segTestRunID(t)
	rec := &segRecordingEmitter{}
	tap, tapCh := NewPerRunEventTap(rec, runID)

	seg := &DispatchSegment{
		Clock:  substrate.NewFakeClock(time.Unix(0, 0)),
		RunID:  runID,
		Config: cfg,
		Tap:    tap,
		TapCh:  tapCh,
		Launch: func(context.Context) (<-chan struct{}, error) {
			log.add("Launch")
			return nil, nil
		},
		OnLaunchFailed:   func(context.Context, error) { log.add("OnLaunchFailed") },
		OnLaunched:       func(context.Context) { log.add("OnLaunched") },
		Deliver:          func(context.Context) { log.add("Deliver") },
		KillReady:        func(context.Context) { log.add("KillReady") },
		KillAbort:        func(context.Context) { log.add("KillAbort") },
		EmitReadyTimeout: func(context.Context) { log.add("EmitReadyTimeout") },
	}
	return seg, rec
}

// TestDispatchSegment_FreshLaunchDeliversBriefAfterReady pins the happy path
// every one of the four sites depends on: launch, THEN the held-back
// launch_initiated wiring, THEN — only once a readiness signal is recognized —
// the brief. The segment ends in Working, which is where the sub-driver takes
// over the completion wait.
//
// The order is load-bearing twice over. launch_initiated emitted before a
// successful launch would attribute a run that never started (hk-4l7zs); a
// brief delivered before readiness is pasted into a TUI that is not yet
// accepting input and is silently lost (hk-kunm4).
func TestDispatchSegment_FreshLaunchDeliversBriefAfterReady(t *testing.T) {
	log := &hookLog{}
	seg, _ := newCharSegment(t, log, charSegConfig())

	// A relay-synthesized agent_ready, as the production SetAgentReadyCallback
	// delivers it: emitted through the tap once the launch has been wired up.
	seg.Adapter = segStubAdapter{ready: func(env core.EventEnvelope) bool {
		return env.Type == string(core.EventTypeAgentReady)
	}}
	seg.OnLaunched = func(ctx context.Context) {
		log.add("OnLaunched")
		if err := seg.Tap.EmitWithRunID(ctx, seg.RunID, core.EventTypeAgentReady, nil); err != nil {
			t.Errorf("relay agent_ready emit: %v", err)
		}
	}

	final := seg.Run(context.Background())

	if final.Phase != runexec.DispatchWorking {
		t.Fatalf("phase = %q, want working", final.Phase)
	}
	got := log.order()
	want := []string{"Launch", "OnLaunched", "Deliver"}
	if len(got) != len(want) {
		t.Fatalf("hook order = %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hook order = %v, want %v", got, want)
		}
	}
}

// TestDispatchSegment_LaunchFailureHoldsBackLaunchInitiated pins the launch
// failure branch: the site's failure hook fires, and the launch/ready/brief
// wiring — OnLaunched, Deliver, KillReady — never runs. This is what keeps a
// spawn-cap-blocked dispatch from registering a heartbeat loop, a comms
// presence join and an agent-ready callback for an agent that does not exist.
func TestDispatchSegment_LaunchFailureHoldsBackLaunchInitiated(t *testing.T) {
	log := &hookLog{}
	seg, rec := newCharSegment(t, log, charSegConfig())
	seg.Launch = func(context.Context) (<-chan struct{}, error) {
		log.add("Launch")
		return nil, errors.New("no window available")
	}

	final := seg.Run(context.Background())

	if final.Phase != runexec.DispatchFailed {
		t.Fatalf("phase = %q, want failed", final.Phase)
	}
	if !log.has("OnLaunchFailed") {
		t.Error("the site's launch-failure hook never fired — the failure would be silent")
	}
	for _, forbidden := range []string{"OnLaunched", "Deliver", "KillReady"} {
		if log.has(forbidden) {
			t.Errorf("%s fired after a failed launch (order %v)", forbidden, log.order())
		}
	}
	// The segment must not ALSO emit a launch-failure event: the site owns that
	// emission because only the site has the rich payload. A segment that
	// emitted here would double-report every capped spawn.
	rec.mu.Lock()
	n := len(rec.calls)
	rec.mu.Unlock()
	if n != 0 {
		t.Errorf("segment emitted %d event(s) on the launch-failure path; the site owns that emission", n)
	}
}

// TestDispatchSegment_LaunchFailureReasonReachesTerminal pins that the
// classified failure reason survives into the terminal state, so the calling
// site can route on the two structural wedge classes rather than on a string
// it has to re-derive.
func TestDispatchSegment_LaunchFailureReasonReachesTerminal(t *testing.T) {
	spawnCap := errors.New("spawn cap timeout")
	tmuxWindow := errors.New("tmux new-window timed out")

	cases := []struct {
		name       string
		launchErr  error
		wantReason string
	}{
		{"spawn cap", spawnCap, string(core.EventTypeSpawnCapBlocked)},
		{"tmux new-window", tmuxWindow, string(core.EventTypeTmuxNewWindowTimeout)},
		{"anything else keeps its message", errors.New("boom"), "boom"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log := &hookLog{}
			seg, _ := newCharSegment(t, log, charSegConfig())
			seg.SpawnCapTimeout = spawnCap
			seg.TmuxNewWindowTimeout = tmuxWindow
			seg.Launch = func(context.Context) (<-chan struct{}, error) { return nil, tc.launchErr }

			final := seg.Run(context.Background())

			if final.Phase != runexec.DispatchFailed {
				t.Fatalf("phase = %q, want failed", final.Phase)
			}
			if final.Reason != tc.wantReason {
				t.Errorf("terminal reason = %q, want %q", final.Reason, tc.wantReason)
			}
		})
	}
}

// TestDispatchSegment_NoAdapterDeliversWithoutWaiting pins the adapter-less
// posture: a resolved agent type with no readiness adapter still gets its
// brief, immediately, without burning the ready window. Removing this would
// make every adapter-less dispatch sit out the full ready timeout and then be
// killed.
//
// The "without waiting" half is proved by construction rather than by an
// assertion: the segment is driven on a FakeClock this test never advances, so
// a segment that waited for readiness would never reach a terminal and the test
// would hang rather than fail. Do not add an elapsed-time assertion here — on a
// clock that cannot move it could never fail.
func TestDispatchSegment_NoAdapterDeliversWithoutWaiting(t *testing.T) {
	log := &hookLog{}
	seg, _ := newCharSegment(t, log, charSegConfig())
	seg.Adapter = nil

	final := seg.Run(context.Background())

	if final.Phase != runexec.DispatchWorking {
		t.Fatalf("phase = %q, want working", final.Phase)
	}
	if !log.has("Deliver") {
		t.Error("no brief was delivered for an adapter-less agent type")
	}
	if log.has("KillReady") {
		t.Error("the adapter-less path took the ready-timeout kill")
	}
}

// TestDispatchSegment_ProcessExitHarnessSkipsTheBriefEntirely pins the
// completion-by-process-exit posture (codex / pi): those harnesses take their
// instructions from the launch spec, not from a pasted brief, so the segment
// goes straight to Working with NO readiness handshake and NO delivery.
//
// This is the branch most likely to be lost in a decomposition that treats
// "deliver the brief" as an unconditional step — doing so would paste a brief
// into a codex process that is not a TUI.
func TestDispatchSegment_ProcessExitHarnessSkipsTheBriefEntirely(t *testing.T) {
	log := &hookLog{}
	cfg := charSegConfig()
	cfg.SkipReadyHandshake = true
	seg, _ := newCharSegment(t, log, cfg)
	seg.Adapter = nil

	final := seg.Run(context.Background())

	if final.Phase != runexec.DispatchWorking {
		t.Fatalf("phase = %q, want working", final.Phase)
	}
	if log.has("Deliver") {
		t.Error("a brief was delivered to a completion-by-process-exit harness")
	}
	if log.has("KillReady") {
		t.Error("the process-exit harness took the ready-timeout kill")
	}
	if !log.has("OnLaunched") {
		t.Error("launch_initiated wiring was skipped for the process-exit harness")
	}
}

// TestDispatchSegment_AgentExitBeforeReadyIsExitedNotFailed pins the exec-path
// crash edge: the watcher's Done channel closing before readiness settles the
// segment into Exited, carrying the exit code — NOT into the ready-timeout
// Failed terminal, and without waiting out the ready window.
//
// The distinction matters downstream: Exited routes to the wait-return
// classification (which can still find a Stop-hook outcome), whereas
// agent_ready_timeout is an unconditional reopen.
func TestDispatchSegment_AgentExitBeforeReadyIsExitedNotFailed(t *testing.T) {
	log := &hookLog{}
	seg, _ := newCharSegment(t, log, charSegConfig())
	seg.Adapter = segStubAdapter{ready: func(core.EventEnvelope) bool { return false }}

	watcherDone := make(chan struct{})
	seg.Launch = func(context.Context) (<-chan struct{}, error) {
		log.add("Launch")
		close(watcherDone) // the agent process died immediately
		return watcherDone, nil
	}

	clock, ok := seg.Clock.(*substrate.FakeClock)
	if !ok {
		t.Fatal("test harness: expected a FakeClock")
	}
	start := clock.Now()

	result := make(chan runexec.DispatchState, 1)
	go func() { result <- seg.Run(context.Background()) }()
	final := pumpUntilDone(t, clock, result)

	if final.Phase != runexec.DispatchExited {
		t.Fatalf("phase = %q, want exited", final.Phase)
	}
	if log.has("KillReady") {
		t.Error("an already-dead agent was killed on the ready-timeout edge")
	}
	if elapsed := clock.Now().Sub(start); elapsed >= charSegConfig().ReadyTimeout {
		t.Errorf("agent exit took %v to settle — the full ready window was waited out", elapsed)
	}
}

// TestDispatchSegment_CancelledContextAbortsWithoutFabricatingATimeout pins the
// shutdown edge: cancelling the run context aborts the dispatch through the
// abort kill, and does NOT emit agent_ready_timeout.
//
// Fabricating a ready timeout on every daemon shutdown would put a structural
// failure into the event stream for every in-flight run, which is exactly the
// kind of false signal the sentinel and the stale watcher act on.
func TestDispatchSegment_CancelledContextAbortsWithoutFabricatingATimeout(t *testing.T) {
	log := &hookLog{}
	seg, _ := newCharSegment(t, log, charSegConfig())
	seg.Adapter = segStubAdapter{ready: func(core.EventEnvelope) bool { return false }}

	ctx, cancel := context.WithCancel(context.Background())
	seg.OnLaunched = func(context.Context) {
		log.add("OnLaunched")
		cancel() // daemon shutdown lands while awaiting readiness
	}

	final := seg.Run(ctx)

	if final.Phase != runexec.DispatchAborted {
		t.Fatalf("phase = %q, want aborted", final.Phase)
	}
	if !log.has("KillAbort") {
		t.Error("the abort kill never fired — the agent would be orphaned")
	}
	if log.has("KillReady") {
		t.Error("the abort took the ready-timeout kill path")
	}
	if log.has("EmitReadyTimeout") {
		t.Error("shutdown fabricated an agent_ready_timeout")
	}
}

// TestDispatchSegment_ReadyTimeoutKillsBeforeEmitting pins the ordering inside
// the ready-timeout edge: the kill-and-reap completes BEFORE
// agent_ready_timeout is emitted. Anything watching the stream — the stale
// watcher, the reconciler — reads the emission as "this agent is already
// gone"; emitting first would make that false for the length of the reap.
func TestDispatchSegment_ReadyTimeoutKillsBeforeEmitting(t *testing.T) {
	log := &hookLog{}
	seg, _ := newCharSegment(t, log, charSegConfig())
	seg.Adapter = segStubAdapter{ready: func(core.EventEnvelope) bool { return false }}

	clock, ok := seg.Clock.(*substrate.FakeClock)
	if !ok {
		t.Fatal("test harness: expected a FakeClock")
	}

	result := make(chan runexec.DispatchState, 1)
	go func() { result <- seg.Run(context.Background()) }()
	final := pumpUntilDone(t, clock, result)

	if final.Phase != runexec.DispatchFailed || final.Reason != "agent_ready_timeout" {
		t.Fatalf("terminal = %q/%q, want failed/agent_ready_timeout", final.Phase, final.Reason)
	}
	killIdx, emitIdx := log.indexOf("KillReady"), log.indexOf("EmitReadyTimeout")
	if killIdx < 0 || emitIdx < 0 {
		t.Fatalf("hook order = %v, want both KillReady and EmitReadyTimeout", log.order())
	}
	if killIdx > emitIdx {
		t.Errorf("hook order = %v, want the kill+reap to complete before the emission", log.order())
	}
}

// TestDispatchSegment_UnsetReadyTimeoutHookSuppressesTheEmission pins the
// reviewer posture: a site that supplies no EmitReadyTimeout hook still gets
// the kill and still reaches the Failed terminal — only the emission is
// suppressed. The reviewer phase relies on this to keep its own event
// vocabulary; a segment that emitted unconditionally would change the reviewer
// stream.
func TestDispatchSegment_UnsetReadyTimeoutHookSuppressesTheEmission(t *testing.T) {
	log := &hookLog{}
	seg, rec := newCharSegment(t, log, charSegConfig())
	seg.Adapter = segStubAdapter{ready: func(core.EventEnvelope) bool { return false }}
	seg.EmitReadyTimeout = nil

	clock, ok := seg.Clock.(*substrate.FakeClock)
	if !ok {
		t.Fatal("test harness: expected a FakeClock")
	}

	result := make(chan runexec.DispatchState, 1)
	go func() { result <- seg.Run(context.Background()) }()
	final := pumpUntilDone(t, clock, result)

	if final.Phase != runexec.DispatchFailed || final.Reason != "agent_ready_timeout" {
		t.Fatalf("terminal = %q/%q, want failed/agent_ready_timeout", final.Phase, final.Reason)
	}
	if !log.has("KillReady") {
		t.Error("suppressing the emission also suppressed the kill — the agent would be orphaned")
	}
	for _, c := range rec.callTypes() {
		if c == core.EventTypeAgentReadyTimeout {
			t.Error("agent_ready_timeout was emitted despite the site supplying no hook")
		}
	}
}

// callTypes returns every event type the recording emitter observed.
func (e *segRecordingEmitter) callTypes() []core.EventType {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]core.EventType, 0, len(e.calls))
	for _, c := range e.calls {
		out = append(out, c.typ)
	}
	return out
}
