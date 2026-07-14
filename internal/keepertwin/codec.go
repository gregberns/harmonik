package keepertwin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ─── Fault injection (re-exported from substrate) ─────────────────────────────

// FaultMode is re-exported (a type alias) from the generic substrate replay
// engine, mirroring codexdigitaltwin, so keeper fault tests read naturally.
type FaultMode = substrate.FaultMode

// FaultConfig is re-exported (a type alias) from the generic substrate replay
// engine.
type FaultConfig = substrate.FaultConfig

// Fault-mode constants re-exported from substrate (Go has no enum re-export
// sugar; restated verbatim, mirroring codexdigitaltwin).
const (
	FaultNone      = substrate.FaultNone
	FaultDropAfter = substrate.FaultDropAfter
	FaultStall     = substrate.FaultStall
	FaultTruncate  = substrate.FaultTruncate
	FaultDup       = substrate.FaultDup
)

// ─── Synthetic fault-event kinds ──────────────────────────────────────────────

// The keeper reactor's input vocabulary (keeper.EventKind) has no native
// transport-error or connection-lost kind — the pre-rebuild keeper read files
// and panes directly, so "the transport failed" was never an input event.
// Per measurement-design §2.3, the codec's two synthetic constructors stand in
// for keeper's restart_failed-class stimuli (gauge/handoff read error; pane
// vanished / session died). They are defined HERE (keeper.EventKind is an open
// string type) so internal/keeper stays untouched; the pure reactor's total
// transition ignores unknown kinds, and the T12 fault-matrix harness asserts
// the never-silence bound around them (a stream that ends on one of these must
// still reach a terminal within the virtual deadline — the harness converts
// the observation into an explicit failure, never a hang).
const (
	// EvTwinTransportError is the FaultTruncate / fatal-decode stimulus
	// (keeper's GaugeReadError/HandoffReadError analog).
	EvTwinTransportError keeper.EventKind = "twin_transport_error"
	// EvTwinDisconnected is the FaultDropAfter stimulus (keeper's
	// PaneVanished/SessionDied analog).
	EvTwinDisconnected keeper.EventKind = "twin_disconnected"
)

// ─── keeperCodec ──────────────────────────────────────────────────────────────

// keeperCodec implements substrate.ReplayCodec[keeper.Event]: it deserializes
// synthesized stimulus lines (EncodeStimulus output — one JSON keeper.Event
// per line) back into keeper input events, and supplies the two synthetic
// fault events the substrate fault injector needs (D2/D3). It is stateless:
// keeper events carry no sequence number (dedup is reactor-side, keyed on
// cycle_id), so no codec-internal seq is required.
type keeperCodec struct{}

// DecodeLine decodes one synthesized stimulus line.
//
//	err != nil   → FATAL: corrupt stimulus; the twin emits ErrorEvent(err)
//	               and closes (measurement-design §2.3).
//	emit == true → deliver ev. Every well-formed line is reactor-relevant
//	               (the synthesizer writes nothing advisory), so there is no
//	               skip path today.
func (c *keeperCodec) DecodeLine(line []byte) (keeper.Event, bool, error) {
	var ev keeper.Event
	if err := json.Unmarshal(line, &ev); err != nil {
		return keeper.Event{}, false, fmt.Errorf("keepertwin: decode stimulus line: %w", err)
	}
	if ev.Kind == "" {
		return keeper.Event{}, false, fmt.Errorf("keepertwin: stimulus line missing event kind")
	}
	return ev, true, nil
}

// ErrorEvent is keeper's transport-error input stimulus (used on a fatal
// decode error and for FaultTruncate). The message rides in Source for
// forensic assertion by the fault matrix.
func (c *keeperCodec) ErrorEvent(msg string) keeper.Event {
	return keeper.Event{Kind: EvTwinTransportError, Source: msg}
}

// DisconnectEvent is keeper's connection-lost input stimulus (used for
// FaultDropAfter).
func (c *keeperCodec) DisconnectEvent() keeper.Event {
	return keeper.Event{Kind: EvTwinDisconnected}
}

// ─── Twin ─────────────────────────────────────────────────────────────────────

// Twin replays a synthesized keeper stimulus stream (NDJSON of keeper.Event)
// as a substrate.EventSource[keeper.Event], optionally injecting transport
// faults. It is a thin wrapper over substrate.Twin[keeper.Event]
// parameterised with keeperCodec, mirroring codexdigitaltwin.Twin.
type Twin struct {
	inner *substrate.Twin[keeper.Event]
}

// New creates a Twin that reads the synthesized stimulus and applies the given
// fault injection. Pass FaultConfig{} (or FaultConfig{Mode: FaultNone}) for a
// clean replay.
func New(stimulus io.Reader, fault FaultConfig) *Twin {
	return &Twin{inner: substrate.NewTwin(stimulus, fault, &keeperCodec{})}
}

// Events implements substrate.EventSource[keeper.Event]. The goroutine
// terminates when the stimulus is exhausted, ctx is cancelled, or the fault
// causes an early stop.
func (t *Twin) Events(ctx context.Context) <-chan keeper.Event {
	return t.inner.Events(ctx)
}
