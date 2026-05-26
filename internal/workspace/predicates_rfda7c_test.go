package workspace

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// Property tests for workspace predicate evaluation (bead hk-rfda7 step c).
//
// These tests exhaustively verify the predicate functions — IsTerminal,
// IsInFlight, WorkspaceState.Valid(), InterruptState.Valid() — over the
// complete set of declared enum values, plus a representative sample of
// non-declared strings. The properties checked are:
//
//   - Completeness: every declared constant satisfies Valid().
//   - Partition: for every valid WorkspaceState, exactly one of IsTerminal or
//     IsInFlight is true (they are mutually exclusive and collectively exhaustive
//     over the valid state space).
//   - Rejection: non-declared strings fail Valid().
//   - Invariant: the zero value of WorkspaceState ("") fails Valid() — it is
//     the "initial" sentinel used by Transition but is NOT a legal lifecycle
//     state per §7.1.
//   - Retirement: the "setup" value retired in WM v0.3.0 fails Valid() per §12.

// allWorkspaceStates is the complete set of declared WorkspaceState constants.
// Must match workspace-model.md §4.4 WM-014 (7 values).
var allWorkspaceStates = []core.WorkspaceState{
	core.WorkspaceStateCreated,
	core.WorkspaceStateReady,
	core.WorkspaceStateLeased,
	core.WorkspaceStateMergePending,
	core.WorkspaceStateConflictResolving,
	core.WorkspaceStateMerged,
	core.WorkspaceStateDiscarded,
}

// allInterruptStates is the complete set of declared InterruptState constants.
// Must match workspace-model.md §4.10 (5 values).
var allInterruptStates = []core.InterruptState{
	core.InterruptStateNone,
	core.InterruptStateOperatorPaused,
	core.InterruptStateOperatorStoppedGraceful,
	core.InterruptStateOperatorStoppedImmediate,
	core.InterruptStateDaemonCrashSuspected,
}

// terminalWorkspaceStates is the subset of allWorkspaceStates that are terminal.
var terminalWorkspaceStates = []core.WorkspaceState{
	core.WorkspaceStateMerged,
	core.WorkspaceStateDiscarded,
}

// inFlightWorkspaceStates is the subset of allWorkspaceStates that are in-flight.
var inFlightWorkspaceStates = []core.WorkspaceState{
	core.WorkspaceStateCreated,
	core.WorkspaceStateReady,
	core.WorkspaceStateLeased,
	core.WorkspaceStateMergePending,
	core.WorkspaceStateConflictResolving,
}

// TestRFDA7c_WorkspaceState_ValidForAllDeclaredConstants verifies that every
// WorkspaceState constant declared in workspace-model.md §4.4 satisfies Valid().
func TestRFDA7c_WorkspaceState_ValidForAllDeclaredConstants(t *testing.T) {
	t.Parallel()

	for _, s := range allWorkspaceStates {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			if !s.Valid() {
				t.Errorf("WorkspaceState(%q).Valid() = false; want true for declared constant", s)
			}
		})
	}
}

// TestRFDA7c_WorkspaceState_ValidRejectsNonConstants verifies that non-declared
// strings fail WorkspaceState.Valid().
func TestRFDA7c_WorkspaceState_ValidRejectsNonConstants(t *testing.T) {
	t.Parallel()

	invalidValues := []string{
		"",             // zero value — "initial" sentinel, not a legal state
		"setup",        // retired in WM v0.3.0 per §12
		"unknown",
		"CREATED",      // case-sensitive
		"merge_pending", // wrong separator
		"in-flight",
		"terminal",
	}

	for _, v := range invalidValues {
		v := v
		t.Run("value="+repr(v), func(t *testing.T) {
			t.Parallel()
			s := core.WorkspaceState(v)
			if s.Valid() {
				t.Errorf("WorkspaceState(%q).Valid() = true; want false for non-declared value", v)
			}
		})
	}
}

// TestRFDA7c_WorkspaceState_ZeroValueIsInvalid verifies that the zero value of
// WorkspaceState ("") is not valid. The empty string is the "initial" sentinel
// used by Transition's (from == "") branch to model pre-create state, but it
// is NOT a legal lifecycle state per §7.1 and MUST fail Valid().
func TestRFDA7c_WorkspaceState_ZeroValueIsInvalid(t *testing.T) {
	t.Parallel()

	var zero core.WorkspaceState
	if zero.Valid() {
		t.Errorf("zero WorkspaceState(%q).Valid() = true; want false (initial sentinel, not a state)", zero)
	}
}

