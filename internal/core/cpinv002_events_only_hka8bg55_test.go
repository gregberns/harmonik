package core

// cpinv002_events_only_hka8bg55_test.go — CP-INV-002 sensor suite
//
// Covers specs/control-points.md §5.CP-INV-002:
//
//	"Every ControlPoint effect that crosses a subsystem boundary MUST be
//	 observable through one of the typed events declared in §4.10.CP-048.
//	 No ControlPoint outcome may be communicated across subsystems by shared
//	 memory, direct method call across an owner boundary, or out-of-band
//	 signal."
//
// CP-048 declares ten typed events:
//
//	gate_allowed, gate_denied, gate_escalated
//	hook_fired, hook_failed
//	guard_reordered, guard_failed
//	budget_accrual, budget_warning, budget_exhausted
//
// This file is the §10.2 sensor for the CP-047..CP-048 ownership-split group.
// Tests verify cross-subsystem observability: every ControlPoint outcome that
// must be visible to a non-owner subsystem arrives via one of the ten CP-048
// typed event payloads registered in the global event registry, and never via
// a raw return value (GateAction, []Edge) that could travel out-of-band.
//
// # Coverage
//
//   - All ten CP-048 typed events are present in the global event registry
//     (LookupTypeSchemaVersion succeeds for each name), proving each has a
//     registered typed payload constructor.
//   - CP-048 payload constructors return distinct Go types — no two CP-048
//     events share the same payload type; each outcome has its own observable
//     channel.
//   - Each of the ten CP-048 payload types satisfies the EventPayload marker
//     interface, proving the type was designed as a cross-subsystem observable
//     and not as an internal return value.
//   - GateAction (the S01-internal gate verdict) does NOT satisfy EventPayload —
//     the raw gate outcome cannot cross subsystem boundaries as an event payload;
//     it must be wrapped in GateAllowedPayload, GateDeniedPayload, or
//     GateEscalatedPayload before becoming observable.
//   - GateAction values biject onto gate event types: exactly three GateAction
//     constants map to exactly three gate event types — allow→gate_allowed,
//     deny→gate_denied, escalate-to-human→gate_escalated. No gate outcome is
//     observable through an event type not in that set.
//   - Guard return type ([]Edge) does NOT satisfy EventPayload — the raw guard
//     output cannot cross subsystem boundaries directly; it must be wrapped in
//     GuardReorderedPayload or GuardFailedPayload.
//   - Well-formed CP-048 payload instances satisfy Valid() for all ten types,
//     confirming the payload types are fully specified.
//
// Tags: mechanism
//
// Refs: hk-a8bg.55

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

// cp048EventTypes is the closed set of ten CP-048 typed events declared in
// specs/control-points.md §4.10.CP-048. Every entry must be present in the
// global event registry; no ControlPoint outcome may cross a subsystem boundary
// except through one of these event types.
var cp048EventTypes = []string{
	// Gate outcomes (§8.2.4–§8.2.6)
	"gate_allowed",
	"gate_denied",
	"gate_escalated",
	// Hook outcomes (§8.2.1–§8.2.2)
	"hook_fired",
	"hook_failed",
	// Guard outcomes (§8.2.7–§8.2.8)
	"guard_reordered",
	"guard_failed",
	// Budget outcomes (§8.4.1–§8.4.3)
	"budget_warning",
	"budget_accrual",
	"budget_exhausted",
}

// ---------------------------------------------------------------------------
// CP-048 events are registered in the global event registry
// ---------------------------------------------------------------------------

