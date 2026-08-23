package codexdigitaltwin

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/codexinput"
)

// InputStratum classifies a synthesized submission scenario. The four strata
// span the reactor's terminal outcomes: positive ack, protocol rejection, the
// AIS-INV-001 stale-timeout front-stop, and the AIS-017 handshake fast-fail.
type InputStratum string

const (
	// StratumAcked — the happy path: spawn, handshake, submit, positive ack, turn
	// completes. Terminal: agent_input_acked.
	StratumAcked InputStratum = "acked"
	// StratumRejected — a protocol-level refusal resolves the synchronous
	// Ack{Rejected}: the ack timer is cancelled, no positive/stale event.
	StratumRejected InputStratum = "rejected"
	// StratumStaleTimeout — a submission that never gets its ack: the armed
	// input_ack_timeout fires → agent_input_stale (the resume-hang front-stop).
	StratumStaleTimeout InputStratum = "stale_timeout"
	// StratumHandshakeFail — the handshake never completes: the armed
	// handshake_timeout fires → agent_launch_failure (AIS-017 fast-fail).
	StratumHandshakeFail InputStratum = "handshake_fail"
)

// AllInputStrata is the canonical stratum ordering the harness iterates.
var AllInputStrata = []InputStratum{
	StratumAcked, StratumRejected, StratumStaleTimeout, StratumHandshakeFail,
}

const (
	synthSeq    = 1
	synthTurnID = "twin-turn-0001"
)

var inputSynthTable = map[InputStratum][]codexinput.Event{
	StratumAcked: {
		{Type: codexinput.EventTypeSpawned},
		{Type: codexinput.EventTypeHandshakeOK},
		{Type: codexinput.EventTypeInputSubmitted, InputSeq: synthSeq},
		{Type: codexinput.EventTypeInputAcked, InputSeq: synthSeq, TurnID: synthTurnID},
		{Type: codexinput.EventTypeTurnCompleted, TurnID: synthTurnID},
	},
	StratumRejected: {
		{Type: codexinput.EventTypeSpawned},
		{Type: codexinput.EventTypeHandshakeOK},
		{Type: codexinput.EventTypeInputSubmitted, InputSeq: synthSeq},
		{Type: codexinput.EventTypeInputRejected, InputSeq: synthSeq, Reason: "protocol_refusal"},
	},
	StratumStaleTimeout: {
		{Type: codexinput.EventTypeSpawned},
		{Type: codexinput.EventTypeHandshakeOK},
		{Type: codexinput.EventTypeInputSubmitted, InputSeq: synthSeq},
	},
	StratumHandshakeFail: {
		{Type: codexinput.EventTypeSpawned},
	},
}

// SynthesizeInputStimulus returns the flat external-stimulus schedule for a
// stratum (a fresh copy the caller may mutate).
func SynthesizeInputStimulus(stratum InputStratum) ([]codexinput.Event, error) {
	steps, ok := inputSynthTable[stratum]
	if !ok {
		return nil, fmt.Errorf("codexdigitaltwin: unknown input stratum %q", stratum)
	}
	out := make([]codexinput.Event, len(steps))
	copy(out, steps)
	return out, nil
}

// EncodeInputStimulus serializes a schedule as NDJSON — the corpus format
// substrate.Twin replays and inputCodec decodes (one codexinput.Event per line).
func EncodeInputStimulus(events []codexinput.Event) ([]byte, error) {
	var buf bytes.Buffer
	for i, ev := range events {
		raw, err := json.Marshal(ev)
		if err != nil {
			return nil, fmt.Errorf("codexdigitaltwin: encode input stimulus %d: %w", i, err)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}