// TestRFDA7c_WorkspaceState_SetupRetiredIsInvalid verifies that the retired
// "setup" value from WM v0.2 fails Valid() per §12 retirement rule.
func TestRFDA7c_WorkspaceState_SetupRetiredIsInvalid(t *testing.T) {
	t.Parallel()

	retired := core.WorkspaceState("setup")
	if retired.Valid() {
		t.Errorf("WorkspaceState(%q).Valid() = true; want false (retired per §12)", retired)
	}
}

// TestRFDA7c_IsTerminal_TrueForTerminalStatesOnly verifies that IsTerminal
// returns true for exactly the two terminal states (merged, discarded) and
// false for all in-flight states.
func TestRFDA7c_IsTerminal_TrueForTerminalStatesOnly(t *testing.T) {
	t.Parallel()

	for _, s := range terminalWorkspaceStates {
		s := s
		t.Run("terminal/"+string(s), func(t *testing.T) {
			t.Parallel()
			if !IsTerminal(s) {
				t.Errorf("IsTerminal(%q) = false; want true for terminal state", s)
			}
		})
	}
	for _, s := range inFlightWorkspaceStates {
		s := s
		t.Run("in-flight/"+string(s), func(t *testing.T) {
			t.Parallel()
			if IsTerminal(s) {
				t.Errorf("IsTerminal(%q) = true; want false for in-flight state", s)
			}
		})
	}
}

// TestRFDA7c_IsInFlight_TrueForInFlightStatesOnly verifies that IsInFlight
// returns true for exactly the five in-flight states and false for the two
// terminal states.
func TestRFDA7c_IsInFlight_TrueForInFlightStatesOnly(t *testing.T) {
	t.Parallel()

	for _, s := range inFlightWorkspaceStates {
		s := s
		t.Run("in-flight/"+string(s), func(t *testing.T) {
			t.Parallel()
			if !IsInFlight(s) {
				t.Errorf("IsInFlight(%q) = false; want true for in-flight state", s)
			}
		})
	}
	for _, s := range terminalWorkspaceStates {
		s := s
		t.Run("terminal/"+string(s), func(t *testing.T) {
			t.Parallel()
			if IsInFlight(s) {
				t.Errorf("IsInFlight(%q) = true; want false for terminal state", s)
			}
		})
	}
}

// TestRFDA7c_IsTerminalIsInFlight_Partition verifies the partition property:
// for every valid WorkspaceState exactly one of IsTerminal or IsInFlight is
// true. Neither state is both terminal and in-flight; neither is neither.
func TestRFDA7c_IsTerminalIsInFlight_Partition(t *testing.T) {
	t.Parallel()

	for _, s := range allWorkspaceStates {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			terminal := IsTerminal(s)
			inFlight := IsInFlight(s)
			switch {
			case terminal && inFlight:
				t.Errorf("state %q is both terminal and in-flight; states must be mutually exclusive", s)
			case !terminal && !inFlight:
				t.Errorf("state %q is neither terminal nor in-flight; valid states must be one or the other", s)
			}
		})
	}
}

// TestRFDA7c_IsTerminalIsInFlight_AreComplementaryForValidStates verifies that
// IsInFlight(s) == !IsTerminal(s) for every declared valid state.
func TestRFDA7c_IsTerminalIsInFlight_AreComplementaryForValidStates(t *testing.T) {
	t.Parallel()

	for _, s := range allWorkspaceStates {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			if IsInFlight(s) == IsTerminal(s) {
				t.Errorf("IsInFlight(%q) == IsTerminal(%q) == %v; want IsInFlight = !IsTerminal",
					s, s, IsTerminal(s))
			}
		})
	}
}

// TestRFDA7c_IsTerminalIsInFlight_InvalidStateReturnsFalse verifies that
// invalid WorkspaceState values return false for both IsTerminal and IsInFlight.
func TestRFDA7c_IsTerminalIsInFlight_InvalidStateReturnsFalse(t *testing.T) {
	t.Parallel()

	invalidValues := []core.WorkspaceState{
		"",        // zero value
		"setup",   // retired
		"unknown",
	}

	for _, s := range invalidValues {
		s := s
		t.Run("value="+repr(string(s)), func(t *testing.T) {
			t.Parallel()
			if IsTerminal(s) {
				t.Errorf("IsTerminal(%q) = true for invalid state; want false", s)
			}
			if IsInFlight(s) {
				t.Errorf("IsInFlight(%q) = true for invalid state; want false", s)
			}
		})
	}
}

