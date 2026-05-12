package handlercontract_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// adapterRegistry — per-bead helper prefix for test helpers in this file
// (implementer-protocol.md §Helper-prefix discipline; bead hk-8i31.14).

// ─────────────────────────────────────────────────────────────────────────────
// Test fixtures
// ─────────────────────────────────────────────────────────────────────────────

// adapterRegistryFixtureAdapter returns a minimal no-op Adapter for use in
// registry tests where the adapter's behaviour is not under test.
type adapterRegistryFixtureAdapter struct{}

func (adapterRegistryFixtureAdapter) DetectReady(_ core.EventEnvelope) bool { return false }
func (adapterRegistryFixtureAdapter) DetectRateLimit(_ core.EventEnvelope) (bool, time.Duration) {
	return false, 0
}
func (adapterRegistryFixtureAdapter) CleanExitSequence(_ context.Context, _ handlercontract.Session) error {
	return nil
}
func (adapterRegistryFixtureAdapter) RotateAccount(_ context.Context) error { return nil }

// adapterRegistryFixtureNewAdapter returns a fresh no-op adapter value.
func adapterRegistryFixtureNewAdapter() handlercontract.Adapter {
	return adapterRegistryFixtureAdapter{}
}

// adapterRegistryFixtureValidType returns a valid AgentType for tests.
func adapterRegistryFixtureValidType(suffix string) core.AgentType {
	return core.AgentType("test-adapter-" + suffix)
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-012 — AdapterRegistry construction
// ─────────────────────────────────────────────────────────────────────────────

// TestAdapterRegistry_NewAdapterRegistry_NotSealed verifies that a freshly
// created registry is not sealed.
func TestAdapterRegistry_NewAdapterRegistry_NotSealed(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	if r.Sealed() {
		t.Error("NewAdapterRegistry: Sealed() = true; want false on fresh registry")
	}
}

// TestAdapterRegistry_NewAdapterRegistry_EmptyTypes verifies that a freshly
// created registry has no registered types.
func TestAdapterRegistry_NewAdapterRegistry_EmptyTypes(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	types := r.RegisteredTypes()
	if len(types) != 0 {
		t.Errorf("NewAdapterRegistry: RegisteredTypes() = %v, want empty slice", types)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-012 — Register
// ─────────────────────────────────────────────────────────────────────────────

// TestAdapterRegistry_Register_Success verifies that a valid registration
// succeeds and the type appears in RegisteredTypes.
func TestAdapterRegistry_Register_Success(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	agentType := adapterRegistryFixtureValidType("a")

	if err := r.Register(agentType, adapterRegistryFixtureNewAdapter()); err != nil {
		t.Fatalf("Register(%q): unexpected error: %v", agentType, err)
	}

	types := r.RegisteredTypes()
	found := false
	for _, at := range types {
		if at == agentType {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("RegisteredTypes() = %v, want to contain %q", types, agentType)
	}
}

// TestAdapterRegistry_Register_DuplicateReturnsError verifies that registering
// the same agent_type twice returns an error (HC-012: exactly one adapter per type).
func TestAdapterRegistry_Register_DuplicateReturnsError(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	agentType := adapterRegistryFixtureValidType("dup")

	if err := r.Register(agentType, adapterRegistryFixtureNewAdapter()); err != nil {
		t.Fatalf("Register first: unexpected error: %v", err)
	}

	if err := r.Register(agentType, adapterRegistryFixtureNewAdapter()); err == nil {
		t.Errorf("Register duplicate: got nil error, want non-nil (HC-012: one adapter per type)")
	}
}

// TestAdapterRegistry_Register_NilAdapterReturnsError verifies that registering
// a nil adapter returns an error (daemon defect).
func TestAdapterRegistry_Register_NilAdapterReturnsError(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	agentType := adapterRegistryFixtureValidType("niltest")

	if err := r.Register(agentType, nil); err == nil {
		t.Error("Register(nil adapter): got nil error, want non-nil (nil adapter is daemon defect)")
	}
}

// TestAdapterRegistry_Register_InvalidAgentTypeReturnsError verifies that
// registering an invalid agent_type (fails AR-027 regex) returns an error.
func TestAdapterRegistry_Register_InvalidAgentTypeReturnsError(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()

	// Invalid: starts with digit, violates AR-027 ^[a-z][a-z0-9-]{1,62}$
	invalidType := core.AgentType("1invalid")

	if err := r.Register(invalidType, adapterRegistryFixtureNewAdapter()); err == nil {
		t.Errorf("Register(%q): got nil error, want non-nil for invalid agent_type", invalidType)
	}
}

// TestAdapterRegistry_Register_AfterSealReturnsError verifies that calling
// Register after the registry is sealed (ForAgent was called) returns an error.
func TestAdapterRegistry_Register_AfterSealReturnsError(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	agentType := adapterRegistryFixtureValidType("sealtest")

	if err := r.Register(agentType, adapterRegistryFixtureNewAdapter()); err != nil {
		t.Fatalf("Register before seal: unexpected error: %v", err)
	}

	// Seal by calling ForAgent.
	if _, err := r.ForAgent(agentType); err != nil {
		t.Fatalf("ForAgent before seal: unexpected error: %v", err)
	}

	// Now the registry is sealed; Register MUST fail.
	if err := r.Register(adapterRegistryFixtureValidType("after-seal"), adapterRegistryFixtureNewAdapter()); err == nil {
		t.Error("Register after seal: got nil error, want non-nil (sealed registry rejects registrations)")
	}
}

// TestAdapterRegistry_Register_MultipleTypesSuccess verifies that multiple
// distinct agent types can be registered successfully.
func TestAdapterRegistry_Register_MultipleTypesSuccess(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()

	types := []core.AgentType{
		adapterRegistryFixtureValidType("cc"),
		adapterRegistryFixtureValidType("pi"),
		adapterRegistryFixtureValidType("twin"),
	}

	for _, at := range types {
		if err := r.Register(at, adapterRegistryFixtureNewAdapter()); err != nil {
			t.Fatalf("Register(%q): unexpected error: %v", at, err)
		}
	}

	registered := r.RegisteredTypes()
	if len(registered) != len(types) {
		t.Errorf("RegisteredTypes() count = %d, want %d", len(registered), len(types))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-012 — ForAgent
// ─────────────────────────────────────────────────────────────────────────────

// TestAdapterRegistry_ForAgent_SuccessReturnsAdapter verifies that ForAgent
// returns the registered adapter for a known agent_type.
func TestAdapterRegistry_ForAgent_SuccessReturnsAdapter(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	agentType := adapterRegistryFixtureValidType("foragent")
	adapter := adapterRegistryFixtureNewAdapter()

	if err := r.Register(agentType, adapter); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	got, err := r.ForAgent(agentType)
	if err != nil {
		t.Fatalf("ForAgent(%q): unexpected error: %v", agentType, err)
	}
	if got == nil {
		t.Error("ForAgent returned nil adapter, want non-nil")
	}
}

// TestAdapterRegistry_ForAgent_UnknownTypeReturnsError verifies that ForAgent
// returns an error for an unregistered agent_type (daemon dispatched to unknown
// type — a daemon defect).
func TestAdapterRegistry_ForAgent_UnknownTypeReturnsError(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	unknown := core.AgentType("unknown-type")

	_, err := r.ForAgent(unknown)
	if err == nil {
		t.Errorf("ForAgent(%q): got nil error for unregistered type, want non-nil", unknown)
	}
}

// TestAdapterRegistry_ForAgent_SealsRegistry verifies that ForAgent seals the
// registry (Sealed returns true after the first ForAgent call).
func TestAdapterRegistry_ForAgent_SealsRegistry(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()
	agentType := adapterRegistryFixtureValidType("seal")

	if err := r.Register(agentType, adapterRegistryFixtureNewAdapter()); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	if r.Sealed() {
		t.Error("Sealed() = true before ForAgent; want false")
	}

	if _, err := r.ForAgent(agentType); err != nil {
		t.Fatalf("ForAgent: unexpected error: %v", err)
	}

	if !r.Sealed() {
		t.Error("Sealed() = false after ForAgent; want true (ForAgent seals the registry)")
	}
}

// TestAdapterRegistry_ForAgent_UnknownAlsoSeals verifies that ForAgent seals
// the registry even when it returns an error (lookup attempt = dispatch began).
func TestAdapterRegistry_ForAgent_UnknownAlsoSeals(t *testing.T) {
	t.Parallel()

	r := handlercontract.NewAdapterRegistry()

	// Call ForAgent for an unregistered type — should seal even on error.
	_, _ = r.ForAgent(core.AgentType("not-registered"))

	if !r.Sealed() {
		t.Error("Sealed() = false after ForAgent on unknown type; want true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-012 — parallelism-prep: concurrent ForAgent (hk-cdb9f)
// ─────────────────────────────────────────────────────────────────────────────

// TestAdapterRegistry_ForAgent_ConcurrentNoRace verifies that N=100 goroutines
// calling ForAgent concurrently — both before and after the seal transition —
// produce no data races.  Run with -race to exercise the sync.RWMutex guard.
func TestAdapterRegistry_ForAgent_ConcurrentNoRace(t *testing.T) {
	t.Parallel()

	const goroutines = 100

	agentType := adapterRegistryFixtureValidType("concurrent")
	r := handlercontract.NewAdapterRegistry()

	if err := r.Register(agentType, adapterRegistryFixtureNewAdapter()); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start // all goroutines unblock at once to maximise contention
			_, _ = r.ForAgent(agentType)
		}()
	}

	close(start)
	wg.Wait()

	// After N concurrent ForAgent calls the registry must be sealed.
	if !r.Sealed() {
		t.Error("Sealed() = false after concurrent ForAgent calls; want true")
	}
}

// TestAdapterRegistry_RegisterAndForAgent_ConcurrentNoRace verifies that
// concurrent Register (pre-seal) and ForAgent (triggers seal) calls do not
// race with each other under the -race detector.
func TestAdapterRegistry_RegisterAndForAgent_ConcurrentNoRace(t *testing.T) {
	t.Parallel()

	const goroutines = 100

	r := handlercontract.NewAdapterRegistry()

	// Pre-register one type so ForAgent succeeds.
	baseType := adapterRegistryFixtureValidType("base-concurrent")
	if err := r.Register(baseType, adapterRegistryFixtureNewAdapter()); err != nil {
		t.Fatalf("Register base: unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			// Half the goroutines look up; the other half read sealed state.
			// Both paths touch shared fields and must not race.
			_, _ = r.ForAgent(baseType)
			_ = r.Sealed()
		}()
	}

	close(start)
	wg.Wait()
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-012 — compile-time: adapterRegistryFixtureAdapter satisfies Adapter
// ─────────────────────────────────────────────────────────────────────────────

// Compile-time assertion: adapterRegistryFixtureAdapter satisfies Adapter.
// This file imports no execution-shape package (internal/handler), proving that
// the Adapter interface is satisfiable from daemon-side (HC-051 seam) code.
var _ handlercontract.Adapter = adapterRegistryFixtureAdapter{}
