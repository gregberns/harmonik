package codextest_test

// The AIS-INV-001 bounded-liveness conformance ORACLE (T9;
// harness-acceptance-design §"Bounded-liveness oracle"). Unlike the discrete
// matrix (a manual virtual cursor), this test drives the REAL substrate.Run loop
// over an injected FakeClock: it arms the driver ack-timeout, waits until the
// reactor's sleep is live on the clock (arm-before-advance — the BlockUntil
// idiom, avoiding the advance-before-arm race), advances virtual time across the
// window, and asserts exactly one agent_input_stale fired past it and NOT before.
// Timing rides the injected ClockPort; there is no wall-clock wait for the window.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/substrate"
)

// aisChanSource is a dynamic EventSource: the test and the timer effector feed
// events into `in`; Events relays them to Run and closes on ctx cancel.
type aisChanSource struct{ in chan codexinput.Event }

func (s *aisChanSource) Events(ctx context.Context) <-chan codexinput.Event {
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

// aisTimerEffector translates ArmTimer/CancelTimer into ClockPort sleeps
// (RS-015): on ArmTimer it sleeps on the FakeClock and, when the full window
// elapses, feeds a TimerFired event back in. It records Emit actions.
type aisTimerEffector struct {
	mu    sync.Mutex
	clock *substrate.FakeClock
	out   chan codexinput.Event
	//nolint:containedctx // test double: parent ctx scopes the timer goroutine lifecycle across Execute calls
	parent  context.Context
	emits   []codexinput.Action
	armed   map[codexinput.TimerKind]bool
	cancels map[codexinput.TimerKind]context.CancelFunc
}

//nolint:contextcheck // test double derives timers from the stored parent ctx so a later CancelTimer can cancel across Execute calls
func (e *aisTimerEffector) Execute(_ context.Context, a codexinput.Action) error {
	switch a.Type {
	case codexinput.ActionTypeArmTimer:
		ctx, cancel := context.WithCancel(e.parent)
		kind, d := a.Kind, a.Duration
		e.mu.Lock()
		e.armed[kind] = true
		e.cancels[kind] = cancel
		e.mu.Unlock()
		go func() {
			fired := e.clock.Sleep(ctx, d)
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
		// SendHandshake/WriteInput/CloseInput/Interrupt: no bounded-liveness effect.
	}
	return nil
}

func (e *aisTimerEffector) waitArmed(k codexinput.TimerKind) {
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

func (e *aisTimerEffector) waitEmit(t *testing.T, want codexinput.EmitType) codexinput.Action {
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

func (e *aisTimerEffector) hasEmit(want codexinput.EmitType) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, a := range e.emits {
		if a.Emit == want {
			return true
		}
	}
	return false
}

// TestAIS_BoundedLiveness_StaleFiresPastWindow proves AIS-INV-001 in virtual
// time: an unacked submission fires agent_input_stale exactly when the FakeClock
// is advanced past InputAckTimeout — and NOT before.
func TestAIS_BoundedLiveness_StaleFiresPastWindow(t *testing.T) {
	t.Parallel()
	const window = 60 * time.Second
	clock := substrate.NewFakeClock(time.Unix(0, 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &aisChanSource{in: make(chan codexinput.Event, 16)}
	eff := &aisTimerEffector{
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

	// Arm-before-advance: wait until the ack timer is live on the clock.
	eff.waitArmed(codexinput.TimerInputAck)

	// Just before the window: nothing stale yet.
	clock.Advance(window - time.Nanosecond)
	if eff.hasEmit(codexinput.EmitInputStale) {
		t.Fatal("agent_input_stale fired BEFORE the window elapsed")
	}

	// Past the window: exactly one agent_input_stale for seq 1, no positive ack.
	clock.Advance(2 * time.Nanosecond)
	stale := eff.waitEmit(t, codexinput.EmitInputStale)
	if stale.InputSeq != 1 {
		t.Fatalf("stale for wrong seq: %+v", stale)
	}
	if eff.hasEmit(codexinput.EmitInputAcked) {
		t.Fatal("must NOT emit a positive ack on the timeout path")
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestAIS_BoundedLiveness_HandshakeFastFail proves the AIS-017 handshake edge is
// bounded the same way: no handshake within the window → agent_launch_failure,
// never a silent exit-0.
func TestAIS_BoundedLiveness_HandshakeFastFail(t *testing.T) {
	t.Parallel()
	const window = 30 * time.Second
	clock := substrate.NewFakeClock(time.Unix(0, 0))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &aisChanSource{in: make(chan codexinput.Event, 16)}
	eff := &aisTimerEffector{
		clock:   clock,
		out:     src.in,
		parent:  ctx,
		armed:   map[codexinput.TimerKind]bool{},
		cancels: map[codexinput.TimerKind]context.CancelFunc{},
	}
	r := codexinput.New(codexinput.Config{InputAckTimeout: 60 * time.Second, HandshakeTimeout: window})

	runDone := make(chan error, 1)
	go func() { runDone <- r.Run(ctx, src, eff) }()

	src.in <- codexinput.Event{Type: codexinput.EventTypeSpawned}
	eff.waitArmed(codexinput.TimerHandshake)

	clock.Advance(window - time.Nanosecond)
	if eff.hasEmit(codexinput.EmitLaunchFailure) {
		t.Fatal("agent_launch_failure fired BEFORE the handshake window elapsed")
	}
	clock.Advance(2 * time.Nanosecond)
	eff.waitEmit(t, codexinput.EmitLaunchFailure)

	cancel()
	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
