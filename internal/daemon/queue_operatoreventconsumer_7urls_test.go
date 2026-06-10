package daemon_test

// queue_operatoreventconsumer_7urls_test.go — unit + integration tests for
// QueueOperatorEventConsumer (hk-7urls).
//
// Helper prefix: queueOpDrainFixture
//
// Coverage:
//   - TestQueueOpDrain_ActiveToPausedByDrain_OnPausing     — pause_status=pausing → paused-by-drain
//   - TestQueueOpDrain_ActiveToPausedByDrain_OnPaused      — pause_status=paused  → paused-by-drain
//   - TestQueueOpDrain_NoOpWhenAlreadyPausedByDrain        — idempotent on duplicate pause event
//   - TestQueueOpDrain_NoOpWhenNilQueue                    — no queue loaded → no-op
//   - TestQueueOpDrain_PausedByDrainToActive_OnResuming    — operator_resuming → active
//   - TestQueueOpDrain_ResumeNoOpWhenActive                — already active → no-op on resume
//   - TestQueueOpDrain_PausedByFailureNotResumedByDrain    — paused-by-failure unaffected by operator_resuming
//   - TestQueueOpDrain_QueuePausedEventEmitted             — queue_paused{operator_drain} emitted on pause
//   - TestQueueOpDrain_PauseSurvivesReload                 — integration: persisted paused-by-drain survives Load
//
// Spec ref: specs/queue-model.md §8.5 QM-054, §8.6 QM-055.
// Bead ref: hk-7urls.

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// queueOpDrainFixtureSealedBus returns a sealed in-memory EventBus for tests
// that do not need to subscribe additional consumers before Seal.
func queueOpDrainFixtureSealedBus(t *testing.T) eventbus.EventBus {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("queueOpDrainFixtureSealedBus: Seal: %v", err)
	}
	return bus
}

// queueOpDrainFixtureConsumer constructs a QueueOperatorEventConsumer with the
// given store and an in-memory bus. The bus is NOT yet sealed so the caller can
// subscribe the consumer before sealing.
func queueOpDrainFixtureConsumer(
	t *testing.T,
	qs *daemon.QueueStore,
	bus eventbus.EventBus,
) *daemon.QueueOperatorEventConsumer {
	t.Helper()
	return daemon.ExportedNewQueueOperatorEventConsumer(daemon.ExportedQueueOperatorEventConsumerConfig{
		QueueStore: qs,
		Bus:        bus,
	})
}

// queueOpDrainFixtureActiveQueue builds a minimal *queue.Queue with status active.
func queueOpDrainFixtureActiveQueue(t *testing.T) *queue.Queue {
	t.Helper()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qopd-" + t.Name(),
		SubmittedAt:   time.Now().UTC(),
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{},
				CreatedAt:  time.Now().UTC(),
			},
		},
		Status: queue.QueueStatusActive,
	}
}

// queueOpDrainFixtureSynthEvent builds a minimal core.Event with the given
// type and JSON payload.
func queueOpDrainFixtureSynthEvent(t *testing.T, evtType string, payload interface{}) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("queueOpDrainFixtureSynthEvent: NewV7 for EventID: %v", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("queueOpDrainFixtureSynthEvent: marshal payload: %v", err)
	}
	return core.Event{
		EventID:         core.EventID(evID),
		SchemaVersion:   1,
		Type:            evtType,
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(payloadJSON),
	}
}

// queueOpDrainFixturePauseEvent returns a synthetic operator_pause_status event.
func queueOpDrainFixturePauseEvent(t *testing.T, status core.OperatorPauseStatusValue) core.Event {
	t.Helper()
	payload := core.OperatorPauseStatusPayload{
		Status:    status,
		ChangedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return queueOpDrainFixtureSynthEvent(t, string(core.EventTypeOperatorPauseStatus), payload)
}

// queueOpDrainFixtureResumingEvent returns a synthetic operator_resuming event.
func queueOpDrainFixtureResumingEvent(t *testing.T) core.Event {
	t.Helper()
	payload := core.OperatorResumingPayload{
		ResumedAt: time.Now().UTC().Format(time.RFC3339),
	}
	return queueOpDrainFixtureSynthEvent(t, string(core.EventTypeOperatorResuming), payload)
}

// ─────────────────────────────────────────────────────────────────────────────
// State-transition unit tests
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueOpDrain_ActiveToPausedByDrain_OnPausing verifies that an
// operator_pause_status{status:pausing} event transitions an active queue to
// paused-by-drain (QM-054).
func TestQueueOpDrain_ActiveToPausedByDrain_OnPausing(t *testing.T) {
	t.Parallel()

	bus := queueOpDrainFixtureSealedBus(t)
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)

	q := queueOpDrainFixtureActiveQueue(t)
	qs.SetQueue(q)

	evt := queueOpDrainFixturePauseEvent(t, core.OperatorPauseStatusValuePausing)
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus: %v", err)
	}

	got := qs.Queue()
	if got == nil {
		t.Fatal("queue was cleared unexpectedly; want paused-by-drain")
	}
	if got.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("queue.Status = %q, want %q", got.Status, queue.QueueStatusPausedByDrain)
	}
}

