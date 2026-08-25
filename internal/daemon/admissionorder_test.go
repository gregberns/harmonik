package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

const (
	admissionWakePumpInterval = 2 * time.Millisecond

	admissionObserveWindow = 600 * time.Millisecond

	admissionMinTicks = 4
)

var errAdmissionClaimRefused = errors.New("admission-order fake: claim refused")

var errAdmissionShowFailed = errors.New("admission-order fake: br show failed")

type admissionLedger struct {
	mu sync.Mutex

	// showStatus is the coarse status every ShowBead reply carries.
	showStatus core.CoarseStatus

	// showLabels are the labels every ShowBead reply carries. The greenlight gate
	// reads labels off the pre-claim record, so this field is the ONLY route by
	// which the needs-greenlight label reaches the loop.
	showLabels []string

	// showLabelsByBead overrides showLabels for the named beads. A fixture that
	// needs ONE labeled bead beside an unlabeled sibling cannot express that
	// through showLabels, which every reply shares. A bead absent from this map
	// falls back to showLabels. Bead ref: hk-nown4.
	showLabelsByBead map[core.BeadID][]string

	// showErr, when set, makes every ShowBead fail.
	showErr error

	// readyResult is what Ready returns. Empty for every queue-path test.
	readyResult []core.BeadRecord

	// onShowBead, when set, runs after each ShowBead reply is counted and
	// receives the new total. The br-ready test uses it to arm a handler pause at
	// the exact tick the attempt budget runs out.
	onShowBead func(total int)

	showTotal      int
	showCalls      map[core.BeadID]int
	claimCalls     map[core.BeadID]int
	claimErr       error
	lastClaimRunID core.RunID
	lastClaimTID   core.TransitionID

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
	perBead, hasPerBead := l.showLabelsByBead[id]
	labels := append([]string(nil), l.showLabels...)
	if hasPerBead {
		labels = append([]string(nil), perBead...)
	}
	showErr := l.showErr
	hook := l.onShowBead
	l.mu.Unlock()

	if hook != nil {
		hook(total)
	}
	if showErr != nil {
		return core.BeadRecord{}, showErr
	}
	return core.BeadRecord{BeadID: id, Status: status, Labels: labels}, nil
}

func (l *admissionLedger) ClaimBead(_ context.Context, _ string, _ brcli.TimeoutConfig, runID core.RunID, transitionID core.TransitionID, id core.BeadID) error {
	l.mu.Lock()
	l.claimCalls[id]++
	l.lastClaimRunID = runID
	l.lastClaimTID = transitionID
	l.mu.Unlock()
	if l.claimErr != nil {
		return l.claimErr
	}
	return errAdmissionClaimRefused
}

func (l *admissionLedger) lastClaimIdentity() (core.RunID, core.TransitionID) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastClaimRunID, l.lastClaimTID
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

func admissionQueue(name string, items ...queue.Item) *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
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

func admissionParkedItem(id core.BeadID) queue.Item {
	return queue.Item{
		BeadID:   id,
		Status:   queue.ItemStatusPending,
		Attempts: queue.MaxItemAttempts,
	}
}

func admissionDeps(t *testing.T, ledger *admissionLedger, qs *queuewiring.QueueStore, qLedger queue.BeadLedger, noAutoPull bool, pause *daemon.HandlerPauseController) daemon.TestRuntimeParams {
	t.Helper()
	return admissionDepsWithBus(t, ledger, qs, qLedger, noAutoPull, pause, &stubEventCollector{})
}

func admissionDepsWithBus(t *testing.T, ledger *admissionLedger, qs *queuewiring.QueueStore, qLedger queue.BeadLedger, noAutoPull bool, pause *daemon.HandlerPauseController, bus *stubEventCollector) daemon.TestRuntimeParams {
	t.Helper()
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	return daemon.TestRuntimeParams{
		BrAdapter:              ledger,
		Bus:                    bus,
		ProjectDir:             projectDir,
		HandlerBinary:          "/bin/sh",
		HandlerArgs:            []string{"-c", "exit 0"},
		IntentLogDir:           filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2:       NewSealedAdapterRegistryForTest(t),
		QueueStore:             qs,
		QueueLedger:            qLedger,
		NoAutoPull:             noAutoPull,
		HandlerPauseController: pause,
	}
}

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
	awaitLoopTeardown(t, loopDone, "admission-order work loop")
	<-pumpDone
}

