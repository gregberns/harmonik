package workspace

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

func wm023FixtureRunID() core.RunID {
	return core.RunID(uuid.MustParse("0196b200-0000-7000-8000-000000023500"))
}

func wm023FixtureWorkspace() *Workspace {
	runID := wm023FixtureRunID()
	ref := core.HandlerRef("agentic-claude")
	return &Workspace{
		WorkspaceID:           WorkspaceIDFromRunID(runID.String()),
		RunID:                 runID,
		Repository:            "/tmp/harmonik-test-repo",
		ParentCommit:          "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
		BranchName:            "run/" + runID.String(),
		Path:                  "/tmp/harmonik-test-repo/.harmonik/worktrees/" + runID.String(),
		State:                 core.WorkspaceStateConflictResolving,
		InterruptState:        core.InterruptStateNone,
		ImplementerHandlerRef: &ref,
		Metadata: map[string]string{
			"created_at":           "2026-05-07T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: WorkspaceSchemaVersion,
	}
}

var wm023FixtureConflictPaths = []string{"internal/core/foo.go", "cmd/harmonik/main.go"}

const wm023FixtureEscalatedAt = "2026-05-07T12:00:00Z"

// TestWM023_StateGuard_RequiresConflictResolving verifies that
// BuildConflictEscalationPayload rejects any state other than conflict-resolving
// and wraps ErrEscalationNotInConflictResolving.
//
// Spec ref: WM-023 — "the transition is single-entry; escalation marks the
// resolution path as exhausted and the workspace transitions to discarded after
// the escalation event is emitted."
func TestWM023_StateGuard_RequiresConflictResolving(t *testing.T) {
	t.Parallel()

	nonConflictStates := []core.WorkspaceState{
		core.WorkspaceStateCreated,
		core.WorkspaceStateReady,
		core.WorkspaceStateLeased,
		core.WorkspaceStateMergePending,
		core.WorkspaceStateMerged,
		core.WorkspaceStateDiscarded,
	}

	for _, s := range nonConflictStates {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()

			ws := wm023FixtureWorkspace()
			ws.State = s

			_, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
			if err == nil {
				t.Errorf("WM-023: BuildConflictEscalationPayload from state %q: expected error, got nil", s)
				return
			}
			if !errors.Is(err, ErrEscalationNotInConflictResolving) {
				t.Errorf("WM-023: state %q: error does not wrap ErrEscalationNotInConflictResolving: %v", s, err)
			}
		})
	}
}

// TestWM023_StateGuard_AcceptsConflictResolving verifies that
// BuildConflictEscalationPayload succeeds when the workspace is in
// conflict-resolving state.
func TestWM023_StateGuard_AcceptsConflictResolving(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace() // already in conflict-resolving
	payload, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err != nil {
		t.Fatalf("WM-023: BuildConflictEscalationPayload from conflict-resolving: %v", err)
	}
	if !payload.Valid() {
		t.Errorf("WM-023: returned payload fails Valid()")
	}
}

// TestWM023_StateGuard_NilWorkspace verifies that a nil workspace is rejected.
func TestWM023_StateGuard_NilWorkspace(t *testing.T) {
	t.Parallel()

	_, err := BuildConflictEscalationPayload(nil, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err == nil {
		t.Error("WM-023: BuildConflictEscalationPayload(nil ws): expected error, got nil")
	}
}

// TestWM023_Payload_EmptyConflictPathsRejected verifies that an empty
// conflictPaths slice is rejected per §8.5.6 (non-nil and non-empty).
func TestWM023_Payload_EmptyConflictPathsRejected(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()

	t.Run("nil-paths", func(t *testing.T) {
		t.Parallel()
		_, err := BuildConflictEscalationPayload(ws, nil, wm023FixtureEscalatedAt)
		if err == nil {
			t.Error("WM-023: nil conflictPaths: expected error, got nil")
		}
	})

	t.Run("empty-slice", func(t *testing.T) {
		t.Parallel()
		_, err := BuildConflictEscalationPayload(ws, []string{}, wm023FixtureEscalatedAt)
		if err == nil {
			t.Error("WM-023: empty conflictPaths: expected error, got nil")
		}
	})
}

// TestWM023_Payload_EmptyEscalatedAtRejected verifies that an empty escalatedAt
// string is rejected per §8.5.6 (required non-empty).
func TestWM023_Payload_EmptyEscalatedAtRejected(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	_, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, "")
	if err == nil {
		t.Error("WM-023: empty escalatedAt: expected error, got nil")
	}
}

