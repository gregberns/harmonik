package queue

// rpc_emitorlog_hk9z2yl_test.go — coverage for HandlerAdapter.emitOrLog
// (de9aac48, follow-up bead hk-9z2yl).
//
// The three queue-event emit sites used to read `_ = a.bus.Emit(ctx, ...)`.
// A dropped queue event is invisible to the workloop, the dashboard and replay,
// and the RPC has already succeeded by then so it cannot be rolled back — the
// only remaining obligation is that the failure leaves a trace. emitOrLog is
// that trace, and nothing asserted it existed.
//
// Reverting emitOrLog's body to `_ = a.bus.Emit(ctx, t, raw)` makes
// TestEmitOrLog_BusFailure_LeavesATrace fail: the log buffer stays empty.
//
// These tests do NOT call t.Parallel: they swap the process-global log output.
//
// Bead ref: hk-9z2yl.

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// emitOrLogFakeBus is an EventEmitter that records what it was handed and
// returns a fixed error.
type emitOrLogFakeBus struct {
	err     error
	calls   int
	lastTyp core.EventType
	lastRaw []byte
}

func (b *emitOrLogFakeBus) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	b.calls++
	b.lastTyp = eventType
	b.lastRaw = append([]byte(nil), payload...)
	return b.err
}

// emitOrLogSyncBuffer is a mutex-guarded log sink. The package's other tests
// run in parallel in the same binary and a stray log.Printf from any of them
// would race a bare bytes.Buffer under -race.
type emitOrLogSyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *emitOrLogSyncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *emitOrLogSyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// emitOrLogCaptureLog redirects the standard logger into a buffer for the
// duration of the test and returns it.
func emitOrLogCaptureLog(t *testing.T) *emitOrLogSyncBuffer {
	t.Helper()
	sink := &emitOrLogSyncBuffer{}
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(sink)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	return sink
}

// TestEmitOrLog_BusFailure_LeavesATrace verifies a failed Emit is reported
// (with the calling handler, the event type and the cause) rather than dropped.
func TestEmitOrLog_BusFailure_LeavesATrace(t *testing.T) {
	sink := emitOrLogCaptureLog(t)

	busErr := errors.New("event bus closed")
	bus := &emitOrLogFakeBus{err: busErr}
	adapter := NewHandlerAdapter(nil, t.TempDir(), nil, bus)

	adapter.emitOrLog(context.Background(), "HandleQueueSubmit", core.EventTypeQueueSubmitted, []byte(`{"queue_id":"q1"}`))

	if bus.calls != 1 {
		t.Fatalf("bus.Emit called %d times, want 1", bus.calls)
	}
	logged := sink.String()
	if logged == "" {
		t.Fatal("emitOrLog dropped a failed bus.Emit silently: nothing was logged")
	}
	for _, want := range []string{"HandleQueueSubmit", string(core.EventTypeQueueSubmitted), busErr.Error()} {
		if !strings.Contains(logged, want) {
			t.Errorf("log line %q does not mention %q", logged, want)
		}
	}
}

// TestEmitOrLog_Success_IsSilent verifies the happy path both forwards the
// payload untouched and logs nothing — emitOrLog must not turn every emit into
// daemon log noise.
func TestEmitOrLog_Success_IsSilent(t *testing.T) {
	sink := emitOrLogCaptureLog(t)

	bus := &emitOrLogFakeBus{}
	adapter := NewHandlerAdapter(nil, t.TempDir(), nil, bus)

	raw := []byte(`{"queue_id":"q2","group_index":0}`)
	adapter.emitOrLog(context.Background(), "HandleQueueAppend", core.EventTypeQueueAppended, raw)

	if bus.calls != 1 {
		t.Fatalf("bus.Emit called %d times, want 1", bus.calls)
	}
	if bus.lastTyp != core.EventTypeQueueAppended {
		t.Errorf("bus received event type %q, want %q", bus.lastTyp, core.EventTypeQueueAppended)
	}
	if !bytes.Equal(bus.lastRaw, raw) {
		t.Errorf("bus received payload %q, want %q", bus.lastRaw, raw)
	}
	if logged := sink.String(); logged != "" {
		t.Errorf("successful emit logged %q, want nothing", logged)
	}
}
