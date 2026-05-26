package core

// cpregistry_hka8bg2_test.go — MapRegistry name-uniqueness tests (hk-a8bg.2)
//
// Covers specs/control-points.md §4.1.CP-002 (ControlPoint name is unique
// within the registry) and the associated MapRegistry registration contract:
//
//   - CP-002: every ControlPoint carries a unique name within the registry.
//   - CP-001: structurally invalid ControlPoints (including empty Name) are
//     rejected at registration.
//   - CP-020: cognition-tagged Guard is rejected at registration.
//   - CP-044: re-registration with identical body → silent success; divergent
//     body under the same name → ErrDivergentBody.
//   - CP-046: All(), LookupByTrigger() return results sorted by Name ascending.
//   - CP-007: LookupByAttachPoint() returns Gates sorted by declaration order.
//   - CP-045: daemon-local scope — independent MapRegistry instances share no
//     state.
//
// # Helper prefix: cp002Fixture

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// cp002FixtureGate returns a valid Gate ControlPoint with the given name.
// Uses registryFixtureControlPoint to ensure structural validity.
func cp002FixtureGate(t *testing.T, name string) ControlPoint {
	t.Helper()
	cp := registryFixtureControlPoint(t, KindGate)
	cp.Name = name
	return cp
}

// cp002FixtureHook returns a valid Hook ControlPoint with the given name.
func cp002FixtureHook(t *testing.T, name string) ControlPoint {
	t.Helper()
	cp := registryFixtureControlPoint(t, KindHook)
	cp.Name = name
	return cp
}

// cp002FixtureGuardMechanism returns a valid mechanism-tagged Guard ControlPoint.
func cp002FixtureGuardMechanism(t *testing.T, name string) ControlPoint {
	t.Helper()
	cp := registryFixtureControlPoint(t, KindGuard)
	cp.Name = name
	return cp
}

// cp002FixtureBudget returns a valid Budget ControlPoint with the given name.
func cp002FixtureBudget(t *testing.T, name string) ControlPoint {
	t.Helper()
	cp := registryFixtureControlPoint(t, KindBudget)
	cp.Name = name
	return cp
}

// cp002FixtureCognitionGuard returns a cognition-tagged Guard ControlPoint — invalid for registration per CP-020.
func cp002FixtureCognitionGuard(t *testing.T, name string) ControlPoint {
	t.Helper()
	dp := &DelegationPath{
		Role:              "reviewer",
		ModelClass:        "reviewer-tier-1",
		InputSchemaRef:    "guard-input-v1",
		ResponseSchemaRef: "guard-response-v1",
		PromptTemplateRef: "guard-prompt-v1",
	}
	nodeID := NodeID("node-review")
	return ControlPoint{
		Name:          name,
		Kind:          KindGuard,
		Trigger:       Trigger{Name: ""},
		Evaluator:     Evaluator{Mode: ModeTagCognition, DelegationPath: dp},
		OutcomeAction: OutcomeActionReorder,
		Payload:       KindPayload{Guard: &GuardPayload{AppliesToNode: &nodeID}},
		Axes:          BaselineAxisTags,
		ModeTag:       ModeTagCognition,
		SchemaVersion: 1,
	}
}

// ---------------------------------------------------------------------------
// CP-001 + CP-002: structural validity and name uniqueness enforcement
// ---------------------------------------------------------------------------

// TestMapRegistry_RegisterValid_AllKinds verifies that a structurally valid
// ControlPoint of each Kind registers without error (positive path for CP-001
// and CP-002).
func TestMapRegistry_RegisterValid_AllKinds(t *testing.T) {
	t.Parallel()

	kinds := []Kind{KindGate, KindHook, KindGuard, KindBudget}
	for _, k := range kinds {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()

			reg := NewMapRegistry()
			cp := registryFixtureControlPoint(t, k)

			if err := reg.Register(cp); err != nil {
				t.Errorf("Register valid %s ControlPoint: unexpected error: %v", k, err)
			}

			// CP-002: LookupByName returns the registered ControlPoint.
			got, ok := reg.LookupByName(cp.Name)
			if !ok {
				t.Fatalf("LookupByName(%q) not found after registration", cp.Name)
			}
			if got.Name != cp.Name {
				t.Errorf("LookupByName(%q).Name = %q, want %q", cp.Name, got.Name, cp.Name)
			}
		})
	}
}