// TestWM023_Payload_ZeroRunIDRejected verifies that a workspace with a zero RunID
// produces an invalid payload and is rejected.
func TestWM023_Payload_ZeroRunIDRejected(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	ws.RunID = core.RunID{} // zero UUID

	_, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err == nil {
		t.Error("WM-023: zero RunID: expected error (invalid payload), got nil")
	}
}

// TestWM023_Payload_ConflictPathsCarried verifies that the conflictPaths slice is
// carried into the payload unchanged per event-model.md §8.5.6.
func TestWM023_Payload_ConflictPathsCarried(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	paths := []string{"a/b.go", "c/d.go", "e/f.go"}
	payload, err := BuildConflictEscalationPayload(ws, paths, wm023FixtureEscalatedAt)
	if err != nil {
		t.Fatalf("WM-023: unexpected error: %v", err)
	}
	if len(payload.ConflictPaths) != len(paths) {
		t.Errorf("WM-023: payload.ConflictPaths len = %d, want %d",
			len(payload.ConflictPaths), len(paths))
	}
	for i, want := range paths {
		if payload.ConflictPaths[i] != want {
			t.Errorf("WM-023: payload.ConflictPaths[%d] = %q, want %q",
				i, payload.ConflictPaths[i], want)
		}
	}
}

// TestWM023_Payload_EscalatedAtCarried verifies that escalatedAt is carried into
// the payload unchanged per event-model.md §8.5.6.
func TestWM023_Payload_EscalatedAtCarried(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	at := "2026-05-07T15:30:00Z"
	payload, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, at)
	if err != nil {
		t.Fatalf("WM-023: unexpected error: %v", err)
	}
	if payload.EscalatedAt != at {
		t.Errorf("WM-023: payload.EscalatedAt = %q, want %q", payload.EscalatedAt, at)
	}
}

// TestWM023_Payload_RunIDDerived verifies that the payload RunID equals the
// workspace's RunID per event-model.md §8.5.6.
func TestWM023_Payload_RunIDDerived(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	payload, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err != nil {
		t.Fatalf("WM-023: unexpected error: %v", err)
	}
	if payload.RunID != ws.RunID {
		t.Errorf("WM-023: payload.RunID = %v, want %v (workspace RunID)", payload.RunID, ws.RunID)
	}
}

// TestWM023_Payload_WorkspaceIDDerived verifies that the payload WorkspaceID is
// derived from the workspace's RunID per workspace-model.md §4.1.WM-004
// ("workspace_id = 'ws-' + run_id"; UUID part equals run_id UUID).
func TestWM023_Payload_WorkspaceIDDerived(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	payload, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err != nil {
		t.Fatalf("WM-023: unexpected error: %v", err)
	}

	wantWorkspaceID := core.WorkspaceID(ws.RunID)
	if payload.WorkspaceID != wantWorkspaceID {
		t.Errorf("WM-023: payload.WorkspaceID = %v, want %v (core.WorkspaceID(ws.RunID) per WM-004)",
			payload.WorkspaceID, wantWorkspaceID)
	}

	if uuid.UUID(payload.WorkspaceID) == uuid.Nil {
		t.Error("WM-023: payload.WorkspaceID is uuid.Nil; want non-nil UUID derived from RunID")
	}
}

