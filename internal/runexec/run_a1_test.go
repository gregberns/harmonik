package runexec

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// run_a1_test.go — Amendment A1 payload plumbing (RSM-031..035): event-sourced
// reopen/terminal strings, the single-mode path-label latch, the staged merge
// failure strings, the rejected-prefix gate-failure pairing, and the
// subsumed-approved close. Each row mirrors the RT7 design-note string table
// (.kerf/works/2026-07-14-run-state-machine/04-design/rt7-single-mode-failure-mapping.md §2).

// driveToDispatching brings a fresh Run to Dispatching.
func driveToDispatching(t *testing.T, cfg RunConfig) *Run {
	t.Helper()
	m := NewRun(cfg)
	m.Step(Event{Kind: EvStartRun, Mode: "single", At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	if m.State().Phase != RunDispatching {
		t.Fatalf("setup: phase %s", m.State().Phase)
	}
	return m
}

func TestRunA1_ProvisionFailedCarriesStrings(t *testing.T) {
	// RSM-032: rows 0a–0c — reason and summary ride the event; distinct strings
	// (the worktree-create case) are both preserved.
	m := NewRun(stdRunCfg())
	got := m.Step(Event{
		Kind: EvProvisionFailed, At: at(1),
		Reason: "create worktree failed: boom",
		Detail: "worktree_create_failed: boom",
	})
	if !eqKinds(kinds(got), []ActionKind{ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("kinds: %v", kinds(got))
	}
	if got[0].Reason != "create worktree failed: boom" {
		t.Fatalf("reopen reason: %q", got[0].Reason)
	}
	if got[1].Summary != "worktree_create_failed: boom" || got[1].Success {
		t.Fatalf("terminal: %+v", got[1])
	}
}

func TestRunA1_ProvisionFailedEmptyFallsBackToConfig(t *testing.T) {
	m := NewRun(stdRunCfg())
	got := m.Step(Event{Kind: EvProvisionFailed, At: at(1)})
	if got[0].Reason != stdRunCfg().ReopenReason || got[1].Summary != stdRunCfg().ReopenReason {
		t.Fatalf("fallback: %+v", got)
	}
}

func TestRunA1_ModeFailureCarriesStrings(t *testing.T) {
	// RSM-031: rows 1/1b/14 — a failed single-mode Dispatch is a mode failure
	// whose strings ride the event.
	m := driveToDispatching(t, stdRunCfg())
	got := m.Step(Event{
		Kind: EvModeOutcome, ModeOutcome: ModeFailure, At: at(3),
		Reason: "noChange-timeout",
		Detail: "noChange-timeout: no commit in commitPollTimeout window",
	})
	if !eqKinds(kinds(got), []ActionKind{ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("kinds: %v", kinds(got))
	}
	if got[0].Reason != "noChange-timeout" ||
		got[1].Summary != "noChange-timeout: no commit in commitPollTimeout window" {
		t.Fatalf("strings: %+v", got)
	}
}

func TestRunA1_SubsumedApprovedClose(t *testing.T) {
	// RSM-035: row 13 — subsumed with the emit-approved flag closes with
	// outcome_emitted=approved and the event-carried summary.
	m := driveToDispatching(t, stdRunCfg())
	got := m.Step(Event{
		Kind: EvModeOutcome, ModeOutcome: ModeSubsumed, EmitOutcome: true, At: at(3),
		Detail: "noChange-subsumed: bead found in main",
	})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActCloseBead}) {
		t.Fatalf("kinds: %v", kinds(got))
	}
	if got[0].Type != core.EventTypeOutcomeEmitted || got[0].Detail != "approved" {
		t.Fatalf("approved emit: %+v", got[0])
	}
	term := m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(4)})
	if term[0].Summary != "noChange-subsumed: bead found in main" || !term[0].Success {
		t.Fatalf("terminal: %+v", term[0])
	}
}

