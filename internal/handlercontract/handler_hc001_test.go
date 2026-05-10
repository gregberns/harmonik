package handlercontract_test

import (
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// Tests for the Handler interface per specs/handler-contract.md §6.1 HC-001.
//
// Helper prefix: handlerFixture (bead hk-8i31.71; distinct from other
// handlercontract helper prefixes).

// handlerFixtureStub is a minimal Handler implementation for interface-conformance testing.
type handlerFixtureStub struct {
	agentType string
	sessions  map[string]handlercontract.Session // keyed by "run_id/node_id"
}

// Launch implements Handler. Returns ErrStructural to keep the stub simple.
func (h *handlerFixtureStub) Launch(ctx context.Context, spec *handlercontract.LaunchSpec) (handlercontract.Session, error) {
	if spec == nil {
		return nil, handlercontract.ErrStructural
	}
	key := spec.RunID.String() + "/" + string(spec.NodeID)
	if sess, ok := h.sessions[key]; ok {
		return sess, nil // idempotent on (run_id, node_id)
	}
	return nil, handlercontract.ErrStructural
}

// AgentType implements Handler.
func (h *handlerFixtureStub) AgentType() string { return h.agentType }

// handlerFixtureMakeSpec returns a minimal valid LaunchSpec for stub tests.
func handlerFixtureMakeSpec(t *testing.T) *handlercontract.LaunchSpec {
	t.Helper()
	return &handlercontract.LaunchSpec{
		RunID:               core.RunID(uuid.MustParse("0196f100-0000-7000-8000-000000000001")),
		WorkflowID:          core.WorkflowID(uuid.MustParse("0196f100-0000-7000-8000-000000000002")),
		NodeID:              core.NodeID("impl-node-1"),
		AgentType:           core.AgentType("claude-code"),
		WorkspacePath:       "/tmp/test-workspace",
		RequiredSkills:      []string{},
		SkillSearchPaths:    []string{},
		Timeout:             3600,
		ProvisioningTimeout: 60,
		Budget:              core.BudgetRef("default"),
		FreedomProfileRef:   "standard",
		SchemaVersion:       handlercontract.LaunchSpecSchemaVersion,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-001: Interface conformance
// ─────────────────────────────────────────────────────────────────────────────

// TestHandler_InterfaceConformance verifies that a concrete type implementing
// both Handler methods satisfies the Handler interface at compile time.
// This is a compile-time check encoded as a runtime test.
func TestHandler_InterfaceConformance(t *testing.T) {
	t.Parallel()

	var _ handlercontract.Handler = &handlerFixtureStub{agentType: "claude-code"}
}

// TestHandler_AgentTypeIsNonEmpty verifies that AgentType() returns a non-empty
// string — the handler MUST identify itself.
func TestHandler_AgentTypeIsNonEmpty(t *testing.T) {
	t.Parallel()

	h := &handlerFixtureStub{agentType: "claude-code"}
	if h.AgentType() == "" {
		t.Error("HC-001: AgentType() returned empty string; want non-empty identifier")
	}
}

// TestHandler_AgentTypeLowercaseHyphenated verifies that the convention of
// lowercase-hyphenated agent_type identifiers is testable.
// The value "claude-code" is the canonical example.
func TestHandler_AgentTypeLowercaseHyphenated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		agentType string
		valid     bool
	}{
		{"claude-code", true},
		{"twin-claude-code", true},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.agentType, func(t *testing.T) {
			t.Parallel()
			h := &handlerFixtureStub{agentType: tc.agentType}
			got := h.AgentType()
			if tc.valid && got == "" {
				t.Errorf("HC-001: AgentType() = %q; want non-empty", got)
			}
			if !tc.valid && got != "" {
				t.Errorf("HC-001: AgentType() = %q; want empty for invalid fixture", got)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-001: Launch method shape
// ─────────────────────────────────────────────────────────────────────────────

// TestHandler_LaunchAcceptsCtxAndSpec verifies that Launch accepts a context
// and a *LaunchSpec (not LaunchSpec by value) — the pointer signature allows
// idempotency tracking by the caller.
func TestHandler_LaunchAcceptsCtxAndSpec(t *testing.T) {
	t.Parallel()

	h := &handlerFixtureStub{agentType: "claude-code", sessions: map[string]handlercontract.Session{}}
	spec := handlerFixtureMakeSpec(t)

	// The stub returns ErrStructural for unknown (run_id, node_id) pairs.
	_, err := h.Launch(context.Background(), spec)
	if err == nil {
		t.Error("HC-001: stub Launch with unknown spec returned nil error; want ErrStructural sentinel")
	}
}

// TestHandler_LaunchNilSpecReturnsError verifies that a nil spec causes
// Launch to return an error rather than panic.
func TestHandler_LaunchNilSpecReturnsError(t *testing.T) {
	t.Parallel()

	h := &handlerFixtureStub{agentType: "claude-code", sessions: map[string]handlercontract.Session{}}
	_, err := h.Launch(context.Background(), nil)
	if err == nil {
		t.Error("HC-001: Launch(nil spec) = nil; want error")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-001 + HC-004: Idempotency on (run_id, node_id)
// ─────────────────────────────────────────────────────────────────────────────

// sessionFixtureNop is a no-op Session for testing idempotency.
type sessionFixtureNop struct{}

func (sessionFixtureNop) ID() core.SessionID                           { return "sess-001" }
func (sessionFixtureNop) SendInput(_ context.Context, _ string) error  { return nil }
func (sessionFixtureNop) Attach(_ context.Context) (io.Reader, error)  { return nil, nil }
func (sessionFixtureNop) Kill(_ context.Context) error                 { return nil }
func (sessionFixtureNop) Wait(_ context.Context) (core.Outcome, error) { return core.Outcome{}, nil }
func (sessionFixtureNop) LogLocation() string                          { return "" }

// TestHandler_LaunchIdempotentOnRunIDNodeID verifies that a second Launch call
// with the same (run_id, node_id) returns the same Session without spawning a
// new subprocess (HC-004 idempotency).
func TestHandler_LaunchIdempotentOnRunIDNodeID(t *testing.T) {
	t.Parallel()

	spec := handlerFixtureMakeSpec(t)
	key := spec.RunID.String() + "/" + string(spec.NodeID)

	// Pre-register the session so the stub's idempotency check fires.
	existingSess := sessionFixtureNop{}
	h := &handlerFixtureStub{
		agentType: "claude-code",
		sessions:  map[string]handlercontract.Session{key: existingSess},
	}

	sess1, err1 := h.Launch(context.Background(), spec)
	if err1 != nil {
		t.Fatalf("HC-004: first Launch: %v", err1)
	}

	sess2, err2 := h.Launch(context.Background(), spec)
	if err2 != nil {
		t.Fatalf("HC-004: second Launch (idempotent): %v", err2)
	}

	// Both calls MUST return the same Session object.
	if sess1 != sess2 {
		t.Error("HC-004: second Launch returned a different Session; want idempotent return of existing Session")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Error class discipline
// ─────────────────────────────────────────────────────────────────────────────

// TestHandler_LaunchFailureUsesTypedSentinels verifies that Launch failure
// paths return one of the declared sentinel error classes, not a raw error.
// The stub's ErrStructural return models a binary-not-found failure.
func TestHandler_LaunchFailureUsesTypedSentinels(t *testing.T) {
	t.Parallel()

	h := &handlerFixtureStub{agentType: "claude-code", sessions: map[string]handlercontract.Session{}}
	spec := handlerFixtureMakeSpec(t)

	_, err := h.Launch(context.Background(), spec)
	if err == nil {
		t.Fatal("HC-001: Launch returned nil error for unknown session; want sentinel")
	}

	// Must be one of the declared sentinel classes.
	class := handlercontract.Class(err)
	validClasses := map[string]bool{
		"transient":     true,
		"structural":    true,
		"deterministic": true,
		"canceled":      true,
		"budget":        true,
	}
	if !validClasses[class] {
		t.Errorf("HC-001: Launch error class = %q; want one of transient/structural/deterministic/canceled/budget", class)
	}
}