// TestWM023_SingleEntryInvariant_ConflictResolvingEnteredOnce verifies that
// workspace enters conflict-resolving exactly once per merge-pending cycle.
//
// The state machine prohibits re-entering conflict-resolving from conflict-resolving
// (conflict-resolving → conflict-resolving is not in the §7.1 table). Escalation
// (the exit from conflict-resolving) uses conflict-resolving → discarded, not
// conflict-resolving → conflict-resolving.
//
// Spec ref: WM-023 — "the transition is single-entry."
func TestWM023_SingleEntryInvariant_ConflictResolvingEnteredOnce(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	err := Transition(ws, core.WorkspaceStateConflictResolving)
	if err == nil {
		t.Error("WM-023[single-entry]: Transition(conflict-resolving → conflict-resolving): want ErrInvalidTransition, got nil")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("WM-023[single-entry]: expected ErrInvalidTransition; got: %v", err)
	}
	if ws.State != core.WorkspaceStateConflictResolving {
		t.Errorf("WM-023[single-entry]: ws.State changed to %q after rejected transition; want conflict-resolving",
			ws.State)
	}
}

// TestWM023_SingleEntryInvariant_EscalationGoesDirectlyToDiscarded verifies that
// the escalation path is conflict-resolving → discarded (NOT via conflict-resolving
// again), and that BuildConflictEscalationPayload is only callable from
// conflict-resolving.
func TestWM023_SingleEntryInvariant_EscalationGoesDirectlyToDiscarded(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace() // conflict-resolving

	payload, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err != nil {
		t.Fatalf("WM-023: BuildConflictEscalationPayload: %v", err)
	}
	if !payload.Valid() {
		t.Fatalf("WM-023: payload.Valid() = false")
	}

	if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
		t.Fatalf("WM-023[single-entry]: Transition(conflict-resolving → discarded): %v", err)
	}
	if ws.State != core.WorkspaceStateDiscarded {
		t.Errorf("WM-023[single-entry]: state = %q; want discarded", ws.State)
	}

	_, err = BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err == nil {
		t.Error("WM-023[single-entry]: BuildConflictEscalationPayload from discarded state: expected error, got nil")
	}
	if !errors.Is(err, ErrEscalationNotInConflictResolving) {
		t.Errorf("WM-023[single-entry]: post-discard: expected ErrEscalationNotInConflictResolving; got: %v", err)
	}
}

// TestWM023_AllEscalateDecisionsRouteToEscalation verifies that each of the three
// ConflictResolveDecision escalate variants — EscalateNullRef, EscalateRetiredHandler,
// EscalateCapExhausted — routes to the BuildConflictEscalationPayload path, and
// that each produces a valid payload and successfully transitions to discarded.
//
// Spec ref: WM-023 — escalation covers cap-exhausted (WM-024), all-mechanical
// (WM-022a), and retired-handler (WM-024 terminal clause) paths.
func TestWM023_AllEscalateDecisionsRouteToEscalation(t *testing.T) {
	t.Parallel()

	noRetire := func(_ core.HandlerRef) bool { return false }
	retireAll := func(_ core.HandlerRef) bool { return true }
	activeRef := core.HandlerRef("agentic-claude")
	attemptCap := DefaultConflictResolutionAttemptCap

	cases := []struct {
		name     string
		ref      *core.HandlerRef
		attempts int
		retire   func(core.HandlerRef) bool
		wantDec  ConflictResolveDecision
	}{
		{
			name:     "EscalateNullRef (all-mechanical WM-022a)",
			ref:      nil,
			attempts: 0,
			retire:   noRetire,
			wantDec:  ConflictResolveEscalateNullRef,
		},
		{
			name:     "EscalateRetiredHandler (WM-024 terminal clause)",
			ref:      &activeRef,
			attempts: 0,
			retire:   retireAll,
			wantDec:  ConflictResolveEscalateRetiredHandler,
		},
		{
			name:     "EscalateCapExhausted (WM-024 / WM-023)",
			ref:      &activeRef,
			attempts: attemptCap,
			retire:   noRetire,
			wantDec:  ConflictResolveEscalateCapExhausted,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dec := ShouldDispatchConflictResolver(tc.ref, tc.attempts, attemptCap, tc.retire)
			if dec != tc.wantDec {
				t.Fatalf("WM-023: ShouldDispatchConflictResolver = %q; want %q", dec, tc.wantDec)
			}
			if dec == ConflictResolveDispatch {
				t.Fatalf("WM-023: unexpected Dispatch decision for %q; want escalate", tc.name)
			}

			ws := wm023FixtureWorkspace()

			payload, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
			if err != nil {
				t.Fatalf("WM-023[%s]: BuildConflictEscalationPayload: %v", tc.name, err)
			}
			if !payload.Valid() {
				t.Errorf("WM-023[%s]: payload.Valid() = false", tc.name)
			}

			if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
				t.Fatalf("WM-023[%s]: Transition(conflict-resolving → discarded): %v", tc.name, err)
			}
			if ws.State != core.WorkspaceStateDiscarded {
				t.Errorf("WM-023[%s]: state = %q; want discarded", tc.name, ws.State)
			}

			if ws.InterruptState != core.InterruptStateNone {
				t.Errorf("WM-023[%s]+WM-037a: interrupt_state = %q after discarded; want none",
					tc.name, ws.InterruptState)
			}
		})
	}
}