// TestMapRegistry_RegisterEmptyName verifies that a ControlPoint with an empty
// Name is rejected at registration with ErrInvalidControlPoint (CP-001 via
// cp.Valid() gate).
//
// specs/control-points.md §4.1.CP-002: the name MUST be non-empty (it serves
// as the registry key and DOT attribute resolution target).
func TestMapRegistry_RegisterEmptyName(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	cp := registryFixtureControlPoint(t, KindGate)
	cp.Name = "" // invalid: CP-001 via Valid()

	err := reg.Register(cp)
	if err == nil {
		t.Fatal("Register with empty Name: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidControlPoint) {
		t.Errorf("Register with empty Name: got %v, want ErrInvalidControlPoint", err)
	}

	// Registry must remain empty.
	if len(reg.All()) != 0 {
		t.Errorf("registry has %d entries after empty-Name rejection, want 0", len(reg.All()))
	}
}

// TestMapRegistry_RegisterInvalidControlPoint verifies that a structurally
// invalid ControlPoint is rejected with ErrInvalidControlPoint (CP-001).
func TestMapRegistry_RegisterInvalidControlPoint(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	cp := registryFixtureControlPoint(t, KindGate)
	cp.SchemaVersion = 0 // invalid: must be ≥ 1 (CP-038)

	err := reg.Register(cp)
	if err == nil {
		t.Fatal("Register with invalid ControlPoint: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidControlPoint) {
		t.Errorf("Register with invalid ControlPoint: got %v, want ErrInvalidControlPoint", err)
	}
}

// ---------------------------------------------------------------------------
// CP-002: name uniqueness — positive (distinct names coexist)
// ---------------------------------------------------------------------------

// TestMapRegistry_DistinctNamesCoexist verifies that two ControlPoints with
// different names are both accepted and accessible via LookupByName (CP-002).
func TestMapRegistry_DistinctNamesCoexist(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	alpha := cp002FixtureGate(t, "alpha-gate")
	beta := cp002FixtureHook(t, "beta-hook")

	if err := reg.Register(alpha); err != nil {
		t.Fatalf("Register alpha: %v", err)
	}
	if err := reg.Register(beta); err != nil {
		t.Fatalf("Register beta: %v", err)
	}

	// Both are accessible.
	for _, name := range []string{"alpha-gate", "beta-hook"} {
		if _, ok := reg.LookupByName(name); !ok {
			t.Errorf("LookupByName(%q) = not found, want found", name)
		}
	}

	// Registry has exactly two entries.
	all := reg.All()
	if len(all) != 2 {
		t.Errorf("All() len = %d, want 2", len(all))
	}
}

// ---------------------------------------------------------------------------
// CP-044: idempotent re-registration and divergent-body rejection
// ---------------------------------------------------------------------------

// TestMapRegistry_IdempotentByNameIdenticalBody verifies that registering a
// ControlPoint with the same name and identical body succeeds silently (CP-044).
func TestMapRegistry_IdempotentByNameIdenticalBody(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	cp := cp002FixtureGate(t, "gate-alpha")

	if err := reg.Register(cp); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	// Second registration — identical body.
	if err := reg.Register(cp); err != nil {
		t.Errorf("second Register (identical body): unexpected error: %v", err)
	}

	// Still exactly one entry.
	all := reg.All()
	if len(all) != 1 {
		t.Errorf("All() len = %d after idempotent re-registration, want 1", len(all))
	}
}

// TestMapRegistry_DivergentBodyRejected verifies that registering a different
// body under an existing name fails with ErrDivergentBody (CP-044).
//
// "Different body" means (Kind, Trigger, Evaluator, Payload) differ; here we
// change the Trigger to produce a different body hash.
func TestMapRegistry_DivergentBodyRejected(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	original := cp002FixtureGate(t, "gate-shared")

	if err := reg.Register(original); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Build a divergent version: same name, different Trigger.
	divergent := original
	divergent.Trigger = Trigger{Name: "different-trigger"}

	err := reg.Register(divergent)
	if err == nil {
		t.Fatal("Register with divergent body: expected error, got nil")
	}
	if !errors.Is(err, ErrDivergentBody) {
		t.Errorf("Register with divergent body: got %v, want ErrDivergentBody", err)
	}

	// Registry still has the original entry, unchanged.
	got, ok := reg.LookupByName("gate-shared")
	if !ok {
		t.Fatal("LookupByName after divergent rejection: not found")
	}
	if got.Trigger.Name != original.Trigger.Name {
		t.Errorf("Trigger after divergent rejection = %q, want original %q",
			got.Trigger.Name, original.Trigger.Name)
	}
}

// ---------------------------------------------------------------------------
// CP-020: cognition-tagged Guard rejection
// ---------------------------------------------------------------------------

// TestMapRegistry_CognitionGuardRejected verifies that a Guard ControlPoint
// with a cognition-tagged evaluator is rejected with ErrCognitionGuard (CP-020).
func TestMapRegistry_CognitionGuardRejected(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	bad := cp002FixtureCognitionGuard(t, "bad-guard")

	err := reg.Register(bad)
	if err == nil {
		t.Fatal("Register cognition-tagged Guard: expected error, got nil")
	}
	if !errors.Is(err, ErrCognitionGuard) {
		t.Errorf("Register cognition-tagged Guard: got %v, want ErrCognitionGuard", err)
	}

	// Registry remains empty; the bad registration must not persist.
	if len(reg.All()) != 0 {
		t.Errorf("registry has %d entries after CP-020 rejection, want 0", len(reg.All()))
	}
}

// TestMapRegistry_MechanismGuardAccepted verifies the valid path adjacent to
// CP-020: a mechanism-tagged Guard registers without error.
func TestMapRegistry_MechanismGuardAccepted(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	guard := cp002FixtureGuardMechanism(t, "good-guard")

	if err := reg.Register(guard); err != nil {
		t.Errorf("Register mechanism-tagged Guard: unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CP-046: deterministic ordering
// ---------------------------------------------------------------------------

// TestMapRegistry_AllReturnsSortedByName verifies that All() returns
// ControlPoints in Name-ascending order on every call (CP-046).
func TestMapRegistry_AllReturnsSortedByName(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register in non-alphabetical order to probe sort.
	names := []string{"zz-gate", "aa-hook", "mm-budget"}
	cps := []ControlPoint{
		cp002FixtureGate(t, names[0]),
		cp002FixtureHook(t, names[1]),
		cp002FixtureBudget(t, names[2]),
	}
	for _, cp := range cps {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	all := reg.All()
	if len(all) != len(names) {
		t.Fatalf("All() len = %d, want %d", len(all), len(names))
	}

	for i := 1; i < len(all); i++ {
		if all[i].Name <= all[i-1].Name {
			t.Errorf("All()[%d].Name = %q <= All()[%d].Name = %q (not ascending)",
				i, all[i].Name, i-1, all[i-1].Name)
		}
	}

	// Deterministic: second call must match first.
	all2 := reg.All()
	for i, cp := range all2 {
		if cp.Name != all[i].Name {
			t.Errorf("second All()[%d].Name = %q, first = %q (non-deterministic)",
				i, cp.Name, all[i].Name)
		}
	}
}

// TestMapRegistry_AllReturnsEmptySliceNotNil verifies that All() returns an
// empty non-nil slice when the registry is empty (defensive API contract).
func TestMapRegistry_AllReturnsEmptySliceNotNil(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	all := reg.All()
	if all == nil {
		t.Error("All() on empty registry returned nil, want non-nil empty slice")
	}
	if len(all) != 0 {
		t.Errorf("All() on empty registry len = %d, want 0", len(all))
	}
}

// TestMapRegistry_LookupByNameDeterministic verifies that LookupByName returns
// the same ControlPoint on repeated calls given unchanged registry state (CP-046).
func TestMapRegistry_LookupByNameDeterministic(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	cp := cp002FixtureGate(t, "lookup-gate")
	if err := reg.Register(cp); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := range 5 {
		got, ok := reg.LookupByName("lookup-gate")
		if !ok {
			t.Fatalf("iteration %d: LookupByName not found", i)
		}
		if got.Name != cp.Name {
			t.Errorf("iteration %d: Name = %q, want %q", i, got.Name, cp.Name)
		}
		if got.Kind != cp.Kind {
			t.Errorf("iteration %d: Kind = %q, want %q", i, got.Kind, cp.Kind)
		}
	}
}

// TestMapRegistry_LookupByNameMissing verifies that LookupByName returns false
// when the name is not registered (CP-046 determinism: absent key).
func TestMapRegistry_LookupByNameMissing(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	_, ok := reg.LookupByName("nonexistent")
	if ok {
		t.Error("LookupByName(\"nonexistent\") = true, want false")
	}
}

// ---------------------------------------------------------------------------
// CP-045: daemon-local scope
// ---------------------------------------------------------------------------

// TestMapRegistry_DaemonLocalScope verifies that two independent MapRegistry
// instances share no state, modelling CP-045 (daemon-local registry scope).
func TestMapRegistry_DaemonLocalScope(t *testing.T) {
	t.Parallel()

	reg1 := NewMapRegistry()
	reg2 := NewMapRegistry()

	cp := cp002FixtureGate(t, "shared-name")
	if err := reg1.Register(cp); err != nil {
		t.Fatalf("reg1.Register: %v", err)
	}

	// reg2 is independent: reg1's registration must not appear in reg2.
	_, found := reg2.LookupByName("shared-name")
	if found {
		t.Errorf("reg2 contains %q which was only registered in reg1 — CP-045 violated", cp.Name)
	}

	if len(reg2.All()) != 0 {
		t.Errorf("reg2 has %d entries, want 0 (daemon-local scope per CP-045)", len(reg2.All()))
	}
}

// ---------------------------------------------------------------------------
// CP-007: LookupByAttachPoint declaration-order sorting
// ---------------------------------------------------------------------------

// TestMapRegistry_LookupByAttachPoint_DeclarationOrder verifies that
// LookupByAttachPoint returns Gates at the given attach point sorted by
// declaration order (registration order) per §4.1.CP-007.
func TestMapRegistry_LookupByAttachPoint_DeclarationOrder(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	// Register three Gates at the same attach point in a specific order.
	names := []string{"third-gate", "first-gate", "second-gate"}
	for _, name := range names {
		cp := cp002FixtureGate(t, name)
		// All three use AttachPointNodePreEntry from registryFixtureControlPoint.
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	if len(got) != 3 {
		t.Fatalf("LookupByAttachPoint len = %d, want 3", len(got))
	}

	// Declaration order must match registration order: third, first, second.
	wantOrder := names
	for i, cp := range got {
		if cp.Name != wantOrder[i] {
			t.Errorf("LookupByAttachPoint[%d].Name = %q, want %q (declaration order)", i, cp.Name, wantOrder[i])
		}
	}
}

// TestMapRegistry_LookupByAttachPoint_OtherKindsExcluded verifies that
// LookupByAttachPoint returns only Gates (not Hooks, Guards, or Budgets).
func TestMapRegistry_LookupByAttachPoint_OtherKindsExcluded(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	gate := cp002FixtureGate(t, "my-gate")
	hook := cp002FixtureHook(t, "my-hook")
	budget := cp002FixtureBudget(t, "my-budget")

	for _, cp := range []ControlPoint{gate, hook, budget} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	if len(got) != 1 {
		t.Errorf("LookupByAttachPoint len = %d, want 1 (only Gate)", len(got))
	}
	if len(got) > 0 && got[0].Kind != KindGate {
		t.Errorf("LookupByAttachPoint[0].Kind = %q, want Gate", got[0].Kind)
	}
}

// TestMapRegistry_LookupByAttachPoint_EmptySliceNotNil verifies that
// LookupByAttachPoint returns a non-nil empty slice when nothing matches.
func TestMapRegistry_LookupByAttachPoint_EmptySliceNotNil(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	if got == nil {
		t.Error("LookupByAttachPoint on empty registry returned nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("LookupByAttachPoint on empty registry len = %d, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// LookupByTrigger
// ---------------------------------------------------------------------------

// TestMapRegistry_LookupByTrigger_SortedByName verifies that LookupByTrigger
// returns matching Hooks and Gates sorted by Name ascending (CP-046).
func TestMapRegistry_LookupByTrigger_SortedByName(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register two Hooks with the same trigger event name, in reverse alphabetical order.
	h1 := cp002FixtureHook(t, "zz-hook")
	h2 := cp002FixtureHook(t, "aa-hook")
	// Both use Trigger{Name: "on_agent_started"} from registryFixtureControlPoint(KindHook).
	for _, cp := range []ControlPoint{h1, h2} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	triggerName := h1.Trigger.Name // "on_agent_started"
	got := reg.LookupByTrigger(triggerName)
	if len(got) != 2 {
		t.Fatalf("LookupByTrigger(%q) len = %d, want 2", triggerName, len(got))
	}
	if got[0].Name != "aa-hook" || got[1].Name != "zz-hook" {
		t.Errorf("LookupByTrigger not sorted: got [%q, %q], want [aa-hook, zz-hook]",
			got[0].Name, got[1].Name)
	}
}

// TestMapRegistry_LookupByTrigger_EmptySliceNotNil verifies that
// LookupByTrigger returns a non-nil empty slice when nothing matches.
func TestMapRegistry_LookupByTrigger_EmptySliceNotNil(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	got := reg.LookupByTrigger("no-such-trigger")
	if got == nil {
		t.Error("LookupByTrigger returned nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("LookupByTrigger returned %d entries, want 0", len(got))
	}
}

// TestMapRegistry_LookupByTrigger_GuardsAndBudgetsExcluded verifies that
// Guards and Budgets are not returned by LookupByTrigger even if their
// Trigger.Name matches (CP-002: trigger lookup is Hook+Gate only).
func TestMapRegistry_LookupByTrigger_GuardsAndBudgetsExcluded(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	// Register a Hook and a Guard. The Guard fixture has Trigger{Name: ""}.
	// The Budget fixture has Trigger{Name: "dispatch"}.
	hook := cp002FixtureHook(t, "relevant-hook")
	guard := cp002FixtureGuardMechanism(t, "irrelevant-guard")
	budget := cp002FixtureBudget(t, "irrelevant-budget")

	for _, cp := range []ControlPoint{hook, guard, budget} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	// Lookup by the Hook's trigger name.
	triggerName := hook.Trigger.Name
	got := reg.LookupByTrigger(triggerName)
	for _, cp := range got {
		if cp.Kind == KindGuard || cp.Kind == KindBudget {
			t.Errorf("LookupByTrigger returned %s %q — only Gates and Hooks should match",
				cp.Kind, cp.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// CP-002: name is the resolution key for DOT attributes
// ---------------------------------------------------------------------------

// TestMapRegistry_NameIsResolutionKey verifies that Policy YAML and DOT
// attribute references (gate_ref etc.) can resolve to a registered ControlPoint
// via LookupByName using the ControlPoint's name as the sole key (CP-002).
//
// This test exercises the name-as-key lookup that DOT workflow attributes rely
// on (execution-model.md §4.2: gate_ref, policy_ref, freedom_profile_ref,
// budget_ref all resolve to registered names).
func TestMapRegistry_NameIsResolutionKey(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Simulate a policy YAML declaring a gate named "deployment-gate".
	gateName := "deployment-gate"
	cp := cp002FixtureGate(t, gateName)
	if err := reg.Register(cp); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// A DOT workflow node carries gate_ref = "deployment-gate".
	// The resolution: LookupByName("deployment-gate") must return the Gate.
	gateRef := gateName // simulates gate_ref DOT attribute value
	resolved, ok := reg.LookupByName(gateRef)
	if !ok {
		t.Fatalf("LookupByName(%q) = not found; DOT gate_ref resolution failed", gateRef)
	}
	if resolved.Kind != KindGate {
		t.Errorf("resolved ControlPoint Kind = %q, want Gate", resolved.Kind)
	}
	if resolved.Name != gateName {
		t.Errorf("resolved ControlPoint Name = %q, want %q", resolved.Name, gateName)
	}
}
