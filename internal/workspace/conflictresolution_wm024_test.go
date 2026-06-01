package workspace

// conflictresolution_wm024_test.go — tests for WM-024 conflict-resolver
// dispatch mechanism (hk-8mwo.36).
//
// Covers:
//   - ValidateConflictResolutionAttemptCap: bounds [1, 10] per WM-024.
//   - EffectiveConflictResolutionAttemptCap: zero-value default resolution.
//   - ShouldDispatchConflictResolver: decision routing per WM-022a, WM-023, WM-024.
//   - BuildConflictResolverLaunchSpec: LaunchSpec construction and field rules.
//
// Spec ref: workspace-model.md §4.6 WM-022, WM-022a, WM-023, WM-024.
// Bead ref: hk-8mwo.36.

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ── ValidateConflictResolutionAttemptCap ─────────────────────────────────────

// TestValidateConflictResolutionAttemptCap_ValidRange verifies that all values
// in [1, 10] are accepted per WM-024.
func TestValidateConflictResolutionAttemptCap_ValidRange(t *testing.T) {
	t.Parallel()
	for cap := ConflictResolutionAttemptCapMin; cap <= ConflictResolutionAttemptCapMax; cap++ {
		if err := ValidateConflictResolutionAttemptCap(cap); err != nil {
			t.Errorf("WM-024: ValidateConflictResolutionAttemptCap(%d) = %v; want nil", cap, err)
		}
	}
}

// TestValidateConflictResolutionAttemptCap_OutOfRange verifies that values
// outside [1, 10] are rejected and wrap ErrConflictResolutionCapOutOfRange.
func TestValidateConflictResolutionAttemptCap_OutOfRange(t *testing.T) {
	t.Parallel()
	cases := []int{0, -1, 11, 100}
	for _, cap := range cases {
		err := ValidateConflictResolutionAttemptCap(cap)
		if err == nil {
			t.Errorf("WM-024: ValidateConflictResolutionAttemptCap(%d) = nil; want error", cap)
			continue
		}
		if !errors.Is(err, ErrConflictResolutionCapOutOfRange) {
			t.Errorf("WM-024: ValidateConflictResolutionAttemptCap(%d) error does not wrap ErrConflictResolutionCapOutOfRange: %v", cap, err)
		}
	}
}

// ── EffectiveConflictResolutionAttemptCap ────────────────────────────────────

// TestEffectiveConflictResolutionAttemptCap_ZeroUsesDefault verifies that a
// zero operatorCap returns DefaultConflictResolutionAttemptCap.
func TestEffectiveConflictResolutionAttemptCap_ZeroUsesDefault(t *testing.T) {
	t.Parallel()
	got := EffectiveConflictResolutionAttemptCap(0)
	if got != DefaultConflictResolutionAttemptCap {
		t.Errorf("EffectiveConflictResolutionAttemptCap(0) = %d; want %d", got, DefaultConflictResolutionAttemptCap)
	}
}

// TestEffectiveConflictResolutionAttemptCap_NonZeroPassthrough verifies that a
// non-zero operatorCap is returned as-is.
func TestEffectiveConflictResolutionAttemptCap_NonZeroPassthrough(t *testing.T) {
	t.Parallel()
	for _, op := range []int{1, 5, 10} {
		got := EffectiveConflictResolutionAttemptCap(op)
		if got != op {
			t.Errorf("EffectiveConflictResolutionAttemptCap(%d) = %d; want %d", op, got, op)
		}
	}
}

// ── ShouldDispatchConflictResolver ───────────────────────────────────────────

// TestShouldDispatch_NullRefEscalates verifies that a nil implementer_handler_ref
// always produces EscalateNullRef per WM-022a, regardless of attempt count or cap.
func TestShouldDispatch_NullRefEscalates(t *testing.T) {
	t.Parallel()
	decision := ShouldDispatchConflictResolver(nil, 0, DefaultConflictResolutionAttemptCap, nil)
	if decision != ConflictResolveEscalateNullRef {
		t.Errorf("WM-022a: ShouldDispatch(nil ref) = %q; want %q", decision, ConflictResolveEscalateNullRef)
	}
	// Even if we're under cap, null ref wins.
	decision = ShouldDispatchConflictResolver(nil, 1, 10, nil)
	if decision != ConflictResolveEscalateNullRef {
		t.Errorf("WM-022a: ShouldDispatch(nil ref, under-cap) = %q; want %q", decision, ConflictResolveEscalateNullRef)
	}
}

