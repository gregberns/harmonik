package codextest_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

func aisConfig() codexinput.Config {
	return codexinput.Config{
		HandshakeTimeout: 30 * time.Second,
		InputAckTimeout:  60 * time.Second,
	}
}

func aisVirtualBound(cfg codexinput.Config) time.Duration {
	return cfg.HandshakeTimeout + cfg.InputAckTimeout + time.Minute
}

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
	}
}

func (s *InputBridgeSink) emitCount(want codexinput.EmitType) int {
	n := 0
	for _, a := range s.Emits {
		if a.Emit == want {
			n++
		}
	}
	return n
}

func (s *InputBridgeSink) firstEmit(want codexinput.EmitType) (codexinput.Action, bool) {
	for _, a := range s.Emits {
		if a.Emit == want {
			return a, true
		}
	}
	return codexinput.Action{}, false
}

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

	for _, ev := range stimuli {
		feed(ev)
	}

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

	if ph := r.State().Phase; ph == codexinput.AwaitingAck || ph == codexinput.Handshaking {
		t.Fatalf("%s: SILENCE — reactor left in phase %s with no stimulus and no armed timer", stratum, ph)
	}

	return sink, now.Sub(start)
}
