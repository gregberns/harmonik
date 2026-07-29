package runloop

// runbridge_characterization_test.go — characterization of the TERMINAL spine
// of the shared launch → dispatch → wait → probe → teardown sequence.
//
// RunBridge is where the pure Run state machine meets the live daemon: it is
// the single place a bead is closed or reopened and the single place a run
// terminal is emitted. Every dispatch site ends here. It had no tests.
//
// These pin the OUTCOMES the bridge produces for each terminal class — what
// happens to the bead, whether the run reports success, and which events reach
// the bus — using fake ports. They do not pin the hook layout, so a Phase-3
// decomposition that preserves the outcomes preserves these.
//
// NOT covered here, deliberately, because the current code makes it
// unreachable without a real git repository: every branch that runs a merge or
// the scenario gate. mergeHook calls runmerge.RunBranchToTarget and gateHook
// calls runScenarioGateIfNeededVia directly — neither is behind a port — so the
// per-mode merge-retry budget (single 1 / review-loop 3 / DOT 1), the
// retryable-vs-fatal classification, and the DOT already-approved carve-out
// cannot be exercised in a unit test. That is a real seam finding, not an
// omission.
//
// Spec: specs/run-state-machine.md RSM-020/021/022/033/035.

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/substrate"
)

// ── fakes ────────────────────────────────────────────────────────────────────

type bridgeLedgerCall struct {
	op             string // "close" | "reopen"
	reason         string
	needsAttention bool
	ctxAlive       bool // the context the bridge handed the ledger was usable
}

// bridgeLedger records every terminal write the bridge made, and whether the
// context it arrived with was still usable. The liveness flag is the point:
// a terminal write handed a dead context is a write that silently does nothing.
type bridgeLedger struct {
	mu       sync.Mutex
	calls    []bridgeLedgerCall
	closeErr error
}

func (l *bridgeLedger) ShowBead(context.Context, core.BeadID) (core.BeadRecord, error) {
	return core.BeadRecord{}, nil
}

func (l *bridgeLedger) ReopenBead(ctx context.Context, _ core.RunID, _ core.TransitionID, _ core.BeadID, reason string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, bridgeLedgerCall{op: "reopen", reason: reason, ctxAlive: ctx.Err() == nil})
	return nil
}

func (l *bridgeLedger) CloseBead(ctx context.Context, _ core.RunID, _ core.TransitionID, _ core.BeadID, needsAttention bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, bridgeLedgerCall{op: "close", needsAttention: needsAttention, ctxAlive: ctx.Err() == nil})
	return l.closeErr
}

func (l *bridgeLedger) ops() []bridgeLedgerCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bridgeLedgerCall(nil), l.calls...)
}

func (l *bridgeLedger) only(op string) []bridgeLedgerCall {
	var out []bridgeLedgerCall
	for _, c := range l.ops() {
		if c.op == op {
			out = append(out, c)
		}
	}
	return out
}

type bridgeEmit struct {
	typ     core.EventType
	payload []byte
}

type bridgeEmitter struct {
	mu    sync.Mutex
	calls []bridgeEmit
}

func (e *bridgeEmitter) Emit(_ context.Context, typ core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, bridgeEmit{typ: typ, payload: payload})
	return nil
}

func (e *bridgeEmitter) EmitWithRunID(ctx context.Context, _ core.RunID, typ core.EventType, payload []byte) error {
	return e.Emit(ctx, typ, payload)
}

func (e *bridgeEmitter) emitted() []bridgeEmit {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]bridgeEmit(nil), e.calls...)
}

func (e *bridgeEmitter) saw(typ core.EventType) bool {
	for _, c := range e.emitted() {
		if c.typ == typ {
			return true
		}
	}
	return false
}

// outcomeReason digs the reason out of the last outcome_emitted payload.
func (e *bridgeEmitter) outcome(t *testing.T) (kind, reason string, found bool) {
	t.Helper()
	for _, c := range e.emitted() {
		if c.typ != core.EventTypeOutcomeEmitted {
			continue
		}
		var pl struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(c.payload, &pl); err != nil {
			t.Fatalf("outcome_emitted payload is not decodable: %v", err)
		}
		kind, reason, found = pl.Kind, pl.Reason, true
	}
	return kind, reason, found
}

type terminalRecord struct {
	called   bool
	success  bool
	summary  string
	draining bool
	ctxAlive bool
}

