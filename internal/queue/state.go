package queue

import (
	"context"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// AdvanceGroup evaluates g's transition eligibility under the current
// queueStatus and returns the resulting GroupStatus plus the ordered list of
// events to emit.
//
// Rules applied in priority order:
//
//  1. QM-032 — terminal states are absorbing: if g is already
//     complete-success or complete-with-failures, return unchanged with no
//     events.
//
//  2. QM-031 — pending → active gate: if g is pending, transition only when
//     queueStatus == active. The caller is responsible for supplying the
//     correct predecessor-complete-success trigger context; AdvanceGroup does
//     not re-inspect predecessor state.
//
//  3. QM-030 + all-terminal gate: if g is active, transition only when every
//     item is terminal (completed or failed). In-flight dispatched items block
//     the transition (QM-034 — failed items do not interrupt siblings).
//
// The returned events are ordered per the §5 and §8 emit sequences:
//   - pending → active:  queue_group_started
//   - active → complete-success:  queue_group_completed{complete-success}
//   - active → complete-with-failures:  queue_group_completed{complete-with-failures},
//     queue_paused{group_failure}
//
// The queue_id and timestamps on the returned events use queueID and the now
// argument; callers that need the persist-before-emit discipline (QM-063) MUST
// persist before calling into the event bus with the returned events.
//
// ctx is reserved for future cancellation integration; it is checked for
// Done but no long-running operations are performed.
//
// Returns ErrGroupNil when g is nil.
// Returns ErrQueueIDEmpty when queueID is empty.
func AdvanceGroup(
	ctx context.Context,
	g *Group,
	queueStatus QueueStatus,
	queueID string,
	now time.Time,
) (newStatus GroupStatus, events []EventIntent, err error) {
	if g == nil {
		return "", nil, ErrGroupNil
	}
	if queueID == "" {
		return "", nil, ErrQueueIDEmpty
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}

	if groupIsTerminal(g.Status) {
		return g.Status, nil, nil
	}

	switch g.Status {
	case GroupStatusPending:
		return advancePending(g, queueStatus, queueID, now)
	case GroupStatusActive:
		return advanceActive(g, queueID, now)
	default:
		return g.Status, nil, fmt.Errorf("queue: AdvanceGroup: unrecognised GroupStatus %q", g.Status)
	}
}

// EligibleItems returns the items within an active group that are ready for
// dispatch consideration. It does NOT filter by capacity; the caller applies
// QM-062 (min(pending, --max-concurrent - running)).
//
// Wave groups (QM-036): any pending item (not deferred-for-ledger-dep) is
// eligible. Order is not prescribed; the slice preserves item-list order.
//
// Stream groups (QM-035): dispatched, terminal, and deferred-for-ledger-dep
// items are skipped; the first pending item found is returned. Deferred items
// do not HOL-block dep-free tail items (hk-cb5ow). An in-flight (dispatched)
// head does NOT block subsequent pending items; this allows --max-concurrent > 1.
//
// Returns nil (empty slice) when:
//   - g is nil or not active.
//   - No eligible item exists under the group's dispatch semantics.
func EligibleItems(g *Group) []*Item {
	if g == nil || g.Status != GroupStatusActive {
		return nil
	}
	switch g.Kind {
	case GroupKindWave:
		return waveEligible(g)
	case GroupKindStream:
		return streamEligible(g)
	default:
		return nil
	}
}

func groupIsTerminal(s GroupStatus) bool {
	return s == GroupStatusCompleteSuccess || s == GroupStatusCompleteWithFailures
}

func itemIsTerminal(s ItemStatus) bool {
	return s == ItemStatusCompleted || s == ItemStatusFailed
}

func advancePending(
	g *Group,
	queueStatus QueueStatus,
	queueID string,
	now time.Time,
) (GroupStatus, []EventIntent, error) {
	if queueStatus != QueueStatusActive {
		return GroupStatusPending, nil, nil
	}

	nowStr := now.UTC().Format(time.RFC3339Nano)

	evt, err := NewEventIntent(core.EventTypeQueueGroupStarted, &core.QueueGroupStartedPayload{
		QueueID:    queueID,
		GroupIndex: g.GroupIndex,
		GroupKind:  string(g.Kind),
		ItemCount:  len(g.Items),
		StartedAt:  nowStr,
	})
	if err != nil {
		return GroupStatusPending, nil, fmt.Errorf("queue: AdvanceGroup: build queue_group_started: %w", err)
	}

	return GroupStatusActive, []EventIntent{evt}, nil
}

func advanceActive(
	g *Group,
	queueID string,
	now time.Time,
) (GroupStatus, []EventIntent, error) {
	if !allItemsTerminal(g) {
		return GroupStatusActive, nil, nil
	}

	successCount, failCount := countOutcomes(g)
	nowStr := now.UTC().Format(time.RFC3339Nano)

	if failCount == 0 {
		evt, err := NewEventIntent(core.EventTypeQueueGroupCompleted, &core.QueueGroupCompletedPayload{
			QueueID:      queueID,
			GroupIndex:   g.GroupIndex,
			FinalStatus:  string(GroupStatusCompleteSuccess),
			SuccessCount: successCount,
			FailCount:    failCount,
			CompletedAt:  nowStr,
		})
		if err != nil {
			return GroupStatusActive, nil, fmt.Errorf("queue: AdvanceGroup: build queue_group_completed: %w", err)
		}
		return GroupStatusCompleteSuccess, []EventIntent{evt}, nil
	}

	evtCompleted, err := NewEventIntent(core.EventTypeQueueGroupCompleted, &core.QueueGroupCompletedPayload{
		QueueID:      queueID,
		GroupIndex:   g.GroupIndex,
		FinalStatus:  string(GroupStatusCompleteWithFailures),
		SuccessCount: successCount,
		FailCount:    failCount,
		CompletedAt:  nowStr,
	})
	if err != nil {
		return GroupStatusActive, nil, fmt.Errorf("queue: AdvanceGroup: build queue_group_completed: %w", err)
	}

	evtPaused, err := NewEventIntent(core.EventTypeQueuePaused, &core.QueuePausedPayload{
		QueueID:    queueID,
		GroupIndex: g.GroupIndex,
		FailCount:  failCount,
		PausedAt:   nowStr,
		Reason:     "group_failure",
	})
	if err != nil {
		return GroupStatusActive, nil, fmt.Errorf("queue: AdvanceGroup: build queue_paused: %w", err)
	}

	return GroupStatusCompleteWithFailures, []EventIntent{evtCompleted, evtPaused}, nil
}

func allItemsTerminal(g *Group) bool {
	for i := range g.Items {
		if !itemIsTerminal(g.Items[i].Status) {
			return false
		}
	}
	return true
}

func countOutcomes(g *Group) (successCount, failCount int) {
	for i := range g.Items {
		switch g.Items[i].Status {
		case ItemStatusCompleted:
			successCount++
		case ItemStatusFailed:
			failCount++
		case ItemStatusPending, ItemStatusDispatched, ItemStatusDeferredForLedgerDep:
		}
	}
	return successCount, failCount
}

func waveEligible(g *Group) []*Item {
	var out []*Item
	for i := range g.Items {
		if g.Items[i].Status == ItemStatusPending && g.Items[i].Attempts < MaxItemAttempts {
			out = append(out, &g.Items[i])
		}
	}
	return out
}

func streamEligible(g *Group) []*Item {
	lastEligiblePending := make(map[core.BeadID]int)
	for i := range g.Items {
		if g.Items[i].Status == ItemStatusPending && g.Items[i].Attempts < MaxItemAttempts {
			lastEligiblePending[g.Items[i].BeadID] = i
		}
	}

	for i := range g.Items {
		switch g.Items[i].Status {
		case ItemStatusPending:
			if g.Items[i].Attempts >= MaxItemAttempts {
				continue
			}
			return []*Item{&g.Items[i]}
		case ItemStatusDeferredForLedgerDep:
			continue
		case ItemStatusDispatched, ItemStatusCompleted, ItemStatusFailed:
			if idx, ok := lastEligiblePending[g.Items[i].BeadID]; ok && idx > i {
				return []*Item{&g.Items[idx]}
			}
			continue
		}
	}
	return nil
}

// ReevaluateDeferred re-evaluates every deferred-for-ledger-dep item in g and
// transitions any whose blockers have all resolved back to pending, per the
// §2.8 normative rule:
//
//	"When the blocking bead closes, the dispatcher MUST re-evaluate and
//	 transition the item back to pending."
//
// It is the un-defer counterpart to the QM-025 submit/append-time deferral
// (Validate / buildDeferredSet): both consult the same BeadLedger.BlocksEdge
// seam so the un-defer condition is the exact inverse of the deferral
// condition. An item I deferred at submit time because a sibling B satisfied
// BlocksEdge(B, I) becomes eligible again only once ALL such blockers are
// resolved.
//
// A blocker B of item I is resolved when EITHER:
//   - B completed within this queue group; the daemon marks the successful item
//     completed before the dispatch loop re-evaluates, so a predecessor that
//     just landed satisfies this branch. A failed item is not resolved because
//     its bead reopens and its dependents must remain blocked; or
//   - B is no longer open in the Beads ledger — LookupStatus(B) reports a
//     status other than open/in_progress (closed, tombstoned, not-found). This
//     branch covers blockers closed externally via `br close` independent of
//     queue completion.
//
// ReevaluateDeferred is called on every dispatch-loop tick (execution-model.md
// §7.4) under the QM-060 single-writer write lock; g is mutated in place. It
// returns the bead IDs that were transitioned deferred → pending (for logging;
// per §2.8 no event is emitted on this transition). A nil ledger is a no-op
// (returns nil, nil) so legacy/test callers without a ledger seam are safe.
//
// Spec ref: specs/queue-model.md §2.8, §6.6 QM-025; specs/execution-model.md §7.4.
// Bead ref: hk-nbjht.
func ReevaluateDeferred(ctx context.Context, g *Group, ledger BeadLedger) ([]core.BeadID, error) {
	if g == nil || ledger == nil {
		return nil, nil
	}

	var undeferred []core.BeadID
	for i := range g.Items {
		if g.Items[i].Status != ItemStatusDeferredForLedgerDep {
			continue
		}
		blocked := g.Items[i].BeadID

		allResolved := true
		dependencyFailed := false
		for j := range g.Items {
			if j == i {
				continue
			}
			blocker := g.Items[j].BeadID
			blocks, err := ledger.BlocksEdge(ctx, blocker, blocked)
			if err != nil {
				return undeferred, fmt.Errorf("queue: ReevaluateDeferred: BlocksEdge %q→%q: %w", blocker, blocked, err)
			}
			if !blocks {
				continue
			}
			if g.Items[j].Status == ItemStatusFailed {
				if err := FailDeferredItem(&g.Items[i], string(blocker)); err != nil {
					return undeferred, err
				}
				dependencyFailed = true
				break
			}
			if g.Items[j].Status == ItemStatusCompleted {
				continue
			}
			status, err := ledger.LookupStatus(ctx, blocker)
			if err != nil {
				return undeferred, fmt.Errorf("queue: ReevaluateDeferred: LookupStatus %q: %w", blocker, err)
			}
			if status == BeadStatusOpen || status == BeadStatusInProgress {
				allResolved = false
				break
			}
		}

		if dependencyFailed {
			continue
		}
		if allResolved {
			if err := ResolveDeferredItem(&g.Items[i]); err != nil {
				return undeferred, err
			}
			undeferred = append(undeferred, blocked)
		}
	}

	return undeferred, nil
}

// FailDeferredDependents propagates one failed item through the dependency
// edges in its group. It marks only descendants of failedBead. Independent
// chains in the same group continue to run.
func FailDeferredDependents(ctx context.Context, g *Group, failedBead core.BeadID, ledger BeadLedger) ([]core.BeadID, error) { //nolint:gocognit // Transitive graph propagation needs the fixed-point loop and edge checks together.
	if g == nil || ledger == nil || failedBead == "" {
		return nil, nil
	}
	failed := map[core.BeadID]bool{failedBead: true}
	var propagated []core.BeadID
	for changed := true; changed; {
		changed = false
		for i := range g.Items {
			if g.Items[i].Status != ItemStatusDeferredForLedgerDep || failed[g.Items[i].BeadID] {
				continue
			}
			for blocker := range failed {
				blocks, err := ledger.BlocksEdge(ctx, blocker, g.Items[i].BeadID)
				if err != nil {
					return propagated, fmt.Errorf("queue: FailDeferredDependents: BlocksEdge %q→%q: %w", blocker, g.Items[i].BeadID, err)
				}
				if !blocks {
					continue
				}
				if err := FailDeferredItem(&g.Items[i], string(blocker)); err != nil {
					return propagated, err
				}
				failed[g.Items[i].BeadID] = true
				propagated = append(propagated, g.Items[i].BeadID)
				changed = true
				break
			}
		}
	}
	return propagated, nil
}

// Sentinel errors returned by AdvanceGroup.
var (
	// ErrGroupNil is returned when AdvanceGroup is called with a nil group.
	ErrGroupNil = fmt.Errorf("queue: AdvanceGroup: group must not be nil")

	// ErrQueueIDEmpty is returned when AdvanceGroup is called with an empty
	// queueID.
	ErrQueueIDEmpty = fmt.Errorf("queue: AdvanceGroup: queueID must not be empty")
)
