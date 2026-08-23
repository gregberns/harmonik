package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

type deferralLedger struct {
	mu      sync.Mutex
	blocks  bool
	edgeErr error
	calls   []string
}

func (l *deferralLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, fmt.Sprintf("BlocksEdge(%s,%s)", blocker, blocked))
	if l.edgeErr != nil {
		return false, l.edgeErr
	}
	return l.blocks, nil
}

func (l *deferralLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, fmt.Sprintf("LookupStatus(%s)", id))
	return queue.BeadStatusOpen, nil
}

func (l *deferralLedger) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

var errDeferralLedgerDown = errors.New("deferral fake: ledger cannot answer")

func runDeferralLoop(
	t *testing.T,
	qs *queuewiring.QueueStore,
	window time.Duration,
	wakePump bool,
	runLoop func(context.Context),
	inspect func(),
) {
	t.Helper()

	if !wakePump {
		drainWake(qs)
		if pendingWake(qs) {
			t.Fatal("wake channel still holds a token after draining, so an indefinite idle wait could " +
				"return once and be mistaken for the bounded poll")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), window+20*time.Second)
	defer cancel()

	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		runLoop(ctx)
	}()

	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		if !wakePump {
			return
		}
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

	time.Sleep(window)
	inspect()

	cancel()
	awaitLoopTeardown(t, loopDone, "deferred-reeval work loop")
	<-pumpDone
}

