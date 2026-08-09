package lifecycle

// startup_pl005_qm002_test.go — the boot pass that frees a queue item stranded
// at dispatched (QM-002a, reconcileDispatchedItems).
//
// A release that loses its snapshot race on all three attempts returns
// release_contended and leaves the item at dispatched, naming a run that will
// never execute it. Nothing re-selects a dispatched item, so the daemon tells
// the operator to wait for this pass. If this pass stops working, the item is
// lost in silence and the operator was told to wait for a rescue that never
// comes. That is why the assertions here read the queue file back from disk: a
// revert that lives only in the daemon's memory dies with the next kill, and
// reading memory is how this class of defect stays invisible.
//
// The opposite mistake is worse than the strand. A bead the ledger still shows
// as in progress belongs to a live run. Returning it to pending hands the same
// bead to a second agent. So the revert fires only on a ledger-confirmed open
// bead, and a ledger that cannot answer at all buys no revert either.
//
// Out of scope here: dispatched plus a CLOSED bead advancing to completed. That
// is QM-002b Class A' in reconcileThreeWay, and the daemon-level
// TestScenario_RestartRecovery_QM002bDeadlock owns it.
//
// Spec refs:
//   - specs/queue-model.md §3.2a QM-002a — the Beads cross-check.
//   - specs/queue-model.md §9.3 QM-063 — persist BEFORE emit.
//   - specs/process-lifecycle.md §4.2 PL-005 step 8a.

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// bootRevertBeadID and bootRevertRunID are the fixture's one item and the
// run it still names. The run ID is what proves the strand: it points at a run
// that the lost release means will never execute.
const (
	bootRevertBeadID core.BeadID = "hk-stranded"
	bootRevertRunID  string      = "0190b3c4-7001-7000-8000-00000000a001"
)

// bootRevertLedger answers the cross-check with one fixed verdict for every
// bead. The pass under test branches on nothing else, so one verdict is the
// whole ledger it can see.
type bootRevertLedger struct {
	status  core.CoarseStatus
	showErr error
}

func (f bootRevertLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	if f.showErr != nil {
		return core.BeadRecord{}, f.showErr
	}
	return core.BeadRecord{BeadID: id, Title: "stranded item", Status: f.status}, nil
}

func (bootRevertLedger) ListInFlightBeads(context.Context) ([]core.BeadRecord, error) {
	return nil, nil
}

// bootRevertEmitter records each event AND the item status it can read off
// disk at the moment the event fires. The second half is what makes the QM-063
// persist-before-emit order testable: a subscriber that acts on
// queue_item_reconciled and finds "dispatched" still on disk has been told the
// item is free while the file says otherwise, and a crash in that window loses
// the revert.
type bootRevertEmitter struct {
	projectDir   string
	types        []core.EventType
	statusOnDisk []queue.ItemStatus
}

func (e *bootRevertEmitter) Emit(ctx context.Context, eventType core.EventType, _ []byte) error {
	e.types = append(e.types, eventType)
	onDisk, err := queue.Load(ctx, e.projectDir, queue.QueueNameMain)
	switch {
	case err != nil || onDisk == nil:
		e.statusOnDisk = append(e.statusOnDisk, queue.ItemStatus("<unreadable>"))
	default:
		e.statusOnDisk = append(e.statusOnDisk, onDisk.Groups[0].Items[0].Status)
	}
	return nil
}

// seedStrandedQueue writes the state a contended release leaves behind: an
// active queue whose only item sits at dispatched and still names its run.
func seedStrandedQueue(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()

	runID := bootRevertRunID
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0a01",
		Name:          queue.QueueNameMain,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			Items: []queue.Item{{
				BeadID: bootRevertBeadID,
				Status: queue.ItemStatusDispatched,
				RunID:  &runID,
			}},
		}},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	return projectDir
}

// loadStrandedItem reads the one fixture item back off disk.
func loadStrandedItem(t *testing.T, projectDir string) queue.Item {
	t.Helper()
	onDisk, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("read queue back: %v", err)
	}
	if onDisk == nil {
		t.Fatal("the queue file is gone; the pass under test must leave a queue with pending work on disk")
	}
	if len(onDisk.Groups) != 1 || len(onDisk.Groups[0].Items) != 1 {
		t.Fatalf("want one group with one item, got %d groups", len(onDisk.Groups))
	}
	return onDisk.Groups[0].Items[0]
}

