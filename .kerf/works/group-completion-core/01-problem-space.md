# Problem space

## Summary

The daemon owns group completion in one large effectful function.
That function changes queue items and groups, selects the next group, persists state, wakes dispatch, cancels runs, and emits events.
The mixed ownership makes process-death behavior and transition tests hard to prove.

## Goals

- Define group completion as a typed value decision.
- Keep persistence, wake, cancel, and event emission in the daemon shell.
- Preserve queue state, event order, and wire data.
- Make one fixed completion time control all events from one completion.
- Let table tests cover every admitted queue and group state.

## Non-goals

- Do not redesign the full run machine.
- Do not split `RunEnv` or `SharedHandles` in this work.
- Do not change queue persistence format or event payload format.
- Do not change retry, claim, merge, or bead-close policy.
- Do not change daemon shutdown behavior.

## Constraints

- The queue package owns queue and group state rules.
- The daemon package owns external effects and composition.
- Existing event order must stay stable.
- A paused queue must remain on disk.
- A fully successful queue must complete and unlink.
- The current long daemon suite is not a reliable local gate.
- Focused tests must prove the changed production path.

## Success criteria

- A pure queue function accepts queue state, item identity, outcome, and completion time.
- The function returns the next queue state, ordered events, and a typed completion disposition.
- The function reads no clock and performs no I/O.
- The daemon applies the returned disposition through its existing effect ports.
- Table tests cover stale identity, invalid indexes, item success, item failure, next-group activation, pause, and full completion.
- A mutation of each main decision branch makes its named test fail.
- Focused queue and daemon tests pass.
- Repository compile, tagged compile, build, and vet checks pass.
