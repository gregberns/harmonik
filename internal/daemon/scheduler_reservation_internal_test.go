package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// The reservation is the one write that decides whether a dispatch happened.
// These tests pin the four things that write must guarantee:
//
//  1. status and RunID reach disk together — there is no window where an item
//     is dispatched with no run to find;
//  2. a failed write abandons the dispatch and leaves the item pending;
//  3. a bead already in flight from another queue is refused;
//  4. an item at the attempt bound leaves pending, so dispatch cannot live-lock.

// reservationFixture builds a project directory holding one active queue with
// one pending item, and returns the store that owns it.
func reservationFixture(t *testing.T, queueName string, beadID core.BeadID) (string, *queuewiring.QueueStore, *queue.Queue) {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("mkdir queues: %v", err)
	}
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		Name:          queueName,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusPending}},
		}},
	}
	store := queuewiring.NewQueueStore()
	store.SetQueueByName(queueName, q)
	return projectDir, store, q
}

func reservationDeps(projectDir string, store *queuewiring.QueueStore) workLoopDeps {
	return workLoopDeps{projectDir: projectDir, queueStore: store}
}

func newReservationRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

// loadPersistedItem reads the queue back off disk. Reading the file rather than
// the in-memory store is the point: the claim under test is about durability.
func loadPersistedItem(t *testing.T, projectDir, queueName string) queue.Item {
	t.Helper()
	//nolint:gosec // the path is built from t.TempDir(); the test wrote this file itself
	raw, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", queueName+".json"))
	if err != nil {
		t.Fatalf("read persisted queue: %v", err)
	}
	var persisted queue.Queue
	if unmarshalErr := json.Unmarshal(raw, &persisted); unmarshalErr != nil {
		t.Fatalf("unmarshal persisted queue: %v", unmarshalErr)
	}
	if len(persisted.Groups) == 0 || len(persisted.Groups[0].Items) == 0 {
		t.Fatalf("persisted queue has no items: %s", raw)
	}
	return persisted.Groups[0].Items[0]
}

// A committed reservation puts the dispatched status AND the RunID on disk in
// the same write. The previous two-write path could leave the first on disk
// without the second.
func TestReserveQueueItem_StampsStatusAndRunIDInOneWrite(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-reserve-1")
	projectDir, store, _ := reservationFixture(t, queueName, beadID)
	runID := newReservationRunID(t)

	got := reserveQueueItem(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: runID,
	})

	if got.Verdict != reservationReserved {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationReserved)
	}
	item := loadPersistedItem(t, projectDir, queueName)
	if item.Status != queue.ItemStatusDispatched {
		t.Errorf("persisted status = %q; want %q", item.Status, queue.ItemStatusDispatched)
	}
	if item.RunID == nil {
		t.Fatal("persisted RunID is nil; the reservation must stamp it in the same write")
	}
	if *item.RunID != runID.String() {
		t.Errorf("persisted RunID = %q; want %q", *item.RunID, runID.String())
	}
	if item.Attempts != 1 {
		t.Errorf("persisted Attempts = %d; want 1", item.Attempts)
	}
}

// A write that cannot reach disk must abandon the dispatch: the caller gets
// reservationWriteFailed and the item is still pending, so the next tick
// re-picks it. The item never started, so re-picking it is correct.
func TestReserveQueueItem_WriteFailureAbandonsDispatch(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-reserve-2")
	projectDir, store, _ := reservationFixture(t, queueName, beadID)

	// Make the queues directory unwritable so the atomic-write sequence fails.
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring the fixture directory so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

	got := reserveQueueItem(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
	})

	if got.Verdict != reservationWriteFailed {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationWriteFailed)
	}
	if got.Err == nil {
		t.Error("write failure carries no error; the operator message would say nothing useful")
	}
	// The in-memory item must not have been advanced either — a dispatch that
	// did not reach disk did not happen.
	live := store.QueueByName(queueName)
	if live == nil {
		t.Fatal("queue vanished from the store")
	}
	if live.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Errorf("item status = %q after a failed write; want %q so the next tick re-picks it",
			live.Groups[0].Items[0].Status, queue.ItemStatusPending)
	}
	if live.Groups[0].Items[0].RunID != nil {
		t.Error("item carries a RunID after a failed write; no run was started")
	}
}

