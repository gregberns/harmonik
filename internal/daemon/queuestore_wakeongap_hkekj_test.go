package daemon_test

// queuestore_wakeongap_hkekj_test.go — wake-gap fix tests (hk-ekj).
//
// Coverage:
//   - Resume case: handleOperatorResuming signals WakeCh after transitioning
//     a paused-by-drain queue back to active so the idle workloop unblocks.
//   - No-op resume: when no queue is paused, WakeCh is NOT spuriously signaled.
//   - Startup case: QueueStore.Wake() delivers a signal after startup
//     queue-load so the idle workloop unblocks on its first tick.
//
// Spec ref: specs/queue-model.md §8.6 QM-055 (pause survives restart).
// Bead ref: hk-ekj.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// wakeGapFixturePausedQueue returns a queue whose status is paused-by-drain.
func wakeGapFixturePausedQueue(t *testing.T, name string) *queue.Queue {
	t.Helper()
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "wg-" + name,
		Name:          name,
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusPausedByDrain,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: "wg-bead-001", Status: queue.ItemStatusPending},
				},
				CreatedAt: time.Now().UTC(),
			},
		},
	}
}

// wakeGapFixtureResumeEvent builds an operator_resuming event payload.
func wakeGapFixtureResumeEvent(t *testing.T, queueName string) core.Event {
	t.Helper()
	payload := core.OperatorResumingPayload{
		ResumedAt: time.Now().UTC().Format(time.RFC3339),
		QueueName: queueName,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("wakeGapFixtureResumeEvent: marshal: %v", err)
	}
	return core.Event{
		Type:    string(core.EventTypeOperatorResuming),
		Payload: raw,
	}
}

// wakeGapFixtureConsumer builds a QueueOperatorEventConsumer over a sealed bus.
func wakeGapFixtureConsumer(t *testing.T, qs *daemon.QueueStore) *daemon.QueueOperatorEventConsumer {
	t.Helper()
	bus := eventbus.NewBusImpl()
	c := daemon.ExportedNewQueueOperatorEventConsumer(daemon.ExportedQueueOperatorEventConsumerConfig{
		QueueStore: qs,
		Bus:        bus,
	})
	if err := bus.Seal(); err != nil {
		t.Fatalf("wakeGapFixtureConsumer: Seal: %v", err)
	}
	return c
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestWakeGap_ResumeSignalsWakeCh verifies that handleOperatorResuming fires
// WakeCh after transitioning a paused-by-drain queue to active. Without this,
// a workloop blocked in workloopIdleWait would not wake until the next
// submit/append — the ~8-min wake-gap observed in the gurney-q repro (hk-ekj).
func TestWakeGap_ResumeSignalsWakeCh(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()
	q := wakeGapFixturePausedQueue(t, "main")
	qs.SetQueue(q)

	// Drain the SetQueue signal so we start with an empty wake channel.
	select {
	case <-qs.WakeCh():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SetQueue did not signal WakeCh")
	}

	// Confirm channel is now empty.
	select {
	case <-qs.WakeCh():
		t.Fatal("unexpected signal before resume")
	default:
	}

	c := wakeGapFixtureConsumer(t, qs)
	evt := wakeGapFixtureResumeEvent(t, "main")

	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming: %v", err)
	}

	// Resume must have signaled WakeCh (hk-ekj fix).
	select {
	case <-qs.WakeCh():
		// pass
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WakeCh not signaled after queue resume (wake-gap regression)")
	}

	// Queue status must be active now.
	got := qs.Queue()
	if got == nil {
		t.Fatal("queue absent after resume")
	}
	if got.Status != queue.QueueStatusActive {
		t.Fatalf("queue status after resume: got %q, want %q", got.Status, queue.QueueStatusActive)
	}
}

// TestWakeGap_ResumeNoop_NoSpuriousWake verifies that when no queue is
// paused-by-drain, handleOperatorResuming does NOT spuriously signal WakeCh.
func TestWakeGap_ResumeNoop_NoSpuriousWake(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()

	// Load an ACTIVE (not paused) queue.
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "wg-noop-q",
		Name:          "main",
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items:      []queue.Item{{BeadID: "wg-noop-bead", Status: queue.ItemStatusPending}},
				CreatedAt:  time.Now().UTC(),
			},
		},
	}
	qs.SetQueue(q)

	// Drain the SetQueue signal.
	select {
	case <-qs.WakeCh():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SetQueue did not signal WakeCh for active queue")
	}

	c := wakeGapFixtureConsumer(t, qs)
	evt := wakeGapFixtureResumeEvent(t, "main")

	if err := daemon.ExportedQueueOpConsumerHandleResuming(c, context.Background(), evt); err != nil {
		t.Fatalf("handleOperatorResuming on already-active queue: %v", err)
	}

	// No transition → no wake signal expected.
	select {
	case <-qs.WakeCh():
		t.Fatal("spurious WakeCh signal when no paused-by-drain queue was transitioned")
	default:
		// pass
	}
}

// TestWakeGap_StartupLoadWakesWorkloop verifies that QueueStore.Wake() delivers
// a signal that a workloop goroutine can receive — modelling the daemon startup
// path (daemon.Start calls qs.Wake() after LoadQueueAtStartup, hk-ekj).
func TestWakeGap_StartupLoadWakesWorkloop(t *testing.T) {
	t.Parallel()

	qs := daemon.ExportedNewQueueStore()

	// Simulate startup: install a queue (mirroring the daemon.Start loop that
	// calls qs.SetQueue for each loadedQueue), then fire a defensive Wake().
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "wg-startup-q",
		Name:          "main",
		SubmittedAt:   time.Now().UTC(),
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusPending,
				Items: []queue.Item{
					{BeadID: "wg-startup-bead-001", Status: queue.ItemStatusPending},
					{BeadID: "wg-startup-bead-002", Status: queue.ItemStatusPending},
					{BeadID: "wg-startup-bead-003", Status: queue.ItemStatusPending},
				},
				CreatedAt: time.Now().UTC(),
			},
		},
	}
	qs.SetQueue(q)

	// Drain the SetQueue signal to model the case where the startup signal was
	// already consumed by an early workloop-sleep before workloopIdleWait.
	select {
	case <-qs.WakeCh():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SetQueue did not signal WakeCh at startup")
	}

	// Now simulate the defensive Wake() added after the loadedQueues loop.
	qs.Wake()

	// A simulated workloop blocked in workloopIdleWait should unblock.
	select {
	case <-qs.WakeCh():
		// pass
	case <-time.After(100 * time.Millisecond):
		t.Fatal("WakeCh not signaled by startup Wake() — workloop would not unblock (hk-ekj)")
	}
}
