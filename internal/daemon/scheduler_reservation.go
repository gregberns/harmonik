package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

var (
	errReserveCrossQueueDuplicate = errors.New("daemon: reservation: bead is already in flight from another queue")

	errReserveItemNotPending = errors.New("daemon: reservation: queue item is no longer pending")

	errReleaseItemNotDispatched = errors.New("daemon: reservation: queue item is not dispatched")

	errReleaseRunMismatch = errors.New("daemon: reservation: queue item is dispatched to another run")

	errReservationQueueMismatch = errors.New("daemon: reservation: queue identity changed")
)

type queueReservation struct {
	QueueName         string
	QueueID           string
	GroupIndex        int
	ItemIndex         int
	BeadID            core.BeadID
	RunID             core.RunID
	ClaimTransitionID core.TransitionID
}

type reservationVerdict string

const (
	reservationReserved reservationVerdict = "reserved"

	reservationItemFailed reservationVerdict = "item_failed"

	reservationRetryLater reservationVerdict = "retry_later"

	reservationWriteFailed reservationVerdict = "write_failed"

	reservationReleased reservationVerdict = "released"

	reservationReleaseContended reservationVerdict = "release_contended"

	reservationCrossQueueCollision reservationVerdict = "cross_queue_collision"
)

type crossQueueDisposition string

const (
	crossQueueNoCollision crossQueueDisposition = ""

	crossQueueSiblingRunning crossQueueDisposition = "sibling_running"

	crossQueueSiblingFinished crossQueueDisposition = "sibling_finished"
)

type crossQueueCollision struct {
	ConflictingQueue string
	Disposition      crossQueueDisposition
}

func releaseOutcomeAdvice(verdict reservationVerdict) string {
	switch verdict {
	case reservationRetryLater:
		return "nothing was written because the item is not this run's to release — " +
			"it already moved or another run holds it, so this path stranded nothing"

	case reservationReleaseContended:
		return "the item is still dispatched and will not be re-selected until the boot reconciliation pass"

	default:
		return "the item may still be dispatched and may not be re-selected until the boot reconciliation pass"
	}
}

const releaseRetryBudget = 3

type reservationResult struct {
	Verdict reservationVerdict

	// FailureReason is the LastFailureReason written to the item, set only
	// when Verdict is reservationItemFailed.
	FailureReason string

	// Collision names the other queue that already holds the bead and what its
	// hold means, set only when Verdict is reservationCrossQueueCollision.
	Collision crossQueueCollision

	// Outcome and Err are the durable result of the last write attempted.
	Outcome queue.NamespaceOutcome
	Err     error
}

func reserveQueueItem(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, res queueReservation) reservationResult {
	snapshot := queueStore.Snapshot(res.QueueName)
	if snapshot.Queue == nil {
		return reservationResult{Verdict: reservationRetryLater, Outcome: queue.OutcomeRejected}
	}

	var (
		collision      crossQueueCollision
		maxAttemptsHit bool
	)
	result := queueStore.Transact(ctx, queuewiring.TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationReservation,
		Precondition:  crossQueueDuplicateGuard(res.BeadID, &collision),
		Mutate: func(q *queue.Queue) error {
			if q.QueueID != res.QueueID {
				return errReservationQueueMismatch
			}
			item := activeQueueItem(q, res.GroupIndex, res.ItemIndex, res.BeadID)
			if item == nil || item.Status != queue.ItemStatusPending {
				return errReserveItemNotPending
			}
			item.Attempts++
			if item.Attempts >= maxItemAttempts {
				item.Status = queue.ItemStatusFailed
				item.LastFailureReason = "max_attempts_exceeded"
				item.PreclaimTerminal = preclaimTerminalBinding(res, queue.PreclaimTerminalMaxAttempts)
				maxAttemptsHit = true
				return nil
			}
			stampedRunID := res.RunID.String()
			item.Status = queue.ItemStatusDispatched
			item.RunID = &stampedRunID
			return nil
		},
	})

	switch {
	case result.Committed() && maxAttemptsHit:
		return reservationResult{
			Verdict:       reservationItemFailed,
			FailureReason: "max_attempts_exceeded",
			Outcome:       result.Outcome,
		}

	case result.Committed():
		return reservationResult{Verdict: reservationReserved, Outcome: result.Outcome}

	case errors.Is(result.Err, errReserveCrossQueueDuplicate):
		return reservationResult{
			Verdict:   reservationCrossQueueCollision,
			Collision: collision,
			Outcome:   result.Outcome,
		}

	case errors.Is(result.Err, queuewiring.ErrQueueQuarantined):
		return reservationResult{Verdict: reservationWriteFailed, Outcome: result.Outcome, Err: result.Err}

	case result.Outcome == queue.OutcomeRejected:
		return reservationResult{Verdict: reservationRetryLater, Outcome: result.Outcome, Err: result.Err}

	default:
		return reservationResult{Verdict: reservationWriteFailed, Outcome: result.Outcome, Err: result.Err}
	}
}

