// inputsynth.go — the INPUT-direction stimulus synthesizer (T9).
//
// It builds the discrete stripped stimulus corpora the fault matrix replays:
// per-stratum flat sequences of codexinput.Event that carry one input submission
// through the driver's lifecycle to a definite outcome. The captured OUTPUT
// corpus (testdata/codex-app-server/) proves the wire codec parses zero-raw (the
// drift canary guards it); this synthesizer supplies the reactor's INPUT-side
// stimuli, whose vocabulary (Spawned / HandshakeOK / InputSubmitted / InputAcked
// / InputRejected / TurnCompleted / CloseRequested) the captured output frames do
// not contain — the same input-vs-output gap keepertwin's SynthesizeStimulus
// closes (measurement-design §2).
//
// NO pre-scheduled TimerFired lines are emitted: timer firings are produced by
// the discrete-event harness from the reactor's own ArmTimer actions (the L2
// harness), never delivered as external stimulus. That keeps the EventN domain
// equal to the number of external stimuli.

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

// the fixed submission seq + turn id the strata use.
const (
	synthSeq    = 1
	synthTurnID = "twin-turn-0001"
)

// inputSynthTable is the single reviewed decision table: each stratum maps to the
// flat external-stimulus schedule (NO TimerFired lines — the harness generates
// those from ArmTimer). Every schedule opens at Spawned so that FaultStall@1 /
// FaultTruncate@1 foreclose the whole lifecycle (no submission opens), matching
// the T9 entry-foreclosed acceptance shape.
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
