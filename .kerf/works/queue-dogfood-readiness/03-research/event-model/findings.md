# Research — Event model

## Questions

1. Does a recovery event add an audit fact?
2. If it does, what payload and durability identify one recovered queue?
3. Which event must not be reused?

## Findings

- Section 8.10 has eight queue lifecycle events. Its `queue_paused` reason is
  an exhaustive two-value enum. The text reserves `queue_resumed` for v0.2.
- `EV-021`, `EV-022`, and `EV-INV-001` make events observational. Queue
  persistence remains the authority.
- `EV-027` requires the normal foundation amendment for a new cross-bus event.
  It needs payload, ordering, durability, idempotence, producer, and consumer
  rules.
- `operator_resuming` drives only `paused-by-drain` to active. It has no
  failed-item facts and is not a recovery event.

## Patterns to keep

- Emit any recovery event only after the queue transaction commits.
- Keep queue persistence as the authority. Do not reconstruct recovery from an
  event.

## Risks and decisions

- A `queue_resumed` event is useful audit evidence. It needs queue ID, name,
  re-armed IDs or count, and recovery time.
- No new event is also viable when durable queue state and direct RPC output
  give enough audit evidence. Make this choice explicit.
- Reusing `operator_resuming` would conflate drain release with failure
  recovery and retain the asynchronous-success defect.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Event model research findings

#### Questions

1. Is queue status enough to audit recovery request, result, and rejection?
2. Which fields make recovery observable across queue, group, item, and run?
3. What ordering must hold around recovery and renewed dispatch?

#### Findings

`specs/event-model.md` §8.10 has `queue_group_completed` and `queue_paused`.
Its pause reasons are `group_failure` and `operator_drain`; `queue_resumed` is
reserved for later work. `run_started`, `run_completed`, and `run_failed` are
the terminal item landmarks. Queue events emit after a durable queue write.
`daemon_shutdown` has no run or queue identity.

`QueueOperatorEventConsumer` documents drain resume as observable only through
queue status. The status alone does not prove a failed recovery request or
which items changed without a durable receipt.

#### Patterns and risks

Git and queue records remain authoritative. Events let an assessor find them.
Reusing drain-resume signals confuses two different transitions. JSONL cannot
become the recovery authority.

#### Design constraints

- Choose F-class recovery request/result events, or extend queue status with a
  persisted recovery receipt containing identity, prior/current state, item
  list or count, run identity, time, and rejection reason.
- Emit only after the transaction commits. State accepted, no-op, and rejected
  behavior.
- Preserve existing run events as the only item terminal events.
- Add replay and ordering proof that exposes a second dispatch or merge.
