package workspace

// conflictresolution_wm022a_test.go — tests for WM-022a all-mechanical branch
// escalation guard (hk-8mwo.34).
//
// Covers:
//   - IsAllMechanicalBranch: predicate on workspace ImplementerHandlerRef.
//   - IsAllMechanicalRef: predicate on raw HandlerRef pointer.
//   - Integration: null ref → ShouldDispatchConflictResolver returns EscalateNullRef
//     (WM-024 is skipped; no BuildConflictResolverLaunchSpec call proceeds).
//   - Integration: null ref → BuildConflictEscalationPayload proceeds from
//     conflict-resolving → discarded (the WM-022a path end-to-end).
//   - No-silent-remap: verifies the system MUST NOT remap the implementer role when
//     implementer_handler_ref is null.
//
// Spec ref: workspace-model.md §4.6 WM-022a — "If implementer_handler_ref is null
//   at merge-pending entry … the workspace manager MUST skip WM-024 re-dispatch and
//   emit merge_conflict_escalation directly per WM-023 on conflict detection. The
//   system MUST NOT silently remap the implementer role to an unrelated handler class."
//
// Bead ref: hk-8mwo.34.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// wm022aFixtureRunID returns a deterministic test RunID for WM-022a tests.
func wm022aFixtureRunID() core.RunID {
	return core.RunID(uuid.MustParse("0196b200-0000-7000-8000-00000022a000"))
}

