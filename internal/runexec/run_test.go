package runexec

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// run_test.go — L0 per-transition + terminal-spine tests for the Run machine
// (RSM-007/008/009, RSM-020..022, RSM-003 terminal exclusivity). Pure and zero-token: the reactor
// reads no clock and mints no ids. The headline test proves the 6 pre-change
// close-ladder variants collapse onto ONE tail with byte-identical strings.

func stdRunCfg() RunConfig {
	return RunConfig{
		Mode:                 "single",
		MaxMergeAttempts:     3,
		EmitOutcome:          true,
		CloseSummary:         "implementer completed: merged to main",
		BrUnavailableSummary: "implementer completed: merged (br unavailable)",
		NoMergeCloseSummary:  "implementer completed: subsumed",
		ReopenReason:         "run failed; reopened for retry",
	}
}

func runKinds(actions []Action) []ActionKind { return kinds(actions) }

func TestRun_HappyCloseSpine(t *testing.T) {
	m := NewRun(stdRunCfg())
	if got := runKinds(m.Step(Event{Kind: EvStartRun, Mode: "single", At: at(1)})); !eqKinds(got, []ActionKind{ActCreateWorktree}) {
		t.Fatalf("start: %v", got)
	}
	if got := runKinds(m.Step(Event{Kind: EvProvisioned, At: at(2)})); !eqKinds(got, []ActionKind{ActEmit}) {
		t.Fatalf("provisioned: %v", got)
	}
	if m.State().Phase != RunDispatching {
		t.Fatalf("phase %s", m.State().Phase)
	}
	// Single-mode dispatch terminal → Guarding → guards pass → Gating.
	if got := runKinds(m.Step(Event{Kind: EvAgentCompleted, At: at(3)})); !eqKinds(got, []ActionKind{ActCheckEscape}) {
		t.Fatalf("agentcompleted: %v", got)
	}
	if m.State().Phase != RunGuarding {
		t.Fatalf("phase %s", m.State().Phase)
	}
	if got := runKinds(m.Step(Event{Kind: EvGuardsPassed, At: at(4)})); !eqKinds(got, []ActionKind{ActRunGate}) {
		t.Fatalf("guardspassed: %v", got)
	}
	if got := runKinds(m.Step(Event{Kind: EvGatePassed, At: at(5)})); !eqKinds(got, []ActionKind{ActPrepareMerge}) {
		t.Fatalf("gatepassed: %v", got)
	}
	// Merge success → close ladder: outcome_emitted approved + close_bead.
	got := m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(6)})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActCloseBead}) {
		t.Fatalf("merge success: %v", kinds(got))
	}
	if m.State().Phase != RunFinalizing {
		t.Fatalf("phase %s", m.State().Phase)
	}
	// Close result → Done{closed}, run terminal once.
	got = m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(7)})
	if !eqKinds(kinds(got), []ActionKind{ActEmitRunTerminal}) {
		t.Fatalf("closeresult: %v", kinds(got))
	}
	if m.State().Phase != RunDone || m.State().DoneOutcome != "closed" || !m.State().Success {
		t.Fatalf("done: %+v", m.State())
	}
	if got[0].Success != true || got[0].Summary != stdRunCfg().CloseSummary {
		t.Fatalf("terminal summary: %+v", got[0])
	}
}

func TestRun_GuardingOnlySingleShot(t *testing.T) {
	// RSM-008: review-loop / DOT (EvModeOutcome success) MUST NOT enter Guarding;
	// they go straight to Gating.
	m := NewRun(stdRunCfg())
	m.Step(Event{Kind: EvStartRun, Mode: "review_loop", At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)})
	if m.State().Phase != RunGating {
		t.Fatalf("review-loop success must skip Guarding, got %s", m.State().Phase)
	}
}

func TestRun_PreLaunchFailureReopenSpine(t *testing.T) {
	// RSM-009: every pre-launch failure routes through the reopen spine.
	for _, entry := range []RunPhase{RunResolving, RunProvisioning} {
		m := NewRun(stdRunCfg())
		if entry == RunProvisioning {
			m.Step(Event{Kind: EvStartRun, At: at(1)})
		}
		got := m.Step(Event{Kind: EvProvisionFailed, Reason: "config", At: at(2)})
		if !eqKinds(kinds(got), []ActionKind{ActReopenBead, ActEmitRunTerminal}) {
			t.Fatalf("%s reopen: %v", entry, kinds(got))
		}
		if m.State().Phase != RunDone || m.State().DoneOutcome != "reopened" || m.State().Success {
			t.Fatalf("%s done: %+v", entry, m.State())
		}
	}
}

