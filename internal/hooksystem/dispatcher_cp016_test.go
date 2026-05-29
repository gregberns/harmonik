package hooksystem_test

// dispatcher_cp016_test.go — conformance tests for CP-016 "Hook dispatch is
// owned by S05; delivery is at-least-once".
//
// CP-016 requires:
//  1. S05 MUST deliver each Hook's side-effect at least once.
//  2. Duplicate delivery is acceptable for idempotency_class = idempotent.
//  3. For idempotency_class = non-idempotent, S05 MUST bound delivery to
//     at-most-once via a persisted delivery-receipt mechanism (post-MVH).
//  4. The hook's declared idempotency_class flows through to the
//     SideEffectDescriptor carried in the hook_fired event.
//  5. When idempotency_class is not declared on the hook, the spec default
//     (non-idempotent per §6.3 YAML) applies.
//
// Spec ref: specs/control-points.md §4.3.CP-016, §6.3 YAML.
// Bead ref: hk-a8bg.15
//
// All package-level identifiers use the cp016Fixture prefix per the
// implementer-protocol.md helper-prefix discipline.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/hooksystem"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// cp016FixtureMakeHookCPWithIdempotency builds a Hook ControlPoint with an
// explicit IdempotencyClass on the HookPayload.
func cp016FixtureMakeHookCPWithIdempotency(
	name string,
	triggerEvent string,
	idempotencyClass core.IdempotencyClass,
) core.ControlPoint {
	expr := core.PolicyExpression("true")
	return core.ControlPoint{
		Name:          name,
		Kind:          core.KindHook,
		Trigger:       core.Trigger{Name: triggerEvent},
		Evaluator:     core.Evaluator{Mode: core.ModeTagMechanism, Expression: &expr},
		OutcomeAction: core.OutcomeActionSideEffect,
		Payload: core.KindPayload{
			Hook: &core.HookPayload{
				TriggerEvent:     triggerEvent,
				SideEffectKind:   core.SideEffectKindEmitEvent,
				HaltOnFailure:    false,
				IdempotencyClass: idempotencyClass,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

// cp016FixtureMakeHookCPNoIdempotency builds a Hook ControlPoint WITHOUT an
// explicit IdempotencyClass (zero value), to verify that the spec default
// (non-idempotent per §6.3) is applied by the dispatcher.
func cp016FixtureMakeHookCPNoIdempotency(name, triggerEvent string) core.ControlPoint {
	expr := core.PolicyExpression("true")
	return core.ControlPoint{
		Name:          name,
		Kind:          core.KindHook,
		Trigger:       core.Trigger{Name: triggerEvent},
		Evaluator:     core.Evaluator{Mode: core.ModeTagMechanism, Expression: &expr},
		OutcomeAction: core.OutcomeActionSideEffect,
		Payload: core.KindPayload{
			Hook: &core.HookPayload{
				TriggerEvent:   triggerEvent,
				SideEffectKind: core.SideEffectKindEmitEvent,
				HaltOnFailure:  false,
				// IdempotencyClass deliberately omitted (zero value "").
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

// cp016FixtureCollectFiredDescriptors subscribes an observer that collects
// SideEffectDescriptor from every hook_fired event. Returns the slice pointer
// and the bus-registration function.
func cp016FixtureCollectFiredDescriptors(
	descriptors *[]core.SideEffect,
	mu *sync.Mutex,
) func(eventbus.EventBus) error {
	return func(b eventbus.EventBus) error {
		_, err := b.Subscribe(core.Subscription{
			ConsumerID:    "test.cp016-descriptor-collector",
			ConsumerClass: core.ConsumerClassObserver,
			EventPattern:  core.EventPattern{Wildcard: true},
			OnPanic:       core.OnPanicRecoverAndLog,
			Handler: func(_ context.Context, ev core.Event) error {
				if ev.Type != "hook_fired" {
					return nil
				}
				var pl core.HookFiredPayload
				if err := json.Unmarshal(ev.Payload, &pl); err != nil {
					return nil
				}
				mu.Lock()
				*descriptors = append(*descriptors, pl.SideEffectDescriptor)
				mu.Unlock()
				return nil
			},
		})
		return err
	}
}

// ---------------------------------------------------------------------------
// CP-016: idempotency_class declared as idempotent propagates to hook_fired
// ---------------------------------------------------------------------------

// TestCP016_IdempotentClassPropagatesInHookFired verifies that when a hook
// declares idempotency_class = idempotent, the hook_fired event's
// SideEffectDescriptor carries IdempotencyClassIdempotent.
//
// Spec ref: specs/control-points.md §4.3.CP-016.
func TestCP016_IdempotentClassPropagatesInHookFired(t *testing.T) {
	t.Parallel()

	cp := cp016FixtureMakeHookCPWithIdempotency(
		"idempotent-hook",
		"on_agent_started",
		core.IdempotencyClassIdempotent,
	)
	reg := cp012FixtureNewRegistry(cp)

	var descriptors []core.SideEffect
	var mu sync.Mutex
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector,
		func(b eventbus.EventBus) error {
			disp = hooksystem.NewDispatcher(reg, b)
			return disp.Subscribe()
		},
		cp016FixtureCollectFiredDescriptors(&descriptors, &mu),
	)
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	mu.Lock()
	got := make([]core.SideEffect, len(descriptors))
	copy(got, descriptors)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("CP-016: no hook_fired event emitted for idempotent hook")
	}
	for _, se := range got {
		if se.IdempotencyClass != core.IdempotencyClassIdempotent {
			t.Errorf("CP-016: SideEffectDescriptor.IdempotencyClass = %q, want %q",
				se.IdempotencyClass, core.IdempotencyClassIdempotent)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-016: idempotency_class declared as non-idempotent propagates to hook_fired
// ---------------------------------------------------------------------------

// TestCP016_NonIdempotentClassPropagatesInHookFired verifies that when a hook
// declares idempotency_class = non-idempotent, the hook_fired event's
// SideEffectDescriptor carries IdempotencyClassNonIdempotent.
//
// Spec ref: specs/control-points.md §4.3.CP-016.
func TestCP016_NonIdempotentClassPropagatesInHookFired(t *testing.T) {
	t.Parallel()

	cp := cp016FixtureMakeHookCPWithIdempotency(
		"non-idempotent-hook",
		"on_agent_started",
		core.IdempotencyClassNonIdempotent,
	)
	reg := cp012FixtureNewRegistry(cp)

	var descriptors []core.SideEffect
	var mu sync.Mutex
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector,
		func(b eventbus.EventBus) error {
			disp = hooksystem.NewDispatcher(reg, b)
			return disp.Subscribe()
		},
		cp016FixtureCollectFiredDescriptors(&descriptors, &mu),
	)
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	mu.Lock()
	got := make([]core.SideEffect, len(descriptors))
	copy(got, descriptors)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("CP-016: no hook_fired event emitted for non-idempotent hook")
	}
	for _, se := range got {
		if se.IdempotencyClass != core.IdempotencyClassNonIdempotent {
			t.Errorf("CP-016: SideEffectDescriptor.IdempotencyClass = %q, want %q",
				se.IdempotencyClass, core.IdempotencyClassNonIdempotent)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-016: spec default (non-idempotent) applies when class is not declared
// ---------------------------------------------------------------------------

// TestCP016_DefaultIsNonIdempotentWhenClassOmitted verifies that when a hook
// omits idempotency_class (zero value on HookPayload), the dispatcher applies
// the spec default — non-idempotent per §6.3 YAML — and the hook_fired event's
// SideEffectDescriptor carries IdempotencyClassNonIdempotent.
//
// Spec ref: specs/control-points.md §6.3 "default non-idempotent".
func TestCP016_DefaultIsNonIdempotentWhenClassOmitted(t *testing.T) {
	t.Parallel()

	cp := cp016FixtureMakeHookCPNoIdempotency("no-class-hook", "on_agent_started")
	reg := cp012FixtureNewRegistry(cp)

	var descriptors []core.SideEffect
	var mu sync.Mutex
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector,
		func(b eventbus.EventBus) error {
			disp = hooksystem.NewDispatcher(reg, b)
			return disp.Subscribe()
		},
		cp016FixtureCollectFiredDescriptors(&descriptors, &mu),
	)
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	mu.Lock()
	got := make([]core.SideEffect, len(descriptors))
	copy(got, descriptors)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("CP-016: no hook_fired event emitted when idempotency_class omitted")
	}
	for _, se := range got {
		if se.IdempotencyClass != core.IdempotencyClassNonIdempotent {
			t.Errorf("CP-016: SideEffectDescriptor.IdempotencyClass = %q, want %q (spec default)",
				se.IdempotencyClass, core.IdempotencyClassNonIdempotent)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-016: SideEffectDescriptor is well-formed (Valid()) in hook_fired
// ---------------------------------------------------------------------------

// TestCP016_HookFiredSideEffectDescriptorIsValid verifies that the
// SideEffectDescriptor in every hook_fired event satisfies SideEffect.Valid().
// This asserts that S05 always emits a structurally correct descriptor,
// satisfying the at-least-once delivery floor for well-formed hooks.
//
// Spec ref: specs/control-points.md §4.3.CP-016; event-model.md §8.2.1.
func TestCP016_HookFiredSideEffectDescriptorIsValid(t *testing.T) {
	t.Parallel()

	// Two hooks with different idempotency classes to cover both paths.
	cpIdem := cp016FixtureMakeHookCPWithIdempotency(
		"idem-hook", "on_agent_started", core.IdempotencyClassIdempotent,
	)
	cpNonIdem := cp016FixtureMakeHookCPWithIdempotency(
		"non-idem-hook", "on_agent_started", core.IdempotencyClassNonIdempotent,
	)
	cpNonIdem.DeclarationIndex = 1

	reg := cp012FixtureNewRegistry(cpIdem, cpNonIdem)

	var descriptors []core.SideEffect
	var mu sync.Mutex
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector,
		func(b eventbus.EventBus) error {
			disp = hooksystem.NewDispatcher(reg, b)
			return disp.Subscribe()
		},
		cp016FixtureCollectFiredDescriptors(&descriptors, &mu),
	)
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	mu.Lock()
	got := make([]core.SideEffect, len(descriptors))
	copy(got, descriptors)
	mu.Unlock()

	if len(got) == 0 {
		t.Fatal("CP-016: no hook_fired events emitted")
	}
	for i, se := range got {
		if !se.Valid() {
			t.Errorf("CP-016: SideEffectDescriptor[%d] is invalid: kind=%q target=%q idempotency=%q",
				i, se.Kind, se.Target, se.IdempotencyClass)
		}
	}
}
