# Components

## Affected existing specs

### `specs/queue-model.md`

- **Change summary:** The implementation gains a queue-owned transition API for
  queue, group, and item statuses. The API must preserve the state machines and
  durable-write ordering that this spec already defines.
- **Requirements:**
  - The API accepts only transitions allowed by the queue, group, and item state
    machines in §§2, 5, and 8.
  - `QueueStore.Transact` remains the durable boundary for loaded queues. The
    bounded startup candidate path and existing terminal namespace sequence
    stay explicit. A failed durable write must not expose a successful
    in-memory transition on a path this lane changes, as required by QM-001,
    QM-060, and QM-063. Deferred recovery remains the Alpha handoff.
  - Group completion remains gated by terminal item statuses per QM-030.
  - The outside-daemon slice must not change queue lifecycle meanings,
    persistence records, receipts, or event payloads.
  - The transition API replaces 15 measured direct status assignments in
    `internal/queue`, `internal/lifecycle`, and `internal/queuewiring`. One
    further source assignment, deferred recovery in `state.go`, moves into the
    queue transition primitive but keeps its unsafe daemon compatibility caller
    until Alpha wires the store form. One covered site is a pre-publication
    submit-candidate adjustment. This lane does not modify the 14 assignments
    in `internal/daemon`.
  - Focused tests prove a valid transition, a rejected transition, and a failed
    durable write. A shrink-only bypass check reports the 14 remaining daemon
    writers and the 18 construction-path writes. `scripts/queuewiring-freeze-gate.sh`
    passes in the same commit.
- **Dependencies:** `execution-model.md`, `beads-integration.md`,
  `operator-nfr.md`, and `event-model.md` define consumed behavior. This slice
  does not change them.

### `specs/execution-model.md`

- **Change summary:** It defines item/run lifecycle meaning that queue item
  transitions consume.
- **Requirements:** No change in this slice. Research maps each item transition
  to the existing run outcome it observes.
- **Dependencies:** `queue-model.md` owns the queue-side status result.

### `specs/beads-integration.md`

- **Change summary:** It defines ledger-dependent item states and recovery.
- **Requirements:** No change in this slice. The API must preserve the existing
  deferred-for-ledger-dependency behavior.
- **Dependencies:** `queue-model.md` owns queue status transitions.

### `specs/operator-nfr.md`

- **Change summary:** It defines pause and drain inputs that change queue state.
- **Requirements:** No change in this slice. The API preserves paused-by-drain
  and pause recovery behavior.
- **Dependencies:** `queue-model.md` owns the queue state machine.

### `specs/event-model.md`

- **Change summary:** It defines events emitted after queue status changes.
- **Requirements:** No change in this slice. The API must preserve persistence
  before any caller emits its current event.
- **Dependencies:** `queue-model.md` supplies the durable transition result.

## New specs

None. This is an implementation boundary for existing queue-model behavior. It
does not add a state, a wire field, or a durable record.

## Dependency map

`specs/queue-model.md` defines the transition and durability rules. The four
referenced specs define inputs or effects that this slice consumes unchanged.
Research maps each of the 16 owned writers to its queue-model rule. The API
design follows that map. A spec amendment is a blocker only if a measured writer
has no existing rule.

## Goal to area traceability

| Goal | Spec area |
| --- | --- |
| Queue owns queue, group, and item status transitions | `queue-model.md` §§2, 5, and 8 |
| Each transition is valid | `queue-model.md` §§2.2, 2.5, 2.7, 5.1, and 8 |
| Persistence precedes a successful covered mutation | `queue-model.md` QM-001 and QM-063 |
| A narrow API removes direct non-daemon writes | `queue-model.md` state-machine rules above |
| Tests detect bypasses and durable-write loss | `queue-model.md` QM-001, QM-060, and QM-063 |
| Deferred items, pauses, and events retain their meaning | `beads-integration.md`, `operator-nfr.md`, `event-model.md` |
