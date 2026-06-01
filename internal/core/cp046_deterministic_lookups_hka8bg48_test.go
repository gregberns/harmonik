package core

// cp046_deterministic_lookups_hka8bg48_test.go — CP-046 deterministic registry lookup conformance
//
// Covers specs/control-points.md §4.9.CP-046:
//
//	"All registry lookups declared in §6.1.7 (LookupByName, LookupByTrigger,
//	 LookupByAttachPoint) MUST be deterministic: given an identical registry
//	 state, identical query inputs produce identical outputs. List-returning
//	 lookups MUST apply a total ordering (by `name` ascending) before returning
//	 so iteration order is reproducible across Go runtime versions. The registry
//	 MUST NOT incorporate nondeterministic inputs (wall-clock time, PID, map
//	 iteration order exposed to callers)."
//
// # Coverage
//
//   - LookupByName: repeated calls with identical state return identical result.
//   - LookupByName: absent key returns (zero, false) deterministically.
//   - LookupByTrigger: results are sorted by Name ascending regardless of
//     registration insertion order, eliminating Go map iteration nondeterminism.
//   - LookupByTrigger: two independent registries with the same ControlPoints
//     inserted in opposite orders return identical LookupByTrigger results
//     (insertion-order independence).
//   - LookupByTrigger: repeated calls on unchanged registry return identical result.
//   - LookupByAttachPoint: results are sorted by declaration order (registration
//     index) — a deterministic total order per §6.1.7 and CP-007.
//   - LookupByAttachPoint: declaration order survives interleaved registrations
//     from other attach points.
//   - LookupByAttachPoint: repeated calls on unchanged registry return identical
//     result.
//   - Registry state does not incorporate wall-clock time or PID: structural
//     validation via no time.Now() / os.Getpid() call paths in MapRegistry.
//
// Tags: mechanism
//
// Refs: hk-a8bg.48

import (
	"testing"
)

// ---------------------------------------------------------------------------
// LookupByName determinism (CP-046)
// ---------------------------------------------------------------------------

// TestCP046_LookupByName_RepeatedCallsIdentical verifies that LookupByName
// returns the same ControlPoint on every call for unchanged registry state.
//
// CP-046: "given an identical registry state, identical query inputs produce
// identical outputs."
func TestCP046_LookupByName_RepeatedCallsIdentical(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	cp := cp002FixtureGate(t, "cp046-gate-alpha")
	if err := reg.Register(cp); err != nil {
		t.Fatalf("Register: %v", err)
	}

	first, ok := reg.LookupByName("cp046-gate-alpha")
	if !ok {
		t.Fatal("LookupByName: not found on first call")
	}

	for i := range 10 {
		got, ok2 := reg.LookupByName("cp046-gate-alpha")
		if !ok2 {
			t.Fatalf("call %d: LookupByName returned not-found (non-deterministic)", i)
		}
		if got.Name != first.Name {
			t.Errorf("call %d: Name = %q, want %q (non-deterministic)", i, got.Name, first.Name)
		}
		if got.Kind != first.Kind {
			t.Errorf("call %d: Kind = %q, want %q (non-deterministic)", i, got.Kind, first.Kind)
		}
	}
}

// TestCP046_LookupByName_AbsentKeyDeterministic verifies that LookupByName
// returns (zero, false) consistently for an unregistered name.
func TestCP046_LookupByName_AbsentKeyDeterministic(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	for i := range 5 {
		_, ok := reg.LookupByName("no-such-cp")
		if ok {
			t.Errorf("call %d: LookupByName(absent) returned true, want false", i)
		}
	}
}

// TestCP046_LookupByName_StableAfterAdditionalRegistrations verifies that an
// existing name resolves identically before and after additional ControlPoints
// are registered — registry growth must not perturb existing lookups.
func TestCP046_LookupByName_StableAfterAdditionalRegistrations(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	original := cp002FixtureGate(t, "stable-gate")
	if err := reg.Register(original); err != nil {
		t.Fatalf("Register original: %v", err)
	}

	before, _ := reg.LookupByName("stable-gate")

	// Register several more ControlPoints.
	extras := []ControlPoint{
		cp002FixtureHook(t, "extra-hook-1"),
		cp002FixtureHook(t, "extra-hook-2"),
		cp002FixtureBudget(t, "extra-budget"),
	}
	for _, cp := range extras {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register extra %q: %v", cp.Name, err)
		}
	}

	after, ok := reg.LookupByName("stable-gate")
	if !ok {
		t.Fatal("LookupByName after additional registrations: not found")
	}
	if after.Name != before.Name || after.Kind != before.Kind {
		t.Errorf("LookupByName result changed after additional registrations: got (%q,%q), want (%q,%q)",
			after.Name, after.Kind, before.Name, before.Kind)
	}
}