// TestAdmissionOrder_DiskLowLatchSkipsClaim proves the Stage 5 disk port does
// more than make claiming absent. The counted probe shows maintenance ran. The
// low result sets the latch, and the latch keeps ClaimBead out of the queue
// path. Both cleanup seams are stubs, so this test cannot touch shared caches
// or remove a worktree.
func TestAdmissionOrder_DiskLowLatchSkipsClaim(t *testing.T) {
	const beadID core.BeadID = "hk-stage5-disk-low-latch"

	ledger := newAdmissionLedger()
	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(admissionQueue("main", queue.Item{BeadID: beadID, Status: queue.ItemStatusPending}))
	deps := daemon.ExportedTestRuntime(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

	var callsMu sync.Mutex
	probeCalls := 0
	reclaimCalls := 0
	diskReclaim := daemon.ExportedDiskReclaimPortForTesting(deps, time.Nanosecond,
		func(string) (uint64, error) {
			callsMu.Lock()
			probeCalls++
			callsMu.Unlock()
			return 0, nil
		},
		func(context.Context, string, []string) error {
			callsMu.Lock()
			reclaimCalls++
			callsMu.Unlock()
			return nil
		},
	)

	runAdmissionLoop(t, qs,
		func(ctx context.Context) {
			daemon.ExportedRunWorkLoopWithDiskReclaim(ctx, deps, diskReclaim) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
		},
		func() {},
	)

	callsMu.Lock()
	probes, reclaims := probeCalls, reclaimCalls
	callsMu.Unlock()
	if probes == 0 {
		t.Fatal("disk probe did not run; zero ClaimBead calls alone would not prove the latch")
	}
	if reclaims != 0 {
		t.Errorf("worktree reclaim stub called %d time(s), want 0 with no stale worktrees", reclaims)
	}
	if claims := ledger.claimCount(beadID); claims != 0 {
		t.Errorf("disk-low latch allowed ClaimBead %d time(s), want 0", claims)
	}
	ledger.assertNoRunPathCalls(t)
}

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

func newAdmissionPauseController(t *testing.T) *daemon.HandlerPauseController {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("newAdmissionPauseController: bus.Seal: %v", err)
	}
	return daemon.NewHandlerPauseController(bus, nil)
}

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

	observe := func(t *testing.T, status core.CoarseStatus) (showCalls, ticks int) {
		t.Helper()
		ledger := newAdmissionLedger()
		ledger.setStatus(status)

		qLedger := &admissionQueueLedger{}
		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main", queue.Item{BeadID: beadID, Status: queue.ItemStatusPending}))

		reg := runregistry.NewRunRegistry()
		reg.Register(core.RunID(uuid.New()), &runregistry.RunHandle{BeadID: beadID})
		resetter := &admissionResetter{}

		var tickMu sync.Mutex
		tickCount := 0
		params := admissionDeps(t, ledger, qs, qLedger, true, nil)
		params.RunRegistry = reg
		params.StrandedInProgressResetter = resetter
		deps := daemon.ExportedTestRuntime(params)
		diskReclaim := daemon.ExportedDiskReclaimPortForTesting(deps, time.Nanosecond,
			func(string) (uint64, error) {
				tickMu.Lock()
				tickCount++
				tickMu.Unlock()
				return 1 << 62, nil
			},
			func(context.Context, string, []string) error { return nil },
		)

		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoopWithDiskReclaimAndTestPorts(c, deps, diskReclaim, params) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
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

	observe := func(t *testing.T, labels []string) (showCalls, claimCalls int, item queue.Item) {
		t.Helper()
		ledger := newAdmissionLedger()
		ledger.showLabels = labels

		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main",
			queue.Item{BeadID: beadID, Status: queue.ItemStatusPending},
			admissionParkedItem(parkedID),
		))
		deps := daemon.ExportedTestRuntime(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

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

		if showCalls == 0 {
			t.Fatal("ShowBead was never called, so the needs-greenlight label never entered the loop. " +
				"Any hold observed below would be evidence about something else.")
		}
		if claimCalls != 0 {
			t.Errorf("ClaimBead called %d time(s) for a bead carrying needs-greenlight, want 0.\n"+
				"The greenlight gate (hk-lacr) reads preClaimRecord.Labels. It must run AFTER the pre-claim ShowBead "+
				"fills that record. Above it the record is the zero value, the gate finds no labels, and it "+
				"silently never fires — which compiles clean.", claimCalls)
		}
		if item.Status != queue.ItemStatusPending {
			t.Errorf("held item status = %q, want %q — the greenlight gate must defer without stamping",
				item.Status, queue.ItemStatusPending)
		}
		if item.RunID != nil {
			t.Errorf("held item carries RunID %v — a run was stamped for a bead that must not dispatch", *item.RunID)
		}
	})
}

