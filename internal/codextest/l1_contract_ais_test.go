package codextest_test

// L1 contract tier for the INPUT driver (T9): the synthesized stimulus
// round-trips through the InputTwin codec, and the twin drives the reactor's
// full expected action sequence on the happy path (no fault) via the real
// substrate.Run seam.

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/substrate"
)

// TestL1AIS_StimulusRoundTrip — every stratum's synthesized schedule survives
// encode → InputTwin decode unchanged (no fault).
func TestL1AIS_StimulusRoundTrip(t *testing.T) {
	t.Parallel()
	for _, stratum := range codexdigitaltwin.AllInputStrata {
		events, err := codexdigitaltwin.SynthesizeInputStimulus(stratum)
		if err != nil {
			t.Fatalf("synthesize %s: %v", stratum, err)
		}
		raw, err := codexdigitaltwin.EncodeInputStimulus(events)
		if err != nil {
			t.Fatalf("encode %s: %v", stratum, err)
		}
		twin := codexdigitaltwin.NewInputTwin(bytes.NewReader(raw), codexdigitaltwin.FaultConfig{})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var got []codexinput.Event
		for ev := range twin.Events(ctx) {
			got = append(got, ev)
		}
		cancel()

		if len(got) != len(events) {
			t.Fatalf("%s: round-trip produced %d events, want %d", stratum, len(got), len(events))
		}
		for i := range events {
			if got[i] != events[i] {
				t.Fatalf("%s event %d: round-trip %+v != %+v", stratum, i, got[i], events[i])
			}
		}
	}
}

// TestL1AIS_TwinProducesExpectedActions — the happy stratum through the real
// substrate.Run loop produces the full expected action sequence, ending in the
// positive agent_input_acked emit (no timer fires on this path).
func TestL1AIS_TwinProducesExpectedActions(t *testing.T) {
	t.Parallel()
	events, err := codexdigitaltwin.SynthesizeInputStimulus(codexdigitaltwin.StratumAcked)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	raw, err := codexdigitaltwin.EncodeInputStimulus(events)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	twin := codexdigitaltwin.NewInputTwin(bytes.NewReader(raw), codexdigitaltwin.FaultConfig{})
	r := codexinput.New(aisConfig())
	eff := &substrate.FakeEffector[codexinput.Action]{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Run(ctx, twin, eff); err != nil {
		t.Fatalf("run: %v", err)
	}

	var emits []codexinput.EmitType
	var arms, cancels int
	for _, a := range eff.Actions() {
		switch a.Type {
		case codexinput.ActionTypeEmit:
			emits = append(emits, a.Emit)
		case codexinput.ActionTypeArmTimer:
			arms++
		case codexinput.ActionTypeCancelTimer:
			cancels++
		default:
			// WriteInput/SendHandshake/CloseInput/Interrupt: not asserted here.
		}
	}
	// submitted then acked, in that order.
	if len(emits) != 2 || emits[0] != codexinput.EmitInputSubmitted || emits[1] != codexinput.EmitInputAcked {
		t.Fatalf("emit sequence = %v, want [agent_input_submitted agent_input_acked]", emits)
	}
	// handshake + input-ack timers both armed and both cancelled (nothing leaks).
	if arms != 2 || cancels != 2 {
		t.Fatalf("timers armed=%d cancelled=%d, want 2 and 2 (no leak)", arms, cancels)
	}
	if r.State().Phase != codexinput.Ready {
		t.Fatalf("final phase = %s, want ready (turn completed)", r.State().Phase)
	}
}
