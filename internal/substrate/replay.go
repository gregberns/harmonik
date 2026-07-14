package substrate

import (
	"bufio"
	"bytes"
	"context"
	"io"
)

// defaultBufferSize is the Twin scanner's default buffer capacity. It is 1 MB,
// not the stdlib bufio.Scanner 64 KB default, so an oversized corpus line does
// not truncate the replay invisibly (RS-010).
const defaultBufferSize = 1 << 20 // 1 MB

// ─── Fault injection ─────────────────────────────────────────────────────────

// FaultMode selects the fault-injection behaviour of the Twin. The modes are
// stated in vertical-neutral terms (RS-012); no transport vocabulary leaks into
// the substrate surface.
type FaultMode int

const (
	// FaultNone disables fault injection; the corpus is replayed unmodified.
	FaultNone FaultMode = iota

	// FaultDropAfter delivers events 1..N, then delivers the vertical's
	// connection-lost event (codec.DisconnectEvent()) and ends the stream.
	FaultDropAfter

	// FaultStall delivers events 1..N-1, then blocks before event N until ctx
	// is cancelled.
	FaultStall

	// FaultTruncate replaces event N with the vertical's transport-error event
	// (codec.ErrorEvent(...)) and ends the stream.
	FaultTruncate

	// FaultDup delivers event N, then delivers the identical event value a
	// second time (an idempotence probe).
	FaultDup
)

// FaultConfig parameterises fault injection. EventN is 1-based over the
// post-skip event stream (the first emitted event is event 1). EventN is
// ignored when Mode == FaultNone.
type FaultConfig struct {
	Mode   FaultMode
	EventN int
}

// ─── ReplayCodec ─────────────────────────────────────────────────────────────

// ReplayCodec is everything a vertical supplies to replay its corpus. It fuses
// the vertical's decode, error-policy, filter, and map steps into DecodeLine
// and supplies the two synthetic-event constructors the fault injector needs.
// Implementations MAY be stateful (own their sequence counter) — any per-line
// sequence/dedup state lives inside the codec, never on the substrate surface
// (RS-008).
type ReplayCodec[E any] interface {
	// DecodeLine decodes one corpus line.
	//   emit=false, err=nil  → skip this line (not reactor-relevant).
	//   err!=nil             → FATAL transport failure: twin emits
	//                          ErrorEvent(err) and closes.
	//   emit=true,  err=nil  → deliver ev.
	DecodeLine(line []byte) (ev E, emit bool, err error)

	// ErrorEvent is the vertical's transport-error terminal event (used on a
	// fatal decode error and for FaultTruncate).
	ErrorEvent(msg string) E

	// DisconnectEvent is the vertical's connection-lost event (used for
	// FaultDropAfter). A vertical lacking a natural disconnect concept supplies
	// its restart_failed-class terminal event here.
	DisconnectEvent() E
}

// ─── Twin ────────────────────────────────────────────────────────────────────

// Twin is the generic replay engine. It presents a captured corpus (an
// io.Reader of append-only NDJSON) as an EventSource[E], applying the
// configured fault mode via the supplied ReplayCodec[E] (RS-008).
type Twin[E any] struct {
	corpus io.Reader
	fault  FaultConfig
	codec  ReplayCodec[E]
	bufCap int
}

// TwinOption configures a Twin at construction.
type TwinOption func(*twinConfig)

type twinConfig struct {
	bufCap int
}

// WithBufferSize overrides the Twin scanner's buffer capacity (default 1 MB).
func WithBufferSize(n int) TwinOption {
	return func(c *twinConfig) { c.bufCap = n }
}

// NewTwin creates a Twin that reads corpus and applies the given fault
// injection, decoding each line through codec. Pass FaultConfig{} (or
// FaultConfig{Mode: FaultNone}) for a clean replay.
func NewTwin[E any](corpus io.Reader, fault FaultConfig, codec ReplayCodec[E], opts ...TwinOption) *Twin[E] {
	cfg := twinConfig{bufCap: defaultBufferSize}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Twin[E]{corpus: corpus, fault: fault, codec: codec, bufCap: cfg.bufCap}
}

// Events implements EventSource[E]. The goroutine started here terminates when
// the corpus is exhausted, ctx is cancelled, or the fault causes an early stop.
func (t *Twin[E]) Events(ctx context.Context) <-chan E {
	ch := make(chan E, 16)
	go func() {
		defer close(ch)
		t.replay(ctx, ch)
	}()
	return ch
}

func (t *Twin[E]) replay(ctx context.Context, ch chan<- E) {
	send := func(ev E) bool {
		select {
		case ch <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}

	scanner := bufio.NewScanner(t.corpus)
	scanner.Buffer(make([]byte, 0, t.bufCap), t.bufCap)

	evIdx := 0 // count of events emitted so far (post-skip)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		ev, emit, err := t.codec.DecodeLine(line)
		if err != nil {
			// Fatal transport failure: emit the transport-error terminal event
			// and close the stream.
			send(t.codec.ErrorEvent(err.Error()))
			return
		}
		if !emit {
			// Skip: not reactor-relevant.
			continue
		}

		evIdx++

		// Apply the fault when we reach the configured event index.
		if t.fault.Mode != FaultNone && evIdx == t.fault.EventN {
			switch t.fault.Mode {
			case FaultDropAfter:
				// Deliver this event, then the connection-lost event, then stop.
				if !send(ev) {
					return
				}
				send(t.codec.DisconnectEvent())
				return

			case FaultStall:
				// Block before this event until ctx cancellation.
				<-ctx.Done()
				return

			case FaultTruncate:
				// Replace this event with the transport-error event.
				send(t.codec.ErrorEvent("substrate: truncated frame"))
				return

			case FaultDup:
				// Deliver the same event value twice.
				if !send(ev) {
					return
				}
				if !send(ev) {
					return
				}
				continue

			case FaultNone:
				// Unreachable (guarded above); fall through to normal delivery.
			}
		}

		if !send(ev) {
			return
		}
	}
}