// TestAdmissionOrder_CrossQueueDedupPrecedesTheClaim pins the hk-a11re
// cross-queue dedup guard.
//
// One bead sits in two queues. The winning queue's item is already stamped
// dispatched. The dedup guard must see that stamp and stop the losing queue's
// item WITHOUT claiming the bead. Without the guard the same bead gets two
// implementers, which is the bug hk-a11re fixed.
//
// # What the loser's item does, and why it changed
//
// The guard used to write the loser's item terminally FAILED. That parked the
// loser's whole queue — one failed item takes its group to complete-with-
// failures — over a bead that had done nothing wrong, and an operator could not
// undo it while the winner still held the bead. specs/queue-model.md §9.8 QM-067
// names this exact case as a per-tick REFUSAL and forbids making it durable:
// "eligibility is a property of the queue item, and a refusal is a property of
// this tick." So the loser now stays pending, and the queue goes on dispatching
// the items behind it. Bead ref: hk-nsion.
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
	const behindID core.BeadID = "hk-a11re-behind-bead"

	ledger := newAdmissionLedger()

	alphaRunID := "alpha-run-id"
	alpha := admissionQueue("alpha", queue.Item{
		BeadID: sharedBead,
		Status: queue.ItemStatusDispatched,
		RunID:  &alphaRunID,
	})

	beta := admissionQueue("beta",
		queue.Item{BeadID: sharedBead, Status: queue.ItemStatusPending},
		queue.Item{BeadID: behindID, Status: queue.ItemStatusPending},
	)

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(alpha)
	qs.SetQueue(beta)

	bus := &stubEventCollector{}
	deps := daemon.ExportedTestRuntime(admissionDepsWithBus(t, ledger, qs, &admissionQueueLedger{}, true, nil, bus))

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

	if got := ledger.claimCount(sharedBead); got != 0 {
		t.Errorf("ClaimBead called %d time(s) for a bead queue %q had already dispatched, want 0.\n"+
			"The hk-a11re dedup guard must run before the dispatch stamp and the claim. Without it the "+
			"same bead gets two implementers.", got, "alpha")
	}

	if got := ledger.claimCount(behindID); got == 0 {
		t.Errorf("ClaimBead was never called for the bead sitting BEHIND the refused one.\n" +
			"Either the loop never reached the dedup guard — in which case every assertion here is empty — or the " +
			"refusal cost the queue its turn, which is the head-of-line stall §9.8 QM-067 forbids.")
	}

	betaItem := admissionFirstItem(t, betaSnapshot)
	if betaItem.Status != queue.ItemStatusPending {
		t.Errorf("losing item status = %q, want %q — a refusal belongs to the tick, so it must leave the item "+
			"eligible (§9.8 QM-067, hk-nsion)", betaItem.Status, queue.ItemStatusPending)
	}
	if betaItem.LastFailureReason != "" {
		t.Errorf("losing item LastFailureReason = %q, want empty — nothing failed, so nothing may claim it did",
			betaItem.LastFailureReason)
	}
	if betaItem.Attempts != 0 {
		t.Errorf("losing item Attempts = %d, want 0 — losing another queue's race must not spend this item's "+
			"dispatch budget", betaItem.Attempts)
	}
	if betaItem.RunID != nil {
		t.Errorf("losing item carries RunID %v — it was stamped before the dedup guard fired", *betaItem.RunID)
	}
	if betaSnapshot.Status != queue.QueueStatusActive {
		t.Errorf("losing QUEUE status = %q, want %q — parking the queue is the cost the terminal failure carried, "+
			"and it stops every unrelated item behind the collision", betaSnapshot.Status, queue.QueueStatusActive)
	}

	collisions := admissionCollisionPayloads(t, bus)
	if len(collisions) != 1 {
		t.Fatalf("%d cross_queue_collision events, want exactly 1 naming both queues: %+v", len(collisions), collisions)
	}
	if collisions[0].LosingQueue != "beta" || collisions[0].WinningQueue != "alpha" {
		t.Errorf("collision names losing=%q winning=%q, want losing=%q winning=%q",
			collisions[0].LosingQueue, collisions[0].WinningQueue, "beta", "alpha")
	}
	if collisions[0].Disposition != core.CrossQueueCollisionRefused {
		t.Errorf("collision disposition = %q, want %q", collisions[0].Disposition, core.CrossQueueCollisionRefused)
	}

	alphaItem := admissionFirstItem(t, alphaSnapshot)
	if alphaItem.Status != queue.ItemStatusDispatched {
		t.Errorf("winning item status = %q, want %q — the dedup guard must stop the loser, not the winner",
			alphaItem.Status, queue.ItemStatusDispatched)
	}
}

