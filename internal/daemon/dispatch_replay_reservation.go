package daemon

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

func replayPreparedReservation(ctx context.Context, projectDir string, intent dispatch.Intent) error {
	if err := intent.Validate(); err != nil {
		return fmt.Errorf("daemon: replay reservation requires a valid prepared intent: %w", err)
	}
	if intent.Phase != dispatch.PhasePrepared {
		return fmt.Errorf("daemon: replay reservation requires prepared phase, got %q", intent.Phase)
	}
	store, err := loadReplayReservationStore(ctx, projectDir)
	if err != nil {
		return err
	}
	binding := intent.Binding
	result := reserveQueueItem(ctx, store, projectDir, queueReservation{
		QueueName: binding.QueueName, QueueID: binding.QueueID,
		GroupIndex: binding.GroupIndex, ItemIndex: binding.ItemIndex,
		BeadID: binding.BeadID, RunID: binding.RunID, ClaimTransitionID: binding.ClaimTransitionID,
	})
	if result.Verdict != reservationReserved && result.Verdict != reservationItemFailed {
		return fmt.Errorf("daemon: replay reservation failed with verdict %q: %w", result.Verdict, result.Err)
	}
	durable, err := queue.Load(ctx, projectDir, binding.QueueName)
	if err != nil {
		return fmt.Errorf("daemon: reload replayed reservation: %w", err)
	}
	observation, err := dispatch.ClassifyQueueObservation(intent, durable)
	if err != nil {
		return err
	}
	if observation.Queue == dispatch.QueueReserved || observation.Preclaim == dispatch.PreclaimMaxAttemptsItemTerminal {
		return nil
	}
	return fmt.Errorf("daemon: replay reservation did not produce exact durable authority")
}

func loadReplayReservationStore(ctx context.Context, projectDir string) (*queuewiring.QueueStore, error) {
	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		return nil, fmt.Errorf("daemon: enumerate queues for replay reservation: %w", err)
	}
	store := queuewiring.NewQueueStore()
	for _, name := range names {
		value, loadErr := queue.Load(ctx, projectDir, name)
		if loadErr != nil {
			return nil, fmt.Errorf("daemon: load queue %q for replay reservation: %w", name, loadErr)
		}
		if value != nil {
			store.SetQueueByName(name, value)
		}
	}
	return store, nil
}