// TestWM023_FullEscalationSequence verifies the complete §7.1 escalation path:
//
//  1. merge-pending → conflict-resolving (initial conflict detection; single-entry)
//  2. BuildConflictEscalationPayload succeeds (resolution path exhausted)
//  3. (caller emits merge_conflict_escalation — simulated via recorder)
//  4. conflict-resolving → discarded (per WM-023 "workspace transitions to discarded
//     after the escalation event is emitted")
//  5. workspace_discarded emission (per §7.1 table row)
//
// The recorder verifies ordering: merge_conflict_escalation BEFORE workspace_discarded.
//
// Spec ref: workspace-model.md §7.1 row:
//
//	"conflict-resolving | re-dispatch exhausted OR all-mechanical per WM-022a |
//	 WM-023 trigger conditions | discarded |
//	 merge_conflict_escalation, workspace_discarded"
func TestWM023_FullEscalationSequence(t *testing.T) {
	t.Parallel()

	runID := wm023FixtureRunID()
	ref := core.HandlerRef("agentic-claude")
	ws := &Workspace{
		WorkspaceID:           WorkspaceIDFromRunID(runID.String()),
		RunID:                 runID,
		Repository:            "/tmp/harmonik-test-repo",
		ParentCommit:          "deadbeef" + "deadbeef" + "deadbeef" + "deadbeef" + "dead",
		BranchName:            "run/" + runID.String(),
		Path:                  "/tmp/harmonik-test-repo/.harmonik/worktrees/" + runID.String(),
		State:                 core.WorkspaceStateLeased,
		InterruptState:        core.InterruptStateNone,
		ImplementerHandlerRef: &ref,
		Metadata: map[string]string{
			"created_at":           "2026-05-07T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: WorkspaceSchemaVersion,
	}

	rec := workspaceEventsFixtureNewRecorder()

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateMergePending, rec, nil)
	if ws.State != core.WorkspaceStateMergePending {
		t.Fatalf("WM-023[full-seq]: state after merge-pending transition = %q", ws.State)
	}

	if err := Transition(ws, core.WorkspaceStateConflictResolving); err != nil {
		t.Fatalf("WM-023[full-seq]: merge-pending → conflict-resolving: %v", err)
	}
	if ws.State != core.WorkspaceStateConflictResolving {
		t.Fatalf("WM-023[full-seq]: state after conflict-resolving transition = %q", ws.State)
	}

	noRetire := func(_ core.HandlerRef) bool { return false }
	dec := ShouldDispatchConflictResolver(
		ws.ImplementerHandlerRef, DefaultConflictResolutionAttemptCap,
		DefaultConflictResolutionAttemptCap, noRetire,
	)
	if dec != ConflictResolveEscalateCapExhausted {
		t.Fatalf("WM-023[full-seq]: decision = %q; want EscalateCapExhausted", dec)
	}

	payload, err := BuildConflictEscalationPayload(
		ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt,
	)
	if err != nil {
		t.Fatalf("WM-023[full-seq]: BuildConflictEscalationPayload: %v", err)
	}

	rec.record("merge_conflict_escalation", core.WorkspaceStateConflictResolving,
		map[string]string{
			"workspace_id": payload.WorkspaceID.String(),
			"escalated_at": payload.EscalatedAt,
		})

	workspaceEventsFixtureTransitionAndRecord(t, ws, core.WorkspaceStateDiscarded, rec,
		map[string]string{"reason": "post_escalation"})

	posEsc := rec.positionOf("merge_conflict_escalation")
	posDisc := rec.positionOf("workspace_discarded")
	if posEsc < 0 {
		t.Fatal("WM-023[full-seq]: merge_conflict_escalation not recorded")
	}
	if posDisc < 0 {
		t.Fatal("WM-023[full-seq]: workspace_discarded not recorded")
	}
	if posEsc >= posDisc {
		t.Errorf("WM-023[full-seq]: merge_conflict_escalation at pos %d >= workspace_discarded at pos %d; want escalation before discard",
			posEsc, posDisc)
	}

	if rec.countOf("workspace_discarded") != 1 {
		t.Errorf("WM-023[full-seq]: workspace_discarded emitted %d times; want 1",
			rec.countOf("workspace_discarded"))
	}

	if ws.State != core.WorkspaceStateDiscarded {
		t.Errorf("WM-023[full-seq]: final state = %q; want discarded", ws.State)
	}

	if ws.InterruptState != core.InterruptStateNone {
		t.Errorf("WM-023[full-seq]+WM-037a: interrupt_state = %q; want none", ws.InterruptState)
	}
}

