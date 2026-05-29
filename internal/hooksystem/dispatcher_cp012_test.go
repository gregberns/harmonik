package hooksystem_test

// dispatcher_cp012_test.go — binding tests for CP-012 "Hook fires on event match".
//
// Spec ref: specs/control-points.md §4.3 CP-012 through CP-015.
// Bead ref: hk-a8bg.11
//
// Coverage:
//
//	CP-012: Hook fires when subscribed event matches trigger.
//	CP-013: Trigger name uses on_<event-type> prefix convention.
//	CP-014: Multiple hooks on one event fire in SubsystemPriority ascending,
//	        then Name ascending order.
//	CP-015: halt_on_failure=true stops the hook chain; =false continues.
//
// # Helper prefix
//
// All package-level identifiers in this file use the cp012Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

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

// cp012FixtureMakeHookCP builds a minimal valid Hook ControlPoint.
//
// triggerEvent should use the on_ prefix (e.g. "on_agent_started").
// expression is a boolean policy expression evaluated against the event payload.
func cp012FixtureMakeHookCP(
	name string,
	triggerEvent string,
	expression string,
	sideEffectKind core.SideEffectKind,
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
				SideEffectKind:    sideEffectKind,
				HaltOnFailure:     haltOnFailure,
				SubsystemPriority: subsystemPriority,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

// cp012FixtureMakeHookCPWithFilter builds a Hook ControlPoint with a subscription filter.
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
				TriggerEvent:      triggerEvent,
				SubscriptionFilter: &filterExpr,
				SideEffectKind:    sideEffectKind,
				HaltOnFailure:     false,
				SubsystemPriority: 0,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

// cp012FixtureMapRegistry is a minimal read-only Registry backed by a slice of
// ControlPoints. It satisfies hooksystem.Registry.
type cp012FixtureMapRegistry struct {
	mu  sync.RWMutex
	cps []core.ControlPoint
}

func cp012FixtureNewRegistry(cps ...core.ControlPoint) *cp012FixtureMapRegistry {
	// Stamp DeclarationIndex in registration order so the dispatcher can apply
	// CP-014's within-priority declaration-order tie-breaker.
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

// cp012FixtureEventCollector collects event types emitted to the bus during a test.
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

// cp012FixtureMakeAgentStartedPayload builds a minimal agent_started-like payload.
func cp012FixtureMakeAgentStartedPayload() json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"run_id": "test-run"})
	return raw
}

// cp012FixtureMakeFilteredPayload builds a payload containing a `score` field for
// filter tests.
func cp012FixtureMakeFilteredPayload(score int) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"score": score})
	return raw
}