func admissionCollisionPayloads(t *testing.T, bus *stubEventCollector) []core.CrossQueueCollisionPayload {
	t.Helper()
	events := bus.allEvents()
	out := make([]core.CrossQueueCollisionPayload, 0, len(events))
	for _, evt := range events {
		if evt.EventType != string(core.EventTypeCrossQueueCollision) {
			continue
		}
		var payload core.CrossQueueCollisionPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal cross_queue_collision payload: %v", err)
		}
		out = append(out, payload)
	}
	return out
}

// TestAdmissionOrder_CrossQueueFinishedSiblingCompletesTheLoser is the other
// half of the split guard.
//
// A sibling that is still RUNNING can lapse, so its collision is a refusal. A
// sibling that has already FINISHED cannot: the bead is closed and the work is
// done. specs/queue-model.md §3.2b QM-002b Class A says an item whose bead has
// already finished is advanced to COMPLETED, and failing it instead parks the
// queue over work that succeeded.
//
// The two cases were one boolean before hk-nsion, which is why every collision
// ended in a durable failure.
func TestAdmissionOrder_CrossQueueFinishedSiblingCompletesTheLoser(t *testing.T) {
	t.Parallel()

	const sharedBead core.BeadID = "hk-nsion-finished-shared-bead"
	const parkedID core.BeadID = "hk-nsion-finished-parked-bead"

	ledger := newAdmissionLedger()

	alpha := admissionQueue("alpha", queue.Item{BeadID: sharedBead, Status: queue.ItemStatusCompleted})

	beta := admissionQueue("beta",
		queue.Item{BeadID: sharedBead, Status: queue.ItemStatusPending},
		admissionParkedItem(parkedID),
	)

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(alpha)
	qs.SetQueue(beta)

	bus := &stubEventCollector{}
	deps := daemon.ExportedTestRuntime(admissionDepsWithBus(t, ledger, qs, &admissionQueueLedger{}, true, nil, bus))

	var betaSnapshot *queue.Queue
	runAdmissionLoop(t, qs,
		func(c context.Context) {
			daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
		},
		func() { betaSnapshot = qs.QueueByName("beta") },
	)
	ledger.assertNoRunPathCalls(t)

	if got := ledger.claimCount(sharedBead); got != 0 {
		t.Errorf("ClaimBead called %d time(s) for a bead another queue has already finished, want 0 — the work is "+
			"done, so claiming it again reopens finished work", got)
	}

	betaItem := admissionFirstItem(t, betaSnapshot)
	if betaItem.Status != queue.ItemStatusCompleted {
		t.Errorf("losing item status = %q, want %q — the bead is closed, so the item is advanced, not failed "+
			"(§3.2b QM-002b Class A)", betaItem.Status, queue.ItemStatusCompleted)
	}
	if betaItem.LastFailureReason != "" {
		t.Errorf("losing item LastFailureReason = %q, want empty — nothing failed", betaItem.LastFailureReason)
	}
	if betaSnapshot.Status != queue.QueueStatusActive {
		t.Errorf("losing QUEUE status = %q, want %q — a queue must not park over an item whose work succeeded",
			betaSnapshot.Status, queue.QueueStatusActive)
	}

	collisions := admissionCollisionPayloads(t, bus)
	if len(collisions) != 1 || collisions[0].Disposition != core.CrossQueueCollisionCompleted {
		t.Errorf("collision events = %+v, want exactly one with disposition %q",
			collisions, core.CrossQueueCollisionCompleted)
	}
}

