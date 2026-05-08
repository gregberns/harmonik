package workspace

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// Tests for the workspace lifecycle state machine per workspace-model.md §4.4
// WM-014 and the §7.1 transition table.
//
// Helper prefix: stateMachineFixture (bead hk-8mwo.24; avoids collision with
// sibling-bead helpers such as leaseFixture, mergeBackFixture, etc.).

// stateMachineFixtureWorkspace returns a *Workspace initialised to the
// zero/pre-create state, suitable for threading through Transition calls.
// The WorkspaceID, RunID, and other required fields are pre-populated with
// deterministic test values.
func stateMachineFixtureWorkspace() *Workspace {
	runID := "0196a1b2-c3d4-7000-8000-000000000014"
	return &Workspace{
		WorkspaceID:    "ws-" + runID,
		Repository:     "/tmp/harmonik-test-repo",
		ParentCommit:   "aabbccdd" + "aabbccdd" + "aabbccdd" + "aabbccdd" + "aabb",
		BranchName:     "run/" + runID,
		Path:           "/tmp/harmonik-test-repo/.harmonik/worktrees/" + runID,
		State:          "", // zero value: "initial" per §7.1
		InterruptState: core.InterruptStateNone,
		Metadata: map[string]string{
			"created_at":           "2026-05-07T00:00:00Z",
			"operator_fingerprint": "test-operator",
		},
		SchemaVersion: 1,
	}
}

// TestWM014_HappyPath_FullLifecycle exercises the canonical lifecycle path:
//
//	(initial) → created → ready → leased → merge-pending → merged
//
// per workspace-model.md §4.4 WM-014 and §7.1.
func TestWM014_HappyPath_FullLifecycle(t *testing.T) {
	t.Parallel()

	ws := stateMachineFixtureWorkspace()

	steps := []struct {
		name string
		next core.WorkspaceState
	}{
		{"initial → created", core.WorkspaceStateCreated},
		{"created → ready", core.WorkspaceStateReady},
		{"ready → leased", core.WorkspaceStateLeased},
		{"leased → merge-pending", core.WorkspaceStateMergePending},
		{"merge-pending → merged", core.WorkspaceStateMerged},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			if err := Transition(ws, step.next); err != nil {
				t.Fatalf("WM-014[%s]: Transition(%q → %q): unexpected error: %v",
					step.name, ws.State, step.next, err)
			}
			if ws.State != step.next {
				t.Errorf("WM-014[%s]: ws.State = %q, want %q", step.name, ws.State, step.next)
			}
		})
	}

	// Merged is terminal — no further transitions allowed.
	if !IsTerminal(ws.State) {
		t.Errorf("WM-014: merged state should be terminal; IsTerminal(%q) = false", ws.State)
	}
}

// TestWM014_HappyPath_LeasedToDiscarded exercises the failure-path:
//
//	(initial) → created → ready → leased → discarded
//
// This models the run-reached-terminal-failure case per §7.1.
func TestWM014_HappyPath_LeasedToDiscarded(t *testing.T) {
	t.Parallel()

	ws := stateMachineFixtureWorkspace()

	transitions := []core.WorkspaceState{
		core.WorkspaceStateCreated,
		core.WorkspaceStateReady,
		core.WorkspaceStateLeased,
		core.WorkspaceStateDiscarded,
	}
	for _, next := range transitions {
		if err := Transition(ws, next); err != nil {
			t.Fatalf("WM-014[leased→discarded]: Transition(%q → %q): unexpected error: %v",
				ws.State, next, err)
		}
	}

	if ws.State != core.WorkspaceStateDiscarded {
		t.Errorf("WM-014: want discarded, got %q", ws.State)
	}
	if !IsTerminal(ws.State) {
		t.Errorf("WM-014: discarded state should be terminal")
	}
}

