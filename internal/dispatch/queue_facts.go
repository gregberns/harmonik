package dispatch

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/queue"
)

// QueueObservation joins the exact queue item with its pre-claim terminal fact.
type QueueObservation struct {
	Queue    QueueFact
	Preclaim PreclaimFact
}

// ClassifyQueueObservation reads no external state. Invalid intent input is an
// error. Missing, corrupt, or mismatched queue state fails closed as conflict.
func ClassifyQueueObservation(intent Intent, snapshot *queue.Queue) (QueueObservation, error) {
	if err := intent.Validate(); err != nil {
		return QueueObservation{}, fmt.Errorf("dispatch: classify queue: %w", err)
	}
	conflict := QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}
	group, item, ok := exactQueueItem(intent.Binding, snapshot)
	if !ok {
		return conflict, nil
	}
	if item.PreclaimTerminal != nil {
		return classifyPreclaimTerminal(intent, *group, *item), nil
	}
	return classifyOrdinaryQueueItem(intent, *group, *item), nil
}

func classifyOrdinaryQueueItem(intent Intent, group queue.Group, item queue.Item) QueueObservation {
	conflict := QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}
	switch item.Status {
	case queue.ItemStatusPending:
		return classifyPendingItem(intent, group, item)
	case queue.ItemStatusDispatched:
		return classifyRunOwnedItem(intent, group, item, QueueReserved)
	case queue.ItemStatusCompleted:
		return classifyRunOwnedItem(intent, group, item, QueueTerminalSuccess)
	case queue.ItemStatusFailed:
		return classifyRunOwnedItem(intent, group, item, QueueTerminalUnreopened)
	case queue.ItemStatusDeferredForLedgerDep:
		return conflict
	default:
		return conflict
	}
}

func classifyPendingItem(intent Intent, group queue.Group, item queue.Item) QueueObservation {
	conflict := QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}
	if group.Status != queue.GroupStatusActive || item.RunID != nil {
		return conflict
	}
	if intent.Phase != PhaseClaimRefused {
		return QueueObservation{Queue: QueueOfferable, Preclaim: PreclaimAbsent}
	}
	if intent.Refusal.Cause == ClaimRefusalSupportedNonOpen {
		return QueueObservation{Queue: QueueOfferable, Preclaim: PreclaimReleased}
	}
	return conflict
}

func classifyRunOwnedItem(intent Intent, group queue.Group, item queue.Item, fact QueueFact) QueueObservation {
	groupAdmits := group.Status == queue.GroupStatusActive
	if fact != QueueReserved {
		groupAdmits = terminalGroupAdmitsItem(group.Status)
	}
	if !groupAdmits || !itemRunMatches(item, intent) {
		return QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}
	}
	return QueueObservation{Queue: fact, Preclaim: PreclaimAbsent}
}

func exactQueueItem(binding Binding, snapshot *queue.Queue) (*queue.Group, *queue.Item, bool) {
	if snapshot == nil || snapshot.SchemaVersion != 1 || snapshot.QueueID != binding.QueueID ||
		queue.NormaliseQueueName(snapshot.Name) != binding.QueueName {
		return nil, nil, false
	}
	if ok, _ := queue.ValidateQueueName(queue.NormaliseQueueName(snapshot.Name)); !ok {
		return nil, nil, false
	}
	if !validQueueSnapshot(*snapshot) {
		return nil, nil, false
	}
	var target *queue.Group
	for groupIndex := range snapshot.Groups {
		group := &snapshot.Groups[groupIndex]
		if group.GroupIndex == binding.GroupIndex {
			if target != nil {
				return nil, nil, false
			}
			target = group
		}
	}
	if target == nil || binding.ItemIndex >= len(target.Items) {
		return nil, nil, false
	}
	item := &target.Items[binding.ItemIndex]
	if item.BeadID != binding.BeadID {
		return nil, nil, false
	}
	return target, item, true
}

