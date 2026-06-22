package eventbus_test

// Tests for EV-014d: consumer-recovery replay contract (specs/event-model.md §4.3).
//
// Coverage:
//   TestBusImpl_SealRunsStartupReplayForConsumersWithSince
//     — Seal replays events after Since to matching consumers
//   TestBusImpl_SealUsesOffsetCheckpointWhenSinceNil
//     — OffsetCheckpointEventID drives replay when Since is nil
//   TestBusImpl_SealSkipsSyncConsumerReplay
//     — synchronous consumers are never replayed (EV-014d)
//   TestBusImpl_SealSkipsConsumersWithNilSince
//     — consumers without Since/OffsetCheckpoint start from live stream
//   TestBusImpl_SealInvokesTailTruncationCallback
//     — OnTailTruncation fired with lastDurableID when JSONL tail is torn
//   TestBusImpl_SealNoReplayWhenJSONLPathEmpty
//     — no JSONL path → Seal is a no-op (test/unit-test mode)
//   TestBusImpl_ReplayFrom_DeliversEventsAfterSince
//     — ReplayFrom delivers matching events after since
//   TestBusImpl_ReplayFrom_FiltersEventsByConsumerPattern
//     — events not matching consumer's pattern are skipped
//   TestBusImpl_ReplayFrom_UnknownConsumerReturnsError
//     — unknown consumer ID returns a non-nil error
//   TestBusImpl_ReplayFrom_EmptyPathNoOp
//     — no JSONL path → ReplayFrom is a no-op
//   TestBusImpl_DeadLetterReplay_DeliversMatchingEvents
//     — DeadLetterReplay dispatches dead-letter entries to the consumer
//   TestBusImpl_DeadLetterReplay_FilterNarrowsDelivery
//     — explicit filter constrains which dead-letter types reach the handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// ev014dFixture is the test harness for EV-014d tests.
type ev014dFixture struct {
	dir     string
	logPath string
	writer  *eventbus.JSONLWriter
}

