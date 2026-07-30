package daemon_test

// admissionorder_test.go — the admission gates of runWorkLoop must run in a
// fixed order, and these tests hold that order.
//
// # Why this file exists
//
// runWorkLoop in internal/daemon/scheduler.go decides whether to dispatch a
// queue item through a run of inline gates. A measurement pass found nine
// ordering constraints between those gates and almost nothing that asserts any
// of them. The order lived only in the source order plus comments, and several
// of those comments cited line numbers that were already wrong.
//
// The order is load-bearing and the failure mode is silent. Move a gate and the
// code still compiles, the gate stops firing, and no test goes red. So the
// planned fold of these gates into internal/orchestrator (DECOMPOSITION-MAP.md
// §3 Step 3) is unsafe until the order is executable. That is what this file
// makes it.
//
// # The constraint map
//
// The numbers below are the numbers in DECOMPOSITION-MAP.md §3 "Ordering
// constraints that exist only as the current source order". They are kept the
// same on purpose, so the plan and this file can be read side by side. Do not
// renumber one without the other.
//
//   - 1 cooldown before the pre-claim br show — PINNED.
//     TestAdmissionOrder_CooldownRunsBeforePreClaimShowBead. The cooldown's whole
//     purpose is to suppress that subprocess at poll cadence.
//   - 2 greenlight after the pre-claim br show — PINNED.
//     TestAdmissionOrder_GreenlightRunsAfterPreClaimShowBead. The gate reads
//     labels off preClaimRecord, which is the zero value until ShowBead returns.
//   - 3 local-cap before the Phase-3 stamp — TWO CLAUSES, both now pinned, in
//     two different places. The guard's POSITION relative to the stamp is pinned
//     by l5saf_localonly_strand_test.go, which drives a real tick and asserts the
//     persisted item status. That is the shape this file copies and it is not
//     duplicated here. The second clause — localInFlight must not be incremented
//     before the guard — is pinned by
//     TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount, because the
//     l5saf fixture structurally cannot see it: it sits ON the saturated side of
//     the cap, where hoisting the increment does not change the branch.
//   - 4 cross-queue dedup inside the same write-lock hold as the stamp —
//     HALF PINNED. TestAdmissionOrder_CrossQueueDedupPrecedesTheClaim pins the
//     outcome: the loser is failed for the dedup reason and never claimed. The
//     lock HOLD is a declared gap. See that test's own comment for why.
//   - 5 attempts-bound(a) fused to the stamp — PINNED.
//     TestAdmissionOrder_AttemptsBoundStaysFusedToTheStamp. A bead held by a
//     pre-stamp gate must not spend its dispatch budget.
//   - 6 governor.tick before the sentinel-queue gate in the same tick — PINNED,
//     in sentinelgate_test.go rather than here. The gate is
//     m.sentinelBlocksDispatch, which returns false unless a movementGovernor was
//     constructed. That construction gates on the movement_governor subsystem
//     switch first (enabled by default, so every fixture passes it) and on
//     workLoopDeps.governorState second. governorState now has a mirror on
//     WorkLoopDepsParams (GovernorState), so a loop in which the gate can fire is
//     buildable from daemon_test. Three tests: the gate holds on the queue path,
//     it holds on the br-ready path, and a trip armed INSIDE governor.tick gates
//     the same tick it was armed.
//   - 7 the two dispatch paths order the same gates differently — PINNED.
//     TestAdmissionOrder_ReadyPathBoundsAttemptsBeforeHandlerPause.
//   - 8 delay is two different outcomes — HALF PINNED.
//     TestAdmissionOrder_TerminalDedupLeavesAWakeTokenPending pins the fact that
//     decides the merge: a wake token is already pending at four of the five
//     no-sleep sites, so merging those toward the sleeping variant costs zero
//     latency, not one poll interval. The other direction — merging toward the
//     no-sleep variant — busy-spins the cooldown, and no test can reach a code
//     shape that does not exist. See OPEN-DEFECTS.md.
//   - 9 decision-required and sentinel-queue are freely swappable — nothing to
//     pin. The plan records this as the one pair with no real constraint, and
//     re-reading the two blocks agrees: only the stderr string differs.
//
// # How these tests are built
//
// Each one drives real ticks of runWorkLoop and asserts an observable the gate
// controls: how many times a subprocess seam was called, whether the bead was
// claimed, what the persisted queue item says. None of them re-states the
// boolean a gate evaluates. Re-stating the condition is the documented way this
// codebase's guards have been missed — see the header of
// l5saf_localonly_strand_test.go.
//
// Every negative assertion is paired with a positive control on the SAME
// fixture. Without the control a test would still pass when the fixture simply
// could not reach the thing under test, and it would prove nothing.
//
// Every test here was checked by mutation: the ordering it claims to protect was
// broken in a throwaway copy of the tree, and the test was confirmed to go red
// while go build and go vet stayed green.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixture constants
// ─────────────────────────────────────────────────────────────────────────────

const (
	// admissionWakePumpInterval is how often these tests poke the queue store's
	// wake channel. Every hold gate in runWorkLoop sleeps one poll interval
	// (workloopPollInterval, 2 s) and workloopSleep returns as soon as a wake
	// signal arrives. Without the pump an observation window holds one tick, and
	// a test that must count ticks cannot work.
	admissionWakePumpInterval = 2 * time.Millisecond

	// admissionObserveWindow is how long each test lets the loop run. At the pump
	// rate above it holds many ticks.
	//
	// No test that USES this window asserts an exact tick count. Each asserts a
	// floor on an observable, or an exact count on a value that saturates, so a
	// slow machine makes the window hold fewer ticks rather than making the test
	// wrong. Both saturating cases — the attempts budget and the pre-increment
	// claim count — are bounded by queue.MaxItemAttempts, and no non-test code path
	// resets a queue item's Attempts, so extra ticks cannot inflate them.
	admissionObserveWindow = 600 * time.Millisecond

	// admissionMinTicks is the floor a positive control must clear before the
	// paired negative assertion means anything. A "the gate suppressed the call"
	// claim is empty if the window only ever held one tick.
	admissionMinTicks = 4
)