func releaseReservation(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, res queueReservation, reason string) reservationResult {
	return releaseFrom(ctx, queueStore, projectDir, queueStore.Snapshot(res.QueueName), res, reason)
}

func releaseFrom(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, snapshot queuewiring.Snapshot, res queueReservation, reason string) reservationResult {
	var last reservationResult
	for attempt := 0; attempt < releaseRetryBudget; attempt++ {
		if snapshot.Queue == nil {
			return reservationResult{Verdict: reservationRetryLater, Outcome: queue.OutcomeRejected}
		}
		result := releaseAttempt(ctx, queueStore, projectDir, snapshot, res, reason)
		if result.Verdict != reservationReleaseContended {
			return result
		}
		last = result
		snapshot = queueStore.Snapshot(res.QueueName)
	}
	return last
}

func releaseAttempt(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, snapshot queuewiring.Snapshot, res queueReservation, reason string) reservationResult {
	result := queueStore.Transact(ctx, queuewiring.TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationAdvance,
		Mutate: func(q *queue.Queue) error {
			item := activeQueueItem(q, res.GroupIndex, res.ItemIndex, res.BeadID)
			if item == nil || item.Status != queue.ItemStatusDispatched {
				return errReleaseItemNotDispatched
			}
			if item.RunID == nil || *item.RunID != res.RunID.String() {
				return errReleaseRunMismatch
			}
			item.Status = queue.ItemStatusPending
			item.RunID = nil
			item.LastFailureReason = reason
			return nil
		},
	})

	switch {
	case result.Committed():
		return reservationResult{Verdict: reservationReleased, FailureReason: reason, Outcome: result.Outcome}

	case errors.Is(result.Err, queuewiring.ErrQueueQuarantined):
		return reservationResult{Verdict: reservationWriteFailed, Outcome: result.Outcome, Err: result.Err}

	case errors.Is(result.Err, errReleaseItemNotDispatched),
		errors.Is(result.Err, errReleaseRunMismatch):
		return reservationResult{Verdict: reservationRetryLater, Outcome: result.Outcome, Err: result.Err}

	case result.Outcome == queue.OutcomeRejected:
		return reservationResult{Verdict: reservationReleaseContended, Outcome: result.Outcome, Err: result.Err}

	default:
		return reservationResult{Verdict: reservationWriteFailed, Outcome: result.Outcome, Err: result.Err}
	}
}

func activeQueueItem(q *queue.Queue, groupIndex, itemIdx int, beadID core.BeadID) *queue.Item {
	if q == nil {
		return nil
	}
	for gi := range q.Groups {
		if q.Groups[gi].Status != queue.GroupStatusActive {
			continue
		}
		if q.Groups[gi].GroupIndex != groupIndex {
			continue
		}
		if itemIdx < 0 || itemIdx >= len(q.Groups[gi].Items) {
			continue
		}
		if q.Groups[gi].Items[itemIdx].BeadID != beadID {
			continue
		}
		return &q.Groups[gi].Items[itemIdx]
	}
	return nil
}