func ev014dSetup(t *testing.T) *ev014dFixture {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	w, err := eventbus.OpenJSONLWriter(logPath)
	if err != nil {
		t.Fatalf("OpenJSONLWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return &ev014dFixture{dir: dir, logPath: logPath, writer: w}
}

// ev014dWriteRawEvent appends a pre-built event JSON line directly to the log
// file, bypassing the bus (so we can control the event_id precisely).
func ev014dWriteRawEvent(t *testing.T, logPath string, ev core.Event) {
	t.Helper()
	line, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(line); err != nil {
		t.Fatalf("write log: %v", err)
	}
}

// ev014dMakeEvent builds a minimal core.Event with the given EventID and Type.
func ev014dMakeEvent(id core.EventID, eventType string) core.Event {
	return core.Event{
		EventID:       id,
		SchemaVersion: 1,
		Type:          eventType,
		Payload:       json.RawMessage(`{}`),
	}
}

// ev014dMonotonicIDs returns n sequential EventIDs in ascending UUIDv7 order.
// Using the process generator ensures monotonicity.
func ev014dMonotonicIDs(t *testing.T, n int) []core.EventID {
	t.Helper()
	gen := core.NewEventIDGenerator()
	ids := make([]core.EventID, n)
	for i := range ids {
		id, err := gen.Next()
		if err != nil {
			t.Fatalf("EventIDGenerator.Next: %v", err)
		}
		ids[i] = id
	}
	return ids
}

// ─────────────────────────────────────────────────────────────────────────────
// Seal startup replay
// ─────────────────────────────────────────────────────────────────────────────

// TestBusImpl_SealRunsStartupReplayForConsumersWithSince asserts that Seal
// replays JSONL events strictly after sub.Since to the consumer's handler.
func TestBusImpl_SealRunsStartupReplayForConsumersWithSince(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 4)
	// Write 4 events to the log.
	for i, id := range ids {
		ev := ev014dMakeEvent(id, "run_started")
		ev014dWriteRawEvent(t, fix.logPath, ev)
		_ = i
	}

	// Consumer with Since = ids[1]: should replay ids[2] and ids[3].
	sinceID := ids[1]
	var mu sync.Mutex
	var got []core.EventID

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	sub := core.Subscription{
		ConsumerID:    "replay-consumer",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Since:         &sinceID,
		Handler: func(_ context.Context, ev core.Event) error {
			mu.Lock()
			got = append(got, ev.EventID)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("EV-014d: want 2 replayed events (ids[2]+ids[3]), got %d", len(got))
	}
	if got[0] != ids[2] {
		t.Errorf("EV-014d: first replayed event_id = %v, want %v", got[0], ids[2])
	}
	if got[1] != ids[3] {
		t.Errorf("EV-014d: second replayed event_id = %v, want %v", got[1], ids[3])
	}
}

// TestBusImpl_SealUsesOffsetCheckpointWhenSinceNil asserts that when Since is
// nil but OffsetCheckpointEventID is set, it drives replay.
func TestBusImpl_SealUsesOffsetCheckpointWhenSinceNil(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 3)
	for _, id := range ids {
		ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(id, "run_started"))
	}

	// OffsetCheckpointEventID = ids[0]: should replay ids[1] and ids[2].
	checkpoint := ids[0]
	var mu sync.Mutex
	var got []core.EventID

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	sub := core.Subscription{
		ConsumerID:              "checkpoint-consumer",
		ConsumerClass:           core.ConsumerClassAsynchronous,
		EventPattern:            core.EventPattern{Wildcard: true},
		OnPanic:                 core.OnPanicRecoverAndLog,
		OffsetCheckpointEventID: &checkpoint,
		Handler: func(_ context.Context, ev core.Event) error {
			mu.Lock()
			got = append(got, ev.EventID)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("EV-014d: want 2 replayed events, got %d", len(got))
	}
}

// TestBusImpl_SealSkipsSyncConsumerReplay asserts that synchronous consumers
// with a non-nil Since are NOT replayed (EV-014d).
func TestBusImpl_SealSkipsSyncConsumerReplay(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 2)
	for _, id := range ids {
		ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(id, "run_started"))
	}

	sinceID := ids[0]
	called := false

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	sub := core.Subscription{
		ConsumerID:    "sync-consumer",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Since:         &sinceID,
		Handler: func(_ context.Context, _ core.Event) error {
			called = true
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if called {
		t.Error("EV-014d: synchronous consumer handler was called during startup replay; must not be")
	}
}

// TestBusImpl_SealSkipsConsumersWithNilSince asserts that consumers with no
// Since and no OffsetCheckpointEventID receive no replay events.
func TestBusImpl_SealSkipsConsumersWithNilSince(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 2)
	for _, id := range ids {
		ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(id, "run_started"))
	}

	called := false
	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	sub := core.Subscription{
		ConsumerID:    "live-only-consumer",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			called = true
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if called {
		t.Error("consumer without Since should receive no replay events; handler was called")
	}
}