// TestShouldDispatch_RetiredHandlerEscalates verifies that a retired handler class
// produces EscalateRetiredHandler per WM-024 terminal clause.
func TestShouldDispatch_RetiredHandlerEscalates(t *testing.T) {
	t.Parallel()
	ref := core.HandlerRef("agentic-v1-retired")
	isRetired := func(h core.HandlerRef) bool { return h == "agentic-v1-retired" }

	decision := ShouldDispatchConflictResolver(&ref, 0, DefaultConflictResolutionAttemptCap, isRetired)
	if decision != ConflictResolveEscalateRetiredHandler {
		t.Errorf("WM-024: retired handler: ShouldDispatch = %q; want %q", decision, ConflictResolveEscalateRetiredHandler)
	}
}

// TestShouldDispatch_CapExhaustedEscalates verifies that attemptCount >= cap
// produces EscalateCapExhausted per WM-024 / WM-023.
func TestShouldDispatch_CapExhaustedEscalates(t *testing.T) {
	t.Parallel()
	ref := core.HandlerRef("agentic-claude")
	noRetire := func(_ core.HandlerRef) bool { return false }

	// Exactly at cap.
	decision := ShouldDispatchConflictResolver(&ref, 3, 3, noRetire)
	if decision != ConflictResolveEscalateCapExhausted {
		t.Errorf("WM-024: cap=3, attempts=3: ShouldDispatch = %q; want %q", decision, ConflictResolveEscalateCapExhausted)
	}

	// Over cap.
	decision = ShouldDispatchConflictResolver(&ref, 4, 3, noRetire)
	if decision != ConflictResolveEscalateCapExhausted {
		t.Errorf("WM-024: cap=3, attempts=4: ShouldDispatch = %q; want %q", decision, ConflictResolveEscalateCapExhausted)
	}
}

// TestShouldDispatch_UnderCapDispatches verifies that when ref is non-nil,
// handler is active, and attemptCount < cap, the decision is Dispatch.
func TestShouldDispatch_UnderCapDispatches(t *testing.T) {
	t.Parallel()
	ref := core.HandlerRef("agentic-claude")
	noRetire := func(_ core.HandlerRef) bool { return false }

	for attempts := 0; attempts < DefaultConflictResolutionAttemptCap; attempts++ {
		decision := ShouldDispatchConflictResolver(&ref, attempts, DefaultConflictResolutionAttemptCap, noRetire)
		if decision != ConflictResolveDispatch {
			t.Errorf("WM-024: attempts=%d < cap=%d: ShouldDispatch = %q; want %q",
				attempts, DefaultConflictResolutionAttemptCap, decision, ConflictResolveDispatch)
		}
	}
}

// TestShouldDispatch_NilIsRetiredDoesNotPanic verifies that a nil isRetired
// function is safe (no panic) and does not block dispatch.
func TestShouldDispatch_NilIsRetiredDoesNotPanic(t *testing.T) {
	t.Parallel()
	ref := core.HandlerRef("agentic-claude")
	decision := ShouldDispatchConflictResolver(&ref, 0, DefaultConflictResolutionAttemptCap, nil)
	if decision != ConflictResolveDispatch {
		t.Errorf("ShouldDispatch(nil isRetired) = %q; want %q", decision, ConflictResolveDispatch)
	}
}

// ── BuildConflictResolverLaunchSpec ──────────────────────────────────────────

// wm024FixtureRunID returns a test run ID derived from a deterministic UUID.
func wm024FixtureRunID() core.RunID {
	return core.RunID(uuid.MustParse("0196b200-0000-7000-8000-000000024000"))
}

// wm024FixtureWorkflowID returns a test workflow ID.
func wm024FixtureWorkflowID() core.WorkflowID {
	return core.WorkflowID(uuid.MustParse("0196b200-0000-7000-8000-000000024001"))
}

