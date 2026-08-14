package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

type dispatchReplayClaimLedger interface {
	ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error
}

type dispatchReplayExecutor struct {
	projectDir   string
	intentLogDir string
	claimLedger  dispatchReplayClaimLedger
	now          func() time.Time
}

func (e dispatchReplayExecutor) execute(ctx context.Context, step dispatchReplayStep) error {
	switch step.Action {
	case dispatch.ReplayReservation:
		return replayPreparedReservation(ctx, e.projectDir, step.Intent)
	case dispatch.ReplayClaim:
		return e.replayClaim(ctx, step.Intent)
	case dispatch.WriteRunRecord:
		return e.writeRunRecord(step.Intent)
	case dispatch.AdvanceRunPhase:
		return e.advanceRunPhase(step.Intent)
	default:
		return fmt.Errorf("daemon: dispatch replay executor does not support action %q for run %s", step.Action, step.Intent.Binding.RunID)
	}
}

func (e dispatchReplayExecutor) writeRunRecord(intent dispatch.Intent) error {
	if intent.Phase != dispatch.PhaseClaimDurable {
		return fmt.Errorf("daemon: replay run record requires claim_durable intent, got %q", intent.Phase)
	}
	if e.now == nil {
		return fmt.Errorf("daemon: dispatch replay clock is not configured")
	}
	record, err := runpkg.NewDispatchRecord(intent.Binding, e.now())
	if err != nil {
		return err
	}
	if err := runpkg.CreateDispatchRecord(e.projectDir, record); err != nil {
		return fmt.Errorf("daemon: create dispatch run record: %w", err)
	}
	return nil
}

func (e dispatchReplayExecutor) advanceRunPhase(intent dispatch.Intent) error {
	next, err := intent.WithRunDurable()
	if err != nil {
		return err
	}
	if err := dispatchstore.New(e.projectDir).Advance(intent, next); err != nil {
		return fmt.Errorf("daemon: advance dispatch intent after run record: %w", err)
	}
	return nil
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
		case dispatch.ReplayReservation, dispatch.ReplayClaim, dispatch.WriteRunRecord, dispatch.AdvanceRunPhase:
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
