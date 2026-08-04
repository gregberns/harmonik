# Queue status writer — problem space

## Summary

Queue, group, and item status changes have many direct writers. The writers
span `internal/queue`, `internal/queuewiring`, `internal/lifecycle`, and
`internal/daemon`. The current protection is a mutex and local convention.
It does not state one owner for state changes or for their durable write.

The first implementation slice moves the writers outside `internal/daemon`
behind a queue-owned transition API. Daemon writers remain unchanged until
Step 7 gives them a safe integration seam.

## Goals

- Make `internal/queue` own queue, group, and item status transitions.
- Keep each transition valid against the queue state machine.
- Keep durable persistence before a caller reports a completed mutation for the
  writers this lane can complete without editing `internal/daemon`.
- Give callers a narrow API that makes direct status writes unnecessary.
- Add focused tests that fail when a caller bypasses transition validation.

## Non-goals

- Do not change `internal/daemon` writers in this slice.
- Do not redesign queue receipts, archive intents, or event payloads.
- Do not add queue resume or queue control features.
- Do not change the queue state-machine semantics.

## Constraints

- `internal/queue` remains the state and persistence owner.
- Existing `QueueStore.Transact` protection remains the durable mutation path.
- A failed durable write must not expose a successful in-memory transition on a
  path this lane changes. Deferred recovery remains the explicit Alpha handoff.
- `scripts/queuewiring-freeze-gate.sh` must pass in the same commit.
- The implementation follows `PRINCIPLES.md`, the delete-and-rewrite charter,
  and `DECOMPOSITION-MAP.md` Step 11.

## Success criteria

- Fifteen of the sixteen non-daemon direct status writers use a queue-owned
  transition path with durable ordering. The remaining deferred-recovery source
  assignment moves into the queue transition primitive, but its live daemon
  caller remains a named Alpha handoff because this lane must not edit
  `internal/daemon`.
- Each covered transition path validates the target state and preserves
  durable-write ordering.
- Focused tests cover a valid transition and a failed durable write.
- The implementation does not modify `internal/daemon`.

## Spec areas

`specs/queue-model.md` owns the queue, group, and item state machines. The
change should match its current requirements. A spec amendment is needed only
if the measured code reveals that the current transition contract is missing.