// wm024FixtureParams returns a well-formed ConflictResolverLaunchSpecParams
// for tests that exercise BuildConflictResolverLaunchSpec.
func wm024FixtureParams() ConflictResolverLaunchSpecParams {
	return ConflictResolverLaunchSpecParams{
		RunID:               wm024FixtureRunID(),
		WorkflowID:          wm024FixtureWorkflowID(),
		NodeID:              "conflict-resolve-node-1",
		AgentType:           "agentic-claude",
		WorkspacePath:       "/tmp/harmonik-test-repo/.harmonik/worktrees/0196b200-0000-7000-8000-000000024000",
		AdditionalSkills:    nil,
		SkillSearchPaths:    []string{},
		Timeout:             3600,
		ProvisioningTimeout: 60,
		Budget:              "handler-default",
		FreedomProfileRef:   "standard",
		BeadID:              nil,
		AttemptNumber:       1,
	}
}

// TestBuildConflictResolverLaunchSpec_WellFormed verifies that a well-formed
// ConflictResolverLaunchSpecParams produces a valid LaunchSpec.
func TestBuildConflictResolverLaunchSpec_WellFormed(t *testing.T) {
	t.Parallel()
	params := wm024FixtureParams()
	spec, err := BuildConflictResolverLaunchSpec(params)
	if err != nil {
		t.Fatalf("BuildConflictResolverLaunchSpec: unexpected error: %v", err)
	}
	if err := spec.Valid(); err != nil {
		t.Errorf("returned LaunchSpec.Valid() = %v; want nil", err)
	}
}

// TestBuildConflictResolverLaunchSpec_ConflictResolutionSkillFirst verifies that
// ConflictResolutionSkillName appears first in RequiredSkills per WM-024.
func TestBuildConflictResolverLaunchSpec_ConflictResolutionSkillFirst(t *testing.T) {
	t.Parallel()
	params := wm024FixtureParams()
	params.AdditionalSkills = []string{"beads-cli", "agent-reviewer"}
	spec, err := BuildConflictResolverLaunchSpec(params)
	if err != nil {
		t.Fatalf("BuildConflictResolverLaunchSpec: unexpected error: %v", err)
	}
	if len(spec.RequiredSkills) == 0 || spec.RequiredSkills[0] != ConflictResolutionSkillName {
		t.Errorf("RequiredSkills[0] = %q; want %q", spec.RequiredSkills[0], ConflictResolutionSkillName)
	}
	if len(spec.RequiredSkills) != 3 {
		t.Errorf("len(RequiredSkills) = %d; want 3", len(spec.RequiredSkills))
	}
}

// TestBuildConflictResolverLaunchSpec_WorkspacePath verifies that the returned
// spec carries the caller-supplied workspace_path (existing worktree per WM-024).
func TestBuildConflictResolverLaunchSpec_WorkspacePath(t *testing.T) {
	t.Parallel()
	params := wm024FixtureParams()
	spec, err := BuildConflictResolverLaunchSpec(params)
	if err != nil {
		t.Fatalf("BuildConflictResolverLaunchSpec: unexpected error: %v", err)
	}
	if spec.WorkspacePath != params.WorkspacePath {
		t.Errorf("spec.WorkspacePath = %q; want %q", spec.WorkspacePath, params.WorkspacePath)
	}
}

// TestBuildConflictResolverLaunchSpec_NoPhaseModeIterationCount verifies that
// Phase, WorkflowMode, and IterationCount are all nil (conflict-resolution is
// not a review-loop phase; HC-006 co-presence rule satisfied).
func TestBuildConflictResolverLaunchSpec_NoPhaseModeIterationCount(t *testing.T) {
	t.Parallel()
	params := wm024FixtureParams()
	spec, err := BuildConflictResolverLaunchSpec(params)
	if err != nil {
		t.Fatalf("BuildConflictResolverLaunchSpec: unexpected error: %v", err)
	}
	if spec.Phase != nil {
		t.Errorf("spec.Phase = %v; want nil (conflict-resolution is not a review-loop phase)", spec.Phase)
	}
	if spec.WorkflowMode != nil {
		t.Errorf("spec.WorkflowMode = %v; want nil", spec.WorkflowMode)
	}
	if spec.IterationCount != nil {
		t.Errorf("spec.IterationCount = %v; want nil (HC-006 co-presence rule)", spec.IterationCount)
	}
}