func TestRun_EscapeReopenEmitsEscape(t *testing.T) {
	m := NewRun(stdRunCfg())
	m.Step(Event{Kind: EvStartRun, At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(Event{Kind: EvAgentCompleted, At: at(3)})
	got := m.Step(Event{Kind: EvEscapeDetected, Reason: "escaped", At: at(4)})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("escape: %v", kinds(got))
	}
	if got[0].Type != core.EventTypeImplementerEscapedWorktree {
		t.Fatalf("escape emit type: %s", got[0].Type)
	}
}

func TestRun_MergeRetryThenExhaustedReopen(t *testing.T) {
	cfg := stdRunCfg()
	cfg.MaxMergeAttempts = 2
	cfg.ReAmendTrailer = true
	m := NewRun(cfg)
	m.Step(Event{Kind: EvStartRun, At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(Event{Kind: EvAgentCompleted, At: at(3)})
	m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
	m.Step(Event{Kind: EvGatePassed, At: at(5)})
	// Retryable, attempt 0+1 < 2 → re-prepare + re-amend.
	got := m.Step(Event{Kind: EvMergeResult, Merge: MergeRetryable, MergeReason: "conflict", At: at(6)})
	if !eqKinds(kinds(got), []ActionKind{ActPrepareMerge, ActReAmendTrailer}) {
		t.Fatalf("retry: %v", kinds(got))
	}
	if m.State().Phase != RunMerging || m.State().MergeAttempt != 1 {
		t.Fatalf("retry state: %+v", m.State())
	}
	// Retryable again, attempt 1+1 == 2 → exhausted → rejected + reopen spine.
	got = m.Step(Event{Kind: EvMergeResult, Merge: MergeRetryable, MergeReason: "conflict", At: at(7)})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("exhausted: %v", kinds(got))
	}
	if got[0].Detail != "rejected" {
		t.Fatalf("exhausted outcome detail: %s", got[0].Detail)
	}
	if m.State().DoneOutcome != "reopened" {
		t.Fatalf("done: %+v", m.State())
	}
}

func TestRun_DOTAlreadyApprovedCarveOut(t *testing.T) {
	// RF :4138 — a fatal merge with already-approved-on-main closes, not reopens.
	// RT9 parity fix: the pre-RT9 DOT carve-out falls through to the SAME
	// approved close ladder as a merge success (pre-RT9 workloop.go :4206–:4217
	// skips only the reopen; outcome_emitted=approved is still emitted), so the
	// carve-out row emits the approved outcome per cfg.EmitOutcome. The original
	// RT6 expectation ([close_bead] only) contradicted the production stream.
	cfg := stdRunCfg()
	m := NewRun(cfg)
	m.Step(Event{Kind: EvStartRun, Mode: "dot", At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)})
	m.Step(Event{Kind: EvGatePassed, At: at(4)})
	got := m.Step(Event{Kind: EvMergeResult, Merge: MergeFatal, AlreadyApprovedOnMain: true, At: at(5)})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActCloseBead}) {
		t.Fatalf("carveout: %v", kinds(got))
	}
	if got[0].Type != core.EventTypeOutcomeEmitted || got[0].Detail != "approved" {
		t.Fatalf("carveout outcome: %+v", got[0])
	}
	if m.State().Phase != RunFinalizing || m.State().FinalizeMode != FinalizeClose {
		t.Fatalf("carveout state: %+v", m.State())
	}
}

func TestRun_SubsumedNoMergeClose(t *testing.T) {
	m := NewRun(stdRunCfg())
	m.Step(Event{Kind: EvStartRun, Mode: "review_loop", At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	got := m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSubsumed, At: at(3)})
	// No-merge close: no outcome_emitted, just close_bead with the no-merge summary.
	if !eqKinds(kinds(got), []ActionKind{ActCloseBead}) {
		t.Fatalf("subsumed: %v", kinds(got))
	}
	if got[0].Summary != stdRunCfg().NoMergeCloseSummary {
		t.Fatalf("subsumed summary: %s", got[0].Summary)
	}
}