// TestQueueOpDrain_ActiveToPausedByDrain_OnPaused verifies that an
// operator_pause_status{status:paused} event also transitions to paused-by-drain.
func TestQueueOpDrain_ActiveToPausedByDrain_OnPaused(t *testing.T) {
	t.Parallel()

	bus := queueOpDrainFixtureSealedBus(t)
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)

	qs.SetQueue(queueOpDrainFixtureActiveQueue(t))

	evt := queueOpDrainFixturePauseEvent(t, core.OperatorPauseStatusValuePaused)
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus: %v", err)
	}

	got := qs.Queue()
	if got == nil {
		t.Fatal("queue was cleared unexpectedly; want paused-by-drain")
	}
	if got.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("queue.Status = %q, want %q", got.Status, queue.QueueStatusPausedByDrain)
	}
}

// TestQueueOpDrain_NoOpWhenAlreadyPausedByDrain verifies that a duplicate pause
// event is idempotent: queue stays paused-by-drain and no error is returned.
func TestQueueOpDrain_NoOpWhenAlreadyPausedByDrain(t *testing.T) {
	t.Parallel()

	bus := queueOpDrainFixtureSealedBus(t)
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)

	q := queueOpDrainFixtureActiveQueue(t)
	q.Status = queue.QueueStatusPausedByDrain
	qs.SetQueue(q)

	evt := queueOpDrainFixturePauseEvent(t, core.OperatorPauseStatusValuePausing)
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus (duplicate): %v", err)
	}

	got := qs.Queue()
	if got.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("idempotent: queue.Status = %q, want %q", got.Status, queue.QueueStatusPausedByDrain)
	}
}

// TestQueueOpDrain_NoOpWhenNilQueue verifies that a pause event is a no-op
// when no queue is loaded.
func TestQueueOpDrain_NoOpWhenNilQueue(t *testing.T) {
	t.Parallel()

	bus := queueOpDrainFixtureSealedBus(t)
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)
	// qs has no queue loaded

	evt := queueOpDrainFixturePauseEvent(t, core.OperatorPauseStatusValuePausing)
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus (nil queue): %v", err)
	}

	if qs.Queue() != nil {
		t.Error("queue was set unexpectedly after pause with no queue loaded")
	}
}

// TestQueueOpDrain_PausedByDrainToActive_OnResuming verifies that an
// operator_resuming event transitions a paused-by-drain queue back to active.
func TestQueueOpDrain_PausedByDrainToActive_OnResuming(t *testing.T) {
	t.Parallel()

	bus := queueOpDrainFixtureSealedBus(t)
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)

	q := queueOpDrainFixtureActiveQueue(t)
	q.Status = queue.QueueStatusPausedByDrain
	qs.SetQueue(q)

	evt := queueOpDrainFixtureResumingEvent(t)
	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming: %v", err)
	}

	got := qs.Queue()
	if got == nil {
		t.Fatal("queue was cleared unexpectedly; want active")
	}
	if got.Status != queue.QueueStatusActive {
		t.Errorf("queue.Status = %q, want %q", got.Status, queue.QueueStatusActive)
	}
}

// TestQueueOpDrain_ResumeNoOpWhenActive verifies that operator_resuming is a
// no-op when the queue is already active (idempotency).
func TestQueueOpDrain_ResumeNoOpWhenActive(t *testing.T) {
	t.Parallel()

	bus := queueOpDrainFixtureSealedBus(t)
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)

	qs.SetQueue(queueOpDrainFixtureActiveQueue(t))

	evt := queueOpDrainFixtureResumingEvent(t)
	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming (already active): %v", err)
	}

	got := qs.Queue()
	if got.Status != queue.QueueStatusActive {
		t.Errorf("idempotent resume: queue.Status = %q, want %q", got.Status, queue.QueueStatusActive)
	}
}