// TestBuildConflictResolverLaunchSpec_NoClaudeSessionID verifies that
// ClaudeSessionID is nil (each dispatch is a fresh session per WM-024 budget rule).
func TestBuildConflictResolverLaunchSpec_NoClaudeSessionID(t *testing.T) {
	t.Parallel()
	params := wm024FixtureParams()
	spec, err := BuildConflictResolverLaunchSpec(params)
	if err != nil {
		t.Fatalf("BuildConflictResolverLaunchSpec: unexpected error: %v", err)
	}
	if spec.ClaudeSessionID != nil {
		t.Errorf("spec.ClaudeSessionID = %v; want nil (fresh session per WM-024)", spec.ClaudeSessionID)
	}
}

// TestBuildConflictResolverLaunchSpec_MissingRequired verifies that missing
// required fields produce errors.
func TestBuildConflictResolverLaunchSpec_MissingRequired(t *testing.T) {
	t.Parallel()

	t.Run("zero-RunID", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.RunID = core.RunID{}
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for zero RunID")
		}
	})

	t.Run("zero-WorkflowID", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.WorkflowID = core.WorkflowID{}
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for zero WorkflowID")
		}
	})

	t.Run("empty-NodeID", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.NodeID = ""
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for empty NodeID")
		}
	})

	t.Run("empty-AgentType", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.AgentType = ""
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for empty AgentType")
		}
	})

	t.Run("empty-WorkspacePath", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.WorkspacePath = ""
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for empty WorkspacePath")
		}
	})

	t.Run("zero-Timeout", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.Timeout = 0
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for zero Timeout")
		}
	})

	t.Run("zero-ProvisioningTimeout", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.ProvisioningTimeout = 0
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for zero ProvisioningTimeout")
		}
	})

	t.Run("empty-Budget", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.Budget = ""
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for empty Budget")
		}
	})

	t.Run("empty-FreedomProfileRef", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.FreedomProfileRef = ""
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for empty FreedomProfileRef")
		}
	})

	t.Run("zero-AttemptNumber", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.AttemptNumber = 0
		if _, err := BuildConflictResolverLaunchSpec(p); err == nil {
			t.Error("want error for zero AttemptNumber")
		}
	})
}

// TestBuildConflictResolverLaunchSpec_SchemaVersion verifies the schema version
// is set to the current LaunchSpecSchemaVersion.
func TestBuildConflictResolverLaunchSpec_SchemaVersion(t *testing.T) {
	t.Parallel()
	params := wm024FixtureParams()
	spec, err := BuildConflictResolverLaunchSpec(params)
	if err != nil {
		t.Fatalf("BuildConflictResolverLaunchSpec: unexpected error: %v", err)
	}
	if spec.SchemaVersion != handlercontract.LaunchSpecSchemaVersion {
		t.Errorf("spec.SchemaVersion = %d; want %d", spec.SchemaVersion, handlercontract.LaunchSpecSchemaVersion)
	}
}

// TestBuildConflictResolverLaunchSpec_BeadIDOptional verifies that BeadID is
// threaded through when provided and omitted when nil.
func TestBuildConflictResolverLaunchSpec_BeadIDOptional(t *testing.T) {
	t.Parallel()

	t.Run("nil-BeadID", func(t *testing.T) {
		t.Parallel()
		p := wm024FixtureParams()
		p.BeadID = nil
		spec, err := BuildConflictResolverLaunchSpec(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec.BeadID != nil {
			t.Errorf("spec.BeadID = %v; want nil", spec.BeadID)
		}
	})

	t.Run("non-nil-BeadID", func(t *testing.T) {
		t.Parallel()
		id := "hk-test-bead"
		p := wm024FixtureParams()
		p.BeadID = &id
		spec, err := BuildConflictResolverLaunchSpec(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if spec.BeadID == nil || *spec.BeadID != id {
			t.Errorf("spec.BeadID = %v; want %q", spec.BeadID, id)
		}
	})
}
