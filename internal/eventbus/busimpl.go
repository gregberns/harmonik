package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// busImpl is the concrete in-process implementation of [EventBus].
//
// Emit applies the full EV-035 redaction pipeline before JSONL append and
// consumer dispatch: HC-031 common-prefix field-name redaction PLUS HC-032
// per-handler value-pattern redaction via the [handlercontract.RedactionRegistry]
// supplied at construction.
//
// When constructed via [NewBusImpl] (no patterns), the registry applies HC-031
// only. When constructed via [NewBusImplWithRegistry], the caller's patterns
// are also applied.
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014a, EV-035.
// Bead refs: hk-8mup.62, hk-8i31.83.
type busImpl struct {
	registry      *handlercontract.RedactionRegistry
	mu            sync.Mutex
	subscriptions []core.Subscription
	sealed        bool
}

// NewBusImpl constructs a busImpl with a zero-pattern RedactionRegistry.
//
// This constructor provides backward compatibility for call sites that do not
// yet have a RedactionRegistry available. Redaction applies HC-031
// (common-prefix field names) only. Callers that need HC-032 per-handler
// value-pattern redaction MUST use [NewBusImplWithRegistry].
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-035, PL-005 step 0.
func NewBusImpl() EventBus {
	return &busImpl{registry: handlercontract.NewRedactionRegistry()}
}

// NewBusImplWithRegistry constructs a busImpl that delegates all redaction to
// the supplied [handlercontract.RedactionRegistry].
//
// The registry MUST be fully populated (all RegisterPattern calls complete)
// before the bus is sealed per PL-005 step 0. Passing a nil registry is
// equivalent to calling [NewBusImpl].
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-035; specs/handler-contract.md §4.7.HC-032.
func NewBusImplWithRegistry(registry *handlercontract.RedactionRegistry) EventBus {
	if registry == nil {
		return NewBusImpl()
	}
	return &busImpl{registry: registry}
}

// Emit applies EV-035 redaction to payload via the registry's
// RedactionMiddleware, then dispatches to all matching registered consumers.
//
// JSONL persistence is deferred because the JSONLWriter requires a
// daemon-startup-resolved file path not yet threaded through daemon.Config.
// The redaction + dispatch path is fully exercised. JSONL wiring is added by
// the JSONL-path bead.
//
// Spec ref: specs/event-model.md §6.1, §7.1, §4.4 EV-035.
func (b *busImpl) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	// Step 1: decode payload to map for redaction (EV-035).
	var rawPayload map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &rawPayload); err != nil {
			return fmt.Errorf("eventbus.Emit: payload unmarshal for redaction: %w", err)
		}
	}

	// Step 2: apply HC-031 + HC-032 redaction pipeline BEFORE JSONL append
	// and consumer dispatch (EV-035).
	redacted := b.registry.RedactionMiddleware(rawPayload)

	// Step 3: re-encode redacted payload.
	redactedBytes, err := json.Marshal(redacted)
	if err != nil {
		return fmt.Errorf("eventbus.Emit: re-encoding redacted payload: %w", err)
	}

	// Step 4: JSONL append (deferred — file path not yet wired at this bead
	// scope). Redacted bytes are available here for the wiring bead.

	// Step 5: dispatch to matching consumers.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(string(eventType)) {
			continue
		}
		// Synchronous consumers are invoked on the caller's goroutine (EV-010).
		// Asynchronous / observer dispatch is a follow-on concern (EV-014a).
		if sub.Handler != nil {
			evt := core.Event{
				Type:    string(eventType),
				Payload: redactedBytes,
			}
			if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
				return fmt.Errorf("eventbus.Emit: consumer %q: %w", sub.ConsumerID, handlerErr)
			}
		}
	}
	return nil
}

// Subscribe registers a consumer with the bus.
//
// Returns a typed error if called after Seal (EV-009).
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-009.
func (b *busImpl) Subscribe(sub core.Subscription) (core.Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.sealed {
		return core.Subscription{}, fmt.Errorf("eventbus: Subscribe called after Seal (EV-009): consumer %q", sub.ConsumerID)
	}
	b.subscriptions = append(b.subscriptions, sub)
	return sub, nil
}

// Seal closes the subscription-registration window.
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-009.
func (b *busImpl) Seal() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sealed = true
	return nil
}

// ReplayFrom re-issues JSONL events to the named consumer (stub — JSONL path
// wiring is deferred to the JSONL bead).
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014b.
func (b *busImpl) ReplayFrom(_ string, _ core.EventID) error {
	return nil
}

// DeadLetterReplay replays events from the dead-letter log (stub).
//
// Spec ref: specs/event-model.md §6.1, §6.2, §4.2 EV-011.
func (b *busImpl) DeadLetterReplay(_ string, _ *core.EventPattern) error {
	return nil
}

// Drain blocks until all in-flight dispatches complete.
//
// At MVH (synchronous dispatch only) this is a no-op; the async worker pool
// is a follow-on concern per EV-014a.
//
// Spec ref: specs/event-model.md §6.1.
func (b *busImpl) Drain(_ context.Context) error {
	return nil
}
