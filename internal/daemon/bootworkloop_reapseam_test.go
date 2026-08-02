package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

func TestWireStaleWatcherReapSeams_ForceReapPersistsGroupAdvance(t *testing.T) {
	projectDir := t.TempDir()
	queueID := newTestQueueID()
	queueName := "force-reap"
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       queueID,
		Name:          queueName,
		Status:        queue.QueueStatusActive,
		SubmittedAt:   time.Now().UTC(),
		Groups: []queue.Group{{
			GroupIndex: 0,
			Kind:       queue.GroupKindStream,
			Status:     queue.GroupStatusActive,
			Items: []queue.Item{
				{BeadID: "hk-finished", Status: queue.ItemStatusFailed},
				{BeadID: "hk-force-reaped", Status: queue.ItemStatusDispatched},
			},
		}},
	}
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("Persist initial queue: %v", err)
	}

	queueStore := queuewiring.NewQueueStore()
	queueStore.SetQueue(q)
	deps := workLoopDeps{
		bus:         &noopEmitter{},
		projectDir:  projectDir,
		queueStore:  queueStore,
		runRegistry: newLocalRunRegistry(),
	}
	bus := eventbus.NewBusImpl()
	runFailedCount := 0
	if _, err := bus.Subscribe(core.Subscription{
		ConsumerID:    "force-reap-test",
		ConsumerClass: core.ConsumerClassSynchronous,
		EventPattern: core.EventPattern{Types: map[core.EventType]struct{}{
			core.EventTypeRunFailed: {},
		}},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: func(_ context.Context, _ core.Event) error {
			runFailedCount++
			return nil
		},
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := bus.Seal(); err != nil {
		t.Fatalf("Seal: %v", err)
	}

	watcher := NewStaleWatcher(StaleWatcherConfig{Registry: newLocalRunRegistry()})
	bs := &bootState{
		bus:          bus,
		staleWatcher: watcher,
	}
	bs.wireStaleWatcherReapSeams(context.Background(), &deps, loopLifecyclePort{}, deps.testCapacity, deps.testQueueSurface, eagerRefillPort{})

	forceReap := watcher.forceReapFn()
	if forceReap == nil {
		t.Fatal("force-reap callback was not wired")
	}
	groupIndex := 0
	forceReap(core.RunID{}, &RunHandle{
		BeadID:          "hk-force-reaped",
		QueueName:       queueName,
		QueueID:         &queueID,
		QueueGroupIndex: &groupIndex,
		QueueItemIndex:  1,
	})

	if got, want := q.Groups[0].Items[1].Status, queue.ItemStatusFailed; got != want {
		t.Errorf("in-memory force-reaped item status = %q, want %q", got, want)
	}
	if got, want := q.Groups[0].Status, queue.GroupStatusCompleteWithFailures; got != want {
		t.Errorf("in-memory group status = %q, want %q", got, want)
	}
	if got, want := q.Status, queue.QueueStatusPausedByFailure; got != want {
		t.Errorf("in-memory queue status = %q, want %q", got, want)
	}
	if got, want := runFailedCount, 1; got != want {
		t.Errorf("run_failed events = %d, want %d", got, want)
	}

	persisted, err := queue.Load(context.Background(), projectDir, queueName)
	if err != nil {
		t.Fatalf("Load persisted queue: %v", err)
	}
	if persisted == nil {
		t.Fatal("force reap did not leave a durable queue file")
	}
	if got, want := persisted.Groups[0].Items[1].Status, queue.ItemStatusFailed; got != want {
		t.Errorf("force-reaped item status = %q, want %q", got, want)
	}
	if got, want := persisted.Groups[0].Status, queue.GroupStatusCompleteWithFailures; got != want {
		t.Errorf("group status = %q, want %q", got, want)
	}
	if got, want := persisted.Status, queue.QueueStatusPausedByFailure; got != want {
		t.Errorf("queue status = %q, want %q", got, want)
	}
}
