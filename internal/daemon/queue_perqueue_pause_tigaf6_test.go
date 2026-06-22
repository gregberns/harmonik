package daemon_test

// queue_perqueue_pause_tigaf6_test.go — NQ-C1 per-queue pause/resume tests (hk-tigaf.6).
//
// Acceptance criteria (per bead spec):
//   - Named pause halts only the named queue; other queues keep going.
//   - Named resume restores the paused queue to active.
//   - Unnamed global pause still drains all queues.
//   - Named pause does NOT set the global IsPaused flag (br-ready gate unaffected).
//   - Global IsPaused remains false after a per-queue pause.
//
// Coverage:
//   - TestPerQueuePause_OnlyNamedQueueIsPaused           — named pause drains only the target
//   - TestPerQueuePause_OtherQueueUnaffected             — non-targeted queue stays active
//   - TestPerQueuePause_NamedResume_RestoresQueue        — named resume transitions back to active
//   - TestPerQueuePause_GlobalPauseDrainsAll             — unnamed pause transitions all queues
//   - TestPerQueuePause_DoesNotSetGlobalFlag             — IsPaused() stays false after named pause
//   - TestPerQueuePause_EmitsQueuePausedEventForTarget   — queue_paused{operator_drain} emitted for named queue
//   - TestPerQueuePause_GlobalResumeRestoresAll          — global resume restores all paused queues
//
// Bead ref: hk-tigaf.6.

import (
	"context"
	"encoding/json"
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

func perQueueFixtureBus(t *testing.T) eventbus.EventBus {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("perQueueFixtureBus: Seal: %v", err)
	}
	return bus
}

func perQueueFixtureActiveQueue(t *testing.T, name string) *queue.Queue {
	t.Helper()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "pq-" + name + "-" + t.Name(),
		Name:          name,
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

// perQueueFixtureConsumerWithBus builds a QueueOperatorEventConsumer + bus that
// is not yet sealed (so callers can subscribe before sealing if needed).
func perQueueFixtureConsumerWithBus(t *testing.T, qs *daemon.QueueStore) (*daemon.QueueOperatorEventConsumer, eventbus.EventBus) {
	t.Helper()
	bus := eventbus.NewBusImpl()
	c := daemon.ExportedNewQueueOperatorEventConsumer(daemon.ExportedQueueOperatorEventConsumerConfig{
		QueueStore: qs,
		Bus:        bus,
	})
	if err := bus.Seal(); err != nil {
		t.Fatalf("perQueueFixtureConsumerWithBus: Seal: %v", err)
	}
	return c, bus
}

func perQueueFixturePauseEvent(t *testing.T, status core.OperatorPauseStatusValue, queueName string) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("perQueueFixturePauseEvent: NewV7: %v", err)
	}
	payload := core.OperatorPauseStatusPayload{
		Status:    status,
		ChangedAt: time.Now().UTC().Format(time.RFC3339),
		QueueName: queueName,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("perQueueFixturePauseEvent: marshal: %v", err)
	}
	return core.Event{
		EventID:         core.EventID(evID),
		SchemaVersion:   1,
		Type:            string(core.EventTypeOperatorPauseStatus),
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(raw),
	}
}

func perQueueFixtureResumingEvent(t *testing.T, queueName string) core.Event {
	t.Helper()
	evID, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("perQueueFixtureResumingEvent: NewV7: %v", err)
	}
	payload := core.OperatorResumingPayload{
		ResumedAt: time.Now().UTC().Format(time.RFC3339),
		QueueName: queueName,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("perQueueFixtureResumingEvent: marshal: %v", err)
	}
	return core.Event{
		EventID:         core.EventID(evID),
		SchemaVersion:   1,
		Type:            string(core.EventTypeOperatorResuming),
		TimestampWall:   time.Now(),
		SourceSubsystem: "test",
		Payload:         json.RawMessage(raw),
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestPerQueuePause_OnlyNamedQueueIsPaused verifies that a named pause event
// transitions only the named queue to paused-by-drain and leaves other queues
// unchanged.
func TestPerQueuePause_OnlyNamedQueueIsPaused(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	c, _ := perQueueFixtureConsumerWithBus(t, qs)

	investigateQ := perQueueFixtureActiveQueue(t, "investigate")
	mainQ := perQueueFixtureActiveQueue(t, "main")
	qs.SetQueue(investigateQ)
	qs.SetQueue(mainQ)

	// Named pause targeting "investigate".
	evt := perQueueFixturePauseEvent(t, core.OperatorPauseStatusValuePausing, "investigate")
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus: %v", err)
	}

	gotInvestigate := qs.QueueByName("investigate")
	if gotInvestigate == nil {
		t.Fatal("investigate queue cleared unexpectedly")
	}
	if gotInvestigate.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("investigate.Status = %q, want paused-by-drain", gotInvestigate.Status)
	}
}

