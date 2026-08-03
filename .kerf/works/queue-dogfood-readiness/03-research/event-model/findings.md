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