func crossQueueDuplicateGuard(beadID core.BeadID, collision *crossQueueCollision) func(map[string]*queue.Queue) error {
	return func(others map[string]*queue.Queue) error {
		found := crossQueueCollision{}
		for otherName, otherQueue := range others {
			switch siblingBeadDisposition(otherQueue, beadID) {
			case crossQueueSiblingRunning:
				*collision = crossQueueCollision{ConflictingQueue: otherName, Disposition: crossQueueSiblingRunning}
				return errReserveCrossQueueDuplicate
			case crossQueueSiblingFinished:
				if found.Disposition == crossQueueNoCollision {
					found = crossQueueCollision{ConflictingQueue: otherName, Disposition: crossQueueSiblingFinished}
				}
			case crossQueueNoCollision:
			}
		}
		if found.Disposition == crossQueueNoCollision {
			return nil
		}
		*collision = found
		return errReserveCrossQueueDuplicate
	}
}

func siblingBeadDisposition(q *queue.Queue, beadID core.BeadID) crossQueueDisposition {
	if q == nil || q.Status != queue.QueueStatusActive {
		return crossQueueNoCollision
	}
	found := crossQueueNoCollision
	for _, group := range q.Groups {
		for _, item := range group.Items {
			if item.BeadID != beadID {
				continue
			}
			switch item.Status {
			case queue.ItemStatusDispatched:
				return crossQueueSiblingRunning
			case queue.ItemStatusCompleted:
				found = crossQueueSiblingFinished
			default:
			}
		}
	}
	return found
}

const maxCrossQueueCollisions = 12

type crossQueueCollisionOutcome string

const (
	crossQueueRefuseTick crossQueueCollisionOutcome = "refuse_tick"

	crossQueueAdvanceCompleted crossQueueCollisionOutcome = "advance_completed"

	crossQueueFailTerminal crossQueueCollisionOutcome = "fail_terminal"
)

type crossQueueCollisionState struct {
	// consecutive counts refusals since the last time this item made progress.
	consecutive int

	// reported is the disposition already announced for this item. It is what
	// makes the collision report fire once per collision rather than once per
	// tick: a refusal repeats every poll interval for as long as the sibling
	// runs, and an event per repeat would bury the one an operator needs.
	reported crossQueueDisposition
}

func recordCrossQueueCollision(
	states map[queuePreClaimAttemptKey]crossQueueCollisionState,
	key queuePreClaimAttemptKey,
	disposition crossQueueDisposition,
) (crossQueueCollisionOutcome, bool) {
	state := states[key]
	report := state.reported != disposition
	state.reported = disposition

	if disposition == crossQueueSiblingFinished {
		states[key] = state
		return crossQueueAdvanceCompleted, report
	}

	state.consecutive++
	states[key] = state
	if state.consecutive >= maxCrossQueueCollisions {
		return crossQueueFailTerminal, true
	}
	return crossQueueRefuseTick, report
}

type crossQueueCollisionSite struct {
	QueueName         string
	QueueID           string
	GroupIndex        int
	ItemIndex         int
	BeadID            core.BeadID
	RunID             core.RunID
	ClaimTransitionID core.TransitionID
	Now               time.Time
}

type crossQueueCollisionPorts struct {
	emitter      handlercontract.EventEmitter
	queueStore   *queuewiring.QueueStore
	projectDir   string
	reap         reapSeamPort
	collisions   map[queuePreClaimAttemptKey]crossQueueCollisionState
	tickRefusals map[core.BeadID]bool
	refusedUntil map[core.BeadID]time.Time
}

