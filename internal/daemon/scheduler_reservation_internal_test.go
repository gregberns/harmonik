package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runloop"
)

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

func reservationDeps(projectDir string, store *queuewiring.QueueStore) testRuntime {
	return testRuntime{env: runloop.RunEnv{ProjectDir: projectDir}, queueStore: store}
}

func reserveQueueItemForTest(ctx context.Context, runtime testRuntime, reservation queueReservation) reservationResult {
	if reservation.QueueID == "" {
		if snapshot := runtime.queueStore.Snapshot(reservation.QueueName); snapshot.Queue != nil {
			reservation.QueueID = snapshot.Queue.QueueID
		}
	}
	return reserveQueueItem(ctx, runtime.queueStore, runtime.env.ProjectDir, reservation)
}

func newReservationRunID(t *testing.T) core.RunID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.RunID(id)
}

func newReservationTransitionID(t *testing.T) core.TransitionID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return core.TransitionID(id)
}

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

	got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: runID, ClaimTransitionID: newReservationTransitionID(t),
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

	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	if err := os.Chmod(queuesDir, 0o500); err != nil { //nolint:gosec // the test needs a readable but non-writable directory
		t.Fatalf("chmod queues dir: %v", err)
	}
	t.Cleanup(func() {
		if chmodErr := os.Chmod(queuesDir, 0o700); chmodErr != nil { //nolint:gosec // restoring the fixture directory so t.TempDir cleanup can remove it
			t.Logf("restore queues dir permissions: %v", chmodErr)
		}
	})

	reservation := queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t), ClaimTransitionID: newReservationTransitionID(t),
	}
	got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), reservation)

	if got.Verdict != reservationWriteFailed {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationWriteFailed)
	}
	if got.Err == nil {
		t.Error("write failure carries no error; the operator message would say nothing useful")
	}
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

func siblingQueue(name string, beadID core.BeadID, status queue.ItemStatus, runID *string) *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       newTestQueueID(),
		Name:          name,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindWave,
			Status:     queue.GroupStatusActive,
			Items:      []queue.Item{{BeadID: beadID, Status: status, RunID: runID}},
		}},
	}
}

// A bead already dispatched from another queue must not be reserved here.
// Two implementers on one bead is the failure this prevents.
//
// It must ALSO not be failed. The reservation reports the collision and writes
// nothing: the sibling's hold is a property of this tick, and a durable failure
// here parks the loser's whole queue behind an item that did nothing wrong
// (specs/queue-model.md §9.8 QM-067, hk-nsion).
func TestReserveQueueItem_RefusesBeadInFlightFromAnotherQueue(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-reserve-3")
	projectDir, store, _ := reservationFixture(t, queueName, beadID)

	otherRunID := newReservationRunID(t).String()
	store.SetQueueByName("beta", siblingQueue("beta", beadID, queue.ItemStatusDispatched, &otherRunID))

	reservation := queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t), ClaimTransitionID: newReservationTransitionID(t),
	}
	got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), reservation)

	if got.Verdict != reservationCrossQueueCollision {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationCrossQueueCollision)
	}
	if got.Collision.ConflictingQueue != "beta" {
		t.Errorf("ConflictingQueue = %q; want %q — the collision report names both queues",
			got.Collision.ConflictingQueue, "beta")
	}
	if got.Collision.Disposition != crossQueueSiblingRunning {
		t.Errorf("Disposition = %q; want %q — a running sibling can lapse, so this refusal must not be made durable",
			got.Collision.Disposition, crossQueueSiblingRunning)
	}

	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik", "queues", queueName+".json")); !os.IsNotExist(err) {
		item := loadPersistedItem(t, projectDir, queueName)
		t.Errorf("a queue file was written with item status %q / reason %q; a per-tick refusal must write nothing",
			item.Status, item.LastFailureReason)
	}
	live := store.QueueByName(queueName)
	if live == nil {
		t.Fatal("queue vanished from the store")
	}
	if live.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Errorf("item status = %q; want %q — the loser stays selectable, so the work is still reachable when "+
			"the sibling's run ends", live.Groups[0].Items[0].Status, queue.ItemStatusPending)
	}
	if live.Groups[0].Items[0].Attempts != 0 {
		t.Errorf("item Attempts = %d; want 0 — losing somebody else's race must not spend this item's dispatch budget",
			live.Groups[0].Items[0].Attempts)
	}
}