// TestQueueOpDrain_PausedByFailureNotResumedByDrain verifies that a queue in
// paused-by-failure state is NOT affected by operator_resuming — the consumer
// only acts on paused-by-drain transitions (QM-054 / QM-055).
func TestQueueOpDrain_PausedByFailureNotResumedByDrain(t *testing.T) {
	t.Parallel()

	bus := queueOpDrainFixtureSealedBus(t)
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)

	q := queueOpDrainFixtureActiveQueue(t)
	q.Status = queue.QueueStatusPausedByFailure
	qs.SetQueue(q)

	evt := queueOpDrainFixtureResumingEvent(t)
	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming (paused-by-failure): %v", err)
	}

	got := qs.Queue()
	if got.Status != queue.QueueStatusPausedByFailure {
		t.Errorf("paused-by-failure must be unaffected by operator_resuming: got %q, want %q",
			got.Status, queue.QueueStatusPausedByFailure)
	}
}

// TestQueueOpDrain_QueuePausedEventEmitted verifies that exactly one
// queue_paused{reason: "operator_drain"} event is emitted when an active queue
// transitions to paused-by-drain (QM-054 step 2).
//
// The test uses a real bus with a synchronous subscriber to capture emissions.
func TestQueueOpDrain_QueuePausedEventEmitted(t *testing.T) {
	t.Parallel()

	bus := eventbus.NewBusImpl()
	qs := daemon.ExportedNewQueueStore()
	c := queueOpDrainFixtureConsumer(t, qs, bus)

	// Capture queue_paused events via a synchronous subscriber.
	var capturedPayloads []core.QueuePausedPayload
	sub := core.Subscription{
		ConsumerID:    "test-capture-queue-paused",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[string]struct{}{
				string(core.EventTypeQueuePaused): {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, evt core.Event) error {
			var p core.QueuePausedPayload
			if err := json.Unmarshal(evt.Payload, &p); err != nil {
				t.Errorf("unmarshal queue_paused payload: %v", err)
				return err
			}
			capturedPayloads = append(capturedPayloads, p)
			return nil
		},
	}
	if _, err := bus.Subscribe(sub); err != nil {
		t.Fatalf("bus.Subscribe test capture: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}

	q := queueOpDrainFixtureActiveQueue(t)
	qs.SetQueue(q)

	evt := queueOpDrainFixturePauseEvent(t, core.OperatorPauseStatusValuePausing)
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus: %v", err)
	}

	if len(capturedPayloads) != 1 {
		t.Fatalf("queue_paused event count = %d, want 1", len(capturedPayloads))
	}
	got := capturedPayloads[0]
	if got.Reason != "operator_drain" {
		t.Errorf("queue_paused.reason = %q, want %q", got.Reason, "operator_drain")
	}
	if got.QueueID == "" {
		t.Error("queue_paused.queue_id is empty")
	}
	if got.GroupIndex < 0 {
		t.Errorf("queue_paused.group_index = %d, want >= 0", got.GroupIndex)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Integration test: persisted pause survives Load (QM-055)
// ─────────────────────────────────────────────────────────────────────────────

// TestQueueOpDrain_PauseSurvivesReload verifies the QM-055 requirement: a queue
// paused via transitionToPausedByDrain that is written to disk via queue.Persist
// loads back as paused-by-drain when read through queue.Load.
//
// This test does NOT use the consumer directly; it exercises the persistence
// path that the consumer invokes — queue.Persist followed by queue.Load —
// to confirm that paused-by-drain status is round-tripped correctly.
//
// Spec ref: specs/queue-model.md §8.6 QM-055.
func TestQueueOpDrain_PauseSurvivesReload(t *testing.T) {
	t.Parallel()

	// Set up a temporary project directory.
	projectDir := t.TempDir()
	harmonikDir := projectDir + "/.harmonik"
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}

	// Build and persist a paused-by-drain queue.
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "qopd-persist-test",
		SubmittedAt:   time.Now().UTC(),
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{},
				CreatedAt:  time.Now().UTC(),
			},
		},
		Status: queue.QueueStatusPausedByDrain,
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	// Reload and assert status is preserved.
	loaded, err := queue.Load(context.Background(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil; want the persisted queue")
	}
	if loaded.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("reloaded queue.Status = %q, want %q", loaded.Status, queue.QueueStatusPausedByDrain)
	}
}
