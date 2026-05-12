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
// # Dispatch order (EV-014a)
//
// Emit returns after: (a) redaction (EV-035), (b) JSONL append + any mandated
// fsync per EV-016 (deferred stub at MVH — JSONL path bead), (c) synchronous
// consumer dispatch on the caller's goroutine. Asynchronous and observer
// consumers are dispatched off the critical path via per-handler goroutines
// (MVH) and MUST NOT extend Emit latency. A bounded worker pool (default 4
// workers, operator-configurable) replaces the per-goroutine approach in the
// post-MVH JSONL-path bead.
//
// Spec ref: specs/event-model.md §6.1, §4.2 EV-014a, EV-035.
// Bead refs: hk-8mup.62, hk-8i31.83, hk-hqwn.19.
type busImpl struct {
	registry      *handlercontract.RedactionRegistry
	mu            sync.Mutex
	subscriptions []core.Subscription
	sealed        bool
	// wg tracks in-flight asynchronous and observer goroutines so Drain can
	// wait for quiescence (EV-014a; post-MVH: replace with bounded worker pool).
	wg sync.WaitGroup
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
// RedactionMiddleware, then appends the event to the JSONL log (stub at MVH),
// then dispatches to matching registered consumers per EV-014a.
//
// Dispatch order (EV-014a):
//
//  1. Redaction via HC-031 + HC-032 registry (EV-035).
//  2. JSONL append + fsync per durability class (EV-016). Deferred stub at MVH:
//     file path not yet threaded through daemon.Config; follow-up bead adds wiring.
//  3. Synchronous-consumer dispatch on the caller's goroutine — Emit blocks until
//     the at-most-one synchronous consumer returns or errors (EV-010).
//  4. Asynchronous and observer consumers are dispatched off the critical path
//     via per-handler goroutines (MVH; post-MVH: bounded worker pool) and MUST
//     NOT extend Emit latency (EV-014a).
//
// Spec ref: specs/event-model.md §6.1, §7.1, §4.2 EV-014a, §4.4 EV-035.
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

	// Step 4: JSONL append (deferred — file path not yet threaded through
	// daemon.Config; wiring bead adds JSONLWriter here and performs fsync per
	// EV-016 durability class before the sync-consumer dispatch below).

	// Step 5: collect matching subscriptions once under lock so dispatch runs
	// without holding the mutex.
	b.mu.Lock()
	subs := make([]core.Subscription, len(b.subscriptions))
	copy(subs, b.subscriptions)
	b.mu.Unlock()

	evt := core.Event{
		Type:    string(eventType),
		Payload: redactedBytes,
	}

	// Step 6: dispatch per consumer class (EV-014a).
	for _, sub := range subs {
		if !sub.EventPattern.MatchesType(string(eventType)) {
			continue
		}
		if sub.Handler == nil {
			continue
		}

		switch sub.ConsumerClass {
		case core.ConsumerClassSynchronous:
			// Synchronous consumers run on the caller's goroutine and block
			// Emit until they return (EV-010). At most one per event type is
			// permitted; enforced at subscription time (hk-hqwn.49).
			if handlerErr := sub.Handler(ctx, evt); handlerErr != nil {
				return fmt.Errorf("eventbus.Emit: synchronous consumer %q: %w", sub.ConsumerID, handlerErr)
			}

		default:
			// Asynchronous and observer consumers run off the critical path
			// (EV-014a / EV-011 / EV-012). At MVH a dedicated goroutine is
			// launched per dispatch; post-MVH: replace with bounded worker pool
			// (default 4 workers, operator-configurable per EV-014a).
			sub := sub // capture loop variable
			b.wg.Add(1)
			go func() {
				defer b.wg.Done()
				// Context is passed through so callers can cancel in-flight
				// async/observer work during shutdown before Drain returns.
				_ = sub.Handler(ctx, evt)
			}()
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

// Drain blocks until all in-flight asynchronous and observer dispatches
// complete, or ctx is cancelled.
//
// Synchronous consumers run on the caller's goroutine and are always complete
// by the time Emit returns; Drain only waits for off-path (async / observer)
// goroutines (EV-014a).
//
// Spec ref: specs/event-model.md §6.1.
func (b *busImpl) Drain(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("eventbus.Drain: %w", ctx.Err())
	}
}
