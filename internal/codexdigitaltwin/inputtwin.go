package codexdigitaltwin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/substrate"
)

const (
	// EvTwinTransportError is the FaultTruncate / fatal-decode stimulus. Its type
	// is deliberately NOT codexinput.EventTypeError ("error"), so the reactor's
	// Step drops it and reaches its own timer-driven terminal instead of acting
	// on a transport terminal (the keepertwin path-A idiom).
	EvTwinTransportError codexinput.EventType = "twin_transport_error"
	// EvTwinDisconnected is the FaultDropAfter stimulus (analogously ignored).
	EvTwinDisconnected codexinput.EventType = "twin_disconnected"
)

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