// errAdmissionClaimRefused is what the fake ledger returns from ClaimBead.
//
// A failing claim is what keeps these tests hermetic. The loop logs the failure,
// reverts the queue item and continues, so it never reaches beadRunOne. No
// worktree, no tmux window and no agent process is created, and the test needs
// no -short exclusion.
//
// DO NOT "SIMPLIFY" THIS TEXT. It deliberately avoids the word "blocked".
// runWorkLoop's dependency-blocked detector (hk-n91y0) is a bare
// strings.Contains(claimErr.Error(), "blocked"), so ANY error text containing that
// word anywhere routes the item down a different branch: it is driven terminal
// through evaluateGroupAdvanceWithOutcome instead of being reverted to pending and
// retried. Every test here that counts claims across ticks depends on the retry
// path. The substring match itself is a recorded defect — see OPEN-DEFECTS.md.
var errAdmissionClaimRefused = errors.New("admission-order fake: claim refused")

// errAdmissionShowFailed is what the fake ledger returns from ShowBead when the
// test arms showErr.
var errAdmissionShowFailed = errors.New("admission-order fake: br show failed")

// ─────────────────────────────────────────────────────────────────────────────
// admissionLedger — the bead-ledger fake these tests observe the loop through
// ─────────────────────────────────────────────────────────────────────────────

// admissionLedger is a beadLedger fake that counts the two calls the admission
// order is visible through: ShowBead (the pre-claim subprocess the cooldown
// exists to suppress, and the only source of the labels the greenlight gate
// reads) and ClaimBead (the first irreversible act of a dispatch).
//
// It records rather than absorbs. CloseBead and ReopenBead belong to the run
// path, which no test here reaches, so a call to either lands in unexpected and
// fails the test. A permissive stub would swallow that signal.
type admissionLedger struct {
	mu sync.Mutex

	// showStatus is the coarse status every ShowBead reply carries.
	showStatus core.CoarseStatus

	// showLabels are the labels every ShowBead reply carries. The greenlight gate
	// reads labels off the pre-claim record, so this field is the ONLY route by
	// which the needs-greenlight label reaches the loop.
	showLabels []string

	// showErr, when set, makes every ShowBead fail.
	showErr error

	// readyResult is what Ready returns. Empty for every queue-path test.
	readyResult []core.BeadRecord

	// onShowBead, when set, runs after each ShowBead reply is counted and
	// receives the new total. The br-ready test uses it to arm a handler pause at
	// the exact tick the attempt budget runs out.
	onShowBead func(total int)

	showTotal  int
	showCalls  map[core.BeadID]int
	claimCalls map[core.BeadID]int

	// unexpected records calls no test in this file should ever cause.
	unexpected []string
}

func newAdmissionLedger() *admissionLedger {
	return &admissionLedger{
		showStatus: core.CoarseStatusOpen,
		showCalls:  make(map[core.BeadID]int),
		claimCalls: make(map[core.BeadID]int),
	}
}

func (l *admissionLedger) Ready(_ context.Context) ([]core.BeadRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.readyResult, nil
}

func (l *admissionLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	l.mu.Lock()
	l.showTotal++
	l.showCalls[id]++
	total := l.showTotal
	status := l.showStatus
	labels := append([]string(nil), l.showLabels...)
	showErr := l.showErr
	hook := l.onShowBead
	l.mu.Unlock()

	// The hook runs with the lock released so it may reach back into the fixture.
	if hook != nil {
		hook(total)
	}
	if showErr != nil {
		return core.BeadRecord{}, showErr
	}
	return core.BeadRecord{BeadID: id, Status: status, Labels: labels}, nil
}

func (l *admissionLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID) error {
	l.mu.Lock()
	l.claimCalls[id]++
	l.mu.Unlock()
	return errAdmissionClaimRefused
}

func (l *admissionLedger) CloseBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ bool) error {
	l.recordUnexpected("CloseBead", id)
	return nil
}

func (l *admissionLedger) ReopenBead(_ context.Context, _ string, _ brcli.TimeoutConfig, _ core.RunID, _ core.TransitionID, id core.BeadID, _ string) error {
	l.recordUnexpected("ReopenBead", id)
	return nil
}

func (l *admissionLedger) recordUnexpected(method string, id core.BeadID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.unexpected = append(l.unexpected, fmt.Sprintf("%s(%s)", method, id))
}

func (l *admissionLedger) showCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.showCalls[id]
}

func (l *admissionLedger) claimCount(id core.BeadID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.claimCalls[id]
}

func (l *admissionLedger) setStatus(s core.CoarseStatus) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.showStatus = s
}

func (l *admissionLedger) setShowErr(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.showErr = err
}

func (l *admissionLedger) setShowHook(fn func(total int)) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onShowBead = fn
}