// TestWM023_AllMechanicalFullSequence verifies the all-mechanical path per
// WM-022a: null implementer_handler_ref → skip re-dispatch → direct escalation →
// discarded.
//
// This path enters conflict-resolving at initial conflict detection and immediately
// calls BuildConflictEscalationPayload (no re-dispatch attempts).
//
// Spec ref: workspace-model.md §4.6.WM-022a — "the workspace manager MUST skip
// WM-024 re-dispatch and emit merge_conflict_escalation directly per WM-023."
func TestWM023_AllMechanicalFullSequence(t *testing.T) {
	t.Parallel()

	ws := wm023FixtureWorkspace()
	ws.ImplementerHandlerRef = nil // all-mechanical per WM-022a

	noRetire := func(_ core.HandlerRef) bool { return false }
	dec := ShouldDispatchConflictResolver(nil, 0, DefaultConflictResolutionAttemptCap, noRetire)
	if dec != ConflictResolveEscalateNullRef {
		t.Fatalf("WM-023[all-mechanical]: decision = %q; want EscalateNullRef", dec)
	}

	payload, err := BuildConflictEscalationPayload(ws, wm023FixtureConflictPaths, wm023FixtureEscalatedAt)
	if err != nil {
		t.Fatalf("WM-023[all-mechanical]: BuildConflictEscalationPayload: %v", err)
	}
	if !payload.Valid() {
		t.Errorf("WM-023[all-mechanical]: payload.Valid() = false")
	}

	if err := Transition(ws, core.WorkspaceStateDiscarded); err != nil {
		t.Fatalf("WM-023[all-mechanical]: Transition(conflict-resolving → discarded): %v", err)
	}
	if ws.State != core.WorkspaceStateDiscarded {
		t.Errorf("WM-023[all-mechanical]: state = %q; want discarded", ws.State)
	}
}