// TestWM014_ConflictResolvingPath exercises the optional conflict path:
//
//	… → leased → merge-pending → conflict-resolving → merge-pending → merged
//
// per §7.1 (conflict-resolving → merge-pending on implementer resolution).
func TestWM014_ConflictResolvingPath(t *testing.T) {
	t.Parallel()

	ws := stateMachineFixtureWorkspace()

	transitions := []core.WorkspaceState{
		core.WorkspaceStateCreated,
		core.WorkspaceStateReady,
		core.WorkspaceStateLeased,
		core.WorkspaceStateMergePending,
		core.WorkspaceStateConflictResolving,
		core.WorkspaceStateMergePending, // implementer resolves
		core.WorkspaceStateMerged,
	}
	for i, next := range transitions {
		from := ws.State
		if err := Transition(ws, next); err != nil {
			t.Fatalf("WM-014[conflict-path step %d]: Transition(%q → %q): unexpected error: %v",
				i, from, next, err)
		}
	}

	if ws.State != core.WorkspaceStateMerged {
		t.Errorf("WM-014[conflict-path]: final state = %q, want %q",
			ws.State, core.WorkspaceStateMerged)
	}
}

// TestWM014_ConflictResolvingToDiscarded exercises the escalation path:
//
//	… → conflict-resolving → discarded
//
// per §7.1 (re-dispatch exhausted OR all-mechanical per WM-023).
func TestWM014_ConflictResolvingToDiscarded(t *testing.T) {
	t.Parallel()

	ws := stateMachineFixtureWorkspace()

	transitions := []core.WorkspaceState{
		core.WorkspaceStateCreated,
		core.WorkspaceStateReady,
		core.WorkspaceStateLeased,
		core.WorkspaceStateMergePending,
		core.WorkspaceStateConflictResolving,
		core.WorkspaceStateDiscarded,
	}
	for i, next := range transitions {
		from := ws.State
		if err := Transition(ws, next); err != nil {
			t.Fatalf("WM-014[escalation step %d]: Transition(%q → %q): unexpected error: %v",
				i, from, next, err)
		}
	}

	if ws.State != core.WorkspaceStateDiscarded {
		t.Errorf("WM-014[escalation]: state = %q, want %q",
			ws.State, core.WorkspaceStateDiscarded)
	}
}

// TestWM014_InvalidTransitions verifies that Transition returns ErrInvalidTransition
// for every pair not declared in the §7.1 table.
func TestWM014_InvalidTransitions(t *testing.T) {
	t.Parallel()

	// Each case names a (from, to) pair that §7.1 does NOT permit.
	cases := []struct {
		name string
		from core.WorkspaceState
		to   core.WorkspaceState
	}{
		// Forward skips.
		{"created → leased (skip ready)", core.WorkspaceStateCreated, core.WorkspaceStateLeased},
		{"created → merge-pending (skip)", core.WorkspaceStateCreated, core.WorkspaceStateMergePending},
		{"ready → merge-pending (skip leased)", core.WorkspaceStateReady, core.WorkspaceStateMergePending},
		{"ready → discarded (not permitted)", core.WorkspaceStateReady, core.WorkspaceStateDiscarded},

		// Backward transitions (not permitted in this state machine).
		{"leased → ready (backward)", core.WorkspaceStateLeased, core.WorkspaceStateReady},
		{"leased → created (backward)", core.WorkspaceStateLeased, core.WorkspaceStateCreated},
		{"merge-pending → leased (backward)", core.WorkspaceStateMergePending, core.WorkspaceStateLeased},
		{"merged → discarded (terminal absorbing)", core.WorkspaceStateMerged, core.WorkspaceStateDiscarded},

		// Terminal self-transitions.
		{"merged → merged", core.WorkspaceStateMerged, core.WorkspaceStateMerged},
		{"discarded → discarded", core.WorkspaceStateDiscarded, core.WorkspaceStateDiscarded},
		{"discarded → created", core.WorkspaceStateDiscarded, core.WorkspaceStateCreated},

		// conflict-resolving may not jump to merged directly.
		{"conflict-resolving → merged (must go through merge-pending)", core.WorkspaceStateConflictResolving, core.WorkspaceStateMerged},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ws := stateMachineFixtureWorkspace()
			ws.State = tc.from

			err := Transition(ws, tc.to)
			if err == nil {
				t.Fatalf("WM-014[%s]: Transition(%q → %q): expected ErrInvalidTransition, got nil",
					tc.name, tc.from, tc.to)
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("WM-014[%s]: Transition(%q → %q): error %v; want errors.Is(_, ErrInvalidTransition)",
					tc.name, tc.from, tc.to, err)
			}
			// State must remain unchanged after a rejected transition.
			if ws.State != tc.from {
				t.Errorf("WM-014[%s]: ws.State changed from %q to %q on rejected transition",
					tc.name, tc.from, ws.State)
			}
		})
	}
}

