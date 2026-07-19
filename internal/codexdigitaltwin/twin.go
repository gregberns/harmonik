// Package codexdigitaltwin is the T4 digital twin for the codex app-server
// integration (codex-app-server T4, hk-swc8p).
//
// The twin reads a captured JSONL corpus (real codex app-server session traffic)
// and replays it through the real wire parser (codexwire T2), producing a
// codexreactor.EventSource that the T3 reactor can consume. This closes the loop
// between raw captured bytes and the reactor's typed event abstractions.
//
// Fault injection is supported for testing the reactor under adverse transport
// conditions. The four fault modes are the vertical-neutral modes defined by
// the generic substrate replay engine; for the codex instantiation they mean:
//
//   - FaultDropAfter N: emit the first N events, then the connection-lost event
//     (Disconnected), and close.
//   - FaultStall N: block before emitting the Nth event until ctx is cancelled.
//   - FaultTruncate N: replace the Nth event with the transport-error event
//     (Error, simulating a malformed/truncated JSONL line), then close.
//   - FaultDup N: emit the Nth event twice with the same Seq (reactor I2 dedup
//     must drop the second copy).
//
// # Architecture
//
//	corpus JSONL
//	     ↓  codexCodec.DecodeLine (codexwire.Parse + frameToEvent)
//	  Event
//	     ↓  substrate.Twin[Event] (fault injection, generic seam)
//	  EventSource channel
//	     ↓  Reactor.Run (T3)
//	  Actions → FakeEffector
//
// FaultMode/FaultConfig and the replay engine now live in internal/substrate;
// this package re-exports the fault names, supplies a codexCodec that fuses the
// former replay-loop leak points (decode, filter, map, and the two synthetic
// terminal events), and keeps a thin wrapper Twin so every call site — and thus
// codextest — compiles unchanged (RS-021; substrate-design §2.3).
//
// Bead: hk-swc8p [codex-app-server T4]
package codexdigitaltwin