func TestRun_ShutdownDrainEdge(t *testing.T) {
	// RSM-021: the shutdown-drain terminal edge submits directly (no gate).
	m := NewRun(stdRunCfg())
	m.Step(Event{Kind: EvStartRun, At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	got := m.Step(Event{Kind: EvShutdownDrain, WorktreeAheadSHA: "deadbeef", At: at(3)})
	if !eqKinds(kinds(got), []ActionKind{ActSubmitMerge}) {
		t.Fatalf("drain: %v", kinds(got))
	}
	if !m.State().Draining || m.State().Phase != RunMerging {
		t.Fatalf("drain state: %+v", m.State())
	}
}

func TestRun_BrUnavailableTransientString(t *testing.T) {
	m := NewRun(stdRunCfg())
	m.Step(Event{Kind: EvStartRun, At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(Event{Kind: EvAgentCompleted, At: at(3)})
	m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
	m.Step(Event{Kind: EvGatePassed, At: at(5)})
	m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(6)})
	got := m.Step(Event{Kind: EvCloseResult, Close: CloseBrUnavailable, At: at(7)})
	if got[0].Summary != stdRunCfg().BrUnavailableSummary {
		t.Fatalf("br_unavailable transient string not preserved: %s", got[0].Summary)
	}
	if m.State().DoneOutcome != "closed" {
		t.Fatalf("done: %+v", m.State())
	}
}

// ─── Terminal-spine unification: the four terminal-entry events → ONE spine ──

// driveToClose runs a Run to a merge-success close via the given dispatch-
// terminal event, returning the close-tail actions (outcome_emitted + close_bead
// + emit_run_terminal). All six variants MUST produce byte-identical strings
// (RSM-020: the ONE close ladder). `mode` selects the upstream fork; single-mode
// terminals pass through Guarding, review-loop/DOT skip it.
func driveToClose(t *testing.T, cfg RunConfig, mode string, dispatchTerminal Event) []Action {
	t.Helper()
	m := NewRun(cfg)
	m.Step(Event{Kind: EvStartRun, Mode: mode, At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(dispatchTerminal)
	// Single-shot terminals (agent-completed, clean-exit) land in Guarding.
	if m.State().Phase == RunGuarding {
		m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
	}
	m.Step(Event{Kind: EvGatePassed, At: at(5)})
	var tail []Action
	tail = append(tail, m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(6)})...)
	tail = append(tail, m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(7)})...)
	return tail
}

// TestRun_CloseLadderByteIdentical is the RT6 acceptance headline: the
// pre-change close-ladder variants (review-loop, DOT, single-shot agent-
// completed, exit-0 clean-exit, and the noChange merge) all map onto ONE tail
// with BYTE-IDENTICAL strings. The budget-needs-attention close is a DISTINCT
// ladder (rejected outcome + needs-attention close + run_failed; see
// TestRun_BudgetNeedsAttentionLadder) — the pre-RT9 production stream, not the
// approved ladder the original RT6 variant assumed.
func TestRun_CloseLadderByteIdentical(t *testing.T) {
	cfg := stdRunCfg() // identical summary/reason data across all variants

	variants := map[string][]Action{
		"review_loop":      driveToClose(t, cfg, "review_loop", Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)}),
		"dot":              driveToClose(t, cfg, "dot", Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)}),
		"single_completed": driveToClose(t, cfg, "single", Event{Kind: EvAgentCompleted, At: at(3)}),
		"single_exit0":     driveToClose(t, cfg, "single", Event{Kind: EvCleanExit, ExitCode: 0, At: at(3)}),
	}
	// noChange merge shares the merge close tail.
	noChange := func() []Action {
		m := NewRun(cfg)
		m.Step(Event{Kind: EvStartRun, Mode: "single", At: at(1)})
		m.Step(Event{Kind: EvProvisioned, At: at(2)})
		m.Step(Event{Kind: EvAgentCompleted, At: at(3)})
		m.Step(Event{Kind: EvGuardsPassed, At: at(4)})
		m.Step(Event{Kind: EvGatePassed, At: at(5)})
		var tail []Action
		tail = append(tail, m.Step(Event{Kind: EvMergeResult, Merge: MergeNoChange, At: at(6)})...)
		tail = append(tail, m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(7)})...)
		return tail
	}()
	variants["merge_no_change"] = noChange

	// Reference: the close tail's exact strings (outcome_emitted approved →
	// close_bead{summary} → emit_run_terminal{success, summary}).
	want := []Action{
		{Kind: ActEmit, Type: core.EventTypeOutcomeEmitted, Detail: "approved"},
		{Kind: ActCloseBead, Summary: cfg.CloseSummary},
		{Kind: ActEmitRunTerminal, Success: true, Summary: cfg.CloseSummary},
	}
	for name, got := range variants {
		if len(got) != len(want) {
			t.Fatalf("variant %s: len %d != %d (%v)", name, len(got), len(want), kinds(got))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("variant %s action[%d] not byte-identical:\n got=%+v\nwant=%+v", name, i, got[i], want[i])
			}
		}
	}
}