func classifyPreclaimTerminal(intent Intent, group queue.Group, item queue.Item) QueueObservation {
	conflict := QueueObservation{Queue: QueueConflict, Preclaim: PreclaimConflict}
	binding := item.PreclaimTerminal
	if item.Status != queue.ItemStatusFailed || binding.RunID != intent.Binding.RunID.String() ||
		binding.ClaimTransitionID != intent.Binding.ClaimTransitionID.String() {
		return conflict
	}
	itemFact, groupFact, ok := preclaimFactsForCause(intent, item)
	if !ok {
		return conflict
	}
	switch group.Status {
	case queue.GroupStatusActive:
		return QueueObservation{Queue: QueueTerminalUnreopened, Preclaim: itemFact}
	case queue.GroupStatusCompleteWithFailures:
		return QueueObservation{Queue: QueueTerminalUnreopened, Preclaim: groupFact}
	default:
		return conflict
	}
}

func preclaimFactsForCause(intent Intent, item queue.Item) (itemFact, groupFact PreclaimFact, ok bool) {
	binding := item.PreclaimTerminal
	switch binding.Cause {
	case queue.PreclaimTerminalMaxAttempts:
		if intent.Phase != PhasePrepared || item.RunID != nil {
			return "", "", false
		}
		return PreclaimMaxAttemptsItemTerminal, PreclaimMaxAttemptsGroupDurable, true
	case queue.PreclaimTerminalCrossQueue:
		if intent.Phase != PhasePrepared || item.RunID != nil {
			return "", "", false
		}
		return PreclaimCrossQueueItemTerminal, PreclaimCrossQueueGroupDurable, true
	case queue.PreclaimTerminalDependencyRefusal:
		if intent.Phase != PhaseClaimRefused || intent.Refusal.Cause != ClaimRefusalDependency || !itemRunMatches(item, intent) {
			return "", "", false
		}
		return PreclaimDependencyItemTerminal, PreclaimDependencyGroupDurable, true
	default:
		return "", "", false
	}
}

func itemRunMatches(item queue.Item, intent Intent) bool {
	return item.RunID != nil && *item.RunID == intent.Binding.RunID.String()
}

func terminalGroupAdmitsItem(status queue.GroupStatus) bool {
	return status == queue.GroupStatusActive || status == queue.GroupStatusCompleteSuccess ||
		status == queue.GroupStatusCompleteWithFailures
}

func validGroupShape(group queue.Group) bool {
	if group.GroupIndex < 0 || len(group.Items) == 0 ||
		(group.Kind != queue.GroupKindWave && group.Kind != queue.GroupKindStream) {
		return false
	}
	completed, failed, nonterminal, valid := groupItemCounts(group.Items)
	if !valid {
		return false
	}
	switch group.Status {
	case queue.GroupStatusPending:
		return allItemsPending(group.Items)
	case queue.GroupStatusActive:
		return true
	case queue.GroupStatusCompleteSuccess:
		return completed == len(group.Items)
	case queue.GroupStatusCompleteWithFailures:
		return nonterminal == 0 && failed > 0
	default:
		return false
	}
}

func allItemsPending(items []queue.Item) bool {
	for _, item := range items {
		if item.Status != queue.ItemStatusPending {
			return false
		}
	}
	return true
}

func groupItemCounts(items []queue.Item) (completed, failed, nonterminal int, valid bool) {
	for _, item := range items {
		if !validItemShape(item) {
			return 0, 0, 0, false
		}
		switch item.Status {
		case queue.ItemStatusCompleted:
			completed++
		case queue.ItemStatusFailed:
			failed++
		default:
			nonterminal++
		}
	}
	return completed, failed, nonterminal, true
}