import (
	"context"
	"io"

	"github.com/gregberns/harmonik/internal/codexreactor"
	"github.com/gregberns/harmonik/internal/codexwire"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ─── Fault injection (re-exported from substrate) ─────────────────────────────

// FaultMode is re-exported (a type alias) from the generic substrate replay
// engine so existing call sites keep compiling unchanged.
type FaultMode = substrate.FaultMode

// FaultConfig is re-exported (a type alias) from the generic substrate replay
// engine so existing call sites keep compiling unchanged.
type FaultConfig = substrate.FaultConfig

// Fault-mode constants re-exported from substrate. Go has no enum re-export
// sugar, so these are restated verbatim; this is what lets composite literals
// such as FaultConfig{Mode: FaultDropAfter, EventN: 2} keep compiling.
const (
	FaultNone      = substrate.FaultNone
	FaultDropAfter = substrate.FaultDropAfter
	FaultStall     = substrate.FaultStall
	FaultTruncate  = substrate.FaultTruncate
	FaultDup       = substrate.FaultDup
)

// ─── codexCodec ────────────────────────────────────────────────────────────────

// substrateTruncateSentinel is the message substrate.Twin passes to ErrorEvent
// on the FaultTruncate path (internal/substrate/replay.go). The codex twin
// historically reported truncation as "twin: truncated frame"; the codec
// translates the neutral substrate sentinel back to the codex phrasing so the
// existing fault tests stay green without edits. A genuine fatal decode error
// carries the real parser message and is passed through unchanged.
const substrateTruncateSentinel = "substrate: truncated frame"

// codexCodec implements substrate.ReplayCodec[codexreactor.Event]. It fuses the
// codex replay leak points — wire decode, server-notification filter, and the
// Frame→Event map — into DecodeLine, and supplies the two codex-typed synthetic
// terminal events. The seq counter is codec-internal state (it no longer
// threads through the substrate surface; RS-008, substrate-design §2.3).
type codexCodec struct {
	seq uint64
}

// DecodeLine fuses decode + server-notification filter + Frame→Event map.
//
//	err  != nil  → fatal transport failure (twin emits ErrorEvent, closes)
//	emit == false → skip (client frame, server response, or non-mapped notif)
//	emit == true  → deliver ev
func (c *codexCodec) DecodeLine(line []byte) (codexreactor.Event, bool, error) {
	frame, err := codexwire.Parse(line)
	if err != nil {
		// Fatal transport failure: substrate emits ErrorEvent(err) and closes.
		return codexreactor.Event{}, false, err
	}
	// Only server notifications translate to reactor events; client frames,
	// server responses, and server-originated requests (approval prompts —
	// FrameKindServerRequest) are skipped (not fatal). The twin replays a
	// captured corpus and never answers requests, so a server request is a
	// no-op here — but it is now classified distinctly rather than misfiled as a
	// client request, keeping the twin in parity with the live session (RU-07).
	if frame.Kind != codexwire.FrameKindServerNotification {
		return codexreactor.Event{}, false, nil
	}
	ev, mapped := frameToEvent(frame, &c.seq)
	// mapped==false → not a reactor-relevant notification (configWarning, …) → skip.
	return ev, mapped, nil
}

// ErrorEvent is the codex transport-error terminal event. Its Seq is the codec's
// current seq (advanced by the most recent frameToEvent), satisfying the
// FaultTruncate "current seq" requirement for free.
func (c *codexCodec) ErrorEvent(msg string) codexreactor.Event {
	if msg == substrateTruncateSentinel {
		msg = "twin: truncated frame"
	}
	return codexreactor.Event{Seq: c.seq, Type: codexreactor.EventTypeError, Message: msg}
}

// DisconnectEvent is the codex connection-lost event. Seq=0 bypasses reactor I2
// dedup (connection-lifecycle events always process).
func (c *codexCodec) DisconnectEvent() codexreactor.Event {
	return codexreactor.Event{Seq: 0, Type: codexreactor.EventTypeDisconnected}
}

// ─── Twin ────────────────────────────────────────────────────────────────────

// Twin replays a captured codex app-server JSONL corpus as a
// codexreactor.EventSource, optionally injecting transport faults. It is a thin
// wrapper over substrate.Twin[codexreactor.Event] parameterised with codexCodec.
type Twin struct {
	inner *substrate.Twin[codexreactor.Event]
}

// New creates a Twin that reads corpus and applies the given fault injection.
// Pass FaultConfig{} (or FaultConfig{Mode: FaultNone}) for a clean replay. The
// signature is unchanged from the pre-extraction Twin, so every call site
// compiles untouched.
func New(corpus io.Reader, fault FaultConfig) *Twin {
	return &Twin{inner: substrate.NewTwin(corpus, fault, &codexCodec{})}
}

// Events implements codexreactor.EventSource. It delegates to the generic Twin,
// whose goroutine terminates when the corpus is exhausted, ctx is cancelled, or
// the fault causes an early stop.
func (t *Twin) Events(ctx context.Context) <-chan codexreactor.Event {
	return t.inner.Events(ctx)
}

// ─── Frame → Event translation ───────────────────────────────────────────────

// frameToEvent translates a parsed codexwire server notification into a
// codexreactor.Event. seq is incremented for each successfully translated
// event. Returns (zero, false) for notifications that are not reactor-relevant.
func frameToEvent(frame codexwire.Frame, seq *uint64) (codexreactor.Event, bool) {
	switch frame.Method {

	case "turn/started":
		p, ok := frame.Params.(*codexwire.TurnStartedParams)
		if !ok || p == nil {
			return codexreactor.Event{}, false
		}
		*seq++
		return codexreactor.Event{
			Seq:      *seq,
			Type:     codexreactor.EventTypeTurnStarted,
			ThreadID: p.ThreadID,
			TurnID:   p.Turn.ID,
		}, true

	case "turn/completed":
		p, ok := frame.Params.(*codexwire.TurnCompletedParams)
		if !ok || p == nil {
			return codexreactor.Event{}, false
		}
		*seq++
		return codexreactor.Event{
			Seq:      *seq,
			Type:     codexreactor.EventTypeTurnCompleted,
			ThreadID: p.ThreadID,
			TurnID:   p.Turn.ID,
			Status:   p.Turn.Status,
		}, true

	case "item/agentMessage/delta":
		p, ok := frame.Params.(*codexwire.ItemAgentMessageDeltaParams)
		if !ok || p == nil {
			return codexreactor.Event{}, false
		}
		*seq++
		return codexreactor.Event{
			Seq:      *seq,
			Type:     codexreactor.EventTypeMessageDelta,
			ThreadID: p.ThreadID,
			TurnID:   p.TurnID,
			ItemID:   p.ItemID,
			Delta:    p.Delta,
		}, true

	case "thread/status/changed":
		p, ok := frame.Params.(*codexwire.ThreadStatusChangedParams)
		if !ok || p == nil {
			return codexreactor.Event{}, false
		}
		*seq++
		return codexreactor.Event{
			Seq:      *seq,
			Type:     codexreactor.EventTypeThreadStatus,
			ThreadID: p.ThreadID,
			Status:   p.Status.Type,
		}, true

	case "thread/tokenUsage/updated":
		p, ok := frame.Params.(*codexwire.ThreadTokenUsageUpdatedParams)
		if !ok || p == nil {
			return codexreactor.Event{}, false
		}
		*seq++
		return codexreactor.Event{
			Seq:           *seq,
			Type:          codexreactor.EventTypeTokenUsage,
			ThreadID:      p.ThreadID,
			TurnID:        p.TurnID,
			TotalTokens:   p.TokenUsage.Total.TotalTokens,
			ContextWindow: p.TokenUsage.ModelContextWindow,
		}, true
	}

	return codexreactor.Event{}, false
}
