package hooksystem_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/hooksystem"
)

func cp016FixtureMakeHookCPWithIdempotency(
	name string,
	idempotencyClass core.IdempotencyClass,
) core.ControlPoint {
	const triggerEvent = "on_agent_started"
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
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

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
					return err
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

// TestCP016_IdempotentClassPropagatesInHookFired verifies that when a hook
// declares idempotency_class = idempotent, the hook_fired event's
// SideEffectDescriptor carries IdempotencyClassIdempotent.
//
// Spec ref: specs/control-points.md §4.3.CP-016.
func TestCP016_IdempotentClassPropagatesInHookFired(t *testing.T) {
	t.Parallel()

	cp := cp016FixtureMakeHookCPWithIdempotency(
		"idempotent-hook",
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

// TestCP016_NonIdempotentClassPropagatesInHookFired verifies that when a hook
// declares idempotency_class = non-idempotent, the hook_fired event's
// SideEffectDescriptor carries IdempotencyClassNonIdempotent.
//
// Spec ref: specs/control-points.md §4.3.CP-016.
func TestCP016_NonIdempotentClassPropagatesInHookFired(t *testing.T) {
	t.Parallel()

	cp := cp016FixtureMakeHookCPWithIdempotency(
		"non-idempotent-hook",
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

// TestCP016_HookFiredSideEffectDescriptorIsValid verifies that the
// SideEffectDescriptor in every hook_fired event satisfies SideEffect.Valid().
// This asserts that S05 always emits a structurally correct descriptor,
// satisfying the at-least-once delivery floor for well-formed hooks.
//
// Spec ref: specs/control-points.md §4.3.CP-016; event-model.md §8.2.1.
func TestCP016_HookFiredSideEffectDescriptorIsValid(t *testing.T) {
	t.Parallel()

	cpIdem := cp016FixtureMakeHookCPWithIdempotency(
		"idem-hook", core.IdempotencyClassIdempotent,
	)
	cpNonIdem := cp016FixtureMakeHookCPWithIdempotency(
		"non-idem-hook", core.IdempotencyClassNonIdempotent,
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
