# Operator NFR findings

## Questions

1. What does pause, drain, and resume require?
2. What live behavior differs from startup recovery?
3. What transaction scope is required?

## Findings

- ON-027 and QM-054 require `active → paused-by-drain` before the
  `queue_paused{reason: operator_drain}` observation. In-flight runs continue
  to their checkpoint.
- Live operator resume is `paused-by-drain → active` followed by store wake.
  It must not revive paused-by-failure, completed, or cancelled queues.
- QM-055 keeps a persisted paused-by-drain queue paused across startup. It is
  not an automatic resume path.
- Named queue selection and already-target status are no-ops. A multi-queue
  operation can partly commit before a later queue fails. This slice must not
  claim global atomicity.

## Patterns to follow

- Use one transaction per queue and emit only after that queue commits.
- Keep live operator resume separate from startup repair.

## Risks

- The current direct operator writer changes installed memory before `Persist`.
- Emitting from inside a store transaction can re-enter the store while its
  write lock is held.

