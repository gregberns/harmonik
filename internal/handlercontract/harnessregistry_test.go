package handlercontract_test

import (
	"io"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

type harnessRegistryFixtureHarness struct {
	agentType core.AgentType
}

func (h harnessRegistryFixtureHarness) AgentType() core.AgentType { return h.agentType }
func (harnessRegistryFixtureHarness) LaunchSpec(_ handlercontract.RunCtx) (handlercontract.SpawnSpec, error) {
	return handlercontract.SpawnSpec{}, nil
}

func (harnessRegistryFixtureHarness) Seed(_ handlercontract.Session, _ handlercontract.RunCtx) error {
	return nil
}

func (harnessRegistryFixtureHarness) Retask(_ handlercontract.Session, _ string, _ handlercontract.RunCtx) error {
	return nil
}
func (harnessRegistryFixtureHarness) Teardown(_ handlercontract.Session) error { return nil }
func (harnessRegistryFixtureHarness) DetectReady(_ handlercontract.EventEnvelope) bool {
	return false
}

func (harnessRegistryFixtureHarness) SessionIDPolicy() handlercontract.SessionIDPolicy {
	return handlercontract.SessionIDMinted
}

func (harnessRegistryFixtureHarness) Completion() handlercontract.CompletionMode {
	return handlercontract.CompletionEventStreamThenQuit
}

func (harnessRegistryFixtureHarness) NewSessionIDInterceptor(inner io.Reader, _ func(string), _ func()) io.Reader {
	return inner
}

var _ handlercontract.Harness = harnessRegistryFixtureHarness{}

func harnessRegistryFixtureNewHarness(at core.AgentType) handlercontract.Harness {
	return harnessRegistryFixtureHarness{agentType: at}
}

func harnessRegistryFixtureValidType(suffix string) core.AgentType {
	return core.AgentType("test-harness-" + suffix)
}

// TestHarnessRegistry_RegisterThenForAgent verifies a registered harness is
// returned by ForAgent for the same agent_type.
func TestHarnessRegistry_RegisterThenForAgent(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewHarnessRegistry()
	at := harnessRegistryFixtureValidType("a")
	want := harnessRegistryFixtureNewHarness(at)
	if err := reg.Register(at, want); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	got, err := reg.ForAgent(at)
	if err != nil {
		t.Fatalf("ForAgent: unexpected error: %v", err)
	}
	if got.AgentType() != at {
		t.Errorf("ForAgent(%q).AgentType() = %q; want %q", at, got.AgentType(), at)
	}
}

// TestHarnessRegistry_ForAgentUnregistered verifies ForAgent on an unregistered
// agent_type returns a non-nil error and a nil harness (well-defined: errors, does
// not silently succeed).
func TestHarnessRegistry_ForAgentUnregistered(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewHarnessRegistry()
	if err := reg.Register(harnessRegistryFixtureValidType("present"),
		harnessRegistryFixtureNewHarness(harnessRegistryFixtureValidType("present"))); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}

	got, err := reg.ForAgent(harnessRegistryFixtureValidType("absent"))
	if err == nil {
		t.Fatal("ForAgent(unregistered): expected error, got nil")
	}
	if got != nil {
		t.Errorf("ForAgent(unregistered): expected nil harness, got %v", got)
	}
}

// TestHarnessRegistry_DuplicateRegistration verifies a second Register for the same
// agent_type returns an error (HC-012-style exactly-one invariant).
func TestHarnessRegistry_DuplicateRegistration(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewHarnessRegistry()
	at := harnessRegistryFixtureValidType("dup")
	if err := reg.Register(at, harnessRegistryFixtureNewHarness(at)); err != nil {
		t.Fatalf("first Register: unexpected error: %v", err)
	}
	if err := reg.Register(at, harnessRegistryFixtureNewHarness(at)); err == nil {
		t.Error("second Register for same agent_type: expected duplicate error, got nil")
	}
}

// TestHarnessRegistry_InvalidAgentType verifies Register rejects an agent_type that
// does not satisfy the AR-025 regex.
func TestHarnessRegistry_InvalidAgentType(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewHarnessRegistry()
	bad := core.AgentType("A") // uppercase + too short → invalid
	if err := reg.Register(bad, harnessRegistryFixtureNewHarness(bad)); err == nil {
		t.Errorf("Register(%q): expected invalid-agent_type error, got nil", bad)
	}
}

// TestHarnessRegistry_NilHarness verifies Register rejects a nil harness.
func TestHarnessRegistry_NilHarness(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewHarnessRegistry()
	at := harnessRegistryFixtureValidType("nil")
	if err := reg.Register(at, nil); err == nil {
		t.Error("Register(nil harness): expected error, got nil")
	}
}

// TestHarnessRegistry_SealedAfterForAgent verifies that the first ForAgent call
// seals the registry: subsequent Register calls fail, and Sealed() reports true.
func TestHarnessRegistry_SealedAfterForAgent(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewHarnessRegistry()
	at := harnessRegistryFixtureValidType("seal")
	if err := reg.Register(at, harnessRegistryFixtureNewHarness(at)); err != nil {
		t.Fatalf("Register: unexpected error: %v", err)
	}
	if reg.Sealed() {
		t.Fatal("registry sealed before any ForAgent call")
	}

	if _, err := reg.ForAgent(at); err != nil {
		t.Fatalf("ForAgent: unexpected error: %v", err)
	}
	if !reg.Sealed() {
		t.Error("registry not sealed after ForAgent")
	}

	at2 := harnessRegistryFixtureValidType("seal2")
	if err := reg.Register(at2, harnessRegistryFixtureNewHarness(at2)); err == nil {
		t.Error("Register after seal: expected sealed-registry error, got nil")
	}
}

// TestHarnessRegistry_RegisteredTypes verifies RegisteredTypes returns the set of
// registered agent types.
func TestHarnessRegistry_RegisteredTypes(t *testing.T) {
	t.Parallel()

	reg := handlercontract.NewHarnessRegistry()
	a := harnessRegistryFixtureValidType("rt-a")
	b := harnessRegistryFixtureValidType("rt-b")
	if err := reg.Register(a, harnessRegistryFixtureNewHarness(a)); err != nil {
		t.Fatalf("Register a: %v", err)
	}
	if err := reg.Register(b, harnessRegistryFixtureNewHarness(b)); err != nil {
		t.Fatalf("Register b: %v", err)
	}

	got := reg.RegisteredTypes()
	if len(got) != 2 {
		t.Fatalf("RegisteredTypes len = %d; want 2 (%v)", len(got), got)
	}
	seen := map[core.AgentType]bool{}
	for _, at := range got {
		seen[at] = true
	}
	if !seen[a] || !seen[b] {
		t.Errorf("RegisteredTypes = %v; want both %q and %q", got, a, b)
	}
}

// TestHarnessRegistry_ZeroValuePanics verifies a zero-value registry panics on
// method calls (callers MUST use NewHarnessRegistry).
func TestHarnessRegistry_ZeroValuePanics(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Error("zero-value HarnessRegistry.ForAgent: expected panic, got none")
		}
	}()
	var reg handlercontract.HarnessRegistry
	if _, err := reg.ForAgent(harnessRegistryFixtureValidType("zv")); err != nil {
		t.Errorf("zero-value HarnessRegistry.ForAgent returned error %v; expected panic", err)
	}
}
