package handler_test

// terminalclassify_characterization_test.go — characterization of the PROBE
// step of the shared launch → dispatch → wait → probe → teardown sequence:
// MapWaitReturnToTerminalEvent, the function that turns "the agent process
// ended" into "the agent did / did not do the work".
//
// It has exactly ONE production caller today: workloop.go's single mode. The
// other four dispatch sites — reviewloop.go's implementer and reviewer phases,
// dot_cascade_core.go's per-node dispatch, and dot_gate.go — call the same wait
// primitive but DISCARD its outcome (`_, implEI := …`) and decide success by
// probing whether the worktree HEAD moved instead. So an agent that reported
// WORK_COMPLETE or FAILURE_SIGNAL through its Stop hook has that report thrown
// away everywhere except single mode. That asymmetry is a Phase-3 finding in
// its own right; it is recorded here so nobody reads this file as evidence that
// the probe leg is already shared.
//
// Before this file it had no tests, so its three branches and their defaults
// were unprotected.
//
// The tests pin the CLASSIFICATION — the observable answer, given the three
// inputs the wait step produces. They do not pin how the branches are laid out,
// so a decomposition that keeps the same answers keeps them green.
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-020.

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func outcome(kind, subReason, class string) *handler.ExportedOutcomeEmittedPayload {
	return &handler.ExportedOutcomeEmittedPayload{
		Kind: kind, SubReason: subReason, SuggestedClass: class,
	}
}

// TestMapWaitReturn_StopHookBeatsTheExitCode is the headline of the whole probe
// step (OQ2): a Stop hook that reported the work complete makes the run a
// SUCCESS even though the process exited non-zero. Claude routinely exits
// dirty after doing the work; deriving success from the exit code instead
// would reopen every one of those beads.
func TestMapWaitReturn_StopHookBeatsTheExitCode(t *testing.T) {
	for _, kind := range []string{"WORK_COMPLETE", "REVIEWER_VERDICT"} {
		t.Run(kind+" with a dirty exit", func(t *testing.T) {
			got := handler.MapWaitReturnToTerminalEvent(
				"sess-1", 1, errors.New("exit status 1"), outcome(kind, "", ""))

			if got.Type != handlercontract.ProgressMsgTypeAgentCompleted {
				t.Fatalf("type = %q, want agent_completed — the Stop hook must win over the exit code", got.Type)
			}
			if got.ExitCode != 1 {
				t.Errorf("ExitCode = %d, want the real exit code carried through", got.ExitCode)
			}
			if got.Class != "" || got.SubReason != "" {
				t.Errorf("a completed run carried failure detail: class=%q sub_reason=%q", got.Class, got.SubReason)
			}
		})
	}
}

// TestMapWaitReturn_FailureSignalCarriesItsOwnDetail pins that an agent which
// reported its OWN failure keeps the class and sub-reason it chose. Those two
// fields are what decide transient-vs-structural downstream, so overwriting
// them here would erase the agent's diagnosis.
func TestMapWaitReturn_FailureSignalCarriesItsOwnDetail(t *testing.T) {
	got := handler.MapWaitReturnToTerminalEvent(
		"sess-1", 0, nil, outcome("FAILURE_SIGNAL", "tests_failed", "transient"))

	if got.Type != handlercontract.ProgressMsgTypeAgentFailed {
		t.Fatalf("type = %q, want agent_failed", got.Type)
	}
	if got.Class != "transient" {
		t.Errorf("class = %q, want the agent's own suggested class", got.Class)
	}
	if got.SubReason != "tests_failed" {
		t.Errorf("sub_reason = %q, want the agent's own sub-reason", got.SubReason)
	}
}

// TestMapWaitReturn_FailureSignalDefaultsFailClosed pins the defaults for an
// under-specified failure signal: an agent that says only "I failed" is treated
// as a STRUCTURAL failure, not a transient one. Defaulting to transient would
// make an unspecified failure retryable and could spin a broken bead
// indefinitely.
func TestMapWaitReturn_FailureSignalDefaultsFailClosed(t *testing.T) {
	got := handler.MapWaitReturnToTerminalEvent(
		"sess-1", 0, nil, outcome("FAILURE_SIGNAL", "", ""))

	if got.Type != handlercontract.ProgressMsgTypeAgentFailed {
		t.Fatalf("type = %q, want agent_failed", got.Type)
	}
	if got.Class != "structural" {
		t.Errorf("class = %q, want structural — an unspecified failure must not be retryable", got.Class)
	}
	if got.SubReason != "claude_failure" {
		t.Errorf("sub_reason = %q, want a named placeholder rather than an empty string", got.SubReason)
	}
}