// A bead another queue has already FINISHED is a different case from one it is
// still running, and the guard must say which. The work is done and the bead is
// closed, so the loser's item is advanced to completed per §3.2b QM-002b Class A
// rather than refused forever or failed.
func TestReserveQueueItem_ReportsAFinishedSiblingSeparately(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-reserve-3b")
	projectDir, store, _ := reservationFixture(t, queueName, beadID)

	store.SetQueueByName("beta", siblingQueue("beta", beadID, queue.ItemStatusCompleted, nil))

	got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
	})

	if got.Verdict != reservationCrossQueueCollision {
		t.Fatalf("verdict = %q (outcome=%s err=%v); want %q", got.Verdict, got.Outcome, got.Err, reservationCrossQueueCollision)
	}
	if got.Collision.Disposition != crossQueueSiblingFinished {
		t.Errorf("Disposition = %q; want %q — welding the two dispositions into one boolean is what made every "+
			"collision terminal", got.Collision.Disposition, crossQueueSiblingFinished)
	}
	if got.Collision.ConflictingQueue != "beta" {
		t.Errorf("ConflictingQueue = %q; want %q", got.Collision.ConflictingQueue, "beta")
	}
}

// A bead that is completed in one sibling and dispatched in another is running
// NOW. The running answer must win, because advancing this item to completed on
// the strength of the older record would call work that is in flight done.
func TestReserveQueueItem_RunningSiblingOutranksAFinishedOne(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-reserve-3c")
	projectDir, store, _ := reservationFixture(t, queueName, beadID)

	runID := newReservationRunID(t).String()
	store.SetQueueByName("done", siblingQueue("done", beadID, queue.ItemStatusCompleted, nil))
	store.SetQueueByName("busy", siblingQueue("busy", beadID, queue.ItemStatusDispatched, &runID))

	for attempt := range 20 {
		got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), queueReservation{
			QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t),
		})
		if got.Collision.Disposition != crossQueueSiblingRunning {
			t.Fatalf("attempt %d: Disposition = %q (queue %q); want %q — the in-flight sibling must outrank the "+
				"finished one", attempt, got.Collision.Disposition, got.Collision.ConflictingQueue, crossQueueSiblingRunning)
		}
		if got.Collision.ConflictingQueue != "busy" {
			t.Fatalf("attempt %d: ConflictingQueue = %q; want %q", attempt, got.Collision.ConflictingQueue, "busy")
		}
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

	reservation := queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t), ClaimTransitionID: newReservationTransitionID(t),
	}
	got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), reservation)

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
	assertPreclaimTerminalBinding(t, item, reservation, queue.PreclaimTerminalMaxAttempts)
}

func assertPreclaimTerminalBinding(t *testing.T, item queue.Item, reservation queueReservation, cause queue.PreclaimTerminalCause) {
	t.Helper()
	if item.PreclaimTerminal == nil {
		t.Fatal("persisted preclaim terminal binding is nil")
	}
	if item.PreclaimTerminal.RunID != reservation.RunID.String() ||
		item.PreclaimTerminal.ClaimTransitionID != reservation.ClaimTransitionID.String() ||
		item.PreclaimTerminal.Cause != cause {
		t.Fatalf("persisted preclaim terminal binding = %+v", item.PreclaimTerminal)
	}
}

func TestFailQueueItemDependencyRefusalRequiresExactOwner(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-refusal-owner")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	ownerRunID := newReservationRunID(t)
	ownerRunText := ownerRunID.String()
	q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	q.Groups[0].Items[0].RunID = &ownerRunText
	store.SetQueueByName(queueName, q)

	base := queueReservation{
		QueueName: queueName, QueueID: q.QueueID, GroupIndex: 0, ItemIndex: 0,
		BeadID: beadID, RunID: ownerRunID, ClaimTransitionID: newReservationTransitionID(t),
	}
	for _, tc := range []struct {
		name   string
		mutate func(*queueReservation)
	}{
		{name: "different run", mutate: func(res *queueReservation) { res.RunID = newReservationRunID(t) }},
		{name: "replacement queue", mutate: func(res *queueReservation) { res.QueueID = newTestQueueID() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := base
			tc.mutate(&res)
			got := failQueueItem(context.Background(), store, projectDir, res, "claim_dependency_refusal", queue.PreclaimTerminalDependencyRefusal)
			if got.Verdict != reservationRetryLater {
				t.Fatalf("verdict = %q outcome=%s err=%v", got.Verdict, got.Outcome, got.Err)
			}
			item := store.QueueByName(queueName).Groups[0].Items[0]
			if item.Status != queue.ItemStatusDispatched || item.RunID == nil || *item.RunID != ownerRunText || item.PreclaimTerminal != nil {
				t.Fatalf("owner changed: %+v", item)
			}
		})
	}
}

