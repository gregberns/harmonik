package daemon

import (
	"context"
	"fmt"

	"github.com/gregberns/harmonik/internal/dispatch"
)

type dispatchReplayExecutor struct {
	projectDir string
}

func (e dispatchReplayExecutor) execute(ctx context.Context, step dispatchReplayStep) error {
	switch step.Action {
	case dispatch.ReplayReservation:
		return replayPreparedReservation(ctx, e.projectDir, step.Intent)
	default:
		return fmt.Errorf("daemon: dispatch replay executor does not support action %q for run %s", step.Action, step.Intent.Binding.RunID)
	}
}

func executeDispatchReplayPlan(
	ctx context.Context,
	steps []dispatchReplayStep,
	executor dispatchReplayExecutor,
) error {
	for _, step := range steps {
		if step.Action != dispatch.ReplayReservation {
			return fmt.Errorf("daemon: dispatch replay executor does not support action %q for run %s", step.Action, step.Intent.Binding.RunID)
		}
	}
	for _, step := range steps {
		if err := executor.execute(ctx, step); err != nil {
			return fmt.Errorf("daemon: execute dispatch replay action %q for run %s: %w", step.Action, step.Intent.Binding.RunID, err)
		}
	}
	return nil
}
