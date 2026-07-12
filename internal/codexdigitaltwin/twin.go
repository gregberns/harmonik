// Package codexdigitaltwin is the T4 digital twin for the codex app-server
// integration (codex-app-server T4, hk-swc8p).
//
// The twin reads a captured JSONL corpus (real codex app-server session traffic)
// and replays it through the real wire parser (codexwire T2), producing a
// codexreactor.EventSource that the T3 reactor can consume. This closes the loop
// between raw captured bytes and the reactor's typed event abstractions.
//
// Fault injection is supported for testing the reactor under adverse transport
// conditions. Four modes are available:
//
//   - FaultDropAfter N: emit the first N events, inject Disconnected, close.
//   - FaultStall N: block before emitting the Nth event until ctx is cancelled.
//   - FaultTruncate N: replace the Nth event with an Error event (simulates a
//     malformed/truncated JSONL line), then close.
//   - FaultDup N: emit the Nth event twice with the same Seq (reactor I2 dedup
//     must drop the second copy).
//
// # Architecture
//
//	corpus JSONL
//	     ↓  codexwire.Parse (T2)
//	  Frame
//	     ↓  frameToEvent
//	  Event
//	     ↓  fault injection
//	  EventSource channel
//	     ↓  Reactor.Run (T3)
//	  Actions → FakeEffector
//
// Bead: hk-swc8p [codex-app-server T4]
package codexdigitaltwin

import (
	"bufio"
	"bytes"
	"context"
	"io"

	"github.com/gregberns/harmonik/internal/codexreactor"
	"github.com/gregberns/harmonik/internal/codexwire"
)

// ─── Fault injection ─────────────────────────────────────────────────────────

// FaultMode selects the fault injection behaviour of the Twin.
type FaultMode int

const (
	// FaultNone disables fault injection; the corpus is replayed unmodified.
	FaultNone FaultMode = iota

	// FaultDropAfter emits the first EventN reactor events, then injects a
	// Disconnected event and closes the source.
	FaultDropAfter

	// FaultStall blocks before emitting the EventN-th reactor event until ctx
	// is cancelled, then closes the source.
	FaultStall

	// FaultTruncate replaces the EventN-th reactor event with an Error event
	// (simulating a malformed / truncated wire line), then closes the source.
	FaultTruncate

	// FaultDup emits the EventN-th reactor event twice with the same Seq field.
	// The reactor's I2 dedup invariant must drop the second copy.
	FaultDup
)

// FaultConfig parameterises fault injection. EventN is 1-based (the first
// emitted reactor event is event 1). EventN is ignored when Mode == FaultNone.
type FaultConfig struct {
	Mode   FaultMode
	EventN int
}

// ─── Twin ────────────────────────────────────────────────────────────────────

// Twin replays a captured codex app-server JSONL corpus as a
// codexreactor.EventSource, optionally injecting transport faults.
type Twin struct {
	corpus io.Reader
	fault  FaultConfig
}

// New creates a Twin that reads corpus and applies the given fault injection.
// Pass FaultConfig{} (or FaultConfig{Mode: FaultNone}) for a clean replay.
func New(corpus io.Reader, fault FaultConfig) *Twin {
	return &Twin{corpus: corpus, fault: fault}
}

// Events implements codexreactor.EventSource.
//
// The goroutine started here terminates when:
//   - the corpus is exhausted,
//   - ctx is cancelled,
//   - the fault causes an early stop.
func (t *Twin) Events(ctx context.Context) <-chan codexreactor.Event {
	ch := make(chan codexreactor.Event, 16)
	go func() {
		defer close(ch)
		t.replay(ctx, ch)
	}()
	return ch
}

func (t *Twin) replay(ctx context.Context, ch chan<- codexreactor.Event) {
	send := func(ev codexreactor.Event) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	scanner := bufio.NewScanner(t.corpus)
	// Default 64 KB buffer is sufficient for the corpus (max line ~1 KB).

	var seq uint64 // monotonically assigned to emitted events
	evIdx := 0     // count of reactor events emitted so far

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		frame, err := codexwire.Parse(line)
		if err != nil {
			// Parse error: emit an Error event so the reactor can handle it.
			seq++
			evIdx++
			ev := codexreactor.Event{Seq: seq, Type: codexreactor.EventTypeError, Message: err.Error()}
			send(ev)
			return // treat parse error as a fatal transport failure
		}

		// Only server notifications are translated to reactor events.
		// Client frames and server responses are not emitted.
		if frame.Kind != codexwire.FrameKindServerNotification {
			continue
		}

		ev, mapped := frameToEvent(frame, &seq)
		if !mapped {
			// Not a reactor-relevant notification (configWarning, etc.) — skip.
			continue
		}

		evIdx++

		// Apply fault when we've reached the configured event index.
		if t.fault.Mode != FaultNone && evIdx == t.fault.EventN {
			switch t.fault.Mode {
			case FaultDropAfter:
				// Emit this event, inject Disconnected, then stop.
				if !send(ev) {
					return
				}
				disc := codexreactor.Event{Seq: 0, Type: codexreactor.EventTypeDisconnected}
				send(disc)
				return

			case FaultStall:
				// Block until ctx cancellation.
				<-ctx.Done()
				return

			case FaultTruncate:
				// Replace event with an error (simulates a truncated wire line).
				errEv := codexreactor.Event{Seq: seq, Type: codexreactor.EventTypeError, Message: "twin: truncated frame"}
				send(errEv)
				return

			case FaultDup:
				// Send the same event twice (same Seq); reactor I2 dedup drops copy.
				if !send(ev) {
					return
				}
				if !send(ev) {
					return
				}
				continue
			}
		}

		if !send(ev) {
			return
		}
	}
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
