package codexinput_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ready drives a fresh reactor through Spawned + HandshakeOK to the Ready phase,
// returning it for a focused transition test.
func ready(t *testing.T) *codexinput.Reactor {
	t.Helper()
	r := codexinput.New(codexinput.Config{})
	if got := r.Step(codexinput.Event{Type: codexinput.EventTypeSpawned}); len(got) != 2 {
		t.Fatalf("Spawned: want 2 actions (handshake+arm), got %d: %+v", len(got), got)
	}
	if r.State().Phase != codexinput.Handshaking {
		t.Fatalf("after Spawned: want Handshaking, got %s", r.State().Phase)
	}
	r.Step(codexinput.Event{Type: codexinput.EventTypeHandshakeOK})
	if r.State().Phase != codexinput.Ready {
		t.Fatalf("after HandshakeOK: want Ready, got %s", r.State().Phase)
	}
	return r
}

// TestStep_SubmitToAck walks the happy path: submit → acked.
func TestStep_SubmitToAck(t *testing.T) {
	r := ready(t)

	got := r.Step(codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 7})
	want := []codexinput.Action{
		{Type: codexinput.ActionTypeWriteInput, InputSeq: 7},
		{Type: codexinput.ActionTypeEmit, Emit: codexinput.EmitInputSubmitted, InputSeq: 7},
		{Type: codexinput.ActionTypeArmTimer, Kind: codexinput.TimerInputAck, Duration: 60 * time.Second},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("submit actions:\n got %+v\nwant %+v", got, want)
	}
	if r.State().Phase != codexinput.AwaitingAck || r.State().PendingSeq != 7 {
		t.Fatalf("after submit: want AwaitingAck/pending=7, got %s/%d", r.State().Phase, r.State().PendingSeq)
	}

	got = r.Step(codexinput.Event{Type: codexinput.EventTypeInputAcked, InputSeq: 7, TurnID: "turn-1"})
	want = []codexinput.Action{
		{Type: codexinput.ActionTypeCancelTimer, Kind: codexinput.TimerInputAck},
		{Type: codexinput.ActionTypeEmit, Emit: codexinput.EmitInputAcked, InputSeq: 7, TurnID: "turn-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ack actions:\n got %+v\nwant %+v", got, want)
	}
	if r.State().Phase != codexinput.InTurn || r.State().TurnID != "turn-1" {
		t.Fatalf("after ack: want InTurn/turn-1, got %s/%q", r.State().Phase, r.State().TurnID)
	}
}

// TestStep_Rejected resolves a synchronous Ack{Rejected} with no positive event.
func TestStep_Rejected(t *testing.T) {
	r := ready(t)
	r.Step(codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 3})
	got := r.Step(codexinput.Event{Type: codexinput.EventTypeInputRejected, InputSeq: 3, Reason: "bad"})
	want := []codexinput.Action{{Type: codexinput.ActionTypeCancelTimer, Kind: codexinput.TimerInputAck}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reject actions:\n got %+v\nwant %+v", got, want)
	}
	for _, a := range got {
		if a.Type == codexinput.ActionTypeEmit && a.Emit == codexinput.EmitInputAcked {
			t.Fatalf("reject must NOT emit a positive ack")
		}
	}
	if r.State().Phase != codexinput.Ready || r.State().PendingSeq != 0 {
		t.Fatalf("after reject: want Ready/pending=0, got %s/%d", r.State().Phase, r.State().PendingSeq)
	}
}

// TestStep_TimerFiredAlwaysActs is the T4 structural invariant: every TimerFired
// edge reachable in the machine lands in a state with an outgoing action — no
// silent wedge (AIS-INV-001).
func TestStep_TimerFiredAlwaysActs(t *testing.T) {
	// Handshake-timeout edge (Handshaking): launch failure.
	r := codexinput.New(codexinput.Config{})
	r.Step(codexinput.Event{Type: codexinput.EventTypeSpawned})
	got := r.Step(codexinput.Event{Type: codexinput.EventTypeTimerFired, Kind: codexinput.TimerHandshake})
	if len(got) == 0 || got[0].Emit != codexinput.EmitLaunchFailure {
		t.Fatalf("handshake TimerFired must emit launch_failure, got %+v", got)
	}
	if r.State().Phase != codexinput.Exited {
		t.Fatalf("handshake timeout must terminate, got %s", r.State().Phase)
	}

	// Input-ack-timeout edge (AwaitingAck): stale.
	r = ready(t)
	r.Step(codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 5})
	got = r.Step(codexinput.Event{Type: codexinput.EventTypeTimerFired, Kind: codexinput.TimerInputAck})
	if len(got) != 1 || got[0].Emit != codexinput.EmitInputStale || got[0].InputSeq != 5 {
		t.Fatalf("input-ack TimerFired must emit agent_input_stale{seq=5}, got %+v", got)
	}
	if r.State().Phase != codexinput.Ready || r.State().PendingSeq != 0 {
		t.Fatalf("after stale: want Ready/pending=0, got %s/%d", r.State().Phase, r.State().PendingSeq)
	}
}

