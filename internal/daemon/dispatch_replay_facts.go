package daemon

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/queue"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

// dispatchReplayObservations contains values read from each replay authority.
// Claim, Git, and outcome facts stay explicit until their exact stores have
// production readers. This function does not infer them from Beads or events.
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

// buildDispatchReplayFacts joins exact observations through the package-owned
// classifiers. It performs no I/O and returns detached facts.
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
