package daemon

// handlerpause_policy_37zy8.go — handler-pause policy goroutine (hk-37zy8).
//
// HandlerPausePolicyGoroutine subscribes to daemon outcome events and calls
// HandlerPauseController.Pause when a trip condition is met:
//
//   - Rate-limit hysteresis: trip on TWO consecutive agent_rate_limit_status
//     active events for the same agent_type without an intervening cleared event;
//     reset the counter on cleared.
//
//   - Budget exhaustion: trip immediately on a budget_exhausted event that maps
//     to an agent handler account (single-hit, no hysteresis).
//
// The controller's Pause method is idempotent on double-trip (second call while
// already paused is a no-op), so concurrent or duplicate events are safe.
//
// The policy goroutine does NOT emit handler_paused — the controller already
// does so inside Pause.
//
// Failure-class taxonomy: minimum for THIS bead (rate-limit + budget-exhausted).
// TODO(hk-107gz): expand the failure-class taxonomy with further classes.
//
// Architecture placement: internal/daemon/ — same reasoning as
// HandlerPauseController: the policy goroutine needs access to RunRegistry
// (for the in-flight freeze-list) and HandlerPauseController, both of which live
// in the composition root package.
//
// Spec ref: specs/handler-pause.md §7 (HandlerPauseController contract).
// Bead ref: hk-37zy8.

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// rateLimitHysteresisCount is the number of consecutive rate-limit active
// events required to trip a pause.  Two consecutive hits (without clearance)
// trigger a pause.  This is the minimum hysteresis per hk-37zy8.
const rateLimitHysteresisCount = 2

// HandlerPausePolicyConfig carries the configuration parameters for
// NewHandlerPausePolicyGoroutine.
type HandlerPausePolicyConfig struct {
	// AgentType is the handler type this policy instance monitors.
	// At MVH, one policy governs all agent types; we use a single instance
	// for AgentTypeClaudeCode.  Required; must satisfy AgentType.Valid().
	AgentType core.AgentType

	// Controller is the HandlerPauseController to call Pause on when a trip
	// condition is met.  Required; must not be nil.
	Controller *HandlerPauseController

	// Registry is the in-flight run registry used to build the freeze-list at
	// pause time.  Required; must not be nil.
	Registry *RunRegistry
}

// HandlerPausePolicyGoroutine is the daemon-side component that watches
// event-bus events and triggers pauses on the HandlerPauseController.
//
// Lifecycle: call Run(ctx) in a goroutine; cancel ctx to stop.
// Subscribe MUST be called before the bus is sealed (EV-009).
type HandlerPausePolicyGoroutine struct {
	cfg HandlerPausePolicyConfig

	// mu guards hysteresis state.
	mu sync.Mutex

	// rateLimitConsecutive is the consecutive-hit counter for rate-limit events
	// per agent type.  Reset to 0 on a cleared event for the same agent type.
	// Trip condition: rateLimitConsecutive >= rateLimitHysteresisCount.
	rateLimitConsecutive map[core.AgentType]int
}

// NewHandlerPausePolicyGoroutine creates a new HandlerPausePolicyGoroutine
// from the given config.  Subscribe must be called before the bus is sealed.
func NewHandlerPausePolicyGoroutine(cfg HandlerPausePolicyConfig) *HandlerPausePolicyGoroutine {
	return &HandlerPausePolicyGoroutine{
		cfg:                  cfg,
		rateLimitConsecutive: make(map[core.AgentType]int),
	}
}

