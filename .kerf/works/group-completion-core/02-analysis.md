# Current-state analysis

## Affected areas

### Daemon completion shell

`internal/daemon/scheduler.go` `evaluateGroupAdvanceWithOutcome` owns the current path.
It resolves a queue under the store write lock.
It validates the queue ID, group index, and item index.
It changes the item status and calls `queue.AdvanceGroup`.

The same function selects a pending successor group.
It also decides whether the queue pauses or completes.
It persists or unlinks the queue, updates the store, wakes dispatch, cancels contexts, and emits events.

`internal/daemon/bootworkloop.go` calls the function after a force reap.
`internal/daemon/scheduler.go` also calls it after reservation failures, claim failures, and normal run completion.

### Queue state machine

`internal/queue/state.go` owns `AdvanceGroup`.
This function reads values and returns a group status plus ordered events.
It reads no clock because the caller supplies `now`.
It performs no I/O.

`internal/queue/types.go` owns `Queue`, `Group`, `Item`, and their named status types.
The types are value records with slices for groups and items.

`internal/queue/persistence.go` owns `Persist` and `CompleteAndUnlink`.
These are effect functions and must remain outside the new decision.

### Queue store effects

`internal/queuewiring/store.go` owns the mutable queue store.
The daemon holds its mutation lock while it changes the selected queue.
The store also owns `Wake` and `ClearQueueByName`.

The new decision cannot import `queuewiring` without reversing the package direction.

## Existing constraints

- Queue and event JSON must not change.
- `queue.AdvanceGroup` defines event order for group transitions.
- Complete success calls `CompleteAndUnlink` and removes the queue from memory.
- Complete with failures persists a paused queue.
- An intermediate completion persists the queue and wakes dispatch.
- Event emission occurs after the store lock is released.
- A stale queue ID or invalid group location causes no change.
- The daemon has several completion callers and two cancellation hooks.

## Test shape

`internal/queue/state_test.go` uses table tests with fixed times.
Daemon tests exercise force reap, claim failure, restart recovery, and queue persistence.
The full daemon suite has long timing failures, so the change needs focused named tests.

## Recent work

Recent commits fixed reservation release, claim failure routing, stale runs, and event typing.
The latest core slice passes one completion time into all group transitions.
This work must keep those compiler and recovery edges.

## Relevant code health issues

The daemon function has no typed result for its effect policy.
Callers cannot test the decision without a queue store, filesystem, bus, and cancellation hooks.
The function also uses string conversions when it calls pure orchestrator predicates.
Those conversions hide queue vocabulary from the compiler.