// TestBusImpl_SealInvokesTailTruncationCallback asserts that OnTailTruncation
// is invoked with the last durable event_id when the JSONL tail is torn.
func TestBusImpl_SealInvokesTailTruncationCallback(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 2)
	// Write first event as complete line.
	ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(ids[0], "run_started"))
	// Write second event as a torn tail (no trailing newline).
	partial, _ := json.Marshal(ev014dMakeEvent(ids[1], "run_started"))
	f, _ := os.OpenFile(fix.logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	_, _ = f.Write(partial) // no '\n' — torn tail
	_ = f.Close()

	sinceID := core.EventID{} // start from the beginning (before ids[0])
	var cbID core.EventID
	cbCalled := false

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	sub := core.Subscription{
		ConsumerID:    "trunc-consumer",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Since:         &sinceID,
		OnTailTruncation: func(_ context.Context, lastDurableEventID core.EventID) {
			cbCalled = true
			cbID = lastDurableEventID
		},
		Handler: func(_ context.Context, _ core.Event) error { return nil },
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if !cbCalled {
		t.Fatal("EV-014d: OnTailTruncation was not called for torn-tail JSONL")
	}
	if cbID != ids[0] {
		t.Errorf("EV-014d: OnTailTruncation lastDurableEventID = %v, want %v (last complete event)", cbID, ids[0])
	}
}

// TestBusImpl_SealNoReplayWhenJSONLPathEmpty asserts that Seal is a no-op for
// replay when the bus was constructed without a JSONL path.
func TestBusImpl_SealNoReplayWhenJSONLPathEmpty(t *testing.T) {
	ids := ev014dMonotonicIDs(t, 1)
	sinceID := ids[0]
	called := false

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, nil, nil, "", "" /* no jsonlPath */)
	sub := core.Subscription{
		ConsumerID:    "no-path-consumer",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Since:         &sinceID,
		Handler: func(_ context.Context, _ core.Event) error {
			called = true
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if called {
		t.Error("no JSONL path: handler should not be called during Seal replay")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReplayFrom
// ─────────────────────────────────────────────────────────────────────────────

// TestBusImpl_ReplayFrom_DeliversEventsAfterSince asserts that ReplayFrom
// delivers events strictly after since to the named consumer.
func TestBusImpl_ReplayFrom_DeliversEventsAfterSince(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 4)
	for _, id := range ids {
		ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(id, "run_started"))
	}

	var mu sync.Mutex
	var got []core.EventID

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	sub := core.Subscription{
		ConsumerID:    "replay-target",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			mu.Lock()
			got = append(got, ev.EventID)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if err := bus.ReplayFrom("replay-target", ids[1]); err != nil {
		t.Fatalf("ReplayFrom: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Expect ids[2] and ids[3].
	if len(got) != 2 {
		t.Fatalf("ReplayFrom: want 2 events, got %d", len(got))
	}
	if got[0] != ids[2] {
		t.Errorf("ReplayFrom: got[0] = %v, want %v", got[0], ids[2])
	}
	if got[1] != ids[3] {
		t.Errorf("ReplayFrom: got[1] = %v, want %v", got[1], ids[3])
	}
}

// TestBusImpl_ReplayFrom_FiltersEventsByConsumerPattern asserts that events
// not matching the consumer's EventPattern are not delivered during ReplayFrom.
func TestBusImpl_ReplayFrom_FiltersEventsByConsumerPattern(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 4)
	// Alternate between two event types.
	ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(ids[0], "run_started"))
	ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(ids[1], "run_completed"))
	ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(ids[2], "run_started"))
	ev014dWriteRawEvent(t, fix.logPath, ev014dMakeEvent(ids[3], "run_completed"))

	var mu sync.Mutex
	var got []string

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	// Subscribe only to run_completed.
	sub := core.Subscription{
		ConsumerID:    "type-filtered",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeRunCompleted: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			mu.Lock()
			got = append(got, ev.Type)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Replay from the very beginning (zero EventID).
	if err := bus.ReplayFrom("type-filtered", core.EventID{}); err != nil {
		t.Fatalf("ReplayFrom: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Should receive only run_completed events (ids[1] and ids[3]).
	if len(got) != 2 {
		t.Fatalf("ReplayFrom filter: want 2 run_completed events, got %d (%v)", len(got), got)
	}
	for _, tp := range got {
		if tp != string(core.EventTypeRunCompleted) {
			t.Errorf("ReplayFrom filter: unexpected event type %q; want run_completed", tp)
		}
	}
}

// TestBusImpl_ReplayFrom_UnknownConsumerReturnsError asserts that an unknown
// consumer ID returns a non-nil error.
func TestBusImpl_ReplayFrom_UnknownConsumerReturnsError(t *testing.T) {
	fix := ev014dSetup(t)

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	err := bus.ReplayFrom("nonexistent", core.EventID{})
	if err == nil {
		t.Error("ReplayFrom unknown consumer: want non-nil error, got nil")
	}
}

// TestBusImpl_ReplayFrom_EmptyPathNoOp asserts that ReplayFrom is a no-op
// when the bus has no JSONL path configured.
func TestBusImpl_ReplayFrom_EmptyPathNoOp(t *testing.T) {
	called := false
	bus := eventbus.NewBusImplWithWriterAndHWM(nil, nil, nil, "", "")
	sub := core.Subscription{
		ConsumerID:    "no-path",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			called = true
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := bus.ReplayFrom("no-path", core.EventID{}); err != nil {
		t.Errorf("ReplayFrom with empty path: want nil error, got %v", err)
	}
	if called {
		t.Error("ReplayFrom with empty path: handler must not be called")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DeadLetterReplay
// ─────────────────────────────────────────────────────────────────────────────

// ev014dWriteDeadLetterEvent appends one entry to the dead-letters.jsonl file
// adjacent to logPath.  The format mirrors the unexported deadLetterEntry struct
// inside busimpl.go: {"envelope": <event>}.
func ev014dWriteDeadLetterEvent(t *testing.T, logPath string, ev core.Event) {
	t.Helper()
	dlPath := filepath.Join(filepath.Dir(logPath), "dead-letters.jsonl")
	entry := fmt.Sprintf(`{"envelope":%s}`, func() string {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal dead-letter event: %v", err)
		}
		return string(b)
	}())
	f, err := os.OpenFile(dlPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatalf("open dead-letter log: %v", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintln(f, entry); err != nil {
		t.Fatalf("write dead-letter entry: %v", err)
	}
}

// TestBusImpl_DeadLetterReplay_DeliversMatchingEvents asserts that
// DeadLetterReplay dispatches every dead-letter entry that matches the
// consumer's EventPattern to its handler (nil filter = no extra narrowing).
func TestBusImpl_DeadLetterReplay_DeliversMatchingEvents(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 3)
	for _, id := range ids {
		ev014dWriteDeadLetterEvent(t, fix.logPath, ev014dMakeEvent(id, "run_failed"))
	}

	var mu sync.Mutex
	var got []core.EventID

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	sub := core.Subscription{
		ConsumerID:    "dl-consumer",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			mu.Lock()
			got = append(got, ev.EventID)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if err := bus.DeadLetterReplay("dl-consumer", nil); err != nil {
		t.Fatalf("DeadLetterReplay: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("DeadLetterReplay: want 3 events, got %d", len(got))
	}
	for i, id := range ids {
		if got[i] != id {
			t.Errorf("DeadLetterReplay: got[%d] = %v, want %v", i, got[i], id)
		}
	}
}

// TestBusImpl_DeadLetterReplay_FilterNarrowsDelivery asserts that a non-nil
// filter further constrains which dead-letter events reach the consumer's
// handler: only events matching BOTH the consumer pattern AND the filter pass.
func TestBusImpl_DeadLetterReplay_FilterNarrowsDelivery(t *testing.T) {
	fix := ev014dSetup(t)

	ids := ev014dMonotonicIDs(t, 4)
	ev014dWriteDeadLetterEvent(t, fix.logPath, ev014dMakeEvent(ids[0], "run_failed"))
	ev014dWriteDeadLetterEvent(t, fix.logPath, ev014dMakeEvent(ids[1], "run_started"))
	ev014dWriteDeadLetterEvent(t, fix.logPath, ev014dMakeEvent(ids[2], "run_failed"))
	ev014dWriteDeadLetterEvent(t, fix.logPath, ev014dMakeEvent(ids[3], "run_started"))

	var mu sync.Mutex
	var got []string

	bus := eventbus.NewBusImplWithWriterAndHWM(nil, fix.writer, nil, "", fix.logPath)
	// Consumer pattern = wildcard (receives all types).
	sub := core.Subscription{
		ConsumerID:    "dl-filtered",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			mu.Lock()
			got = append(got, ev.Type)
			mu.Unlock()
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	// Replay only run_failed entries via the filter.
	filter := &core.EventPattern{
		Types: map[core.EventType]struct{}{
			core.EventTypeRunFailed: {},
		},
	}
	if err := bus.DeadLetterReplay("dl-filtered", filter); err != nil {
		t.Fatalf("DeadLetterReplay with filter: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Only ids[0] and ids[2] are run_failed; the two run_started entries should be skipped.
	if len(got) != 2 {
		t.Fatalf("DeadLetterReplay filter: want 2 run_failed events, got %d (%v)", len(got), got)
	}
	for _, tp := range got {
		if tp != string(core.EventTypeRunFailed) {
			t.Errorf("DeadLetterReplay filter: unexpected type %q; want run_failed", tp)
		}
	}
}
