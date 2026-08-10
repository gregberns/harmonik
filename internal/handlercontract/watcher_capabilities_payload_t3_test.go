package handlercontract_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

type watcherCapabilityEvent struct {
	eventType core.EventType
	payload   []byte
}

type watcherCapabilityPublisher struct {
	mu     sync.Mutex
	events []watcherCapabilityEvent
}

func (p *watcherCapabilityPublisher) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, watcherCapabilityEvent{eventType: eventType, payload: bytes.Clone(payload)})
	return nil
}

func (p *watcherCapabilityPublisher) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return p.Emit(ctx, eventType, payload)
}

func (p *watcherCapabilityPublisher) Events() []watcherCapabilityEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]watcherCapabilityEvent(nil), p.events...)
}

type watcherCapabilityDeadLetter struct {
	mu     sync.Mutex
	events []watcherCapabilityDeadLetterEvent
}

type watcherCapabilityDeadLetterEvent struct {
	eventType core.EventType
	payload   []byte
	reason    string
}

func (d *watcherCapabilityDeadLetter) Append(eventType core.EventType, payload []byte, reason string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, watcherCapabilityDeadLetterEvent{
		eventType: eventType,
		payload:   bytes.Clone(payload),
		reason:    reason,
	})
	return nil
}

func (d *watcherCapabilityDeadLetter) Events() []watcherCapabilityDeadLetterEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]watcherCapabilityDeadLetterEvent(nil), d.events...)
}

func TestWatcher_HandlerCapabilitiesUsesConfiguredRunIDWithoutMachine(t *testing.T) {
	runID := core.RunID(uuid.MustParse("018f6a48-7d44-7bf5-8000-000000000001"))
	wire := []byte(`{"type":"handler_capabilities","supported_versions":[1,2],"claude_session_id":"claude-session"}`)
	pub := &watcherCapabilityPublisher{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("capabilities-session"),
		RunID:          runID,
		ProgressStream: strings.NewReader(string(wire) + "\n"),
		Publisher:      pub,
		DeadLetter:     &watcherCapabilityDeadLetter{},
	})
	watcherFixtureWait(t, w)

	events := pub.Events()
	if len(events) != 1 {
		t.Fatalf("published events = %d, want 1", len(events))
	}
	if events[0].eventType != core.EventTypeHandlerCapabilities {
		t.Fatalf("published type = %q, want %q", events[0].eventType, core.EventTypeHandlerCapabilities)
	}
	if bytes.Equal(events[0].payload, wire) {
		t.Fatal("handler_capabilities published its raw wire payload")
	}

	event := core.Event{Type: core.EventTypeHandlerCapabilities, Payload: events[0].payload}
	decoded, err := event.DecodePayload()
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	payload, ok := decoded.(*core.HandlerCapabilitiesPayload)
	if !ok {
		t.Fatalf("DecodePayload type = %T, want *core.HandlerCapabilitiesPayload", decoded)
	}
	if payload.RunID != runID {
		t.Errorf("RunID = %s, want %s", payload.RunID, runID)
	}
	if payload.SessionID != "capabilities-session" {
		t.Errorf("SessionID = %q, want %q", payload.SessionID, "capabilities-session")
	}
	if got, want := strings.Join(payload.ProtocolVersionsSupported, ","), "1,2"; got != want {
		t.Errorf("ProtocolVersionsSupported = %q, want %q", got, want)
	}
	if payload.ClaudeSessionID == nil || *payload.ClaudeSessionID != "claude-session" {
		t.Errorf("ClaudeSessionID = %v, want claude-session", payload.ClaudeSessionID)
	}
	if !payload.Valid() {
		t.Error("typed handler_capabilities payload is invalid")
	}
}

func TestWatcher_HandlerCapabilitiesWithoutRunIDDoesNotPublishRawWire(t *testing.T) {
	wire := []byte(`{"type":"handler_capabilities","supported_versions":[1]}`)
	pub := &watcherCapabilityPublisher{}
	dl := &watcherCapabilityDeadLetter{}

	w := handlercontract.SpawnWatcher(t.Context(), handlercontract.SpawnWatcherConfig{
		SessionID:      core.SessionID("missing-run-id-session"),
		ProgressStream: strings.NewReader(string(wire) + "\n"),
		Publisher:      pub,
		DeadLetter:     dl,
	})
	watcherFixtureWait(t, w)

	if events := pub.Events(); len(events) != 0 {
		t.Fatalf("published events = %d, want 0", len(events))
	}
	deadLetters := dl.Events()
	if len(deadLetters) != 1 {
		t.Fatalf("dead-letter events = %d, want 1", len(deadLetters))
	}
	if deadLetters[0].eventType != core.EventTypeHandlerCapabilities {
		t.Errorf("dead-letter type = %q, want %q", deadLetters[0].eventType, core.EventTypeHandlerCapabilities)
	}
	if !bytes.Equal(deadLetters[0].payload, wire) {
		t.Error("dead-letter payload does not retain the original wire message")
	}
	if !strings.Contains(deadLetters[0].reason, "no valid run ID") {
		t.Errorf("dead-letter reason = %q, want missing run ID", deadLetters[0].reason)
	}
}