// ─── Run-machine property tests (pure, zero-token) ──────────────────────────

var allRunPhases = []RunPhase{
	RunResolving, RunProvisioning, RunDispatching, RunGuarding, RunGating,
	RunMerging, RunFinalizing, RunDone,
}

var allRunEventKinds = []EventKind{
	EvStartRun, EvProvisioned, EvProvisionFailed, EvModeOutcome, EvAgentCompleted,
	EvCleanExit, EvEscapeDetected, EvNoCommitGuardReopen, EvGuardsPassed,
	EvGatePassed, EvGateFailed, EvMergeResult, EvCloseResult, EvShutdownDrain,
	EvTimerFired,
}

// TestRun_TotalityNoPanic: Step is total over all (phase, event) pairs (RSM-003).
func TestRun_TotalityNoPanic(t *testing.T) {
	for _, ph := range allRunPhases {
		for _, ek := range allRunEventKinds {
			m := &Run{cfg: stdRunCfg(), state: RunState{Phase: ph}}
			_ = m.Step(Event{Kind: ek, At: at(1)})
		}
	}
}

// TestRun_DoneTerminalNoOutgoing: Done has no outgoing edges (RSM-003; the four
// terminal-entry events all converge on Done and it is absorbing).
func TestRun_DoneTerminalNoOutgoing(t *testing.T) {
	for _, ek := range allRunEventKinds {
		m := &Run{cfg: stdRunCfg(), state: RunState{Phase: RunDone, DoneOutcome: "closed", Success: true}}
		got := m.Step(Event{Kind: ek, At: at(1)})
		if len(got) != 0 {
			t.Fatalf("Done emitted actions on %s: %v", ek, kinds(got))
		}
		if m.State().Phase != RunDone {
			t.Fatalf("Done changed phase on %s -> %s", ek, m.State().Phase)
		}
	}
}

// TestRun_AllTerminalEntriesReachOneDone: the four terminal-entry events
// (RSM-020/022 spine unification; RSM-003 exactly-one-terminal) each land the
// machine in exactly one Done state, and every reopen path lands Done{reopened},
// every close path Done{closed}.
func TestRun_AllTerminalEntriesReachOneDone(t *testing.T) {
	cfg := stdRunCfg()
	// close entries.
	for _, v := range []Event{
		{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)},
		{Kind: EvAgentCompleted, At: at(3)},
		{Kind: EvCleanExit, At: at(3)},
	} {
		tail := driveToClose(t, cfg, "single", v)
		if len(tail) == 0 || tail[len(tail)-1].Kind != ActEmitRunTerminal {
			t.Fatalf("entry %s did not reach the terminal spine", v.Kind)
		}
	}
	// reopen entry (gate failure).
	m := NewRun(cfg)
	m.Step(Event{Kind: EvStartRun, Mode: "review_loop", At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)})
	m.Step(Event{Kind: EvGateFailed, At: at(4)})
	if m.State().Phase != RunDone || m.State().DoneOutcome != "reopened" {
		t.Fatalf("gate-fail reopen: %+v", m.State())
	}
}

// ─── RT9 additions: the mode-outcome latches, the attention ladder, drain ────

