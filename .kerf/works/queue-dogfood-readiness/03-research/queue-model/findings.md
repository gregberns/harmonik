# Research — Queue model

## Questions

1. Which queue identity and failed items can recovery change?
2. What durable boundary proves recovery before dispatch restarts?
3. What happens if recovery write failure quarantines the queue?

## Findings

- Section 1.2 and Appendix A.3 defer `queue-recover` to v0.2. `QM-032` makes
  `complete-with-failures` terminal. `QM-052` requires a new submit after
  restart. This conflicts with the current recovery code and command name.
- `internal/queue/resume.go` `ResumeFromFailure` re-arms failed items, clears
  attempts and failure reason, reopens failed groups, and sets
  `paused-by-failure` to active. It has no production caller.
- `QueueStore.Transact` is the required boundary: snapshot, clone and mutate,
  write a replacement, install memory only after commit, then wake.
- Raw `SetQueue`, `SetQueueByName`, and locked setter paths clear quarantine.
  Recovery must use `Transact`, not `Persist` with a raw setter.

## Patterns to keep

- Match queue recovery to one named or ID-selected queue.
- Use the transaction path so disk and memory change together.
- Treat a failed durable write or quarantine as recovery failure with no
  in-memory success state.

## Risks and decisions

- Decide whether recovery re-arms all failed items or a stated selected subset.
  The existing pure function re-arms all failed items.
- Decide whether fresh submit remains a separate replacement action for a
  failed queue. Two unlabelled recovery paths would confuse audit and operators.
- Add a production-path fault test. Unit tests of `ResumeFromFailure` do not
  prove durable recovery.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Queue model research findings

#### Questions

1. What does failed-item resume do now?
2. Does the production command invoke that recovery?
3. Does release use the durable queue transaction owner?
4. What state must recovery preserve?

#### Findings

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

#### Patterns and risks

The pure mutation and the production command are different paths. Raw release
writes can reopen a queue after a durable write failed. The present resume
shape can leave a resumed item naming its prior terminal run.

#### Design constraints

- One durable, idempotent recovery transaction must change the queue, failed
  groups, and only failed items before dispatch restarts.
- It must clear or replace old run identity and preserve completed items.
- Acquire, undo, terminal release, and recovery must use the same transaction
  owner.
- Quarantine may clear only through explicit durable recovery classification.
