# Queue model research findings

## Questions

1. What does failed-item resume do now?
2. Does the production command invoke that recovery?
3. Does release use the durable queue transaction owner?
4. What state must recovery preserve?

## Findings

`specs/queue-model.md` defers `queue-resume` and makes a failed queue
`paused-by-failure`. `internal/queue/resume.go` `ResumeFromFailure` can reopen
failed groups and re-arm failed items in memory. It calls
`ReactivateFailedItem`, which clears attempts and the failure reason but leaves
the old `RunID` in place.

No production caller invokes `ResumeFromFailure`. `internal/queue/cli/resume.go`
`RunQueueResume` sends `operator-resume`. `QueueOperatorEventConsumer` in
`internal/queuewiring/operatorevents.go` accepts only `paused-by-drain` and
calls `ResumeQueueFromDrain`.

`QueueStore.Transact` in `internal/queuewiring/store.go` is the clone, mutate,
persist, install boundary. Raw `SetQueueByName` and `LockedSetQueueByName`
clear quarantine. `evaluateGroupAdvanceWithOutcome` and `adoptLiveRunSession`
in `internal/daemon/scheduler.go` use that raw path and can leave memory and
disk different after a failed persist.

## Patterns and risks

The pure mutation and the production command are different paths. Raw release
writes can reopen a queue after a durable write failed. The present resume
shape can leave a resumed item naming its prior terminal run.

## Design constraints

- One durable, idempotent recovery transaction must change the queue, failed
  groups, and only failed items before dispatch restarts.
- It must clear or replace old run identity and preserve completed items.
- Acquire, undo, terminal release, and recovery must use the same transaction
  owner.
- Quarantine may clear only through explicit durable recovery classification.
