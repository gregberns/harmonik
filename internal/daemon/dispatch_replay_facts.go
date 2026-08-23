package daemon

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

type dispatchReplayObservations struct {
	Queue      *queue.Queue
	Bead       *core.BeadRecord
	RunRecord  *runpkg.DispatchRecord
	Worktrees  []dispatch.WorktreeObservation
	Session    dispatch.SessionTargetObservation
	Receipt    *dispatch.SessionStartReceipt
	Claim      dispatch.ClaimFact
	Git        dispatch.GitFact
	RunOutcome bool
}

func buildDispatchReplayFacts(
	intent dispatch.Intent,
	observations dispatchReplayObservations,
) (dispatch.ReplayFacts, error) {
	if err := intent.Validate(); err != nil {
		return dispatch.ReplayFacts{}, fmt.Errorf("daemon: build dispatch replay facts: %w", err)
	}
	queueObservation, err := dispatch.ClassifyQueueObservation(intent, observations.Queue)
	if err != nil {
		return dispatch.ReplayFacts{}, err
	}
	return dispatch.ReplayFacts{
		IntentPhase:       intent.Phase,
		Queue:             queueObservation.Queue,
		Bead:              dispatch.ClassifyBeadRecord(intent, observations.Bead),
		RunRecord:         runpkg.ClassifyDispatchRecord(intent, observations.RunRecord),
		Worktree:          dispatch.ClassifyWorktreeObservations(intent, observations.Worktrees),
		Session:           dispatch.ClassifySessionTarget(intent, observations.Session),
		SessionReceipt:    runpkg.ClassifySessionStartReceipt(intent, observations.RunRecord, observations.Receipt),
		Git:               observations.Git,
		Claim:             observations.Claim,
		RefusalCause:      intentRefusalCause(intent),
		Preclaim:          queueObservation.Preclaim,
		RunOutcomeDurable: observations.RunOutcome,
	}, nil
}