// TestRun_BudgetNeedsAttentionLadder: the review-loop budget-exhausted close is
// its own ladder (pre-RT9 workloop.go hk-c1ah6 block): outcome_emitted=rejected
// → close_bead{needs_attention} → run_failed with the exhausted summary on
// EVERY close result; a hard close error reopens first (hk-hypbi: transient
// BrUnavailable does NOT reopen — the bead stays in_progress for BI-031).
func TestRun_BudgetNeedsAttentionLadder(t *testing.T) {
	const exhausted = "review_loop_budget_exhausted (max=3 failures): summary"
	drive := func() *Run {
		m := NewRun(stdRunCfg())
		m.Step(Event{Kind: EvStartRun, Mode: "review_loop", At: at(1)})
		m.Step(Event{Kind: EvProvisioned, At: at(2)})
		got := m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeBudget, NeedsAttention: true, Detail: exhausted, At: at(3)})
		if !eqKinds(kinds(got), []ActionKind{ActEmit, ActCloseBead}) {
			t.Fatalf("attention entry: %v", kinds(got))
		}
		if got[0].Detail != "rejected" {
			t.Fatalf("attention outcome: %+v", got[0])
		}
		if !got[1].NeedsAttention || got[1].Summary != exhausted {
			t.Fatalf("attention close: %+v", got[1])
		}
		return m
	}

	// Close success → bead closed, but the run terminal is still run_failed.
	m := drive()
	got := m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(4)})
	if !eqKinds(kinds(got), []ActionKind{ActEmitRunTerminal}) || got[0].Success || got[0].Summary != exhausted {
		t.Fatalf("attention closed: %v %+v", kinds(got), got)
	}
	// Transient BrUnavailable → NO reopen (BI-031 recovery), run_failed.
	m = drive()
	got = m.Step(Event{Kind: EvCloseResult, Close: CloseBrUnavailable, At: at(4)})
	if !eqKinds(kinds(got), []ActionKind{ActEmitRunTerminal}) || got[0].Success || got[0].Summary != exhausted {
		t.Fatalf("attention transient: %v %+v", kinds(got), got)
	}
	// Hard close error → reopen with the exhausted summary, then run_failed.
	m = drive()
	got = m.Step(Event{Kind: EvCloseResult, Close: CloseError, Detail: "close-error: boom", At: at(4)})
	if !eqKinds(kinds(got), []ActionKind{ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("attention close-error: %v", kinds(got))
	}
	if got[0].Reason != exhausted || got[1].Success || got[1].Summary != exhausted {
		t.Fatalf("attention close-error strings: %+v", got)
	}
}

// TestRun_ModeOutcomeLatchesLabelAndSummary: a review-loop/DOT success latches
// the path label (merge-failure string composition) + the dynamic close summary
// (RT9); the BrUnavailable transient comes from the config override, NOT the
// label composition ("close-transient-merged (review-loop APPROVE)" vs
// "(review-loop)").
func TestRun_ModeOutcomeLatchesLabelAndSummary(t *testing.T) {
	cfg := stdRunCfg()
	cfg.BrUnavailableSummary = "close-transient-merged (review-loop APPROVE)"
	drive := func() *Run {
		m := NewRun(cfg)
		m.Step(Event{Kind: EvStartRun, Mode: "review_loop", At: at(1)})
		m.Step(Event{Kind: EvProvisioned, At: at(2)})
		m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, PathLabel: "review-loop", Detail: "APPROVE at iteration 2", At: at(3)})
		m.Step(Event{Kind: EvGatePassed, At: at(4)})
		return m
	}
	// Merge success → close → terminal carries the latched dynamic summary.
	m := drive()
	m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(5)})
	got := m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(6)})
	if got[0].Summary != "APPROVE at iteration 2" || !got[0].Success {
		t.Fatalf("latched close summary: %+v", got[0])
	}
	// BrUnavailable → the config transient wins over label composition.
	m = drive()
	m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(5)})
	got = m.Step(Event{Kind: EvCloseResult, Close: CloseBrUnavailable, At: at(6)})
	if got[0].Summary != "close-transient-merged (review-loop APPROVE)" {
		t.Fatalf("mode transient: %+v", got[0])
	}
	// Merge fatal → the label-parameterized failure strings.
	m = drive()
	got = m.Step(Event{Kind: EvMergeResult, Merge: MergeFatal, MergeStage: MergeStageMerge, MergeReason: "non_ff", At: at(5)})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActReopenBead, ActEmitRunTerminal}) {
		t.Fatalf("mode merge fatal: %v", kinds(got))
	}
	if got[1].Reason != "merge-to-main failed: non_ff" || got[2].Summary != "merge-failed (review-loop): non_ff" {
		t.Fatalf("mode merge fatal strings: %+v", got)
	}
	// Code-sync fatal → the code-sync label strings.
	m = drive()
	got = m.Step(Event{Kind: EvMergeResult, Merge: MergeFatal, MergeStage: MergeStageCodeSync, MergeReason: "fetch failed", At: at(5)})
	if got[1].Reason != "code-sync failed (review-loop): fetch failed" ||
		got[2].Summary != "code-sync-failed (review-loop): fetch failed" {
		t.Fatalf("mode code-sync strings: %+v", got)
	}
}

