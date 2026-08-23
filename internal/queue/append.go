package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// AppendItems appends beadIDs to the stream group at groupIndex in q.
//
// It runs the append-path validation subset (QM-040, QM-042-acceptance-gate,
// QM-024 via [Validate] IsAppend=true, QM-020..QM-024, QM-026, QM-025) and,
// on success, mutates the in-memory Queue by tail-appending new items per
// QM-041. Items that are QM-025-deferred at accept time are stored with
// status deferred-for-ledger-dep.
//
// Event emission (QM-042):
//   - One queue_appended event.
//   - One queue_item_deferred_for_ledger_dep event per deferred item, in
//     append order, after queue_appended.
//
// The caller is responsible for QM-001 persistence before passing the returned
// events to the event bus (QM-063: persist-before-emit discipline). AppendItems
// does not persist; it returns the mutated Queue for the caller to persist.
//
// Returns:
//   - A pointer to the mutated Queue (same pointer as q, mutated in place).
//   - The ordered event list (queue_appended first, then deferred-for-ledger-dep
//     events in append order).
//   - A [ValidationError] wrapped as an error when the append fails validation.
//     Use [IsValidationError] to distinguish validation failures from I/O errors.
//
// Returns [ErrAppendQueueNil] when q is nil.
// Returns [ErrAppendEmptyBeadIDs] when beadIDs is empty.
func AppendItems(
	ctx context.Context,
	q *Queue,
	groupIndex int,
	beadIDs []string,
	ledger BeadLedger,
	acceptedAt time.Time,
	otherQueues ...*Queue,
) (*Queue, []EventIntent, error) {
	if q == nil {
		return nil, nil, ErrAppendQueueNil
	}
	if len(beadIDs) == 0 {
		return nil, nil, ErrAppendEmptyBeadIDs
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if groupIndex < 0 || groupIndex >= len(q.Groups) {
		return nil, nil, &ValidationError{
			Reason: ReasonAppendTargetInvalid,
			Detail: map[string]any{
				"group_index":   groupIndex,
				"actual_kind":   nil,
				"actual_status": nil,
			},
		}
	}

	appendItems := make([]Item, len(beadIDs))
	for i, id := range beadIDs {
		appendItems[i] = NewPendingItem(Item{
			BeadID: core.BeadID(id),
		})
	}

	vreq := ValidationRequest{
		Groups: []Group{
			NewPendingGroup(Group{
				GroupIndex: groupIndex,
				Kind:       GroupKindStream,
				// This is a validation placeholder. QM-024 checks the live group.
				Items: appendItems,
			}),
		},
		ActiveQueue:      q,
		IsAppend:         true,
		AppendGroupIndex: groupIndex,
		OtherQueues:      otherQueues,
	}

	verrs, deferredPairs, err := Validate(ctx, vreq, ledger)
	if err != nil {
		return nil, nil, fmt.Errorf("queue: AppendItems: validation: %w", err)
	}
	if len(verrs) > 0 {
		return nil, nil, &verrs[0]
	}

	now := acceptedAt.UTC()
	nowStr := now.Format(time.RFC3339Nano)

	deferredSet := make(map[core.BeadID]core.BeadID)
	for _, p := range deferredPairs {
		if p.GroupIndex == groupIndex {
			deferredSet[p.BeadID] = p.BlockerBeadID
		}
	}

	existingGroup := q.Groups[groupIndex]
	for _, newItem := range appendItems {
		if _, already := deferredSet[newItem.BeadID]; already {
			continue
		}
		for _, existing := range existingGroup.Items {
			if itemIsTerminalStatus(existing.Status) {
				continue
			}
			if existing.BeadID == newItem.BeadID {
				continue
			}
			blocks, bErr := ledger.BlocksEdge(ctx, existing.BeadID, newItem.BeadID)
			if bErr != nil {
				return nil, nil, fmt.Errorf("queue: AppendItems: QM-025 cross-check %q→%q: %w",
					existing.BeadID, newItem.BeadID, bErr)
			}
			if blocks {
				deferredSet[newItem.BeadID] = existing.BeadID
				break
			}
		}
	}

	newItems := make([]Item, len(beadIDs))
	for i, id := range beadIDs {
		beadID := core.BeadID(id)
		appended := now
		newItems[i] = NewPendingItem(Item{
			BeadID:     beadID,
			RunID:      nil,
			AppendedAt: &appended,
		})
		if _, deferred := deferredSet[beadID]; deferred {
			if err := DeferItemForLedgerDependency(&newItems[i]); err != nil {
				return nil, nil, fmt.Errorf("queue: AppendItems: defer item %q: %w", beadID, err)
			}
		}
	}

	g := &q.Groups[groupIndex]
	g.Items = append(g.Items, newItems...)

	appendedBeadIDStrs := make([]string, len(beadIDs))
	copy(appendedBeadIDStrs, beadIDs)

	evtAppended, err := NewEventIntent(core.EventTypeQueueAppended, &core.QueueAppendedPayload{
		QueueID:         q.QueueID,
		GroupIndex:      groupIndex,
		AppendedBeadIDs: appendedBeadIDStrs,
		AppendedAt:      nowStr,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("queue: AppendItems: build queue_appended: %w", err)
	}

	events := []EventIntent{evtAppended}

	for _, id := range beadIDs {
		beadID := core.BeadID(id)
		blockerID, deferred := deferredSet[beadID]
		if !deferred {
			continue
		}
		evtDeferred, evtErr := NewEventIntent(core.EventTypeQueueItemDeferredForLedgerDep, &core.QueueItemDeferredForLedgerDepPayload{
			QueueID:       q.QueueID,
			GroupIndex:    groupIndex,
			BeadID:        string(beadID),
			BlockerBeadID: string(blockerID),
			DetectedAt:    nowStr,
		})
		if evtErr != nil {
			return nil, nil, fmt.Errorf("queue: AppendItems: build queue_item_deferred_for_ledger_dep: %w", evtErr)
		}
		events = append(events, evtDeferred)
	}

	return q, events, nil
}

// IsValidationError reports whether err is a [ValidationError] returned by
// [AppendItems]. Callers that need to distinguish validation rejections from
// I/O errors should use this predicate rather than a type assertion.
func IsValidationError(err error) bool {
	if err == nil {
		return false
	}
	var validationErr *ValidationError
	return errors.As(err, &validationErr)
}

// ValidationReason returns the [QueueValidationReason] from err when it is a
// [ValidationError], and the zero value otherwise.
func ValidationReason(err error) QueueValidationReason {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Reason
	}
	return ""
}

func itemIsTerminalStatus(s ItemStatus) bool {
	return s == ItemStatusCompleted || s == ItemStatusFailed
}

// Sentinel errors returned by AppendItems.
var (
	// ErrAppendQueueNil is returned when AppendItems is called with a nil Queue.
	ErrAppendQueueNil = fmt.Errorf("queue: AppendItems: queue must not be nil")

	// ErrAppendEmptyBeadIDs is returned when AppendItems is called with an
	// empty beadIDs slice.
	ErrAppendEmptyBeadIDs = fmt.Errorf("queue: AppendItems: beadIDs must not be empty")
)