func TestRunA1_SubsumedWithoutFlagKeepsRT6Close(t *testing.T) {
	// DOT subsumed path unchanged: no flag → no outcome emission, config summary.
	m := driveToDispatching(t, stdRunCfg())
	got := m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSubsumed, At: at(3)})
	if !eqKinds(kinds(got), []ActionKind{ActCloseBead}) {
		t.Fatalf("kinds: %v", kinds(got))
	}
	if got[0].Summary != stdRunCfg().NoMergeCloseSummary {
		t.Fatalf("summary: %q", got[0].Summary)
	}
}

func TestRunA1_PathLabelLatchAndCloseSummaries(t *testing.T) {
	// RSM-033 + rows 9–11: the latched label parameterizes the close summaries.
	cases := []struct {
		kind      EventKind
		detail    string
		label     string
		transient string
	}{
		{EvAgentCompleted, "agent_completed: stop-hook outcome", "agent_completed", "close-transient-merged (agent_completed)"},
		{EvCleanExit, "auto-close: exit=0", "auto-close", "close-transient-merged (auto-close)"},
	}
	for _, tc := range cases {
		for _, close := range []CloseOutcomeClass{CloseClosed, CloseBrUnavailable} {
			m := driveToDispatching(t, stdRunCfg())
			m.Step(Event{Kind: tc.kind, Detail: tc.detail, At: at(3)})
			if m.State().PathLabel != tc.label {
				t.Fatalf("%s label: %q", tc.kind, m.State().PathLabel)
			}
			m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
			m.Step(Event{Kind: EvGatePassed, At: at(5)})
			m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(6)})
			term := m.Step(Event{Kind: EvCloseResult, Close: close, At: at(7)})
			want := tc.detail
			if close == CloseBrUnavailable {
				want = tc.transient
			}
			if term[0].Summary != want || !term[0].Success {
				t.Fatalf("%s/%s terminal: %+v", tc.kind, close, term[0])
			}
		}
	}
}

func TestRunA1_CloseErrorEventDetailSummary(t *testing.T) {
	// Row 12: the close-error summary is composed shell-side and rides the event.
	m := driveToDispatching(t, stdRunCfg())
	m.Step(Event{Kind: EvAgentCompleted, Detail: "agent_completed: stop-hook outcome", At: at(3)})
	m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
	m.Step(Event{Kind: EvGatePassed, At: at(5)})
	m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(6)})
	term := m.Step(Event{Kind: EvCloseResult, Close: CloseError, Detail: "close-error: br exploded", At: at(7)})
	if term[0].Summary != "close-error: br exploded" || term[0].Success {
		t.Fatalf("terminal: %+v", term[0])
	}
}

func TestRunA1_GateFailedRejectedPrefix(t *testing.T) {
	// RSM-034 + row 4: an event-classified gate failure pairs the rejected
	// emission with the reopen; reason == summary == sgr.reason.
	m := driveToDispatching(t, stdRunCfg())
	m.Step(Event{Kind: EvAgentCompleted, Detail: "agent_completed: stop-hook outcome", At: at(3)})
	m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
	got := m.Step(Event{Kind: EvGateFailed, Reason: "scenario gate RED: TestFoo", At: at(5)})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("kinds: %v", kinds(got))
	}
	if got[0].Type != core.EventTypeOutcomeEmitted || got[0].Detail != "rejected" {
		t.Fatalf("rejected emit: %+v", got[0])
	}
	if got[1].Reason != "scenario gate RED: TestFoo" || got[2].Summary != "scenario gate RED: TestFoo" {
		t.Fatalf("strings: %+v", got)
	}
}

func TestRunA1_GateFailedEmptyReasonKeepsRT6(t *testing.T) {
	m := driveToDispatching(t, stdRunCfg())
	m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)})
	got := m.Step(Event{Kind: EvGateFailed, At: at(4)})
	if !eqKinds(kinds(got), []ActionKind{ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("kinds: %v", kinds(got))
	}
	if got[0].Reason != stdRunCfg().ReopenReason {
		t.Fatalf("fallback: %q", got[0].Reason)
	}
}

