package handlercontract_test

// workqueue_hc016_test.go — sensors for HC-016 (work queue per agent role).
//
// Spec refs: specs/handler-contract.md §4.3.HC-016; bead hk-8i31.19.
//
// Helper prefix: workQueueFixture (per implementer-protocol.md
// §Helper-prefix discipline).
//
// What this file provides:
//
//   1. TestWorkQueueHC016_OneQueuePerDeclaredRole — NewWorkQueueSet creates
//      exactly one channel per ActorRole returned by core.AllActorRoles().
//
//   2. TestWorkQueueHC016_UnknownRoleReturnsFalse — Queue returns (nil, false)
//      for an unregistered role string.
//
//   3. TestWorkQueueHC016_QueueRolesMatchDeclaredRoles — WorkQueueSet.Roles()
//      returns the same set as core.AllActorRoles() (order-independent).
//
//   4. TestWorkQueueHC016_ChannelIsDistinctPerRole — each role's channel is a
//      distinct pointer; sharing channels would violate the "one queue per role"
//      invariant.
//
//   5. TestWorkQueueHC016_SendAndReceive — work items enqueued to one role's
//      channel are received from that channel and not from another role's
//      channel (cross-queue isolation).
//
//   6. TestWorkQueueHC016_ZeroCapacityUnbuffered — capacity=0 produces
//      unbuffered channels (len == 0, cap == 0).
//
//   7. TestWorkQueueHC016_NonZeroCapacityBuffered — capacity>0 produces
//      channels with the declared buffer depth.