// newBridge builds a RunBridge over fake ports, plus the recorders for its two
// observable outputs: the ledger writes and the run-terminal emission.
func newBridge(t *testing.T, mode core.WorkflowMode) (*RunBridge, *bridgeLedger, *bridgeEmitter, *terminalRecord) {
	t.Helper()
	ledger := &bridgeLedger{}
	emitter := &bridgeEmitter{}
	term := &terminalRecord{}

	b := NewRunBridge(
		RunEnv{TargetBranch: "main"},
		RunPorts{Ledger: ledger, Emitter: emitter, Clock: substrate.NewFakeClock(time.Unix(0, 0))},
		SharedHandles{TIDGen: core.NewTransitionIDGenerator()},
		core.RunID{}, core.BeadID("bead-1"), mode,
		func(ctx context.Context, success bool, summary string, draining bool) {
			term.called = true
			term.success = success
			term.summary = summary
			term.draining = draining
			term.ctxAlive = ctx.Err() == nil
		},
	)
	// The spine hooks with no merge/gate context: the DOT posture (SkipGate)
	// keeps the gate off the real toolchain, and PreMergeSync is a tripwire —
	// every terminal exercised below must reach the ledger WITHOUT merging, so
	// any merge attempt should name itself rather than surface as a nil panic.
	b.WireSpine(SpineArgs{
		SkipGate: true,
		PreMergeSync: func() string {
			t.Error("this terminal attempted a merge; every case in this file must close or reopen without one")
			return "merge attempted in a no-merge test"
		},
	})
	return b, ledger, emitter, term
}

// ── the outcomes ─────────────────────────────────────────────────────────────

// TestRunBridge_ReopenSurvivesACancelledRunContext pins RSM-022 / hk-e3fy: when
// the run's own context is already dead — a stale-watcher abort, a daemon
// shutdown — the reopen STILL reaches the ledger with a usable context.
//
// This is the single most consequential property in the file. A reopen that
// no-ops under a dead context leaves the bead in_progress with no run behind
// it, and nothing ever picks it up again.
func TestRunBridge_ReopenSurvivesACancelledRunContext(t *testing.T) {
	b, ledger, _, term := newBridge(t, core.WorkflowModeSingle)

	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx, core.WorkflowModeSingle)
	cancel() // the run's context dies mid-flight
	b.Fail(ctx, "agent_ready_timeout", "agent_ready_timeout")

	reopens := ledger.only("reopen")
	if len(reopens) != 1 {
		t.Fatalf("reopen calls = %d, want exactly 1 — the bead would be stranded in_progress", len(reopens))
	}
	if !reopens[0].ctxAlive {
		t.Error("the reopen was handed an already-cancelled context; the ledger write would be dropped")
	}
	if reopens[0].reason != "agent_ready_timeout" {
		t.Errorf("reopen reason = %q, want the classified failure reason", reopens[0].reason)
	}
	if !term.called || term.success {
		t.Errorf("run terminal: called=%v success=%v, want called with success=false", term.called, term.success)
	}
}

// TestRunBridge_MachineEmissionsAreSuppressedExceptOutcome pins the emission
// split: the bridge forwards ONLY outcome_emitted to the bus. run_started and
// the escaped-worktree event are emitted imperatively at their own sites (with
// payloads the machine does not carry), so forwarding them here would put a
// second, payload-less copy of each into the event stream that every consumer
// downstream would have to dedupe.
func TestRunBridge_MachineEmissionsAreSuppressedExceptOutcome(t *testing.T) {
	b, _, emitter, _ := newBridge(t, core.WorkflowModeSingle)

	b.Start(context.Background(), core.WorkflowModeSingle)

	if emitter.saw(core.EventTypeRunStarted) {
		t.Error("run_started reached the bus from the machine; it is already emitted imperatively — double emission")
	}
	if got := len(emitter.emitted()); got != 0 {
		t.Errorf("provisioning emitted %d event(s), want none from this path", got)
	}
}