// TestStep_TransportTerminalWhilePending: a disconnect while AwaitingAck resolves
// the pending submission to agent_input_stale (never silence).
func TestStep_TransportTerminalWhilePending(t *testing.T) {
	r := ready(t)
	r.Step(codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 9})
	got := r.Step(codexinput.Event{Type: codexinput.EventTypeDisconnected})
	var sawStale bool
	for _, a := range got {
		if a.Type == codexinput.ActionTypeEmit && a.Emit == codexinput.EmitInputStale && a.InputSeq == 9 {
			sawStale = true
		}
	}
	if !sawStale {
		t.Fatalf("disconnect while pending must emit agent_input_stale{seq=9}, got %+v", got)
	}
	if r.State().Phase != codexinput.Exited {
		t.Fatalf("disconnect must exit, got %s", r.State().Phase)
	}
}

// TestStep_CloseWhileAwaitingAck: a CloseInput that interleaves while a
// submission is still AwaitingAck must resolve the pending seq to
// agent_input_stale immediately — the Draining phase drops the guarded timer
// fire, so silence here would be an unrecoverable resume-hang (AIS-INV-001).
func TestStep_CloseWhileAwaitingAck(t *testing.T) {
	r := ready(t)
	r.Step(codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 11})
	if r.State().Phase != codexinput.AwaitingAck {
		t.Fatalf("after submit: want AwaitingAck, got %s", r.State().Phase)
	}
	got := r.Step(codexinput.Event{Type: codexinput.EventTypeCloseRequested})
	var sawStale, sawClose bool
	for _, a := range got {
		if a.Type == codexinput.ActionTypeEmit && a.Emit == codexinput.EmitInputStale && a.InputSeq == 11 {
			sawStale = true
		}
		if a.Type == codexinput.ActionTypeCloseInput {
			sawClose = true
		}
	}
	if !sawStale {
		t.Fatalf("close while AwaitingAck must emit agent_input_stale{seq=11}, got %+v", got)
	}
	if !sawClose {
		t.Fatalf("close while AwaitingAck must still close stdin, got %+v", got)
	}
	if r.State().PendingSeq != 0 {
		t.Fatalf("pending seq must be cleared after close-resolve, got %d", r.State().PendingSeq)
	}
	// A late timer fire is now a no-op (already resolved) — never a second terminal.
	if late := r.Step(codexinput.Event{Type: codexinput.EventTypeTimerFired, Kind: codexinput.TimerInputAck}); late != nil {
		t.Fatalf("late timer after close-resolve must be nil, got %+v", late)
	}
}

// TestStep_Purity: Step is deterministic and side-effect-free — the same
// (state, event) yields identical actions across two independent reactors, and
// no-op events return nil (not an empty slice).
func TestStep_Purity(t *testing.T) {
	a, b := ready(t), ready(t)
	ev := codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 1}
	if !reflect.DeepEqual(a.Step(ev), b.Step(ev)) {
		t.Fatal("Step is not deterministic across reactors")
	}
	if got := a.Step(codexinput.Event{Type: codexinput.EventTypeDelta, TurnID: "x"}); got != nil {
		t.Fatalf("Delta must return nil (not empty slice), got %+v", got)
	}
}

// TestEventActionRoundTrip: the flat vocab is JSON-round-trippable (scenario
// files, corpus).
func TestEventActionRoundTrip(t *testing.T) {
	ev := codexinput.Event{Type: codexinput.EventTypeInputAcked, InputSeq: 42, TurnID: "t"}
	var ev2 codexinput.Event
	if b, err := json.Marshal(ev); err != nil {
		t.Fatal(err)
	} else if err := json.Unmarshal(b, &ev2); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ev, ev2) {
		t.Fatalf("event round-trip: got %+v want %+v", ev2, ev)
	}
}

// ─── FakeClock bounded-liveness proof (T4 / AIS-INV-001) ──────────────────────

// chanSource is a dynamic EventSource: the test and the timer effector feed
// events into `in`; Events relays them to Run and closes on ctx cancel.
type chanSource struct{ in chan codexinput.Event }

func (s *chanSource) Events(ctx context.Context) <-chan codexinput.Event {
	out := make(chan codexinput.Event)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-s.in:
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out
}

