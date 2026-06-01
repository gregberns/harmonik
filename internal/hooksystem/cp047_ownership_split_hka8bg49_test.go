package hooksystem_test

// cp047_ownership_split_hka8bg49_test.go — CP-047 S05 ownership conformance (hooksystem)
//
// Covers specs/control-points.md §4.10.CP-047 from the S05 (Hook System) perspective:
//
//	"S05 (Hook System) owns the Hook dispatch loop (subscribing to the event bus,
//	 ordering Hooks per §4.3.CP-014, applying side-effects, isolating failures);
//	 S05 consults the registry but does NOT own it."
//
// # Coverage
//
//   - hooksystem.Registry interface is read-only: no Register() method, so the
//     Dispatcher structurally cannot write to the registry.
//   - hooksystem.Registry interface is narrower than core.Registry: it exposes
//     only LookupByTrigger, not LookupByName, LookupByAttachPoint, or All().
//     This structural narrowing prevents S05 from straying into S01's Gate
//     invocation path (LookupByAttachPoint) or S02's registration audit
//     (All()).
//   - The Dispatcher uses LookupByTrigger (not LookupByAttachPoint): Hook
//     dispatch is trigger-event-driven, not attach-point-driven. S01 uses
//     LookupByAttachPoint for Gate invocation; S05 must not.
//   - S05 Dispatcher dispatches Hook-kind ControlPoints: when a Hook fires,
//     the bus emits hook_fired, confirming S05 ownership of the Hook dispatch path.
//
// Tags: mechanism
//
// Refs: hk-a8bg.49

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/hooksystem"
)

// ---------------------------------------------------------------------------
// hooksystem.Registry interface structural conformance (CP-047)
// ---------------------------------------------------------------------------

// TestCP047_HooksystemRegistryInterface_NoRegisterMethod verifies that the
// hooksystem.Registry interface does not expose a Register() method.
//
// CP-047: S05 "consults the registry but does NOT own it." Unlike core.Registry
// (which includes Register per §6.1.7), hooksystem.Registry is a narrow subset
// interface that deliberately omits Register. This structural narrowing means
// the Dispatcher cannot write ControlPoints into the registry even by accident —
// the compiler enforces the read-only contract at S05's type boundary.
func TestCP047_HooksystemRegistryInterface_NoRegisterMethod(t *testing.T) {
	t.Parallel()

	registryType := reflect.TypeOf((*hooksystem.Registry)(nil)).Elem()

	_, hasRegister := registryType.MethodByName("Register")
	if hasRegister {
		t.Error("hooksystem.Registry exposes Register() — S05 must not be able to write to the registry; only S02 may register ControlPoints (CP-047)")
	}
}

// TestCP047_HooksystemRegistryInterface_NarrowsToHookDispatch verifies that
// hooksystem.Registry exposes only LookupByTrigger — the single method S05
// needs to dispatch Hooks — and not the S01-specific methods LookupByAttachPoint
// or All().
//
// This narrowing is load-bearing for CP-047:
//   - LookupByAttachPoint is how S01 performs Gate invocation; exposing it to
//     S05 would let S05 stray into the Gate path.
//   - All() is used by S01BuildGuardEvaluator to enumerate Guards; exposing it
//     to S05 would let S05 scan all ControlPoint kinds.
//   - LookupByName is not needed for Hook dispatch (Hooks are resolved by
//     trigger name, not by ControlPoint name).
//
// S05 needs only LookupByTrigger to satisfy its dispatch obligation.
func TestCP047_HooksystemRegistryInterface_NarrowsToHookDispatch(t *testing.T) {
	t.Parallel()

	registryType := reflect.TypeOf((*hooksystem.Registry)(nil)).Elem()

	// Must have exactly one method: LookupByTrigger.
	if n := registryType.NumMethod(); n != 1 {
		t.Errorf("hooksystem.Registry has %d method(s), want exactly 1 (LookupByTrigger) — the interface exposes more than S05 needs (CP-047)", n)
	}

	if _, ok := registryType.MethodByName("LookupByTrigger"); !ok {
		t.Error("hooksystem.Registry is missing LookupByTrigger — S05 cannot dispatch Hooks without it")
	}

	// S01-specific methods must not be present.
	s01Methods := []string{"LookupByAttachPoint", "All", "LookupByName"}
	for _, m := range s01Methods {
		if _, ok := registryType.MethodByName(m); ok {
			t.Errorf("hooksystem.Registry exposes %q — this is an S01/S02 method that S05 must not have access to (CP-047)", m)
		}
	}
}

// ---------------------------------------------------------------------------
// S05 Dispatcher dispatches Hook-kind CPs (CP-047)
// ---------------------------------------------------------------------------

