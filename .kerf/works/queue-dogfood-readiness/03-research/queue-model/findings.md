# Research — Queue model

## Questions

1. Which queue identity and failed items can recovery change?
2. What durable boundary proves recovery before dispatch restarts?
3. What happens if recovery write failure quarantines the queue?

## Findings

- Section 1.2 and Appendix A.3 defer `queue-resume` to v0.2. `QM-032` makes
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