// Subscribe registers the policy goroutine's event consumers with the bus.
//
// Must be called before bus.Seal (EV-009).  The policy subscribes to two
// event types as asynchronous consumers:
//
//   - agent_rate_limit_status — rate-limit hysteresis logic
//   - budget_exhausted        — single-hit budget-exhausted logic
//
// This method does NOT start any goroutine; event delivery is handled by the
// bus worker pool for asynchronous consumers.
func (p *HandlerPausePolicyGoroutine) Subscribe(bus eventbus.EventBus) error {
	rateLimitSub := core.Subscription{
		ConsumerID:    "handler-pause-policy-rate-limit-" + string(p.cfg.AgentType),
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeAgentRateLimitStatus): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: p.handleRateLimitStatus,
	}
	if _, err := bus.Subscribe(rateLimitSub); err != nil {
		return fmt.Errorf("HandlerPausePolicyGoroutine.Subscribe: rate-limit consumer: %w", err)
	}

	budgetSub := core.Subscription{
		ConsumerID:    "handler-pause-policy-budget-exhausted-" + string(p.cfg.AgentType),
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeBudgetExhausted): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: p.handleBudgetExhausted,
	}
	if _, err := bus.Subscribe(budgetSub); err != nil {
		return fmt.Errorf("HandlerPausePolicyGoroutine.Subscribe: budget-exhausted consumer: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// handleRateLimitStatus — rate-limit hysteresis logic
// ---------------------------------------------------------------------------

// handleRateLimitStatus is the event handler for agent_rate_limit_status events.
//
// Hysteresis rule:
//   - On status=active: increment the consecutive counter for the event's agent type.
//     If the counter reaches rateLimitHysteresisCount, trip the pause.
//   - On status=cleared: reset the consecutive counter to 0 for the event's agent type.
//
// The counter resets on any cleared event.  A single active event does not trip;
// two consecutive active events without clearance do.
func (p *HandlerPausePolicyGoroutine) handleRateLimitStatus(ctx context.Context, evt core.Event) error {
	var payload core.AgentRateLimitStatusPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		// Malformed payload — skip; the bus dead-letter path handles persistent failures.
		return fmt.Errorf("handler-pause-policy: rate-limit: unmarshal: %w", err)
	}
	if !payload.Valid() {
		return nil // silently skip invalid payloads
	}

	// Use the configured agent type for policy decisions.
	// At MVH all beads use claude-code; if the payload's run_id belongs to a
	// different type we still apply to our configured agent type since AgentType
	// is not on the payload.
	agentType := p.cfg.AgentType

	switch payload.Status {
	case core.AgentRateLimitStatusCleared:
		p.mu.Lock()
		p.rateLimitConsecutive[agentType] = 0
		p.mu.Unlock()

	case core.AgentRateLimitStatusActive:
		p.mu.Lock()
		p.rateLimitConsecutive[agentType]++
		count := p.rateLimitConsecutive[agentType]
		p.mu.Unlock()

		if count >= rateLimitHysteresisCount {
			// Trip condition met: pause the handler.
			cause := core.HandlerPauseCause{
				FailureClass: core.FailureClassTransient,
				SubReason:    "rate_limit",
				SourceRunID:  payload.RunID.String(),
				SourceBeadID: string(p.cfg.AgentType), // best-effort at MVH; no bead on payload
				TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			}
			inFlight := p.buildInFlightList()
			if err := p.cfg.Controller.Pause(ctx, agentType, cause, inFlight); err != nil {
				return fmt.Errorf("handler-pause-policy: rate-limit: Pause: %w", err)
			}
			// Schedule auto-resume if the provider reported a retry_after window
			// (hk-0otqs).  The controller applies flap-backoff internally.
			if payload.RetryAfterSeconds != nil && *payload.RetryAfterSeconds > 0 {
				after := time.Duration(*payload.RetryAfterSeconds) * time.Second
				p.cfg.Controller.Schedule(ctx, agentType, after)
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// handleBudgetExhausted — budget-exhaustion single-hit logic
// ---------------------------------------------------------------------------

// handleBudgetExhausted is the event handler for budget_exhausted events.
//
// Single-hit rule: any budget_exhausted event trips a pause immediately.
// The controller's Pause is idempotent on double-trip.
//
// Sub-reason: "budget_exhausted_handler_account" per the specs/handler-pause.md
// §5 trigger taxonomy.
func (p *HandlerPausePolicyGoroutine) handleBudgetExhausted(ctx context.Context, evt core.Event) error {
	var payload core.BudgetExhaustedEventPayload
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		return fmt.Errorf("handler-pause-policy: budget-exhausted: unmarshal: %w", err)
	}
	if !payload.Valid() {
		return nil // silently skip invalid payloads
	}

	agentType := p.cfg.AgentType

	cause := core.HandlerPauseCause{
		FailureClass: core.FailureClassBudgetExhausted,
		SubReason:    "budget_exhausted_handler_account",
		SourceRunID:  payload.RunID.String(),
		SourceBeadID: string(agentType), // best-effort at MVH; budget_exhausted carries no bead_id
		TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
	inFlight := p.buildInFlightList()
	if err := p.cfg.Controller.Pause(ctx, agentType, cause, inFlight); err != nil {
		return fmt.Errorf("handler-pause-policy: budget-exhausted: Pause: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// buildInFlightList — snapshot RunRegistry for the freeze-list
// ---------------------------------------------------------------------------

// buildInFlightList returns the set of in-flight runs for the configured agent
// type at the moment the pause is triggered.
//
// At MVH all runs use AgentTypeClaudeCode, so this is effectively "all in-flight
// runs".  Post-MVH, once per-bead agent-type resolution lands (see
// ResolvedAgentType future-work comment in handlerpause_9hwbw.go), this can be
// filtered by the run's actual agent type.
func (p *HandlerPausePolicyGoroutine) buildInFlightList() []InFlightBeadRecord {
	// Snapshot the registry under its own read lock.
	type runEntry struct {
		runID  core.RunID
		handle *RunHandle
	}

	// RunRegistry.Snapshot returns []*RunHandle but not the keys; we need to
	// iterate in a way that preserves runID.  Use the internal snap approach.
	// Since RunRegistry exports only Snapshot (which drops keys), we iterate via
	// a helper that accesses the map directly.  At MVH, Snapshot is the only
	// public accessor; we build the freeze-list from it.
	//
	// NOTE: RunRegistry.Snapshot does not return the runID keys.  We use a
	// workaround: snapshot returns []*RunHandle; for the freeze-list we need
	// (runID, handle) pairs.  Since RunRegistry does not expose an iterator with
	// keys, we use an internal accessor added here via snapshotWithKeys.
	snap := p.cfg.Registry.snapshotWithKeys()

	out := make([]InFlightBeadRecord, 0, len(snap))
	for runID, handle := range snap {
		out = append(out, InFlightBeadRecordFromRunHandle(runID, handle))
	}
	return out
}