// TestRunBridge_SubsumedClosesWithoutMerging pins the noChange-subsumed
// terminal: work that already landed on the target branch under this bead
// closes the bead and reports SUCCESS without attempting a merge.
//
// Merging here would try to land a branch whose commits are already in the
// target, which fails, which would reopen a bead whose work is done.
func TestRunBridge_SubsumedClosesWithoutMerging(t *testing.T) {
	b, ledger, emitter, term := newBridge(t, core.WorkflowModeSingle)
	ctx := context.Background()

	b.Start(ctx, core.WorkflowModeSingle)
	b.Feed(ctx, runexec.Event{
		Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
		EmitOutcome: true, Detail: "noChange-subsumed: work already on main",
	})

	closes := ledger.only("close")
	if len(closes) != 1 {
		t.Fatalf("close calls = %d, want exactly 1", len(closes))
	}
	if closes[0].needsAttention {
		t.Error("a subsumed close was flagged needs-attention")
	}
	if len(ledger.only("reopen")) != 0 {
		t.Error("a subsumed run also reopened the bead")
	}
	if !b.Success() {
		t.Error("a subsumed run reported failure")
	}
	if !term.called || !term.success {
		t.Errorf("run terminal: called=%v success=%v, want called with success=true", term.called, term.success)
	}
	kind, reason, found := emitter.outcome(t)
	if !found || kind != "approved" {
		t.Errorf("outcome_emitted = %q (found=%v), want approved", kind, found)
	}
	if reason != "" {
		t.Errorf("an approved outcome carried a rejection reason %q", reason)
	}
}

// TestRunBridge_BudgetExhaustedClosesNeedsAttentionButFailsTheRun pins the
// review-loop budget ladder, which is the one terminal that does BOTH: it
// closes the bead (flagged needs-attention, so a human sees it) and reports the
// run as FAILED.
//
// The combination is deliberate and easy to lose. Closing without the flag
// hides a bead that needs a human; reopening instead would re-dispatch work
// that has already burned its budget.
func TestRunBridge_BudgetExhaustedClosesNeedsAttentionButFailsTheRun(t *testing.T) {
	b, ledger, emitter, term := newBridge(t, core.WorkflowModeReviewLoop)
	ctx := context.Background()

	b.Start(ctx, core.WorkflowModeReviewLoop)
	b.SetRejectReason("review_loop_budget_exhausted")
	b.Feed(ctx, runexec.Event{
		Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeBudget,
		NeedsAttention: true, Detail: "review_loop_budget_exhausted (max=2)",
	})

	closes := ledger.only("close")
	if len(closes) != 1 {
		t.Fatalf("close calls = %d, want exactly 1", len(closes))
	}
	if !closes[0].needsAttention {
		t.Error("the budget-exhausted close was not flagged needs-attention — nobody would look at it")
	}
	if len(ledger.only("reopen")) != 0 {
		t.Error("the budget-exhausted bead was reopened; it would be re-dispatched with no budget left")
	}
	if b.Success() || term.success {
		t.Error("a budget-exhausted run reported success")
	}
	kind, reason, found := emitter.outcome(t)
	if !found || kind != "rejected" {
		t.Errorf("outcome_emitted = %q (found=%v), want rejected", kind, found)
	}
	if reason != "review_loop_budget_exhausted" {
		t.Errorf("rejected outcome reason = %q, want the caller-set reject reason", reason)
	}
}

// TestRunBridge_TransientLedgerOutageAfterCloseIsSuccess pins hk-hypbi: the
// bead ledger being temporarily unavailable at close time, AFTER the work has
// already landed, is a SUCCESS. The bead stays in_progress for the ledger
// reconciler to finish, and the run is not reopened.
//
// Treating it as a failure would reopen a bead whose work is already merged,
// and the re-dispatch would find nothing to do.
func TestRunBridge_TransientLedgerOutageAfterCloseIsSuccess(t *testing.T) {
	b, ledger, _, term := newBridge(t, core.WorkflowModeSingle)
	ledger.closeErr = brcli.BrUnavailable
	ctx := context.Background()

	b.Start(ctx, core.WorkflowModeSingle)
	b.Feed(ctx, runexec.Event{
		Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
		EmitOutcome: true, Detail: "noChange-subsumed",
	})

	if len(ledger.only("reopen")) != 0 {
		t.Error("a transient ledger outage reopened a bead whose work had already landed")
	}
	if !b.Success() {
		t.Error("a transient ledger outage was reported as a failed run")
	}
	if !term.called || !term.success {
		t.Errorf("run terminal: called=%v success=%v, want called with success=true", term.called, term.success)
	}
}