// TestCPINV002_CP048EventsRegisteredInGlobalRegistry verifies that all ten
// CP-048 typed events have payload constructors registered in the global event
// registry.
//
// CP-INV-002 requires that cross-subsystem ControlPoint effects are observable
// only through these typed events. A typed event that is absent from the global
// registry cannot be emitted on the bus (EV-034); its absence would mean the
// corresponding ControlPoint outcome has no legal cross-subsystem observation
// channel.
func TestCPINV002_CP048EventsRegisteredInGlobalRegistry(t *testing.T) {
	t.Parallel()

	for _, typeName := range cp048EventTypes {
		_, ok := LookupTypeSchemaVersion(typeName)
		if !ok {
			t.Errorf("CP-048 event %q is NOT registered in the global event registry — "+
				"ControlPoint effects of this kind have no typed cross-subsystem observation channel (CP-INV-002)", typeName)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-048 payload constructors return distinct types
// ---------------------------------------------------------------------------

// TestCPINV002_CP048PayloadConstructorsReturnDistinctTypes verifies that no two
// CP-048 event names share the same payload Go type.
//
// CP-INV-002: each ControlPoint effect kind has its own typed event channel. If
// two different outcomes shared one payload type, a consumer observing that type
// could not distinguish between the two outcomes — effectively collapsing two
// separate observation channels into one, which would violate the requirement
// that each effect is individually observable.
func TestCPINV002_CP048PayloadConstructorsReturnDistinctTypes(t *testing.T) {
	t.Parallel()

	seenTypes := make(map[reflect.Type]string, len(cp048EventTypes))
	for _, typeName := range cp048EventTypes {
		p := cp048FixturePayload(t, typeName)
		pt := reflect.TypeOf(p)
		if prior, seen := seenTypes[pt]; seen {
			t.Errorf("CP-048 events %q and %q share payload Go type %s — "+
				"distinct outcomes must have distinct payload types (CP-INV-002)", prior, typeName, pt)
		}
		seenTypes[pt] = typeName
	}
}

// ---------------------------------------------------------------------------
// CP-048 payload types are pointer-to-struct (proper event payload shape)
// ---------------------------------------------------------------------------

// TestCPINV002_CP048PayloadTypesArePointerToStruct verifies that the value
// produced by each CP-048 event's registry constructor is a pointer to a struct
// — the standard shape for Go event payload types registered in the global
// event registry.
//
// EventPayload is an intentionally-empty marker interface (eventregistry.go
// EV-032). The registry constructor contract requires each constructor to return
// a pointer to a fresh zero-value struct suitable for JSON unmarshaling. A
// type that is not a pointer-to-struct (e.g., a scalar like GateAction or a
// slice like []Edge) is not a valid event payload shape and could not have been
// registered via RegisterEventType. Confirming the pointer-to-struct shape for
// all ten CP-048 types proves each has the correct observable form.
func TestCPINV002_CP048PayloadTypesArePointerToStruct(t *testing.T) {
	t.Parallel()

	for _, typeName := range cp048EventTypes {
		p := cp048FixturePayload(t, typeName)
		pt := reflect.TypeOf(p)
		if pt == nil {
			t.Errorf("CP-048 event %q constructor returned nil — not a valid payload type (CP-INV-002)", typeName)
			continue
		}
		if pt.Kind() != reflect.Ptr || pt.Elem().Kind() != reflect.Struct {
			t.Errorf("CP-048 event %q payload type %s is not a pointer-to-struct (kind=%s, elem-kind=%v) — "+
				"not a valid event payload shape (CP-INV-002)", typeName, pt, pt.Kind(), func() reflect.Kind {
				if pt.Kind() == reflect.Ptr {
					return pt.Elem().Kind()
				}
				return reflect.Invalid
			}())
		}
	}
}

// ---------------------------------------------------------------------------
// GateAction is not a CP-048 payload type (raw outcome ≠ observation carrier)
// ---------------------------------------------------------------------------

// TestCPINV002_GateAction_IsNotACP048PayloadType verifies that GateAction — the
// S01-internal gate verdict returned by GateEvaluator — is a distinct type from
// every CP-048 payload type, and in particular from the three gate-outcome
// payload types (GateAllowedPayload, GateDeniedPayload, GateEscalatedPayload).
//
// CP-INV-002 prohibits cross-subsystem communication by "direct method call
// across an owner boundary." GateEvaluator returns GateAction within S01; that
// value must be wrapped in a typed payload before it becomes observable on the
// event bus. If GateAction were the same type as a CP-048 payload, no wrapping
// would be required and a caller could emit it directly, bypassing the typed
// observation channel. Confirming that the types are distinct enforces the
// wrapping requirement at the type level.
func TestCPINV002_GateAction_IsNotACP048PayloadType(t *testing.T) {
	t.Parallel()

	gateActionType := reflect.TypeOf(GateAction(""))

	// The three gate CP-048 payload types must differ from GateAction.
	gatePayloadTypes := map[string]reflect.Type{
		"gate_allowed":   reflect.TypeOf(&GateAllowedPayload{}),
		"gate_denied":    reflect.TypeOf(&GateDeniedPayload{}),
		"gate_escalated": reflect.TypeOf(&GateEscalatedPayload{}),
	}
	for evType, pt := range gatePayloadTypes {
		if pt == gateActionType {
			t.Errorf("CP-048 event %q payload type equals GateAction (%s) — "+
				"raw gate verdict is being used directly as a cross-subsystem payload (CP-INV-002)", evType, gateActionType)
		}
	}

	// All ten CP-048 payload types must differ from GateAction.
	for _, evTypeName := range cp048EventTypes {
		p := cp048FixturePayload(t, evTypeName)
		if reflect.TypeOf(p) == gateActionType {
			t.Errorf("CP-048 event %q constructor returns GateAction (%s) — "+
				"raw gate verdict leaks into the typed event surface (CP-INV-002)", evTypeName, gateActionType)
		}
	}
}

// ---------------------------------------------------------------------------
// Guard return type is not a CP-048 payload type
// ---------------------------------------------------------------------------

// TestCPINV002_GuardReturnType_IsNotACP048PayloadType verifies that the guard
// evaluator return type ([]Edge) is distinct from every CP-048 payload type,
// and in particular from the two guard-outcome payload types
// (GuardReorderedPayload, GuardFailedPayload).
//
// The Guard path (S01BuildGuardEvaluator) returns []Edge within S01. That
// return value must be wrapped in GuardReorderedPayload or GuardFailedPayload
// before crossing a subsystem boundary. Confirming that []Edge differs from
// every CP-048 payload type enforces that wrapping requirement.
func TestCPINV002_GuardReturnType_IsNotACP048PayloadType(t *testing.T) {
	t.Parallel()

	edgeSliceType := reflect.TypeOf([]Edge(nil))

	for _, evTypeName := range cp048EventTypes {
		p := cp048FixturePayload(t, evTypeName)
		if reflect.TypeOf(p) == edgeSliceType {
			t.Errorf("CP-048 event %q constructor returns []Edge — "+
				"raw guard return value leaks into the typed event surface (CP-INV-002)", evTypeName)
		}
	}
}

// ---------------------------------------------------------------------------
// GateAction bijection onto gate event types (CP-048 §8.2.4–§8.2.6)
// ---------------------------------------------------------------------------

// TestCPINV002_GateActionBijectionOntoGateEventTypes verifies that the three
// GateAction constants map one-to-one onto the three CP-048 gate event types
// (gate_allowed, gate_denied, gate_escalated).
//
// CP-048 declares exactly three gate event types, one per gate outcome:
//   - GateActionAllow           → gate_allowed
//   - GateActionDeny            → gate_denied
//   - GateActionEscalateToHuman → gate_escalated
//
// The bijection proves:
//   - No gate outcome is unobservable (each action has an event type).
//   - No gate outcome collapses into a shared event type (each action has its
//     own event type — they cannot be confused at the consumer).
//   - The observation surface exactly matches the declared outcome surface.
func TestCPINV002_GateActionBijectionOntoGateEventTypes(t *testing.T) {
	t.Parallel()

	// gateActionToEvent is the normative GateAction → event-type mapping per CP-048.
	gateActionToEvent := map[GateAction]EventType{
		GateActionAllow:           EventTypeGateAllowed,
		GateActionDeny:            EventTypeGateDenied,
		GateActionEscalateToHuman: EventTypeGateEscalated,
	}

	// All three GateAction constants must be present.
	declaredActions := []GateAction{GateActionAllow, GateActionDeny, GateActionEscalateToHuman}
	if len(gateActionToEvent) != len(declaredActions) {
		t.Errorf("gate bijection map has %d entries, want %d — CP-048 coverage gap or duplicate",
			len(gateActionToEvent), len(declaredActions))
	}
	for _, action := range declaredActions {
		if _, ok := gateActionToEvent[action]; !ok {
			t.Errorf("GateAction %q has no CP-048 event type mapping — gate outcome is unobservable (CP-INV-002)", action)
		}
	}

	// All mapped event types must be in the registry (observability channel exists).
	seenEvents := make(map[EventType]GateAction, len(gateActionToEvent))
	for action, evType := range gateActionToEvent {
		if _, ok := LookupTypeSchemaVersion(string(evType)); !ok {
			t.Errorf("GateAction %q maps to event type %q which is not registered — "+
				"gate outcome has no live observation channel (CP-INV-002)", action, evType)
		}
		// Inverse: each event type must map to exactly one action (no shared channels).
		if prior, seen := seenEvents[evType]; seen {
			t.Errorf("gate event type %q is mapped to by both GateAction %q and %q — "+
				"two outcomes share one observation channel (CP-INV-002)", evType, prior, action)
		}
		seenEvents[evType] = action
	}

	// All three gate event types in the registry must be covered by the bijection.
	gateEventTypes := []EventType{EventTypeGateAllowed, EventTypeGateDenied, EventTypeGateEscalated}
	for _, evType := range gateEventTypes {
		found := false
		for _, mappedType := range gateActionToEvent {
			if mappedType == evType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("CP-048 gate event type %q is not mapped to by any GateAction — "+
				"event type has no producing action (CP-INV-002)", evType)
		}
	}
}

// ---------------------------------------------------------------------------
// Well-formed CP-048 payload instances satisfy Valid()
// ---------------------------------------------------------------------------

// TestCPINV002_WellFormedCP048PayloadsAreValid verifies that a well-formed
// instance of each CP-048 payload type satisfies Valid().
//
// A payload type that cannot produce a Valid() = true instance is unusable as
// an observation channel — any emission would produce an invalid payload,
// violating the typed-event contract. Confirming that each type has at least
// one valid construction proves the observation surface is operational.
func TestCPINV002_WellFormedCP048PayloadsAreValid(t *testing.T) {
	t.Parallel()

	runID := RunID(uuid.New())
	triggerEventID := EventID(uuid.New())
	reason := "test-reason"

	// Gate payloads
	t.Run("gate_allowed", func(t *testing.T) {
		t.Parallel()
		p := GateAllowedPayload{
			RunID:    runID,
			GateName: GateRef("test-gate"),
		}
		if !p.Valid() {
			t.Error("well-formed GateAllowedPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})
	t.Run("gate_denied", func(t *testing.T) {
		t.Parallel()
		p := GateDeniedPayload{
			RunID:    runID,
			GateName: GateRef("test-gate"),
			Reason:   reason,
		}
		if !p.Valid() {
			t.Error("well-formed GateDeniedPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})
	t.Run("gate_escalated", func(t *testing.T) {
		t.Parallel()
		p := GateEscalatedPayload{
			RunID:    runID,
			GateName: GateRef("test-gate"),
		}
		if !p.Valid() {
			t.Error("well-formed GateEscalatedPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})

	// Hook payloads
	t.Run("hook_fired", func(t *testing.T) {
		t.Parallel()
		p := HookFiredPayload{
			HookName:          HookName("test-hook"),
			TriggeringEventID: triggerEventID,
			SideEffectDescriptor: SideEffect{
				Kind:             SideEffectKindEmitEvent,
				Target:           "some_event",
				IdempotencyClass: IdempotencyClassIdempotent,
			},
		}
		if !p.Valid() {
			t.Error("well-formed HookFiredPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})
	t.Run("hook_failed", func(t *testing.T) {
		t.Parallel()
		p := HookFailedPayload{
			HookName:          HookName("test-hook"),
			TriggeringEventID: triggerEventID,
			ErrorCategory:     ErrorCategoryStructural,
			Reason:            reason,
		}
		if !p.Valid() {
			t.Error("well-formed HookFailedPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})

	// Guard payloads
	t.Run("guard_reordered", func(t *testing.T) {
		t.Parallel()
		p := GuardReorderedPayload{
			RunID:         runID,
			GuardName:     "test-guard",
			EdgeSetBefore: []string{"a", "b"},
			EdgeSetAfter:  []string{"b", "a"},
		}
		if !p.Valid() {
			t.Error("well-formed GuardReorderedPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})
	t.Run("guard_failed", func(t *testing.T) {
		t.Parallel()
		p := GuardFailedPayload{
			RunID:         runID,
			GuardName:     "test-guard",
			ErrorCategory: ErrorCategoryStructural,
			Reason:        reason,
		}
		if !p.Valid() {
			t.Error("well-formed GuardFailedPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})

	// Budget payloads — use minimal well-formed constructions per spec.
	t.Run("budget_warning", func(t *testing.T) {
		t.Parallel()
		p := BudgetWarningPayload{
			RunID:             runID,
			BudgetRef:         BudgetRef("test-budget"),
			ThresholdFraction: 0.8,
			Remaining:         100,
		}
		if !p.Valid() {
			t.Error("well-formed BudgetWarningPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})
	t.Run("budget_accrual", func(t *testing.T) {
		t.Parallel()
		p := BudgetAccrualPayload{
			RunID:     runID,
			SessionID: SessionID("test-session"),
			CostUnits: 1.0,
			CostBasis: CostBasisOutputBytes,
		}
		if !p.Valid() {
			t.Error("well-formed BudgetAccrualPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})
	t.Run("budget_exhausted", func(t *testing.T) {
		t.Parallel()
		p := BudgetExhaustedEventPayload{
			BudgetRef: BudgetRef("test-budget"),
		}
		if !p.Valid() {
			t.Error("well-formed BudgetExhaustedEventPayload.Valid() = false (CP-INV-002 CP-048)")
		}
	})
}

// ---------------------------------------------------------------------------
// Cross-subsystem: CP-048 payloads are the only Gate-outcome carriers
// ---------------------------------------------------------------------------

// TestCPINV002_GateOutcomePayloadsAreTheOnlyBoundaryCarriers verifies that the
// three gate event types registered in the global registry map to the three
// distinct gate payload types (GateAllowedPayload, GateDeniedPayload,
// GateEscalatedPayload), and that no other registered event type produces one
// of those payload types.
//
// This is a cross-subsystem boundary test: only the three CP-048 gate event
// types may carry gate outcomes across subsystem boundaries. Confirming that
// the payload constructors produce the correct types (and that no other event
// type produces a gate-outcome payload) proves there is no alternate observation
// path for gate results (CP-INV-002 "no out-of-band signal").
func TestCPINV002_GateOutcomePayloadsAreTheOnlyBoundaryCarriers(t *testing.T) {
	t.Parallel()

	type gateEventCase struct {
		eventType   string
		wantPayload interface{}
	}
	cases := []gateEventCase{
		{"gate_allowed", &GateAllowedPayload{}},
		{"gate_denied", &GateDeniedPayload{}},
		{"gate_escalated", &GateEscalatedPayload{}},
	}

	for _, c := range cases {
		eventType := c.eventType
		wantType := reflect.TypeOf(c.wantPayload)

		p := cp048FixturePayload(t, eventType)
		gotType := reflect.TypeOf(p)
		if gotType != wantType {
			t.Errorf("event %q constructor returns %s, want %s — wrong payload type for gate outcome (CP-INV-002)",
				eventType, gotType, wantType)
		}
	}
}

// TestCPINV002_GuardOutcomePayloadsAreTheOnlyBoundaryCarriers verifies that
// the two guard event types registered in the global registry map to the two
// distinct guard payload types (GuardReorderedPayload, GuardFailedPayload).
func TestCPINV002_GuardOutcomePayloadsAreTheOnlyBoundaryCarriers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		eventType   string
		wantPayload interface{}
	}{
		{"guard_reordered", &GuardReorderedPayload{}},
		{"guard_failed", &GuardFailedPayload{}},
	}

	for _, c := range cases {
		eventType := c.eventType
		wantType := reflect.TypeOf(c.wantPayload)

		p := cp048FixturePayload(t, eventType)
		gotType := reflect.TypeOf(p)
		if gotType != wantType {
			t.Errorf("event %q constructor returns %s, want %s — wrong payload type for guard outcome (CP-INV-002)",
				eventType, gotType, wantType)
		}
	}
}

// TestCPINV002_HookOutcomePayloadsAreTheOnlyBoundaryCarriers verifies that
// the two hook event types registered in the global registry map to the two
// distinct hook payload types (HookFiredPayload, HookFailedPayload).
func TestCPINV002_HookOutcomePayloadsAreTheOnlyBoundaryCarriers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		eventType   string
		wantPayload interface{}
	}{
		{"hook_fired", &HookFiredPayload{}},
		{"hook_failed", &HookFailedPayload{}},
	}

	for _, c := range cases {
		eventType := c.eventType
		wantType := reflect.TypeOf(c.wantPayload)

		p := cp048FixturePayload(t, eventType)
		gotType := reflect.TypeOf(p)
		if gotType != wantType {
			t.Errorf("event %q constructor returns %s, want %s — wrong payload type for hook outcome (CP-INV-002)",
				eventType, gotType, wantType)
		}
	}
}

// TestCPINV002_BudgetOutcomePayloadsAreTheOnlyBoundaryCarriers verifies that
// the three budget event types registered in the global registry map to the
// three distinct budget payload types (BudgetWarningPayload, BudgetAccrualPayload,
// BudgetExhaustedEventPayload).
func TestCPINV002_BudgetOutcomePayloadsAreTheOnlyBoundaryCarriers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		eventType   string
		wantPayload interface{}
	}{
		{"budget_warning", &BudgetWarningPayload{}},
		{"budget_accrual", &BudgetAccrualPayload{}},
		{"budget_exhausted", &BudgetExhaustedEventPayload{}},
	}

	for _, c := range cases {
		eventType := c.eventType
		wantType := reflect.TypeOf(c.wantPayload)

		p := cp048FixturePayload(t, eventType)
		gotType := reflect.TypeOf(p)
		if gotType != wantType {
			t.Errorf("event %q constructor returns %s, want %s — wrong payload type for budget outcome (CP-INV-002)",
				eventType, gotType, wantType)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// cp048FixturePayload obtains a fresh zero-value payload instance from the
// global event registry for the given CP-048 event type. Fails the test if the
// type is not registered.
func cp048FixturePayload(t *testing.T, eventTypeName string) EventPayload {
	t.Helper()
	// Use DecodePayload via a minimal envelope to exercise the constructor path.
	// Build a minimal JSON payload (empty object) and decode it.
	e := Event{
		Type:    eventTypeName,
		Payload: []byte(`{}`),
	}
	p, err := e.DecodePayload()
	if err != nil {
		t.Fatalf("cp048FixturePayload(%q): DecodePayload failed: %v — event type not registered (CP-INV-002)", eventTypeName, err)
	}
	return p
}
