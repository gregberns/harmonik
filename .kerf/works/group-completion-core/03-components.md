# Components

## Requirement map

| Goal | Requirements |
| --- | --- |
| Typed value decision | V1, D1, D2, D3 |
| Effects at the daemon edge | V2, S1, S2 |
| Stable state and event data | V3, D4, P1, P2 |
| One supplied completion time | V4, D5 |
| Full value tests | T1, T2, T3 |

## 1. Event intent vocabulary

Owner: `internal/queue`.

- V1: `EventIntent` contains an event type and deterministic JSON payload bytes.
- V2: Creating an intent reads no clock and creates no identity.
- V3: The payload JSON and intent order match the current event payloads and order.
- V4: Payload timestamps use only the time supplied to the state function.

This component depends only on `internal/core` event types and payload records.

## 2. Queue completion decision — blocked

Owner: `internal/queue`.

- D1: `DecideGroupCompletion` accepts a detached queue input, an expected queue ID, an item location, an outcome, a completion time, and any prebound receipt identity required by QM-053.
- D2: The function returns a fully detached next queue, receipt-bound ordered event intents, a disposition, and a changed flag.
- D3: The function returns typed errors for malformed locations and corrupt state.
- D4: The function does not change the input queue or its nested slices, maps, and pointers.
- D5: The current group and successor group use the same normalized completion time.
- D6: Stale queue identity returns no change and no error.
- D7: Missing groups, duplicate group indexes, and invalid item indexes return typed errors.
- D8: A repeated matching terminal outcome returns no change. A conflicting terminal outcome returns a typed conflict.

This component depends on the event intent vocabulary and the existing group transition rules.
Final-success design also depends on the QM-001 and QM-005 transaction owner that prebinds the completion receipt before intent bytes exist.
The component must not proceed until that input contract exists.

## 3. Durability policy — blocked

Owner: `internal/daemon` with result types from `internal/queue`.

- P1: Intermediate and paused decisions emit events only after the QM-001 transaction commits.
- P2: Final completion depends on a QM-001 and QM-005 transaction result with receipt, observation, cleanup, and release states.
- P3: A commit failure keeps the live queue, emits no transition events, and fires no completion cancel.
- P4: A pre-release cleanup failure keeps ownership and cancel hooks unchanged. A post-release marker failure does not reacquire ownership.
- P5: A paused queue wakes dispatch and fires only the queue-exit cancel.
- P6: A fully successful queue fires the drain cancel and queue-exit cancel only after the transaction proves QM-053 ownership release.
- P7: Eager refill runs after the shell completes its event and cancellation work.

This component depends on the queue completion decision.
It is provisional until the queue transaction owner exposes the required result.
`CompleteAndUnlinkResult` documents legacy failure classes but is not the target interface.

## 4. Daemon completion shell — blocked

Owner: `internal/daemon`.

- S1: The shell holds the store mutation lock only for lookup, decision, durable write, and store update.
- S2: The shell performs wake, cancel, event emission, and eager refill after lock release.
- S3: The shell marshals each intent payload and passes its type and bytes to the event bus.
- S4: A missing queue or stale queue identity causes no effects.
- S5: The shell logs typed decision and durability failures with queue and group identity.

This component depends on the durability policy and queue completion decision.
It must not replace the live path before the queue transaction dependency exists.

## 5. Proof suite

Owners: `internal/queue` and `internal/daemon` tests.

- T1: Queue table tests cover every no-change, conflict, malformed input, pause, continue, successor, and completion result.
- T2: Tests prove the input queue stays deeply unchanged.
- T3: Tests assert exact intent order, payload bytes, and UTC payload times.
- T4: Shell tests use positive effect counters for persist, clear, wake, cancel, and emit.
- T5: Mutation checks break each main disposition and make its named test fail.

This component depends on all production components.

## Dependency graph

`proof suite -> daemon shell -> durability policy -> queue completion decision -> event intent vocabulary`

The blocked final-success edge is `queue completion decision -> QM-001/QM-005 receipt prebinding contract`.
Only the event intent vocabulary has no blocked dependency.

The graph has no reverse package import.
`internal/queue` does not import `internal/daemon` or `internal/queuewiring`.