// TestPreClaimTerminalStatusCompletesTheItem is the OTHER detector's half of the
// same rule, and it is here rather than in an ordering constraint because it is
// a disposition: it says what happens to an item, not which gate runs first.
//
// Two detectors in this loop can find that an item's work is already done. The
// cross-queue guard above finds a sibling queue holding the bead completed. The
// BI-013c pre-claim ledger re-read — which runs STRICTLY EARLIER in the same
// loop body, so in a live daemon it usually gets there first — finds the bead
// itself closed or tombstoned. They must agree, and hk-nsion's whole subject is
// that recording finished work as a failure parks a queue over work that
// succeeded.
//
// This one is worse than the case hk-nsion fixed. A cross-queue collision lapses
// when the sibling's run ends, so `queue recover` eventually works. A closed
// bead never reopens, and §8.3b QM-052b refuses to recover a queue whose bead is
// not open — so the old durable failure here could not be undone at all.
//
// §3.2b QM-002b Class A states the rule: an item whose bead the ledger shows as
// closed or tombstone "is waiting for a bead that has already finished", and the
// daemon MUST advance it to completed. The startup reconciliation pass has
// always done exactly that to the same item, so the old dispatch-time answer
// also meant a daemon restart silently flipped the item from failed to
// completed.
//
// Bead ref: hk-rern1, hk-nsion.
func TestPreClaimTerminalStatusCompletesTheItem(t *testing.T) {
	t.Parallel()

	for _, status := range []core.CoarseStatus{core.CoarseStatusClosed, core.CoarseStatusTombstone} {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()

			beadID := core.BeadID("hk-rern1-terminal-" + string(status))
			parkedID := core.BeadID("hk-rern1-parked-" + string(status))

			ledger := newAdmissionLedger()
			ledger.setStatus(status)

			qs := daemon.ExportedNewQueueStore()
			qs.SetQueue(admissionQueue("main",
				queue.Item{BeadID: beadID, Status: queue.ItemStatusPending},
				admissionParkedItem(parkedID),
			))

			bus := &stubEventCollector{}
			deps := daemon.ExportedTestRuntime(admissionDepsWithBus(t, ledger, qs, &admissionQueueLedger{}, true, nil, bus))

			var snapshot *queue.Queue
			runAdmissionLoop(t, qs,
				func(c context.Context) {
					daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
				},
				func() { snapshot = qs.QueueByName("main") },
			)
			ledger.assertNoRunPathCalls(t)

			if got := admissionSkippedStatuses(t, bus); len(got) == 0 || got[0] != string(status) {
				t.Fatalf("bead_claim_skipped observed_status values = %v, want the first to be %q — the fixture "+
					"did not reach the BI-013c pre-claim guard, so nothing below is evidence about it", got, status)
			}
			if got := ledger.claimCount(beadID); got != 0 {
				t.Errorf("ClaimBead called %d time(s) for a %s bead, want 0", got, status)
			}

			item := admissionFirstItem(t, snapshot)
			if item.Status != queue.ItemStatusCompleted {
				t.Errorf("item status = %q, want %q — the bead has already finished, so the item is advanced, "+
					"not failed (§3.2b QM-002b Class A, hk-rern1)", item.Status, queue.ItemStatusCompleted)
			}
			if item.LastFailureReason != "" {
				t.Errorf("item LastFailureReason = %q, want empty — nothing failed", item.LastFailureReason)
			}
		})
	}

	t.Run("the queue does not park", func(t *testing.T) {
		t.Parallel()

		const beadID core.BeadID = "hk-rern1-lone-bead"

		ledger := newAdmissionLedger()
		ledger.setStatus(core.CoarseStatusClosed)

		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main", queue.Item{BeadID: beadID, Status: queue.ItemStatusPending}))

		bus := &stubEventCollector{}
		deps := daemon.ExportedTestRuntime(admissionDepsWithBus(t, ledger, qs, &admissionQueueLedger{}, true, nil, bus))

		var snapshot *queue.Queue
		runAdmissionLoop(t, qs,
			func(c context.Context) {
				daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() { snapshot = qs.QueueByName("main") },
		)
		ledger.assertNoRunPathCalls(t)

		if got := admissionSkippedStatuses(t, bus); len(got) == 0 {
			t.Fatal("no bead_claim_skipped event — the fixture did not reach the BI-013c pre-claim guard")
		}
		if snapshot != nil && snapshot.Status == queue.QueueStatusPausedByFailure {
			t.Errorf("queue status = %q, want it not parked — one already-finished bead must not stop every "+
				"unrelated item behind it, and a closed bead makes that park unrecoverable (§8.3b QM-052b)",
				snapshot.Status)
		}
	})
}