func TestFailQueueItemDependencyRefusalRefusesSameNameReplacement(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-refusal-replacement")
	projectDir, store, original := reservationFixture(t, queueName, beadID)
	runID := newReservationRunID(t)
	runText := runID.String()
	res := queueReservation{
		QueueName: queueName, QueueID: original.QueueID, GroupIndex: 0, ItemIndex: 0,
		BeadID: beadID, RunID: runID, ClaimTransitionID: newReservationTransitionID(t),
	}
	replacement := queue.CloneQueue(original)
	replacement.QueueID = newTestQueueID()
	replacement.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	replacement.Groups[0].Items[0].RunID = &runText
	store.SetQueueByName(queueName, replacement)

	got := failQueueItem(context.Background(), store, projectDir, res, "claim_dependency_refusal", queue.PreclaimTerminalDependencyRefusal)
	if got.Verdict != reservationRetryLater {
		t.Fatalf("verdict = %q outcome=%s err=%v", got.Verdict, got.Outcome, got.Err)
	}
	item := store.QueueByName(queueName).Groups[0].Items[0]
	if item.Status != queue.ItemStatusDispatched || item.PreclaimTerminal != nil {
		t.Fatalf("replacement changed: %+v", item)
	}
}

func TestReserveQueueItemRefusesSameNameReplacement(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-reserve-replacement")
	projectDir, store, original := reservationFixture(t, queueName, beadID)
	res := queueReservation{
		QueueName: queueName, QueueID: original.QueueID, GroupIndex: 0, ItemIndex: 0,
		BeadID: beadID, RunID: newReservationRunID(t), ClaimTransitionID: newReservationTransitionID(t),
	}
	replacement := queue.CloneQueue(original)
	replacement.QueueID = newTestQueueID()
	store.SetQueueByName(queueName, replacement)

	got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), res)
	if got.Verdict != reservationRetryLater {
		t.Fatalf("verdict = %q outcome=%s err=%v", got.Verdict, got.Outcome, got.Err)
	}
	item := store.QueueByName(queueName).Groups[0].Items[0]
	if item.Status != queue.ItemStatusPending || item.Attempts != 0 || item.RunID != nil || item.PreclaimTerminal != nil {
		t.Fatalf("replacement changed: %+v", item)
	}
}

func TestFailQueueItemDependencyRefusalPersistsExactClaimIdentity(t *testing.T) {
	const queueName = "main"
	const beadID = core.BeadID("hk-refusal-binding")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	runID := newReservationRunID(t)
	runText := runID.String()
	q.Groups[0].Items[0].Status = queue.ItemStatusDispatched
	q.Groups[0].Items[0].RunID = &runText
	store.SetQueueByName(queueName, q)
	res := queueReservation{
		QueueName: queueName, QueueID: q.QueueID, GroupIndex: 0, ItemIndex: 0,
		BeadID: beadID, RunID: runID, ClaimTransitionID: newReservationTransitionID(t),
	}
	got := failQueueItem(context.Background(), store, projectDir, res, "claim_dependency_refusal", queue.PreclaimTerminalDependencyRefusal)
	if got.Verdict != reservationItemFailed {
		t.Fatalf("verdict = %q outcome=%s err=%v", got.Verdict, got.Outcome, got.Err)
	}
	assertPreclaimTerminalBinding(t, loadPersistedItem(t, projectDir, queueName), res, queue.PreclaimTerminalDependencyRefusal)
}

