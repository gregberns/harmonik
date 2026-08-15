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
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workers"
)

type dispatchReplayClaimLedger interface {
	ClaimBead(context.Context, string, brcli.TimeoutConfig, core.RunID, core.TransitionID, core.BeadID) error
}

type dispatchReplayExecutor struct {
	projectDir   string
	intentLogDir string
	claimLedger  dispatchReplayClaimLedger
	now          func() time.Time
	workers      *workers.Registry
	localKind    runpkg.ExecutionKind
	advanceRun   func(string, runpkg.DispatchRecord, runpkg.DispatchRecord) error
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
	case dispatch.ResumeProvision:
		return e.bindReplayLocation(ctx, step.Intent)
	default:
		return fmt.Errorf("daemon: dispatch replay executor does not support action %q for run %s", step.Action, step.Intent.Binding.RunID)
	}
}

func (e dispatchReplayExecutor) bindReplayLocation(ctx context.Context, intent dispatch.Intent) error {
	if intent.Phase != dispatch.PhaseRunDurable {
		return fmt.Errorf("daemon: replay location requires run_durable intent, got %q", intent.Phase)
	}
	snapshot, err := queue.Load(ctx, e.projectDir, intent.Binding.QueueName)
	if err != nil {
		return fmt.Errorf("daemon: load queue before replay location: %w", err)
	}
	observation, err := dispatch.ClassifyQueueObservation(intent, snapshot)
	if err != nil {
		return fmt.Errorf("daemon: classify queue before replay location: %w", err)
	}
	if observation.Queue != dispatch.QueueReserved {
		return fmt.Errorf("daemon: replay location requires exact reserved queue item, got %q", observation.Queue)
	}
	records, err := runpkg.ScanRegistry(e.projectDir)
	if err != nil {
		return fmt.Errorf("daemon: scan run record before replay location: %w", err)
	}
	record, err := exactDispatchRecord(intent.Binding.RunID, records.Dispatch)
	if err != nil {
		return err
	}
	if runpkg.ClassifyDispatchRecord(intent, record) != dispatch.RunRecordBase {
		return errors.New("daemon: replay location requires one exact base run record")
	}
	location, selectedRemote, err := e.selectReplayLocation(intent, snapshot)
	if err != nil {
		return err
	}
	located, err := record.BindLocation(location)
	if err != nil {
		if selectedRemote {
			e.workers.ReleaseBoundWorker(intent.Binding.RunID)
		}
		return err
	}
	advance := e.advanceRun
	if advance == nil {
		advance = runpkg.AdvanceDispatchRecord
	}
	if err := advance(e.projectDir, *record, located); err != nil {
		if selectedRemote && releaseSelectedWorkerAfterAdvanceError(err) {
			e.workers.ReleaseBoundWorker(intent.Binding.RunID)
		}
		return fmt.Errorf("daemon: bind replay execution location: %w", err)
	}
	return nil
}

func releaseSelectedWorkerAfterAdvanceError(err error) bool {
	var ambiguous *runpkg.DispatchAmbiguousError
	return !errors.As(err, &ambiguous)
}

func (e dispatchReplayExecutor) selectReplayLocation(
	intent dispatch.Intent,
	snapshot *queue.Queue,
) (runpkg.ExecutionLocation, bool, error) {
	local := func() (runpkg.ExecutionLocation, bool, error) {
		if e.localKind != runpkg.ExecutionLocalIndependent && e.localKind != runpkg.ExecutionLocalShared {
			return runpkg.ExecutionLocation{}, false, errors.New("daemon: local replay execution kind is not configured")
		}
		return runpkg.ExecutionLocation{Kind: e.localKind, RepositoryPath: intent.Binding.RepositoryPath}, false, nil
	}
	if snapshot.LocalOnly || intent.Binding.RepositoryPath != e.projectDir || e.workers == nil {
		return local()
	}
	worker, err := e.workers.SelectBoundWorker(intent.Binding.RunID, snapshot.WorkerTarget)
	if err != nil {
		return runpkg.ExecutionLocation{}, false, fmt.Errorf("daemon: select replay worker: %w", err)
	}
	if worker == nil {
		return local()
	}
	return runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: worker.Name, Transport: worker.Transport,
		Host: worker.Host, RepositoryPath: worker.RepoPath,
	}, true, nil
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
		case dispatch.ReplayReservation, dispatch.ReplayClaim, dispatch.WriteRunRecord, dispatch.AdvanceRunPhase, dispatch.ResumeProvision:
		default:
			return fmt.Errorf("daemon: dispatch replay executor does not support action %q for run %s", step.Action, step.Intent.Binding.RunID)
		}
	}
	if len(steps) == 0 {
		return nil
	}
	step := steps[0]
	if err := executor.execute(ctx, step); err != nil {
		return fmt.Errorf("daemon: execute dispatch replay action %q for run %s: %w", step.Action, step.Intent.Binding.RunID, err)
	}
	return nil
}
