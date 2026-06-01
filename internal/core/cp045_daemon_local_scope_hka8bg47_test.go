package core

// cp045_daemon_local_scope_hka8bg47_test.go — CP-045 daemon-local registry scope conformance
//
// Covers specs/control-points.md §4.9.CP-045:
//
//	"The registry MUST be daemon-scoped (process-scoped per [architecture.md §4.4]).
//	 Cross-daemon sharing MUST NOT occur; every per-project daemon maintains its own
//	 independent registry. Daemon restart rebuilds the registry from policy YAML as a
//	 startup step."
//
// These tests target S02PolicyEngine and MapRegistry as the authoritative
// production implementations of the daemon-local scope invariant. The fixture
// tests in twopassreg_hka8bg85_test.go and cpregistry_hka8bg2_test.go also
// exercise CP-045 indirectly; this file is the named conformance suite.
//
// # Coverage
//
//   - S02PolicyEngine instances do not share registry state (two-daemon model).
//   - S02PolicyEngine.Registry() returns the same stable instance within one daemon.
//   - NewS02PolicyEngine() starts with an empty registry (restart-from-scratch model).
//   - Registrations in one S02PolicyEngine are invisible to a fresh S02PolicyEngine
//     (simulates daemon restart: no cross-restart persistence per CP-045).
//   - NoOpPolicyEngine satisfies daemon-local scope trivially (always-empty registry).
//   - MapRegistry global-state absence: concurrent independent instances do not
//     interfere under parallel registration.
//
// Tags: mechanism
//
// Refs: hk-a8bg.47