func (l *admissionLedger) assertNoRunPathCalls(t *testing.T) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.unexpected) != 0 {
		t.Errorf("the run path was reached: unexpected ledger calls %v — these tests must stop at the claim",
			l.unexpected)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// admissionResetter — the stranded-bead auto-reset seam, wired but unreachable
// ─────────────────────────────────────────────────────────────────────────────

// admissionResetter is the hk-l2xd1 stranded-in-progress resetter. Production
// wires this seam unconditionally, so a fixture that leaves it nil is testing a
// configuration that cannot occur.
//
// It is wired here and must never be called. The cooldown fixture registers a
// live run for its bead, so runRegistry.HasBeadRun is true and the auto-reset
// branch is skipped by its own guard.
//
// Read this assertion for what it is: a fixture check, not the proof that the
// cooldown armed. The reset branch arms no cooldown, so if the fixture had taken
// it the ShowBead count would have run away and the count assertion would already
// have failed. What proves the cooldown is the differential between the two
// subtests — draft and in_progress traverse byte-identical code apart from the two
// CoarseStatusInProgress comparisons.
type admissionResetter struct {
	mu    sync.Mutex
	calls []core.BeadID
}

func (r *admissionResetter) ResetBead(_ context.Context, _ string, _ brcli.TimeoutConfig, beadID core.BeadID, _ core.ProjectHash, _ int64) error {
	r.mu.Lock()
	r.calls = append(r.calls, beadID)
	r.mu.Unlock()
	return nil
}

func (r *admissionResetter) assertUnused(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) != 0 {
		t.Errorf("stranded-bead auto-reset ran for %v — the bead has a live run in the registry, so "+
			"the stranded-bead auto-reset branch (hk-l2xd1) must be skipped and the in-progress cooldown (hk-403fw) must arm instead", r.calls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// admissionQueueLedger — the seam ReevaluateDeferred re-checks blockers through
// ─────────────────────────────────────────────────────────────────────────────

// admissionQueueLedger is the queue.BeadLedger the loop calls
// queue.ReevaluateDeferred with. ReevaluateDeferred returns early on a nil
// ledger, so a test whose item is deferred needs a non-nil one to get the item
// back to pending on the next tick.
//
// Both methods count and then fail the test. Every group in this file that can
// be deferred holds ONE deferrable item, and ReevaluateDeferred only asks about
// in-group SIBLINGS, so neither method has a legitimate caller here. A call
// means the fixture stopped being the fixture the tests were reasoned about.
type admissionQueueLedger struct {
	mu    sync.Mutex
	calls []string
}

func (l *admissionQueueLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	l.mu.Lock()
	l.calls = append(l.calls, fmt.Sprintf("LookupStatus(%s)", id))
	l.mu.Unlock()
	return queue.BeadStatusOpen, nil
}

func (l *admissionQueueLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	l.mu.Lock()
	l.calls = append(l.calls, fmt.Sprintf("BlocksEdge(%s,%s)", blocker, blocked))
	l.mu.Unlock()
	return false, nil
}

func (l *admissionQueueLedger) assertUnused(t *testing.T) {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.calls) != 0 {
		t.Errorf("queue ledger was consulted %v — the deferrable groups here hold one item each, "+
			"so ReevaluateDeferred has no sibling to ask about", l.calls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Shared fixture helpers
// ─────────────────────────────────────────────────────────────────────────────

// admissionQueue builds a single-group active queue holding items.
func admissionQueue(name string, items ...queue.Item) *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "admission-" + name + "-id",
		Name:          name,
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			CreatedAt:  time.Now().UTC(),
			Items:      items,
		}},
	}
}

// admissionParkedItem returns an item the dispatcher can never select and that
// never counts as terminal.
//
// The projection in internal/queue admits a pending item only while Attempts is
// below queue.MaxItemAttempts, and itemIsTerminal counts only completed and
// failed. So an over-budget pending item is invisible to selection AND keeps its
// group off all-terminal.
//
// That matters because a group that reaches all-terminal advances, which can
// clear the queue out of the store and erase the state a test is about to read.
// Parking one item keeps the queue alive without adding a second dispatchable
// bead.
func admissionParkedItem(id core.BeadID) queue.Item {
	return queue.Item{
		BeadID:   id,
		Status:   queue.ItemStatusPending,
		Attempts: queue.MaxItemAttempts,
	}
}

// admissionDeps builds the work-loop deps every test here shares.
//
// NoAutoPull is left to the caller: the queue-path tests set it so the br-ready
// fallback cannot supply dispatch input, and the br-ready test clears it.
//
// DiskFreeBytesFunc reports far above the watermark on purpose. Below it the
// periodic disk check runs stale-worktree reclaim and `go clean -cache` as real
// subprocesses, which would wipe this machine's shared Go build cache.
func admissionDeps(t *testing.T, ledger *admissionLedger, qs *queuewiring.QueueStore, qLedger queue.BeadLedger, noAutoPull bool, pause *daemon.HandlerPauseController) daemon.WorkLoopDepsParams {
	t.Helper()
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	return daemon.WorkLoopDepsParams{
		BrAdapter:              ledger,
		Bus:                    &stubEventCollector{},
		ProjectDir:             projectDir,
		HandlerBinary:          "/bin/sh",
		HandlerArgs:            []string{"-c", "exit 0"},
		IntentLogDir:           filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:       NewSealedAdapterRegistryForTest(t),
		QueueStore:             qs,
		QueueLedger:            qLedger,
		NoAutoPull:             noAutoPull,
		HandlerPauseController: pause,
		DiskFreeBytesFunc:      func(string) (uint64, error) { return 1 << 62, nil },
	}
}

// runAdmissionLoop drives real ticks of the work loop, then calls inspect while
// the loop is STILL ALIVE, then shuts the loop down.
//
// inspect must run before the cancel. The shutdown drain (drainCancelledQueue)
// moves active queues to cancelled and clears the in-memory store, which erases
// exactly the state these tests read.
//
// runLoop is a closure rather than a deps argument because workLoopDeps is
// unexported, so no helper outside package daemon can name it in a signature.
func runAdmissionLoop(t *testing.T, qs *queuewiring.QueueStore, runLoop func(context.Context), inspect func()) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), admissionObserveWindow+20*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		runLoop(ctx)
	}()

	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		tick := time.NewTicker(admissionWakePumpInterval)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				qs.Wake()
			case <-ctx.Done():
				return
			}
		}
	}()

	time.Sleep(admissionObserveWindow)
	inspect()

	cancel()
	select {
	case <-loopDone:
	case <-time.After(15 * time.Second):
		t.Error("work loop did not exit within 15s after context cancel")
	}
	<-pumpDone
}

// admissionFirstItem returns the item under test out of a snapshot, or fails the
// test. Every fixture here puts the bead under test at index 0 of the single
// group, and any later index holds only the parked filler item.
func admissionFirstItem(t *testing.T, q *queue.Queue) queue.Item {
	t.Helper()
	if q == nil {
		t.Fatal("queue snapshot is nil — the queue was never loaded, so the assertions below would prove nothing")
	}
	if len(q.Groups) != 1 {
		t.Fatalf("queue snapshot has %d groups, want 1: %+v", len(q.Groups), q)
	}
	if len(q.Groups[0].Items) == 0 {
		t.Fatalf("queue snapshot has no items: %+v", q)
	}
	return q.Groups[0].Items[0]
}

