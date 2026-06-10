package handlercontract_test

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// Tests for the Handler interface per specs/handler-contract.md §6.1 HC-001
// and HC-004 idempotency key derivation.
//
// Helper prefix: handlerFixture (bead hk-8i31.71; distinct from other
// handlercontract helper prefixes).
// HC-004 helper prefix: hc004Fixture (bead hk-7om2q.7).

// handlerFixtureStub is a minimal Handler implementation for interface-conformance testing.
// It uses LaunchKey to key sessions, matching the HC-004 conditional 4-tuple spec.
type handlerFixtureStub struct {
	agentType string
	mu        sync.Mutex
	sessions  map[string]handlercontract.Session // keyed by LaunchKey(spec)
}

// Launch implements Handler. Returns ErrStructural to keep the stub simple.
func (h *handlerFixtureStub) Launch(ctx context.Context, spec *handlercontract.LaunchSpec) (handlercontract.Session, error) {
	if spec == nil {
		return nil, handlercontract.ErrStructural
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	key := handlercontract.LaunchKey(spec)
	if sess, ok := h.sessions[key]; ok {
		return sess, nil // idempotent on LaunchKey(spec)
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
// HC-001 + HC-004: Idempotency on (run_id, node_id) and 4-tuple key
// ─────────────────────────────────────────────────────────────────────────────

// sessionFixtureNop is a no-op Session for testing idempotency.
type sessionFixtureNop struct{}

func (sessionFixtureNop) ID() core.SessionID                           { return "sess-001" }
func (sessionFixtureNop) SendInput(_ context.Context, _ string) error  { return nil }
func (sessionFixtureNop) Attach(_ context.Context) (io.Reader, error)  { return nil, nil }
func (sessionFixtureNop) Kill(_ context.Context) error                 { return nil }
func (sessionFixtureNop) Wait(_ context.Context) (core.Outcome, error) { return core.Outcome{}, nil }
func (sessionFixtureNop) LogLocation() string                          { return "" }

// hc004FixtureMakeReviewLoopSpec returns a valid LaunchSpec for review-loop
// dispatch tests (phase + iteration_count present, ClaudeSessionID set for
// implementer-resume).
func hc004FixtureMakeReviewLoopSpec(t *testing.T, phase handlercontract.ReviewLoopPhase, iter int) *handlercontract.LaunchSpec {
	t.Helper()
	spec := handlerFixtureMakeSpec(t)
	mode := "review-loop"
	spec.WorkflowMode = &mode
	spec.Phase = &phase
	spec.IterationCount = &iter
	if phase == handlercontract.ReviewLoopPhaseImplementerResume {
		sid := "claude-session-test-abc"
		spec.ClaudeSessionID = &sid
	}
	return spec
}

// TestHandler_LaunchIdempotentOnRunIDNodeID verifies that a second Launch call
// with the same (run_id, node_id) 2-tuple returns the same Session without
// spawning a new subprocess (HC-004 idempotency — single-mode key shape).
func TestHandler_LaunchIdempotentOnRunIDNodeID(t *testing.T) {
	t.Parallel()

	spec := handlerFixtureMakeSpec(t)
	key := handlercontract.LaunchKey(spec) // 2-tuple key

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

// TestLaunchKey_TwoTupleWhenPhaseAbsent verifies that LaunchKey returns the
// 2-tuple (run_id, node_id) form when Phase and IterationCount are absent per
// HC-004 (single-mode or mode omitted).
func TestLaunchKey_TwoTupleWhenPhaseAbsent(t *testing.T) {
	t.Parallel()

	spec := handlerFixtureMakeSpec(t)
	// Phase and IterationCount are nil by default.
	key := handlercontract.LaunchKey(spec)

	wantPrefix := spec.RunID.String() + "/" + string(spec.NodeID)
	if key != wantPrefix {
		t.Errorf("HC-004: LaunchKey(single-mode spec) = %q; want 2-tuple %q", key, wantPrefix)
	}
}

// TestLaunchKey_FourTupleWhenPhasePresent verifies that LaunchKey returns the
// 4-tuple (run_id, node_id, phase, iteration_count) form when both Phase and
// IterationCount are present per HC-004 (multi-phase modes, e.g. review-loop).
func TestLaunchKey_FourTupleWhenPhasePresent(t *testing.T) {
	t.Parallel()

	spec := hc004FixtureMakeReviewLoopSpec(t, handlercontract.ReviewLoopPhaseImplementerInitial, 1)
	key := handlercontract.LaunchKey(spec)

	wantKey := spec.RunID.String() + "/" + string(spec.NodeID) + "/implementer-initial/1"
	if key != wantKey {
		t.Errorf("HC-004: LaunchKey(review-loop spec) = %q; want 4-tuple %q", key, wantKey)
	}
}

// TestLaunchKey_SameKeyReturnsSameSession verifies that two Launch calls with
// a review-loop spec sharing the same 4-tuple key return the same Session
// (concurrent-launch idempotency per HC-004).
func TestLaunchKey_SameKeyReturnsSameSession(t *testing.T) {
	t.Parallel()

	spec := hc004FixtureMakeReviewLoopSpec(t, handlercontract.ReviewLoopPhaseImplementerInitial, 1)
	key := handlercontract.LaunchKey(spec)

	existingSess := sessionFixtureNop{}
	h := &handlerFixtureStub{
		agentType: "claude-code",
		sessions:  map[string]handlercontract.Session{key: existingSess},
	}

	sess1, err1 := h.Launch(context.Background(), spec)
	if err1 != nil {
		t.Fatalf("HC-004: first Launch (review-loop): %v", err1)
	}
	sess2, err2 := h.Launch(context.Background(), spec)
	if err2 != nil {
		t.Fatalf("HC-004: second Launch (review-loop idempotent): %v", err2)
	}
	if sess1 != sess2 {
		t.Error("HC-004: second review-loop Launch returned different Session; want same")
	}
}

// TestLaunchKey_DistinctPhaseIterationProduceDistinctKeys verifies that
// distinct (phase, iteration_count) tuples produce distinct keys per HC-004.
// Within a review-loop cycle, the daemon may legitimately launch implementer
// and reviewer phases without idempotency returning a prior session.
func TestLaunchKey_DistinctPhaseIterationProduceDistinctKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase handlercontract.ReviewLoopPhase
		iter  int
	}{
		{handlercontract.ReviewLoopPhaseImplementerInitial, 1},
		{handlercontract.ReviewLoopPhaseReviewer, 1},
		{handlercontract.ReviewLoopPhaseImplementerResume, 2},
		{handlercontract.ReviewLoopPhaseReviewer, 2},
	}

	keys := make(map[string]struct{}, len(cases))
	for _, tc := range cases {
		spec := hc004FixtureMakeReviewLoopSpec(t, tc.phase, tc.iter)
		k := handlercontract.LaunchKey(spec)
		if _, dup := keys[k]; dup {
			t.Errorf("HC-004: duplicate LaunchKey %q for phase=%s iter=%d", k, tc.phase, tc.iter)
		}
		keys[k] = struct{}{}
	}
}

// TestLaunchKey_ConcurrentLaunchSameKey verifies that concurrent Launch calls
// sharing the same 4-tuple key both return the same Session (stub models
// the idempotency behaviour; concurrent goroutines confirm no data race).
func TestLaunchKey_ConcurrentLaunchSameKey(t *testing.T) {
	t.Parallel()

	spec := hc004FixtureMakeReviewLoopSpec(t, handlercontract.ReviewLoopPhaseReviewer, 1)
	key := handlercontract.LaunchKey(spec)

	existingSess := sessionFixtureNop{}
	h := &handlerFixtureStub{
		agentType: "claude-code",
		sessions:  map[string]handlercontract.Session{key: existingSess},
	}

	const goroutines = 8
	results := make([]handlercontract.Session, goroutines)
	errs := make([]error, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = h.Launch(context.Background(), spec)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("HC-004: goroutine %d Launch error: %v", i, err)
		}
	}
	for i, sess := range results {
		if sess != existingSess {
			t.Errorf("HC-004: goroutine %d returned different Session; want idempotent", i)
		}
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
