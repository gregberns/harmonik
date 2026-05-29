// Package hooksystem implements S05 — the Hook System dispatcher.
//
// S05 is the subsystem that owns Hook dispatch: subscribing to the event bus,
// ordering matching hooks, evaluating each hook's subscription filter and
// evaluator, applying the resulting side-effect, and emitting hook lifecycle
// events (hook_fired, hook_failed).
//
// Spec ref: specs/control-points.md §4.3 CP-012 through CP-017, CP-016.
// Bead ref: hk-a8bg.11
package hooksystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// hookTriggerPrefix is the `on_` prefix that distinguishes Hook-subscription
// names from raw event-type names per CP-013.
const hookTriggerPrefix = "on_"

// Dispatcher is the S05 Hook System dispatcher.
//
// It subscribes to the EventBus as a ConsumerClassObserver (EV-012 — never
// blocks the calling goroutine, failure has no impact on the critical path per
// CP-012: "Hooks MUST NOT block, halt, or alter the run's transition
// progression").
//
// On each event, Dispatcher:
//  1. Translates the event type to the hook trigger name ("on_" + eventType).
//  2. Calls Registry.LookupByTrigger to find matching Hooks.
//  3. Sorts by SubsystemPriority ascending then DeclarationIndex ascending (CP-014).
//  4. For each Hook: evaluates SubscriptionFilter (if present); evaluates the
//     main mechanism Evaluator; emits hook_fired or hook_failed; respects
//     halt_on_failure (CP-015).
//
// Cognition-tagged evaluators are not yet dispatched by this implementation
// (post-MVH per CP-017). A cognition hook is skipped with a hook_failed event
// carrying ErrorCategoryDeterministic.
//
// Tags: mechanism
// Spec ref: specs/control-points.md §4.3 CP-012 through CP-016.
type Dispatcher struct {
	registry Registry
	bus      eventbus.EventBus
	eval     *core.PolicyExprEvaluator
}

// Registry is the read-only view of the ControlPoint registry consumed by S05.
// S01 and S05 both read from it; only S02 writes.
type Registry interface {
	// LookupByTrigger returns all Hooks whose Trigger.Name matches trigger,
	// sorted by Name ascending (CP-046).
	LookupByTrigger(trigger string) []core.ControlPoint
}

// NewDispatcher constructs a Dispatcher wired to the given registry and bus.
//
// The caller MUST call Subscribe before calling [eventbus.EventBus.Seal].
// NewDispatcher does not call Subscribe; it separates construction from
// subscription so that the caller controls the startup-registration window
// (EV-009).
func NewDispatcher(registry Registry, bus eventbus.EventBus) *Dispatcher {
	return &Dispatcher{
		registry: registry,
		bus:      bus,
		eval:     core.NewPolicyExprEvaluator(core.DefaultPolicyExprEvaluatorConfig()),
	}
}

// Subscribe registers the Dispatcher as an observer consumer on the EventBus.
//
// Must be called before [eventbus.EventBus.Seal]. Returns an error if
// registration fails (e.g., the bus is already sealed).
//
// The consumer ID is "s05.hook-dispatcher" — stable and unique per daemon.
// The EventPattern is wildcard so the dispatcher receives every event and
// translates each to the hook trigger namespace per CP-013.
//
// ConsumerClass is Observer (EV-012): delivery failures do not affect the
// emitting goroutine and the dispatcher MUST NOT return errors from the
// handler that alter the bus dispatch path. This satisfies CP-012's
// requirement that Hooks MUST NOT block, halt, or alter the run's transition
// progression.
func (d *Dispatcher) Subscribe() error {
	sub := core.Subscription{
		ConsumerID:    "s05.hook-dispatcher",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler:       d.handleEvent,
	}
	_, err := d.bus.Subscribe(sub)
	return err
}