import (
	"slices"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// workQueueFixtureWork is the item type used across HC-016 sensor tests.
// It is intentionally minimal — the queue semantics are role-keyed dispatch,
// not item content.
type workQueueFixtureWork struct {
	RunID string
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-016 — one queue per declared role
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkQueueHC016_OneQueuePerDeclaredRole verifies that NewWorkQueueSet
// creates exactly one channel for each ActorRole returned by
// core.AllActorRoles().
//
// Spec ref: specs/handler-contract.md §4.3.HC-016.
func TestWorkQueueHC016_OneQueuePerDeclaredRole(t *testing.T) {
	t.Parallel()

	wqs := handlercontract.NewWorkQueueSet[workQueueFixtureWork](8)
	declaredRoles := core.AllActorRoles()

	for _, role := range declaredRoles {
		role := role
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			ch, ok := wqs.Queue(role)
			if !ok {
				t.Errorf("WorkQueueSet.Queue(%q) = (_, false); HC-016 requires one queue per role", role)
			}
			if ch == nil {
				t.Errorf("WorkQueueSet.Queue(%q) returned nil channel; queue MUST be non-nil", role)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-016 — unknown role returns (nil, false)
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkQueueHC016_UnknownRoleReturnsFalse verifies that Queue returns
// (nil, false) for a role string that is not one of the declared ActorRoles.
//
// This protects against callers accidentally queuing work for a non-existent
// role and having it silently discarded.
//
// Spec ref: specs/handler-contract.md §4.3.HC-016.
func TestWorkQueueHC016_UnknownRoleReturnsFalse(t *testing.T) {
	t.Parallel()

	wqs := handlercontract.NewWorkQueueSet[workQueueFixtureWork](8)

	unknownRole := core.ActorRole("UnknownRole-hk-8i31.19")
	ch, ok := wqs.Queue(unknownRole)
	if ok {
		t.Errorf("WorkQueueSet.Queue(%q) = (_, true); want (nil, false) for undeclared role", unknownRole)
	}
	if ch != nil {
		t.Errorf("WorkQueueSet.Queue(%q) returned non-nil channel for undeclared role", unknownRole)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-016 — Roles() matches declared roles
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkQueueHC016_QueueRolesMatchDeclaredRoles verifies that
// WorkQueueSet.Roles() returns the same set as core.AllActorRoles()
// (order-independent comparison).
//
// Spec ref: specs/handler-contract.md §4.3.HC-016.
func TestWorkQueueHC016_QueueRolesMatchDeclaredRoles(t *testing.T) {
	t.Parallel()

	wqs := handlercontract.NewWorkQueueSet[workQueueFixtureWork](1)
	declared := core.AllActorRoles()
	got := wqs.Roles()

	if len(got) != len(declared) {
		t.Errorf("WorkQueueSet.Roles() len = %d, want %d", len(got), len(declared))
	}

	for _, r := range declared {
		if !slices.Contains(got, r) {
			t.Errorf("WorkQueueSet.Roles() missing declared role %q", r)
		}
	}

	for _, r := range got {
		if !slices.Contains(declared, r) {
			t.Errorf("WorkQueueSet.Roles() contains undeclared role %q", r)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-016 — each role's channel is distinct
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkQueueHC016_ChannelIsDistinctPerRole verifies that no two roles share
// the same channel pointer. Sharing a channel would collapse two roles' queues
// into one, violating HC-016's "one queue per role" invariant.
//
// Spec ref: specs/handler-contract.md §4.3.HC-016.
func TestWorkQueueHC016_ChannelIsDistinctPerRole(t *testing.T) {
	t.Parallel()

	wqs := handlercontract.NewWorkQueueSet[workQueueFixtureWork](8)
	declared := core.AllActorRoles()

	// Collect all channels.
	channels := make(map[core.ActorRole]chan workQueueFixtureWork, len(declared))
	for _, r := range declared {
		ch, ok := wqs.Queue(r)
		if !ok {
			t.Fatalf("WorkQueueSet.Queue(%q) returned ok=false during distinctness check", r)
		}
		channels[r] = ch
	}

	// Verify no two roles share the same underlying channel.
	for i, r1 := range declared {
		for _, r2 := range declared[i+1:] {
			if channels[r1] == channels[r2] {
				t.Errorf(
					"roles %q and %q share the same channel pointer; HC-016 requires one distinct queue per role",
					r1, r2,
				)
			}
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-016 — cross-queue isolation: send on one, receive only on that one
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkQueueHC016_SendAndReceive verifies that a work item enqueued to the
// Planner's channel is received from the Planner's channel and does NOT appear
// on the Builder's channel.
//
// This is the concrete expression of "workers drain their own queue; cross-queue
// handoff MUST go through explicit transitions per [execution-model.md §4.6],
// never shared memory."
//
// Spec ref: specs/handler-contract.md §4.3.HC-016.
func TestWorkQueueHC016_SendAndReceive(t *testing.T) {
	t.Parallel()

	wqs := handlercontract.NewWorkQueueSet[workQueueFixtureWork](1)

	plannerCh, ok := wqs.Queue(core.ActorRolePlanner)
	if !ok {
		t.Fatal("WorkQueueSet.Queue(Planner) returned ok=false")
	}
	builderCh, ok := wqs.Queue(core.ActorRoleBuilder)
	if !ok {
		t.Fatal("WorkQueueSet.Queue(Builder) returned ok=false")
	}

	work := workQueueFixtureWork{RunID: "run-hc016-test"}
	plannerCh <- work

	// Receive from the Planner channel and verify it is the same item.
	received := <-plannerCh
	if received != work {
		t.Errorf("received work %+v from Planner queue, want %+v", received, work)
	}

	// Builder channel MUST be empty (nothing was sent there).
	select {
	case unexpected := <-builderCh:
		t.Errorf("Builder queue received unexpected item %+v; cross-queue isolation violated (HC-016)", unexpected)
	default:
		// expected: no item on builder channel
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-016 — channel capacity semantics
// ─────────────────────────────────────────────────────────────────────────────

// TestWorkQueueHC016_ZeroCapacityUnbuffered verifies that NewWorkQueueSet(0)
// produces unbuffered channels (cap == 0).
//
// Spec ref: specs/handler-contract.md §4.3.HC-016.
func TestWorkQueueHC016_ZeroCapacityUnbuffered(t *testing.T) {
	t.Parallel()

	wqs := handlercontract.NewWorkQueueSet[workQueueFixtureWork](0)

	ch, ok := wqs.Queue(core.ActorRolePlanner)
	if !ok {
		t.Fatal("WorkQueueSet.Queue(Planner) returned ok=false")
	}
	if cap(ch) != 0 {
		t.Errorf("NewWorkQueueSet(0): Planner channel cap = %d, want 0 (unbuffered)", cap(ch))
	}
}

// TestWorkQueueHC016_NonZeroCapacityBuffered verifies that NewWorkQueueSet(n)
// produces channels with buffer depth n.
//
// Spec ref: specs/handler-contract.md §4.3.HC-016.
func TestWorkQueueHC016_NonZeroCapacityBuffered(t *testing.T) {
	t.Parallel()

	const depth = 16
	wqs := handlercontract.NewWorkQueueSet[workQueueFixtureWork](depth)

	for _, role := range core.AllActorRoles() {
		role := role
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			ch, ok := wqs.Queue(role)
			if !ok {
				t.Fatalf("WorkQueueSet.Queue(%q) returned ok=false", role)
			}
			if cap(ch) != depth {
				t.Errorf("NewWorkQueueSet(%d): %q channel cap = %d, want %d",
					depth, role, cap(ch), depth)
			}
		})
	}
}