func resolveCrossQueueCollision(ctx context.Context, ports crossQueueCollisionPorts, site crossQueueCollisionSite, collision crossQueueCollision) bool {
	key := queuePreClaimAttemptKey{
		queueID:    site.QueueID,
		groupIndex: site.GroupIndex,
		itemIdx:    site.ItemIndex,
		beadID:     site.BeadID,
	}
	outcome, report := recordCrossQueueCollision(ports.collisions, key, collision.Disposition)
	if report {
		reportCrossQueueCollision(ctx, ports.emitter, site, collision, outcome)
	}

	switch outcome {
	case crossQueueRefuseTick:
		ports.tickRefusals[site.BeadID] = true
		ports.refusedUntil[site.BeadID] = site.Now.Add(claimSkipInProgressCooldown)
		return true

	case crossQueueAdvanceCompleted:
		delete(ports.collisions, key)
		evaluateGroupAdvanceWithOutcome(ctx, ports.reap, site.QueueName, site.QueueID, site.GroupIndex, site.ItemIndex, true, site.Now)
		return false

	default: // crossQueueFailTerminal
		delete(ports.collisions, key)
		failed := failQueueItem(ctx, ports.queueStore, ports.projectDir, queueReservation{
			QueueName:         site.QueueName,
			QueueID:           site.QueueID,
			GroupIndex:        site.GroupIndex,
			ItemIndex:         site.ItemIndex,
			BeadID:            site.BeadID,
			RunID:             site.RunID,
			ClaimTransitionID: site.ClaimTransitionID,
		}, "cross_queue_duplicate", queue.PreclaimTerminalCrossQueue)
		if err := finishPreclaimTerminal(failed, func() {
			evaluateGroupAdvanceWithOutcome(ctx, ports.reap, site.QueueName, site.QueueID, site.GroupIndex, site.ItemIndex, false, site.Now)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: workloop: persist cross-queue terminal binding: %v\n", err)
		}
		return false
	}
}

func reportCrossQueueCollision(
	ctx context.Context,
	emitter handlercontract.EventEmitter,
	site crossQueueCollisionSite,
	collision crossQueueCollision,
	outcome crossQueueCollisionOutcome,
) {
	var disposition core.CrossQueueCollisionDisposition
	var advice string
	switch outcome {
	case crossQueueAdvanceCompleted:
		disposition = core.CrossQueueCollisionCompleted
		advice = "that queue already finished it, so this item is advanced to completed rather than run again"
	case crossQueueFailTerminal:
		disposition = core.CrossQueueCollisionFailed
		advice = fmt.Sprintf("the collision outlived %d refusals, so this item is now failed and its queue parks — "+
			"remove the bead from one of the two queues", maxCrossQueueCollisions)
	default: // crossQueueRefuseTick
		disposition = core.CrossQueueCollisionRefused
		advice = "this item stays pending and is refused until that run ends; the queue keeps dispatching the items behind it"
	}

	fmt.Fprintf(os.Stderr,
		"daemon: workloop: bead %s is held by queue %q as well as queue %q — %s (hk-nsion, hk-a11re)\n",
		site.BeadID, collision.ConflictingQueue, site.QueueName, advice)

	if emitter == nil {
		return
	}
	emitTypedEvent(ctx, emitter, core.EventTypeCrossQueueCollision, core.CrossQueueCollisionPayload{
		BeadID:       string(site.BeadID),
		LosingQueue:  site.QueueName,
		WinningQueue: collision.ConflictingQueue,
		Disposition:  disposition,
		DetectedAt:   site.Now.UTC().Format(time.RFC3339),
	})
}

func failQueueItem(ctx context.Context, queueStore *queuewiring.QueueStore, projectDir string, res queueReservation, reason string, cause queue.PreclaimTerminalCause) reservationResult {
	snapshot := queueStore.Snapshot(res.QueueName)
	if snapshot.Queue == nil {
		return reservationResult{Verdict: reservationRetryLater, Outcome: queue.OutcomeRejected}
	}
	result := queueStore.Transact(ctx, queuewiring.TransactionRequest{
		Snapshot:      snapshot,
		ProjectDir:    projectDir,
		OperationKind: queue.OperationAdvance,
		Mutate: func(q *queue.Queue) error {
			if q.QueueID != res.QueueID {
				return errReservationQueueMismatch
			}
			item := activeQueueItem(q, res.GroupIndex, res.ItemIndex, res.BeadID)
			if item == nil {
				return errReserveItemNotPending
			}
			if cause == queue.PreclaimTerminalDependencyRefusal &&
				(item.Status != queue.ItemStatusDispatched || item.RunID == nil || *item.RunID != res.RunID.String()) {
				return errReleaseRunMismatch
			}
			item.Status = queue.ItemStatusFailed
			item.LastFailureReason = reason
			item.PreclaimTerminal = preclaimTerminalBinding(res, cause)
			return nil
		},
	})
	switch {
	case result.Committed():
		return reservationResult{Verdict: reservationItemFailed, FailureReason: reason, Outcome: result.Outcome}
	case errors.Is(result.Err, queuewiring.ErrQueueQuarantined):
		return reservationResult{Verdict: reservationWriteFailed, Outcome: result.Outcome, Err: result.Err}
	case result.Outcome == queue.OutcomeRejected:
		return reservationResult{Verdict: reservationRetryLater, Outcome: result.Outcome, Err: result.Err}
	default:
		return reservationResult{Verdict: reservationWriteFailed, Outcome: result.Outcome, Err: result.Err}
	}
}

func preclaimTerminalBinding(res queueReservation, cause queue.PreclaimTerminalCause) *queue.PreclaimTerminalBinding {
	return &queue.PreclaimTerminalBinding{
		RunID:             res.RunID.String(),
		ClaimTransitionID: res.ClaimTransitionID.String(),
		Cause:             cause,
	}
}

func finishDependencyRefusal(result reservationResult, completeGroup func()) error {
	return finishPreclaimTerminal(result, completeGroup)
}

func finishPreclaimTerminal(result reservationResult, completeGroup func()) error {
	if result.Verdict != reservationItemFailed {
		return fmt.Errorf("verdict=%s outcome=%s: %w", result.Verdict, result.Outcome, result.Err)
	}
	completeGroup()
	return nil
}

func reportQueueWriteError(ctx context.Context, gates dispatchGatesPort, queueName string, res reservationResult) {
	detail := fmt.Sprintf("queue %q write outcome=%s", queueName, res.Outcome)
	if res.Err != nil {
		detail += ": " + res.Err.Error()
	}

	if _, reported := gates.queueWriteErrorReported[queueName]; reported {
		return
	}
	gates.queueWriteErrorReported[queueName] = struct{}{}

	fmt.Fprintf(os.Stderr,
		"daemon: workloop: QUEUE WRITE FAILED — %s. Dispatch abandoned; no bead was claimed and no run was started. "+
			"Queue %q now refuses further writes and the daemon is degraded. This does not clear by retrying: "+
			"check free disk space and the .harmonik/queues directory, then restart the daemon.\n",
		detail, queueName)

	if gates.bus == nil {
		return
	}
	emitTypedEvent(ctx, gates.bus, core.EventTypeInfrastructureUnavailable, core.InfrastructureUnavailablePayload{
		FailedPrerequisite: core.InfrastructurePrerequisiteQueueWriteError,
		DetailString:       detail,
		RetryCount:         0,
	})
	emitTypedEvent(ctx, gates.bus, core.EventTypeDaemonDegraded, core.DaemonDegradedPayload{
		DetectedAt: time.Now().UTC().Format(time.RFC3339),
		Reason:     core.DaemonDegradedReasonInfrastructureUnavailable,
	})
}

type validPayload interface{ Valid() bool }

func emitTypedEvent(ctx context.Context, bus handlercontract.EventEmitter, eventType core.EventType, payload validPayload) {
	if !payload.Valid() {
		fmt.Fprintf(os.Stderr, "daemon: workloop: %s: payload is not valid; not emitting\n", eventType)
		return
	}
	payloadJSON, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: %s: marshal: %v\n", eventType, marshalErr)
		return
	}
	if emitErr := bus.Emit(ctx, eventType, payloadJSON); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: workloop: %s: emit: %v\n", eventType, emitErr)
	}
}