// handleEvent is the EventBus handler invoked for every event (observer class).
//
// It translates the event type to the hook trigger namespace, looks up
// matching hooks, sorts them per CP-014, and fires each in order.
//
// Returning a non-nil error from an observer handler has no effect on the
// bus dispatch path per EV-012. The dispatcher returns nil always to avoid
// any observable side-effect on the bus error path.
func (d *Dispatcher) handleEvent(ctx context.Context, ev core.Event) error {
	triggerName := hookTriggerPrefix + ev.Type
	hooks := d.registry.LookupByTrigger(triggerName)
	if len(hooks) == 0 {
		return nil
	}

	// CP-014: sort by SubsystemPriority ascending; within the same priority,
	// by DeclarationIndex ascending (registration/declaration order per spec).
	// LookupByTrigger returns Name-sorted for CP-046; we re-sort here to enforce
	// the priority-first, then declaration-order secondary key.
	sort.SliceStable(hooks, func(i, j int) bool {
		pi := hooks[i].Payload.Hook.SubsystemPriority
		pj := hooks[j].Payload.Hook.SubsystemPriority
		if pi != pj {
			return pi < pj
		}
		return hooks[i].DeclarationIndex < hooks[j].DeclarationIndex
	})

	for _, cp := range hooks {
		halt, err := d.fireHook(ctx, ev, cp)
		if err != nil && halt {
			// CP-015: halt_on_failure = true stops the chain on evaluator error.
			break
		}
	}
	return nil
}

// fireHook evaluates one Hook ControlPoint against the triggering event and
// emits the appropriate hook lifecycle event.
//
// Returns (halt=true, err) when the hook fails with halt_on_failure=true.
// Returns (halt=false, nil) on success or non-halting failure.
func (d *Dispatcher) fireHook(ctx context.Context, ev core.Event, cp core.ControlPoint) (halt bool, _ error) {
	hookPL := cp.Payload.Hook
	hookName := core.HookName(cp.Name)
	triggeringID := ev.EventID
	haltOnFailure := hookPL.HaltOnFailure

	// Evaluate the SubscriptionFilter (CP-013 / §6.1.2). Skip the hook if the
	// filter is present and evaluates to false.
	if hookPL.SubscriptionFilter != nil {
		match, err := d.evalBoolFilter(ctx, string(*hookPL.SubscriptionFilter), ev)
		if err != nil {
			_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, classifyEvalError(ctx, err),
				fmt.Sprintf("subscription_filter evaluation failed: %v", err))
			return haltOnFailure, err
		}
		if !match {
			return false, nil
		}
	}

	switch cp.Evaluator.Mode {
	case core.ModeTagMechanism:
		return d.fireMechanismHook(ctx, ev, cp, hookName, triggeringID, hookPL, haltOnFailure)
	case core.ModeTagCognition:
		// Cognition-tagged hooks are post-MVH (CP-017). Emit hook_failed and
		// continue (cognition hooks default to halt_on_failure=false).
		msg := "cognition-tagged hook evaluator not yet supported (post-MVH CP-017)"
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	default:
		msg := fmt.Sprintf("unknown evaluator mode %q", cp.Evaluator.Mode)
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}
}

// fireMechanismHook evaluates a mechanism-tagged Hook and, when the evaluator
// fires, emits hook_fired with the produced SideEffect.
//
// The mechanism evaluator expression is a boolean expression evaluated against
// the event payload. true → hook fires; false → no-op. The SideEffect is
// constructed from the hook's declared SideEffectKind, Target (hook name), and
// IdempotencyClass per CP-016. When IdempotencyClass is not set on the hook
// declaration the spec default (non-idempotent per §6.3) applies.
//
// TODO(post-MVH): extend to support evaluator expressions that return a full
// SideEffect map {target, payload, idempotency} for richer side-effect control.
func (d *Dispatcher) fireMechanismHook(
	ctx context.Context,
	ev core.Event,
	cp core.ControlPoint,
	hookName core.HookName,
	triggeringID core.EventID,
	hookPL *core.HookPayload,
	haltOnFailure bool,
) (halt bool, _ error) {
	if cp.Evaluator.Expression == nil {
		msg := "mechanism hook has nil evaluator expression"
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	fires, err := d.evalBoolFilter(ctx, string(*cp.Evaluator.Expression), ev)
	if err != nil {
		errMsg := fmt.Sprintf("evaluator expression failed: %v", err)
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, classifyEvalError(ctx, err), errMsg)
		return haltOnFailure, err
	}
	if !fires {
		return false, nil
	}

	// Resolve the effective idempotency class per CP-016. When the hook
	// declaration omits IdempotencyClass (zero value), apply the spec default:
	// non-idempotent (specs/control-points.md §6.3 YAML, §4.3.CP-016).
	ic := hookPL.IdempotencyClass
	if !ic.Valid() {
		ic = core.IdempotencyClassNonIdempotent
	}

	// Build the SideEffect descriptor per CP-012 / CP-016.
	se := core.SideEffect{
		Kind:             hookPL.SideEffectKind,
		Target:           cp.Name,
		IdempotencyClass: ic,
	}

	if err := d.emitHookFired(ctx, ev, hookName, triggeringID, se); err != nil {
		_ = d.emitHookFailed(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient,
			fmt.Sprintf("hook_fired emit failed: %v", err))
		return haltOnFailure, err
	}

	// TODO(post-MVH): apply the side effect (emit event, state mutation, external
	// action). For MVH the hook_fired event is the observable signal; application
	// is deferred pending the per-kind effector registry per CP-016.

	return false, nil
}

