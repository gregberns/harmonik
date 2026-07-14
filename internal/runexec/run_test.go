package runexec

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// run_test.go — L0 per-transition + terminal-spine tests for the Run machine
// (RSM-007/008/009, RSM-020..022, RSM-INV-004). Pure and zero-token: the reactor
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
	cfg := stdRunCfg()
	m := NewRun(cfg)
	m.Step(Event{Kind: EvStartRun, Mode: "dot", At: at(1)})
	m.Step(Event{Kind: EvProvisioned, At: at(2)})
	m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)})
	m.Step(Event{Kind: EvGatePassed, At: at(4)})
	got := m.Step(Event{Kind: EvMergeResult, Merge: MergeFatal, AlreadyApprovedOnMain: true, At: at(5)})
	if !eqKinds(kinds(got), []ActionKind{ActCloseBead}) {
		t.Fatalf("carveout: %v", kinds(got))
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

// TestRun_CloseLadderByteIdentical is the RT6 acceptance headline: the six
// pre-change close-ladder variants (review-loop, DOT, single-shot agent-
// completed, exit-0 clean-exit, budget-needs-attention close, and a second
// single-shot entry) all map onto ONE tail with BYTE-IDENTICAL strings.
func TestRun_CloseLadderByteIdentical(t *testing.T) {
	cfg := stdRunCfg() // identical summary/reason data across all variants

	variants := map[string][]Action{
		"review_loop":      driveToClose(t, cfg, "review_loop", Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)}),
		"dot":              driveToClose(t, cfg, "dot", Event{Kind: EvModeOutcome, ModeOutcome: ModeSuccess, At: at(3)}),
		"single_completed": driveToClose(t, cfg, "single", Event{Kind: EvAgentCompleted, At: at(3)}),
		"single_exit0":     driveToClose(t, cfg, "single", Event{Kind: EvCleanExit, ExitCode: 0, At: at(3)}),
	}
	// The budget-needs-attention close reaches the SAME tail without a merge.
	budget := func() []Action {
		m := NewRun(cfg)
		m.Step(Event{Kind: EvStartRun, Mode: "review_loop", At: at(1)})
		m.Step(Event{Kind: EvProvisioned, At: at(2)})
		var tail []Action
		tail = append(tail, m.Step(Event{Kind: EvModeOutcome, ModeOutcome: ModeBudget, NeedsAttention: true, At: at(3)})...)
		tail = append(tail, m.Step(Event{Kind: EvCloseResult, Close: CloseClosed, At: at(4)})...)
		return tail
	}()
	variants["budget_needs_attention"] = budget

	// noChange merge is the sixth variant (shares the merge close tail).
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
// (RSM-INV-004 spine unification) each land the machine in exactly one Done
// state, and every reopen path lands Done{reopened}, every close path
// Done{closed}.
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