func TestRunA1_GuardReasonsRideEvents(t *testing.T) {
	// Rows 2–3: the guard strings ride the events; reason == summary.
	for _, tc := range []struct {
		kind   EventKind
		reason string
	}{
		{EvEscapeDetected, "implementer_escaped_worktree: 2 file(s) dirty in main: a, b"},
		{EvNoCommitGuardReopen, "no_commit_during_implementer: HEAD did not advance past parent abc at iteration 1 exit=0"},
	} {
		m := driveToDispatching(t, stdRunCfg())
		m.Step(Event{Kind: EvAgentCompleted, Detail: "agent_completed: stop-hook outcome", At: at(3)})
		got := m.Step(Event{Kind: tc.kind, Reason: tc.reason, At: at(4)})
		last := got[len(got)-1]
		reopen := got[len(got)-2]
		if reopen.Reason != tc.reason || last.Summary != tc.reason {
			t.Fatalf("%s strings: %+v", tc.kind, got)
		}
	}
}

func TestRunA1_MergeStageStrings(t *testing.T) {
	// RSM-033 staged merge failures, rows 5–8: note the load-bearing byte
	// difference — reopen "code-sync failed" (space) vs summary
	// "code-sync-failed" (hyphen).
	cases := []struct {
		kind        EventKind
		detail      string
		stage       MergeStage
		wantReason  string
		wantSummary string
	}{
		{
			EvAgentCompleted, "agent_completed: stop-hook outcome", MergeStageCodeSync,
			"code-sync failed (agent_completed): push refused",
			"code-sync-failed (agent_completed): push refused",
		},
		{
			EvCleanExit, "auto-close: exit=0", MergeStageCodeSync,
			"code-sync failed (auto-close): push refused",
			"code-sync-failed (auto-close): push refused",
		},
		{
			EvAgentCompleted, "agent_completed: stop-hook outcome", MergeStageMerge,
			"merge-to-main failed: push refused",
			"merge-failed (agent_completed): push refused",
		},
		{
			EvCleanExit, "auto-close: exit=0", MergeStageMerge,
			"merge-to-main failed: push refused",
			"merge-failed (auto-close): push refused",
		},
	}
	for _, tc := range cases {
		m := driveToDispatching(t, stdRunCfg())
		m.Step(Event{Kind: tc.kind, Detail: tc.detail, At: at(3)})
		m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
		m.Step(Event{Kind: EvGatePassed, At: at(5)})
		got := m.Step(Event{
			Kind: EvMergeResult, Merge: MergeFatal, MergeStage: tc.stage,
			MergeReason: "push refused", At: at(6),
		})
		if !eqKinds(kinds(got), []ActionKind{ActEmit, ActReopenBead, ActEmitRunTerminal}) {
			t.Fatalf("kinds: %v", kinds(got))
		}
		if got[1].Reason != tc.wantReason || got[2].Summary != tc.wantSummary {
			t.Fatalf("%s/%s strings: reopen=%q summary=%q", tc.kind, tc.stage, got[1].Reason, got[2].Summary)
		}
	}
}

func TestRunA1_MergeFatalNoLabelKeepsRT6(t *testing.T) {
	// Review-loop/DOT (no latched label) merge failure keeps the config strings.
	m := driveToDispatching(t, stdRunCfg())
	m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)})
	m.Step(Event{Kind: EvGatePassed, At: at(4)})
	got := m.Step(Event{Kind: EvMergeResult, Merge: MergeFatal, MergeReason: "boom", At: at(5)})
	if got[1].Reason != stdRunCfg().ReopenReason || got[2].Summary != stdRunCfg().ReopenReason {
		t.Fatalf("fallback: %+v", got)
	}
}
