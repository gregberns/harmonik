package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workspace"
)

type dispatchReplayBeadReader interface {
	ShowBead(context.Context, core.BeadID) (core.BeadRecord, error)
}

// filesystemDispatchReplayReader reads only durable production authorities.
// Claim, Git, and outcome readers are separate prerequisites. Until they are
// present, non-prepared replay cannot be classified as safe.
type filesystemDispatchReplayReader struct {
	projectDir string
	beads      dispatchReplayBeadReader
	resolve    sessionStartAdapterResolver
	worktrees  dispatchWorktreeObserverResolver
}

func (r filesystemDispatchReplayReader) ReadDispatchReplayFacts(
	ctx context.Context,
	intent dispatch.Intent,
) (dispatch.ReplayFacts, error) {
	observations, err := r.readObservations(ctx, intent)
	if err != nil {
		return dispatch.ReplayFacts{}, err
	}
	return buildDispatchReplayFacts(intent, observations)
}

func (r filesystemDispatchReplayReader) readObservations(
	ctx context.Context,
	intent dispatch.Intent,
) (dispatchReplayObservations, error) {
	if err := intent.Validate(); err != nil {
		return dispatchReplayObservations{}, fmt.Errorf("daemon: validate dispatch replay read: %w", err)
	}
	if r.projectDir == "" || r.beads == nil {
		return dispatchReplayObservations{}, errors.New("daemon: dispatch replay reader is not configured")
	}
	q, err := queue.Load(ctx, r.projectDir, intent.Binding.QueueName)
	if err != nil {
		return dispatchReplayObservations{}, fmt.Errorf("read queue: %w", err)
	}
	bead, err := r.beads.ShowBead(ctx, intent.Binding.BeadID)
	if err != nil {
		return dispatchReplayObservations{}, fmt.Errorf("read bead: %w", err)
	}
	records, err := runpkg.ScanRegistry(r.projectDir)
	if err != nil {
		return dispatchReplayObservations{}, fmt.Errorf("scan run registry: %w", err)
	}
	record, err := exactDispatchRecord(intent.Binding.RunID, records.Dispatch)
	if err != nil {
		return dispatchReplayObservations{}, err
	}
	recordFact := runpkg.ClassifyDispatchRecord(intent, record)
	var discovered []workspace.DiscoveredWorktree
	if recordFact == dispatch.RunRecordLocated || recordFact == dispatch.RunRecordSession {
		if r.worktrees == nil {
			return dispatchReplayObservations{}, errors.New("daemon: dispatch replay worktree observer is not configured")
		}
		observer, resolveErr := r.worktrees(*record)
		if resolveErr != nil {
			return dispatchReplayObservations{}, fmt.Errorf("resolve dispatch worktree owner: %w", resolveErr)
		}
		discovered, err = observer.Observe(ctx, *record)
		if err != nil {
			return dispatchReplayObservations{}, fmt.Errorf("observe dispatch worktree: %w", err)
		}
	}
	sessionRecord := record
	if recordFact != dispatch.RunRecordSession {
		sessionRecord = nil
	}
	receipt, err := exactSessionReceipt(r.projectDir, intent.Binding.RunID)
	if err != nil {
		return dispatchReplayObservations{}, err
	}
	return dispatchReplayObservations{
		Queue: q, Bead: &bead, RunRecord: record,
		Worktrees: mapDiscoveredWorktrees(discovered),
		Session:   readDispatchTargetObservation(ctx, r.resolve, intent, sessionRecord),
		Receipt:   receipt,
		Claim:     dispatch.ClaimNone, Git: dispatch.GitAbsent,
	}, nil
}

func exactDispatchRecord(runID core.RunID, records []runpkg.DispatchRecord) (*runpkg.DispatchRecord, error) {
	var match *runpkg.DispatchRecord
	for index := range records {
		if records[index].RunID != runID {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("daemon: duplicate universal run record %s", runID)
		}
		value := records[index]
		match = &value
	}
	return match, nil
}

func exactSessionReceipt(projectDir string, runID core.RunID) (*dispatch.SessionStartReceipt, error) {
	receipt, err := dispatchstore.New(projectDir).LoadSessionStartReceipt(runID)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session receipt: %w", err)
	}
	return &receipt, nil
}

func mapDiscoveredWorktrees(values []workspace.DiscoveredWorktree) []dispatch.WorktreeObservation {
	result := make([]dispatch.WorktreeObservation, 0, len(values))
	for _, value := range values {
		observation := dispatch.WorktreeObservation{
			RunID: value.RunID, Path: value.WorktreePath, Registered: value.RegisteredInGit,
			CanonicalPath: true, GitBranch: value.GitBranch, HeadCommit: value.HeadCommit,
			HasSessions:     value.HasSessionsDir,
			HasExactSidecar: value.HasExactSidecar,
			FactConflict:    value.GitRegistrationConflict || value.SessionsPathConflict,
			LeasePresent:    value.LeaseLock != nil || value.LeaseLockUnreadable,
			LeaseReadable:   value.LeaseLock != nil,
		}
		if value.LeaseLock != nil {
			observation.LeaseRunID = value.LeaseLock.RunID
			observation.LeasePID = value.LeaseLock.PID
			observation.LeaseCreatedAt = value.LeaseLock.CreatedAt
			observation.LeaseTTLSec = value.LeaseLock.TTLSec
		}
		result = append(result, observation)
	}
	return result
}

func readDispatchTargetObservation(
	ctx context.Context,
	resolve sessionStartAdapterResolver,
	intent dispatch.Intent,
	record *runpkg.DispatchRecord,
) dispatch.SessionTargetObservation {
	if intent.Phase != dispatch.PhaseHandoffDurable {
		return dispatch.SessionTargetObservation{Status: dispatch.SessionTargetSessionAbsent}
	}
	if record == nil || record.Location == nil || resolve == nil {
		return dispatch.SessionTargetObservation{Status: dispatch.SessionTargetOptionsUnreadable}
	}
	adapter, err := resolve(*record.Location)
	if err != nil {
		return dispatch.SessionTargetObservation{Status: dispatch.SessionTargetOptionsUnreadable}
	}
	prober, ok := adapter.(dispatchTargetProber)
	if !ok {
		return dispatch.SessionTargetObservation{Status: dispatch.SessionTargetOptionsUnreadable}
	}
	return mapDispatchTargetProbe(prober.ProbeDispatchTarget(ctx, intent.Handoff.SessionName, intent.Handoff.WindowName))
}