func TestFinishDependencyRefusalDoesNotCompleteGroupAfterBindingFault(t *testing.T) {
	completed := 0
	err := finishDependencyRefusal(reservationResult{
		Verdict: reservationWriteFailed,
		Outcome: queue.OutcomeCommitIndeterminate,
		Err:     errors.New("binding write fault"),
	}, func() { completed++ })
	if err == nil {
		t.Fatal("finishDependencyRefusal returned nil")
	}
	if completed != 0 {
		t.Fatalf("group completion calls = %d", completed)
	}

	if err := finishDependencyRefusal(reservationResult{Verdict: reservationItemFailed, Outcome: queue.OutcomeCommittedDurable}, func() { completed++ }); err != nil {
		t.Fatal(err)
	}
	if completed != 1 {
		t.Fatalf("positive group completion calls = %d", completed)
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

	got := reserveQueueItemForTest(context.Background(), reservationDeps(projectDir, store), queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t), ClaimTransitionID: newReservationTransitionID(t),
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
	deps := testRuntime{ports: runloop.RunPorts{Emitter: emitter}, dispatchGates: newDispatchGatesPort(emitter, nil, nil, nil)}

	reportQueueWriteError(context.Background(), newDispatchGatesPortFromDeps(deps), "main", reservationResult{
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
	deps := testRuntime{ports: runloop.RunPorts{Emitter: emitter}, dispatchGates: newDispatchGatesPort(emitter, nil, nil, nil)}
	failure := reservationResult{Verdict: reservationWriteFailed, Outcome: queue.OutcomeNotCommitted}

	for range 5 {
		reportQueueWriteError(context.Background(), newDispatchGatesPortFromDeps(deps), "main", failure)
	}
	if len(emitter.types) != 2 {
		t.Errorf("emitted %d events for five failures on one queue; want 2 (reported once)", len(emitter.types))
	}

	reportQueueWriteError(context.Background(), newDispatchGatesPortFromDeps(deps), "other", failure)
	if len(emitter.types) != 4 {
		t.Errorf("emitted %d events after a second queue failed; want 4", len(emitter.types))
	}
}

// A nil bus must not panic the dispatch loop; the stderr message still goes out.
func TestReportQueueWriteError_NilBusIsSafe(t *testing.T) {
	reportQueueWriteError(context.Background(), newDispatchGatesPortFromDeps(testRuntime{}), "main", reservationResult{
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

	first := reserveQueueItemForTest(context.Background(), deps, queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t), ClaimTransitionID: newReservationTransitionID(t),
	})
	if first.Verdict != reservationWriteFailed {
		t.Fatalf("first verdict = %q; want %q", first.Verdict, reservationWriteFailed)
	}

	if err := os.Chmod(queuesDir, 0o700); err != nil { //nolint:gosec // restoring write access to prove the quarantine is sticky
		t.Fatalf("chmod queues dir: %v", err)
	}
	second := reserveQueueItemForTest(context.Background(), deps, queueReservation{
		QueueName: queueName, GroupIndex: 0, ItemIndex: 0, BeadID: beadID, RunID: newReservationRunID(t), ClaimTransitionID: newReservationTransitionID(t),
	})
	if second.Verdict != reservationWriteFailed {
		t.Errorf("verdict after quarantine = %q; want %q — a refused queue must not read as retry_later",
			second.Verdict, reservationWriteFailed)
	}
	if !errors.Is(second.Err, queuewiring.ErrQueueQuarantined) {
		t.Errorf("err = %v; want it to wrap ErrQueueQuarantined so the caller can tell it apart", second.Err)
	}
}

func collisionKey(beadID core.BeadID) queuePreClaimAttemptKey {
	return queuePreClaimAttemptKey{queueID: "queue-id", groupIndex: 0, itemIdx: 0, beadID: beadID}
}

// The counter refuses up to the bound and then falls through to the terminal
// failure, so today's loud wrong answer is the tail case rather than the first.
func TestRecordCrossQueueCollision_RefusesUpToTheBoundThenFails(t *testing.T) {
	states := map[queuePreClaimAttemptKey]crossQueueCollisionState{}
	key := collisionKey("hk-nsion-bound")

	for i := 1; i < maxCrossQueueCollisions; i++ {
		outcome, _ := recordCrossQueueCollision(states, key, crossQueueSiblingRunning)
		if outcome != crossQueueRefuseTick {
			t.Fatalf("collision %d of %d: outcome = %q; want %q — the item must stay pending while the sibling runs",
				i, maxCrossQueueCollisions, outcome, crossQueueRefuseTick)
		}
	}

	outcome, report := recordCrossQueueCollision(states, key, crossQueueSiblingRunning)
	if outcome != crossQueueFailTerminal {
		t.Errorf("collision %d: outcome = %q; want %q — a refusal that never lapses is a silent stall",
			maxCrossQueueCollisions, outcome, crossQueueFailTerminal)
	}
	if !report {
		t.Error("the terminal collision was not reported; it is the one outcome an operator has to act on")
	}
}

// The report fires once per collision, not once per tick. Without this the
// refusal would emit an event every poll interval for as long as the sibling
// runs, and the one event an operator needs would be buried in copies of itself.
func TestRecordCrossQueueCollision_ReportsOncePerCollision(t *testing.T) {
	states := map[queuePreClaimAttemptKey]crossQueueCollisionState{}
	key := collisionKey("hk-nsion-report-once")

	if _, report := recordCrossQueueCollision(states, key, crossQueueSiblingRunning); !report {
		t.Fatal("the first collision was not reported, so nothing says two queues hold one bead")
	}
	for i := 2; i < maxCrossQueueCollisions; i++ {
		if _, report := recordCrossQueueCollision(states, key, crossQueueSiblingRunning); report {
			t.Fatalf("collision %d was reported again; the same collision must be announced once", i)
		}
	}

	states[key] = crossQueueCollisionState{consecutive: 1, reported: crossQueueSiblingRunning}
	outcome, report := recordCrossQueueCollision(states, key, crossQueueSiblingFinished)
	if outcome != crossQueueAdvanceCompleted {
		t.Errorf("outcome = %q; want %q — a finished sibling settles the item", outcome, crossQueueAdvanceCompleted)
	}
	if !report {
		t.Error("the disposition changed from running to finished and nothing was reported")
	}
}

// Progress clears the counter, so only a CONSECUTIVE run of collisions reaches
// the bound. An item that dispatched once and collides again months later must
// start its budget over.
func TestRecordCrossQueueCollision_IsPerItemAndConsecutive(t *testing.T) {
	states := map[queuePreClaimAttemptKey]crossQueueCollisionState{}
	key := collisionKey("hk-nsion-consecutive")
	other := collisionKey("hk-nsion-other-item")

	for range maxCrossQueueCollisions - 1 {
		recordCrossQueueCollision(states, key, crossQueueSiblingRunning)
	}
	if outcome, _ := recordCrossQueueCollision(states, other, crossQueueSiblingRunning); outcome != crossQueueRefuseTick {
		t.Errorf("a second item's first collision = %q; want %q — the budget is per item", outcome, crossQueueRefuseTick)
	}
	delete(states, key)
	if outcome, _ := recordCrossQueueCollision(states, key, crossQueueSiblingRunning); outcome != crossQueueRefuseTick {
		t.Errorf("after progress the item's next collision = %q; want %q — the budget must not carry over",
			outcome, crossQueueRefuseTick)
	}
}

func collisionPorts(projectDir string, store *queuewiring.QueueStore, emitter *intentDurabilityEmitter) (ports crossQueueCollisionPorts, tickRefusals map[core.BeadID]bool, refusedUntil map[core.BeadID]time.Time) {
	tickRefusals = map[core.BeadID]bool{}
	refusedUntil = map[core.BeadID]time.Time{}
	return crossQueueCollisionPorts{
		emitter:      emitter,
		queueStore:   store,
		projectDir:   projectDir,
		reap:         reapSeamPort{bus: emitter, projectDir: projectDir, queueStore: store, runRegistry: newLocalRunRegistry(), maxConcurrent: 2},
		collisions:   map[queuePreClaimAttemptKey]crossQueueCollisionState{},
		tickRefusals: tickRefusals,
		refusedUntil: refusedUntil,
	}, tickRefusals, refusedUntil
}

const collisionQueueName = "alpha"

func collisionSite(beadID core.BeadID, queueID string) crossQueueCollisionSite {
	return crossQueueCollisionSite{
		QueueName: collisionQueueName, QueueID: queueID, GroupIndex: 0, ItemIndex: 0,
		BeadID: beadID, RunID: core.RunID(uuid.MustParse("0197d001-0000-7000-8000-000000000901")),
		ClaimTransitionID: core.TransitionID(uuid.MustParse("0197d001-0000-7000-8000-000000000902")),
		Now:               time.Now(),
	}
}

// A running sibling refuses the item for this tick and writes nothing. Both
// refusal sets are armed: the clockless one bounds this tick's walk, the timed
// one stops the collision being re-tested every poll interval.
func TestResolveCrossQueueCollision_RunningSiblingRefusesWithoutWriting(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-nsion-refuse")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	emitter := &intentDurabilityEmitter{}
	ports, tickRefusals, refusedUntil := collisionPorts(projectDir, store, emitter)

	refused := resolveCrossQueueCollision(context.Background(), ports,
		collisionSite(beadID, q.QueueID),
		crossQueueCollision{ConflictingQueue: "beta", Disposition: crossQueueSiblingRunning})

	if !refused {
		t.Fatal("the resolution did not report a refusal, so the loop starts a new tick instead of offering the " +
			"next eligible item behind this one (§9.8 QM-067)")
	}
	if !tickRefusals[beadID] {
		t.Error("the clockless tick-refusal set was not armed; this tick's walk can re-offer the same item forever")
	}
	if expiry, ok := refusedUntil[beadID]; !ok || !expiry.After(time.Now()) {
		t.Errorf("the timed refusal set holds %v (present=%v); without it the collision is re-tested every poll "+
			"interval for as long as the sibling runs", expiry, ok)
	}
	if _, err := os.Stat(filepath.Join(projectDir, ".harmonik", "queues", queueName+".json")); !os.IsNotExist(err) {
		item := loadPersistedItem(t, projectDir, queueName)
		t.Errorf("the refusal wrote status %q reason %q to disk; a refusal is a property of the tick and must "+
			"not be made durable", item.Status, item.LastFailureReason)
	}
	live := store.QueueByName(queueName)
	if live.Groups[0].Items[0].Status != queue.ItemStatusPending {
		t.Errorf("item status = %q; want %q", live.Groups[0].Items[0].Status, queue.ItemStatusPending)
	}
	if live.Status != queue.QueueStatusActive {
		t.Errorf("queue status = %q; want %q — a refusal must not park the queue behind an item that did nothing wrong",
			live.Status, queue.QueueStatusActive)
	}
}

// A finished sibling advances the item to COMPLETED. The work is done and the
// bead is closed; failing the item instead parks the queue over work that
// succeeded (§3.2b QM-002b Class A).
func TestResolveCrossQueueCollision_FinishedSiblingCompletesTheItem(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-nsion-finished")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	q.Groups[0].Items = append(q.Groups[0].Items, queue.Item{BeadID: "hk-nsion-filler", Status: queue.ItemStatusPending})
	store.SetQueueByName(queueName, q)

	emitter := &intentDurabilityEmitter{}
	ports, tickRefusals, _ := collisionPorts(projectDir, store, emitter)

	refused := resolveCrossQueueCollision(context.Background(), ports,
		collisionSite(beadID, q.QueueID),
		crossQueueCollision{ConflictingQueue: "beta", Disposition: crossQueueSiblingFinished})

	if refused {
		t.Error("a finished sibling was reported as a refusal; the collision cannot lapse and must settle now")
	}
	if tickRefusals[beadID] {
		t.Error("a finished sibling armed a refusal; the item is terminal and will never be offered again")
	}
	item := loadPersistedItem(t, projectDir, queueName)
	if item.Status != queue.ItemStatusCompleted {
		t.Errorf("persisted status = %q; want %q — the bead is closed, so running it again duplicates finished work",
			item.Status, queue.ItemStatusCompleted)
	}
	if item.LastFailureReason != "" {
		t.Errorf("persisted LastFailureReason = %q; want empty — nothing failed", item.LastFailureReason)
	}
	live := store.QueueByName(queueName)
	if live.Status != queue.QueueStatusActive {
		t.Errorf("queue status = %q; want %q", live.Status, queue.QueueStatusActive)
	}
}

// Past the bound the item is failed exactly as it always was, and its queue
// parks. This is the backstop, and it must still work: a refusal that never
// lapses is worse than a loud wrong answer.
func TestResolveCrossQueueCollision_PastTheBoundFailsTheItem(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-nsion-backstop")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	emitter := &intentDurabilityEmitter{}
	ports, _, _ := collisionPorts(projectDir, store, emitter)
	site := collisionSite(beadID, q.QueueID)
	collision := crossQueueCollision{ConflictingQueue: "beta", Disposition: crossQueueSiblingRunning}

	for i := 1; i < maxCrossQueueCollisions; i++ {
		if !resolveCrossQueueCollision(context.Background(), ports, site, collision) {
			t.Fatalf("collision %d settled the item early; the bound is what makes the failure the tail case", i)
		}
	}
	if resolveCrossQueueCollision(context.Background(), ports, site, collision) {
		t.Fatal("the collision at the bound was still a refusal; the backstop never fires and a stuck sibling " +
			"strands this item forever")
	}

	item := loadPersistedItem(t, projectDir, queueName)
	if item.Status != queue.ItemStatusFailed {
		t.Errorf("persisted status = %q; want %q", item.Status, queue.ItemStatusFailed)
	}
	if item.LastFailureReason != "cross_queue_duplicate" {
		t.Errorf("persisted LastFailureReason = %q; want %q — an operator reading queue status needs the why",
			item.LastFailureReason, "cross_queue_duplicate")
	}
	assertPreclaimTerminalBinding(t, item, queueReservation{
		RunID: site.RunID, ClaimTransitionID: site.ClaimTransitionID,
	}, queue.PreclaimTerminalCrossQueue)
	live := store.QueueByName(queueName)
	if live.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("queue status = %q; want %q — the backstop is today's behaviour, moved to the tail",
			live.Status, queue.QueueStatusPausedByFailure)
	}
}

func TestFinishPreclaimTerminal_WriteFailureDoesNotFinalizeGroup(t *testing.T) {
	completionCalls := 0
	err := finishPreclaimTerminal(reservationResult{
		Verdict: reservationWriteFailed,
		Outcome: queue.OutcomeCommitIndeterminate,
		Err:     errors.New("injected binding write failure"),
	}, func() {
		completionCalls++
	})
	if err == nil {
		t.Fatal("write failure returned nil; group finalization could proceed without a durable preclaim binding")
	}
	if completionCalls != 0 {
		t.Fatalf("completion calls = %d; want 0 when the preclaim binding write failed", completionCalls)
	}
}

// The collision report names BOTH queues and fires once per collision.
//
// Removing the durable failure would otherwise make a real misconfiguration
// silent: two queues holding one bead is a planning mistake somebody has to fix,
// and neither queue name on its own says where to look.
func TestResolveCrossQueueCollision_ReportsBothQueuesOnce(t *testing.T) {
	const queueName = "alpha"
	const beadID = core.BeadID("hk-nsion-report")
	projectDir, store, q := reservationFixture(t, queueName, beadID)
	emitter := &intentDurabilityEmitter{}
	ports, _, _ := collisionPorts(projectDir, store, emitter)
	site := collisionSite(beadID, q.QueueID)
	collision := crossQueueCollision{ConflictingQueue: "beta", Disposition: crossQueueSiblingRunning}

	const repeats = 5
	for range repeats {
		resolveCrossQueueCollision(context.Background(), ports, site, collision)
	}

	payloads := make([]core.CrossQueueCollisionPayload, 0, emitter.count())
	for i := range emitter.count() {
		eventType, raw := emitter.event(i)
		if eventType != core.EventTypeCrossQueueCollision {
			continue
		}
		var payload core.CrossQueueCollisionPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("unmarshal collision payload: %v", err)
		}
		payloads = append(payloads, payload)
	}

	if len(payloads) != 1 {
		t.Fatalf("%d cross_queue_collision events for %d observations of the SAME collision; want 1. "+
			"A refusal repeats for as long as the sibling runs, so an event per observation buries the one an "+
			"operator needs.", len(payloads), repeats)
	}
	got := payloads[0]
	if got.LosingQueue != queueName || got.WinningQueue != "beta" {
		t.Errorf("payload names losing=%q winning=%q; want losing=%q winning=%q — one name alone does not say "+
			"where the duplicate is", got.LosingQueue, got.WinningQueue, queueName, "beta")
	}
	if got.BeadID != string(beadID) {
		t.Errorf("payload bead_id = %q; want %q", got.BeadID, beadID)
	}
	if got.Disposition != core.CrossQueueCollisionRefused {
		t.Errorf("payload disposition = %q; want %q — the disposition is what tells an operator whether to act",
			got.Disposition, core.CrossQueueCollisionRefused)
	}
	if !got.Valid() {
		t.Errorf("payload is not valid and would be dropped before emission: %+v", got)
	}
}
