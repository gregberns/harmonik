// inputtwin.go — the INPUT-direction digital twin for the structured Codex
// app-server driver (agent-input-substrate M2, T9).
//
// The sibling twin.go replays a captured OUTPUT-direction corpus
// (codexreactor.Event: turn/started, deltas, turn/completed) for the output
// reactor. This file is its INPUT-direction peer: it replays a synthesized
// codexinput.Event stimulus stream (the driver's ack/response direction — the
// D9 seam-gap resolution: "model the driver's ack/response stream as E") through
// the SAME generic substrate replay engine, so the four closed fault modes
// (RS-012) apply to input stimuli with zero new fault code.
//
// # The sentinel-ignore idiom (keepertwin/codec.go path A)
//
// The two synthetic fault constructors return SENTINEL events whose EventType is
// NOT in the codexinput reactor's Step switch (twin_transport_error /
// twin_disconnected — distinct from the reactor's native "error"/"disconnected").
// The reactor's total transition therefore IGNORES them (Step returns nil for an
// unknown type) and proceeds to its OWN bounded-liveness terminal: the armed
// input_ack_timeout fires → agent_input_stale (AIS-INV-001). This keeps the
// production reactor untouched (RS-009: never a new fault mode or reactor vocab)
// and is what makes the entry-foreclosed cells (Truncate@1 / Stall@1) open no
// submission at all, exactly as the T9 acceptance requires.

package codexdigitaltwin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ─── Synthetic fault-event sentinels (ignored by the reactor's Step) ──────────

const (
	// EvTwinTransportError is the FaultTruncate / fatal-decode stimulus. Its type
	// is deliberately NOT codexinput.EventTypeError ("error"), so the reactor's
	// Step drops it and reaches its own timer-driven terminal instead of acting
	// on a transport terminal (the keepertwin path-A idiom).
	EvTwinTransportError codexinput.EventType = "twin_transport_error"
	// EvTwinDisconnected is the FaultDropAfter stimulus (analogously ignored).
	EvTwinDisconnected codexinput.EventType = "twin_disconnected"
)

// ─── inputCodec ───────────────────────────────────────────────────────────────

// inputCodec implements substrate.ReplayCodec[codexinput.Event]: it deserializes
// synthesized stimulus lines (EncodeInputStimulus output — one JSON
// codexinput.Event per line) and supplies the two ignored sentinel constructors
// the substrate fault injector needs. Stateless: codexinput events carry their
// own InputSeq, so no codec-internal sequence is required.
type inputCodec struct{}

// DecodeLine decodes one synthesized stimulus line. Every well-formed line is
// reactor-relevant, so there is no skip path; a malformed line is a FATAL corpus
// error (twin emits ErrorEvent and closes).
func (inputCodec) DecodeLine(line []byte) (codexinput.Event, bool, error) {
	var ev codexinput.Event
	if err := json.Unmarshal(line, &ev); err != nil {
		return codexinput.Event{}, false, fmt.Errorf("codexdigitaltwin: decode input stimulus: %w", err)
	}
	if ev.Type == "" {
		return codexinput.Event{}, false, fmt.Errorf("codexdigitaltwin: input stimulus missing type")
	}
	return ev, true, nil
}

// ErrorEvent is the ignored transport-error sentinel (FaultTruncate / fatal
// decode). The message rides in Reason for forensic assertion.
func (inputCodec) ErrorEvent(msg string) codexinput.Event {
	return codexinput.Event{Type: EvTwinTransportError, Reason: msg}
}

// DisconnectEvent is the ignored connection-lost sentinel (FaultDropAfter).
func (inputCodec) DisconnectEvent() codexinput.Event {
	return codexinput.Event{Type: EvTwinDisconnected}
}

// ─── InputTwin ─────────────────────────────────────────────────────────────────

// InputTwin replays a synthesized codexinput.Event stimulus stream (NDJSON) as a
// substrate.EventSource[codexinput.Event], optionally injecting transport faults.
// It is a thin wrapper over substrate.Twin[codexinput.Event] parameterised with
// inputCodec, mirroring keepertwin.Twin and the sibling output Twin.
type InputTwin struct {
	inner *substrate.Twin[codexinput.Event]
}

// NewInputTwin creates an InputTwin over stimulus with the given fault injection.
// Pass FaultConfig{} (FaultNone) for a clean replay. FaultConfig / FaultMode and
// the fault constants are the substrate aliases already re-exported in twin.go.
func NewInputTwin(stimulus io.Reader, fault FaultConfig) *InputTwin {
	return &InputTwin{inner: substrate.NewTwin(stimulus, fault, inputCodec{})}
}

// Events implements substrate.EventSource[codexinput.Event].
func (t *InputTwin) Events(ctx context.Context) <-chan codexinput.Event {
	return t.inner.Events(ctx)
}
