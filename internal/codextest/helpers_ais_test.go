package codextest_test

// Shared helpers for the INPUT-driver (codexinput) L0–L3 tiers of the T9
// agent-input-substrate acceptance harness. These live alongside the pre-M2
// OUTPUT-reactor codextest files (l0_wire/l1_contract/l2_integration/l3_live,
// hk-oe86p); all input-harness symbols are `ais`-prefixed to avoid collision.
//
// The §2.2 DISCRETE-EVENT harness (runInputDiscrete) mirrors keepertest's
// runDiscrete: the synthesizer emits NO TimerFired lines; the harness arms
// virtual-time timers from the reactor's own ArmTimer actions and fires them
// once the external stimulus is exhausted. Silence (a submission left with no
// terminal) is converted into an explicit failure, never a hang (AIS-INV-001).

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

// aisConfig is the explicit-scalar reactor config for replay (short, definite
// virtual windows; the harness never waits wall-clock for them).
func aisConfig() codexinput.Config {
	return codexinput.Config{
		HandshakeTimeout: 30 * time.Second,
		InputAckTimeout:  60 * time.Second,
	}
}

// aisVirtualBound is the AIS-INV-001 bounded window: every submission (or the
// handshake) must reach its terminal within HandshakeTimeout + InputAckTimeout +
// injection overhead, measured in virtual time.
func aisVirtualBound(cfg codexinput.Config) time.Duration {
	return cfg.HandshakeTimeout + cfg.InputAckTimeout + time.Minute
}

// ─── InputBridgeSink ─────────────────────────────────────────────────────────

// InputBridgeSink records what the real driver effector would drive for the
// non-timer actions (ArmTimer/CancelTimer are intercepted by the harness before
// the sink). It owns no assertion logic — the graded artifact, not the grader
// (RS-020, D13).
type InputBridgeSink struct {
	Emits      []codexinput.Action // ActionTypeEmit actions, in order
	WriteInput int
	CloseInput int
	Interrupts int
	Handshakes int
}

func (s *InputBridgeSink) exec(a codexinput.Action) {
	switch a.Type {
	case codexinput.ActionTypeEmit:
		s.Emits = append(s.Emits, a)
	case codexinput.ActionTypeWriteInput:
		s.WriteInput++
	case codexinput.ActionTypeCloseInput:
		s.CloseInput++
	case codexinput.ActionTypeInterrupt:
		s.Interrupts++
	case codexinput.ActionTypeSendHandshake:
		s.Handshakes++
	default:
		// ArmTimer/CancelTimer are intercepted by the harness before the sink.
	}
}

// emitCount counts emitted actions carrying the given cross-bus event name.
func (s *InputBridgeSink) emitCount(want codexinput.EmitType) int {
	n := 0
	for _, a := range s.Emits {
		if a.Emit == want {
			n++
		}
	}
	return n
}

// firstEmit returns the first emit action with the given name (or false).
func (s *InputBridgeSink) firstEmit(want codexinput.EmitType) (codexinput.Action, bool) {
	for _, a := range s.Emits {
		if a.Emit == want {
			return a, true
		}
	}
	return codexinput.Action{}, false
}

// ─── the §2.2 discrete-event harness ─────────────────────────────────────────

// drainInputTwin materializes the twin's decoded stimulus stream. For FaultStall
// the twin blocks forever by design, so a generous per-receive idle timeout
// converts the stall into end-of-stimulus (harness liveness plumbing, never a
// golden). For every other mode the channel closes; hitting the timeout there is
// reported as a silence failure.
func drainInputTwin(t *testing.T, twin *codexdigitaltwin.InputTwin, stallExpected bool) []codexinput.Event {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := twin.Events(ctx)

	var out []codexinput.Event
	for {
		idle := time.NewTimer(2 * time.Second)
		select {
		case ev, ok := <-ch:
			idle.Stop()
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-idle.C:
			if !stallExpected {
				t.Fatalf("twin produced no event within the idle budget (silence bug?); got %d events", len(out))
			}
			cancel()
			for range ch { // drain so the twin goroutine exits
			}
			return out
		}
	}
}

// runInputDiscrete replays one stratum through the discrete-event harness and
// returns the sink plus the total VIRTUAL time elapsed. It FAILS the test if the
// reactor is left in a bounded-liveness-bearing phase (AwaitingAck / Handshaking)
// once the stimulus AND every armed timer are exhausted — that is the silence
// bug (AIS-INV-001), converted into an explicit failure, never a hang.
func runInputDiscrete(t *testing.T, stratum codexdigitaltwin.InputStratum, fault codexdigitaltwin.FaultConfig, stallExpected bool) (*InputBridgeSink, time.Duration) {
	t.Helper()

	events, err := codexdigitaltwin.SynthesizeInputStimulus(stratum)
	if err != nil {
		t.Fatalf("synthesize %s: %v", stratum, err)
	}
	raw, err := codexdigitaltwin.EncodeInputStimulus(events)
	if err != nil {
		t.Fatalf("encode %s: %v", stratum, err)
	}

	twin := codexdigitaltwin.NewInputTwin(bytes.NewReader(raw), fault)
	stimuli := drainInputTwin(t, twin, stallExpected)

	cfg := aisConfig()
	r := codexinput.New(cfg)
	sink := &InputBridgeSink{}

	// Virtual-time cursor + harness-owned timer registry. Deadlines anchor at
	// ArmTimer EXECUTION time (t0 for these immediate stimuli).
	var now time.Time // zero epoch
	start := now
	timers := map[codexinput.TimerKind]time.Time{}

	feed := func(ev codexinput.Event) {
		for _, a := range r.Step(ev) {
			switch a.Type {
			case codexinput.ActionTypeArmTimer:
				timers[a.Kind] = now.Add(a.Duration)
			case codexinput.ActionTypeCancelTimer:
				delete(timers, a.Kind)
			default:
				sink.exec(a)
			}
		}
	}

	// Deliver every external stimulus (all at t0 — immediate arrivals).
	for _, ev := range stimuli {
		feed(ev)
	}

	// Fire armed timers in deadline order until none remain. A fire may resolve
	// (cancel) or arm no further timers for these strata.
	for steps := 0; len(timers) > 0; steps++ {
		if steps > 100_000 {
			t.Fatalf("%s: discrete-event livelock (>100k timer steps)", stratum)
		}
		var bestK codexinput.TimerKind
		var bestT time.Time
		found := false
		for k, dl := range timers {
			if !found || dl.Before(bestT) {
				bestK, bestT, found = k, dl, true
			}
		}
		now = bestT
		delete(timers, bestK)
		feed(codexinput.Event{Type: codexinput.EventTypeTimerFired, Kind: bestK})
	}

	// Silence check: the reactor must never be left awaiting a terminal that no
	// longer has an armed timer to fire it (AIS-INV-001).
	if ph := r.State().Phase; ph == codexinput.AwaitingAck || ph == codexinput.Handshaking {
		t.Fatalf("%s: SILENCE — reactor left in phase %s with no stimulus and no armed timer", stratum, ph)
	}

	return sink, now.Sub(start)
}