// TestWM014_TerminalStateClearsInterruptState verifies WM-037a: entering a
// terminal state MUST clear any non-none InterruptState back to none.
func TestWM014_TerminalStateClearsInterruptState(t *testing.T) {
	t.Parallel()

	interruptValues := []core.InterruptState{
		core.InterruptStateOperatorPaused,
		core.InterruptStateOperatorStoppedGraceful,
		core.InterruptStateOperatorStoppedImmediate,
		core.InterruptStateDaemonCrashSuspected,
	}

	terminalTransitions := []struct {
		name string
		from core.WorkspaceState
		to   core.WorkspaceState
	}{
		{"leased → discarded", core.WorkspaceStateLeased, core.WorkspaceStateDiscarded},
		{"merge-pending → merged", core.WorkspaceStateMergePending, core.WorkspaceStateMerged},
		{"conflict-resolving → discarded", core.WorkspaceStateConflictResolving, core.WorkspaceStateDiscarded},
	}

	for _, tt := range terminalTransitions {
		for _, iv := range interruptValues {
			t.Run(tt.name+"/interrupt="+string(iv), func(t *testing.T) {
				t.Parallel()

				ws := stateMachineFixtureWorkspace()
				ws.State = tt.from
				ws.InterruptState = iv // non-none interrupt before terminal transition

				if err := Transition(ws, tt.to); err != nil {
					t.Fatalf("WM-037a[%s/%s]: Transition: %v", tt.name, iv, err)
				}
				if ws.InterruptState != core.InterruptStateNone {
					t.Errorf("WM-037a[%s/%s]: interrupt_state = %q after terminal transition; want %q",
						tt.name, iv, ws.InterruptState, core.InterruptStateNone)
				}
			})
		}
	}
}

// TestWM014_IsTerminal verifies IsTerminal returns true for merged and discarded,
// and false for all in-flight states.
func TestWM014_IsTerminal(t *testing.T) {
	t.Parallel()

	cases := []struct {
		state    core.WorkspaceState
		terminal bool
	}{
		{core.WorkspaceStateMerged, true},
		{core.WorkspaceStateDiscarded, true},
		{core.WorkspaceStateCreated, false},
		{core.WorkspaceStateReady, false},
		{core.WorkspaceStateLeased, false},
		{core.WorkspaceStateMergePending, false},
		{core.WorkspaceStateConflictResolving, false},
	}

	for _, tc := range cases {
		t.Run(string(tc.state), func(t *testing.T) {
			t.Parallel()

			got := IsTerminal(tc.state)
			if got != tc.terminal {
				t.Errorf("IsTerminal(%q) = %v, want %v", tc.state, got, tc.terminal)
			}
			// IsInFlight must be the logical complement for valid states.
			gotInFlight := IsInFlight(tc.state)
			if gotInFlight == tc.terminal {
				t.Errorf("IsInFlight(%q) = %v; want opposite of IsTerminal (%v)",
					tc.state, gotInFlight, tc.terminal)
			}
		})
	}
}

// TestWM014_SetupRetired verifies that the retired "setup" value from WM v0.2
// is not a valid WorkspaceState constant and will be rejected as a Transition
// target (via the Valid() guard path).
func TestWM014_SetupRetired(t *testing.T) {
	t.Parallel()

	retiredSetup := core.WorkspaceState("setup")
	if retiredSetup.Valid() {
		t.Errorf("WM-014: retired value %q must not be valid per workspace-model.md §12", retiredSetup)
	}

	ws := stateMachineFixtureWorkspace()
	ws.State = core.WorkspaceStateCreated

	err := Transition(ws, retiredSetup)
	if err == nil {
		t.Fatal("WM-014: Transition to retired 'setup' value must return an error")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("WM-014: expected ErrInvalidTransition for retired 'setup', got: %v", err)
	}
}
