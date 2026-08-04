# Event model findings

## Questions

1. What ordering applies to queue observations?
2. What does recovery use as source of truth?
3. Which events must the API avoid inventing?

## Findings

- QM-063 and QM-065 require durable mutation before normal observation and
  mutation-order event order. Event failure is diagnostic and does not roll
  back committed queue state.
- EV-021, EV-022, ON-030, and QM-002 keep JSONL out of recovery authority.
  Queue files, transaction records, git, and Beads state decide recovery.
- `queue_paused` accepts `group_failure` or `operator_drain`. There is no
  v0.1 `queue_resumed` event.
- Startup must not synthesize a pause event just because it loaded a paused
  queue.

## Patterns to follow

- Return transition facts from the queue boundary. Let the caller emit only
  after it receives a committed result.

## Risks

- A retrying caller can emit after a rejected or indeterminate transaction.
- A new resume event would change the wire contract outside this work.

