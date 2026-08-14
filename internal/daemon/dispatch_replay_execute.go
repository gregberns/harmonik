package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
)

type dispatchReplayClaimLedger interface {
	ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error
}

type dispatchReplayExecutor struct {
	projectDir   string
	intentLogDir string
	claimLedger  dispatchReplayClaimLedger
}

func (e dispatchReplayExecutor) execute(ctx context.Context, step dispatchReplayStep) error {
	switch step.Action {
	case dispatch.ReplayReservation:
		return replayPreparedReservation(ctx, e.projectDir, step.Intent)
	case dispatch.ReplayClaim:
		return e.replayClaim(ctx, step.Intent)
	default:
		return fmt.Errorf("daemon: dispatch replay executor does not support action %q for run %s", step.Action, step.Intent.Binding.RunID)
	}
}

func (e dispatchReplayExecutor) replayClaim(ctx context.Context, intent dispatch.Intent) error {
	if e.claimLedger == nil {
		return fmt.Errorf("daemon: dispatch replay claim ledger is not configured")
	}
	if intent.Phase != dispatch.PhasePrepared {
		return fmt.Errorf("daemon: replay claim requires prepared intent, got %q", intent.Phase)
	}
	claimErr := e.claimLedger.ClaimBead(
		ctx,
		e.intentLogDir,
		brcli.TimeoutConfig{},
		intent.Binding.RunID,
		intent.Binding.ClaimTransitionID,
		intent.Binding.BeadID,
	)
	var next dispatch.Intent
	var err error
	switch {
	case claimErr == nil:
		next, err = intent.WithClaimDurable()
	case errors.Is(claimErr, brcli.ErrClaimDependencyBlocked):
		next, err = intent.WithClaimRefused(dispatch.ClaimRefusalDependency)
	default:
		return fmt.Errorf("daemon: replay exact claim: %w", claimErr)
	}
	if err != nil {
		return err
	}
	if err := dispatchstore.New(e.projectDir).Advance(intent, next); err != nil {
		return fmt.Errorf("daemon: advance dispatch intent after claim: %w", err)
	}
	return nil
}

func executeDispatchReplayPlan(
	ctx context.Context,
	steps []dispatchReplayStep,
	executor dispatchReplayExecutor,
) error {
	for _, step := range steps {
		switch step.Action {
		case dispatch.ReplayReservation, dispatch.ReplayClaim:
		default:
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
