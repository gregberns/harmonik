package queue

// state.go — group state machine for the queue subsystem.
//
// Implements the per-group transition table from specs/queue-model.md §5
// (QM-030..QM-036) and the associated queue-level lifecycle transitions from
// §8 (QM-050..QM-055).
//
// Exported surface:
//   - AdvanceGroup — evaluate one group's readiness to transition; returns new
//     GroupStatus and the ordered event list to emit.
//   - EligibleItems — return dispatch-eligible items for an active group,
//     respecting wave (QM-036) vs. stream (QM-035) head-of-line semantics.
//
// Spec ref: specs/queue-model.md §5, §8.
// Bead ref: hk-e4s70.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
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
) (newStatus GroupStatus, events []core.Event, err error) {
	if g == nil {
		return "", nil, ErrGroupNil
	}
	if queueID == "" {
		return "", nil, ErrQueueIDEmpty
	}
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}

	// QM-032 — no re-entry of terminal states.
	if groupIsTerminal(g.Status) {
		return g.Status, nil, nil
	}

	switch g.Status {
	case GroupStatusPending:
		return advancePending(g, queueStatus, queueID, now)
	case GroupStatusActive:
		return advanceActive(g, queueID, now)
	default:
		// Unknown status — leave unchanged, surface as an error so callers
		// can detect corrupt group records without silently swallowing them.
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
// Stream groups (QM-035): dispatched and terminal items are skipped; the
// first pending item found is returned. HOL blocking applies only when the
// first non-terminal, non-dispatched item is deferred-for-ledger-dep — in
// that case nil is returned. An in-flight (dispatched) head does NOT block
// subsequent pending items; this allows --max-concurrent > 1 to work.
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

// -----------------------------------------------------------------------
// internal helpers
// -----------------------------------------------------------------------

// groupIsTerminal reports whether s is one of the two terminal GroupStatus
// values per specs/queue-model.md §2.5.
func groupIsTerminal(s GroupStatus) bool {
	return s == GroupStatusCompleteSuccess || s == GroupStatusCompleteWithFailures
}

// itemIsTerminal reports whether s is a terminal ItemStatus per §2.7.
// deferred-for-ledger-dep is NOT terminal (per §2.8 normative sentence).
func itemIsTerminal(s ItemStatus) bool {
	return s == ItemStatusCompleted || s == ItemStatusFailed
}

// advancePending applies the pending → active transition per QM-031.
// Guard: queueStatus MUST be active.
func advancePending(
	g *Group,
	queueStatus QueueStatus,
	queueID string,
	now time.Time,
) (GroupStatus, []core.Event, error) {
	// QM-031 guard: only advance when queue is active.
	if queueStatus != QueueStatusActive {
		return GroupStatusPending, nil, nil
	}

	nowStr := now.UTC().Format(time.RFC3339Nano)

	evt, err := newEvent("queue_group_started", &core.QueueGroupStartedPayload{
		QueueID:    queueID,
		GroupIndex: g.GroupIndex,
		GroupKind:  string(g.Kind),
		ItemCount:  len(g.Items),
		StartedAt:  nowStr,
	})
	if err != nil {
		return GroupStatusPending, nil, fmt.Errorf("queue: AdvanceGroup: build queue_group_started: %w", err)
	}

	return GroupStatusActive, []core.Event{evt}, nil
}

// advanceActive applies the active → terminal transition per QM-030.
// Guard: every item MUST be terminal (QM-034 — failed siblings don't interrupt).
func advanceActive(
	g *Group,
	queueID string,
	now time.Time,
) (GroupStatus, []core.Event, error) {
	// QM-030 — all-terminal gate.
	if !allItemsTerminal(g) {
		return GroupStatusActive, nil, nil
	}

	successCount, failCount := countOutcomes(g)
	nowStr := now.UTC().Format(time.RFC3339Nano)

	if failCount == 0 {
		// active → complete-success (§5.1 row 3)
		evt, err := newEvent("queue_group_completed", &core.QueueGroupCompletedPayload{
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
		return GroupStatusCompleteSuccess, []core.Event{evt}, nil
	}

	// active → complete-with-failures (§5.1 row 4)
	// Emit queue_group_completed, then queue_paused{group_failure}.
	evtCompleted, err := newEvent("queue_group_completed", &core.QueueGroupCompletedPayload{
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

	evtPaused, err := newEvent("queue_paused", &core.QueuePausedPayload{
		QueueID:    queueID,
		GroupIndex: g.GroupIndex,
		FailCount:  failCount,
		PausedAt:   nowStr,
		Reason:     "group_failure",
	})
	if err != nil {
		return GroupStatusActive, nil, fmt.Errorf("queue: AdvanceGroup: build queue_paused: %w", err)
	}

	return GroupStatusCompleteWithFailures, []core.Event{evtCompleted, evtPaused}, nil
}

// allItemsTerminal reports whether every item in g has reached a terminal
// ItemStatus per QM-030. An empty items list is considered all-terminal
// (vacuously true).
func allItemsTerminal(g *Group) bool {
	for i := range g.Items {
		if !itemIsTerminal(g.Items[i].Status) {
			return false
		}
	}
	return true
}

// countOutcomes counts completed vs. failed items in g.
func countOutcomes(g *Group) (successCount, failCount int) {
	for i := range g.Items {
		switch g.Items[i].Status {
		case ItemStatusCompleted:
			successCount++
		case ItemStatusFailed:
			failCount++
		}
	}
	return successCount, failCount
}

// waveEligible returns all pending (non-deferred) items in a wave group per
// QM-036: wave admission is unordered; deferred-for-ledger-dep siblings are
// skipped while non-deferred siblings proceed.
func waveEligible(g *Group) []*Item {
	var out []*Item
	for i := range g.Items {
		if g.Items[i].Status == ItemStatusPending && g.Items[i].Attempts < MaxItemAttempts {
			out = append(out, &g.Items[i])
		}
	}
	return out
}

// streamEligible returns at most the earliest-indexed eligible item in a
// stream group per QM-035 head-of-line blocking.
//
// Scanning skips terminal items (completed, failed) and in-flight items
// (dispatched). The first non-skipped item is evaluated:
//   - If it is pending → return it (eligible for dispatch).
//   - If it is deferred-for-ledger-dep → return nil (HOL blocked per QM-035 v0.1).
//
// Dispatched items are skipped (not HOL-blocking) so that a second pending
// item can be dispatched concurrently with an in-flight head under
// --max-concurrent > 1. This matches QM-035: "after all earlier items have
// at least entered dispatched" the tail item is eligible.
//
// Spec ref: specs/queue-model.md §5.6 QM-035.
// Bead ref: hk-9a27q.
func streamEligible(g *Group) []*Item {
	for i := range g.Items {
		switch g.Items[i].Status {
		case ItemStatusPending:
			if g.Items[i].Attempts >= MaxItemAttempts {
				// Over-limit: skip as if terminal (defense-in-depth; hk-6pspu).
				continue
			}
			return []*Item{&g.Items[i]}
		case ItemStatusDeferredForLedgerDep:
			// HOL blocked: deferred head prevents dispatch of subsequent items
			// in v0.1 (out-of-order dispatch is deferred per QM-035).
			return nil
		case ItemStatusDispatched, ItemStatusCompleted, ItemStatusFailed:
			// In-flight or terminal: skip and scan for the next pending item.
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
//   - B is terminal within this queue group (completed/failed); the daemon
//     marks the completing item terminal before the dispatch loop re-evaluates,
//     so a chained predecessor that just finished satisfies this branch; or
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

		// Find this item's blockers among its in-group siblings and check
		// whether every one is resolved. The deferral at submit time was keyed
		// on intra-group BlocksEdge pairs (validation.go §QM-025), so the
		// un-defer check scans the same sibling set.
		allResolved := true
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
			// blocker blocks blocked. It is resolved iff it is terminal in the
			// queue OR no longer open in the ledger.
			if itemIsTerminal(g.Items[j].Status) {
				continue
			}
			status, err := ledger.LookupStatus(ctx, blocker)
			if err != nil {
				return undeferred, fmt.Errorf("queue: ReevaluateDeferred: LookupStatus %q: %w", blocker, err)
			}
			if status == BeadStatusOpen || status == BeadStatusInProgress {
				// Blocker still open → item stays deferred.
				allResolved = false
				break
			}
		}

		if allResolved {
			g.Items[i].Status = ItemStatusPending
			undeferred = append(undeferred, blocked)
		}
	}

	return undeferred, nil
}

// newEvent constructs a core.Event for the given type+payload, marshalling
// the payload to json.RawMessage and stamping a fresh UUIDv7 EventID.
//
// Callers that need daemon-stamped IDs (EV-002b) should discard the EventID
// and let the daemon watcher re-stamp. The SourceSubsystem field is set to
// the queue subsystem identifier per EV-034a.
//
// Returns an error if UUID generation or JSON marshalling fails.
func newEvent(eventType string, payload core.EventPayload) (core.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return core.Event{}, fmt.Errorf("queue: newEvent: marshal payload for %q: %w", eventType, err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return core.Event{}, fmt.Errorf("queue: newEvent: uuid.NewV7: %w", err)
	}
	eventID := core.EventID(id)
	now := time.Now().UTC()
	e := core.Event{
		EventID:         eventID,
		SchemaVersion:   1,
		Type:            eventType,
		TimestampWall:   now,
		SourceSubsystem: subsystemID,
		Payload:         raw,
	}
	return e, nil
}

// Sentinel errors returned by AdvanceGroup.
var (
	// ErrGroupNil is returned when AdvanceGroup is called with a nil group.
	ErrGroupNil = fmt.Errorf("queue: AdvanceGroup: group must not be nil")

	// ErrQueueIDEmpty is returned when AdvanceGroup is called with an empty
	// queueID.
	ErrQueueIDEmpty = fmt.Errorf("queue: AdvanceGroup: queueID must not be empty")
)