import (
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// S02PolicyEngine daemon-local scope
// ---------------------------------------------------------------------------

// TestCP045_S02PolicyEngine_IndependentRegistries verifies that two
// S02PolicyEngine instances do not share registry state, modelling CP-045's
// "every per-project daemon maintains its own independent registry" requirement.
//
// This simulates two daemons running on the same host: each must be isolated.
func TestCP045_S02PolicyEngine_IndependentRegistries(t *testing.T) {
	t.Parallel()

	daemon1 := NewS02PolicyEngine()
	daemon2 := NewS02PolicyEngine()

	cp := cp002FixtureGate(t, "cross-daemon-gate")
	if err := daemon1.RegisterControlPoints([]ControlPoint{cp}); err != nil {
		t.Fatalf("daemon1.RegisterControlPoints: %v", err)
	}

	// daemon2 must not see daemon1's registration.
	_, found := daemon2.Registry().LookupByName("cross-daemon-gate")
	if found {
		t.Errorf("daemon2 sees %q registered in daemon1 — CP-045 cross-daemon isolation violated", cp.Name)
	}
	if len(daemon2.Registry().All()) != 0 {
		t.Errorf("daemon2 registry has %d entries, want 0 (no cross-daemon sharing per CP-045)",
			len(daemon2.Registry().All()))
	}
}

// TestCP045_S02PolicyEngine_RegistryStableWithinDaemon verifies that
// S02PolicyEngine.Registry() returns the same stable Registry instance across
// multiple calls within one daemon lifetime.
//
// CP-045 requires daemon-scoped (process-scoped) state: the registry MUST NOT
// be re-created per call; all subsystems that call Registry() within the same
// daemon MUST share the same in-process table.
func TestCP045_S02PolicyEngine_RegistryStableWithinDaemon(t *testing.T) {
	t.Parallel()

	engine := NewS02PolicyEngine()

	r1 := engine.Registry()
	r2 := engine.Registry()
	r3 := engine.Registry()

	// Register via r1 and verify visibility in r2 and r3 — proving same table.
	cp := cp002FixtureGate(t, "stable-registry-gate")
	if err := r1.(interface{ Register(ControlPoint) error }).Register(cp); err != nil {
		t.Fatalf("r1.Register: %v", err)
	}

	if _, ok := r2.LookupByName("stable-registry-gate"); !ok {
		t.Error("r2.LookupByName did not find entry registered via r1 — registry is not stable within daemon")
	}
	if _, ok := r3.LookupByName("stable-registry-gate"); !ok {
		t.Error("r3.LookupByName did not find entry registered via r1 — registry is not stable within daemon")
	}
}

// TestCP045_S02PolicyEngine_FreshRegistryOnConstruction verifies that
// NewS02PolicyEngine() always constructs an empty registry, modelling the
// "daemon restart rebuilds from policy YAML" requirement of CP-045.
//
// A restarted daemon must not inherit the in-process state of its predecessor;
// the registry is ephemeral. This test shows that constructing a new
// S02PolicyEngine (the daemon-restart analog) starts with a clean slate.
func TestCP045_S02PolicyEngine_FreshRegistryOnConstruction(t *testing.T) {
	t.Parallel()

	// First daemon: populate the registry.
	firstDaemon := NewS02PolicyEngine()
	cp := cp002FixtureGate(t, "pre-restart-gate")
	if err := firstDaemon.RegisterControlPoints([]ControlPoint{cp}); err != nil {
		t.Fatalf("firstDaemon.RegisterControlPoints: %v", err)
	}
	if len(firstDaemon.Registry().All()) != 1 {
		t.Fatalf("firstDaemon should have 1 entry, got %d", len(firstDaemon.Registry().All()))
	}

	// Simulate daemon restart: construct a new engine.
	restartedDaemon := NewS02PolicyEngine()

	// Restarted daemon must start empty — no persistence from the first daemon.
	if len(restartedDaemon.Registry().All()) != 0 {
		t.Errorf("restarted daemon registry has %d entries, want 0 (ephemeral per CP-045)",
			len(restartedDaemon.Registry().All()))
	}
	if _, found := restartedDaemon.Registry().LookupByName("pre-restart-gate"); found {
		t.Errorf("restarted daemon sees %q — cross-restart state leak violates CP-045", cp.Name)
	}
}

// TestCP045_S02PolicyEngine_RegisteredControlPointsVisibleViaRegistry verifies
// the full round-trip: RegisterControlPoints populates the registry owned by
// the same daemon, and Registry() exposes those registrations.
//
// This is the positive path for the daemon-local invariant: within a single
// daemon, registration and lookup are consistent.
func TestCP045_S02PolicyEngine_RegisteredControlPointsVisibleViaRegistry(t *testing.T) {
	t.Parallel()

	engine := NewS02PolicyEngine()
	cps := []ControlPoint{
		cp002FixtureGate(t, "gate-alpha"),
		cp002FixtureHook(t, "hook-beta"),
	}

	if err := engine.RegisterControlPoints(cps); err != nil {
		t.Fatalf("RegisterControlPoints: %v", err)
	}

	reg := engine.Registry()
	for _, cp := range cps {
		if _, ok := reg.LookupByName(cp.Name); !ok {
			t.Errorf("Registry().LookupByName(%q) not found after RegisterControlPoints", cp.Name)
		}
	}
	if len(reg.All()) != len(cps) {
		t.Errorf("Registry().All() len = %d, want %d", len(reg.All()), len(cps))
	}
}

// ---------------------------------------------------------------------------
// NoOpPolicyEngine daemon-local scope
// ---------------------------------------------------------------------------

// TestCP045_NoOpPolicyEngine_RegistryAlwaysEmpty verifies that
// NoOpPolicyEngine.Registry() satisfies CP-045 for the MVH binding: its
// registry carries no state and is therefore trivially daemon-local.
//
// NoOpPolicyEngine is the production binding at MVH; no ControlPoints are
// loaded, so its registry is always empty. CP-045 is satisfied because an
// empty registry cannot cross daemon boundaries.
func TestCP045_NoOpPolicyEngine_RegistryAlwaysEmpty(t *testing.T) {
	t.Parallel()

	e1 := NoOpPolicyEngine{}
	e2 := NoOpPolicyEngine{}

	if len(e1.Registry().All()) != 0 {
		t.Errorf("NoOpPolicyEngine e1 Registry().All() len = %d, want 0", len(e1.Registry().All()))
	}
	if len(e2.Registry().All()) != 0 {
		t.Errorf("NoOpPolicyEngine e2 Registry().All() len = %d, want 0", len(e2.Registry().All()))
	}

	// LookupByName on either must return not-found.
	if _, found := e1.Registry().LookupByName("any-name"); found {
		t.Error("NoOpPolicyEngine Registry().LookupByName returned found on an empty registry")
	}
}

// ---------------------------------------------------------------------------
// MapRegistry global-state absence (concurrent isolation)
// ---------------------------------------------------------------------------

// TestCP045_MapRegistry_ConcurrentInstancesDoNotInterfere verifies that
// independent MapRegistry instances created and mutated concurrently do not
// share any global state.
//
// This is the structural guarantee that underpins CP-045: the Go implementation
// of MapRegistry MUST NOT use package-level variables for ControlPoint storage.
// Concurrent goroutines registering into distinct instances must never see
// cross-instance pollution.
func TestCP045_MapRegistry_ConcurrentInstancesDoNotInterfere(t *testing.T) {
	t.Parallel()

	const goroutines = 8
	results := make([][]ControlPoint, goroutines)
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			reg := NewMapRegistry()
			// Each goroutine registers a unique gate name.
			name := "concurrent-gate-" + string(rune('a'+idx))
			cp := cp002FixtureGate(t, name)
			if err := reg.Register(cp); err != nil {
				t.Errorf("goroutine %d Register(%q): %v", idx, name, err)
				return
			}
			results[idx] = reg.All()
		}(i)
	}
	wg.Wait()

	// Each registry must have exactly one entry — its own registration.
	for i, all := range results {
		if all == nil {
			continue // registration failed, already reported
		}
		if len(all) != 1 {
			t.Errorf("goroutine %d registry has %d entries, want 1 (no cross-instance state)", i, len(all))
		}
		expectedName := "concurrent-gate-" + string(rune('a'+i))
		if len(all) == 1 && all[0].Name != expectedName {
			t.Errorf("goroutine %d registry[0].Name = %q, want %q (foreign entry leaked in)",
				i, all[0].Name, expectedName)
		}
	}
}