// TestCP047_S05Dispatcher_DispatchesHookKindCPs verifies that the S05 Dispatcher
// fires Hook-kind ControlPoints when a matching event is emitted, and emits a
// hook_fired event to confirm S05 ownership of the Hook dispatch path.
//
// CP-047: S05 "owns the Hook dispatch loop … applying side-effects." The
// hook_fired event is the observable signal of a successful Hook dispatch
// (CP-048 event-based observability).
func TestCP047_S05Dispatcher_DispatchesHookKindCPs(t *testing.T) {
	t.Parallel()

	triggerEvent := "on_agent_started"
	hookCP := cp047FixtureMakeHookCP("s05-owned-hook", triggerEvent)
	reg := cp047FixtureNewRegistry(hookCP)

	var (
		mu          sync.Mutex
		collected   []string
	)
	collector := func(_ context.Context, ev core.Event) error {
		mu.Lock()
		collected = append(collected, ev.Type)
		mu.Unlock()
		return nil
	}

	bus := eventbus.NewBusImpl()
	d := hooksystem.NewDispatcher(reg, bus)
	if err := d.Subscribe(); err != nil {
		t.Fatalf("Dispatcher.Subscribe: %v", err)
	}
	_, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "test.cp047.collector",
		ConsumerClass: core.ConsumerClassObserver,
		EventPattern:  core.EventPattern{Wildcard: true},
		OnPanic:       core.OnPanicRecoverAndLog,
		Handler:       collector,
	})
	if err != nil {
		t.Fatalf("subscribe collector: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{"run_id": "cp047-run"})
	if err := bus.Emit(context.Background(), "agent_started", payload); err != nil {
		t.Fatalf("Emit agent_started: %v", err)
	}
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	mu.Lock()
	events := make([]string, len(collected))
	copy(events, collected)
	mu.Unlock()

	hasFired := false
	for _, ev := range events {
		if ev == "hook_fired" {
			hasFired = true
		}
	}
	if !hasFired {
		t.Errorf("expected hook_fired event from S05 dispatcher; got events: %v (CP-047: S05 owns Hook dispatch)", events)
	}
}

// TestCP047_S05Dispatcher_LookupByTriggerNotAttachPoint verifies that the
// Dispatcher resolves Hooks using LookupByTrigger (event-driven), not
// LookupByAttachPoint (which is S01's Gate invocation path).
//
// This is confirmed structurally by the hooksystem.Registry interface exposing
// only LookupByTrigger; this test reinforces it behaviorally: the Dispatcher
// receives events, translates them to "on_<event-type>" trigger names, and
// calls LookupByTrigger — not LookupByAttachPoint.
func TestCP047_S05Dispatcher_LookupByTriggerNotAttachPoint(t *testing.T) {
	t.Parallel()

	// Use a recording registry that captures which lookup method was called.
	recording := &cp047RecordingRegistry{}

	bus := eventbus.NewBusImpl()
	d := hooksystem.NewDispatcher(recording, bus)
	if err := d.Subscribe(); err != nil {
		t.Fatalf("Dispatcher.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{})
	if err := bus.Emit(context.Background(), "agent_started", payload); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if err := bus.Drain(context.Background()); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	recording.mu.Lock()
	triggers := make([]string, len(recording.triggers))
	copy(triggers, recording.triggers)
	recording.mu.Unlock()

	if len(triggers) == 0 {
		t.Fatal("Dispatcher did not call LookupByTrigger — S05 dispatch loop not active")
	}
	for _, trig := range triggers {
		const prefix = "on_"
		if len(trig) < len(prefix) || trig[:len(prefix)] != prefix {
			t.Errorf("Dispatcher called LookupByTrigger with %q — expected on_<event-type> prefix (CP-013)", trig)
		}
	}
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// cp047FixtureMakeHookCP builds a minimal Hook ControlPoint for CP-047 tests.
func cp047FixtureMakeHookCP(name, triggerEvent string) core.ControlPoint {
	expr := core.PolicyExpression("true")
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
				HaltOnFailure:     false,
				SubsystemPriority: 0,
			},
		},
		Axes:          core.BaselineAxisTags,
		ModeTag:       core.ModeTagMechanism,
		SchemaVersion: 1,
	}
}

// cp047FixtureRegistry is a read-only Registry stub that only returns the
// ControlPoints it was constructed with, filtered to KindHook, on LookupByTrigger.
// Implements hooksystem.Registry.
type cp047FixtureRegistry struct {
	cps []core.ControlPoint
}

func cp047FixtureNewRegistry(cps ...core.ControlPoint) *cp047FixtureRegistry {
	stamped := make([]core.ControlPoint, len(cps))
	for i, cp := range cps {
		cp.DeclarationIndex = i
		stamped[i] = cp
	}
	return &cp047FixtureRegistry{cps: stamped}
}

func (r *cp047FixtureRegistry) LookupByTrigger(trigger string) []core.ControlPoint {
	var out []core.ControlPoint
	for _, cp := range r.cps {
		if cp.Kind == core.KindHook && cp.Trigger.Name == trigger {
			out = append(out, cp)
		}
	}
	return out
}

// cp047RecordingRegistry records every LookupByTrigger call for inspection.
// Implements hooksystem.Registry.
type cp047RecordingRegistry struct {
	mu       sync.Mutex
	triggers []string
}

func (r *cp047RecordingRegistry) LookupByTrigger(trigger string) []core.ControlPoint {
	r.mu.Lock()
	r.triggers = append(r.triggers, trigger)
	r.mu.Unlock()
	return nil // no hooks registered; just recording the call
}