// wm022aFixtureWorkspace returns a *Workspace in the conflict-resolving state with
// a nil ImplementerHandlerRef (all-mechanical task branch per WM-022a).
func wm022aFixtureWorkspace() *Workspace {
	runID := wm022aFixtureRunID()
	return &Workspace{
		WorkspaceID:           WorkspaceIDFromRunID(runID.String()),
		RunID:                 runID,
		Repository:            "/tmp/harmonik-test-repo",
		ParentCommit:          "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
		BranchName:            "run/" + runID.String(),
		Path:                  "/tmp/harmonik-test-repo/.harmonik/worktrees/" + runID.String(),
		State:                 core.WorkspaceStateConflictResolving,
		InterruptState:        core.InterruptStateNone,
		ImplementerHandlerRef: nil, // null per WM-022a (all-mechanical)
		Metadata: map[string]string{
			"created_at":           "2026-05-07T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: WorkspaceSchemaVersion,
	}
}

// ── IsAllMechanicalBranch ────────────────────────────────────────────────────

// TestIsAllMechanicalBranch_NilRefIsAllMechanical verifies that a workspace with
// a nil ImplementerHandlerRef reports as all-mechanical per WM-022a.
func TestIsAllMechanicalBranch_NilRefIsAllMechanical(t *testing.T) {
	t.Parallel()

	ws := wm022aFixtureWorkspace() // ImplementerHandlerRef = nil
	if !IsAllMechanicalBranch(ws) {
		t.Errorf("WM-022a: IsAllMechanicalBranch(nil ref) = false; want true")
	}
}

// TestIsAllMechanicalBranch_NonNilRefIsNotAllMechanical verifies that a workspace
// with a non-nil ImplementerHandlerRef is NOT all-mechanical per WM-022a.
func TestIsAllMechanicalBranch_NonNilRefIsNotAllMechanical(t *testing.T) {
	t.Parallel()

	ws := wm022aFixtureWorkspace()
	ref := core.HandlerRef("agentic-claude")
	ws.ImplementerHandlerRef = &ref

	if IsAllMechanicalBranch(ws) {
		t.Errorf("WM-022a: IsAllMechanicalBranch(non-nil ref) = true; want false")
	}
}

// TestIsAllMechanicalBranch_NilWorkspaceSafe verifies that IsAllMechanicalBranch
// does not panic when called with a nil workspace, and returns false.
func TestIsAllMechanicalBranch_NilWorkspaceSafe(t *testing.T) {
	t.Parallel()

	got := IsAllMechanicalBranch(nil)
	if got {
		t.Errorf("WM-022a: IsAllMechanicalBranch(nil ws) = true; want false (nil is not all-mechanical)")
	}
}

// ── IsAllMechanicalRef ───────────────────────────────────────────────────────

// TestIsAllMechanicalRef_NilIsAllMechanical verifies that a nil HandlerRef pointer
// signals an all-mechanical branch per WM-022a.
func TestIsAllMechanicalRef_NilIsAllMechanical(t *testing.T) {
	t.Parallel()

	if !IsAllMechanicalRef(nil) {
		t.Errorf("WM-022a: IsAllMechanicalRef(nil) = false; want true")
	}
}

// TestIsAllMechanicalRef_NonNilIsNotAllMechanical verifies that a non-nil HandlerRef
// pointer does not signal an all-mechanical branch.
func TestIsAllMechanicalRef_NonNilIsNotAllMechanical(t *testing.T) {
	t.Parallel()

	ref := core.HandlerRef("agentic-claude")
	if IsAllMechanicalRef(&ref) {
		t.Errorf("WM-022a: IsAllMechanicalRef(non-nil) = true; want false")
	}
}

// ── Integration: all-mechanical → ShouldDispatchConflictResolver → EscalateNullRef ──

// TestWM022a_NullRefSkipsReDispatch verifies that when implementer_handler_ref is nil,
// ShouldDispatchConflictResolver returns EscalateNullRef — meaning WM-024 re-dispatch
// is skipped regardless of attempt count or cap.
//
// Spec ref: WM-022a — "the workspace manager MUST skip WM-024 re-dispatch … on
// conflict detection."
func TestWM022a_NullRefSkipsReDispatch(t *testing.T) {
	t.Parallel()

	noRetire := func(_ core.HandlerRef) bool { return false }

	// null ref with 0 attempts (first conflict detection).
	dec := ShouldDispatchConflictResolver(nil, 0, DefaultConflictResolutionAttemptCap, noRetire)
	if dec != ConflictResolveEscalateNullRef {
		t.Errorf("WM-022a: ShouldDispatch(nil, 0, cap, noRetire) = %q; want %q",
			dec, ConflictResolveEscalateNullRef)
	}

	// null ref NEVER dispatches, even below any cap.
	for attempts := 0; attempts <= DefaultConflictResolutionAttemptCap+1; attempts++ {
		dec = ShouldDispatchConflictResolver(nil, attempts, DefaultConflictResolutionAttemptCap, noRetire)
		if dec != ConflictResolveEscalateNullRef {
			t.Errorf("WM-022a: ShouldDispatch(nil, attempts=%d, cap=%d) = %q; want %q",
				attempts, DefaultConflictResolutionAttemptCap, dec, ConflictResolveEscalateNullRef)
		}
		if dec == ConflictResolveDispatch {
			t.Errorf("WM-022a: ShouldDispatch(nil, attempts=%d) returned Dispatch; MUST NOT dispatch for null ref",
				attempts)
		}
	}
}

// TestWM022a_NullRefDecisionIsNotDispatch verifies the "MUST NOT silently remap"
// rule: the decision for a null implementer_handler_ref MUST be an escalate variant,
// never Dispatch. This ensures no unrelated handler class is substituted.
//
// Spec ref: WM-022a — "The system MUST NOT silently remap the implementer role to
// an unrelated handler class."
func TestWM022a_NullRefDecisionIsNotDispatch(t *testing.T) {
	t.Parallel()

	noRetire := func(_ core.HandlerRef) bool { return false }
	retireAll := func(_ core.HandlerRef) bool { return true }

	for _, isRetired := range []func(core.HandlerRef) bool{noRetire, retireAll, nil} {
		dec := ShouldDispatchConflictResolver(nil, 0, DefaultConflictResolutionAttemptCap, isRetired)
		if dec == ConflictResolveDispatch {
			t.Errorf("WM-022a[no-silent-remap]: ShouldDispatch(nil ref) = Dispatch; want any escalate variant")
		}
	}
}

// ── Integration: all-mechanical full path ────────────────────────────────────

// TestWM022a_AllMechanicalDirectEscalationPath verifies the full WM-022a path:
//
//  1. IsAllMechanicalBranch(ws) = true (nil ref)
//  2. ShouldDispatchConflictResolver returns EscalateNullRef (re-dispatch skipped)
//  3. BuildConflictEscalationPayload succeeds from conflict-resolving
//  4. Workspace transitions to discarded per WM-023
//
// Spec ref: WM-022a — "the workspace manager MUST skip WM-024 re-dispatch and
// emit merge_conflict_escalation directly per WM-023 on conflict detection."
func TestWM022a_AllMechanicalDirectEscalationPath(t *testing.T) {
	t.Parallel()

	ws := wm022aFixtureWorkspace() // conflict-resolving, nil ref

	// Step 1: confirm all-mechanical.
	if !IsAllMechanicalBranch(ws) {
		t.Fatal("WM-022a: precondition: IsAllMechanicalBranch = false; want true")
	}

	// Step 2: dispatch decision — must be EscalateNullRef (re-dispatch skipped).
	noRetire := func(_ core.HandlerRef) bool { return false }
	dec := ShouldDispatchConflictResolver(
		ws.ImplementerHandlerRef, // nil
		0,                        // no prior attempts
		DefaultConflictResolutionAttemptCap,
		noRetire,
	)
	if dec != ConflictResolveEscalateNullRef {
		t.Fatalf("WM-022a: decision = %q; want EscalateNullRef", dec)
	}

	// Step 3: build escalation payload (WM-023 path, no re-dispatch).
	conflictPaths := []string{"internal/core/foo.go"}
	payload, err := BuildConflictEscalationPayload(ws, conflictPaths, "2026-05-07T12:00:00Z")
	if err != nil {
		t.Fatalf("WM-022a: BuildConflictEscalationPayload: %v", err)
	}
	if !payload.Valid() {
		t.Fatalf("WM-022a: escalation payload.Valid() = false")
	}

	// Step 4: transition to discarded after escalation event emission.
	if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
		t.Fatalf("WM-022a: Transition(conflict-resolving → discarded): %v", err)
	}
	if ws.State != core.WorkspaceStateDiscarded {
		t.Errorf("WM-022a: final state = %q; want discarded", ws.State)
	}

	// WM-037a: interrupt_state cleared to none in terminal state.
	if ws.InterruptState != core.InterruptStateNone {
		t.Errorf("WM-022a+WM-037a: interrupt_state = %q; want none", ws.InterruptState)
	}
}