// TestRFDA7c_InterruptState_ValidForAllDeclaredConstants verifies that every
// InterruptState constant declared in workspace-model.md §4.10 satisfies Valid().
func TestRFDA7c_InterruptState_ValidForAllDeclaredConstants(t *testing.T) {
	t.Parallel()

	for _, s := range allInterruptStates {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			if !s.Valid() {
				t.Errorf("InterruptState(%q).Valid() = false; want true for declared constant", s)
			}
		})
	}
}

// TestRFDA7c_InterruptState_ValidRejectsNonConstants verifies that non-declared
// strings fail InterruptState.Valid().
func TestRFDA7c_InterruptState_ValidRejectsNonConstants(t *testing.T) {
	t.Parallel()

	invalidValues := []string{
		"",                      // zero value
		"paused",                // abbreviated form
		"operator_paused",       // wrong separator
		"DAEMON-CRASH-SUSPECTED", // wrong case
		"interrupted",
		"stopped",
	}

	for _, v := range invalidValues {
		v := v
		t.Run("value="+repr(v), func(t *testing.T) {
			t.Parallel()
			s := core.InterruptState(v)
			if s.Valid() {
				t.Errorf("InterruptState(%q).Valid() = true; want false for non-declared value", v)
			}
		})
	}
}

// TestRFDA7c_InterruptState_NoneIsValidAndIsZeroLike verifies that
// InterruptStateNone is a declared constant that satisfies Valid() — it is the
// default/initial value, not an "absent" sentinel. Callers should set
// InterruptState to this constant (not "") for a workspace with no interruption.
func TestRFDA7c_InterruptState_NoneIsValidAndIsZeroLike(t *testing.T) {
	t.Parallel()

	none := core.InterruptStateNone
	if !none.Valid() {
		t.Errorf("InterruptStateNone.Valid() = false; want true (none is a declared constant)")
	}
	if string(none) == "" {
		t.Error("InterruptStateNone is the empty string; want a non-empty declared value")
	}
}

// TestRFDA7c_WorkspaceValid_RequiresAllFields verifies the property that
// Workspace.Valid() returns nil iff all required fields are individually
// valid. Corrupting any single required field must make Valid() return an error.
func TestRFDA7c_WorkspaceValid_RequiresAllFields(t *testing.T) {
	t.Parallel()

	// A workspace with every required field populated must pass Valid().
	ws := wsRecordFixtureValid(t)
	if err := ws.Valid(); err != nil {
		t.Errorf("full workspace: Valid() = %v; want nil", err)
	}

	// Each corruption case must fail Valid() independently.
	cases := []struct {
		name    string
		corrupt func(*Workspace)
	}{
		{"empty WorkspaceID", func(ws *Workspace) { ws.WorkspaceID = "" }},
		{"zero RunID", func(ws *Workspace) { ws.RunID = core.RunID{} }},
		{"empty Repository", func(ws *Workspace) { ws.Repository = "" }},
		{"empty ParentCommit", func(ws *Workspace) { ws.ParentCommit = "" }},
		{"empty BranchName", func(ws *Workspace) { ws.BranchName = "" }},
		{"empty Path", func(ws *Workspace) { ws.Path = "" }},
		{"invalid State", func(ws *Workspace) { ws.State = core.WorkspaceState("bad") }},
		{"invalid InterruptState", func(ws *Workspace) { ws.InterruptState = core.InterruptState("bad") }},
		{"zero SchemaVersion", func(ws *Workspace) { ws.SchemaVersion = 0 }},
		{"negative SchemaVersion", func(ws *Workspace) { ws.SchemaVersion = -1 }},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ws := wsRecordFixtureValid(t) // fresh copy per sub-test
			tc.corrupt(ws)
			if err := ws.Valid(); err == nil {
				t.Errorf("workspace with %s: Valid() = nil; want error", tc.name)
			}
		})
	}
}

// TestRFDA7c_WorkspaceValid_OptionalFieldsNilIsAccepted verifies that nil
// optional fields (BeadID, ImplementerHandlerRef) do not cause Valid() to fail.
func TestRFDA7c_WorkspaceValid_OptionalFieldsNilIsAccepted(t *testing.T) {
	t.Parallel()

	ws := wsRecordFixtureValid(t)
	ws.BeadID = nil
	ws.ImplementerHandlerRef = nil
	if err := ws.Valid(); err != nil {
		t.Errorf("nil optional fields: Valid() = %v; want nil", err)
	}
}

// repr returns a printable representation of an empty string (for sub-test names).
func repr(s string) string {
	if s == "" {
		return "(empty)"
	}
	return s
}
