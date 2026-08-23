// Package hooksystem implements S05 — the Hook System dispatcher.
//
// S05 is the subsystem that owns Hook dispatch: subscribing to the event bus,
// ordering matching hooks, evaluating each hook's subscription filter and
// evaluator, applying the resulting side-effect, and emitting hook lifecycle
// events (hook_fired, hook_failed).
//
// Mechanism-tagged Hook evaluators are dispatched inline. Cognition-tagged
// Hook evaluators are dispatched via [CognitionHookEvaluator] when wired via
// [Dispatcher.WithCognition]; without cognition components, cognition hooks
// fail with ErrorCategoryDeterministic per CP-017 / §4.8.
//
// Spec ref: specs/control-points.md §4.3 CP-012 through CP-017, §4.8 CP-039–CP-042.
// Bead ref: hk-a8bg.11, hk-a8bg.16
package hooksystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

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
//     main mechanism Evaluator or delegates to the cognition evaluator per
//     CP-017; emits hook_fired or hook_failed; respects halt_on_failure (CP-015).
//
// Cognition-tagged evaluators require [WithCognition] to be called before
// [Subscribe]. Without cognition components, cognition hooks emit hook_failed
// with ErrorCategoryDeterministic.
//
// Tags: mechanism
// Spec ref: specs/control-points.md §4.3 CP-012 through CP-017, §4.8 CP-039–CP-042.
type Dispatcher struct {
	registry      Registry
	bus           eventbus.EventBus
	eval          *core.PolicyExprEvaluator
	cognitionEval CognitionHookEvaluator // nil when cognition support is not wired
	verdictWriter VerdictFileWriter      // nil when cognition support is not wired
	verdictReader VerdictReader          // nil when cognition support is not wired
}

// Registry is the read-only view of the ControlPoint registry consumed by S05.
// S01 and S05 both read from it; only S02 writes.
type Registry interface {
	// LookupByTrigger returns all Hooks whose Trigger.Name matches trigger,
	// sorted by Name ascending (CP-046).
	LookupByTrigger(trigger string) []core.ControlPoint
}

func (d *Dispatcher) reportHookFailure(
	ctx context.Context,
	ev core.Event,
	hookName core.HookName,
	triggeringID core.EventID,
	category core.ErrorCategory,
	reason string,
) {
	if err := d.emitHookFailed(ctx, ev, hookName, triggeringID, category, reason); err != nil {
		log.Printf("hooksystem: emit hook_failed for hook %q: %v", hookName, err)
	}
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

// WithCognition wires the cognition components required to dispatch
// cognition-tagged Hook evaluators per CP-017 / §4.8.
//
// Must be called before [Subscribe]. Returns the receiver for chaining.
// eval, writer, and reader must all be non-nil; passing a nil for any
// argument panics to surface misconfiguration at construction time.
func (d *Dispatcher) WithCognition(eval CognitionHookEvaluator, writer VerdictFileWriter, reader VerdictReader) *Dispatcher {
	if eval == nil || writer == nil || reader == nil {
		panic("hooksystem.Dispatcher.WithCognition: eval, writer, and reader must be non-nil")
	}
	d.cognitionEval = eval
	d.verdictWriter = writer
	d.verdictReader = reader
	return d
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

func (d *Dispatcher) handleEvent(ctx context.Context, ev core.Event) error {
	triggerName := hookTriggerPrefix + string(ev.Type)
	hooks := d.registry.LookupByTrigger(triggerName)
	if len(hooks) == 0 {
		return nil
	}

	filtered := hooks[:0]
	for _, cp := range hooks {
		if cp.Kind == core.KindHook {
			filtered = append(filtered, cp)
		}
	}
	hooks = filtered
	if len(hooks) == 0 {
		return nil
	}

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
			break
		}
	}
	return nil
}

func (d *Dispatcher) fireHook(ctx context.Context, ev core.Event, cp core.ControlPoint) (halt bool, _ error) {
	hookPL := cp.Payload.Hook
	hookName := core.HookName(cp.Name)
	triggeringID := ev.EventID
	haltOnFailure := hookPL.HaltOnFailure

	if hookPL.SubscriptionFilter != nil {
		match, err := d.evalBoolFilter(ctx, string(*hookPL.SubscriptionFilter), ev)
		if err != nil {
			d.reportHookFailure(ctx, ev, hookName, triggeringID, classifyEvalError(ctx, err),
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
		return d.fireCognitionHook(ctx, ev, cp, hookName, triggeringID, hookPL, haltOnFailure)
	default:
		msg := fmt.Sprintf("unknown evaluator mode %q", cp.Evaluator.Mode)
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
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
// TODO(deferred): extend to support evaluator expressions that return a full
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
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryDeterministic, msg)
		return haltOnFailure, fmt.Errorf("hooksystem: %s: %s", cp.Name, msg)
	}

	fires, err := d.evalBoolFilter(ctx, string(*cp.Evaluator.Expression), ev)
	if err != nil {
		errMsg := fmt.Sprintf("evaluator expression failed: %v", err)
		d.reportHookFailure(ctx, ev, hookName, triggeringID, classifyEvalError(ctx, err), errMsg)
		return haltOnFailure, err
	}
	if !fires {
		return false, nil
	}

	ic := hookPL.IdempotencyClass
	if !ic.Valid() {
		ic = core.IdempotencyClassNonIdempotent
	}

	se := core.SideEffect{
		Kind:             hookPL.SideEffectKind,
		Target:           cp.Name,
		IdempotencyClass: ic,
	}

	if err := d.emitHookFired(ctx, ev, hookName, triggeringID, se); err != nil {
		d.reportHookFailure(ctx, ev, hookName, triggeringID, core.ErrorCategoryTransient,
			fmt.Sprintf("hook_fired emit failed: %v", err))
		return haltOnFailure, err
	}

	// TODO(deferred): apply the side effect (emit event, state mutation, external
	// action). For now the hook_fired event is the observable signal; application
	// is deferred pending the per-kind effector registry per CP-016.

	return false, nil
}

func classifyEvalError(ctx context.Context, err error) core.ErrorCategory {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return core.ErrorCategoryTransient
	}
	if ctx.Err() != nil {
		return core.ErrorCategoryTransient
	}
	return core.ErrorCategoryDeterministic
}

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
