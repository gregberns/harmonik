package queue

// append.go — queue-append semantics for the queue subsystem.
//
// Implements the append operation per specs/queue-model.md §7:
//   - QM-040: stream-only target (wave groups reject)
//   - QM-041: tail-append with status: pending, appended_at stamped
//   - QM-042: emit queue_appended event after persistence; emit
//     queue_item_deferred_for_ledger_dep for each QM-025-deferred item
//   - QM-043: append to active stream is in-flight-safe (no interference)
//   - QM-044: terminal-status rejection (append to complete-* group rejected)
//
// Exported surface:
//   - AppendItems — validate + mutate + emit events for a queue-append request.
//
// Spec ref: specs/queue-model.md §7.
// Bead ref: hk-soxgu.

import (
	"context"
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
) (*Queue, []core.Event, error) {
	if q == nil {
		return nil, nil, ErrAppendQueueNil
	}
	if len(beadIDs) == 0 {
		return nil, nil, ErrAppendEmptyBeadIDs
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	// Build the Items slice for the validation request.
	appendItems := make([]Item, len(beadIDs))
	for i, id := range beadIDs {
		appendItems[i] = Item{
			BeadID: core.BeadID(id),
			Status: ItemStatusPending,
		}
	}

	// Run the validation pipeline (IsAppend=true covers QM-024, QM-020..QM-026,
	// QM-025 informational).
	vreq := ValidationRequest{
		Groups: []Group{
			{
				GroupIndex: groupIndex,
				Kind:       GroupKindStream,
				Status:     GroupStatusPending, // placeholder; QM-024 checks live group
				Items:      appendItems,
			},
		},
		ActiveQueue:      q,
		IsAppend:         true,
		AppendGroupIndex: groupIndex,
	}

	verrs, deferredPairs, err := Validate(ctx, vreq, ledger)
	if err != nil {
		return nil, nil, fmt.Errorf("queue: AppendItems: validation: %w", err)
	}
	if len(verrs) > 0 {
		return nil, nil, &verrs[0]
	}

	// Validation passed. Stamp accept time and resolve deferred items.
	now := time.Now().UTC()
	nowStr := now.UTC().Format(time.RFC3339Nano)

	// Build a set of deferred bead IDs from QM-025 notices returned by Validate.
	// Validate only checks edges within the appended set; we also need to check
	// edges from existing non-terminal group items against each appended item.
	deferredSet := make(map[core.BeadID]core.BeadID)
	for _, p := range deferredPairs {
		if p.GroupIndex == groupIndex {
			deferredSet[p.BeadID] = p.BlockerBeadID
		}
	}

	// QM-044 / QM-025 cross-check: for each newly appended item that is not yet
	// in deferredSet, check whether any existing non-terminal item in the group
	// has a blocks edge against it. This covers the case where the blocker
	// already lives in the group (not in the appended set).
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

	// QM-041 — tail-append: build final items with correct status + appended_at.
	newItems := make([]Item, len(beadIDs))
	for i, id := range beadIDs {
		beadID := core.BeadID(id)
		status := ItemStatusPending
		if _, deferred := deferredSet[beadID]; deferred {
			status = ItemStatusDeferredForLedgerDep
		}
		appended := now
		newItems[i] = Item{
			BeadID:     beadID,
			Status:     status,
			RunID:      nil,
			AppendedAt: &appended,
		}
	}

	// Mutate in place: append to the target group's Items slice.
	g := &q.Groups[groupIndex]
	tailStart := len(g.Items)
	g.Items = append(g.Items, newItems...)

	// QM-042 — emit queue_appended.
	appendedBeadIDStrs := make([]string, len(beadIDs))
	copy(appendedBeadIDStrs, beadIDs)

	evtAppended, err := newEvent("queue_appended", &core.QueueAppendedPayload{
		QueueID:         q.QueueID,
		GroupIndex:      groupIndex,
		AppendedBeadIDs: appendedBeadIDStrs,
		AppendedAt:      nowStr,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("queue: AppendItems: build queue_appended: %w", err)
	}

	events := []core.Event{evtAppended}

	// QM-042 — emit queue_item_deferred_for_ledger_dep per deferred item, in
	// append order, after queue_appended.
	for i, id := range beadIDs {
		beadID := core.BeadID(id)
		blockerID, deferred := deferredSet[beadID]
		if !deferred {
			continue
		}
		_ = tailStart + i // index of the newly appended item; informational
		evtDeferred, evtErr := newEvent("queue_item_deferred_for_ledger_dep", &core.QueueItemDeferredForLedgerDepPayload{
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
	_, ok := err.(*ValidationError)
	return ok
}

// ValidationReason returns the [QueueValidationReason] from err when it is a
// [ValidationError], and the zero value otherwise.
func ValidationReason(err error) QueueValidationReason {
	if ve, ok := err.(*ValidationError); ok {
		return ve.Reason
	}
	return ""
}

// itemIsTerminalStatus reports whether s is a terminal ItemStatus.
// Mirrors the unexported itemIsTerminal in state.go; duplicated here to avoid
// coupling append.go to state.go internals.
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