// timerEffector translates ArmTimer/CancelTimer into ClockPort sleeps (RS-015):
// on ArmTimer it sleeps on the FakeClock and, if the full window elapses, feeds
// a TimerFired event back in. It records Emit actions for assertions.
type timerEffector struct {
	mu    sync.Mutex
	clock *substrate.FakeClock
	out   chan codexinput.Event
	//nolint:containedctx // test double: parent ctx scopes the arm/cancel timer goroutine lifecycle across Execute calls
	parent  context.Context
	emits   []codexinput.Action
	armed   map[codexinput.TimerKind]bool
	cancels map[codexinput.TimerKind]context.CancelFunc
}

//nolint:contextcheck // test double derives timers from the stored parent ctx (not the Execute arg) so a later CancelTimer call can cancel across Execute calls
func (e *timerEffector) Execute(_ context.Context, a codexinput.Action) error {
	switch a.Type {
	case codexinput.ActionTypeArmTimer:
		ctx, cancel := context.WithCancel(e.parent)
		kind, d := a.Kind, a.Duration
		e.mu.Lock()
		e.armed[kind] = true
		e.cancels[kind] = cancel
		e.mu.Unlock()
		go func() {
			fired := e.clock.Sleep(ctx, d) // window measured via ClockPort
			e.mu.Lock()
			e.armed[kind] = false
			e.mu.Unlock()
			if fired {
				select {
				case e.out <- codexinput.Event{Type: codexinput.EventTypeTimerFired, Kind: kind}:
				case <-e.parent.Done():
				}
			}
		}()
	case codexinput.ActionTypeCancelTimer:
		e.mu.Lock()
		if c := e.cancels[a.Kind]; c != nil {
			c()
			e.armed[a.Kind] = false
		}
		e.mu.Unlock()
	case codexinput.ActionTypeEmit:
		e.mu.Lock()
		e.emits = append(e.emits, a)
		e.mu.Unlock()
	default:
		// SendHandshake/WriteInput/CloseInput/Interrupt: no side effect the
		// bounded-liveness timer tests assert on.
	}
	return nil
}

func (e *timerEffector) waitArmed(k codexinput.TimerKind) {
	for {
		e.mu.Lock()
		ok := e.armed[k]
		e.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

func (e *timerEffector) waitEmit(t *testing.T, want codexinput.EmitType) codexinput.Action {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		for _, a := range e.emits {
			if a.Emit == want {
				e.mu.Unlock()
				return a
			}
		}
		e.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for emit %q; got %+v", want, e.emits)
	return codexinput.Action{}
}

// TestBoundedLiveness_StaleFiresPastWindow proves AIS-INV-001 with virtual time:
// an unacked submission fires agent_input_stale exactly when the FakeClock is
// advanced past InputAckTimeout — and NOT before (BlockUntil-style arm-before-
// advance discipline via waitArmed). Timing rides the injected ClockPort, never
// wall-clock.
func TestBoundedLiveness_StaleFiresPastWindow(t *testing.T) {
	const window = 60 * time.Second
	clock := substrate.NewFakeClock(time.Unix(0, 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &chanSource{in: make(chan codexinput.Event, 16)}
	eff := &timerEffector{
		clock:   clock,
		out:     src.in,
		parent:  ctx,
		armed:   map[codexinput.TimerKind]bool{},
		cancels: map[codexinput.TimerKind]context.CancelFunc{},
	}
	r := codexinput.New(codexinput.Config{InputAckTimeout: window, HandshakeTimeout: 30 * time.Second})

	runDone := make(chan error, 1)
	go func() { runDone <- r.Run(ctx, src, eff) }()

	// Drive to a pending submission.
	src.in <- codexinput.Event{Type: codexinput.EventTypeSpawned}
	src.in <- codexinput.Event{Type: codexinput.EventTypeHandshakeOK}
	src.in <- codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 1}

	// Arm-before-advance: wait until the input-ack timer is live on the clock.
	eff.waitArmed(codexinput.TimerInputAck)

	// Just before the window: nothing stale yet.
	clock.Advance(window - time.Nanosecond)
	eff.mu.Lock()
	for _, a := range eff.emits {
		if a.Emit == codexinput.EmitInputStale {
			eff.mu.Unlock()
			t.Fatalf("agent_input_stale fired BEFORE the window elapsed")
		}
	}
	eff.mu.Unlock()

	// Past the window: exactly one agent_input_stale for seq 1, no positive ack.
	clock.Advance(2 * time.Nanosecond)
	stale := eff.waitEmit(t, codexinput.EmitInputStale)
	if stale.InputSeq != 1 {
		t.Fatalf("stale for wrong seq: %+v", stale)
	}
	eff.mu.Lock()
	for _, a := range eff.emits {
		if a.Emit == codexinput.EmitInputAcked {
			eff.mu.Unlock()
			t.Fatalf("must NOT emit a positive ack on the timeout path")
		}
	}
	eff.mu.Unlock()

	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