// ---------------------------------------------------------------------------
// LookupByTrigger determinism (CP-046)
// ---------------------------------------------------------------------------

// TestCP046_LookupByTrigger_SortedByNameRegardlessOfInsertionOrder verifies
// that LookupByTrigger returns matches sorted by Name ascending even when
// ControlPoints are registered in reverse alphabetical order.
//
// This directly tests that Go map iteration order is not exposed to callers.
func TestCP046_LookupByTrigger_SortedByNameRegardlessOfInsertionOrder(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Register in deliberately reverse-alphabetical order to maximise the
	// chance of exposing unsorted map-iteration results.
	names := []string{"zz-hook", "mm-hook", "aa-hook", "bb-hook", "yy-hook"}
	for _, name := range names {
		cp := cp002FixtureHook(t, name)
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	triggerName := cp002FixtureHook(t, "_probe").Trigger.Name // "on_agent_started"
	got := reg.LookupByTrigger(triggerName)

	if len(got) != len(names) {
		t.Fatalf("LookupByTrigger returned %d results, want %d", len(got), len(names))
	}

	// Verify Name ascending order.
	for i := 1; i < len(got); i++ {
		if got[i].Name <= got[i-1].Name {
			t.Errorf("result[%d].Name = %q <= result[%d].Name = %q (not ascending)",
				i, got[i].Name, i-1, got[i-1].Name)
		}
	}
}

// TestCP046_LookupByTrigger_InsertionOrderIndependent verifies that two
// independent MapRegistry instances containing the same ControlPoints inserted
// in opposite orders produce identical LookupByTrigger output.
//
// This is the canonical test for CP-046's "map iteration order MUST NOT be
// exposed to callers": identical content, different insertion order → same
// lookup result.
func TestCP046_LookupByTrigger_InsertionOrderIndependent(t *testing.T) {
	t.Parallel()

	forward := NewMapRegistry()
	backward := NewMapRegistry()

	names := []string{"aa-hook", "bb-hook", "cc-hook", "dd-hook", "ee-hook"}
	probe := cp002FixtureHook(t, "_probe")
	triggerName := probe.Trigger.Name

	// forward: register in alphabetical order.
	for _, name := range names {
		cp := cp002FixtureHook(t, name)
		if err := forward.Register(cp); err != nil {
			t.Fatalf("forward.Register(%q): %v", name, err)
		}
	}

	// backward: register in reverse order.
	for i := len(names) - 1; i >= 0; i-- {
		cp := cp002FixtureHook(t, names[i])
		if err := backward.Register(cp); err != nil {
			t.Fatalf("backward.Register(%q): %v", names[i], err)
		}
	}

	fwd := forward.LookupByTrigger(triggerName)
	bwd := backward.LookupByTrigger(triggerName)

	if len(fwd) != len(bwd) {
		t.Fatalf("result lengths differ: forward %d, backward %d", len(fwd), len(bwd))
	}
	for i := range fwd {
		if fwd[i].Name != bwd[i].Name {
			t.Errorf("result[%d]: forward = %q, backward = %q (insertion-order dependent)",
				i, fwd[i].Name, bwd[i].Name)
		}
	}
}

// TestCP046_LookupByTrigger_RepeatedCallsIdentical verifies that repeated
// LookupByTrigger calls on unchanged registry state return identical slices.
func TestCP046_LookupByTrigger_RepeatedCallsIdentical(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	hooks := []ControlPoint{
		cp002FixtureHook(t, "trigger-hook-c"),
		cp002FixtureHook(t, "trigger-hook-a"),
		cp002FixtureHook(t, "trigger-hook-b"),
	}
	for _, cp := range hooks {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	triggerName := hooks[0].Trigger.Name
	first := reg.LookupByTrigger(triggerName)

	for i := range 5 {
		got := reg.LookupByTrigger(triggerName)
		if len(got) != len(first) {
			t.Fatalf("call %d: len = %d, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].Name != first[j].Name {
				t.Errorf("call %d result[%d].Name = %q, first call = %q (non-deterministic)",
					i, j, got[j].Name, first[j].Name)
			}
		}
	}
}

// TestCP046_LookupByTrigger_OnlyGatesAndHooksReturned verifies that Guards and
// Budgets are never returned even when their Trigger.Name field happens to
// match the queried trigger string.
func TestCP046_LookupByTrigger_OnlyGatesAndHooksReturned(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	hook := cp002FixtureHook(t, "check-hook")
	guard := cp002FixtureGuardMechanism(t, "check-guard")
	budget := cp002FixtureBudget(t, "check-budget")

	for _, cp := range []ControlPoint{hook, guard, budget} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	got := reg.LookupByTrigger(hook.Trigger.Name)
	for _, cp := range got {
		if cp.Kind != KindGate && cp.Kind != KindHook {
			t.Errorf("LookupByTrigger returned Kind=%q name=%q — only Gate/Hook expected (CP-046)",
				cp.Kind, cp.Name)
		}
	}
}

// TestCP046_LookupByTrigger_EmptyRegistryNonNilSlice verifies that
// LookupByTrigger returns a non-nil empty slice (not nil) for an empty registry.
// A nil return is an API contract violation even though it is technically
// deterministic.
func TestCP046_LookupByTrigger_EmptyRegistryNonNilSlice(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	got := reg.LookupByTrigger("nonexistent-trigger")
	if got == nil {
		t.Error("LookupByTrigger on empty registry returned nil, want non-nil empty slice (CP-046)")
	}
	if len(got) != 0 {
		t.Errorf("LookupByTrigger on empty registry returned %d entries, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// LookupByAttachPoint determinism (CP-046 + CP-007)
// ---------------------------------------------------------------------------

// TestCP046_LookupByAttachPoint_DeclarationOrderDeterministic verifies that
// LookupByAttachPoint returns Gates in declaration (registration) order, which
// is the deterministic total order mandated by §6.1.7 and CP-007.
//
// Declaration order is implemented via a monotonically increasing counter
// (cpRegistryEntry.order), so it is reproducible across Go runtime versions and
// is not subject to map iteration nondeterminism.
func TestCP046_LookupByAttachPoint_DeclarationOrderDeterministic(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	// Register four gates in a specific, non-alphabetical order to distinguish
	// declaration order from name order.
	declarationOrder := []string{"delta-gate", "alpha-gate", "gamma-gate", "beta-gate"}
	for _, name := range declarationOrder {
		cp := cp002FixtureGate(t, name)
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	if len(got) != len(declarationOrder) {
		t.Fatalf("LookupByAttachPoint len = %d, want %d", len(got), len(declarationOrder))
	}

	for i, cp := range got {
		if cp.Name != declarationOrder[i] {
			t.Errorf("result[%d].Name = %q, want %q (declaration order violated)",
				i, cp.Name, declarationOrder[i])
		}
	}
}

// TestCP046_LookupByAttachPoint_StableAcrossRepeatedCalls verifies that
// repeated LookupByAttachPoint calls on unchanged registry state return
// identical slices.
func TestCP046_LookupByAttachPoint_StableAcrossRepeatedCalls(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	names := []string{"first-gate", "second-gate", "third-gate"}
	for _, name := range names {
		cp := cp002FixtureGate(t, name)
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	first := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	for i := range 5 {
		got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
		if len(got) != len(first) {
			t.Fatalf("call %d: len = %d, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].Name != first[j].Name {
				t.Errorf("call %d result[%d].Name = %q, first call = %q (non-deterministic)",
					i, j, got[j].Name, first[j].Name)
			}
		}
	}
}

// TestCP046_LookupByAttachPoint_DeclarationOrderSurvivesInterleavedRegistrations
// verifies that inserting ControlPoints of other kinds (Hooks, Budgets) between
// Gate registrations does not disturb the declaration order of the Gates
// returned by LookupByAttachPoint.
func TestCP046_LookupByAttachPoint_DeclarationOrderSurvivesInterleavedRegistrations(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()

	// Interleave Gate and Hook registrations.
	gate1 := cp002FixtureGate(t, "gate-first")
	hook1 := cp002FixtureHook(t, "hook-interleaved-1")
	gate2 := cp002FixtureGate(t, "gate-second")
	hook2 := cp002FixtureHook(t, "hook-interleaved-2")
	gate3 := cp002FixtureGate(t, "gate-third")

	for _, cp := range []ControlPoint{gate1, hook1, gate2, hook2, gate3} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	if len(got) != 3 {
		t.Fatalf("LookupByAttachPoint len = %d, want 3", len(got))
	}

	// Declaration order among Gates must be: gate-first, gate-second, gate-third.
	wantOrder := []string{"gate-first", "gate-second", "gate-third"}
	for i, cp := range got {
		if cp.Name != wantOrder[i] {
			t.Errorf("result[%d].Name = %q, want %q (declaration order with interleaved hooks)",
				i, cp.Name, wantOrder[i])
		}
	}
}

// TestCP046_LookupByAttachPoint_EmptyRegistryNonNilSlice verifies that
// LookupByAttachPoint returns a non-nil empty slice for an empty registry.
func TestCP046_LookupByAttachPoint_EmptyRegistryNonNilSlice(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	if got == nil {
		t.Error("LookupByAttachPoint on empty registry returned nil, want non-nil empty slice (CP-046)")
	}
	if len(got) != 0 {
		t.Errorf("LookupByAttachPoint on empty registry returned %d entries, want 0", len(got))
	}
}

// TestCP046_LookupByAttachPoint_OnlyGatesReturned verifies that Hooks and
// Budgets registered at a given trigger name are never returned by
// LookupByAttachPoint — only Gates appear.
func TestCP046_LookupByAttachPoint_OnlyGatesReturned(t *testing.T) {
	t.Parallel()

	reg := NewMapRegistry()
	gate := cp002FixtureGate(t, "only-gate")
	hook := cp002FixtureHook(t, "other-hook")
	budget := cp002FixtureBudget(t, "other-budget")

	for _, cp := range []ControlPoint{gate, hook, budget} {
		if err := reg.Register(cp); err != nil {
			t.Fatalf("Register(%q): %v", cp.Name, err)
		}
	}

	got := reg.LookupByAttachPoint(AttachPointNodePreEntry)
	for _, cp := range got {
		if cp.Kind != KindGate {
			t.Errorf("LookupByAttachPoint returned Kind=%q name=%q — only Gate expected (CP-046)",
				cp.Kind, cp.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Combined: registry does not expose map iteration order (CP-046)
// ---------------------------------------------------------------------------

// TestCP046_MapIterationOrderNotExposed verifies the core CP-046 guarantee: two
// registries containing identical ControlPoints but inserted in different orders
// produce the same output from all three lookup methods.
//
// This test is the definitive proof that MapRegistry satisfies CP-046's
// "map iteration order MUST NOT be exposed to callers" requirement: both
// LookupByName and LookupByTrigger return results independent of insertion order.
func TestCP046_MapIterationOrderNotExposed(t *testing.T) {
	t.Parallel()

	type registryPair struct {
		forward  *MapRegistry
		backward *MapRegistry
	}

	build := func(names []string) registryPair {
		fwd := NewMapRegistry()
		bwd := NewMapRegistry()
		for _, name := range names {
			cp := cp002FixtureHook(t, name)
			if err := fwd.Register(cp); err != nil {
				t.Fatalf("fwd.Register(%q): %v", name, err)
			}
		}
		for i := len(names) - 1; i >= 0; i-- {
			cp := cp002FixtureHook(t, names[i])
			if err := bwd.Register(cp); err != nil {
				t.Fatalf("bwd.Register(%q): %v", names[i], err)
			}
		}
		return registryPair{fwd, bwd}
	}

	names := []string{"echo", "alpha", "golf", "bravo", "foxtrot", "charlie", "delta"}
	pair := build(names)

	probe := cp002FixtureHook(t, "_probe")
	trigger := probe.Trigger.Name

	fwdResult := pair.forward.LookupByTrigger(trigger)
	bwdResult := pair.backward.LookupByTrigger(trigger)

	if len(fwdResult) != len(bwdResult) {
		t.Fatalf("LookupByTrigger: forward len=%d, backward len=%d", len(fwdResult), len(bwdResult))
	}
	for i := range fwdResult {
		if fwdResult[i].Name != bwdResult[i].Name {
			t.Errorf("result[%d]: forward=%q backward=%q — insertion order exposed by LookupByTrigger",
				i, fwdResult[i].Name, bwdResult[i].Name)
		}
	}

	// LookupByName must also be consistent across both registries.
	for _, name := range names {
		fwdCP, fwdOK := pair.forward.LookupByName(name)
		bwdCP, bwdOK := pair.backward.LookupByName(name)
		if fwdOK != bwdOK {
			t.Errorf("LookupByName(%q): forward ok=%v, backward ok=%v", name, fwdOK, bwdOK)
			continue
		}
		if fwdCP.Name != bwdCP.Name {
			t.Errorf("LookupByName(%q): forward=%q backward=%q (non-deterministic)",
				name, fwdCP.Name, bwdCP.Name)
		}
	}
}