// TestReconcileDispatchedItems_ReturnsAStrandedItemToPending drives the real boot
// entry point, because the promise the daemon makes to the operator is about the
// next boot and not about a function call.
func TestReconcileDispatchedItems_ReturnsAStrandedItemToPending(t *testing.T) {
	t.Parallel()

	projectDir := seedStrandedQueue(t)
	emitter := &bootRevertEmitter{projectDir: projectDir}

	loaded, err := LoadQueueAtStartup(
		context.Background(),
		projectDir,
		bootRevertLedger{status: core.CoarseStatusOpen},
		emitter,
		slog.New(slog.DiscardHandler),
	)
	if err != nil {
		t.Fatalf("LoadQueueAtStartup: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("want the recovered queue handed back to the daemon, got %d queues", len(loaded))
	}

	item := loadStrandedItem(t, projectDir)
	if item.Status != queue.ItemStatusPending {
		t.Fatalf("the item is still %q on disk; nothing re-selects it and the operator was told to wait for this pass",
			item.Status)
	}
	if item.RunID != nil {
		t.Fatalf("the item still names run %q; a later dispatch would mint a second run for the same claim",
			*item.RunID)
	}

	// The event is the operator's only signal that the rescue happened.
	if len(emitter.types) != 1 || emitter.types[0] != core.EventTypeQueueItemReconciled {
		t.Fatalf("want one queue_item_reconciled event, got %v", emitter.types)
	}
	if emitter.statusOnDisk[0] != queue.ItemStatusPending {
		t.Fatalf("the event fired while the file still read %q; QM-063 requires the correction to be durable first",
			emitter.statusOnDisk[0])
	}
}

// TestReconcileDispatchedItems_LeavesADispatchedItemAloneWhenTheBeadIsNotOpen
// calls the pass directly rather than through LoadQueueAtStartup. For a closed
// bead the later QM-002b Class A' pass advances the item to completed and unlinks
// the queue, so a boot-level assertion cannot tell a correct skip here from a
// wrong revert that Class A' then papers over.
func TestReconcileDispatchedItems_LeavesADispatchedItemAloneWhenTheBeadIsNotOpen(t *testing.T) {
	t.Parallel()

	// blocked stands in for every status that is neither open nor terminal. The
	// rule is a whitelist on open, not a blacklist on the two terminal values.
	for _, ledgerStatus := range []core.CoarseStatus{
		core.CoarseStatusInProgress,
		core.CoarseStatusClosed,
		core.CoarseStatusBlocked,
	} {
		t.Run(string(ledgerStatus), func(t *testing.T) {
			t.Parallel()

			projectDir := seedStrandedQueue(t)
			emitter := &bootRevertEmitter{projectDir: projectDir}

			q, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
			if err != nil {
				t.Fatalf("load seeded queue: %v", err)
			}

			if err := reconcileDispatchedItems(
				context.Background(),
				projectDir,
				q,
				bootRevertLedger{status: ledgerStatus},
				emitter,
				slog.New(slog.DiscardHandler),
			); err != nil {
				t.Fatalf("reconcileDispatchedItems: %v", err)
			}

			if got := q.Groups[0].Items[0].Status; got != queue.ItemStatusDispatched {
				t.Fatalf("the pass reopened a bead the ledger reports as %q; in memory the item is now %q, "+
					"which offers a live run's bead to a second agent", ledgerStatus, got)
			}

			item := loadStrandedItem(t, projectDir)
			if item.Status != queue.ItemStatusDispatched {
				t.Fatalf("the pass wrote %q to disk for a bead the ledger reports as %q", item.Status, ledgerStatus)
			}
			if item.RunID == nil || *item.RunID != bootRevertRunID {
				t.Fatalf("the pass dropped the run the live claim is recorded against: %v", item.RunID)
			}
			if len(emitter.types) != 0 {
				t.Fatalf("the pass reported a rescue it did not perform: %v", emitter.types)
			}
		})
	}
}

// TestReconcileDispatchedItems_LeavesADispatchedItemAloneWhenTheLedgerCannotBeRead
// pins the third branch. A boot where br is slow, missing or briefly broken must
// not be read as "every bead is open". That reading re-dispatches every live
// claim at once, which is the loudest way this pass can fail.
func TestReconcileDispatchedItems_LeavesADispatchedItemAloneWhenTheLedgerCannotBeRead(t *testing.T) {
	t.Parallel()

	projectDir := seedStrandedQueue(t)
	emitter := &bootRevertEmitter{projectDir: projectDir}

	q, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("load seeded queue: %v", err)
	}

	if err := reconcileDispatchedItems(
		context.Background(),
		projectDir,
		q,
		bootRevertLedger{showErr: errors.New("br is not on the path")},
		emitter,
		slog.New(slog.DiscardHandler),
	); err != nil {
		t.Fatalf("a ledger that cannot answer must not fail startup: %v", err)
	}

	if got := q.Groups[0].Items[0].Status; got != queue.ItemStatusDispatched {
		t.Fatalf("an unreadable ledger moved the item to %q; only a confirmed open bead may be reverted", got)
	}

	item := loadStrandedItem(t, projectDir)
	if item.Status != queue.ItemStatusDispatched {
		t.Fatalf("an unreadable ledger wrote %q to disk", item.Status)
	}
	if item.RunID == nil || *item.RunID != bootRevertRunID {
		t.Fatalf("an unreadable ledger dropped the run the claim is recorded against: %v", item.RunID)
	}
	if len(emitter.types) != 0 {
		t.Fatalf("the pass reported a rescue it did not perform: %v", emitter.types)
	}
}