func admissionSkippedStatuses(t *testing.T, bus *stubEventCollector) []string {
	t.Helper()
	events := bus.allEvents()
	out := make([]string, 0, len(events))
	for _, evt := range events {
		if evt.EventType != string(core.EventTypeBeadClaimSkipped) {
			continue
		}
		var payload core.BeadClaimSkippedPayload
		if err := json.Unmarshal(evt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal bead_claim_skipped payload: %v", err)
		}
		out = append(out, payload.ObservedStatus)
	}
	return out
}

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
		deps := daemon.ExportedTestRuntime(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))

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

// TestClaimFailureRoutingUsesTypedRefusal proves that claim policy does not
// depend on words in an error message. The paired cases reach the same real
// queue reservation window.
func TestClaimFailureRoutingUsesTypedRefusal(t *testing.T) {
	const beadID core.BeadID = "claim-routing-bead"
	const parkedID core.BeadID = "claim-routing-parked"

	observe := func(t *testing.T, claimErr error) (queue.Item, int, core.RunID, core.TransitionID) {
		t.Helper()
		ledger := newAdmissionLedger()
		ledger.claimErr = claimErr
		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main",
			queue.Item{BeadID: beadID, Status: queue.ItemStatusPending},
			admissionParkedItem(parkedID),
		))
		deps := daemon.ExportedTestRuntime(admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil))
		var snapshot *queue.Queue
		runAdmissionLoop(t, qs,
			//nolint:errcheck,gosec // the loop's error is the cancel this test causes; the assertions below read the queue, not the return.
			func(c context.Context) { daemon.ExportedRunWorkLoop(c, deps) },
			func() { snapshot = qs.Queue() },
		)
		ledger.assertNoRunPathCalls(t)
		runID, transitionID := ledger.lastClaimIdentity()
		return admissionFirstItem(t, snapshot), ledger.claimCount(beadID), runID, transitionID
	}

	t.Run("typed dependency refusal makes the queue item terminal", func(t *testing.T) {
		item, claims, claimedRunID, claimedTransitionID := observe(t, fmt.Errorf("wording may change: %w", brcli.ErrClaimDependencyBlocked))
		if item.Status != queue.ItemStatusFailed {
			t.Fatalf("item status = %q, want failed", item.Status)
		}
		if claims != 1 {
			t.Fatalf("ClaimBead calls = %d, want 1 after terminal disposition", claims)
		}
		if item.PreclaimTerminal == nil || item.PreclaimTerminal.Cause != queue.PreclaimTerminalDependencyRefusal {
			t.Fatalf("preclaim terminal binding = %+v", item.PreclaimTerminal)
		}
		if item.RunID == nil || *item.RunID != item.PreclaimTerminal.RunID || item.PreclaimTerminal.ClaimTransitionID == "" {
			t.Fatalf("dependency refusal identity = item run %v binding %+v", item.RunID, item.PreclaimTerminal)
		}
		if item.PreclaimTerminal.RunID != claimedRunID.String() || item.PreclaimTerminal.ClaimTransitionID != claimedTransitionID.String() {
			t.Fatalf("binding %+v does not match ClaimBead run=%s transition=%s", item.PreclaimTerminal, claimedRunID, claimedTransitionID)
		}
	})

	t.Run("unrelated blocked text releases and retries", func(t *testing.T) {
		item, claims, _, _ := observe(t, errors.New("database operation blocked by lock timeout"))
		if claims < 2 {
			t.Fatalf("ClaimBead calls = %d, want at least 2 to prove release and retry", claims)
		}
		if item.Attempts != queue.MaxItemAttempts {
			t.Fatalf("Attempts = %d, want %d after retries", item.Attempts, queue.MaxItemAttempts)
		}
	})
}

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

		qs := daemon.ExportedNewQueueStore()
		bus := &stubEventCollector{}
		params := admissionDeps(t, ledger, qs, &admissionQueueLedger{}, false, pause)
		params.Bus = bus
		deps := daemon.ExportedTestRuntime(params)

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