// TestPerQueuePause_OtherQueueUnaffected verifies that the non-targeted queue
// remains active after a named pause.
func TestPerQueuePause_OtherQueueUnaffected(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	c, _ := perQueueFixtureConsumerWithBus(t, qs)

	investigateQ := perQueueFixtureActiveQueue(t, "investigate")
	mainQ := perQueueFixtureActiveQueue(t, "main")
	qs.SetQueue(investigateQ)
	qs.SetQueue(mainQ)

	evt := perQueueFixturePauseEvent(t, core.OperatorPauseStatusValuePausing, "investigate")
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus: %v", err)
	}

	gotMain := qs.QueueByName("main")
	if gotMain == nil {
		t.Fatal("main queue cleared unexpectedly")
	}
	if gotMain.Status != queue.QueueStatusActive {
		t.Errorf("main.Status = %q, want active (should be unaffected by named pause)", gotMain.Status)
	}
}

// TestPerQueuePause_NamedResume_RestoresQueue verifies that a named resume
// event transitions the named queue from paused-by-drain back to active.
func TestPerQueuePause_NamedResume_RestoresQueue(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	c, _ := perQueueFixtureConsumerWithBus(t, qs)

	investigateQ := perQueueFixtureActiveQueue(t, "investigate")
	investigateQ.Status = queue.QueueStatusPausedByDrain
	qs.SetQueue(investigateQ)

	evt := perQueueFixtureResumingEvent(t, "investigate")
	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming: %v", err)
	}

	got := qs.QueueByName("investigate")
	if got == nil {
		t.Fatal("investigate queue cleared unexpectedly")
	}
	if got.Status != queue.QueueStatusActive {
		t.Errorf("investigate.Status = %q, want active after named resume", got.Status)
	}
}

// TestPerQueuePause_GlobalPauseDrainsAll verifies that a global pause event
// (QueueName="") transitions ALL active queues to paused-by-drain.
func TestPerQueuePause_GlobalPauseDrainsAll(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	c, _ := perQueueFixtureConsumerWithBus(t, qs)

	investigateQ := perQueueFixtureActiveQueue(t, "investigate")
	mainQ := perQueueFixtureActiveQueue(t, "main")
	qs.SetQueue(investigateQ)
	qs.SetQueue(mainQ)

	// Global pause: no queue name.
	evt := perQueueFixturePauseEvent(t, core.OperatorPauseStatusValuePausing, "")
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus (global): %v", err)
	}

	for _, name := range []string{"investigate", "main"} {
		q := qs.QueueByName(name)
		if q == nil {
			t.Fatalf("%s queue cleared unexpectedly", name)
		}
		if q.Status != queue.QueueStatusPausedByDrain {
			t.Errorf("%s.Status = %q, want paused-by-drain after global pause", name, q.Status)
		}
	}
}

// TestPerQueuePause_DoesNotSetGlobalFlag verifies that a per-queue pause does
// NOT set the OperatorPauseController.IsPaused() global flag (the EM-067
// br-ready gate must remain false).
func TestPerQueuePause_DoesNotSetGlobalFlag(t *testing.T) {
	t.Parallel()

	col := &stubEventCollector{}
	ctrl := daemon.ExportedNewOperatorPauseController(col)

	if err := ctrl.HandleOperatorPause(context.Background(), "investigate"); err != nil {
		t.Fatalf("HandleOperatorPause(named): %v", err)
	}

	if ctrl.IsPaused() {
		t.Error("IsPaused() = true after per-queue pause; br-ready gate should remain clear")
	}
}