// TestDeferredReevaluation_WritesThroughTheTransactionDomain drives real ticks
// of runWorkLoop over a queue holding one deferred item and one sibling, and
// reads what the re-evaluation pass did to the store and to the disk.
//
// The two subtests are the two halves of one property. The flipping half is the
// positive control for the non-flipping half: without it, "no file was written"
// would also be true of a fixture that never reached the pass at all.
func TestDeferredReevaluation_WritesThroughTheTransactionDomain(t *testing.T) {
	t.Parallel()

	const blockerBead core.BeadID = "hk-nbjht-blocker-bead"
	const blockedBead core.BeadID = "hk-nbjht-blocked-bead"

	deferredItem := func() queue.Item {
		item := admissionParkedItem(blockedBead)
		item.Status = queue.ItemStatusDeferredForLedgerDep
		return item
	}

	type observation struct {
		item queue.Item

		// genBefore and genAfter bracket the observation window. genAfter is read
		// while the loop is still alive, for the same reason the queue snapshot is.
		genBefore uint64
		genAfter  uint64

		ledgerCalls int

		// queueFile holds the canonical queue file as it stood DURING the run, and
		// queueFileFound says whether it existed at all.
		//
		// Both are captured inside the inspect callback rather than after the loop
		// exits. The shutdown drain cancels every active queue and renames
		// main.json to main.json.cancelled-<stamp>, so a check made after the loop
		// returns reads an empty directory and reports "the un-deferral never
		// reached the disk" whether it did or not.
		queueFile      []byte
		queueFileFound bool
	}

	type fixtureOpts struct {
		blocks   bool
		edgeErr  error
		wakePump bool
		window   time.Duration
	}

	observe := func(t *testing.T, opts fixtureOpts) observation {
		t.Helper()

		ledger := newAdmissionLedger()
		qLedger := &deferralLedger{blocks: opts.blocks, edgeErr: opts.edgeErr}

		qs := daemon.ExportedNewQueueStore()
		qs.SetQueue(admissionQueue("main",
			deferredItem(),
			admissionParkedItem(blockerBead),
		))

		params := admissionDeps(t, ledger, qs, qLedger, true, nil)
		deps := daemon.ExportedTestRuntime(params)

		queueFilePath := filepath.Join(params.ProjectDir, ".harmonik", "queues", "main.json")
		obs := observation{genBefore: qs.Snapshot("main").Generation}
		if _, err := os.Stat(queueFilePath); !os.IsNotExist(err) {
			t.Fatalf("%s exists before the loop starts (stat err = %v). The fixture only loads the queue "+
				"into memory, so any file found afterwards must have been written by the loop.",
				queueFilePath, err)
		}

		var snapshot *queue.Queue
		runDeferralLoop(t, qs, opts.window, opts.wakePump,
			func(c context.Context) {
				daemon.ExportedRunWorkLoop(c, deps) //nolint:errcheck,gosec // G104: background loop; returns on ctx cancel
			},
			func() {
				snapshot = qs.QueueByName("main")
				obs.genAfter = qs.Snapshot("main").Generation
				data, err := os.ReadFile(queueFilePath) //nolint:gosec // G304: path built from the test's own temp project dir
				switch {
				case err == nil:
					obs.queueFile = data
					obs.queueFileFound = true
				case os.IsNotExist(err):
				default:
					t.Errorf("read %s: %v", queueFilePath, err)
				}
			},
		)

		ledger.assertNoRunPathCalls(t)
		if got := ledger.claimCount(blockedBead); got != 0 {
			t.Errorf("ClaimBead called %d time(s) for a bead parked over its attempt budget, want 0 — "+
				"the dispatcher reached this queue and the generation reading below no longer belongs "+
				"to the re-evaluation pass alone", got)
		}
		obs.item = admissionFirstItem(t, snapshot)
		obs.ledgerCalls = qLedger.callCount()
		return obs
	}

	t.Run("a resolved blocker un-defers the item and commits through the store", func(t *testing.T) {
		t.Parallel()
		obs := observe(t, fixtureOpts{blocks: false, wakePump: true, window: admissionObserveWindow})

		if obs.ledgerCalls == 0 {
			t.Fatal("the queue ledger was never consulted, so the re-evaluation pass never ran and " +
				"nothing below is evidence about it")
		}
		if obs.item.Status != queue.ItemStatusPending {
			t.Fatalf("item status = %q, want %q — no sibling blocks this item, so §2.8 requires it back "+
				"in pending", obs.item.Status, queue.ItemStatusPending)
		}
		if !obs.queueFileFound {
			t.Fatal("no canonical queue file while the loop was running — the un-deferral never reached " +
				"the disk, so it is lost on the next daemon restart")
		}
		assertPersistedItemStatus(t, obs.queueFile, blockedBead, queue.ItemStatusPending)

		if obs.genAfter <= obs.genBefore {
			t.Errorf("store generation for %q stayed at %d after an item was un-deferred, want it to advance.\n"+
				"The §2.8 pass must commit through QueueStore.Transact, the transaction domain QM-001 names. "+
				"Transact advances the generation once per committed replacement; a direct queue.Persist writes "+
				"the same bytes and leaves the counter where it was. With the counter unmoved, a writer holding "+
				"a pre-flip snapshot still passes the staleness guard and puts the item back to "+
				"deferred-for-ledger-dep.", "main", obs.genAfter)
		}
	})

	t.Run("an open blocker leaves the item deferred and writes nothing", func(t *testing.T) {
		t.Parallel()
		obs := observe(t, fixtureOpts{blocks: true, wakePump: true, window: admissionObserveWindow})

		if obs.ledgerCalls == 0 {
			t.Fatal("the queue ledger was never consulted, so the re-evaluation pass never ran and the " +
				"no-write result below would be true of a fixture that does nothing")
		}
		if obs.item.Status != queue.ItemStatusDeferredForLedgerDep {
			t.Fatalf("item status = %q, want %q — the sibling still blocks it and reads open in the ledger",
				obs.item.Status, queue.ItemStatusDeferredForLedgerDep)
		}
		if obs.queueFileFound {
			t.Errorf("the canonical queue file was written over a window of %d ledger calls in which no "+
				"item flipped.\n"+
				"The §2.8 pass runs on every dispatch tick for the daemon's whole life. It must open a "+
				"transaction only when an item actually leaves deferred-for-ledger-dep, or every idle tick "+
				"rewrites the queue file for no state change.", obs.ledgerCalls)
		}
		if obs.genAfter != obs.genBefore {
			t.Errorf("store generation for %q moved from %d to %d with no item flipped, want it unchanged — "+
				"a generation bump means a replacement was committed", "main", obs.genBefore, obs.genAfter)
		}
	})

	t.Run("a ledger that cannot answer rolls the pass back and keeps the bounded poll", func(t *testing.T) {
		t.Parallel()

		const window = 5 * time.Second

		obs := observe(t, fixtureOpts{edgeErr: errDeferralLedgerDown, window: window})

		if obs.ledgerCalls == 0 {
			t.Fatal("the queue ledger was never consulted, so the re-evaluation pass never ran and nothing " +
				"below is evidence about the rejection branch")
		}
		if obs.item.Status != queue.ItemStatusDeferredForLedgerDep {
			t.Errorf("item status = %q, want %q — the ledger never said the blocker resolved, so a rejected "+
				"transaction must leave the item exactly where it was",
				obs.item.Status, queue.ItemStatusDeferredForLedgerDep)
		}
		if obs.queueFileFound {
			t.Errorf("the canonical queue file was written after the ledger failed.\n" +
				"Transact rejects before any namespace I/O, so a pass that could not read the ledger must " +
				"leave the queue file untouched.")
		}
		if obs.genAfter != obs.genBefore {
			t.Errorf("store generation for %q moved from %d to %d after a rejected transaction, want it "+
				"unchanged — a rejection commits nothing", "main", obs.genBefore, obs.genAfter)
		}

		if obs.ledgerCalls < 2 {
			t.Errorf("the re-evaluation pass ran %d time(s) over %v with no wake source, want at least 2.\n"+
				"An item that is still deferred after a FAILED transaction must keep the loop on the bounded "+
				"poll (hk-gf59k). Exactly one pass means the failed transaction was read as \"nothing is "+
				"deferred any more\", so the loop took the indefinite wait and this queue is now parked until "+
				"something unrelated wakes it.", obs.ledgerCalls, window)
		}
	})
}

func assertPersistedItemStatus(t *testing.T, data []byte, beadID core.BeadID, want queue.ItemStatus) {
	t.Helper()
	q, err := queue.UnmarshalQueue(data)
	if err != nil {
		t.Fatalf("unmarshal persisted queue: %v", err)
	}
	for _, group := range q.Groups {
		for _, item := range group.Items {
			if item.BeadID != beadID {
				continue
			}
			if item.Status != want {
				t.Errorf("persisted item %s status = %q, want %q", beadID, item.Status, want)
			}
			return
		}
	}
	t.Errorf("the persisted queue holds no item for bead %s", beadID)
}
