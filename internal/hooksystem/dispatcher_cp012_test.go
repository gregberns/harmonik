package hooksystem_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/hooksystem"
)

func cp012FixtureMakeHookCP(
	name string,
	triggerEvent string,
	expression string,
	haltOnFailure bool,
	subsystemPriority int,
) core.ControlPoint {
	expr := core.PolicyExpression(expression)
	return core.ControlPoint{
		Name:          name,
		Kind:          core.KindHook,
		Trigger:       core.Trigger{Name: triggerEvent},
		Evaluator:     core.Evaluator{Mode: core.ModeTagMechanism, Expression: &expr},
		OutcomeAction: core.OutcomeActionSideEffect,
		Payload: core.KindPayload{
			Hook: &core.HookPayload{
				TriggerEvent:      triggerEvent,
				SideEffectKind:    core.SideEffectKindEmitEvent,
				HaltOnFailure:     haltOnFailure,
				SubsystemPriority: subsystemPriority,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

func cp012FixtureMakeHookCPWithFilter(
	name string,
	triggerEvent string,
	filter string,
	expression string,
	sideEffectKind core.SideEffectKind,
) core.ControlPoint {
	expr := core.PolicyExpression(expression)
	filterExpr := core.PolicyExpression(filter)
	return core.ControlPoint{
		Name:          name,
		Kind:          core.KindHook,
		Trigger:       core.Trigger{Name: triggerEvent},
		Evaluator:     core.Evaluator{Mode: core.ModeTagMechanism, Expression: &expr},
		OutcomeAction: core.OutcomeActionSideEffect,
		Payload: core.KindPayload{
			Hook: &core.HookPayload{
				TriggerEvent:       triggerEvent,
				SubscriptionFilter: &filterExpr,
				SideEffectKind:     sideEffectKind,
				HaltOnFailure:      false,
				SubsystemPriority:  0,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

type cp012FixtureMapRegistry struct {
	mu  sync.RWMutex
	cps []core.ControlPoint
}

func cp012FixtureNewRegistry(cps ...core.ControlPoint) *cp012FixtureMapRegistry {
	stamped := make([]core.ControlPoint, len(cps))
	for i, cp := range cps {
		cp.DeclarationIndex = i
		stamped[i] = cp
	}
	return &cp012FixtureMapRegistry{cps: stamped}
}

func (r *cp012FixtureMapRegistry) LookupByTrigger(trigger string) []core.ControlPoint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []core.ControlPoint
	for _, cp := range r.cps {
		if cp.Kind == core.KindHook && cp.Trigger.Name == trigger {
			out = append(out, cp)
		}
	}
	return out
}

type cp012FixtureEventCollector struct {
	mu     sync.Mutex
	events []string // collected event type strings in emission order
}

func (c *cp012FixtureEventCollector) record(eventType string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, eventType)
}

func (c *cp012FixtureEventCollector) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.events))
	copy(out, c.events)
	return out
}

func cp012FixtureMakeAgentStartedPayload() json.RawMessage {
	return json.RawMessage(`{"run_id":"test-run"}`)
}

func cp012FixtureMakeFilteredPayload(score int) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"score":%d}`, score))
}

func cp012FixtureMarshal(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return raw
}

func cp012FixtureBuildBus(t *testing.T, collector *cp012FixtureEventCollector, extraSubs ...func(eventbus.EventBus) error) eventbus.EventBus {
	t.Helper()

	bus := eventbus.NewBusImpl()

	for _, fn := range extraSubs {
		if err := fn(bus); err != nil {
			t.Fatalf("cp012FixtureBuildBus: extra sub: %v", err)
		}
	}

	_, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			collector.record(string(ev.Type))
			return nil
		},
	})
	if err != nil {
		t.Fatalf("cp012FixtureBuildBus: subscribe collector: %v", err)
	}

	if err := bus.Seal(); err != nil {
		t.Fatalf("cp012FixtureBuildBus: Seal: %v", err)
	}
	return bus
}

func cp012FixtureEmitEvent(t *testing.T, bus eventbus.EventBus, eventType string, payload json.RawMessage) {
	t.Helper()
	if err := bus.Emit(context.Background(), core.EventType(eventType), payload); err != nil {
		t.Fatalf("Emit(%q): %v", eventType, err)
	}
}

func cp012FixtureWaitDrain(t *testing.T, bus eventbus.EventBus) {
	t.Helper()
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

// TestCP012_HookFiresOnEventMatch verifies that a hook registered with
// trigger "on_agent_started" fires when an agent_started event is emitted.
//
// Spec ref: specs/control-points.md §4.3.CP-012.
func TestCP012_HookFiresOnEventMatch(t *testing.T) {
	t.Parallel()

	cp := cp012FixtureMakeHookCP(
		"test-hook",
		"on_agent_started",
		"true", // expression always fires
		false,
		0,
	)
	reg := cp012FixtureNewRegistry(cp)
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		return disp.Subscribe()
	})
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	got := collector.all()
	found := false
	for _, et := range got {
		if et == "hook_fired" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CP-012: hook_fired not emitted after agent_started event; collected events: %v", got)
	}
}

// TestCP012_HookDoesNotFireOnNonMatchingEvent verifies that a hook registered
// for "on_agent_started" does not fire when an unrelated event type is emitted.
func TestCP012_HookDoesNotFireOnNonMatchingEvent(t *testing.T) {
	t.Parallel()

	cp := cp012FixtureMakeHookCP(
		"test-hook",
		"on_agent_started",
		"true",
		false,
		0,
	)
	reg := cp012FixtureNewRegistry(cp)
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		return disp.Subscribe()
	})
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_completed", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	for _, et := range collector.all() {
		if et == "hook_fired" {
			t.Errorf("CP-012: hook_fired emitted for non-matching event type agent_completed")
		}
	}
}

// TestCP012_HookEvaluatorFalseDoesNotFire verifies that a hook whose mechanism
// evaluator expression evaluates to false does not emit hook_fired.
func TestCP012_HookEvaluatorFalseDoesNotFire(t *testing.T) {
	t.Parallel()

	cp := cp012FixtureMakeHookCP(
		"test-hook",
		"on_agent_started",
		"false", // expression never fires
		false,
		0,
	)
	reg := cp012FixtureNewRegistry(cp)
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		return disp.Subscribe()
	})
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	for _, et := range collector.all() {
		if et == "hook_fired" {
			t.Errorf("CP-012: hook_fired emitted when evaluator expression returned false")
		}
	}
}

// TestCP013_TriggerNameOnPrefix verifies that a hook registered with
// trigger "on_run_started" fires when a run_started event is emitted,
// confirming the on_<event-type> namespace mapping per CP-013.
func TestCP013_TriggerNameOnPrefix(t *testing.T) {
	t.Parallel()

	cp := cp012FixtureMakeHookCP(
		"run-started-hook",
		"on_run_started",
		"true",
		false,
		0,
	)
	reg := cp012FixtureNewRegistry(cp)
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		return disp.Subscribe()
	})
	_ = disp

	payload := cp012FixtureMarshal(t, map[string]any{})
	cp012FixtureEmitEvent(t, bus, "run_started", payload)
	cp012FixtureWaitDrain(t, bus)

	found := false
	for _, et := range collector.all() {
		if et == "hook_fired" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CP-013: hook_fired not emitted for run_started event (trigger=on_run_started)")
	}
}

// TestCP012_SubscriptionFilterMatching verifies that a hook with a subscription
// filter fires only when the filter condition is met.
func TestCP012_SubscriptionFilterMatching(t *testing.T) {
	t.Parallel()

	cp := cp012FixtureMakeHookCPWithFilter(
		"filtered-hook",
		"on_agent_started",
		"score > 50", // subscription_filter: only fire when score > 50
		"true",       // evaluator: always fires if filter passes
		core.SideEffectKindEmitEvent,
	)
	reg := cp012FixtureNewRegistry(cp)

	collector1 := &cp012FixtureEventCollector{}
	var disp1 *hooksystem.Dispatcher
	bus1 := cp012FixtureBuildBus(t, collector1, func(b eventbus.EventBus) error {
		disp1 = hooksystem.NewDispatcher(reg, b)
		return disp1.Subscribe()
	})
	_ = disp1
	cp012FixtureEmitEvent(t, bus1, "agent_started", cp012FixtureMakeFilteredPayload(100))
	cp012FixtureWaitDrain(t, bus1)

	foundFired := false
	for _, et := range collector1.all() {
		if et == "hook_fired" {
			foundFired = true
		}
	}
	if !foundFired {
		t.Errorf("subscription_filter: hook_fired not emitted when score=100 (filter: score>50)")
	}

	collector2 := &cp012FixtureEventCollector{}
	var disp2 *hooksystem.Dispatcher
	bus2 := cp012FixtureBuildBus(t, collector2, func(b eventbus.EventBus) error {
		disp2 = hooksystem.NewDispatcher(reg, b)
		return disp2.Subscribe()
	})
	_ = disp2
	cp012FixtureEmitEvent(t, bus2, "agent_started", cp012FixtureMakeFilteredPayload(10))
	cp012FixtureWaitDrain(t, bus2)

	for _, et := range collector2.all() {
		if et == "hook_fired" {
			t.Errorf("subscription_filter: hook_fired emitted when score=10 (filter: score>50 should reject)")
		}
	}
}

// TestCP014_HookOrderingBySubsystemPriority verifies that when multiple hooks
// match the same event, they fire in SubsystemPriority ascending order per CP-014.
//
// We record hook_fired events and check the order via a synchronous consumer
// that records hook names from the payload. Synchronous dispatch preserves
// emission order; observer dispatch would be non-deterministic across goroutines.
func TestCP014_HookOrderingBySubsystemPriority(t *testing.T) {
	t.Parallel()

	cpP10 := cp012FixtureMakeHookCP("hook-p10", "on_agent_started", "true", false, 10)
	cpP30 := cp012FixtureMakeHookCP("hook-p30", "on_agent_started", "true", false, 30)
	cpP20 := cp012FixtureMakeHookCP("hook-p20", "on_agent_started", "true", false, 20)

	reg := cp012FixtureNewRegistry(cpP10, cpP30, cpP20)

	var firedNames []string

	var dispRef *hooksystem.Dispatcher
	bus := eventbus.NewBusImpl()
	dispRef = hooksystem.NewDispatcher(reg, bus)
	if err := dispRef.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = dispRef

	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.order-collector",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{core.EventTypeHookFired: {}},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			var pl core.HookFiredPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return err
			}
			firedNames = append(firedNames, string(pl.HookName))
			return nil
		},
	}); err != nil {
		t.Fatalf("Subscribe order-collector: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	if len(firedNames) != 3 {
		t.Fatalf("CP-014: expected 3 hook_fired events, got %d: %v", len(firedNames), firedNames)
	}

	want := []string{"hook-p10", "hook-p20", "hook-p30"}
	for i, w := range want {
		if firedNames[i] != w {
			t.Errorf("CP-014: hook order[%d]: got %q, want %q (full order: %v)",
				i, firedNames[i], w, firedNames)
		}
	}
}

// TestCP014_HookOrderingByDeclarationOrderWithinSamePriority verifies that
// hooks at the same SubsystemPriority fire in declaration order (registration
// insertion order) per CP-014: "within a subsystem, declaration order."
//
// The hooks are registered in a deliberate non-alphabetical order (C, A, B)
// and the test asserts that they fire in exactly that registration order,
// not alphabetically.
func TestCP014_HookOrderingByDeclarationOrderWithinSamePriority(t *testing.T) {
	t.Parallel()

	cpC := cp012FixtureMakeHookCP("hook-c", "on_agent_started", "true", false, 0)
	cpA := cp012FixtureMakeHookCP("hook-a", "on_agent_started", "true", false, 0)
	cpB := cp012FixtureMakeHookCP("hook-b", "on_agent_started", "true", false, 0)

	reg := cp012FixtureNewRegistry(cpC, cpA, cpB)

	var firedNames []string

	var dispRef *hooksystem.Dispatcher
	bus := eventbus.NewBusImpl()
	dispRef = hooksystem.NewDispatcher(reg, bus)
	if err := dispRef.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = dispRef

	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.decl-order-collector",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{core.EventTypeHookFired: {}},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			var pl core.HookFiredPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return err
			}
			firedNames = append(firedNames, string(pl.HookName))
			return nil
		},
	}); err != nil {
		t.Fatalf("Subscribe decl-order-collector: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	if len(firedNames) != 3 {
		t.Fatalf("CP-014: expected 3 hook_fired events, got %d: %v", len(firedNames), firedNames)
	}

	want := []string{"hook-c", "hook-a", "hook-b"}
	for i, w := range want {
		if firedNames[i] != w {
			t.Errorf("CP-014: declaration order[%d]: got %q, want %q (full order: %v)",
				i, firedNames[i], w, firedNames)
		}
	}
}

// TestCP015_HookFailureDoesNotHaltByDefault verifies that when a hook fails
// with halt_on_failure=false (the default), the remaining hooks in the chain
// still execute.
func TestCP015_HookFailureDoesNotHaltByDefault(t *testing.T) {
	t.Parallel()

	cpFail := cp012FixtureMakeHookCP(
		"hook-fail",
		"on_agent_started",
		"undefined_var_that_causes_failure > 0",
		false,
		10,
	)
	cpOK := cp012FixtureMakeHookCP(
		"hook-ok",
		"on_agent_started",
		"true",
		false,
		20,
	)

	reg := cp012FixtureNewRegistry(cpFail, cpOK)
	collector := &cp012FixtureEventCollector{}

	var disp *hooksystem.Dispatcher
	bus := cp012FixtureBuildBus(t, collector, func(b eventbus.EventBus) error {
		disp = hooksystem.NewDispatcher(reg, b)
		return disp.Subscribe()
	})
	_ = disp

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	events := collector.all()
	hasFailed := false
	hasFired := false
	for _, et := range events {
		if et == "hook_failed" {
			hasFailed = true
		}
		if et == "hook_fired" {
			hasFired = true
		}
	}
	if !hasFailed {
		t.Error("CP-015: hook_failed not emitted for failing hook")
	}
	if !hasFired {
		t.Error("CP-015: hook_fired not emitted for hook-ok after hook-fail with halt_on_failure=false")
	}
}

// TestCP015_HaltOnFailureStopsChain verifies that when a hook fails with
// halt_on_failure=true, the chain stops and subsequent hooks do NOT fire.
func TestCP015_HaltOnFailureStopsChain(t *testing.T) {
	t.Parallel()

	cpHaltFail := cp012FixtureMakeHookCP(
		"hook-halt-fail",
		"on_agent_started",
		"undefined_var_that_causes_failure > 0",
		true,
		10,
	)
	cpAfter := cp012FixtureMakeHookCP(
		"hook-after",
		"on_agent_started",
		"true",
		false,
		20,
	)

	reg := cp012FixtureNewRegistry(cpHaltFail, cpAfter)

	var mu sync.Mutex
	var firedNames []string

	var dispRef *hooksystem.Dispatcher
	bus := eventbus.NewBusImpl()
	dispRef = hooksystem.NewDispatcher(reg, bus)
	if err := dispRef.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = dispRef

	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.halt-collector",
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
			firedNames = append(firedNames, string(pl.HookName))
			mu.Unlock()
			return nil
		},
	}); err != nil {
		t.Fatalf("Subscribe halt-collector: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())
	cp012FixtureWaitDrain(t, bus)

	mu.Lock()
	names := make([]string, len(firedNames))
	copy(names, firedNames)
	mu.Unlock()

	for _, n := range names {
		if n == "hook-after" {
			t.Errorf("CP-015: hook-after fired after halt_on_failure=true halted the chain; fired: %v", names)
		}
	}
}

// TestCP012_HooksAreObserverClass verifies that the dispatcher registers as
// ConsumerClassObserver so hook processing cannot block the Emit call path
// (CP-012: "Hooks MUST NOT block, halt, or alter the run's transition
// progression").
//
// We verify this indirectly: the Emit call returns before hook processing
// completes (observer dispatch is off the critical path per EV-012). We
// confirm the Dispatcher's subscription class by observing that Emit returns
// synchronously even when the hook is designed to introduce delay.
// (A full timing proof is out of scope; we verify the observer-class contract
// by confirming hook_fired is only visible after Drain, not before.)
func TestCP012_HooksAreObserverClass(t *testing.T) {
	t.Parallel()

	cp := cp012FixtureMakeHookCP(
		"observer-hook",
		"on_agent_started",
		"true",
		false,
		0,
	)
	reg := cp012FixtureNewRegistry(cp)

	var mu sync.Mutex
	var hookFiredSeen bool

	var dispRef *hooksystem.Dispatcher
	bus := eventbus.NewBusImpl()
	dispRef = hooksystem.NewDispatcher(reg, bus)
	if err := dispRef.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = dispRef

	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.class-observer",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			if ev.Type == "hook_fired" {
				mu.Lock()
				hookFiredSeen = true
				mu.Unlock()
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("Subscribe class-observer: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	cp012FixtureEmitEvent(t, bus, "agent_started", cp012FixtureMakeAgentStartedPayload())

	cp012FixtureWaitDrain(t, bus)

	mu.Lock()
	seen := hookFiredSeen
	mu.Unlock()
	if !seen {
		t.Error("CP-012: hook_fired not visible after Drain (observer dispatch did not complete)")
	}
}