// A bead already dispatched from another queue must not be reserved here.
// Two implementers on one bead is the failure this prevents.
func TestReserveQueueItem_RefusesBeadInFlightFromAnotherQueue(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-reserve-3")
	projectDir, store, _ := reservationFixture(t, queueName, beadID)

	// A second active queue already holds the same bead dispatched.
	otherRunID := newReservationRunID(t).String()
	store.SetQueueByName("beta", &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		Name:          "beta",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: beadID, Status: queue.ItemStatusDispatched, RunID: &otherRunID}},
		}},
	})

	got := reserveQueueItem(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
	})

	if got.Verdict != reservationItemFailed {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationItemFailed)
	}
	if got.ConflictingQueue != "beta" {
		t.Errorf("ConflictingQueue = %q; want %q — the operator message names it", got.ConflictingQueue, "beta")
	}
	if got.FailureReason != "cross_queue_duplicate" {
		t.Errorf("FailureReason = %q; want %q", got.FailureReason, "cross_queue_duplicate")
	}
	item := loadPersistedItem(t, projectDir, queueName)
	if item.Status != queue.ItemStatusFailed {
		t.Errorf("persisted status = %q; want %q so the group advances instead of stalling",
			item.Status, queue.ItemStatusFailed)
	}
	if item.RunID != nil {
		t.Error("a refused duplicate carries a RunID; nothing was launched for it")
	}
}

// An item that reaches the attempt bound is failed in the reservation write
// itself. Leaving it pending would make the next select re-pick it, re-count,
// and live-lock dispatch.
func TestReserveQueueItem_AttemptBoundFailsItemDurably(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-reserve-4")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	q.Groups[0].Items[0].Attempts = maxItemAttempts - 1
	store.SetQueueByName(queueName, q)

	got := reserveQueueItem(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
	})

	if got.Verdict != reservationItemFailed {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationItemFailed)
	}
	if got.FailureReason != "max_attempts_exceeded" {
		t.Errorf("FailureReason = %q; want %q", got.FailureReason, "max_attempts_exceeded")
	}
	item := loadPersistedItem(t, projectDir, queueName)
	if item.Status != queue.ItemStatusFailed {
		t.Errorf("persisted status = %q; want %q", item.Status, queue.ItemStatusFailed)
	}
	if item.Attempts != maxItemAttempts {
		t.Errorf("persisted Attempts = %d; want %d — the last attempt is still counted", item.Attempts, maxItemAttempts)
	}
	if item.RunID != nil {
		t.Error("an item failed at the attempt bound carries a RunID; nothing was launched for it")
	}
}

// An item that moved since the snapshot writes nothing and asks the caller to
// look again, rather than reporting a failure that never happened.
func TestReserveQueueItem_ItemNoLongerPendingRetriesLater(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-reserve-5")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	q.Groups[0].Items[0].Status = queue.ItemStatusCompleted
	store.SetQueueByName(queueName, q)

	got := reserveQueueItem(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
	})

	if got.Verdict != reservationRetryLater {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationRetryLater)
	}
}

// activeQueueItem must refuse an index whose bead does not match, because the
// dispatch snapshot and the live queue can disagree about what sits there.
func TestActiveQueueItem_RefusesBeadMismatch(t *testing.T) {
	q := &queue.Queue{Groups: []queue.Group{{
		GroupIndex: 0,
		Status:     queue.GroupStatusActive,
		Items:      []queue.Item{{BeadID: "hk-actual", Status: queue.ItemStatusPending}},
	}}}

	if got := activeQueueItem(q, 0, 0, "hk-actual"); got == nil {
		t.Error("activeQueueItem returned nil for a matching bead")
	}
	if got := activeQueueItem(q, 0, 0, "hk-different"); got != nil {
		t.Error("activeQueueItem returned an item whose bead does not match the reservation")
	}
	if got := activeQueueItem(q, 0, 7, "hk-actual"); got != nil {
		t.Error("activeQueueItem returned an item for an out-of-range index")
	}
	if got := activeQueueItem(nil, 0, 0, "hk-actual"); got != nil {
		t.Error("activeQueueItem returned an item for a nil queue")
	}
}

// recordingEmitter captures the payload of every emitted event, which
// CollectingEmitter does not — and the payload is the thing under test here.
type recordingEmitter struct {
	types    []core.EventType
	payloads [][]byte
}

func (e *recordingEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	e.types = append(e.types, eventType)
	e.payloads = append(e.payloads, payload)
	return nil
}

func (e *recordingEmitter) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return e.Emit(ctx, eventType, payload)
}