// TestAdmissionOrder_TerminalStampLeavesAWakeTokenPending pins the fact that
// decides how the two delay variants may be merged.
//
// runWorkLoop ends a tick in one of two ways. Twenty-six sites call
// workloopSleep (or an idle wait) and then continue. Five continue with NO
// sleep: the queue bootstrap, the hk-pina9 pre-claim ShowBead bound, the
// cross-queue collision, the hk-6pspu max-attempts stamp, and the hk-n91y0
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
// # Why this drives the attempts bound rather than the cross-queue collision
//
// It used to drive the collision, whose loser went terminal on sight. That is
// exactly what hk-nsion removed: a collision is now a per-tick refusal, the
// loser stays pending, its group never reaches all-terminal, and a fixture built
// on it hangs to this test's own 20-second fatal. Only the collision BOUND is
// still terminal, and reaching it takes twelve refusals spaced by a five-minute
// cooldown — an hour of real time, which is not a unit test.
//
// The hk-6pspu max-attempts stamp is another of the same five sites and it
// reaches the same branch: the reservation fails the item inside its own write,
// the loop continues without sleeping, evaluateGroupAdvanceWithOutcome takes the
// group to complete-with-failures, and the queue parks. One item at
// MaxItemAttempts-1 gets there on the FIRST tick.
//
// This test measures the token, with no wall clock anywhere. It drains the wake
// channel after fixture setup, drives the terminal stamp, waits for the loop to
// EXIT, and then reads the channel. The loop cannot have consumed the token: the
// stamp's continue does not sleep, and the tick after it returns at the
// dispatch-halt check, so no workloopSleep runs at all.
//
// Shutdown adds no token either — except that it does, and the assertion is
// degenerate as a result. The shutdown drain (drainQueuesForRestart) parks every
// still-active queue with SetQueueByName, which DOES signal the wake channel.
// This fixture's only queue goes terminal, so nothing is left for the drain to
// park and the exit adds nothing; but that is a property of the fixture rather
// than something the assertion states. Read a green result here as "a token
// exists", not as "the terminal path left one". Filed as
// hk-waketoken-degenerate-06ntc.
//
// The drain before the run is the control. Without it a token left over from the
// fixture's own SetQueue calls would satisfy the assertion and it would prove
// nothing about the terminal path.
//
// # What this does NOT test, and why no test can
//
// Merging the other way — every delay becomes the no-sleep variant — is the
// direction that is genuinely wrong. It busy-spins the hk-403fw cooldown, which
// exists to stop the bead_claim_skipped storm the 2.5-second spin produced. No
// test can show that, because there is no code shape in the tree that does it.
// It is an argument about a change nobody has made, so it belongs in the record
// rather than in an assertion. See OPEN-DEFECTS.md.
func TestAdmissionOrder_TerminalStampLeavesAWakeTokenPending(t *testing.T) {
	t.Parallel()

	const boundBead core.BeadID = "hk-6pspu-waketoken-bead"

	ledger := newAdmissionLedger()

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(admissionQueue("main", queue.Item{
		BeadID:   boundBead,
		Status:   queue.ItemStatusPending,
		Attempts: queue.MaxItemAttempts - 1,
	}))

	stopCtx, stopDispatch := context.WithCancel(context.Background())
	defer stopDispatch()

	params := admissionDeps(t, ledger, qs, &admissionQueueLedger{}, true, nil)
	params.StopDispatchCtx = stopCtx
	params.CancelOnQueueExit = stopDispatch
	deps := daemon.ExportedTestRuntime(params)

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
		daemon.ExportedRunWorkLoopWithTestPorts(ctx, deps, params) //nolint:errcheck,gosec // G104: background loop; error unactionable here
	}()

	select {
	case <-loopDone:
	case <-time.After(20 * time.Second):
		cancel()
		t.Fatal("work loop did not exit after the attempts bound drove the queue terminal")
	}

	ledger.assertNoRunPathCalls(t)
	if got := ledger.claimCount(boundBead); got != 0 {
		t.Errorf("ClaimBead called %d time(s) for a bead the attempts bound refused, want 0 — the fixture did not "+
			"take the terminal-stamp path, so the token below says nothing about it", got)
	}
	if !pendingWake(qs) {
		t.Error("no wake token pending after the attempts bound drove the item terminal.\n" +
			"evaluateGroupAdvanceWithOutcome calls queueStore.Wake() on its not-all-succeeded branch, and " +
			"workloopSleep selects on that channel. The token is what makes a sleep at this continue return " +
			"at once, which is why merging the no-sleep sites toward the sleeping variant costs no latency. " +
			"If this is gone, that reasoning is void and the delay merge needs re-deriving.")
	}
}