// classifyEvalError maps an error from the hook evaluator pipeline to the
// appropriate ErrorCategory per CP-015 / handler-contract.md §4.5:
//
//   - timeout / resource-exhaustion (context cancellation) → ErrorCategoryTransient
//   - schema-violation / type-check / compile / cost-ceiling → ErrorCategoryDeterministic
//
// The incoming ctx is checked separately: when the caller's context was already
// canceled the wall-clock abort in the evaluator masks the root cause, so
// ctx.Err() is the authoritative source for transient classification.
func classifyEvalError(ctx context.Context, err error) core.ErrorCategory {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.ErrorCategoryTransient
	}
	if ctx.Err() != nil {
		return core.ErrorCategoryTransient
	}
	return core.ErrorCategoryDeterministic
}

// evalBoolFilter compiles and evaluates expression as a boolean against the
// event payload. The event payload is decoded into a map[string]any and
// exposed to the expression as the evaluation environment.
//
// Returns (true, nil) when the expression evaluates to bool true.
// Returns (false, nil) when the expression evaluates to bool false or a nil
// result. Returns (false, err) on compile or evaluation failure.
func (d *Dispatcher) evalBoolFilter(ctx context.Context, expression string, ev core.Event) (bool, error) {
	var payloadMap map[string]any
	if len(ev.Payload) > 0 {
		if err := json.Unmarshal(ev.Payload, &payloadMap); err != nil {
			payloadMap = map[string]any{}
		}
	} else {
		payloadMap = map[string]any{}
	}

	prog, _, compileErr := d.eval.Compile(expression, payloadMap)
	if compileErr != nil {
		return false, fmt.Errorf("compile: %w", compileErr)
	}

	result, evalErr := d.eval.Evaluate(ctx, prog, payloadMap)
	if evalErr != nil {
		return false, fmt.Errorf("evaluate: %w", evalErr)
	}

	b, ok := result.Value.(bool)
	return ok && b, nil
}

// emitHookFired emits a hook_fired event carrying the side-effect descriptor
// (specs/event-model.md §8.2.1, HookFiredPayload).
func (d *Dispatcher) emitHookFired(
	ctx context.Context,
	ev core.Event,
	hookName core.HookName,
	triggeringID core.EventID,
	se core.SideEffect,
) error {
	pl := core.HookFiredPayload{
		HookName:             hookName,
		TriggeringEventID:    triggeringID,
		SideEffectDescriptor: se,
	}
	if ev.RunID != nil {
		pl.RunID = ev.RunID
	}

	raw, err := json.Marshal(pl)
	if err != nil {
		return fmt.Errorf("hooksystem: marshal hook_fired: %w", err)
	}

	if ev.RunID != nil {
		return d.bus.EmitWithRunID(ctx, *ev.RunID, "hook_fired", raw)
	}
	return d.bus.Emit(ctx, "hook_fired", raw)
}

// emitHookFailed emits a hook_failed event for an evaluator failure
// (specs/event-model.md §8.2.2, HookFailedPayload).
func (d *Dispatcher) emitHookFailed(
	ctx context.Context,
	ev core.Event,
	hookName core.HookName,
	triggeringID core.EventID,
	category core.ErrorCategory,
	reason string,
) error {
	pl := core.HookFailedPayload{
		HookName:          hookName,
		TriggeringEventID: triggeringID,
		ErrorCategory:     category,
		Reason:            reason,
	}
	if ev.RunID != nil {
		pl.RunID = ev.RunID
	}

	raw, err := json.Marshal(pl)
	if err != nil {
		return fmt.Errorf("hooksystem: marshal hook_failed: %w", err)
	}

	if ev.RunID != nil {
		return d.bus.EmitWithRunID(ctx, *ev.RunID, "hook_failed", raw)
	}
	return d.bus.Emit(ctx, "hook_failed", raw)
}