// TestPerQueuePause_EmitsQueuePausedEventForTarget verifies that exactly one
// queue_paused{reason: "operator_drain"} event is emitted for the targeted
// queue when a named pause triggers the consumer.
func TestPerQueuePause_EmitsQueuePausedEventForTarget(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	bus := eventbus.NewBusImpl()
	c := daemon.ExportedNewQueueOperatorEventConsumer(daemon.ExportedQueueOperatorEventConsumerConfig{
		QueueStore: qs,
		Bus:        bus,
	})

	var capturedPayloads []core.QueuePausedPayload
	sub := core.Subscription{
		ConsumerID:    "test-capture-per-queue-paused",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{core.EventTypeQueuePaused: {}},
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
		t.Fatalf("bus.Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("bus.Seal: %v", err)
	}

	investigateQ := perQueueFixtureActiveQueue(t, "investigate")
	mainQ := perQueueFixtureActiveQueue(t, "main")
	qs.SetQueue(investigateQ)
	qs.SetQueue(mainQ)

	evt := perQueueFixturePauseEvent(t, core.OperatorPauseStatusValuePausing, "investigate")
	if err := daemon.ExportedQueueOpConsumerHandlePauseStatus(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorPauseStatus: %v", err)
	}

	if len(capturedPayloads) != 1 {
		t.Fatalf("queue_paused event count = %d, want 1 (only for 'investigate')", len(capturedPayloads))
	}
	got := capturedPayloads[0]
	if got.Reason != "operator_drain" {
		t.Errorf("queue_paused.reason = %q, want %q", got.Reason, "operator_drain")
	}
	if got.QueueID != investigateQ.QueueID {
		t.Errorf("queue_paused.queue_id = %q, want investigate queue %q", got.QueueID, investigateQ.QueueID)
	}
}

// TestPerQueuePause_GlobalResumeRestoresAll verifies that a global resume event
// (QueueName="") transitions ALL paused-by-drain queues back to active.
func TestPerQueuePause_GlobalResumeRestoresAll(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	c, _ := perQueueFixtureConsumerWithBus(t, qs)

	for _, name := range []string{"investigate", "main", "extra"} {
		q := perQueueFixtureActiveQueue(t, name)
		q.Status = queue.QueueStatusPausedByDrain
		qs.SetQueue(q)
	}

	evt := perQueueFixtureResumingEvent(t, "") // global resume
	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming (global): %v", err)
	}

	for _, name := range []string{"investigate", "main", "extra"} {
		q := qs.QueueByName(name)
		if q == nil {
			t.Fatalf("%s queue cleared unexpectedly", name)
		}
		if q.Status != queue.QueueStatusActive {
			t.Errorf("%s.Status = %q, want active after global resume", name, q.Status)
		}
	}
}

// TestPerQueuePause_NamedResumeDoesNotRestoreOtherQueues verifies that a named
// resume only restores the targeted queue and leaves other paused queues alone.
func TestPerQueuePause_NamedResumeDoesNotRestoreOtherQueues(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	c, _ := perQueueFixtureConsumerWithBus(t, qs)

	for _, name := range []string{"investigate", "main"} {
		q := perQueueFixtureActiveQueue(t, name)
		q.Status = queue.QueueStatusPausedByDrain
		qs.SetQueue(q)
	}

	evt := perQueueFixtureResumingEvent(t, "investigate")
	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming (named): %v", err)
	}

	// investigate should be resumed.
	investigate := qs.QueueByName("investigate")
	if investigate == nil {
		t.Fatal("investigate queue cleared unexpectedly")
	}
	if investigate.Status != queue.QueueStatusActive {
		t.Errorf("investigate.Status = %q, want active after named resume", investigate.Status)
	}

	// main should remain paused-by-drain.
	main := qs.QueueByName("main")
	if main == nil {
		t.Fatal("main queue cleared unexpectedly")
	}
	if main.Status != queue.QueueStatusPausedByDrain {
		t.Errorf("main.Status = %q, want paused-by-drain (unaffected by investigate resume)", main.Status)
	}
}
