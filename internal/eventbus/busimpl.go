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
// At MVH the implementation wires EV-035 redaction (HC-031 common-prefix rule
// via handlercontract.RedactByFieldName) into Emit before JSONL append and
// consumer dispatch. Per-handler value-pattern redaction (HC-032) is added by
// the sibling bead hk-8i31.83 which refactors Emit to call the
// RedactionRegistry middleware instead of RedactByFieldName directly.
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014a, EV-035.
// Bead ref: hk-8mup.62.
type busImpl struct {
	mu            sync.Mutex
	subscriptions []core.Subscription
	sealed        bool
}

// NewBusImpl constructs a new busImpl ready for subscription registration.
//
// The returned bus is unsealed; callers MUST call Subscribe for all consumers
// before calling Seal (EV-009). The returned value satisfies [EventBus].
//
// At MVH, redaction is HC-031 (RedactByFieldName) only. The per-handler
// RedactionRegistry (HC-032) is wired by the sibling bead hk-8i31.83, which
// refactors NewBusImpl to accept a registry parameter.
//
// Spec ref: specs/event-model.md §6.1, §4.2 PL-005 step 0.
func NewBusImpl() EventBus {
	return &busImpl{}
}

// Emit applies EV-035 redaction to payload, then dispatches to all matching
// registered consumers.
//
// For this initial implementation (hk-8mup.62), JSONL persistence is deferred
// because the JSONLWriter requires a daemon-startup-resolved file path that is
// not yet threaded through daemon.Config. The redaction + dispatch path is
// fully exercised. JSONL wiring is added by the JSONL-path bead.
//
// Redaction is HC-031 (RedactByFieldName) only; HC-032 per-handler patterns
// are composed in by the sibling hk-8i31.83 RedactionRegistry refactor.
//
// Spec ref: specs/event-model.md §6.1, §7.1, §4.4 EV-035.
func (b *busImpl) Emit(ctx context.Context, eventType core.EventType, payload []byte) error {
	// Step 1: decode payload to map for field-name redaction (EV-035 / HC-031).
	var rawPayload map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &rawPayload); err != nil {
			return fmt.Errorf("eventbus.Emit: payload unmarshal for redaction: %w", err)
		}
	}

	// Step 2: apply HC-031 common-prefix redaction BEFORE JSONL append and
	// consumer dispatch (EV-035).
	redacted := handlercontract.RedactByFieldName(rawPayload)

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
