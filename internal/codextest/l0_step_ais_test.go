package codextest_test

// L0 tier for the INPUT driver (T9): the pure-Step boundary that the whole
// sentinel-ignore harness depends on, plus the twin's fault-event shapes.

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

// aisReady drives a fresh reactor to a pending submission (AwaitingAck).
func aisReady(t *testing.T) *codexinput.Reactor {
	t.Helper()
	r := codexinput.New(aisConfig())
	r.Step(codexinput.Event{Type: codexinput.EventTypeSpawned})
	r.Step(codexinput.Event{Type: codexinput.EventTypeHandshakeOK})
	r.Step(codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: 1})
	if r.State().Phase != codexinput.AwaitingAck {
		t.Fatalf("setup: want AwaitingAck, got %s", r.State().Phase)
	}
	return r
}

// TestL0AIS_SentinelsIgnoredByStep is the load-bearing invariant of the path-A
// idiom: the twin's fault sentinels are NOT in the reactor's Step switch, so Step
// drops them (returns nil) and leaves the submission in flight — the reactor then
// reaches its OWN input_ack_timeout terminal, never a sentinel-driven one. If
// this ever regresses (e.g. the reactor grows a case for these kinds), the whole
// bounded-liveness accounting shifts, so it is asserted directly.
func TestL0AIS_SentinelsIgnoredByStep(t *testing.T) {
	t.Parallel()
	for _, kind := range []codexinput.EventType{
		codexdigitaltwin.EvTwinTransportError,
		codexdigitaltwin.EvTwinDisconnected,
	} {
		r := aisReady(t)
		if acts := r.Step(codexinput.Event{Type: kind}); acts != nil {
			t.Errorf("Step(%s) = %+v, want nil (sentinel must be ignored)", kind, acts)
		}
		if r.State().Phase != codexinput.AwaitingAck {
			t.Errorf("Step(%s) changed phase to %s, want AwaitingAck (submission still in flight)", kind, r.State().Phase)
		}
	}
}

// TestL0AIS_TwinFaultEventShapes asserts the twin materializes the correct
// sentinel event for each stream-ending fault mode.
func TestL0AIS_TwinFaultEventShapes(t *testing.T) {
	t.Parallel()
	stimulus, err := codexdigitaltwin.SynthesizeInputStimulus(codexdigitaltwin.StratumAcked)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	raw, err := codexdigitaltwin.EncodeInputStimulus(stimulus)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	drain := func(fault codexdigitaltwin.FaultConfig) []codexinput.Event {
		twin := codexdigitaltwin.NewInputTwin(bytes.NewReader(raw), fault)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var out []codexinput.Event
		for ev := range twin.Events(ctx) {
			out = append(out, ev)
		}
		return out
	}

	// FaultTruncate@3 replaces event 3 with the transport-error sentinel.
	got := drain(codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultTruncate, EventN: 3})
	if len(got) != 3 || got[2].Type != codexdigitaltwin.EvTwinTransportError {
		t.Fatalf("truncate: last event = %+v, want twin_transport_error at index 2 (got %d events)", lastOr(got), len(got))
	}

	// FaultDropAfter@3 delivers 3 then the disconnect sentinel.
	got = drain(codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultDropAfter, EventN: 3})
	if len(got) != 4 || got[3].Type != codexdigitaltwin.EvTwinDisconnected {
		t.Fatalf("drop_after: events = %d, last = %+v, want twin_disconnected at index 3", len(got), lastOr(got))
	}
}

func lastOr(evs []codexinput.Event) codexinput.Event {
	if len(evs) == 0 {
		return codexinput.Event{}
	}
	return evs[len(evs)-1]
}