// newAdmissionPauseController builds a handler-pause controller on a sealed bus.
func newAdmissionPauseController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("newAdmissionPauseController: bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

// ─────────────────────────────────────────────────────────────────────────────
// Constraint 1 — the cooldown runs BEFORE the pre-claim ShowBead
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionOrder_CooldownRunsBeforePreClaimShowBead pins the position of the
// hk-403fw in-progress cooldown against the pre-claim ShowBead.
//
// The cooldown exists for one reason: to stop the loop calling `br show` and
// emitting bead_claim_skipped at poll cadence while a run holds the bead. It can
// only do that from ABOVE the pre-claim ShowBead. Below it, the subprocess it
// exists to suppress runs on every tick and the gate is decoration.
//
// The test measures the subprocess, not the gate's condition. It counts ShowBead
// calls across many ticks.
//
//   - Armed: the bead reads in_progress, which arms the cooldown for five
//     minutes on the first tick. ShowBead is then called ONCE for the whole
//     window, however many ticks it holds.
//   - Positive control: the bead reads draft instead. Draft takes the same
//     deferral path but arms no cooldown, so ShowBead is called on every tick.
//
// The control is what makes the armed case mean something. Without it the armed
// assertion would also pass on a fixture that never reached ShowBead at all, and
// on a window that only held one tick.
func TestAdmissionOrder_CooldownRunsBeforePreClaimShowBead(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-403fw-cooldown-bead"

	// observe runs one fixture and returns how many times ShowBead was called for
	// the bead under test, plus how many ticks the loop actually completed.
	//
	// The tick count comes from the periodic disk probe with its cadence overridden
	// to a nanosecond, so it fires once per tick. It is the one per-tick observable
	// in this fixture that is independent of ShowBead: on the armed path nothing
	// else moves, because the item is un-deferred once and then simply held Pending
	// by the cooldown on every later tick. Without it the armed subtest could only
	// borrow the control subtest's tick evidence.
	observe := func(t *testing.T, status core.CoarseStatus) (showCalls, ticks int) {
		t.Helper()
		ledger := newAdmissionLedger()
		ledger.setStatus(status)

		qLedger := &admissionQueueLedger{}
		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main", queue.Item{BeadID: beadID, Status: queue.ItemStatusPending}))

		// Reach the cooldown by the route production takes.
		//
		// The resetter seam is wired, because production wires it unconditionally.
		// The way production still reaches the arm site is runRegistry.HasBeadRun:
		// the auto-reset branch guards on the bead having NO live run, and a bead
		// with a live run is exactly what the cooldown is for. So register a run for
		// this bead, which skips the reset branch and arms the cooldown.
		//
		// QueueName is empty, which is the shape a br-ready-dispatched run has. An
		// empty name also keeps this handle out of the "main" queue's per-queue
		// in-flight tally, so selection still offers the item.
		reg := daemon.NewRunRegistry()
		daemon.ExportedRunRegistryRegister(reg, core.RunID(uuid.New()), &daemon.RunHandle{BeadID: beadID})
		resetter := &admissionResetter{}

		// tickCount rises once per tick: the disk probe's cadence is overridden below
		// so it is always due. Free space is reported far above the watermark, so the
		// probe only counts — it never latches diskLow and never runs the reclaim or
		// `go clean -cache` subprocesses that would touch this machine.
		var tickMu sync.Mutex
		tickCount := 0
		params := admissionDeps(t, ledger, qs, qLedger, true, nil)
		params.RunRegistry = reg
		params.StrandedInProgressResetter = resetter
		params.DiskFreeBytesFunc = func(string) (uint64, error) {
			tickMu.Lock()
			tickCount++
			tickMu.Unlock()
			return 1 << 62, nil
		}

		depsPtr := daemon.ExportedWorkLoopDepsPtr(params)
		daemon.ExportedDiskCheckSetCheckInterval(depsPtr, time.Nanosecond)
		deps := *depsPtr

		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() {},
		)

		ledger.assertNoRunPathCalls(t)
		qLedger.assertUnused(t)
		resetter.assertUnused(t)
		if got := ledger.claimCount(beadID); got != 0 {
			t.Errorf("ClaimBead called %d time(s) for a bead that is not open, want 0", got)
		}
		tickMu.Lock()
		ticks = tickCount
		tickMu.Unlock()
		return ledger.showCount(beadID), ticks
	}

	// Positive control FIRST, so a failure reads as "the fixture cannot tick"
	// rather than as a broken gate.
	t.Run("draft bead arms no cooldown, so every tick calls ShowBead", func(t *testing.T) {
		t.Parallel()
		calls, ticks := observe(t, core.CoarseStatusDraft)
		if calls < admissionMinTicks {
			t.Fatalf("ShowBead called %d time(s) over %v with %d tick(s), want at least %d calls. "+
				"Draft takes the same deferral path as in_progress and differs only in that it arms no "+
				"cooldown, so the un-suppressed path must call ShowBead on every tick.",
				calls, admissionObserveWindow, ticks, admissionMinTicks)
		}
	})

	t.Run("in-progress bead arms the cooldown, which suppresses every later ShowBead", func(t *testing.T) {
		t.Parallel()
		calls, ticks := observe(t, core.CoarseStatusInProgress)
		// The tick floor is what lets this subtest stand on its own. Without it,
		// "ShowBead was called once" would also be true of a window that only held
		// one tick, and the result would rest on the control subtest's evidence
		// rather than on its own.
		if ticks < admissionMinTicks {
			t.Fatalf("the loop completed %d tick(s) over %v, want at least %d. "+
				"The suppression claim below is empty unless there were later ticks to suppress.",
				ticks, admissionObserveWindow, admissionMinTicks)
		}
		if calls != 1 {
			t.Fatalf("ShowBead called %d time(s) for %s across %d tick(s), want exactly 1.\n"+
				"The first tick reads in_progress and arms the hk-403fw cooldown for 5 minutes. Every later "+
				"tick must be stopped by the cooldown BEFORE the pre-claim ShowBead. More than one call means "+
				"the cooldown now sits below the ShowBead it exists to suppress, so the loop spawns `br show` "+
				"at poll cadence for the whole cooldown window.",
				calls, beadID, ticks)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Constraint 2 — the greenlight gate runs AFTER the pre-claim ShowBead
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionOrder_GreenlightRunsAfterPreClaimShowBead pins the position of
// the hk-lacr greenlight gate against the pre-claim ShowBead.
//
// This is the sharpest of the ordering constraints. The gate reads
// preClaimRecord.Labels, and preClaimRecord is declared ABOVE the block that
// fills it. Move the gate up and the compiler says nothing: the gate reads a
// zero-valued record, finds no labels, and silently never fires.
//
// A test that only checked "a bead carrying needs-greenlight is held" would
// still pass against that break, because a held bead and a bead the gate never
// looked at are indistinguishable if nothing else in the fixture can dispatch.
// So this test asserts three things together:
//
//  1. the label reached the loop — ShowBead was called, and the fake ledger is
//     the ONLY source of that label;
//  2. the bead was never claimed;
//  3. on the SAME fixture with the label removed, the bead IS claimed.
//
// Point 3 is what makes point 2 evidence. It shows the fixture dispatches
// whenever the label is absent, so the hold in the labeled case is caused by
// the label and by nothing else.
func TestAdmissionOrder_GreenlightRunsAfterPreClaimShowBead(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-lacr-greenlight-bead"
	const parkedID core.BeadID = "hk-lacr-parked-bead"

	// observe runs one fixture with the given labels on the ShowBead reply.
	observe := func(t *testing.T, labels []string) (showCalls, claimCalls int, item queue.Item) {
		t.Helper()
		ledger := newAdmissionLedger()
		ledger.showLabels = labels

		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main",
			queue.Item{BeadID: beadID, Status: queue.ItemStatusPending},
			admissionParkedItem(parkedID),
		))
		deps := daemon.ExportedWorkLoopDeps(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

		var snapshot *queue.Queue
		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() { snapshot = qs.Queue() },
		)
		ledger.assertNoRunPathCalls(t)
		return ledger.showCount(beadID), ledger.claimCount(beadID), admissionFirstItem(t, snapshot)
	}

	// Positive control FIRST. It establishes that this fixture reaches the claim.
	t.Run("unlabeled bead reaches the claim", func(t *testing.T) {
		t.Parallel()
		showCalls, claimCalls, _ := observe(t, nil)
		if showCalls == 0 {
			t.Fatal("ShowBead was never called, so the fixture never reached the pre-claim read")
		}
		if claimCalls == 0 {
			t.Fatalf("ClaimBead was never called for an unlabeled open bead. "+
				"The labeled subtest asserts that the greenlight gate STOPS the claim, and that claim is "+
				"empty unless this same fixture demonstrably claims when the label is absent. "+
				"ShowBead calls: %d.", showCalls)
		}
	})

	t.Run("needs-greenlight bead is held on a label that was actually read", func(t *testing.T) {
		t.Parallel()
		showCalls, claimCalls, item := observe(t, []string{"needs-greenlight"})

		// (1) The label reached the loop. The fake ledger's ShowBead reply is the
		// only place it exists, so no ShowBead means no label was ever read and a
		// hold would have some other cause.
		if showCalls == 0 {
			t.Fatal("ShowBead was never called, so the needs-greenlight label never entered the loop. " +
				"Any hold observed below would be evidence about something else.")
		}
		// (2) The gate held the bead.
		if claimCalls != 0 {
			t.Errorf("ClaimBead called %d time(s) for a bead carrying needs-greenlight, want 0.\n"+
				"The greenlight gate (hk-lacr) reads preClaimRecord.Labels. It must run AFTER the pre-claim ShowBead "+
				"fills that record. Above it the record is the zero value, the gate finds no labels, and it "+
				"silently never fires — which compiles clean.", claimCalls)
		}
		// (3) The item was never even stamped, so it is not stranded.
		if item.Status != queue.ItemStatusPending {
			t.Errorf("held item status = %q, want %q — the greenlight gate must defer without stamping",
				item.Status, queue.ItemStatusPending)
		}
		if item.RunID != nil {
			t.Errorf("held item carries RunID %v — a run was stamped for a bead that must not dispatch", *item.RunID)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Constraint 4 — cross-queue dedup stops the loser before the claim
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionOrder_CrossQueueDedupPrecedesTheClaim pins the hk-a11re
// cross-queue dedup guard.
//
// One bead sits in two queues. The winning queue's item is already stamped
// dispatched. The dedup guard must see that stamp and fail the losing queue's
// item WITHOUT claiming the bead. Without the guard the same bead gets two
// implementers, which is the bug hk-a11re fixed.
//
// # What this test does NOT pin, and why
//
// The source comment says the check must happen while the write lock is HELD, so
// that the winning queue's stamp is visible. This test cannot pin that half, and
// no test built on runWorkLoop can. Selection and stamping happen on ONE
// goroutine for the daemon's whole life, so two queues can never reach the stamp
// concurrently no matter where the lock is taken. The lock defends the stamp
// against the per-run goroutines that write item status through
// evaluateGroupAdvanceWithOutcome, and there is no seam that lets a test
// interleave one of those with the stamp.
//
// So: the observable outcome is pinned here. The lock-hold boundary is a stated
// gap, recorded in OPEN-DEFECTS.md.
func TestAdmissionOrder_CrossQueueDedupPrecedesTheClaim(t *testing.T) {
	t.Parallel()

	const sharedBead core.BeadID = "hk-a11re-shared-bead"
	const parkedID core.BeadID = "hk-a11re-parked-bead"

	ledger := newAdmissionLedger()

	// alpha is the winner: its item already carries the dispatched stamp, so
	// selection skips it and the dedup scan finds it.
	alphaRunID := "alpha-run-id"
	alpha := admissionQueue("alpha", queue.Item{
		BeadID: sharedBead,
		Status: queue.ItemStatusDispatched,
		RunID:  &alphaRunID,
	})

	// beta is the loser: the same bead, still pending, so selection picks it. The
	// parked item keeps beta's group off all-terminal after the dedup guard fails
	// item 0, which keeps the queue in the store for the snapshot below.
	beta := admissionQueue("beta",
		queue.Item{BeadID: sharedBead, Status: queue.ItemStatusPending},
		admissionParkedItem(parkedID),
	)

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(alpha)
	qs.SetQueue(beta)

	deps := daemon.ExportedWorkLoopDeps(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

	var betaSnapshot, alphaSnapshot *queue.Queue
	runAdmissionLoop(t, qs,
		func(c context.Context) {
			daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
		},
		func() {
			betaSnapshot = qs.QueueByName("beta")
			alphaSnapshot = qs.QueueByName("alpha")
		},
	)
	ledger.assertNoRunPathCalls(t)

	// The load-bearing assertion: the bead was never claimed a second time.
	if got := ledger.claimCount(sharedBead); got != 0 {
		t.Errorf("ClaimBead called %d time(s) for a bead queue %q had already dispatched, want 0.\n"+
			"The hk-a11re dedup guard must run before the dispatch stamp and the claim. Without it the "+
			"same bead gets two implementers.", got, "alpha")
	}

	// The reason assertion: the item is terminal FOR THE DEDUP REASON. Any other
	// failure reason would mean some unrelated gate stopped the dispatch and this
	// test proved nothing about the dedup guard.
	betaItem := admissionFirstItem(t, betaSnapshot)
	if betaItem.Status != queue.ItemStatusFailed {
		t.Errorf("losing item status = %q, want %q", betaItem.Status, queue.ItemStatusFailed)
	}
	if betaItem.LastFailureReason != "cross_queue_duplicate" {
		t.Errorf("losing item LastFailureReason = %q, want %q — a different reason means a different gate "+
			"stopped this dispatch and the dedup guard is untested",
			betaItem.LastFailureReason, "cross_queue_duplicate")
	}
	if betaItem.RunID != nil {
		t.Errorf("losing item carries RunID %v — it was stamped before the dedup guard fired", *betaItem.RunID)
	}

	// The winner is untouched.
	alphaItem := admissionFirstItem(t, alphaSnapshot)
	if alphaItem.Status != queue.ItemStatusDispatched {
		t.Errorf("winning item status = %q, want %q — the dedup guard must fail the loser, not the winner",
			alphaItem.Status, queue.ItemStatusDispatched)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Constraint 5 — the attempts bound stays fused to the dispatch stamp
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionOrder_AttemptsBoundStaysFusedToTheStamp pins the hk-6pspu attempt
// counter to the stamp.
//
// The counter increments in exactly one place: inside the write-locked block,
// after the item is found still pending. That fusion is what makes the budget
// mean "real dispatch attempts". Hoist the increment or the bound above any
// pre-stamp hold gate and a bead the daemon is DELIBERATELY holding burns its
// three attempts and is failed with max_attempts_exceeded, having never run.
//
// The two subtests are the two halves of the property:
//
//   - held: a bead parked by the greenlight gate keeps Attempts at 0 across a
//     window that holds many more ticks than the budget allows.
//   - spends: on the same fixture without the label, each tick reaches the stamp,
//     spends one attempt, and the item is failed at the bound.
//
// The second half is also the positive control for the first. It proves the
// window really does hold more ticks than the budget, so "Attempts stayed 0" is
// a suppression result and not an artifact of a short window.
func TestAdmissionOrder_AttemptsBoundStaysFusedToTheStamp(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-6pspu-attempts-bead"
	const parkedID core.BeadID = "hk-6pspu-parked-bead"

	observe := func(t *testing.T, labels []string) queue.Item {
		t.Helper()
		ledger := newAdmissionLedger()
		ledger.showLabels = labels

		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main",
			queue.Item{BeadID: beadID, Status: queue.ItemStatusPending},
			admissionParkedItem(parkedID),
		))
		deps := daemon.ExportedWorkLoopDeps(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

		var snapshot *queue.Queue
		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() { snapshot = qs.Queue() },
		)
		ledger.assertNoRunPathCalls(t)
		return admissionFirstItem(t, snapshot)
	}

	// Positive control FIRST: a stamp attempt does spend budget, and the window
	// holds more ticks than the budget allows.
	t.Run("a real stamp attempt spends one unit of budget", func(t *testing.T) {
		t.Parallel()
		item := observe(t, nil)
		if item.Attempts != queue.MaxItemAttempts {
			t.Fatalf("Attempts = %d after %v, want %d.\n"+
				"Every tick here reaches the stamp and the claim fails, so each tick must spend one attempt "+
				"until the bound. If this is lower the window held fewer ticks than the budget, and the held "+
				"subtest's \"Attempts stayed 0\" result would prove nothing.",
				item.Attempts, admissionObserveWindow, queue.MaxItemAttempts)
		}
		if item.Status != queue.ItemStatusFailed {
			t.Errorf("item status = %q at the attempts bound, want %q", item.Status, queue.ItemStatusFailed)
		}
		if item.LastFailureReason != "max_attempts_exceeded" {
			t.Errorf("item LastFailureReason = %q, want %q", item.LastFailureReason, "max_attempts_exceeded")
		}
	})

	t.Run("a bead held by a pre-stamp gate spends none", func(t *testing.T) {
		t.Parallel()
		item := observe(t, []string{"needs-greenlight"})
		if item.Attempts != 0 {
			t.Errorf("Attempts = %d for a bead held by the greenlight gate, want 0.\n"+
				"The hk-6pspu counter must stay inside the write-locked stamp block, incremented only where "+
				"the item is found pending. Above any hold gate, a deliberately held bead spends its whole "+
				"budget on ticks that never tried to dispatch it.", item.Attempts)
		}
		if item.Status != queue.ItemStatusPending {
			t.Errorf("held item status = %q, want %q — a hold must not drive the item terminal",
				item.Status, queue.ItemStatusPending)
		}
		if item.LastFailureReason == "max_attempts_exceeded" {
			t.Error("held item was failed at the attempts bound — the bound is no longer fused to the stamp")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Constraint 7 — the br-ready path bounds attempts BEFORE handler-pause
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionOrder_ReadyPathBoundsAttemptsBeforeHandlerPause pins the one
// place the two dispatch paths order the same two gates differently.
//
// On the br-ready path the hk-6pspu attempts bound runs BEFORE the hk-kac8g
// handler-pause gate. On the queue path there is no early attempts bound at all
// (its budget is spent at the stamp — see the test above). A fold that merges the
// two orders into one changes the behavior of one path.
//
// The order is observable through the held event. Once a br-ready bead is over
// its attempt budget:
//
//   - bound first: the bead is skipped before the pause gate is reached, so no
//     queue_item_held_for_handler_pause is ever emitted.
//   - pause first: the pause gate is reached and emits the held event.
//
// The fake ledger arms the pause from inside ShowBead, on the call that exhausts
// the budget. That timing matters. Armed any earlier, the pause gate would emit a
// held event on a tick where the bead was still within budget, and the count
// would no longer say anything about the order.
func TestAdmissionOrder_ReadyPathBoundsAttemptsBeforeHandlerPause(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-6pspu-ready-path-bead"

	// observe runs one br-ready fixture and returns the ShowBead count plus the
	// number of held events emitted.
	//
	// armPauseAfter is the ShowBead call on which the handler pause is armed. Zero
	// means "arm before the loop starts".
	observe := func(t *testing.T, showFails bool, armPauseAfter int) (showCalls, heldEvents int) {
		t.Helper()
		ledger := newAdmissionLedger()
		if showFails {
			ledger.setShowErr(errAdmissionShowFailed)
		}
		ledger.readyResult = []core.BeadRecord{{BeadID: beadID, Status: core.CoarseStatusOpen}}

		pause := newAdmissionPauseController(t)
		armPause := func() {
			cause := core.HandlerPauseCause{
				FailureClass: core.FailureClassTransient,
				SubReason:    "rate_limit",
				SourceRunID:  "admission-order-run",
				SourceBeadID: string(beadID),
				TrippedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			}
			if err := pause.Pause(context.Background(), core.AgentTypeClaudeCode, cause, nil); err != nil {
				t.Errorf("arm handler pause: %v", err)
			}
		}
		if armPauseAfter == 0 {
			armPause()
		} else {
			var once sync.Once
			ledger.setShowHook(func(total int) {
				if total >= armPauseAfter {
					once.Do(armPause)
				}
			})
		}

		// An EMPTY queue store. The loop still needs one for the wake channel the
		// tick pump drives, and with zero queues loaded selection finds nothing and
		// falls through to the br-ready path.
		qs := daemon.ExportedNewQueueStore()
		bus := &stubEventCollector{}
		params := admissionDeps(t, ledger, qs, &admissionQueueLedger{}, false, pause)
		params.Bus = bus
		deps := daemon.ExportedWorkLoopDeps(params)

		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() {},
		)
		ledger.assertNoRunPathCalls(t)

		held := 0
		for _, name := range bus.eventTypes() {
			if name == string(core.EventTypeQueueItemHeldForHandlerPause) {
				held++
			}
		}
		return ledger.showCount(beadID), held
	}

	// Positive control FIRST: with the pause armed from the start and the bead
	// always within budget, the pause gate does emit the held event on this
	// fixture. Without this the negative result below could just mean the pause
	// controller was never wired.
	t.Run("a br-ready bead within budget is held for handler pause", func(t *testing.T) {
		t.Parallel()
		_, held := observe(t, false, 0)
		if held == 0 {
			t.Fatal("no queue_item_held_for_handler_pause event with the pause armed and the bead within " +
				"budget. The negative subtest below asserts the ABSENCE of this event, which is only " +
				"evidence if this fixture can produce it.")
		}
	})

	t.Run("a br-ready bead over budget is skipped before the pause gate", func(t *testing.T) {
		t.Parallel()
		// ShowBead fails every time, so each tick spends one attempt. The pause is
		// armed on the call that exhausts the budget.
		showCalls, held := observe(t, true, queue.MaxItemAttempts)
		if showCalls != queue.MaxItemAttempts {
			t.Fatalf("ShowBead called %d time(s), want exactly %d. The bead's br-ready budget is spent one "+
				"unit per failed ShowBead, so a different count means the bound was never reached and the "+
				"held-event assertion below is meaningless.", showCalls, queue.MaxItemAttempts)
		}
		if held != 0 {
			t.Errorf("%d queue_item_held_for_handler_pause event(s) for a bead already over its br-ready "+
				"attempt budget, want 0.\n"+
				"On the br-ready path the hk-6pspu bound runs BEFORE the hk-kac8g pause gate, so an "+
				"over-budget bead is skipped and never reaches the pause gate. A held event means the two "+
				"gates swapped — which is the queue path's order, not this one.", held)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Constraint 8 — the two delay variants are not interchangeable
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionOrder_TerminalDedupLeavesAWakeTokenPending pins the fact that
// decides how the two delay variants may be merged.
//
// runWorkLoop ends a tick in one of two ways. Twenty-six sites call
// workloopSleep (or an idle wait) and then continue. Five continue with NO
// sleep: the queue bootstrap, the hk-pina9 pre-claim ShowBead bound, the
// cross-queue duplicate, the hk-6pspu max-attempts stamp, and the hk-n91y0
// claim-blocked path. Step 3 wants one delay(reason) result, which forces a
// choice about those five.
//
// The load-bearing fact is that FOUR of the five reach the no-sleep continue
// through evaluateGroupAdvanceWithOutcome, which calls queueStore.Wake()
// unconditionally on its not-all-succeeded branch. workloopSleep selects on that
// same wake channel, so a sleep placed at those four continues returns AT ONCE.
// Merging them toward the sleeping variant therefore costs zero latency, not one
// poll interval.
//
// This test measures the token, with no wall clock anywhere. It drains the wake
// channel after fixture setup, drives the cross-queue duplicate, waits for the
// loop to EXIT, and then reads the channel. The loop cannot have consumed the
// token: the dedup continue does not sleep, and the tick after it returns at the
// dispatch-halt check, so no workloopSleep runs at all. Shutdown adds no token
// either — drainCancelledQueue uses ClearQueueByName, which does not wake.
//
// The drain before the run is the control. Without it a token left over from the
// fixture's own SetQueue calls would satisfy the assertion and it would prove
// nothing about the dedup path.
//
// # What this does NOT test, and why no test can
//
// Merging the other way — every delay becomes the no-sleep variant — is the
// direction that is genuinely wrong. It busy-spins the hk-403fw cooldown, which
// exists to stop the bead_claim_skipped storm the 2.5-second spin produced. No
// test can show that, because there is no code shape in the tree that does it.
// It is an argument about a change nobody has made, so it belongs in the record
// rather than in an assertion. See OPEN-DEFECTS.md.
func TestAdmissionOrder_TerminalDedupLeavesAWakeTokenPending(t *testing.T) {
	t.Parallel()

	const sharedBead core.BeadID = "hk-a11re-waketoken-bead"

	ledger := newAdmissionLedger()

	// Same two-queue shape as the dedup test, with ONE difference: beta holds no
	// parked filler item. Its group therefore reaches all-terminal when the dedup
	// guard fails the only item, the queue goes paused-by-failure, and
	// evaluateGroupAdvanceWithOutcome fires cancelOnQueueExit. That is what stops
	// the loop before it can sleep and consume the token this test reads.
	alphaRunID := "alpha-waketoken-run-id"
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(admissionQueue("alpha", queue.Item{
		BeadID: sharedBead,
		Status: queue.ItemStatusDispatched,
		RunID:  &alphaRunID,
	}))
	qs.SetQueue(admissionQueue("beta", queue.Item{BeadID: sharedBead, Status: queue.ItemStatusPending}))

	stopCtx, stopDispatch := context.WithCancel(context.Background())
	defer stopDispatch()

	params := admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil)
	params.StopDispatchCtx = stopCtx
	params.CancelOnQueueExit = stopDispatch
	deps := daemon.ExportedWorkLoopDeps(params)

	// The control: empty the wake channel so any token observed at the end was put
	// there by the run.
	drainWake(qs)
	if pendingWake(qs) {
		t.Fatal("wake channel still holds a token after draining, so the assertion below could not " +
			"attribute a token to the dispatch path")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		daemon.ExportedRunWorkLoop(ctx, deps) //nolint:errcheck,gosec // G104: background loop; error unactionable here
	}()

	// No wake pump here. The loop must reach the dedup guard on its FIRST tick and
	// then exit, so it needs no wake to make progress and cannot consume a token.
	select {
	case <-loopDone:
	case <-time.After(20 * time.Second):
		cancel()
		t.Fatal("work loop did not exit after the cross-queue duplicate drove beta's queue terminal")
	}

	ledger.assertNoRunPathCalls(t)
	if got := ledger.claimCount(sharedBead); got != 0 {
		t.Errorf("ClaimBead called %d time(s) for the duplicate bead, want 0 — the fixture did not take "+
			"the dedup path, so the token below says nothing about it", got)
	}
	if !pendingWake(qs) {
		t.Error("no wake token pending after the cross-queue duplicate drove the item terminal.\n" +
			"evaluateGroupAdvanceWithOutcome calls queueStore.Wake() on its not-all-succeeded branch, and " +
			"workloopSleep selects on that channel. The token is what makes a sleep at this continue return " +
			"at once, which is why merging the no-sleep sites toward the sleeping variant costs no latency. " +
			"If this is gone, that reasoning is void and the delay merge needs re-deriving.")
	}
}

// drainWake empties the queue store's wake channel.
//
// The channel has a buffer of one, so a single non-blocking receive is enough.
// The loop is the channel's only other consumer, so callers must drain before
// starting it.
func drainWake(qs *queuewiring.QueueStore) {
	select {
	case <-qs.WakeCh():
	default:
	}
}

// pendingWake reports whether a wake token is waiting, WITHOUT consuming it in
// the false case. It does consume the token when one is present, so call it once
// per observation.
func pendingWake(qs *queuewiring.QueueStore) bool {
	select {
	case <-qs.WakeCh():
		return true
	default:
		return false
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Constraint 3, second clause — the local-cap guard reads a PRE-increment count
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount pins the half of
// constraint 3 that l5saf_localonly_strand_test.go structurally cannot see.
//
// Constraint 3 has two clauses. The first — the local-cap guard runs before the
// Phase-3 stamp — is pinned by that test. The second is the hoist's own safety
// argument: localInFlight MUST NOT be incremented before the guard, because the
// guard's pre-stamp read is only safe while it stays below gateMax through
// dispatch. Move deps.localInFlight.Add(1) above the guard and a local-only bead
// one slot below the cap is refused forever.
//
// # Why the existing test cannot catch it
//
// l5saf preloads the counter to gateMax with gateMax = 1, so the guard reads
// 1 >= 1 and holds. Hoisting the increment makes it read 2 >= 1, which is the
// SAME branch: the item stays pending and that test stays green. Its fixture sits
// on the saturated side of the boundary, so it cannot see the boundary move.
//
// # What this test does instead
//
// It sits one slot BELOW the cap, which is the side where the increment's
// position changes the answer. gateMax is 2 and the counter is preloaded to 1, so
// the guard must read 1 >= 2 and admit. The claim then fails, which is what keeps
// the counter still: the real increment is post-claim, so it never runs, and the
// bead is retried until its dispatch budget is spent.
//
// The assertion is therefore an exact, saturating count rather than anything
// timing-dependent. Two claims land, then the third stamp attempt hits the
// attempts bound and fails the item before reaching a claim. Extra ticks cannot
// push the number higher.
//
// With the increment hoisted above the guard the first tick takes the counter to
// gateMax, the guard refuses, and the primary capacity gate parks the loop from
// the next tick on. Zero claims.
func TestAdmissionOrder_LocalCapGuardReadsThePreIncrementCount(t *testing.T) {
	t.Parallel()

	const beadID core.BeadID = "hk-l5saf-preincrement-bead"
	const gateMax = 2

	ledger := newAdmissionLedger()

	// A LOCAL-ONLY queue, so capturedQueueLocalOnly is true and the guard applies.
	q := admissionQueue("main", queue.Item{BeadID: beadID, Status: queue.ItemStatusPending})
	q.LocalOnly = true

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	qLedger := &admissionQueueLedger{}
	params := admissionDeps(t, ledger, qs, qLedger, true, nil)
	params.MaxConcurrent = gateMax
	deps := daemon.ExportedWorkLoopDeps(params)

	// One slot below the cap. No worker registry, so the primary split gate at
	// Step 2 passes on the local branch alone and the secondary local-cap guard is
	// the only thing that can refuse this bead.
	daemon.ExportedStoreLocalInFlight(deps, gateMax-1)

	runAdmissionLoop(t, qs,
		func(c context.Context) {
			daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
		},
		func() {},
	)
	ledger.assertNoRunPathCalls(t)
	qLedger.assertUnused(t)

	// MaxItemAttempts-1 claims: each tick stamps and claims, the claim fails and
	// the item reverts, and the stamp attempt that reaches the bound fails the item
	// before it can claim again.
	wantClaims := queue.MaxItemAttempts - 1
	if got := ledger.claimCount(beadID); got != wantClaims {
		t.Errorf("ClaimBead called %d time(s) for a local-only bead one slot below the cap, want %d.\n"+
			"The local-cap guard must read the count as it stands BEFORE this dispatch increments it. "+
			"Zero claims means deps.localInFlight.Add(1) now runs above the guard, so the guard sees the "+
			"slot this very bead is about to take and refuses it — the hoist's safety argument depends on "+
			"that increment staying post-claim.", got, wantClaims)
	}
}
