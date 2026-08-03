# Event model research findings

## Questions

1. Is queue status enough to audit recovery request, result, and rejection?
2. Which fields make recovery observable across queue, group, item, and run?
3. What ordering must hold around recovery and renewed dispatch?

## Findings

`specs/event-model.md` §8.10 has `queue_group_completed` and `queue_paused`.
Its pause reasons are `group_failure` and `operator_drain`; `queue_resumed` is
reserved for later work. `run_started`, `run_completed`, and `run_failed` are
the terminal item landmarks. Queue events emit after a durable queue write.
`daemon_shutdown` has no run or queue identity.

`QueueOperatorEventConsumer` documents drain resume as observable only through
queue status. The status alone does not prove a failed recovery request or
which items changed without a durable receipt.

## Patterns and risks

Git and queue records remain authoritative. Events let an assessor find them.
Reusing drain-resume signals confuses two different transitions. JSONL cannot
become the recovery authority.

## Design constraints

- Choose F-class recovery request/result events, or extend queue status with a
  persisted recovery receipt containing identity, prior/current state, item
  list or count, run identity, time, and rejection reason.
- Emit only after the transaction commits. State accepted, no-op, and rejected
  behavior.
- Preserve existing run events as the only item terminal events.
- Add replay and ordering proof that exposes a second dispatch or merge.
