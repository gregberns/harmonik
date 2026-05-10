package handlercontract

import (
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
)

// WorkQueueSet enforces the HC-016 invariant: one work queue per agent role.
//
// The orchestrator MUST maintain exactly one channel-based work queue for each
// declared ActorRole (per [architecture.md §4.8]). Workers drain their own
// queue; cross-queue handoff MUST go through explicit transitions per
// [execution-model.md §4.6], never shared memory.
//
// WorkQueueSet is instantiated once at daemon init. Its zero value is not
// usable; callers MUST use NewWorkQueueSet.
//
// Spec: specs/handler-contract.md §4.3.HC-016.
//
// Tags: mechanism
type WorkQueueSet[T any] struct {
	queues map[core.ActorRole]chan T
}

// NewWorkQueueSet creates a WorkQueueSet containing one buffered channel per
// valid ActorRole declared in core.
//
// capacity is the buffer depth of each per-role channel. A capacity of 0
// produces unbuffered channels; callers MUST ensure sends do not block
// indefinitely. The daemon SHOULD use a non-zero capacity to decouple the
// dispatcher from per-role workers.
//
// NewWorkQueueSet panics if any declared ActorRole is missing from the
// constructed set — a daemon defect that indicates the declared-role list and
// the queue-set diverged. This panic is intentional: HC-016 requires exactly
// one queue per role; a missing queue is a structural invariant violation, not
// a recoverable condition.
//
// Spec: specs/handler-contract.md §4.3.HC-016; [architecture.md §4.8 AR-032].
func NewWorkQueueSet[T any](capacity int) *WorkQueueSet[T] {
	roles := core.AllActorRoles()
	queues := make(map[core.ActorRole]chan T, len(roles))
	for _, r := range roles {
		queues[r] = make(chan T, capacity)
	}
	wqs := &WorkQueueSet[T]{queues: queues}
	// Invariant check: every declared role must have a queue.
	for _, r := range roles {
		if _, ok := wqs.queues[r]; !ok {
			panic(fmt.Sprintf("workqueue: HC-016 invariant violated: no queue for role %q", r))
		}
	}
	return wqs
}

// Queue returns the work channel for role.
//
// Returns (ch, true) when role is registered, (nil, false) for unknown roles.
// Callers MUST NOT cross-post work items to a different role's channel; a
// cross-queue handoff MUST go through an explicit transition per
// [execution-model.md §4.6].
//
// Spec: specs/handler-contract.md §4.3.HC-016.
func (wqs *WorkQueueSet[T]) Queue(role core.ActorRole) (chan T, bool) {
	ch, ok := wqs.queues[role]
	return ch, ok
}

// Roles returns the set of ActorRoles for which queues are registered.
//
// The returned slice is a snapshot; callers MUST NOT modify it.
// The order is unspecified.
//
// Spec: specs/handler-contract.md §4.3.HC-016.
func (wqs *WorkQueueSet[T]) Roles() []core.ActorRole {
	roles := make([]core.ActorRole, 0, len(wqs.queues))
	for r := range wqs.queues {
		roles = append(roles, r)
	}
	return roles
}