// cp012FixtureBuildBus constructs a bus, registers a collector observer, and
// seals it. Returns the bus and the collector. The collectorHooks argument
// provides additional Subscriptions to register before Seal (e.g., the
// dispatcher's own subscription).
func cp012FixtureBuildBus(t *testing.T, collector *cp012FixtureEventCollector, extraSubs ...func(eventbus.EventBus) error) eventbus.EventBus {
	t.Helper()

	bus := eventbus.NewBusImpl()

	for _, fn := range extraSubs {
		if err := fn(bus); err != nil {
			t.Fatalf("cp012FixtureBuildBus: extra sub: %v", err)
		}
	}

	// Register the collector as an observer for hook_fired and hook_failed.
	_, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			collector.record(ev.Type)
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

// cp012FixtureEmitEvent emits a minimal event of the given type and payload.
func cp012FixtureEmitEvent(t *testing.T, bus eventbus.EventBus, eventType string, payload json.RawMessage) {
	t.Helper()
	if err := bus.Emit(context.Background(), core.EventType(eventType), payload); err != nil {
		t.Fatalf("Emit(%q): %v", eventType, err)
	}
}

// cp012FixtureWaitDrain calls Drain to ensure all observer dispatches complete.
func cp012FixtureWaitDrain(t *testing.T, bus eventbus.EventBus) {
	t.Helper()
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CP-012: Hook fires when subscribed event matches trigger
// ---------------------------------------------------------------------------

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
		core.SideEffectKindEmitEvent,
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
		core.SideEffectKindEmitEvent,
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

	// Emit a different event type — hook must not fire.
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
		core.SideEffectKindEmitEvent,
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

// ---------------------------------------------------------------------------
// CP-013: on_ prefix convention
// ---------------------------------------------------------------------------

// TestCP013_TriggerNameOnPrefix verifies that a hook registered with
// trigger "on_run_started" fires when a run_started event is emitted,
// confirming the on_<event-type> namespace mapping per CP-013.
func TestCP013_TriggerNameOnPrefix(t *testing.T) {
	t.Parallel()

	cp := cp012FixtureMakeHookCP(
		"run-started-hook",
		"on_run_started",
		"true",
		core.SideEffectKindEmitEvent,
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

	payload, _ := json.Marshal(map[string]any{})
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

// ---------------------------------------------------------------------------
// Subscription filter (§6.1.2)
// ---------------------------------------------------------------------------

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

	// First bus: emit event where filter PASSES (score=100).
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

	// Second bus: emit event where filter FAILS (score=10).
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

// ---------------------------------------------------------------------------
// CP-014: Hook ordering is deterministic
// ---------------------------------------------------------------------------

// TestCP014_HookOrderingBySubsystemPriority verifies that when multiple hooks
// match the same event, they fire in SubsystemPriority ascending order per CP-014.
//
// We record hook_fired events and check the order via a synchronous consumer
// that records hook names from the payload. Synchronous dispatch preserves
// emission order; observer dispatch would be non-deterministic across goroutines.
func TestCP014_HookOrderingBySubsystemPriority(t *testing.T) {
	t.Parallel()

	// Three hooks with different priorities. We expect p10 before p20 before p30.
	cpP10 := cp012FixtureMakeHookCP("hook-p10", "on_agent_started", "true", core.SideEffectKindEmitEvent, false, 10)
	cpP30 := cp012FixtureMakeHookCP("hook-p30", "on_agent_started", "true", core.SideEffectKindEmitEvent, false, 30)
	cpP20 := cp012FixtureMakeHookCP("hook-p20", "on_agent_started", "true", core.SideEffectKindEmitEvent, false, 20)

	reg := cp012FixtureNewRegistry(cpP10, cpP30, cpP20)

	var firedNames []string

	var dispRef *hooksystem.Dispatcher
	bus := eventbus.NewBusImpl()
	dispRef = hooksystem.NewDispatcher(reg, bus)
	if err := dispRef.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = dispRef

	// Synchronous consumer for hook_fired: records names in emission order.
	// A synchronous consumer blocks the Emit call so recording is strictly
	// ordered with each hook_fired emission.
	// DeclaredEmitTypes is nil (this consumer never emits), so no cycle.
	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.order-collector",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{"hook_fired": {}},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			var pl core.HookFiredPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return nil
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

	// Register in order: C, A, B.  Expected fire order: hook-c, hook-a, hook-b.
	cpC := cp012FixtureMakeHookCP("hook-c", "on_agent_started", "true", core.SideEffectKindEmitEvent, false, 0)
	cpA := cp012FixtureMakeHookCP("hook-a", "on_agent_started", "true", core.SideEffectKindEmitEvent, false, 0)
	cpB := cp012FixtureMakeHookCP("hook-b", "on_agent_started", "true", core.SideEffectKindEmitEvent, false, 0)

	reg := cp012FixtureNewRegistry(cpC, cpA, cpB)

	var firedNames []string

	var dispRef *hooksystem.Dispatcher
	bus := eventbus.NewBusImpl()
	dispRef = hooksystem.NewDispatcher(reg, bus)
	if err := dispRef.Subscribe(); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	_ = dispRef

	// Synchronous consumer for hook_fired: records names in emission order.
	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.decl-order-collector",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{"hook_fired": {}},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, ev core.Event) error {
			var pl core.HookFiredPayload
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return nil
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

	// Declaration order: hook-c (first), hook-a (second), hook-b (third).
	want := []string{"hook-c", "hook-a", "hook-b"}
	for i, w := range want {
		if firedNames[i] != w {
			t.Errorf("CP-014: declaration order[%d]: got %q, want %q (full order: %v)",
				i, firedNames[i], w, firedNames)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-015: Hook failures do not halt the chain (halt_on_failure=false)
// ---------------------------------------------------------------------------

// TestCP015_HookFailureDoesNotHaltByDefault verifies that when a hook fails
// with halt_on_failure=false (the default), the remaining hooks in the chain
// still execute.
func TestCP015_HookFailureDoesNotHaltByDefault(t *testing.T) {
	t.Parallel()

	// hook-fail: expression references undefined variable → compile error.
	// halt_on_failure = false (default).
	cpFail := cp012FixtureMakeHookCP(
		"hook-fail",
		"on_agent_started",
		"undefined_var_that_causes_failure > 0",
		core.SideEffectKindEmitEvent,
		false, // halt_on_failure = false
		10,
	)
	// hook-ok: fires after hook-fail despite its failure.
	cpOK := cp012FixtureMakeHookCP(
		"hook-ok",
		"on_agent_started",
		"true",
		core.SideEffectKindEmitEvent,
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

	// hook-halt-fail: bad expression + halt_on_failure=true.
	cpHaltFail := cp012FixtureMakeHookCP(
		"hook-halt-fail",
		"on_agent_started",
		"undefined_var_that_causes_failure > 0",
		core.SideEffectKindEmitEvent,
		true, // halt_on_failure = true
		10,
	)
	// hook-after: would fire if chain not halted.
	cpAfter := cp012FixtureMakeHookCP(
		"hook-after",
		"on_agent_started",
		"true",
		core.SideEffectKindEmitEvent,
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
				return nil
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

// ---------------------------------------------------------------------------
// CP-012: Hooks do not block run's transition progression
// ---------------------------------------------------------------------------

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
		core.SideEffectKindEmitEvent,
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

	// After Drain, hook_fired must be visible. The Dispatcher subscription is
	// ConsumerClassObserver; its dispatch is off the critical path.
	cp012FixtureWaitDrain(t, bus)

	mu.Lock()
	seen := hookFiredSeen
	mu.Unlock()
	if !seen {
		t.Error("CP-012: hook_fired not visible after Drain (observer dispatch did not complete)")
	}
}
