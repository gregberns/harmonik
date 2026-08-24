package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
	"github.com/gregberns/harmonik/internal/runloop"
)

func TestQueueIdleWaitPollsWhileTransientItemsNeedReevaluation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		snapshot queueIdleSnapshot
		want     queueIdleWait
	}{
		{
			name: "empty queue waits for a schedule or submission",
			want: queueIdleScheduleAware,
		},
		{
			name:     "deferred item polls for a ledger change",
			snapshot: queueIdleSnapshot{HasDeferredItems: true},
			want:     queueIdlePoll,
		},
		{
			name:     "temporarily refused item polls for its cooldown",
			snapshot: queueIdleSnapshot{HasSkippedBeads: true},
			want:     queueIdlePoll,
		},
		{
			name: "both transient conditions still require one poll",
			snapshot: queueIdleSnapshot{
				HasDeferredItems: true,
				HasSkippedBeads:  true,
			},
			want: queueIdlePoll,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := decideQueueIdleWait(tt.snapshot); got != tt.want {
				t.Fatalf("decideQueueIdleWait(%+v) = %v, want %v", tt.snapshot, got, tt.want)
			}
		})
	}
}

func TestRunWorkLoopDelegatesQueueIdleWaitDecision(t *testing.T) {
	queueStore := queuewiring.NewQueueStore()
	queueStore.SetQueueByName("paused", &queue.Queue{
		Name:   "paused",
		Status: queue.QueueStatusPausedByDrain,
	})
	ledger := &ownerPortLedger{}
	runtime := testRuntime{
		env:         runloop.RunEnv{ProjectDir: t.TempDir()},
		handles:     runloop.SharedHandles{LocalInFlight: new(atomic.Int32)},
		ledger:      ledger,
		queueStore:  queueStore,
		runRegistry: newLocalRunRegistry(),
		capacity:    newCapacityPort(1, nil),
		queueSurface: newQueueSurfacePort(
			queueStore.WakeCh(), nil,
		),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observed := make(chan queueIdleSnapshot, 1)
	collaborators := loopCollaborators{
		ledgerRepair:  runtime.ledgerRepair(),
		diskReclaim:   newTestDiskReclaimPort(runtime),
		capacity:      runtime.capacity,
		queueSurface:  runtime.queueSurface,
		dispatchGates: newDispatchGatesPortFromDeps(runtime),
		queueIdleWait: func(snapshot queueIdleSnapshot) queueIdleWait {
			observed <- snapshot
			cancel()
			return queueIdlePoll
		},
	}

	if err := runWorkLoop(ctx, workLoopInput{
		baseEnv: runtime.env, basePorts: runtime.ports, handles: runtime.handles,
		ledger: ledger, queueStore: queueStore, runRegistry: runtime.runRegistry,
	}, collaborators, true); err != nil {
		t.Fatalf("runWorkLoop = %v, want nil", err)
	}

	select {
	case snapshot := <-observed:
		if snapshot.HasDeferredItems || snapshot.HasSkippedBeads {
			t.Fatalf("queue idle snapshot = %+v, want no transient items", snapshot)
		}
	default:
		t.Fatal("runWorkLoop did not delegate the queue idle wait decision")
	}
}