func drainWake(qs *queuewiring.QueueStore) {
	select {
	case <-qs.WakeCh():
	default:
	}
}

func pendingWake(qs *queuewiring.QueueStore) bool {
	select {
	case <-qs.WakeCh():
		return true
	default:
		return false
	}
}

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

	q := admissionQueue("main", queue.Item{BeadID: beadID, Status: queue.ItemStatusPending})
	q.LocalOnly = true

	qs := daemon.ExportedNewQueueStore()
	qs.SetQueue(q)

	qLedger := &admissionQueueLedger{}
	params := admissionDeps(t, ledger, qs, qLedger, true, nil)
	params.MaxConcurrent = gateMax
	deps := daemon.ExportedTestRuntime(params)

	daemon.ExportedStoreLocalInFlight(deps, gateMax-1)

	runAdmissionLoop(t, qs,
		func(c context.Context) {
			daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
		},
		func() {},
	)
	ledger.assertNoRunPathCalls(t)
	qLedger.assertUnused(t)

	wantClaims := queue.MaxItemAttempts - 1
	if got := ledger.claimCount(beadID); got != wantClaims {
		t.Errorf("ClaimBead called %d time(s) for a local-only bead one slot below the cap, want %d.\n"+
			"The local-cap guard must read the count as it stands BEFORE this dispatch increments it. "+
			"Zero claims means deps.localInFlight.Add(1) now runs above the guard, so the guard sees the "+
			"slot this very bead is about to take and refuses it — the hoist's safety argument depends on "+
			"that increment staying post-claim.", got, wantClaims)
	}
}