func validItemShape(item queue.Item) bool {
	switch item.Status {
	case queue.ItemStatusPending, queue.ItemStatusDeferredForLedgerDep:
		return item.RunID == nil && item.PreclaimTerminal == nil
	case queue.ItemStatusDispatched:
		return item.RunID != nil && validOptionalItemRun(item.RunID) && item.PreclaimTerminal == nil
	case queue.ItemStatusCompleted:
		return item.PreclaimTerminal == nil && validOptionalItemRun(item.RunID)
	case queue.ItemStatusFailed:
		return validOptionalItemRun(item.RunID) && validFailedItemShape(item)
	default:
		return false
	}
}

func validOptionalItemRun(runID *string) bool {
	return runID == nil || validateUUIDv7(*runID) == nil
}

func validFailedItemShape(item queue.Item) bool {
	if item.PreclaimTerminal == nil {
		return true
	}
	if err := item.PreclaimTerminal.Validate(); err != nil {
		return false
	}
	switch item.PreclaimTerminal.Cause {
	case queue.PreclaimTerminalDependencyRefusal:
		return item.RunID != nil && *item.RunID == item.PreclaimTerminal.RunID
	case queue.PreclaimTerminalMaxAttempts, queue.PreclaimTerminalCrossQueue:
		return item.RunID == nil
	default:
		return false
	}
}

func validQueueSnapshot(snapshot queue.Queue) bool {
	if len(snapshot.Groups) == 0 || snapshot.Status == queue.QueueStatusCancelled {
		return false
	}
	beads := make(map[string]struct{})
	runs := make(map[string]struct{})
	transitions := make(map[string]struct{})
	for index := range snapshot.Groups {
		group := snapshot.Groups[index]
		if group.GroupIndex != index || !validGroupShape(group) || !addUniqueQueueIdentities(group, beads, runs, transitions) {
			return false
		}
	}
	if snapshot.Status == queue.QueueStatusCompleted {
		return allGroupsHaveStatus(snapshot.Groups, queue.GroupStatusCompleteSuccess)
	}
	return validQueueFrontier(snapshot)
}

func addUniqueQueueIdentities(group queue.Group, beads, runs, transitions map[string]struct{}) bool {
	for _, item := range group.Items {
		beadID := string(item.BeadID)
		if beadID == "" || contains(beads, beadID) {
			return false
		}
		beads[beadID] = struct{}{}
		runID := item.RunID
		if item.PreclaimTerminal != nil {
			preclaimRunID := item.PreclaimTerminal.RunID
			runID = &preclaimRunID
			transitionID := item.PreclaimTerminal.ClaimTransitionID
			if contains(transitions, transitionID) {
				return false
			}
			transitions[transitionID] = struct{}{}
		}
		if runID == nil {
			continue
		}
		if contains(runs, *runID) {
			return false
		}
		runs[*runID] = struct{}{}
	}
	return true
}

func contains(values map[string]struct{}, value string) bool {
	_, exists := values[value]
	return exists
}

func validQueueFrontier(snapshot queue.Queue) bool {
	frontier := -1
	for index, group := range snapshot.Groups {
		if group.Status == queue.GroupStatusActive || group.Status == queue.GroupStatusCompleteWithFailures {
			if frontier >= 0 {
				return false
			}
			frontier = index
		}
	}
	if frontier < 0 {
		return false
	}
	for index, group := range snapshot.Groups {
		want := queue.GroupStatusPending
		if index < frontier {
			want = queue.GroupStatusCompleteSuccess
		} else if index == frontier {
			want = group.Status
		}
		if group.Status != want {
			return false
		}
	}
	if snapshot.Status == queue.QueueStatusPausedByFailure {
		return snapshot.Groups[frontier].Status == queue.GroupStatusCompleteWithFailures
	}
	return (snapshot.Status == queue.QueueStatusActive || snapshot.Status == queue.QueueStatusPausedByDrain ||
		snapshot.Status == queue.QueueStatusPausedByBudget) && snapshot.Groups[frontier].Status == queue.GroupStatusActive
}

func allGroupsHaveStatus(groups []queue.Group, status queue.GroupStatus) bool {
	for _, group := range groups {
		if group.Status != status {
			return false
		}
	}
	return true
}