// TestRunBridge_HardCloseErrorFailsTheRunAndLeavesTheBeadAlone pins the
// close-error terminal: a close that fails for a reason other than a transient
// outage fails the run — and does NOT reopen the bead.
//
// The bead is left in_progress on purpose: the work may well have merged, and
// reopening would re-dispatch it. This is a known operator-intervention state,
// and pinning it is what makes a future decision to change it deliberate.
func TestRunBridge_HardCloseErrorFailsTheRunAndLeavesTheBeadAlone(t *testing.T) {
	b, ledger, _, term := newBridge(t, core.WorkflowModeSingle)
	ledger.closeErr = errors.New("ledger rejected the transition")
	ctx := context.Background()

	b.Start(ctx, core.WorkflowModeSingle)
	b.Feed(ctx, runexec.Event{
		Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeSubsumed,
		EmitOutcome: true, Detail: "noChange-subsumed",
	})

	if len(ledger.only("close")) != 1 {
		t.Fatalf("close calls = %d, want exactly 1", len(ledger.only("close")))
	}
	if len(ledger.only("reopen")) != 0 {
		t.Error("a hard close error reopened the bead; today it is deliberately left in_progress")
	}
	if b.Success() || term.success {
		t.Error("a hard close error reported run success")
	}
	if !term.called {
		t.Error("no run terminal was emitted for a hard close error")
	}
}

// TestRunBridge_DrainWithoutACommitReopensAndEmitsNoRunTerminal pins the
// shutdown-drain requeue edge: a run interrupted before it committed anything
// is reopened for re-dispatch and emits NO run terminal at all.
//
// The silence is the behaviour. A run_failed here would record a failure for
// every in-flight bead on every daemon restart, and the sentinel and the
// dashboards act on those counts.
func TestRunBridge_DrainWithoutACommitReopensAndEmitsNoRunTerminal(t *testing.T) {
	b, ledger, _, term := newBridge(t, core.WorkflowModeSingle)

	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx, core.WorkflowModeSingle)
	cancel()         // SIGTERM
	b.Drain(ctx, "") // nothing was committed

	reopens := ledger.only("reopen")
	if len(reopens) != 1 {
		t.Fatalf("reopen calls = %d, want exactly 1 so the bead is re-dispatched next boot", len(reopens))
	}
	if !reopens[0].ctxAlive {
		t.Error("the drain reopen was handed a cancelled context — the requeue would be lost on shutdown")
	}
	if len(ledger.only("close")) != 0 {
		t.Error("an uncommitted drained run closed its bead")
	}
	if term.called {
		t.Errorf("the drain requeue emitted a run terminal (success=%v, summary=%q); it must be silent", term.success, term.summary)
	}
}

// TestRunBridge_OrdinaryFailureIsNotMarkedDraining pins the negative half of
// the drain flag the terminal emitter reads: an ordinary run failure must not
// claim to be a shutdown drain. The emitter uses the flag to skip session-data
// collection, so a falsely-set flag silently loses session data for every
// failed run.
//
// The positive half — the flag reaching the emitter set — is NOT pinned here.
// It is only observable on the drain-WITH-a-commit path, which merges, and the
// merge is not behind a port (see the file header).
func TestRunBridge_OrdinaryFailureIsNotMarkedDraining(t *testing.T) {
	b, _, _, term := newBridge(t, core.WorkflowModeSingle)
	ctx := context.Background()

	b.Start(ctx, core.WorkflowModeSingle)
	b.Feed(ctx, runexec.Event{Kind: runexec.EvModeOutcome, ModeOutcome: runexec.ModeFailure, Reason: "r", Detail: "s"})

	if !term.called {
		t.Fatal("no run terminal emitted on the ordinary failure path")
	}
	if term.draining {
		t.Error("an ordinary run failure was marked as a shutdown drain")
	}
}

// TestRunBridge_PreDispatchFailureStillReopens pins that a failure classified
// BEFORE the agent was ever dispatched — a launch-spec build error, a refused
// credential — reaches the same reopen spine as a post-dispatch failure. Both
// must return the bead to the queue; neither may leave it claimed.
func TestRunBridge_PreDispatchFailureStillReopens(t *testing.T) {
	b, ledger, _, term := newBridge(t, core.WorkflowModeSingle)
	ctx := context.Background()

	// No Start(): the machine is still in its pre-dispatch phase.
	b.Fail(ctx, "worktree_create_failed: disk full", "worktree_create_failed")

	reopens := ledger.only("reopen")
	if len(reopens) != 1 {
		t.Fatalf("reopen calls = %d, want exactly 1", len(reopens))
	}
	if reopens[0].reason != "worktree_create_failed: disk full" {
		t.Errorf("reopen reason = %q, want the classified pre-dispatch reason", reopens[0].reason)
	}
	if !term.called || term.success {
		t.Errorf("run terminal: called=%v success=%v, want called with success=false", term.called, term.success)
	}
}
