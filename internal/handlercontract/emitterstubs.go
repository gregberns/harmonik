package handlercontract

import (
	"context"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
)

// CollectingEmitter is an EventEmitter implementation that records every emitted
// event type.  It is safe for concurrent use.
//
// Intended for use by callers (e.g. internal/handler tests) that need a
// concrete EventEmitter without importing internal/core directly.  The caller
// retrieves accumulated event types via EventTypes.
//
// CollectingEmitter never returns an error from Emit.
type CollectingEmitter struct {
	mu         sync.Mutex
	eventTypes []string
}

// Emit records eventType and returns nil.
func (e *CollectingEmitter) Emit(_ context.Context, eventType core.EventType, _ []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventTypes = append(e.eventTypes, string(eventType))
	return nil
}

// EmitWithRunID records eventType (run_id is not stored; CollectingEmitter is
// a test stub and observes only event types).  Returns nil.
func (e *CollectingEmitter) EmitWithRunID(_ context.Context, _ core.RunID, eventType core.EventType, _ []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.eventTypes = append(e.eventTypes, string(eventType))
	return nil
}

// EventTypes returns a snapshot of the collected event type strings in
// emission order.  Safe to call concurrently with Emit.
func (e *CollectingEmitter) EventTypes() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.eventTypes))
	copy(out, e.eventTypes)
	return out
}

// NoopWatcherDeadLetter is a WatcherDeadLetterSink that silently discards all
// events.  Use when the caller does not need to observe dead-letter events.
//
// Intended for use by callers (e.g. internal/handler tests) that need a
// concrete WatcherDeadLetterSink without importing internal/core directly.
type NoopWatcherDeadLetter struct{}

// Append discards the event and returns nil.
func (NoopWatcherDeadLetter) Append(_ core.EventType, _ []byte, _ string) error {
	return nil
}

// NoopDeadLetterSink is a type alias for core.NoopDeadLetterSink, re-exported
// so that handler-side packages can name the type without importing
// internal/core directly (EV-002b boundary).
//
// Bead ref: hk-2m3bq.
type NoopDeadLetterSink = core.NoopDeadLetterSink

// Compile-time interface checks.
var (
	_ EventEmitter          = (*CollectingEmitter)(nil)
	_ WatcherDeadLetterSink = NoopWatcherDeadLetter{}
	_ DeadLetterSink        = NoopDeadLetterSink{}
)