// QM-001 requires three things of a failed queue write: refuse further
// mutations, emit infrastructure_unavailable{failed_prerequisite:
// queue_write_error}, and go degraded. Nothing did any of them before this
// change, and the enum value the event needs did not exist, so the payload the
// spec asks for could not even be built.
func TestReportQueueWriteError_EmitsBothEventsQM001Requires(t *testing.T) {
	emitter := &recordingEmitter{}
	deps := workLoopDeps{bus: emitter, queueWriteErrorReported: map[string]struct{}{}}

	reportQueueWriteError(context.Background(), deps, "main", reservationResult{
		Verdict: reservationWriteFailed,
		Outcome: queue.OutcomeNotCommitted,
		Err:     errReserveItemNotPending,
	})

	if len(emitter.types) != 2 {
		t.Fatalf("emitted %d events; want infrastructure_unavailable and daemon_degraded", len(emitter.types))
	}
	if emitter.types[0] != core.EventTypeInfrastructureUnavailable {
		t.Errorf("first event = %q; want %q", emitter.types[0], core.EventTypeInfrastructureUnavailable)
	}
	if emitter.types[1] != core.EventTypeDaemonDegraded {
		t.Errorf("second event = %q; want %q — QM-001 also requires the degraded transition",
			emitter.types[1], core.EventTypeDaemonDegraded)
	}

	var infra core.InfrastructureUnavailablePayload
	if err := json.Unmarshal(emitter.payloads[0], &infra); err != nil {
		t.Fatalf("unmarshal infrastructure_unavailable: %v", err)
	}
	if infra.FailedPrerequisite != core.InfrastructurePrerequisiteQueueWriteError {
		t.Errorf("failed_prerequisite = %q; want %q",
			infra.FailedPrerequisite, core.InfrastructurePrerequisiteQueueWriteError)
	}
	if !infra.Valid() {
		t.Error("infrastructure_unavailable payload is not valid per event-model.md §8.7.15")
	}
	// The detail must name the queue and the underlying cause, or an operator
	// reading the event learns only that something failed somewhere.
	if !strings.Contains(infra.DetailString, "main") {
		t.Errorf("detail_string %q does not name the queue", infra.DetailString)
	}
	if !strings.Contains(infra.DetailString, errReserveItemNotPending.Error()) {
		t.Errorf("detail_string %q does not carry the underlying error", infra.DetailString)
	}

	var degraded core.DaemonDegradedPayload
	if err := json.Unmarshal(emitter.payloads[1], &degraded); err != nil {
		t.Fatalf("unmarshal daemon_degraded: %v", err)
	}
	if degraded.Reason != core.DaemonDegradedReasonInfrastructureUnavailable {
		t.Errorf("degraded reason = %q; want %q", degraded.Reason, core.DaemonDegradedReasonInfrastructureUnavailable)
	}
	if !degraded.Valid() {
		t.Error("daemon_degraded payload is not valid per event-model.md §8.7.5")
	}
}

// The quarantine is sticky, so every later tick re-derives the same failure.
// Reporting it each time would bury the one event an operator needs under tens
// of thousands of copies of itself.
func TestReportQueueWriteError_ReportsOncePerQueue(t *testing.T) {
	emitter := &recordingEmitter{}
	deps := workLoopDeps{bus: emitter, queueWriteErrorReported: map[string]struct{}{}}
	failure := reservationResult{Verdict: reservationWriteFailed, Outcome: queue.OutcomeNotCommitted}

	for range 5 {
		reportQueueWriteError(context.Background(), deps, "main", failure)
	}
	if len(emitter.types) != 2 {
		t.Errorf("emitted %d events for five failures on one queue; want 2 (reported once)", len(emitter.types))
	}

	// A different queue is a different failure and reports on its own.
	reportQueueWriteError(context.Background(), deps, "other", failure)
	if len(emitter.types) != 4 {
		t.Errorf("emitted %d events after a second queue failed; want 4", len(emitter.types))
	}
}

// A nil bus must not panic the dispatch loop; the stderr message still goes out.
func TestReportQueueWriteError_NilBusIsSafe(t *testing.T) {
	reportQueueWriteError(context.Background(), workLoopDeps{}, "main", reservationResult{
		Outcome: queue.OutcomeCommitIndeterminate,
	})
}

// After a failed write the store refuses the queue, and that refusal must keep
// reporting as a write failure. Calling it retry_later would turn a queue that
// will never accept another write into a silent two-second spin.
func TestReserveQueueItem_QuarantinedQueueStaysLoud(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-reserve-6")
	projectDir, store, _ := reservationFixture(t, queueName, beadID)
	deps := reservationDeps(projectDir, store)

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring the fixture directory so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

	first := reserveQueueItem(context.Background(), deps, queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
	})
	if first.Verdict != reservationWriteFailed {
		t.Fatalf("first verdict = %q; want %q", first.Verdict, reservationWriteFailed)
	}

	// Repairing the directory must not un-quarantine the queue: QM-001 says the
	// daemon refuses further mutations, and recovery is an operator restart.
	if err := os.Chmod(queuesDir, 0o700); err != nil { //nolint:gosec // restoring write access to prove the quarantine is sticky
		t.Fatalf("chmod queues dir: %v", err)
	}
	second := reserveQueueItem(context.Background(), deps, queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
	})
	if second.Verdict != reservationWriteFailed {
		t.Errorf("verdict after quarantine = %q; want %q — a refused queue must not read as retry_later",
			second.Verdict, reservationWriteFailed)
	}
	if !errors.Is(second.Err, queuewiring.ErrQueueQuarantined) {
		t.Errorf("err = %v; want it to wrap ErrQueueQuarantined so the caller can tell it apart", second.Err)
	}
}