// TestMapWaitReturn_NoOutcomeIsAlwaysStructuralFailure pins branch 3: an agent
// that ended without ever reporting an outcome is a structural failure whatever
// its exit code. "Exited quietly" is never success — that is the rule that
// stops a crashed agent from closing its bead.
//
// The sub-reason split is the observable detail the daemon formats into the
// reopen summary: `claude_crashed` when the reap saw a real failure,
// `claude_exit_without_outcome` otherwise.
func TestMapWaitReturn_NoOutcomeIsAlwaysStructuralFailure(t *testing.T) {
	cases := []struct {
		name          string
		exitCode      int
		waitErr       error
		wantSubReason string
	}{
		{"clean exit, nothing reported", 0, nil, "claude_exit_without_outcome"},
		{"non-zero exit with a reap error", 137, errors.New("signal: killed"), "claude_crashed"},
		// Both of the mixed cases fall on the exit_without_outcome side: the
		// split requires a reap error AND a non-zero code together.
		{"reap error but a zero exit code", 0, errors.New("exit status 0?"), "claude_exit_without_outcome"},
		{"non-zero exit code but no reap error", 3, nil, "claude_exit_without_outcome"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := handler.MapWaitReturnToTerminalEvent("sess-1", tc.exitCode, tc.waitErr, nil)

			if got.Type != handlercontract.ProgressMsgTypeAgentFailed {
				t.Fatalf("type = %q, want agent_failed — a silent exit is never success", got.Type)
			}
			if got.Class != "structural" {
				t.Errorf("class = %q, want structural", got.Class)
			}
			if got.SubReason != tc.wantSubReason {
				t.Errorf("sub_reason = %q, want %q", got.SubReason, tc.wantSubReason)
			}
			if got.ExitCode != tc.exitCode {
				t.Errorf("ExitCode = %d, want %d", got.ExitCode, tc.exitCode)
			}
		})
	}
}

// TestMapWaitReturn_UnrecognisedOutcomeKindFailsClosed pins the fail-closed
// posture for an outcome the daemon does not understand: it is treated as if no
// outcome had arrived at all, i.e. a structural failure, rather than being
// optimistically accepted. A future Stop-hook vocabulary addition therefore
// reopens beads until the daemon is taught the new kind — loud, not silent.
func TestMapWaitReturn_UnrecognisedOutcomeKindFailsClosed(t *testing.T) {
	got := handler.MapWaitReturnToTerminalEvent(
		"sess-1", 0, nil, outcome("SOMETHING_NEW", "whatever", "transient"))

	if got.Type != handlercontract.ProgressMsgTypeAgentFailed {
		t.Fatalf("type = %q, want agent_failed for an unrecognised outcome kind", got.Type)
	}
	if got.Class != "structural" {
		t.Errorf("class = %q, want structural", got.Class)
	}
	if got.SubReason != "claude_exit_without_outcome" {
		t.Errorf("sub_reason = %q, want the no-outcome sub-reason — the unknown payload's own detail must not leak through", got.SubReason)
	}
}

// TestMapWaitReturn_SessionIDIsCarriedOnEveryBranch pins that the session
// identifier survives every classification, so the emitted terminal is
// attributable no matter which branch produced it.
func TestMapWaitReturn_SessionIDIsCarriedOnEveryBranch(t *testing.T) {
	const sess = "sess-attribution"
	branches := map[string]*handler.ExportedOutcomeEmittedPayload{
		"completed":     outcome("WORK_COMPLETE", "", ""),
		"failed_signal": outcome("FAILURE_SIGNAL", "x", "structural"),
		"no_outcome":    nil,
	}
	for name, o := range branches {
		t.Run(name, func(t *testing.T) {
			if got := handler.MapWaitReturnToTerminalEvent(sess, 0, nil, o); got.SessionID != sess {
				t.Errorf("SessionID = %q, want %q", got.SessionID, sess)
			}
		})
	}
}