// TestRun_DOTSubsumedEventLabelTransient: the DOT noChange-subsumed close
// carries its own label on the event so the transient composes to
// "close-transient-merged (dot noChange-subsumed)" even when the mode-level
// config transient ("…(dot success)") is set (RT9).
func TestRun_DOTSubsumedEventLabelTransient(t *testing.T) {
	cfg := stdRunCfg()
	cfg.BrUnavailableSummary = "close-transient-merged (dot success)"
	m := NewRun(cfg)
	m.Step(Event{Kind: EvStartRun, Mode: "dot", At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	got := m.Step(Event{
		Kind: EvModeOutcome, ModeOutcome: ModeSubsumed, EmitOutcome: true,
		PathLabel: "dot noChange-subsumed", Detail: "noChange-subsumed: bead found in main", At: at(3),
	})
	if !eqKinds(kinds(got), []ActionKind{ActEmit, ActCloseBead}) || got[0].Detail != "approved" {
		t.Fatalf("dot subsumed entry: %v %+v", kinds(got), got)
	}
	term := m.Step(Event{Kind: EvCloseResult, Close: CloseBrUnavailable, At: at(4)})
	if term[0].Summary != "close-transient-merged (dot noChange-subsumed)" || !term[0].Success {
		t.Fatalf("dot subsumed transient: %+v", term[0])
	}
}

// TestRun_ShutdownDrainLadders: the drain edge's three outcomes (RSM-021,
// pre-RT9 hk-dnrg block): no commit → reopen with the requeue reason and NO
// run terminal; merged → the drain close ladder (no outcome emission, the
// drain summaries); merge failure → the same requeue reopen, no terminal.
func TestRun_ShutdownDrainLadders(t *testing.T) {
	drive := func() *Run {
		m := NewRun(stdRunCfg())
		m.Step(Event{Kind: EvStartRun, Mode: "single", At: at(1)})
		m.Step(Event{Kind: EvProvisioned, At: at(2)})
		return m
	}
	// No commit: straight requeue reopen, no terminal.
	m := drive()
	got := m.Step(Event{Kind: EvShutdownDrain, At: at(3)})
	if !eqKinds(kinds(got), []ActionKind{ActReopenBead}) || got[0].Reason != "context_cancelled: daemon shutdown, requeue pending" {
		t.Fatalf("drain no-commit: %v %+v", kinds(got), got)
	}
	if m.State().Phase != RunDone || m.State().DoneOutcome != "reopened" {
		t.Fatalf("drain no-commit state: %+v", m.State())
	}
	// Committed + merge success → drain close (no outcome emission).
	m = drive()
	m.Step(Event{Kind: EvShutdownDrain, WorktreeAheadSHA: "deadbeef", At: at(3)})
	got = m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(4)})
	if !eqKinds(kinds(got), []ActionKind{ActCloseBead}) || got[0].Summary != "shutdown-drain: committed work merged" {
		t.Fatalf("drain close entry: %v %+v", kinds(got), got)
	}
	term := m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(5)})
	if term[0].Summary != "shutdown-drain: committed work merged" || !term[0].Success {
		t.Fatalf("drain closed terminal: %+v", term[0])
	}
	// Committed + merge success + transient close → the drain transient string.
	m = drive()
	m.Step(Event{Kind: EvShutdownDrain, WorktreeAheadSHA: "deadbeef", At: at(3)})
	m.Step(Event{Kind: EvMergeResult, Merge: MergeSuccess, At: at(4)})
	term = m.Step(Event{Kind: EvCloseResult, Close: CloseBrUnavailable, At: at(5)})
	if term[0].Summary != "close-transient-merged (shutdown-drain)" || !term[0].Success {
		t.Fatalf("drain transient terminal: %+v", term[0])
	}
	// Committed + merge failure → requeue reopen, no terminal.
	m = drive()
	m.Step(Event{Kind: EvShutdownDrain, WorktreeAheadSHA: "deadbeef", At: at(3)})
	got = m.Step(Event{Kind: EvMergeResult, Merge: MergeFatal, MergeReason: "non_ff", At: at(4)})
	if !eqKinds(kinds(got), []ActionKind{ActReopenBead}) || got[0].Reason != "context_cancelled: daemon shutdown, requeue pending" {
		t.Fatalf("drain merge-fail: %v %+v", kinds(got), got)
	}
}