// TestWM022a_IsAllMechanicalBranchConsistentWithRef verifies that IsAllMechanicalBranch
// and IsAllMechanicalRef are consistent: IsAllMechanicalBranch(ws) always equals
// IsAllMechanicalRef(ws.ImplementerHandlerRef) for non-nil workspaces.
func TestWM022a_IsAllMechanicalBranchConsistentWithRef(t *testing.T) {
	t.Parallel()

	// nil ref case.
	wsNil := wm022aFixtureWorkspace() // nil ref
	if IsAllMechanicalBranch(wsNil) != IsAllMechanicalRef(wsNil.ImplementerHandlerRef) {
		t.Errorf("WM-022a: IsAllMechanicalBranch(nil-ref ws) = %v, IsAllMechanicalRef(nil) = %v; must be equal",
			IsAllMechanicalBranch(wsNil), IsAllMechanicalRef(wsNil.ImplementerHandlerRef))
	}

	// non-nil ref case.
	wsAgentic := wm022aFixtureWorkspace()
	ref := core.HandlerRef("agentic-claude")
	wsAgentic.ImplementerHandlerRef = &ref
	if IsAllMechanicalBranch(wsAgentic) != IsAllMechanicalRef(wsAgentic.ImplementerHandlerRef) {
		t.Errorf("WM-022a: IsAllMechanicalBranch(agentic ws) = %v, IsAllMechanicalRef(&ref) = %v; must be equal",
			IsAllMechanicalBranch(wsAgentic), IsAllMechanicalRef(wsAgentic.ImplementerHandlerRef))
	}
}
