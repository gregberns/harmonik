package daemon

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/schedule"
)

func TestSchedulePort_SharesLoadedStoreWithQuiesceArbiter(t *testing.T) {
	t.Parallel()

	store := schedule.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}
	port := newSchedulePort("", t.TempDir(), nil, store, nil)
	arbiter := &QuiesceArbiter{}
	arbiter.SetScheduleStore(port.store)

	if port.store != store {
		t.Fatal("schedule port did not retain the loaded store")
	}
	if arbiter.cfg.ScheduleStore != port.store {
		t.Fatal("quiesce arbiter and schedule port do not share one store")
	}
	if port.wakeC == nil {
		t.Fatal("schedule port has no mutation wake channel")
	}
}

func TestScheduleAwareIdleWait_UsesSeparateScheduleAndQueueWakes(t *testing.T) {
	t.Parallel()

	store := schedule.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("load store: %v", err)
	}
	port := schedulePort{store: store, wakeC: store.WakeCh()}
	if err := store.Add(schedule.ScheduledJob{
		ID:       "enabled",
		Schedule: schedule.Schedule{Kind: schedule.ScheduleKindEvery, Interval: "1h"},
		Action:   schedule.Action{Kind: schedule.ActionKindCommand, Argv: []string{"true"}},
		Enabled:  true,
	}); err != nil {
		t.Fatalf("add schedule: %v", err)
	}
	if err := scheduleAwareIdleWait(context.Background(), port, make(chan struct{})); err != nil {
		t.Fatalf("schedule wake: %v", err)
	}

	// Consume the mutation wake from Add. The schedule remains armed, so this
	// next wait proves the independent queue submit channel still wakes it.
	queueWake := make(chan struct{}, 1)
	queueWake <- struct{}{}
	if err := scheduleAwareIdleWait(context.Background(), port, queueWake); err != nil {
		t.Fatalf("queue wake: %v", err)
	}
}
