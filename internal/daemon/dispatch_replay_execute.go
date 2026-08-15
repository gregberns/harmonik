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

var errDispatchReplayPending = errors.New("daemon: dispatch replay is pending")

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
	worktrees    dispatchWorktreeObserverResolver
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
		return e.resumeReplayProvision(ctx, step.Intent)
	default:
		return fmt.Errorf("daemon: dispatch replay executor does not support action %q for run %s", step.Action, step.Intent.Binding.RunID)
	}
}

func (e dispatchReplayExecutor) resumeReplayProvision(ctx context.Context, intent dispatch.Intent) error {
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
	switch runpkg.ClassifyDispatchRecord(intent, record) {
	case dispatch.RunRecordBase:
		return e.bindReplayLocation(intent, snapshot, *record)
	case dispatch.RunRecordLocated:
		return e.prepareReplayWorktree(ctx, intent, *record)
	default:
		return errors.New("daemon: replay provision requires one exact base or located run record")
	}
}

func (e dispatchReplayExecutor) bindReplayLocation(
	intent dispatch.Intent,
	snapshot *queue.Queue,
	record runpkg.DispatchRecord,
) error {
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
	if err := advance(e.projectDir, record, located); err != nil {
		if selectedRemote && releaseSelectedWorkerAfterAdvanceError(err) {
			e.workers.ReleaseBoundWorker(intent.Binding.RunID)
		}
		return fmt.Errorf("daemon: bind replay execution location: %w", err)
	}
	return nil
}

func (e dispatchReplayExecutor) prepareReplayWorktree(
	ctx context.Context,
	intent dispatch.Intent,
	record runpkg.DispatchRecord,
) error {
	if record.Location == nil || e.worktrees == nil {
		return errors.New("daemon: dispatch replay worktree provisioner is not configured")
	}
	if err := e.acquireReplayWorker(intent, record); err != nil {
		return err
	}
	provisioner, err := e.worktrees(record)
	if err != nil {
		return fmt.Errorf("daemon: resolve replay worktree provisioner: %w", err)
	}
	fact, err := observeReplayWorktree(ctx, provisioner, intent, record)
	if err != nil {
		return err
	}
	if fact == dispatch.WorktreePrepared {
		return nil
	}
	if fact != dispatch.WorktreeAbsent {
		return fmt.Errorf("daemon: replay worktree requires repair before create: %s", fact)
	}
	if err := provisioner.PrepareBase(ctx, record); err != nil {
		return fmt.Errorf("daemon: prepare replay worktree base: %w", err)
	}
	createErr := provisioner.Create(ctx, record)
	fact, observeErr := observeReplayWorktree(ctx, provisioner, intent, record)
	if observeErr != nil {
		return errors.Join(createErr, observeErr)
	}
	if fact == dispatch.WorktreePrepared {
		return nil
	}
	if fact == dispatch.WorktreeAbsent {
		if createErr != nil {
			return fmt.Errorf("%w: replay worktree create left no authority: %w", errDispatchReplayPending, createErr)
		}
		return fmt.Errorf("%w: replay worktree create left no authority", errDispatchReplayPending)
	}
	if createErr != nil {
		return fmt.Errorf("daemon: replay worktree create requires repair with fact %q: %w", fact, createErr)
	}
	return fmt.Errorf("daemon: replay worktree create requires repair with fact %q", fact)
}

func (e dispatchReplayExecutor) acquireReplayWorker(intent dispatch.Intent, record runpkg.DispatchRecord) error {
	if record.Location.Kind != runpkg.ExecutionRemote {
		return nil
	}
	if e.workers == nil {
		return errors.New("daemon: dispatch replay worker registry is not configured")
	}
	worker, err := e.workers.AcquireBoundWorker(intent.Binding.RunID, workers.BoundWorker{
		Name: record.Location.WorkerName, Transport: record.Location.Transport,
		Host: record.Location.Host, RepositoryPath: record.Location.RepositoryPath,
	})
	if err != nil {
		return fmt.Errorf("daemon: acquire bound replay worker: %w", err)
	}
	if worker == nil {
		return fmt.Errorf("%w: exact worker is disabled or full", errDispatchReplayPending)
	}
	return nil
}

func observeReplayWorktree(
	ctx context.Context,
	provisioner dispatchWorktreeProvisioner,
	intent dispatch.Intent,
	record runpkg.DispatchRecord,
) (dispatch.WorktreeFact, error) {
	values, err := provisioner.Observe(ctx, record)
	if err != nil {
		return dispatch.WorktreeConflict, fmt.Errorf("daemon: observe replay worktree: %w", err)
	}
	return dispatch.ClassifyWorktreeObservations(intent, mapDiscoveredWorktrees(values)), nil
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
